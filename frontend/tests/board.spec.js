import { test, expect } from "@playwright/test";
import { fitGrid, initials, avatarHue, statusLabel, TILE_ASPECT } from "../src/boardLayout.js";

// ---- Pure layout helpers: no browser and no backend needed. ----

test("fitGrid picks the largest layout that fits, and scrolls only below a readable size", () => {
  const sizes = [
    [2, 1920, 1080],
    [12, 1366, 768],
    [24, 3840, 2160],
    [100, 3840, 2160],
    [100, 1366, 768],
    [1, 390, 700],
  ];
  for (const [count, width, height] of sizes) {
    const fit = fitGrid(count, width, height, { gap: 12 });
    expect(fit.columns * fit.rows).toBeGreaterThanOrEqual(count);
    expect(fit.tileHeight).toBe(Math.round(fit.tileWidth / TILE_ASPECT));
    const usedWidth = fit.columns * fit.tileWidth + (fit.columns - 1) * 12;
    expect(usedWidth).toBeLessThanOrEqual(width);
    if (!fit.scroll) {
      // Everything fits on screen with no scrolling.
      expect(fit.rows * fit.tileHeight + (fit.rows - 1) * 12).toBeLessThanOrEqual(height + 1);
    }
  }
  // 2..~100 players fit a TV without scrolling; a phone scrolls, tiles stay readable.
  expect(fitGrid(100, 3840, 2160).scroll).toBe(false);
  expect(fitGrid(24, 3840, 2160).scroll).toBe(false);
  const phone = fitGrid(100, 390, 700);
  expect(phone.scroll).toBe(true);
  expect(phone.tileWidth).toBeGreaterThanOrEqual(92);
  // Few players never produce absurdly large tiles.
  expect(fitGrid(2, 3840, 2160).tileWidth).toBeLessThanOrEqual(520);
  // More room never shrinks the tiles.
  expect(fitGrid(30, 2000, 1200).tileWidth).toBeGreaterThanOrEqual(fitGrid(30, 1000, 600).tileWidth);
  // Empty and unmeasured boards do not throw or divide by zero.
  expect(fitGrid(0, 1000, 600).rows).toBe(0);
  expect(fitGrid(5, 0, 0).scroll).toBe(false);
});

test("initials, avatar colour and status text", () => {
  expect(initials("Alexandra Holm")).toBe("AH");
  expect(initials("bob")).toBe("BO");
  expect(initials("Lars-Erik Andersson Öberg")).toBe("LÖ");
  expect(initials("  ")).toBe("?");
  expect(initials("😀 Smile")).toBe("😀S");
  // The colour is stable per ID and spread across the wheel.
  expect(avatarHue("p1")).toBe(avatarHue("p1"));
  const hues = new Set(Array.from({ length: 40 }, (_, i) => avatarHue(`player-${i}`)));
  expect(hues.size).toBeGreaterThan(25);
  for (const hue of hues) {
    expect(hue).toBeGreaterThanOrEqual(0);
    expect(hue).toBeLessThan(360);
  }
  // Status is always text as well as colour.
  expect(statusLabel("active", false)).toBe("Active");
  expect(statusLabel("active", true)).toBe("Survivor");
  expect(statusLabel("eliminated", false)).toBe("Eliminated");
  expect(statusLabel("withdrawn", true)).toBe("Withdrew");
});

// ---- The board in a browser, against the real backend and MQTT broker. ----

test("public board: lobby, live elimination and withdrawal, ended state, 404 and privacy", async ({ browser, request }) => {
  const owner = await (await request.post("/api/games", { data: { name: `Board ${Date.now()}` } })).json();
  const id = owner.game.id;
  const base = `/api/games/${id}`;
  const auth = a => ({ Authorization: `Bearer ${a.token}` });
  const roster = await (await request.post(`${base}/players`, { headers: auth(owner), data: { names: ["Alice", "Bob", "Charlie", "Dana"] } })).json();
  await request.post(`${base}/missions`, { headers: auth(owner), data: { texts: ["Shake hands", "Say pineapple", "Give a high-five", "Hum a tune"] } });
  const players = [];
  for (const p of roster.players) players.push(await (await request.post("/api/access", {
    data: { invitationCode: owner.game.invitationCode, code: p.code, role: "player" },
  })).json());

  // A fresh browser context: no stored access, no cookies, no login.
  const context = await browser.newContext();
  const page = await context.newPage();
  const errors = [];
  const requests = [];
  page.on("pageerror", e => errors.push(e.message));
  page.on("request", r => requests.push({ url: new URL(r.url()).pathname, auth: !!r.headers().authorization }));
  try {
    // Unknown game: a friendly screen, not a crash.
    await page.goto("/#/board/does-not-exist");
    await expect(page.getByRole("heading", { name: "Board not found" })).toBeVisible();

    // Lobby: waiting state, no players listed.
    await page.goto(`/#/board/${id}`);
    await expect(page.getByText("Waiting for the game to start")).toBeVisible();
    await expect(page.getByText("Live", { exact: true })).toBeVisible();
    await expect(page.getByRole("listitem")).toHaveCount(0);

    // Starting the game shows tiles with no reload: only MQTT can deliver this.
    expect((await request.post(`${base}/start`, { headers: auth(owner) })).status()).toBe(200);
    const tiles = page.getByRole("list", { name: "Players" }).getByRole("listitem");
    await expect(tiles).toHaveCount(4);
    await expect(page.getByTestId("board-active")).toContainText("4");
    for (const name of ["Alice", "Bob", "Charlie", "Dana"]) {
      await expect(page.getByRole("listitem", { name: new RegExp(`^${name}, Active$`) })).toBeVisible();
    }

    // An elimination updates the board live; a withdrawal looks and reads differently.
    const killer = players[0];
    const eliminate = async () => {
      const victimId = (await (await request.get(base, { headers: auth(killer) })).json()).me.victim.id;
      const victim = players.find(p => p.game.me.id === victimId);
      const claim = await (await request.post(`${base}/eliminations`, { headers: auth(killer), data: { victimId } })).json();
      expect((await request.post(`${base}/eliminations/${claim.id}/response`, { headers: auth(victim), data: { accept: true } })).status()).toBe(200);
      return victim;
    };
    const victim = await eliminate();
    const eliminated = page.locator('.tile[data-status="eliminated"]');
    await expect(eliminated).toHaveCount(1);
    await expect(eliminated).toContainText(victim.game.me.name);
    await expect(eliminated).toContainText("Eliminated");
    await expect(page.getByTestId("board-active")).toContainText("3");

    const leaverId = (await (await request.get(base, { headers: auth(killer) })).json()).me.victim.id;
    const leaver = players.find(p => p.game.me.id === leaverId);
    const withdrawal = await (await request.post(`${base}/withdrawals`, { headers: auth(leaver) })).json();
    expect((await request.post(`${base}/withdrawals/confirm`, { headers: auth(leaver), data: { withdrawalToken: withdrawal.withdrawalToken } })).status()).toBe(204);
    const withdrew = page.locator('.tile[data-status="withdrawn"]');
    await expect(withdrew).toHaveCount(1);
    await expect(withdrew).toContainText("Withdrew");
    await expect(withdrew).not.toContainText("Eliminated");
    await expect(page.locator('.tile[data-status="eliminated"]')).toHaveCount(1);

    // The killer eliminates the last opponent: the game ends with one survivor.
    await eliminate();
    await expect(page.locator('.tile[data-status="eliminated"]')).toHaveCount(2);
    await expect(page.getByRole("region", { name: "Game over" })).toBeVisible();
    // The public recap replaces the grid; the players stay one click away.
    await expect(page.getByTestId("recap")).toBeVisible();
    await page.getByRole("button", { name: "Show players" }).click();
    await expect(page.locator(".tile.survivor")).toHaveCount(1);
    await expect(page.locator(".tile.survivor")).toContainText(killer.game.me.name);

    // No horizontal overflow at phone, laptop and TV widths, and the whole game fits without scrolling.
    for (const size of [{ width: 390, height: 844 }, { width: 1366, height: 768 }, { width: 3840, height: 2160 }]) {
      await page.setViewportSize(size);
      await expect(tiles).toHaveCount(4);
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      expect(await page.evaluate(() => {
        const main = document.querySelector(".board-main");
        return main.scrollHeight <= main.clientHeight + 1;
      })).toBe(true);
    }

    // Privacy: the board only ever called the public endpoint, with no credentials,
    // and never touched a private game route or the player/organizer APIs.
    const boardCalls = requests.filter(r => r.url.startsWith("/api/"));
    expect(boardCalls.length).toBeGreaterThan(0);
    for (const call of boardCalls) {
      expect(call.url).toMatch(/^\/api\/(board\/[^/]+|mqtt)$/);
      expect(call.auth).toBe(false);
    }
    const stored = await page.evaluate(() => JSON.stringify({ ...sessionStorage }) + JSON.stringify({ ...localStorage }));
    expect(stored).not.toContain("token");
    expect(await page.content()).not.toMatch(/mission|Shake hands|Say pineapple|Hum a tune|High-five/i);
    expect(errors).toEqual([]);
  } finally {
    await context.close();
    const state = await (await request.get(base, { headers: auth(owner) })).json();
    if (state.status !== "ended") await request.post(`${base}/end`, { headers: auth(owner) });
  }
});
