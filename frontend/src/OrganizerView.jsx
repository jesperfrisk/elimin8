import React, { useEffect, useRef, useState } from "react";
import { request } from "./api";
import { LobbyMissions } from "./LobbyMissions";
import { gameActionError } from "./gameActionError";
import { CopyButton } from "./common";
import { teamStartBlocker } from "./Teams";

export function OrganizerView({ game, access, run, busy, action, refresh }) {
  const [issued, setIssued] = useState([]);
  const [transitionError, setTransitionError] = useState(null);
  const transitionErrorRef = useRef(null);
  useEffect(() => {
    if (transitionError) {
      transitionErrorRef.current?.focus({ preventScroll: true });
      transitionErrorRef.current?.scrollIntoView({ block: "nearest" });
    }
  }, [transitionError]);
  const players = game.players || [];
  const lobby = game.status === "lobby";
  function transition(name) {
    const message =
      name === "start"
        ? `Start with ${game.joinedCount} joined player(s)? ${game.invitedCount - game.joinedCount} invited player(s) have not joined and will be excluded.`
        : "End this game? This cannot be undone.";
    if (!window.confirm(message)) return;
    run(async () => {
      setTransitionError(null);
      try {
        await request(`/api/games/${game.id}/${name}`, {
          method: "POST",
          token: access.token,
        });
      } catch (e) {
        setTransitionError({ message: gameActionError(e, name), reenter: [401, 403, 404].includes(e.status) });
        return;
      }
      try {
        await refresh();
      } catch {
        setTransitionError({ message: `The game ${name === "start" ? "started" : "ended"}, but its latest state could not be loaded. Check your connection and refresh the game.` });
      }
    });
  }
  return (
    <>
      <div className="section-heading">
        <div>
          <h2>Player roster</h2>
          <p className="muted">
            {lobby
              ? "Add your players, then give each one their private code."
              : "The roster is closed. Only players who joined before the start participate."}
          </p>
          {lobby && game.joinedCount < 2 && <p className="muted">At least two players must join before the game can start.</p>}
        </div>
        <div className="actions">
          {lobby && (
            <button disabled={busy || !!teamStartBlocker(game)} onClick={() => transition("start")}>
              Start game →
            </button>
          )}
          {lobby && teamStartBlocker(game) && <p className="muted" role="status">{teamStartBlocker(game)}</p>}
          {game.status !== "ended" && (
            <button
              className="danger secondary"
              disabled={busy}
              onClick={() => transition("end")}
            >
              End game
            </button>
          )}
        </div>
      </div>
      {transitionError && (
        <div className="alert game-action-error" role="alert" tabIndex={-1} ref={transitionErrorRef}>
          <p>{transitionError.message}</p>
          <div className="actions">
            <button className="secondary compact" disabled={busy} onClick={() => run(async () => {
              try {
                await refresh();
                setTransitionError(null);
              } catch {
                setTransitionError({ message: "Could not refresh the game. Check your connection, or reopen the game with your organizer codes.", reenter: true });
              }
            })}>Refresh game</button>
            {transitionError.reenter && <a href="#/organizer">Open organizer desk</a>}
            <button className="text-button" onClick={() => setTransitionError(null)}>Dismiss</button>
          </div>
        </div>
      )}
      <div className={lobby ? "roster-layout" : ""}>
        <section className="panel roster" aria-label="Player roster">
          {!players.length ? (
            <div className="empty">
              <span>＋</span>
              <h3>An empty lobby. A fresh start.</h3>
              <p>Add your first players to get things going.</p>
            </div>
          ) : (
            <div className="table-scroll">
              <table>
                <thead>
                  <tr>
                    <th>Player</th>
                    <th>Status</th>
                    {!lobby && <th>Mission</th>}
                    {!lobby && <th>Victim</th>}
                    <th>
                      <span className="sr-only">Actions</span>
                    </th>
                  </tr>
                </thead>
                <tbody>
                  {players.map((p) => (
                    <tr key={p.id}>
                      <td>
                        <strong>{p.name}</strong>
                      </td>
                      <td>
                        <span
                          className={`player-status ${p.joinedAt ? "joined" : ""}`}
                        >
                          {lobby
                            ? p.joinedAt
                              ? "Joined"
                              : "Invited"
                            : p.included
                              ? p.status === "withdrawn" ? "Withdrawn" : p.eliminatedAt ? "Eliminated" : "Active"
                              : "Not included"}
                        </span>
                      </td>
                      {!lobby && <td className="assigned-mission">{p.mission || "—"}</td>}
                      {!lobby && <td>{p.victim?.name || "—"}</td>}
                      <td>
                        {game.status !== "ended" &&
                          (lobby || p.included) && (
                            <button
                              className="text-button"
                              disabled={busy}
                              onClick={() => {
                                if (
                                  window.confirm(
                                    `Reset ${p.name}’s private code? Their old code and existing access will stop working.`,
                                  )
                                )
                                  action(async () => {
                                    const pcode = await request(
                                      `/api/games/${game.id}/players/${p.id}/code`,
                                      {
                                        method: "POST",
                                        token: access.token,
                                      },
                                    );
                                    setIssued((prev) => [
                                      pcode,
                                      ...prev.filter(
                                        (x) => x.id !== pcode.id,
                                      ),
                                    ]);
                                  });
                              }}
                            >
                              Reset code
                            </button>
                          )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </section>
        {lobby && (
          <section className="panel">
            <h2>Add players</h2>
            <p>
              One name per line. Every player gets their own private code.
            </p>
            <form
              onSubmit={(e) => {
                e.preventDefault();
                const form = e.currentTarget;
                const names = new FormData(form)
                  .get("names")
                  .split("\n")
                  .map((n) => n.trim())
                  .filter(Boolean);
                action(async () => {
                  if (!names.length)
                    throw new Error("Enter at least one player name.");
                  if (names.some((name) => [...name].length > 80))
                    throw new Error(
                      "Player names can contain up to 80 characters.",
                    );
                  // Batch large rosters without limiting the total number of players.
                  for (
                    let offset = 0;
                    offset < names.length;
                    offset += 25
                  ) {
                    const result = await request(
                      `/api/games/${game.id}/players`,
                      {
                        method: "POST",
                        token: access.token,
                        body: { names: names.slice(offset, offset + 25) },
                      },
                    );
                    setIssued((prev) => [...result.players, ...prev]);
                    form.elements.names.value = names
                      .slice(offset + 25)
                      .join("\n");
                  }
                  form.reset();
                });
              }}
            >
              <label>
                Player names
                <textarea
                  name="names"
                  disabled={busy}
                  required
                  rows={5}
                  placeholder={"Alex\nSam\nJordan"}
                />
              </label>
              <button disabled={busy}>
                {busy ? "Adding…" : "Add to roster +"}
              </button>
            </form>
          </section>
        )}
      </div>
      <LobbyMissions game={game} access={access} action={action} busy={busy} />
      {!!issued.length && (
        <section className="secret-panel">
          <div className="section-heading">
            <div>
              <h2>Private player codes</h2>
              <p>
                Shown here until you leave this page. Share each code only
                with its player. Lost codes can be reset.
              </p>
            </div>
            <CopyButton
              text={issued
                .map(
                  (p) =>
                    `${p.name}\nGame: ${game.invitationCode}\nPrivate code: ${p.code}`,
                )
                .join("\n\n")}
              label="Copy all invitations"
            />
          </div>
          <div className="issued-grid">
            {issued.map((p) => (
              <div className="issued-code" key={p.id}>
                <strong>{p.name}</strong>
                <code>{p.code}</code>
                <CopyButton
                  text={`Hi ${p.name}! Join Elimin8 at ${location.origin}/#/player\nGame: ${game.invitationCode}\nYour private code: ${p.code}`}
                  label="Copy invitation"
                />
              </div>
            ))}
          </div>
          <button className="text-button" onClick={() => setIssued([])}>
            Hide private codes
          </button>
        </section>
      )}
    </>
  );
}
