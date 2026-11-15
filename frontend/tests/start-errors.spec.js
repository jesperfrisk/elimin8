import { test, expect } from "@playwright/test";

test("start errors stay visible beside controls and explain how to recover", async ({ page, request }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Start errors ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  await expect(page.getByText("Lobby open", { exact: true })).toBeVisible();
  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice"] } })).json();
  await request.post("/api/access", { data: { invitationCode: owner.game.invitationCode, code: roster.players[0].code, role: "player" } });
  await expect(page.getByRole("row").filter({ hasText: "Alice" }).getByText("Joined", { exact: true })).toBeVisible();
  page.on("dialog", (dialog) => dialog.accept());
  const alert = page.locator(".game-action-error");
  async function startAndExpect(message) {
    await page.getByRole("button", { name: "Start game" }).click();
    await expect(alert).toContainText(message);
    await expect(alert).toBeFocused();
    await expect(alert).toBeInViewport();
    await expect(page.getByText("Lobby open", { exact: true })).toBeVisible();
  }
  await startAndExpect("Add or enable at least one mission");
  const added = await (await request.post(`${base}/missions`, { headers, data: { texts: ["Disabled task"] } })).json();
  await request.patch(`${base}/missions/${added.missions[0].id}`, { headers, data: { enabled: false } });
  await expect(page.getByText("0 enabled / 1 total", { exact: true })).toBeVisible();
  await startAndExpect("Add or enable at least one mission");
  await alert.screenshot({ path: testInfo.outputPath("missing-mission-error.png") });
  let failure;
  await page.route("**/api/games/*/start", (route) => failure.abort
    ? route.abort("failed")
    : route.fulfill({ status: failure.status, contentType: "application/json", body: JSON.stringify({ error: failure.error || "Generic server response" }) }));
  for (const [status, expected, error] of [
    [400, "data is invalid or too large"],
    [401, "organizer access is invalid or expired"],
    [403, "Only this game's organizer can start it"],
    [404, "game could not be found"],
    [409, "already started", "This game has already started. Refresh the game to see its current state."],
    [409, "cannot be started again", "This game has ended and cannot be started again. Create a new game to play again."],
    [409, "Refresh the game and try again", "The game changed while you were working. Refresh the game and try again."],
    [429, "Too many requests"],
    [500, "could not confirm whether the game started"],
    [502, "could not confirm whether the game started"],
  ]) {
    failure = { status, error };
    await startAndExpect(expected);
    if ([401, 403, 404].includes(status)) {
      await expect(alert.getByRole("link", { name: "Open organizer desk" })).toBeVisible();
    }
  }
  failure = { abort: true };
  await startAndExpect("Check your connection and refresh the game");
  await alert.getByRole("button", { name: "Refresh game", exact: true }).click();
  await expect(alert).toHaveCount(0);
  await page.unroute("**/api/games/*/start");
  await request.patch(`${base}/missions/${added.missions[0].id}`, { headers, data: { enabled: true } });
  await expect(page.getByText("1 enabled / 1 total", { exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByText("Game active", { exact: true })).toBeVisible();
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(409);
  await request.post(`${base}/end`, { headers });
  const ended = await request.post(`${base}/start`, { headers });
  expect(ended.status()).toBe(409);
  expect((await ended.json()).error).toContain("has ended and cannot be started again");
});
