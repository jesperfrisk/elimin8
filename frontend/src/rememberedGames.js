import { request } from "./api";

// Remembered player games.
//
// The sign-in itself is an HttpOnly cookie that JavaScript can never read. This
// module only keeps non-secret lookup metadata (game id, name, invitation code)
// in localStorage, so a restarted browser can list a game and ask the server to
// re-admit the same player through POST /api/games/{id}/session. Private codes,
// the remember secret and server hashes are never stored here, in the URL or in
// any message. Organizers are not remembered: their access stays per tab.

const KEY = "elimin8.games.v1";

// Admissions last at most 12 hours. Renew them shortly before they run out, and
// never in a tight loop: if a renewed admission is barely longer than the one it
// replaces, the 30-day remembered credential is about to end, so stop renewing and
// let the normal expiry path ask for the code.
export const RENEW_LEAD_MS = 15 * 60 * 1000;
export const MIN_RENEW_DELAY_MS = 5 * 1000;
export const MIN_EXTENSION_MS = 60 * 1000;
export const RETRY_MS = [5000, 15000, 30000, 60000];

function store() {
  try {
    return window.localStorage;
  } catch {
    return null; // blocked storage: nothing is remembered, everything else works
  }
}

const isEntry = (e) => e && typeof e.gameId === "string" && e.gameId !== "" && typeof e.name === "string";

/** Saved player games, newest first. Only the three known fields are ever read back. */
export function readRemembered() {
  try {
    const value = JSON.parse(store()?.getItem(KEY) || "[]");
    if (!Array.isArray(value)) return [];
    return value.filter(isEntry).map((e) => ({
      gameId: e.gameId,
      name: e.name,
      invitationCode: typeof e.invitationCode === "string" ? e.invitationCode : "",
      savedAt: Number(e.savedAt) || 0,
    }));
  } catch {
    return [];
  }
}

function write(entries) {
  try {
    store()?.setItem(KEY, JSON.stringify(entries));
  } catch {
    /* Storage can be full or blocked; the game just won't be listed after a restart. */
  }
}

/** Remember a player's game after a code login or recovery (organizers are never remembered). */
export function rememberGame(access) {
  if (access?.role !== "player" || !access.game?.id) return;
  const entry = {
    gameId: access.game.id,
    name: access.game.name || "",
    invitationCode: access.game.invitationCode || "",
    savedAt: Date.now(),
  };
  write([entry, ...readRemembered().filter((e) => e.gameId !== entry.gameId)]);
}

/** Drop one game's saved metadata and leave every other game untouched. */
export function forgetRemembered(gameId) {
  write(readRemembered().filter((e) => e.gameId !== gameId));
}

const path = (gameId) => `/api/games/${encodeURIComponent(gameId)}/session`;

/** Ask the server to re-admit this game's remembered player (the cookie is sent automatically). */
export const exchangeSession = (gameId, signal) => request(path(gameId), { method: "POST", signal });

/** Revoke this game's remembered credential on the server and clear its cookie (204). */
export const revokeSession = (gameId) => request(path(gameId), { method: "DELETE" });

/**
 * What a failed session call means:
 *  ended   (401) the remembered sign-in is gone: cookie lost, expired, revoked, code reset
 *  busy    (429) rate limited
 *  offline       the server could not be reached at all
 *  error         anything else
 */
export function sessionFailure(error) {
  if (error?.status === 401) return "ended";
  if (error?.status === 429) return "busy";
  if (!error?.status) return "offline";
  return "error";
}

/**
 * Milliseconds until the next renewal, or null when none should be scheduled.
 * previousExpiresAt (the admission just replaced) turns off renewal when the new
 * admission is not meaningfully longer, which is how the fixed 30-day deadline shows up.
 */
export function nextRenewalDelay(expiresAt, now = Date.now(), previousExpiresAt = null) {
  const expires = new Date(expiresAt).getTime();
  if (!Number.isFinite(expires)) return null;
  if (previousExpiresAt !== null) {
    const previous = new Date(previousExpiresAt).getTime();
    if (Number.isFinite(previous) && expires - previous < MIN_EXTENSION_MS) return null;
  }
  return Math.max(MIN_RENEW_DELAY_MS, expires - now - RENEW_LEAD_MS);
}
