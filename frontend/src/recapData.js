// Pure helpers for the recap UI (no React, so tests can import them).

// 75 -> "01:15", 3725 -> "1:02:05".
export function formatOffset(seconds) {
  const s = Math.max(0, Math.floor(Number(seconds) || 0));
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = (n) => String(n).padStart(2, "0");
  return h ? `${h}:${pad(m)}:${pad(s % 60)}` : `${pad(m)}:${pad(s % 60)}`;
}

// The server sends no winners list, so the hero reads them from the awards it
// does send: the last player standing wins a survival game, the top of the
// standings wins on time, and an organizer-ended game has no winner.
export function recapWinners(recap) {
  const { reason } = recap.summary;
  if (reason === "survival") {
    const w = recap.awards.longestSurvivor;
    return w ? [{ id: w.playerId, name: w.name }] : [];
  }
  if (reason === "timer") return (recap.awards.mostKills || []).map((s) => ({ id: s.id, name: s.name }));
  return [];
}

// Competition ranking: equal kills share a rank (1, 1, 3).
export function rankStandings(leaderboard) {
  return leaderboard.map((s, i) => {
    const first = leaderboard.findIndex((o) => o.kills === s.kills);
    const tied = leaderboard.filter((o) => o.kills === s.kills).length > 1;
    return { ...s, rank: first + 1, tied, key: `${s.kind}/${s.id}/${i}` };
  });
}
