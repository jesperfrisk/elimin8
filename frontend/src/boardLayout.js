// Pure helpers for the live board (Board.jsx). No DOM, so they can be unit
// tested from Playwright specs without a browser or a backend.

// Poster tiles are portrait: width / height.
export const TILE_ASPECT = 3 / 4;

/**
 * Pick the grid that makes tiles as large as possible while every tile still
 * fits in width x height, so a TV shows the whole game without scrolling.
 * Below minWidth tiles stop shrinking and the grid scrolls vertically instead.
 *
 * Returns { columns, rows, tileWidth, tileHeight, scroll }.
 */
export function fitGrid(count, width, height, options = {}) {
  const { gap = 12, aspect = TILE_ASPECT, minWidth = 92, maxWidth = 520 } = options;
  if (!(count > 0) || !(width > 0) || !(height > 0)) {
    return { columns: 1, rows: 0, tileWidth: minWidth, tileHeight: Math.round(minWidth / aspect), scroll: false };
  }
  let best = null;
  for (let columns = 1; columns <= count; columns++) {
    const rows = Math.ceil(count / columns);
    const byWidth = (width - gap * (columns - 1)) / columns;
    const byHeight = ((height - gap * (rows - 1)) / rows) * aspect;
    const tileWidth = Math.floor(Math.min(byWidth, byHeight));
    if (!best || tileWidth > best.tileWidth) best = { columns, rows, tileWidth };
  }
  if (best.tileWidth >= minWidth) {
    const tileWidth = Math.min(best.tileWidth, maxWidth);
    return { ...best, tileWidth, tileHeight: Math.round(tileWidth / aspect), scroll: false };
  }
  // Too many players to fit at a readable size: fill the width, scroll down.
  const columns = Math.max(1, Math.floor((width + gap) / (minWidth + gap)));
  const tileWidth = Math.floor((width - gap * (columns - 1)) / columns);
  return { columns, rows: Math.ceil(count / columns), tileWidth, tileHeight: Math.round(tileWidth / aspect), scroll: true };
}

/** Up to two initials, using code points so emoji and accents are not split. */
export function initials(name) {
  const words = String(name || "").trim().split(/\s+/).filter(Boolean);
  if (!words.length) return "?";
  const first = (word) => Array.from(word)[0] || "";
  const letters = words.length === 1 ? Array.from(words[0]).slice(0, 2) : [first(words[0]), first(words[words.length - 1])];
  return letters.join("").toUpperCase();
}

/** A stable hue (0-359) derived from the player ID, so a colour never changes. */
export function avatarHue(id) {
  let hash = 2166136261;
  for (const char of String(id)) {
    hash ^= char.codePointAt(0);
    hash = Math.imul(hash, 16777619) >>> 0;
  }
  return hash % 360;
}

/** Visible status text. Never rely on colour alone. */
export function statusLabel(status, ended) {
  if (status === "eliminated") return "Eliminated";
  if (status === "withdrawn") return "Withdrew";
  return ended ? "Survivor" : "Active";
}
