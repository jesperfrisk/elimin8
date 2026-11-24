import React, { useEffect, useState } from "react";
import { request } from "./api";

const groups = [
  { status: "active", title: "Active" },
  { status: "eliminated", title: "Eliminated" },
  { status: "withdrawn", title: "Withdrawn" },
];

// Public Kill Board: names and status only. The server response carries
// nothing else, so there is nothing for this panel to hide.
export function KillBoard({ game, access }) {
  const [entries, setEntries] = useState(null);
  const [failed, setFailed] = useState(false);
  // game.revision changes on every elimination or withdrawal and arrives over
  // MQTT, so refetching on it keeps the board live.
  useEffect(() => {
    const abort = new AbortController();
    request(`/api/games/${game.id}/killboard`, { token: access.token, signal: abort.signal })
      .then(data => { setEntries(data.entries); setFailed(false); })
      .catch(e => { if (e.name !== "AbortError") setFailed(true); });
    return () => abort.abort();
  }, [game.id, game.revision, access.token]);
  if (failed && !entries) return null;
  if (!entries || entries.length === 0) return null;
  return (
    <section className="panel kill-board" aria-label="Kill Board">
      <h2>Kill Board</h2>
      {groups.map(({ status, title }) => {
        const names = entries.filter(e => e.status === status);
        return (
          <div key={status} data-testid={`killboard-${status}`}>
            <h3>{title} ({names.length})</h3>
            {names.length > 0 && (
              <ul>
                {names.map(e => <li key={e.id}>{e.name}</li>)}
              </ul>
            )}
          </div>
        );
      })}
    </section>
  );
}
