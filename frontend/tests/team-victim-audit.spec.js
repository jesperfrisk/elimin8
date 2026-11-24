import { test, expect } from "@playwright/test";

for (const mode of ["elimination", "withdrawal", "mixed"]) {
  test(`teammate victims stay impossible through ${mode} transfers`, async ({ request, browser }) => {
    const ownerResponse = await request.post("/api/games", { data: { name: `Teammate audit ${mode} ${Date.now()}` } });
    expect(ownerResponse.ok()).toBe(true);
    const owner = await ownerResponse.json();
    const base = `/api/games/${owner.game.id}`;
    const auth = a => ({ Authorization: `Bearer ${a.token}` });
    const headers = auth(owner);
    const get = async a => {
      const response = await request.get(base, { headers: auth(a) });
      expect(response.ok()).toBe(true);
      return response.json();
    };
    let context;
    try {
      const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["A1", "A2", "B1", "B2", "C1", "C2"] } })).json();
      const players = [];
      for (const p of roster.players) {
        const response = await request.post("/api/access", { data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" } });
        expect(response.ok()).toBe(true);
        players.push(await response.json());
      }
      expect((await request.post(`${base}/missions`, { headers, data: { texts: ["Audit mission"] } })).ok()).toBe(true);
      expect((await request.patch(`${base}/settings`, { headers, data: { mode: "team" } })).ok()).toBe(true);
      const teams = [0, 1, 2].map(i => ({ name: `Team ${i}`, playerIds: roster.players.slice(i * 2, i * 2 + 2).map(p => p.id) }));
      expect((await request.put(`${base}/teams`, { headers, data: { teams } })).ok()).toBe(true);
      expect((await request.post(`${base}/start`, { headers })).ok()).toBe(true);
      const teamFor = Object.fromEntries(teams.flatMap(t => t.playerIds.map(id => [id, t.name])));
      const byId = Object.fromEntries(players.map(p => [p.game.me.id, p]));
      context = await browser.newContext();
      await context.addInitScript(a => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), players[0]);
      const page = await context.newPage();
      await page.goto(`/#/player/${owner.game.id}`);
      const initial = await get(players[0]);
      await expect(page.getByRole("region", { name: "Your victim", exact: true })).toContainText(initial.me.victim.name);
      for (let step = 0; step < 6; step++) {
        const state = await get(owner);
        const views = await Promise.all(players.map(get));
        const living = views.filter(v => v.me.included && !v.me.eliminatedAt && v.me.status !== "withdrawn");
        if (state.status === "ended") {
          expect(state.winner.kind).toBe("team");
          const winners = living.filter(v => v.me.team.name === state.winner.name);
          expect(winners.length).toBeGreaterThan(living.length / 2);
          for (const v of views) expect(v.me.victim).toBeUndefined();
          break;
        }
        expect(living.length).toBeGreaterThan(1);
        for (const v of living) {
          expect(v.me.victim, `${mode}: ${v.me.name} has a victim`).toBeDefined();
          expect(teamFor[v.me.victim.id], `${mode} after ${step} transfers, status=${state.status}, remaining=${living.map(p => p.me.name).join(",")}: ${v.me.name} targets teammate ${v.me.victim.name}`).not.toBe(teamFor[v.me.id]);
        }
        const needsRepair = living.find(hunter => {
          const victim = living.find(v => v.me.id === hunter.me.victim.id);
          return teamFor[victim.me.victim.id] === teamFor[hunter.me.id];
        });
        const hunter = needsRepair || living[0];
        const victim = living.find(v => v.me.id === hunter.me.victim.id);
        if (mode === "withdrawal" || (mode === "mixed" && step % 2 === 0)) {
          const response = await request.post(`${base}/withdrawals`, { headers: auth(byId[victim.me.id]) });
          expect(response.ok()).toBe(true);
          const pending = await response.json();
          expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(byId[victim.me.id]), data: { withdrawalToken: pending.withdrawalToken } })).status()).toBe(204);
        } else {
          const response = await request.post(`${base}/eliminations`, { headers: auth(byId[hunter.me.id]), data: { victimId: victim.me.id } });
          expect(response.ok()).toBe(true);
          const claim = await response.json();
          expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(byId[victim.me.id]), data: { accept: true } })).ok()).toBe(true);
        }
      }
      expect((await get(owner)).status).toBe("ended");
    } finally {
      if (context) await context.close();
      const state = await get(owner);
      if (state.status !== "ended") await request.post(`${base}/end`, { headers });
    }
  });
}
