import React, { useEffect, useState } from "react";
import { request } from "./api";

const reasons = {
  survival: "The game ended with a survival win.",
  timer: "Time ran out. The most confirmed kills wins.",
};

// Game-over panel: shows the server-calculated final result. Kill counts are
// only ever sent here, after the game has ended; nothing is computed in the UI.
export function Results({ game, access }) {
  const [result, setResult] = useState(null);
  const [failed, setFailed] = useState(false);
  const ended = game.status === "ended";
  useEffect(() => {
    if (!ended) return undefined;
    const controller = new AbortController();
    setFailed(false);
    request(`/api/games/${game.id}/result`, {
      token: access.token,
      signal: controller.signal,
    })
      .then(setResult)
      .catch((e) => {
        if (controller.signal.aborted) return;
        // 404: the organizer ended the game, so there is no result to show.
        if (e.status !== 404) setFailed(true);
      });
    return () => controller.abort();
  }, [ended, game.id, access.token]);
  if (!ended) return null;
  if (failed)
    return (
      <section className="panel results" aria-label="Final results">
        <p role="status">The final results could not be loaded. Refresh the page to try again.</p>
      </section>
    );
  if (!result) return null;
  const kindLabel = (kind) => (kind === "team" ? "Team" : "Player");
  const standingsKind = result.standings[0]?.kind || "player";
  return (
    <section className="panel results" aria-label="Final results">
      <h2>Final results</h2>
      <p className="muted">{reasons[result.reason] || ""}</p>
      {result.tie && (
        <p role="status">
          <strong>It’s a tie.</strong> The top score is shared.
        </p>
      )}
      <h3>{result.winners.length > 1 ? "Winners" : "Winner"}</h3>
      {result.winners.length ? (
        <ul className="result-winners">
          {result.winners.map((w) => (
            <li key={`${w.kind}/${w.id}`}>
              <strong>{w.name}</strong> <small>· {kindLabel(w.kind)}</small>
            </li>
          ))}
        </ul>
      ) : (
        <p>No winner.</p>
      )}
      <h3>Final standings</h3>
      <div className="table-scroll">
        <table>
          <thead>
            <tr>
              <th>{kindLabel(standingsKind)}</th>
              <th>Confirmed kills</th>
            </tr>
          </thead>
          <tbody>
            {result.standings.map((s) => (
              <tr key={`${s.kind}/${s.id}`}>
                <td>{s.name}</td>
                <td>{s.kills}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </section>
  );
}
