import { test, expect } from "@playwright/test";

test("two-step withdrawal transfers the victim and mission, records no kill, and a last survivor wins", async ({ browser, request }) => {
  const owner = await (await request.post("/api/games", { data: { name: `Withdrawals ${Date.now()}` } })).json();
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
  const byId = id => players.find(p => p.game.me.id === id);

  const leaving = players[0];
  const leavingBefore = await getGame(leaving);
  const hunter = players.find(p => p !== leaving && p.game.me.id !== leavingBefore.me.victim.id);
  const hunterBefore = await getGame(hunter);
  expect(hunterBefore.me.victim.id).toBe(leaving.game.me.id);
  const third = byId(leavingBefore.me.victim.id);

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
    // Only players can withdraw; requests need a valid token and unconfirmed ones do nothing.
    expect((await request.post(`${base}/withdrawals`, { headers })).status()).toBe(403);
    expect((await request.post(`${base}/withdrawals`)).status()).toBe(401);
    expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(leaving), data: { withdrawalToken: "WRONG" } })).status()).toBe(409);
    expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(leaving), data: {} })).status()).toBe(400);

    const leavingPage = await open(leaving);
    const hunterPage = await open(hunter);
    const panel = leavingPage.getByRole("region", { name: "Withdrawal", exact: true });
    const confirmation = leavingPage.getByRole("region", { name: "Withdrawal confirmation", exact: true });
    await expect(hunterPage.getByRole("region", { name: "Your victim", exact: true })).toContainText(leaving.game.me.name);

    // Step one only asks for confirmation; backing out changes nothing.
    await panel.getByRole("button", { name: "Withdraw from game" }).click();
    await expect(confirmation).toBeVisible();
    expect((await getGame(leaving)).me.status).toBe("active");
    await confirmation.getByRole("button", { name: "Keep playing" }).click();
    await expect(confirmation).toHaveCount(0);
    expect((await getGame(leaving)).me.status).toBe("active");

    // A failed confirmation keeps the player in the game and reports the error.
    await panel.getByRole("button", { name: "Withdraw from game" }).click();
    await leavingPage.route("**/withdrawals/confirm", route => route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Temporary withdrawal failure" }) }));
    await confirmation.getByRole("button", { name: "Confirm withdrawal" }).click();
    await expect(leavingPage.getByRole("alert").filter({ hasText: "Temporary withdrawal failure" })).toBeVisible();
    await expect(confirmation).toBeVisible();
    await leavingPage.unroute("**/withdrawals/confirm");
    expect((await getGame(leaving)).me.status).toBe("active");

    // Step two withdraws. The hunter's page updates over MQTT with no action.
    await confirmation.getByRole("button", { name: "Confirm withdrawal" }).click();
    await expect(leavingPage.getByRole("heading", { name: "You have withdrawn." })).toBeVisible();
    await expect(hunterPage.getByRole("region", { name: "Your victim", exact: true })).toContainText(third.game.me.name);
    await expect(hunterPage.getByRole("region", { name: "Your mission", exact: true })).toContainText(leavingBefore.me.mission);
    await expect(leavingPage.getByRole("region", { name: "Your victim", exact: true })).toHaveCount(0);
    await expect(leavingPage.getByRole("region", { name: "Your mission", exact: true })).toHaveCount(0);
    await expect(panel).toHaveCount(0);
    await expect(leavingPage.getByRole("region", { name: "Eliminations", exact: true }).getByRole("button", { name: "Confirm elimination report" })).toHaveCount(0);

    // API: status only, no assignments, no kill, no extra fields for other roles.
    const hunterAfter = await getGame(hunter);
    expect(hunterAfter.me.victim).toEqual(leavingBefore.me.victim);
    expect(hunterAfter.me.mission).toBe(leavingBefore.me.mission);
    const withdrawnView = await getGame(leaving);
    expect(withdrawnView.me.status).toBe("withdrawn");
    expect(withdrawnView.me.victim).toBeUndefined();
    expect(withdrawnView.me.mission).toBeUndefined();
    const observerView = await getGame(third);
    const statuses = Object.fromEntries(observerView.participants.map(p => [p.id, p.status]));
    expect(statuses).toEqual({ [leaving.game.me.id]: "withdrawn", [hunter.game.me.id]: "active", [third.game.me.id]: "active" });
    for (const p of observerView.participants) expect(Object.keys(p).sort()).toEqual(["id", "name", "status"]);
    expect(JSON.stringify(observerView)).not.toMatch(/withdrawnAt|withdrawals|confirmedKills/);
    expect((await getGame(owner)).players.find(p => p.id === leaving.game.me.id).status).toBe("withdrawn");

    // A withdrawn player cannot withdraw again or report kills.
    expect((await request.post(`${base}/withdrawals`, { headers: auth(leaving) })).status()).toBe(409);
    expect((await request.post(`${base}/eliminations`, { headers: auth(leaving), data: { victimId: hunter.game.me.id } })).status()).toBe(409);

    // With one other player left, withdrawing ends the game and they win.
    const lastRequest = await (await request.post(`${base}/withdrawals`, { headers: auth(third) })).json();
    expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(third), data: { withdrawalToken: lastRequest.withdrawalToken } })).status()).toBe(204);
    await expect(hunterPage.getByText("Game ended", { exact: true })).toBeVisible();
    const finished = await getGame(hunter);
    expect(finished.status).toBe("ended");
    expect(finished.winnerId).toBe(hunter.game.me.id);
    expect(finished.me.victim).toBeUndefined();
    expect(errors).toEqual([]);
  } finally {
    for (const context of contexts) await context.close();
    const state = await getGame(owner);
    if (state.status !== "ended") await request.post(`${base}/end`, { headers });
  }
});
