import { test, expect } from "@playwright/test";
import {
  diffNewlyOut,
  momentDuration,
  advanceRun,
  EMPTY_RUN,
  MOMENT_FULL_MS,
  MOMENT_HALF_MS,
  MOMENT_SNAP_MS,
  MOMENT_MIN_MS,
  WITHDRAW_MS,
  FINALE_MS,
  BURST_BUDGET_MS,
} from "../src/boardMotion.js";

// ---- Pure timing and diff logic: no browser, no backend. ----

test("diffNewlyOut reports only active players who just left, in board order", () => {
  const before = [
    { id: "a", status: "active" },
    { id: "b", status: "active" },
    { id: "c", status: "eliminated" },
    { id: "d", status: "active" },
  ];
  const after = [
    { id: "a", status: "eliminated" },
    { id: "b", status: "active" },
    { id: "c", status: "eliminated" },
    { id: "d", status: "withdrawn" },
    { id: "e", status: "eliminated" }, // a player that was not in the snapshot never animates
  ];
  expect(diffNewlyOut(before, after)).toEqual([
    { id: "a", kind: "eliminated" },
    { id: "d", kind: "withdrawn" },
  ]);
  // First load, no change, and an empty roster produce nothing.
  expect(diffNewlyOut(null, after)).toEqual([]);
  expect(diffNewlyOut(after, after)).toEqual([]);
  expect(diffNewlyOut([], [])).toEqual([]);
});

test("momentDuration: full for the first three, then half, then a snap, within a budget", () => {
  const play = (kinds, reduced = false) => {
    let run = EMPTY_RUN;
    return kinds.map((kind) => {
      const ms = momentDuration(kind, run, reduced);
      run = advanceRun(run, kind, ms);
      return ms;
    });
  };
  const eliminations = (n) => Array.from({ length: n }, () => "eliminated");
  // One elimination at a time always gets the full moment.
  expect(play(eliminations(1))).toEqual([MOMENT_FULL_MS]);
  expect(play(eliminations(3))).toEqual([MOMENT_FULL_MS, MOMENT_FULL_MS, MOMENT_FULL_MS]);
  const burst = play(eliminations(9));
  expect(burst.slice(0, 3)).toEqual([MOMENT_FULL_MS, MOMENT_FULL_MS, MOMENT_FULL_MS]);
  expect(burst.slice(3, 7)).toEqual([MOMENT_HALF_MS, MOMENT_HALF_MS, MOMENT_HALF_MS, MOMENT_HALF_MS]);
  expect(burst.slice(7)).toEqual([MOMENT_SNAP_MS, MOMENT_SNAP_MS]);
  // A huge burst never runs away: once the budget is spent each one is a quick flash.
  const huge = play(eliminations(40));
  expect(huge.reduce((a, b) => a + b, 0)).toBeLessThan(BURST_BUDGET_MS + 3000);
  expect(huge.at(-1)).toBe(MOMENT_MIN_MS);
  // Never longer than the one before it, so a burst only ever speeds up.
  for (let i = 1; i < huge.length; i++) expect(huge[i]).toBeLessThanOrEqual(huge[i - 1]);
  // Withdrawals and the finale have their own lengths and do not use up the elimination steps.
  expect(play(["withdrawn", "eliminated", "finale"])).toEqual([WITHDRAW_MS, MOMENT_FULL_MS, FINALE_MS]);
  // Reduced motion: short fades.
  expect(play(["eliminated", "withdrawn", "finale"], true).every((ms) => ms <= 800)).toBe(true);
});

// A strict fake AudioContext: it rejects the calls a real one would throw on or ignore
// (exponential ramps to zero or negative, ramps in the past, stop before start, ...),
// so a mistake in the tone scheduling can't hide behind play()'s error handling.
function strictContext() {
  const ctx = { currentTime: 1, state: "running", destination: { kind: "destination" }, oscillators: [], gains: [] };
  const check = (cond, message) => {
    if (!cond) throw new Error(message);
  };
  const param = (name) => ({
    value: 0,
    setValueAtTime(v, t) {
      check(Number.isFinite(v) && Number.isFinite(t) && t >= ctx.currentTime, `${name}.setValueAtTime(${v}, ${t})`);
    },
    exponentialRampToValueAtTime(v, t) {
      check(v > 0 && Number.isFinite(v) && Number.isFinite(t) && t >= ctx.currentTime, `${name}.exponentialRampToValueAtTime(${v}, ${t})`);
    },
  });
  ctx.createGain = () => {
    const node = { gain: param("gain"), connect(target) { check(target, "gain.connect(undefined)"); } };
    ctx.gains.push(node);
    return node;
  };
  ctx.createOscillator = () => {
    const node = {
      type: "",
      frequency: param("frequency"),
      started: null,
      stopped: null,
      connect(target) {
        check(target, "oscillator.connect(undefined)");
      },
      start(t) {
        check(node.started === null && t >= ctx.currentTime, `oscillator.start(${t})`);
        node.started = t;
      },
      stop(t) {
        check(node.started !== null && t > node.started, `oscillator.stop(${t}) before start`);
        node.stopped = t;
      },
    };
    ctx.oscillators.push(node);
    return node;
  };
  return ctx;
}

test("the synthesized tones schedule valid Web Audio automation", async () => {
  const { synth } = await import("../src/boardSound.js");
  const gong = strictContext();
  synth.elimination(gong);
  expect(gong.oscillators).toHaveLength(5);
  const winner = strictContext();
  synth.winner(winner);
  expect(winner.oscillators).toHaveLength(4);
  const blip = strictContext();
  synth.enable(blip);
  expect(blip.oscillators).toHaveLength(1);
  for (const ctx of [gong, winner, blip]) {
    for (const osc of ctx.oscillators) {
      expect(osc.started).not.toBeNull();
      expect(osc.stopped).toBeGreaterThan(osc.started);
      expect(osc.stopped - ctx.currentTime).toBeLessThan(2); // short: nothing rings for long
    }
  }
});

// ---- The board in a browser with the API, the MQTT broker and audio stubbed. ----

const players = (n) => Array.from({ length: n }, (_, i) => ({ id: `p${i}`, number: i + 1, name: `Player ${i}`, status: "active" }));
const withStatus = (list, changes) => list.map((p) => (changes[p.id] ? { ...p, status: changes[p.id] } : p));
const payload = (list, over = {}) => ({
  gameId: "g1",
  name: "Mock game",
  status: "active",
  activeCount: list.filter((p) => p.status === "active").length,
  timer: null,
  players: list,
  ...over,
});

// Runs before the page's own scripts: records sound cues and any moment class that
// ever appears, and installs a fake AudioContext (or a failing one).
function installProbes({ audio = "ok", storage = "ok" }) {
  window.__sounds = [];
  window.__moments = [];
  // Browsers only let audio start after a user gesture; model that.
  window.__gesture = false;
  for (const name of ["pointerdown", "keydown", "touchstart"]) addEventListener(name, () => (window.__gesture = true), true);
  window.addEventListener("board:sound", (e) => window.__sounds.push({ ...e.detail, t: performance.now() }));
  const note = (el) => {
    const classes = typeof el.className === "string" ? el.className : "";
    for (const c of classes.split(/\s+/)) if (c.startsWith("moment-")) window.__moments.push(`${c}:${el.getAttribute("data-status")}`);
  };
  new MutationObserver((records) => {
    for (const r of records) {
      if (r.type === "attributes") note(r.target);
      for (const node of r.addedNodes || []) {
        if (node.nodeType !== 1) continue;
        note(node);
        node.querySelectorAll?.('[class*="moment-"]').forEach(note);
      }
    }
  }).observe(document, { subtree: true, childList: true, attributes: true, attributeFilter: ["class"] });
  if (storage === "blocked") {
    Storage.prototype.getItem = () => {
      throw new Error("storage blocked");
    };
    Storage.prototype.setItem = () => {
      throw new Error("storage blocked");
    };
  }
  if (audio === "throws") {
    window.AudioContext = class {
      constructor() {
        throw new Error("audio blocked");
      }
    };
  } else {
    window.AudioContext = class {
      constructor() {
        this.state = "suspended";
        this.currentTime = 0;
        this.destination = {};
        this.listeners = [];
        window.__audioContexts = (window.__audioContexts || 0) + 1;
      }
      addEventListener(name, fn) {
        if (name === "statechange") this.listeners.push(fn);
      }
      async resume() {
        if (audio === "rejects") throw new Error("not allowed to start");
        if (!window.__gesture) return; // stays suspended until the user taps
        this.state = "running";
        this.listeners.forEach((fn) => fn());
      }
      createGain() {
        return { gain: { setValueAtTime() {}, exponentialRampToValueAtTime() {} }, connect() {} };
      }
      createOscillator() {
        return {
          type: "",
          frequency: { setValueAtTime() {}, exponentialRampToValueAtTime() {} },
          connect() {},
          start() {
            window.__oscillators = (window.__oscillators || 0) + 1;
          },
          stop() {},
        };
      }
    };
  }
}

async function openBoard(page, initial, probes = {}) {
  const board = { data: initial, sockets: [] };
  await page.addInitScript(installProbes, probes);
  await page.route("**/api/board/**", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(board.data) }));
  // A minimal MQTT 3.1.1 broker so the page's real client connects and subscribes.
  await page.routeWebSocket("**/api/mqtt", (ws) => {
    board.sockets.push(ws);
    ws.onMessage((message) => {
      const b = Buffer.from(message);
      const type = b[0] >> 4;
      if (type === 1) ws.send(Buffer.from([0x20, 0x02, 0x00, 0x00]));
      else if (type === 8) ws.send(Buffer.from([0x90, 0x03, b[2], b[3], 0x01]));
      else if (type === 12) ws.send(Buffer.from([0xd0, 0x00]));
    });
  });
  // The board changed: store it, then announce it on the board topic like the backend does.
  board.update = (data) => {
    board.data = data;
    const topic = Buffer.from("boards/g1/updates");
    const body = Buffer.from(JSON.stringify({ gameId: "g1", type: "board.updated" }));
    const packet = Buffer.concat([Buffer.from([0x30, 2 + topic.length + body.length, topic.length >> 8, topic.length & 255]), topic, body]);
    for (const ws of board.sockets) ws.send(packet);
  };
  // The board changed but no message arrives (as if it happened while offline).
  board.silently = (data) => {
    board.data = data;
  };
  board.drop = () => {
    for (const ws of board.sockets.splice(0)) ws.close();
  };
  board.cues = (kind) => page.evaluate((k) => window.__sounds.filter((s) => !k || s.kind === k), kind);
  board.moments = () => page.evaluate(() => window.__moments);
  await page.goto("/#/board/g1");
  await expect(page.getByText("Live", { exact: true })).toBeVisible();
  return board;
}

const tile = (page, id) => page.locator(`.tile[aria-label^="Player ${id.slice(1)},"]`);

test("only live changes get a moment: one cue per elimination, nothing on first load", async ({ page }) => {
  const start = withStatus(players(4), { p1: "eliminated" });
  const board = await openBoard(page, payload(start));
  await expect(page.locator(".tile")).toHaveCount(4);
  // First load: the already-eliminated tile is simply shown, silently, and the sound gate waits.
  await expect(tile(page, "p1")).toHaveAttribute("data-status", "eliminated");
  expect(await board.moments()).toEqual([]);
  expect(await board.cues()).toEqual([]);
  await expect(page.getByRole("button", { name: "Tap to enable sound" })).toBeVisible();
  await expect(page.getByTestId("board-active")).toHaveText(/3\s*\/\s*4/);

  // A live elimination: the moment plays on that tile, the count ticks down, one cue (silent: not enabled).
  board.update(payload(withStatus(start, { p2: "eliminated" })));
  await expect(tile(page, "p2")).toHaveClass(/moment-eliminated/);
  await expect(page.getByTestId("board-active")).toHaveText(/2\s*\/\s*4/);
  await expect(page.getByTestId("board-active")).toHaveClass(/tick/);
  const cues = await board.cues("elimination");
  expect(cues).toHaveLength(1);
  expect(cues[0]).toMatchObject({ id: "p2", audible: false });
  // It settles into the normal eliminated state and the class is gone.
  await expect(tile(page, "p2")).not.toHaveClass(/moment-/, { timeout: 5000 });
  await expect(tile(page, "p2")).toHaveAttribute("data-status", "eliminated");
  expect(await board.cues("elimination")).toHaveLength(1);
  expect(await page.evaluate(() => window.__oscillators || 0)).toBe(0); // blocked until enabled
});

test("sound gate, remembered choice, audible cues and the mute toggle", async ({ page }) => {
  const start = players(5);
  const board = await openBoard(page, payload(start));
  await page.getByRole("button", { name: "Tap to enable sound" }).click();
  await expect(page.getByRole("button", { name: "Tap to enable sound" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Sound on" })).toBeVisible();
  expect((await board.cues("enable"))[0]).toMatchObject({ audible: true });
  expect(await page.evaluate(() => localStorage.getItem("elimin8.board.sound.v1"))).toContain('"enabled":true');

  board.update(payload(withStatus(start, { p0: "eliminated" })));
  await expect(tile(page, "p0")).toHaveClass(/moment-eliminated/);
  expect((await board.cues("elimination"))[0]).toMatchObject({ id: "p0", audible: true });
  expect(await page.evaluate(() => window.__oscillators)).toBeGreaterThan(0); // the gong was synthesized
  await expect(tile(page, "p0")).not.toHaveClass(/moment-/, { timeout: 5000 });

  // Muted: the cue is still reported but silent, and the choice is remembered.
  await page.getByRole("button", { name: "Sound on" }).click();
  await expect(page.getByRole("button", { name: "Sound off" })).toBeVisible();
  const before = await page.evaluate(() => window.__oscillators);
  board.update(payload(withStatus(start, { p0: "eliminated", p1: "eliminated" })));
  await expect(tile(page, "p1")).toHaveClass(/moment-eliminated/);
  expect((await board.cues("elimination")).at(-1)).toMatchObject({ id: "p1", audible: false });
  expect(await page.evaluate(() => window.__oscillators)).toBe(before);
  expect(await page.evaluate(() => localStorage.getItem("elimin8.board.sound.v1"))).toContain('"muted":true');

  // A reload on the same device: the remembered choice is used (the first tap re-arms audio).
  await page.reload();
  await expect(page.getByText("Live", { exact: true })).toBeVisible();
  await expect(page.getByRole("button", { name: "Tap to enable sound" })).toBeVisible();
  await page.locator("body").click();
  await expect(page.getByRole("button", { name: "Sound off" })).toBeVisible();
});

test("a burst plays every elimination in order, speeding up after the first three", async ({ page }) => {
  test.setTimeout(60000);
  const start = players(8);
  const board = await openBoard(page, payload(start));
  // Five eliminations arrive in one update.
  board.update(payload(withStatus(start, { p1: "eliminated", p2: "eliminated", p3: "eliminated", p4: "eliminated", p5: "eliminated" })));
  // The first is in its moment; the others still look active until their turn.
  await expect(tile(page, "p1")).toHaveClass(/moment-eliminated/);
  await expect(tile(page, "p2")).toHaveAttribute("data-status", "active");
  await expect(tile(page, "p5")).toHaveAttribute("data-status", "active");
  await expect(page.getByTestId("board-active")).toHaveText(/7\s*\/\s*8/);
  await expect.poll(async () => (await board.cues("elimination")).length, { timeout: 20000 }).toBe(5);
  const cues = await board.cues("elimination");
  expect(cues.map((c) => c.id)).toEqual(["p1", "p2", "p3", "p4", "p5"]);
  const gap = (i) => cues[i + 1].t - cues[i].t;
  expect(gap(0)).toBeGreaterThan(1500); // full moments for the first three
  expect(gap(1)).toBeGreaterThan(1500);
  expect(gap(2)).toBeGreaterThan(1500);
  expect(gap(3)).toBeGreaterThan(600); // then half length
  expect(gap(3)).toBeLessThan(1400);
  await expect(page.getByTestId("board-active")).toHaveText(/3\s*\/\s*8/);
  await expect(page.locator('.tile[class*="moment-"]')).toHaveCount(0, { timeout: 5000 });
  expect(await board.cues("elimination")).toHaveLength(5); // exactly once each
});

test("a withdrawal is quieter and silent, and is distinct from an elimination", async ({ page }) => {
  const start = players(4);
  const board = await openBoard(page, payload(start));
  board.update(payload(withStatus(start, { p2: "withdrawn" })));
  await expect(tile(page, "p2")).toHaveClass(/moment-withdrawn/);
  await expect(tile(page, "p2")).toContainText("Withdrew");
  await expect(tile(page, "p2")).not.toHaveClass(/moment-eliminated/);
  await expect(tile(page, "p2")).not.toHaveClass(/moment-/, { timeout: 4000 });
  expect(await board.cues("elimination")).toHaveLength(0);
  expect(await board.cues("winner")).toHaveLength(0);
  expect(await board.moments()).toEqual(["moment-withdrawn:withdrawn"]);
});

test("no moment or sound on reconnect catch-up, tab wake-up, or an already-ended game", async ({ page }) => {
  test.setTimeout(60000);
  const start = players(5);
  const board = await openBoard(page, payload(start));
  // A change that happened while the connection was down: the board shows it silently on reconnect.
  board.silently(payload(withStatus(start, { p2: "eliminated" })));
  board.drop();
  await expect(tile(page, "p2")).toHaveAttribute("data-status", "eliminated", { timeout: 20000 });
  await expect(page.getByText("Live", { exact: true })).toBeVisible();
  // Waking a sleeping tab catches up silently too.
  board.silently(payload(withStatus(start, { p2: "eliminated", p3: "eliminated" })));
  await page.evaluate(() => {
    Object.defineProperty(document, "visibilityState", { configurable: true, get: () => "visible" });
    document.dispatchEvent(new Event("visibilitychange"));
  });
  await expect(tile(page, "p3")).toHaveAttribute("data-status", "eliminated");
  expect(await board.moments()).toEqual([]);
  expect(await board.cues()).toEqual([]);

  // A game that ended before the board opened is shown ended at once, with no flourish.
  const ended = payload(withStatus(start, { p1: "eliminated", p2: "eliminated", p3: "eliminated", p4: "eliminated" }), { status: "ended" });
  const second = await page.context().newPage();
  await second.addInitScript(installProbes, {});
  await second.route("**/api/board/**", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(ended) }));
  await second.routeWebSocket("**/api/mqtt", () => {});
  await second.goto("/#/board/g1");
  await expect(second.getByRole("region", { name: "Game over" })).toBeVisible();
  expect(await second.evaluate(() => window.__moments)).toEqual([]);
  expect(await second.evaluate(() => window.__sounds)).toEqual([]);
});

test("game end: eliminations first, then the winner flourish and chord, then the banner", async ({ page }) => {
  test.setTimeout(60000);
  const start = players(3);
  const board = await openBoard(page, payload(start));
  await page.getByRole("button", { name: "Tap to enable sound" }).click();
  board.update(payload(withStatus(start, { p1: "eliminated", p2: "eliminated" }), { status: "ended" }));
  await expect(tile(page, "p1")).toHaveClass(/moment-eliminated/);
  // The game is over on the server, but the board keeps playing the moments first.
  await expect(page.getByRole("region", { name: "Game over" })).toHaveCount(0);
  await expect(tile(page, "p0")).toHaveClass(/moment-finale/, { timeout: 15000 });
  await expect(page.getByRole("region", { name: "Game over" })).toBeVisible();
  const winner = await board.cues("winner");
  expect(winner).toHaveLength(1);
  expect(winner[0].audible).toBe(true);
  expect((await board.cues("elimination")).map((c) => c.id)).toEqual(["p1", "p2"]);
  await expect(tile(page, "p0")).toContainText("Survivor");
  await expect(tile(page, "p0")).not.toHaveClass(/moment-/, { timeout: 5000 });
  expect(await board.cues("winner")).toHaveLength(1);
});

test("blocked audio and blocked storage never raise an error", async ({ page }) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const start = players(4);
  const board = await openBoard(page, payload(start), { audio: "rejects", storage: "blocked" });
  // Audio that refuses to start: the gate stays, the board still works.
  await page.getByRole("button", { name: "Tap to enable sound" }).click();
  await expect(page.getByRole("button", { name: "Tap to enable sound" })).toBeVisible();
  board.update(payload(withStatus(start, { p1: "eliminated" })));
  await expect(tile(page, "p1")).toHaveClass(/moment-eliminated/);
  expect((await board.cues("elimination"))[0]).toMatchObject({ audible: false });
  await expect(tile(page, "p1")).not.toHaveClass(/moment-/, { timeout: 5000 });
  expect(errors).toEqual([]);
});

test("a browser with no usable audio hides the gate and keeps the visuals", async ({ page }) => {
  const errors = [];
  page.on("pageerror", (e) => errors.push(e.message));
  const start = players(4);
  const board = await openBoard(page, payload(start), { audio: "throws" });
  await page.getByRole("button", { name: "Tap to enable sound" }).click();
  await expect(page.getByRole("button", { name: "Tap to enable sound" })).toHaveCount(0);
  board.update(payload(withStatus(start, { p3: "eliminated" })));
  await expect(tile(page, "p3")).toHaveClass(/moment-eliminated/);
  await expect(tile(page, "p3")).not.toHaveClass(/moment-/, { timeout: 5000 });
  expect(errors).toEqual([]);
});

test("reduced motion swaps the shake and sweep for a short fade, and keeps the sound toggle", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  const start = players(4);
  const board = await openBoard(page, payload(start));
  await page.getByRole("button", { name: "Tap to enable sound" }).click();
  await expect(page.getByRole("button", { name: "Sound on" })).toBeVisible();
  board.update(payload(withStatus(start, { p1: "eliminated" })));
  await expect(tile(page, "p1")).toHaveClass(/moment-eliminated/);
  const style = await tile(page, "p1").evaluate((el) => ({ name: getComputedStyle(el).animationName, moment: el.style.getPropertyValue("--moment") }));
  expect(style.name).toContain("moment-fade");
  expect(style.name).not.toContain("moment-shake");
  expect(Number.parseInt(style.moment, 10)).toBeLessThanOrEqual(800);
  await expect(tile(page, "p1")).not.toHaveClass(/moment-/, { timeout: 3000 });
  await expect(page.getByRole("button", { name: "Sound on" })).toBeVisible();
});
