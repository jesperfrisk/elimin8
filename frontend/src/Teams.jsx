import React, { useEffect, useState } from "react";
import { request } from "./api";

// Reason the game cannot start in team mode, or "" when setup is valid.
// Uses the saved teams from the organizer view, matching the server rules.
export function teamStartBlocker(game) {
  if (game.mode !== "team") return "";
  const joined = (game.players || []).filter(p => p.joinedAt);
  const teams = game.teams || [];
  const assigned = new Set(teams.flatMap(t => t.playerIds || []));
  const unassigned = joined.filter(p => !assigned.has(p.id));
  if (unassigned.length) {
    return `Assign every joined player to a team (${unassigned.length} unassigned: ${unassigned.map(p => p.name).join(", ")}).`;
  }
  const populated = teams.filter(t => joined.some(p => (t.playerIds || []).includes(p.id)));
  if (populated.length < 2) return "Team games need at least two teams with players.";
  return "";
}

function OrganizerTeams({ game, access, action, busy }) {
  const players = game.players || [];
  const joined = players.filter(p => p.joinedAt);
  const [draft, setDraft] = useState([]);
  const [dirty, setDirty] = useState(false);
  const [teamCount, setTeamCount] = useState("2");
  const [error, setError] = useState("");
  const base = `/api/games/${game.id}`;

  useEffect(() => {
    if (!dirty) setDraft((game.teams || []).map(t => ({ ...t, playerIds: [...(t.playerIds || [])] })));
  }, [game.revision, game.teams, dirty]);

  const call = (path, options) => action(async () => {
    setError("");
    try {
      await request(`${base}${path}`, { ...options, token: access.token });
      setDirty(false);
    } catch (e) {
      setError(e.message);
    }
  });
  const edit = fn => { setDraft(fn); setDirty(true); };
  const teamOf = id => draft.findIndex(t => t.playerIds.includes(id));
  const assign = (playerId, index) => edit(d => d.map((t, i) => ({
    ...t,
    playerIds: i === Number(index)
      ? [...t.playerIds.filter(x => x !== playerId), playerId]
      : t.playerIds.filter(x => x !== playerId),
  })));
  const unassign = playerId => edit(d => d.map(t => ({ ...t, playerIds: t.playerIds.filter(x => x !== playerId) })));
  const count = Number(teamCount);
  const countValid = Number.isInteger(count) && count >= 2 && count <= joined.length;
  const blocker = teamStartBlocker(game);

  return (
    <section className="panel teams" aria-label="Teams">
      <h2>Teams</h2>
      <div className="actions">
        <button className={game.mode === "individual" ? "" : "secondary"} disabled={busy}
          aria-pressed={game.mode === "individual"}
          onClick={() => call("/settings", { method: "PATCH", body: { mode: "individual" } })}>Individual</button>
        <button className={game.mode === "team" ? "" : "secondary"} disabled={busy}
          aria-pressed={game.mode === "team"}
          onClick={() => call("/settings", { method: "PATCH", body: { mode: "team" } })}>Teams</button>
      </div>
      {game.mode !== "team" ? (
        <p className="muted">Everyone plays on their own. Switch to Teams to group players.</p>
      ) : (
        <>
          <div className="actions">
            <label>
              Number of teams
              <input type="number" min="2" max={Math.max(2, joined.length)} value={teamCount}
                onChange={e => setTeamCount(e.target.value)} />
            </label>
            <button disabled={busy || !countValid}
              onClick={() => call("/teams/randomize", { method: "POST", body: { teamCount: count } })}>Randomize teams</button>
            <button className="secondary" disabled={busy}
              onClick={() => edit(d => [...d, { name: `Team ${d.length + 1}`, playerIds: [] }])}>Add team</button>
          </div>
          {draft.map((t, i) => (
            <fieldset key={t.id || `new-${i}`}>
              <legend>{t.name || "Unnamed team"}</legend>
              <label>
                Team name
                <input value={t.name} maxLength={60}
                  onChange={e => edit(d => d.map((x, j) => (j === i ? { ...x, name: e.target.value } : x)))} />
              </label>
              <button type="button" className="danger secondary compact" disabled={busy}
                onClick={() => edit(d => d.filter((_, j) => j !== i))}>Delete team</button>
              <ul>
                {t.playerIds.map(id => (
                  <li key={id}>
                    {players.find(p => p.id === id)?.name || "Unknown"}{" "}
                    <button type="button" className="secondary compact" onClick={() => unassign(id)}>Remove</button>
                  </li>
                ))}
              </ul>
            </fieldset>
          ))}
          <h3>Players</h3>
          <ul>
            {joined.map(p => (
              <li key={p.id}>
                <label>
                  {p.name}
                  <select value={teamOf(p.id)} onChange={e => (e.target.value === "-1" ? unassign(p.id) : assign(p.id, e.target.value))}>
                    <option value="-1">Unassigned</option>
                    {draft.map((t, i) => <option key={t.id || `new-${i}`} value={i}>{t.name || "Unnamed team"}</option>)}
                  </select>
                </label>
              </li>
            ))}
          </ul>
          <div className="actions">
            <button disabled={busy || !dirty}
              onClick={() => call("/teams", { method: "PUT", body: { teams: draft.map(({ id, name, playerIds }) => ({ ...(id ? { id } : {}), name, playerIds })) } })}>Save teams</button>
            {dirty && <button className="secondary" disabled={busy} onClick={() => setDirty(false)}>Discard changes</button>}
          </div>
          {dirty && <p className="muted" role="status">Unsaved changes.</p>}
          {!dirty && blocker && <p className="muted" role="status" data-testid="team-blocker">{blocker}</p>}
        </>
      )}
      {error && <p role="alert" className="error">{error}</p>}
    </section>
  );
}

function PlayerTeam({ game }) {
  const team = game.me?.team;
  if (!team) return null;
  const mates = (team.members || []).filter(m => m.id !== game.me.id);
  return (
    <section className="panel" aria-label="Your team">
      <h2>Your team: {team.name}</h2>
      {mates.length ? (
        <ul>{mates.map(m => <li key={m.id}>{m.name}</li>)}</ul>
      ) : <p className="muted">You have no teammates.</p>}
    </section>
  );
}

export function Teams(props) {
  const { game, access } = props;
  if (game.status === "ended") return null;
  if (access.role === "organizer") return game.status === "lobby" ? <OrganizerTeams {...props} /> : null;
  return <PlayerTeam game={game} />;
}
