import { test, expect } from "@playwright/test";

test("kill board lists names and status only and moves a name on elimination", async ({ browser, request }) => {
  const owner = await (await request.post("/api/games", { data: { name: `Kill board ${Date.now()}` } })).json();
  const base = `/api/games/${owner.game.id}`;
  const auth = a => ({ Authorization: `Bearer ${a.token}` });
  const headers = auth(owner);
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob", "Charlie", "Dana"] } })).json();
  await request.post(`${base}/missions`, { headers, data: { texts: ["Shake hands", "Say pineapple", "Give a high-five", "Hum a tune"] } });
  const players = [];
  for (const p of roster.players) players.push(await (await request.post("/api/access", {
    data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" },
  })).json());
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(200);
  const getGame = async a => (await request.get(base, { headers: auth(a) })).json();
  const board = async a => (await request.get(`${base}/killboard`, { headers: auth(a) })).json();

  const killer = players[0];
  const killerState = await getGame(killer);
  const victim = players.find(p => p.game.me.id === killerState.me.victim.id);
  const observer = players.find(p => p !== killer && p !== victim);
  const victimName = victim.game.me.name;

  // API: exact keys, everyone active, sorted by name; organizer and players may read it.
  for (const viewer of [owner, observer]) {
    const { entries } = await board(viewer);
    expect(entries.map(e => e.name)).toEqual(["Alice", "Bob", "Charlie", "Dana"]);
    for (const e of entries) {
      expect(Object.keys(e).sort()).toEqual(["id", "name", "status"]);
      expect(e.status).toBe("active");
    }
  }
  expect((await request.get(`${base}/killboard`)).status()).toBe(401);

  const context = await browser.newContext();
  const errors = [];
  try {
    await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), observer);
    const page = await context.newPage();
    page.on("pageerror", e => errors.push(e.message));
    await page.goto(`/#/player/${owner.game.id}`);
    await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
    const panel = page.getByRole("region", { name: "Kill Board", exact: true });
    await expect(panel.getByTestId("killboard-active")).toContainText(victimName);
    await expect(panel.getByTestId("killboard-eliminated")).not.toContainText(victimName);
    await expect(panel).not.toContainText(/mission|victim|killed by|team/i);

    // Elimination: the observer page performs no action; MQTT moves the name.
    const claim = await (await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId: victim.game.me.id } })).json();
    expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(victim), data: { accept: true } })).status()).toBe(200);
    await expect(panel.getByTestId("killboard-eliminated")).toContainText(victimName);
    await expect(panel.getByTestId("killboard-active")).not.toContainText(victimName);
    const { entries } = await board(observer);
    expect(entries.map(e => e.name)).toEqual(["Alice", "Bob", "Charlie", "Dana"]);
    expect(entries.find(e => e.id === victim.game.me.id).status).toBe("eliminated");
    expect(errors).toEqual([]);
  } finally {
    await context.close();
    const state = await getGame(owner);
    if (state.status !== "ended") await request.post(`${base}/end`, { headers });
  }
});

test("kill board moves a withdrawn player to Withdrawn live", async ({ browser, request }) => {
  const owner = await (await request.post("/api/games", { data: { name: `Kill board withdrawal ${Date.now()}` } })).json();
  const base = `/api/games/${owner.game.id}`;
  const auth = a => ({ Authorization: `Bearer ${a.token}` });
  const headers = auth(owner);
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob", "Charlie", "Dana"] } })).json();
  await request.post(`${base}/missions`, { headers, data: { texts: ["Shake hands", "Say pineapple", "Give a high-five", "Hum a tune"] } });
  const players = [];
  for (const p of roster.players) players.push(await (await request.post("/api/access", {
    data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" },
  })).json());
  expect((await request.post(`${base}/start`, { headers })).status()).toBe(200);
  const getGame = async a => (await request.get(base, { headers: auth(a) })).json();
  const board = async a => (await request.get(`${base}/killboard`, { headers: auth(a) })).json();
  const killer = players[0];
  const killerState = await getGame(killer);
  const victim = players.find(p => p.game.me.id === killerState.me.victim.id);
  const leaving = players.find(p => p !== killer && p !== victim);
  const watcher = players.find(p => p !== leaving && p !== victim);

  const context = await browser.newContext();
  try {
    await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), watcher);
    const page = await context.newPage();
    await page.goto(`/#/player/${owner.game.id}`);
    await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
    const panel = page.getByRole("region", { name: "Kill Board", exact: true });
    await expect(panel.getByTestId("killboard-active")).toContainText(leaving.game.me.name);

    const pending = await (await request.post(`${base}/withdrawals`, { headers: auth(leaving) })).json();
    expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(leaving), data: { withdrawalToken: pending.withdrawalToken } })).status()).toBe(204);
    await expect(panel.getByTestId("killboard-withdrawn")).toContainText(leaving.game.me.name);
    await expect(panel.getByTestId("killboard-active")).not.toContainText(leaving.game.me.name);

    const claim = await (await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId: victim.game.me.id } })).json();
    expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(victim), data: { accept: true } })).status()).toBe(200);
    await expect(panel.getByTestId("killboard-eliminated")).toContainText(victim.game.me.name);
    await expect(panel.getByTestId("killboard-withdrawn")).toContainText(leaving.game.me.name);

    const { entries } = await board(watcher);
    const status = Object.fromEntries(entries.map(e => [e.id, e.status]));
    expect(status[leaving.game.me.id]).toBe("withdrawn");
    expect(status[victim.game.me.id]).toBe("eliminated");
    for (const e of entries) expect(Object.keys(e).sort()).toEqual(["id", "name", "status"]);
  } finally {
    await context.close();
    const state = await getGame(owner);
    if (state.status !== "ended") await request.post(`${base}/end`, { headers });
  }
});
