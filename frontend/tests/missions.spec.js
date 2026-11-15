import { test, expect } from "@playwright/test";

test("batch lobby missions, disable and re-enable, recover a failed batch, and lock on start", async ({ page, request }, testInfo) => {
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Lobby missions ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  const config = page.getByRole("region", { name: "Lobby missions", exact: true });
  const list = config.getByRole("region", { name: "Mission list", exact: true });
  const draft = config.getByLabel("Mission texts", { exact: true });
  await expect(config.getByText("0 enabled / 0 total", { exact: true })).toBeVisible();
  await draft.fill("  Get a high-five.  \n\nSay pineapple.\nGet a high-five.\n");
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(list.getByRole("listitem")).toHaveCount(3);
  await expect(draft).toHaveValue("");
  await expect(list.getByText("Get a high-five.", { exact: true })).toHaveCount(2);

  const [mirror] = await Promise.all([
    page.waitForEvent("popup"),
    page.evaluate(() => window.open(location.href)),
  ]);
  try {
    await expect(mirror.getByText("3 enabled / 3 total", { exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Disable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
    await expect(mirror.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
    await page.reload();
    await expect(list.getByRole("button", { name: "Enable mission: Say pineapple.", exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Enable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("3 enabled / 3 total", { exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Disable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
  } finally {
    await mirror.close();
  }

  // A later failed batch must retain only unsaved lines, so retry adds no duplicates.
  let batches = 0;
  await page.route("**/api/games/*/missions", async (route) => {
    if (route.request().method() === "POST" && ++batches === 2) {
      await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Temporary mission test failure" }) });
    } else {
      await route.continue();
    }
  });
  const many = Array.from({ length: 105 }, (_, i) => `Batch mission ${i + 1}`);
  await draft.fill(many.join("\n"));
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Temporary mission test failure");
  await expect(draft).toHaveValue(many.slice(100).join("\n"));
  await expect(list.getByRole("listitem")).toHaveCount(103);
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(draft).toHaveValue("");
  await expect(config.getByText("107 enabled / 108 total", { exact: true })).toBeVisible();
  await expect(list.getByRole("listitem")).toHaveCount(108);

  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const view = await (await request.get(base, { headers })).json();
  expect(view.missions.filter((m) => !m.enabled).map((m) => m.text)).toEqual(["Say pineapple."]);
  const missionID = view.missions[0].id;
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice"] } })).json();
  const player = await (await request.post("/api/access", { data: {
    invitationCode: owner.game.invitationCode, code: roster.players[0].code, role: "player",
  } })).json();
  expect(player.game.missions).toBeUndefined();
  const playerHeaders = { Authorization: `Bearer ${player.token}` };
  expect((await request.post(`${base}/missions`, { headers: playerHeaders, data: { texts: ["Forbidden"] } })).status()).toBe(403);
  expect((await request.patch(`${base}/missions/${missionID}`, { headers: playerHeaders, data: { enabled: false } })).status()).toBe(403);
  expect((await request.post(`${base}/missions`, { data: { texts: ["Anonymous"] } })).status()).toBe(401);
  for (const data of [{}, { enabled: null }, { enabled: "false" }]) {
    expect((await request.patch(`${base}/missions/${missionID}`, { headers, data })).status()).toBe(400);
  }
  const invalid = await request.post(`${base}/missions`, { headers, data: { texts: ["Valid", " "] } });
  expect(invalid.status()).toBe(400);
  const other = await (await request.post("/api/games", { data: { name: "Isolated mission lobby" } })).json();
  expect(other.game.missions).toEqual([]);
  const otherHeaders = { Authorization: `Bearer ${other.token}` };
  expect((await request.patch(`/api/games/${other.game.id}/missions/${missionID}`, { headers: otherHeaders, data: { enabled: false } })).status()).toBe(404);
  expect((await request.patch(`/api/games/${other.game.id}/missions/${missionID}`, { headers, data: { enabled: false } })).status()).toBe(403);

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await config.screenshot({ path: testInfo.outputPath("mobile-lobby-missions.png") });
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByText("Game active", { exact: true })).toBeVisible();
  await expect(config.getByRole("button")).toHaveCount(0);
  expect((await request.post(`${base}/missions`, { headers, data: { texts: ["Late"] } })).status()).toBe(409);
  expect((await request.patch(`${base}/missions/${missionID}`, { headers, data: { enabled: false } })).status()).toBe(409);
  for (const [id, auth] of [[owner.game.id, headers], [other.game.id, otherHeaders]]) {
    expect((await request.post(`/api/games/${id}/end`, { headers: auth })).status()).toBe(200);
  }
  expect(errors).toEqual([]);
});
