import { test, expect } from "@playwright/test";

test("assigned target only, mutual confirmation via MQTT, decline, transfer, reconnect, and final survivor", async ({ browser, request }, testInfo) => {
  const owner = await (await request.post("/api/games", { data: { name: `Eliminations ${Date.now()}` } })).json();
  const base = `/api/games/${owner.game.id}`;
  const auth = a => ({ Authorization: `Bearer ${a.token}` });
  const headers = auth(owner);
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob", "Charlie"] } })).json();
  await request.post(`${base}/missions`, { headers, data: { texts: ["Shake hands", "Say pineapple", "Give a high-five"] } });
  const players = [];
  for (const p of roster.players) players.push(await (await request.post("/api/access", {
    data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" },
  })).json());
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(200);
  const getGame = async a => (await request.get(base, { headers: auth(a) })).json();
  const killer = players[0];
  const before = await getGame(killer);
  const victim = players.find(p => p.game.me.id === before.me.victim.id);
  const observer = players.find(p => p !== killer && p !== victim);
  const victimBefore = await getGame(victim);
  const contexts = [];
  const errors = [];
  const open = async access => {
    const context = await browser.newContext();
    contexts.push(context);
    await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), access);
    const page = await context.newPage();
    page.on("pageerror", e => errors.push(e.message));
    await page.goto(`/#/player/${owner.game.id}`);
    await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
    return page;
  };
  try {
    const killerPage = await open(killer);
    const victimPage = await open(victim);
    const observerPage = await open(observer);
    const reports = killerPage.getByRole("region", { name: "Eliminations", exact: true });
    const prompt = victimPage.getByRole("region", { name: "Elimination confirmation", exact: true });
    await expect(reports.getByRole("searchbox")).toHaveCount(0);
    await expect(reports.getByRole("radio")).toHaveCount(0);
    await expect(reports).toContainText(`Report that you killed ${victim.game.me.name}?`);
    expect((await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId: observer.game.me.id } })).status()).toBe(409);
    expect((await getGame(observer)).eliminations).toBeUndefined();
    await reports.getByRole("button", { name: "Confirm elimination report" }).click();
    // The victim and observer pages perform no user actions or reloads here:
    // only the MQTT invalidation can deliver the pending state to their screens.
    await expect(prompt).toContainText(`${killer.game.me.name} says they have killed you. Is this correct?`);
    await expect(observerPage.getByRole("region", { name: "Elimination confirmation" })).toHaveCount(0);
    expect((await getGame(observer)).eliminations).toBeUndefined();
    let state = await getGame(victim);
    let claim = state.eliminations[0];
    expect(state.me.victim).toEqual(victimBefore.me.victim);
    expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(killer), data: { accept: true } })).status()).toBe(403);
    for (const data of [{}, { accept: null }, { accept: "false" }, { accept: false, extra: true }]) {
      expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(victim), data })).status()).toBe(400);
    }
    expect((await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId: victim.game.me.id } })).status()).toBe(409);
    expect((await request.post(`${base}/eliminations`, { headers, data: { victimId: victim.game.me.id } })).status()).toBe(403);
    expect((await request.post(`${base}/eliminations`, { data: { victimId: victim.game.me.id } })).status()).toBe(401);
    // A failed response keeps the request available for a retry.
    await victimPage.route("**/eliminations/*/response", route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Temporary confirmation failure" }) }));
    await prompt.getByRole("button", { name: "Decline elimination" }).click();
    await expect(victimPage.getByRole("alert").filter({ hasText: "Temporary confirmation failure" })).toBeVisible();
    await expect(prompt).toBeVisible();
    await victimPage.unroute("**/eliminations/*/response");
    await prompt.getByRole("button", { name: "Decline elimination" }).click();
    await expect(prompt).toHaveCount(0);
    await expect(killerPage.getByRole("region", { name: "Your elimination reports" })).toContainText("declined");
    expect((await getGame(killer)).me).toEqual(before.me);

    await reports.getByRole("button", { name: "Confirm elimination report" }).click();
    await expect(prompt).toBeVisible();
    await victimPage.reload();
    await expect(prompt).toBeVisible();
    await victimPage.setViewportSize({ width: 390, height: 844 });
    expect(await victimPage.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
    await prompt.screenshot({ path: testInfo.outputPath("mobile-elimination-confirmation.png") });
    await prompt.getByRole("button", { name: "Accept elimination" }).click();
    await expect(victimPage.getByRole("heading", { name: "You have been eliminated." })).toBeVisible();
    await expect(killerPage.getByRole("region", { name: "Your victim", exact: true })).toContainText(victimBefore.me.victim.name);
    await expect(killerPage.getByRole("region", { name: "Your mission", exact: true })).toContainText(victimBefore.me.mission);
    await expect(victimPage.getByRole("region", { name: "Your victim", exact: true })).toHaveCount(0);
    await expect(victimPage.getByRole("region", { name: "Your mission", exact: true })).toHaveCount(0);
    await expect(observerPage.getByRole("region", { name: "Elimination confirmation" })).toHaveCount(0);
    const after = await getGame(killer);
    expect(after.me.victim).toEqual(victimBefore.me.victim);
    expect(after.me.mission).toBe(victimBefore.me.mission);
    await killerPage.reload();
    await expect(killerPage.getByRole("region", { name: "Your mission", exact: true })).toContainText(victimBefore.me.mission);
    await expect(reports).toContainText(`Report that you killed ${observer.game.me.name}?`);
    await reports.getByRole("button", { name: "Confirm elimination report" }).click();
    await observerPage.getByRole("button", { name: "Accept elimination" }).click();
    await expect(killerPage.getByText(`${killer.game.me.name} is the last player standing!`)).toBeVisible();
    await expect(killerPage.getByText("Game ended", { exact: true })).toBeVisible();
    state = await getGame(killer);
    expect(state.winnerId).toBe(killer.game.me.id);
    expect(state.me.victim).toBeUndefined();
    expect(errors).toEqual([]);
  } finally {
    for (const context of contexts) await context.close();
    const state = await getGame(owner);
    if (state.status !== "ended") await request.post(`${base}/end`, { headers });
  }
});
