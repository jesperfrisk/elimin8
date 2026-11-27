// Board sound: short tones synthesized with the Web Audio API (no audio file,
// no licensing). Kept isolated so a real recording can replace `synth` later
// without touching the board.
//
// Browsers block audio until the user interacts with the page, so the board is
// silent until someone taps "Tap to enable sound". The choice (enabled / muted)
// is remembered per device in localStorage, which can be missing or throw, and
// audio itself can be blocked or unsupported; none of that may ever raise an
// error. Every requested cue also fires a `board:sound` window event
// ({ kind, audible, ...meta }), which is how tests observe sound.

const KEY = "elimin8.board.sound.v1";

function safeStorage() {
  try {
    return window.localStorage;
  } catch {
    return null;
  }
}
function readPrefs(storage) {
  try {
    const value = JSON.parse(storage?.getItem(KEY) || "null");
    return { enabled: !!value?.enabled, muted: !!value?.muted };
  } catch {
    return { enabled: false, muted: false };
  }
}
function writePrefs(storage, prefs) {
  try {
    storage?.setItem(KEY, JSON.stringify(prefs));
  } catch {
    /* Storage can be blocked or full; the choice just won't be remembered. */
  }
}

// ---- The tones. Each takes a running AudioContext and schedules itself. ----

function tone(ctx, destination, { type, from, to, start, end, peak, attack = 0.01 }) {
  const osc = ctx.createOscillator();
  const gain = ctx.createGain();
  osc.type = type;
  osc.frequency.setValueAtTime(from, start);
  if (to && to !== from) osc.frequency.exponentialRampToValueAtTime(to, end);
  gain.gain.setValueAtTime(0.0001, start);
  gain.gain.exponentialRampToValueAtTime(peak, start + attack);
  gain.gain.exponentialRampToValueAtTime(0.0001, end);
  osc.connect(gain);
  gain.connect(destination);
  osc.start(start);
  osc.stop(end + 0.05);
}

export const synth = {
  // Low, ominous gong: a falling fundamental with inharmonic partials that
  // ring out at different rates, about 1.4 seconds.
  elimination(ctx) {
    const t = ctx.currentTime;
    const master = ctx.createGain();
    master.gain.setValueAtTime(0.9, t);
    master.connect(ctx.destination);
    const base = 92;
    [
      [1, 0.5, 1.4],
      [2, 0.28, 1.2],
      [2.76, 0.2, 1.0],
      [5.4, 0.11, 0.7],
      [8.93, 0.06, 0.45],
    ].forEach(([ratio, peak, length]) =>
      tone(ctx, master, { type: "sine", from: base * ratio * 1.05, to: base * ratio * 0.84, start: t, end: t + length, peak, attack: 0.012 }),
    );
  },
  // Short rising chord for the end of the game, about 0.9 seconds.
  winner(ctx) {
    const t = ctx.currentTime;
    const master = ctx.createGain();
    master.gain.setValueAtTime(0.9, t);
    master.connect(ctx.destination);
    [392.0, 493.88, 587.33, 783.99].forEach((hz, i) =>
      tone(ctx, master, { type: "triangle", from: hz, start: t + i * 0.09, end: t + i * 0.09 + 0.9, peak: 0.2, attack: 0.02 }),
    );
  },
  // A soft blip so whoever taps the gate can tell the sound works.
  enable(ctx) {
    const t = ctx.currentTime;
    tone(ctx, ctx.destination, { type: "sine", from: 660, to: 880, start: t, end: t + 0.14, peak: 0.12, attack: 0.01 });
  },
};

export function createBoardSound(options = {}) {
  const Ctx =
    options.AudioContextClass ??
    (typeof window !== "undefined" ? window.AudioContext || window.webkitAudioContext : undefined);
  const storage = "storage" in options ? options.storage : safeStorage();
  const prefs = readPrefs(storage);
  let ctx = null;
  let state = { supported: !!Ctx, enabled: prefs.enabled, muted: prefs.muted, unlocked: false };
  const listeners = new Set();

  function set(patch) {
    const next = { ...state, ...patch };
    if (Object.keys(next).every((key) => next[key] === state[key])) return;
    state = next;
    listeners.forEach((listener) => listener());
  }
  function sync() {
    set({ unlocked: ctx?.state === "running" });
  }
  function ensureContext() {
    if (ctx || !Ctx) return ctx;
    try {
      ctx = new Ctx();
      ctx.addEventListener?.("statechange", sync);
    } catch {
      ctx = null;
      set({ supported: false });
    }
    return ctx;
  }
  async function resume() {
    const context = ensureContext();
    if (!context) return false;
    try {
      if (context.state !== "running") await context.resume();
    } catch {
      /* Blocked until a user gesture: stay locked. */
    }
    sync();
    return state.unlocked;
  }

  const api = {
    getState: () => state,
    subscribe(listener) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    resume,
    /** Call from a click/tap handler: this is the user gesture browsers require. */
    async enable() {
      if (!Ctx) return false;
      if (!(await resume())) return false;
      set({ enabled: true, muted: false });
      writePrefs(storage, { enabled: true, muted: false });
      api.play("enable");
      return true;
    },
    toggleMute() {
      set({ muted: !state.muted });
      writePrefs(storage, { enabled: state.enabled, muted: state.muted });
    },
    /** Request a cue. Silent (but still reported) unless sound is unlocked and not muted. */
    play(kind, meta = {}) {
      const audible = state.unlocked && !state.muted && !!ctx && !!synth[kind];
      try {
        window.dispatchEvent(new CustomEvent("board:sound", { detail: { kind, audible, ...meta } }));
      } catch {
        /* Not in a browser. */
      }
      if (!audible) return false;
      try {
        synth[kind](ctx);
      } catch {
        return false;
      }
      return true;
    },
  };

  // A device that enabled sound before: try to resume now, and on the first
  // tap or key press anywhere (the gesture browsers insist on) after a reload.
  if (prefs.enabled && Ctx && typeof window !== "undefined") {
    ensureContext();
    resume();
    const events = ["pointerdown", "keydown", "touchstart"];
    const unlock = () => {
      resume().then((ok) => {
        if (ok) events.forEach((name) => window.removeEventListener(name, unlock, true));
      });
    };
    events.forEach((name) => window.addEventListener(name, unlock, true));
  }
  return api;
}

let shared;
/** The one board sound instance for the page. */
export function getBoardSound() {
  shared ??= createBoardSound();
  return shared;
}
