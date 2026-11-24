import { test, expect } from "@playwright/test";

test("organizer sets up teams, start explains blockers, player sees only their own team", async ({ page, browser, request }) => {
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Teams ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  await expect(page.getByRole("region", { name: "Teams", exact: true })).toBeVisible();
  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob", "Cara", "Dave"] } })).json();
  const players = [];
  for (const p of roster.players) {
    players.push(await (await request.post("/api/access", {
      data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" },
    })).json());
  }
  await request.post(`${base}/missions`, { headers, data: { texts: ["Say pineapple."] } });

  const panel = page.getByRole("region", { name: "Teams", exact: true });
  await expect(panel).toBeVisible();
  await panel.getByRole("button", { name: "Teams", exact: true }).click();
  await expect(panel.getByTestId("team-blocker")).toContainText("4 unassigned");

  const start = page.getByRole("button", { name: /Start game/ });
  await expect(start).toBeDisabled();

  await panel.getByLabel("Number of teams").fill("2");
  await panel.getByRole("button", { name: "Randomize teams" }).click();
  await expect(panel.getByRole("group")).toHaveCount(2);
  await expect(panel.getByTestId("team-blocker")).toHaveCount(0);
  await expect(start).toBeEnabled();

  // Rename a team manually and save.
  await panel.getByLabel("Team name").first().fill("Red Rockets");
  await panel.getByRole("button", { name: "Save teams" }).click();
  await expect(panel.getByRole("group", { name: "Red Rockets" })).toBeVisible();

  await start.click();
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();

  const organizerView = await (await request.get(base, { headers })).json();
  const red = organizerView.teams.find(t => t.name === "Red Rockets");
  const other = organizerView.teams.find(t => t.name !== "Red Rockets");
  const names = Object.fromEntries(roster.players.map(p => [p.id, p.name]));
  const member = players.find(p => red.playerIds.includes(p.game.me.id));

  const context = await browser.newContext();
  try {
    await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), member);
    const responses = [];
    const playerPage = await context.newPage();
    playerPage.on("response", async r => {
      if (r.url().includes(`/api/games/${owner.game.id}`)) responses.push(await r.text().catch(() => ""));
    });
    await playerPage.goto(`/#/player/${owner.game.id}`);
    const own = playerPage.getByRole("region", { name: "Your team" });
    await expect(own.getByRole("heading", { name: "Your team: Red Rockets" })).toBeVisible();
    for (const id of red.playerIds) {
      if (id !== member.game.me.id) await expect(own).toContainText(names[id]);
    }
    await expect(playerPage.getByText(other.name)).toHaveCount(0);
    expect(responses.some(b => b.includes(other.name))).toBe(false);
  } finally {
    await context.close();
  }
});
