// Pure timing and diff logic for the board's elimination moment (Board.jsx).
// No DOM and no React, so it is unit tested without a browser.

export const MOMENT_FULL_MS = 1800; // first three eliminations of a burst
export const MOMENT_HALF_MS = 900; // 4th to 7th
export const MOMENT_SNAP_MS = 350; // 8th onwards
export const MOMENT_MIN_MS = 60; // once the burst budget is spent
export const WITHDRAW_MS = 700; // quieter, shorter, no sound
export const FINALE_MS = 2200;
export const BURST_BUDGET_MS = 11000;
export const REDUCED_MS = { eliminated: 600, withdrawn: 400, finale: 800 };

/**
 * Players who left the game between two board snapshots: an active player who
 * is now eliminated or withdrawn. Only players present in both snapshots count,
 * so a first load, a new player or a roster change never produces a moment.
 * The order is the board's display order.
 */
export function diffNewlyOut(previous, next) {
  const before = new Map((previous || []).map((p) => [p.id, p.status]));
  const out = [];
  for (const p of next || []) {
    if (before.get(p.id) === "active" && (p.status === "eliminated" || p.status === "withdrawn")) {
      out.push({ id: p.id, kind: p.status });
    }
  }
  return out;
}

/**
 * How long the next moment plays. `run` describes the burst so far: how many
 * eliminations have played and how much planned time they used. Eliminations
 * arriving one at a time always get the full moment; in a burst they speed up
 * (full, then half, then a quick snap) so the board never lags far behind the
 * real game, yet every elimination is still shown, in order.
 */
export function momentDuration(kind, run, reduced = false) {
  if (reduced) return REDUCED_MS[kind] ?? REDUCED_MS.eliminated;
  if (kind === "withdrawn") return WITHDRAW_MS;
  if (kind === "finale") return FINALE_MS;
  const base = run.eliminations < 3 ? MOMENT_FULL_MS : run.eliminations < 7 ? MOMENT_HALF_MS : MOMENT_SNAP_MS;
  return run.elapsed + base > BURST_BUDGET_MS ? MOMENT_MIN_MS : base;
}

/** The burst state after a moment of `ms` for `kind` has been scheduled. */
export function advanceRun(run, kind, ms) {
  return {
    eliminations: run.eliminations + (kind === "eliminated" ? 1 : 0),
    elapsed: run.elapsed + ms,
  };
}

export const EMPTY_RUN = { eliminations: 0, elapsed: 0 };
