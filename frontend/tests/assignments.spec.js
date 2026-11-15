import { test, expect } from "@playwright/test";

test("start assigns private missions in complete rounds before reuse", async ({ page, request }, testInfo) => {
  const owner = await (await request.post("/api/games", { data: { name: `Mission assignment ${Date.now()}` } })).json();
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const roster = await (await request.post(`${base}/players`, { headers, data: {
    names: ["Alice", "Bob", "Charlie", "Diana", "Eve", "Frank", "Grace", "Unjoined"],
  } })).json();
  await page.goto("/#/player");
  await page.getByLabel("Game invitation code").fill(owner.game.invitationCode);
  await page.getByLabel("Your private player code").fill(roster.players[0].code);
  await page.getByRole("button", { name: "Enter game" }).click();
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
  await expect(page.getByRole("region", { name: "Your mission", exact: true })).toHaveCount(0);
  const noMissions = await request.post(`${base}/start`, { headers });
  expect(noMissions.status()).toBe(409);
  expect((await noMissions.json()).error).toContain("enable at least one mission");
  const missions = await (await request.post(`${base}/missions`, { headers, data: {
    texts: ["Get a high-five.", "Say pineapple.", "Take a selfie.", "Disabled task"],
  } })).json();
  await request.patch(`${base}/missions/${missions.missions[3].id}`, { headers, data: { enabled: false } });
  const admissions = [];
  for (const player of roster.players.slice(1, 7)) {
    const login = await request.post("/api/access", { data: {
      invitationCode: owner.game.invitationCode, code: player.code, role: "player",
    } });
    expect(login.status()).toBe(200);
    admissions.push(await login.json());
  }
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(200);
  await expect(page.getByRole("heading", { name: "The game is on." })).toBeVisible();
  const game = await (await request.get(base, { headers })).json();
  const assigned = game.players.filter((p) => p.included);
  expect(assigned).toHaveLength(7);
  expect(game.players.find((p) => !p.included).mission).toBeUndefined();
  const allowed = missions.missions.slice(0, 3).map((m) => m.text).sort();
  for (const offset of [0, 3]) {
    expect(assigned.slice(offset, offset + 3).map((p) => p.mission).sort()).toEqual(allowed);
  }
  expect(allowed).toContain(assigned[6].mission);
  const own = assigned[0].mission;
  const mission = page.getByRole("region", { name: "Your mission", exact: true });
  await expect(mission.getByText(own, { exact: true })).toBeVisible();
  for (let i = 0; i < admissions.length; i++) {
    const privateView = await (await request.get(base, { headers: { Authorization: `Bearer ${admissions[i].token}` } })).json();
    expect(privateView.players).toBeUndefined();
    expect(privateView.missions).toBeUndefined();
    expect(privateView.me.mission).toBe(assigned[i + 1].mission);
    expect(Object.hasOwn(privateView, "mission")).toBe(false);
  }
  await page.reload();
  await expect(mission.getByText(own, { exact: true })).toBeVisible();
  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await mission.screenshot({ path: testInfo.outputPath("private-mission.png") });
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(409);
  const afterRetry = await (await request.get(base, { headers })).json();
  expect(afterRetry.players.map((p) => p.mission)).toEqual(game.players.map((p) => p.mission));
  expect((await request.post(`${base}/end`, { headers })).status()).toBe(200);
  await expect(page.getByRole("heading", { name: "This game has ended." })).toBeVisible();
  await expect(mission.getByText(own, { exact: true })).toBeVisible();
});
