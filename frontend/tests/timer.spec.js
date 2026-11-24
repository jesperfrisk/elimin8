import { test, expect } from "@playwright/test";

test("organizer sets and clears the time limit, and everyone sees a countdown once started", async ({ page, request }) => {
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Timer ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  const panel = page.getByRole("region", { name: "Game timer", exact: true });
  await expect(panel).toBeVisible();
  await panel.getByLabel("Minutes (1 to 10080)").fill("45");
  await panel.getByRole("button", { name: "Set time limit" }).click();
  await expect(panel.getByText("Current limit: 45 minutes.")).toBeVisible();
  await panel.getByRole("button", { name: "Clear time limit" }).click();
  await expect(panel.getByText("Current limit: 45 minutes.")).toHaveCount(0);
  await panel.getByLabel("Minutes (1 to 10080)").fill("30");
  await panel.getByRole("button", { name: "Set time limit" }).click();
  await expect(panel.getByText("Current limit: 30 minutes.")).toBeVisible();

  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob"] } })).json();
  for (const p of roster.players) {
    await request.post("/api/access", { data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" } });
  }
  await request.post(`${base}/missions`, { headers, data: { texts: ["Say pineapple."] } });
  await expect(page.getByRole("row").filter({ hasText: "Bob" }).getByText("Joined", { exact: true })).toBeVisible();
  page.on("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByTestId("countdown")).toHaveText(/^00:(29|30):\d\d$/);

  const rejected = await request.put(`${base}/timer`, { headers, data: { durationMinutes: 5 } });
  expect(rejected.status()).toBe(409);
});
