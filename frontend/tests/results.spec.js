import { test, expect } from "@playwright/test";

test("final result: hidden until the game ends, only agreed fields, shown to organizer and players", async ({ browser, request }) => {
  const owner = await (await request.post("/api/games", { data: { name: `Results ${Date.now()}` } })).json();
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
  const everyone = [owner, ...players];

  // No result before the end for anyone, and no kill counts in any mid-game view.
  for (const a of everyone) expect((await request.get(`${base}/result`, { headers: auth(a) })).status()).toBe(409);
  expect((await request.get(`${base}/result`)).status()).toBe(401);

  // The killer eliminates both opponents, one after the other (survival ending).
  const eliminate = async () => {
    const victimId = (await getGame(killer)).me.victim.id;
    const victim = players.find(p => p.game.me.id === victimId);
    const claim = await (await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId } })).json();
    expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(victim), data: { accept: true } })).status()).toBe(200);
  };
  await eliminate();
  for (const a of everyone) expect(JSON.stringify(await getGame(a))).not.toMatch(/kills/i);
  for (const a of everyone) expect((await request.get(`${base}/result`, { headers: auth(a) })).status()).toBe(409);
  await eliminate();

  const ended = await getGame(killer);
  expect(ended.status).toBe("ended");
  for (const a of everyone) expect(JSON.stringify(await getGame(a))).not.toMatch(/kills/i);

  // The result: exactly the agreed fields, the same for every viewer.
  const results = [];
  for (const a of everyone) {
    const response = await request.get(`${base}/result`, { headers: auth(a) });
    expect(response.status()).toBe(200);
    results.push(await response.json());
  }
  const result = results[0];
  expect(Object.keys(result).sort()).toEqual(["reason", "standings", "tie", "winners"]);
  expect(result.reason).toBe("survival");
  expect(result.tie).toBe(false);
  expect(result.winners).toEqual([{ kind: "player", id: killer.game.me.id, name: killer.game.me.name }]);
  expect(result.standings).toHaveLength(3);
  for (const s of result.standings) expect(Object.keys(s).sort()).toEqual(["id", "kills", "kind", "name"]);
  expect(result.standings[0]).toEqual({ kind: "player", id: killer.game.me.id, name: killer.game.me.name, kills: 2 });
  expect(result.standings.slice(1).map(s => s.kills)).toEqual([0, 0]);
  expect(JSON.stringify(result)).not.toMatch(/victim|mission|killer|eliminat|withdraw/i);
  for (const other of results) expect(other).toEqual(result);

  // The results screen appears for a player and for the organizer.
  const contexts = [];
  const errors = [];
  try {
    for (const [access, path] of [[killer, "player"], [owner, "organizer"]]) {
      const context = await browser.newContext();
      contexts.push(context);
      await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), access);
      const page = await context.newPage();
      page.on("pageerror", e => errors.push(e.message));
      await page.goto(`/#/${path}/${owner.game.id}`);
      const panel = page.getByRole("region", { name: "Final results", exact: true });
      await expect(panel).toBeVisible();
      await expect(panel.getByRole("heading", { name: "Winner", exact: true })).toBeVisible();
      await expect(panel.getByRole("listitem")).toContainText(killer.game.me.name);
      await expect(panel).not.toContainText("It’s a tie");
      const rows = panel.getByRole("row");
      await expect(rows).toHaveCount(4); // header + three players
      await expect(rows.nth(1)).toContainText(killer.game.me.name);
      await expect(rows.nth(1)).toContainText("2");
    }
    expect(errors).toEqual([]);
  } finally {
    for (const context of contexts) await context.close();
  }
});
