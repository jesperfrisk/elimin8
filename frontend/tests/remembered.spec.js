import { test, expect } from "@playwright/test";
import { nextRenewalDelay, sessionFailure, RENEW_LEAD_MS, MIN_RENEW_DELAY_MS } from "../src/rememberedGames.js";

// ---- Pure logic: no browser, no backend. ----

test("nextRenewalDelay renews 15 minutes early, never in a tight loop, and stops at the 30-day limit", () => {
  const now = Date.parse("2030-01-01T00:00:00Z");
  const at = (ms) => new Date(now + ms).toISOString();
  // A fresh 12 hour admission renews 15 minutes before it ends.
  expect(nextRenewalDelay(at(12 * 3600e3), now)).toBe(12 * 3600e3 - RENEW_LEAD_MS);
  // Already inside the window: renew soon, but not instantly.
  expect(nextRenewalDelay(at(10 * 60e3), now)).toBe(MIN_RENEW_DELAY_MS);
  expect(nextRenewalDelay(at(-60e3), now)).toBe(MIN_RENEW_DELAY_MS);
  // The replacement is barely longer than the admission it replaced (the fixed 30-day
  // deadline is near): no further renewal is scheduled.
  expect(nextRenewalDelay(at(20 * 60e3), now, at(20 * 60e3 - 5000))).toBeNull();
  expect(nextRenewalDelay(at(12 * 3600e3), now, at(11 * 3600e3))).toBe(12 * 3600e3 - RENEW_LEAD_MS);
  expect(nextRenewalDelay("not a date", now)).toBeNull();
});

test("sessionFailure tells a lost sign-in from a temporary problem", () => {
  expect(sessionFailure({ status: 401 })).toBe("ended");
  expect(sessionFailure({ status: 429 })).toBe("busy");
  expect(sessionFailure({ status: 403 })).toBe("error");
  expect(sessionFailure({ status: 500 })).toBe("error");
  expect(sessionFailure(new TypeError("Failed to fetch"))).toBe("offline");
});

// ---- Remembered player games in a real browser against the real backend. ----

const auth = (token) => ({ Authorization: `Bearer ${token}` });

async function createGame(request, names) {
  const owner = await (await request.post("/api/games", { data: { name: `Remember ${Date.now()}-${Math.random().toString(36).slice(2, 6)}` } })).json();
  const base = `/api/games/${owner.game.id}`;
  const roster = await (await request.post(`${base}/players`, { headers: auth(owner.token), data: { names } })).json();
  return { owner, id: owner.game.id, name: owner.game.name, invitation: owner.game.invitationCode, base, players: roster.players };
}

// A real code login in the browser: this is what sets the HttpOnly remember cookie.
async function signIn(page, game, player) {
  await page.goto("/#/player");
  await page.getByLabel("Game invitation code").fill(game.invitation);
  await page.getByLabel("Your private player code").fill(player.code);
  await page.getByRole("button", { name: "Enter game" }).click();
  await expect(page.getByText(`Playing as ${player.name}`)).toBeVisible();
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
}

// Closing and reopening the browser: cookies and localStorage survive, sessionStorage does not.
async function restart(browser, baseURL, context, { dropCookies = false } = {}) {
  const state = await context.storageState();
  await context.close();
  if (dropCookies) state.cookies = [];
  const next = await browser.newContext({ baseURL, storageState: state });
  return { context: next, page: await next.newPage() };
}

const dump = (page) =>
  page.evaluate(() => ({
    local: Object.fromEntries(Object.entries(localStorage)),
    session: Object.fromEntries(Object.entries(sessionStorage)),
    url: location.href,
    cookie: document.cookie,
  }));
const tabAccess = async (page) => JSON.parse((await dump(page)).session["elimin8.access.v1"] || "[]");
const rememberCookie = async (context, id) => (await context.cookies()).find((c) => c.name === `elimin8_remember_${id}`);
const cookieNames = async (context) => (await context.cookies()).map((c) => c.name).sort();

function track(page) {
  const calls = [];
  page.on("response", (response) => {
    const url = new URL(response.url());
    if (url.pathname.startsWith("/api/") && url.pathname !== "/api/mqtt") calls.push({ method: response.request().method(), path: url.pathname, status: response.status() });
  });
  return calls;
}

test("a restarted browser signs the same player back in; only non-secret metadata is stored", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const [alice] = game.players;
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  const messages = [];
  page.on("console", (m) => messages.push(m.text()));
  await signIn(page, game, alice);

  // What was stored: a cookie JavaScript cannot read, and three plain fields.
  const cookie = await rememberCookie(context, game.id);
  expect(cookie).toMatchObject({ httpOnly: true, sameSite: "Lax", path: `/api/games/${game.id}/session` });
  expect(cookie.expires).toBeGreaterThan(Date.now() / 1000 + 29 * 86400); // about 30 days
  let storage = await dump(page);
  expect(storage.cookie).toBe(""); // not readable from script
  const entries = JSON.parse(storage.local["elimin8.games.v1"]);
  expect(entries).toHaveLength(1);
  expect(Object.keys(entries[0]).sort()).toEqual(["gameId", "invitationCode", "name", "savedAt"]);
  expect(entries[0]).toMatchObject({ gameId: game.id, name: game.name, invitationCode: game.invitation });
  // Neither the private code nor the remember secret is in any storage, the URL or the console.
  for (const secret of [alice.code, cookie.value]) {
    expect(JSON.stringify(storage)).not.toContain(secret);
    expect(messages.join("\n")).not.toContain(secret);
  }
  const before = (await tabAccess(page))[0];

  // Restart: the tab's sessionStorage is gone; the game is listed and opens with no code.
  ({ context, page } = await restart(browser, baseURL, context));
  const calls = track(page);
  await page.goto("/#/player");
  const link = page.getByRole("link", { name: new RegExp(game.name) });
  await expect(link).toContainText("remembered on this device");
  await link.click();
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
  // It was the cookie exchange, not a login or a new player.
  expect(calls.filter((c) => c.path === `${game.base}/session`)).toEqual([{ method: "POST", path: `${game.base}/session`, status: 200 }]);
  expect(calls.some((c) => c.path === "/api/access")).toBe(false);
  const after = (await tabAccess(page))[0];
  expect(after.token).not.toBe(before.token); // a fresh admission
  const me = await (await request.get(game.base, { headers: auth(after.token) })).json();
  expect(me.me.id).toBe(alice.id);
  const roster = await (await request.get(game.base, { headers: auth(game.owner.token) })).json();
  expect(roster.players).toHaveLength(2); // nobody was created or rejoined
  // The 30-day deadline did not move.
  expect((await rememberCookie(context, game.id)).expires).toBe(cookie.expires);
  storage = await dump(page);
  expect(JSON.stringify(storage)).not.toContain(alice.code);
  expect(JSON.stringify(storage)).not.toContain(cookie.value);
  await context.close();
});

test("two games are remembered side by side, each restores its own player, and sign-out is isolated", async ({ browser, request, baseURL }) => {
  const a = await createGame(request, ["Alice", "Zed"]);
  const b = await createGame(request, ["Bob", "Yan"]);
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  await signIn(page, a, a.players[0]);
  const tokenA = (await tabAccess(page))[0].token;
  await signIn(page, b, b.players[0]);
  const tokenB = (await tabAccess(page)).find((x) => x.game.id === b.id).token;
  expect(JSON.parse((await dump(page)).local["elimin8.games.v1"]).map((e) => e.gameId).sort()).toEqual([a.id, b.id].sort());

  // After a restart each game opens as its own player, and neither overwrote the other.
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${a.id}`);
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await page.goto(`/#/player/${b.id}`);
  await expect(page.getByText("Playing as Bob")).toBeVisible();

  // Sign out of game A: the server is asked to revoke only A's credential.
  const calls = track(page);
  await page.goto(`/#/player/${a.id}`);
  await page.getByRole("button", { name: "Sign out of this game" }).click();
  await expect(page.getByRole("heading", { name: /Player entrance|Your place is waiting/ })).toBeVisible();
  expect(calls.filter((c) => c.method === "DELETE")).toEqual([{ method: "DELETE", path: `${a.base}/session`, status: 204 }]);
  const remaining = JSON.parse((await dump(page)).local["elimin8.games.v1"]);
  expect(remaining.map((e) => e.gameId)).toEqual([b.id]);
  expect(await cookieNames(context)).toEqual([`elimin8_remember_${b.id}`]);
  // A's old admission was revoked on the server; B's still works.
  const oldA = await (await request.get(a.base, { headers: auth(tokenA) })).status();
  const stillB = await (await request.get(b.base, { headers: auth(tokenB) })).status();
  expect([oldA, stillB]).toEqual([401, 200]);

  // Back to A needs the code again, while B still restores after another restart.
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${a.id}`);
  await expect(page.getByRole("heading", { name: "Enter your code to return" })).toBeVisible();
  await page.goto(`/#/player/${b.id}`);
  await expect(page.getByText("Playing as Bob")).toBeVisible();
  await context.close();
});

test("a lost cookie shows code recovery; wrong and other-game codes fail; the right code returns the same player", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const other = await createGame(request, ["Cara"]);
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  await signIn(page, game, game.players[0]);

  // Restart without the cookie (cleared, expired or blocked).
  ({ context, page } = await restart(browser, baseURL, context, { dropCookies: true }));
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByRole("heading", { name: "Sign in again to continue" })).toBeVisible();
  await expect(page.getByText(game.name)).toBeVisible();
  await expect(page.getByText(game.invitation)).toBeVisible();
  await expect(page.getByLabel("Game invitation code")).toHaveCount(0); // already known
  const code = page.getByLabel("Your private player code");
  // A wrong code and a code from another game are both refused and the form stays.
  for (const wrong of ["WRONG123", other.players[0].code]) {
    await code.fill(wrong);
    await page.getByRole("button", { name: "Enter game" }).click();
    await expect(page.getByRole("alert")).toBeVisible();
    await expect(page.getByRole("heading", { name: "Sign in again to continue" })).toBeVisible();
  }
  expect(await cookieNames(context)).toEqual([]);
  // The right code returns the same player and sets the cookie again.
  await code.fill(game.players[0].code);
  await page.getByRole("button", { name: "Enter game" }).click();
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  expect(await cookieNames(context)).toEqual([`elimin8_remember_${game.id}`]);
  const me = await (await request.get(game.base, { headers: auth((await tabAccess(page))[0].token) })).json();
  expect(me.me.id).toBe(game.players[0].id);

  // And the next restart works without a code again.
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await context.close();
});

test("an organizer code reset revokes the remembered sign-in, here and after a restart; the new code recovers", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const alice = game.players[0];
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  await signIn(page, game, alice);
  const reset = await (await request.post(`${game.base}/players/${alice.id}/code`, { headers: auth(game.owner.token) })).json();
  expect(reset.code).not.toBe(alice.code);

  // The open tab: its admission is rejected, the cookie no longer works, so it asks for the code.
  await page.reload();
  await expect(page.getByRole("heading", { name: "Sign in again to continue" })).toBeVisible();
  // After a restart the cookie is still there but the exchange is refused: same recovery form.
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByRole("heading", { name: "Sign in again to continue" })).toBeVisible();
  const code = page.getByLabel("Your private player code");
  await code.fill(alice.code); // the old code is dead
  await page.getByRole("button", { name: "Enter game" }).click();
  await expect(page.getByRole("alert")).toBeVisible();
  await code.fill(reset.code);
  await page.getByRole("button", { name: "Enter game" }).click();
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await context.close();
});

test("the open game renews its admission before it expires, without rejoining or extending the 30 days", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  await signIn(page, game, game.players[0]);
  const cookie = await rememberCookie(context, game.id);
  const first = (await tabAccess(page))[0];
  const calls = track(page);
  // Make the tab believe its admission ends in 10 minutes (the server still honours it), then reload:
  // the renewal window has begun, so the page renews on its own.
  await page.evaluate(() => {
    const list = JSON.parse(sessionStorage.getItem("elimin8.access.v1"));
    list[0].expiresAt = new Date(Date.now() + 10 * 60e3).toISOString();
    sessionStorage.setItem("elimin8.access.v1", JSON.stringify(list));
  });
  await page.reload();
  await expect.poll(async () => (await tabAccess(page))[0].token, { timeout: 20000 }).not.toBe(first.token);
  const renewed = (await tabAccess(page))[0];
  expect(new Date(renewed.expiresAt).getTime()).toBeGreaterThan(Date.now() + 11 * 3600e3); // a full new admission
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible(); // MQTT reconnected with the new token
  expect(calls.filter((c) => c.path === `${game.base}/session`).map((c) => [c.method, c.status])).toEqual([["POST", 200]]);
  expect(calls.some((c) => c.path === "/api/access")).toBe(false); // no login, no rejoin
  expect((await rememberCookie(context, game.id)).expires).toBe(cookie.expires); // 30 days not extended
  const roster = await (await request.get(game.base, { headers: auth(game.owner.token) })).json();
  expect(roster.players).toHaveLength(2);
  await context.close();
});

test("a failed sign-out keeps the player signed in and says so; a retry signs out", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();
  await signIn(page, game, game.players[0]);
  // The server cannot be reached.
  await page.route(`**${game.base}/session`, (route) => route.abort());
  await page.getByRole("button", { name: "Sign out of this game" }).click();
  await expect(page.getByRole("alert").filter({ hasText: "Couldn’t sign out" })).toBeVisible();
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  expect(JSON.parse((await dump(page)).local["elimin8.games.v1"])).toHaveLength(1);
  expect(await rememberCookie(context, game.id)).toBeDefined();
  // Back online: it works.
  await page.unroute(`**${game.base}/session`);
  await page.getByRole("button", { name: "Sign out of this game" }).click();
  await expect(page.getByRole("heading", { name: /Your place is waiting/ })).toBeVisible();
  expect(JSON.parse((await dump(page)).local["elimin8.games.v1"])).toHaveLength(0);
  expect(await rememberCookie(context, game.id)).toBeUndefined();
  await context.close();
});

test("opening a saved game while the server is unreachable or busy offers to retry", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  await signIn(page, game, game.players[0]);
  ({ context, page } = await restart(browser, baseURL, context));
  await page.route(`**${game.base}/session`, (route) => route.abort());
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByRole("heading", { name: "Couldn’t open your game" })).toBeVisible();
  await expect(page.getByRole("alert")).toContainText("couldn’t reach the server");
  await page.unroute(`**${game.base}/session`);
  await page.route(`**${game.base}/session`, (route) => route.fulfill({ status: 429, contentType: "application/json", body: JSON.stringify({ error: "Too many attempts." }) }));
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByRole("alert")).toContainText("Too many attempts");
  await page.unroute(`**${game.base}/session`);
  await page.getByRole("button", { name: "Try again" }).click();
  await expect(page.getByText("Playing as Alice")).toBeVisible();
  await context.close();
});

test("eliminated and ended players still restore, and a removed game needs the code again", async ({ browser, request, baseURL }) => {
  test.setTimeout(90000);
  const game = await createGame(request, ["Alice", "Bob", "Cara"]);
  const [alice, bob, cara] = game.players;
  const login = async (p) => (await request.post("/api/access", { data: { invitationCode: game.invitation, code: p.code, role: "player" } })).json();
  const bobAccess = await login(bob);
  const caraAccess = await login(cara);
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  await signIn(page, game, alice);
  const aliceToken = (await tabAccess(page))[0].token;
  await request.post(`${game.base}/missions`, { headers: auth(game.owner.token), data: { texts: ["One", "Two", "Three"] } });
  expect((await request.post(`${game.base}/start`, { headers: auth(game.owner.token) })).status()).toBe(200);
  // Whoever hunts Alice eliminates her; she confirms.
  let hunter;
  for (const access of [bobAccess, caraAccess]) {
    const view = await (await request.get(game.base, { headers: auth(access.token) })).json();
    if (view.me.victim?.id === alice.id) hunter = access;
  }
  const claim = await (await request.post(`${game.base}/eliminations`, { headers: auth(hunter.token), data: { victimId: alice.id } })).json();
  expect((await request.post(`${game.base}/eliminations/${claim.id}/response`, { headers: auth(aliceToken), data: { accept: true } })).status()).toBe(200);

  // An eliminated player reopens the game after a restart.
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByRole("heading", { name: "You have been eliminated." })).toBeVisible();
  // And once the game has ended.
  expect((await request.post(`${game.base}/end`, { headers: auth(game.owner.token) })).status()).toBe(200);
  ({ context, page } = await restart(browser, baseURL, context));
  await page.goto(`/#/player/${game.id}`);
  await expect(page.getByText("Game ended", { exact: true })).toBeVisible();

  // Removing a game from this device (the recovery screen's link) leaves nothing behind.
  ({ context, page } = await restart(browser, baseURL, context, { dropCookies: true }));
  await page.goto(`/#/player/${game.id}`);
  await page.getByRole("button", { name: "Remove this game from this device" }).click();
  await expect(page.getByText(game.name)).toHaveCount(0);
  expect(JSON.parse((await dump(page)).local["elimin8.games.v1"])).toHaveLength(0);
  await context.close();
});

test("storage tampering and blocked storage are harmless", async ({ browser, request, baseURL }) => {
  const game = await createGame(request, ["Alice", "Bob"]);
  const errors = [];
  // Extra or malformed saved entries are ignored, and only the known fields are ever written back.
  let context = await browser.newContext({ baseURL });
  let page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.goto("/#/player");
  await page.evaluate(() =>
    localStorage.setItem(
      "elimin8.games.v1",
      JSON.stringify([{ gameId: "old", name: "Old game", invitationCode: "OLD12345", token: "SECRET-TOKEN", code: "SECRET-CODE", savedAt: 1 }, { nope: true }, "text", null]),
    ),
  );
  await page.reload();
  await expect(page.getByText("Old game")).toBeVisible();
  await signIn(page, game, game.players[0]);
  const saved = JSON.parse((await dump(page)).local["elimin8.games.v1"]);
  expect(saved.map((e) => e.gameId).sort()).toEqual([game.id, "old"].sort());
  expect(JSON.stringify(saved)).not.toContain("SECRET");
  await context.close();

  // With localStorage blocked, signing in still works; the game just isn't remembered.
  context = await browser.newContext({ baseURL });
  page = await context.newPage();
  page.on("pageerror", (e) => errors.push(e.message));
  await page.addInitScript(() => {
    const original = Storage.prototype.setItem;
    Storage.prototype.setItem = function (key, value) {
      if (this === window.localStorage) throw new DOMException("blocked", "QuotaExceededError");
      return original.call(this, key, value);
    };
  });
  await signIn(page, game, game.players[1]);
  expect((await dump(page)).local["elimin8.games.v1"]).toBeUndefined();
  await context.close();
  expect(errors).toEqual([]);
});
