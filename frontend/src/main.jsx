import React, { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { request, readAccess, rememberAccess, forgetAccess } from "./api";
import { useGame } from "./useGame";
import "./style.css";

function useRoute() {
  const [route, setRoute] = useState(location.hash.slice(1) || "/");
  useEffect(() => {
    const change = () => setRoute(location.hash.slice(1) || "/");
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  return route;
}

function App() {
  const route = useRoute();
  const [saved, setSaved] = useState(readAccess);
  const [secret, setSecret] = useState(null);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);
  useEffect(() => {
    setError("");
  }, [route]);
  async function run(work) {
    setBusy(true);
    setError("");
    try {
      await work();
    } catch (e) {
      setError(e.message);
    } finally {
      setBusy(false);
    }
  }
  function admitted(access) {
    rememberAccess(access);
    setSaved(readAccess());
    if (access.managementCode)
      setSecret({ gameId: access.game.id, code: access.managementCode });
    location.hash = `/${access.role}/${access.game.id}`;
  }
  const parts = route.split("/").filter(Boolean);
  const role = parts[0];
  const gameId = parts[1];
  const access = saved.find((a) => a.role === role && a.game.id === gameId);
  return (
    <>
      <header className="topbar">
        <a className="brand" href="#/">
          elimin<span>8</span>
          <span className="brand-dot">●</span>
        </a>
        <nav aria-label="Main navigation">
          <a
            className={role === "organizer" ? "selected" : ""}
            href="#/organizer"
          >
            Organize
          </a>
          <a className={role === "player" ? "selected" : ""} href="#/player">
            Play
          </a>
        </nav>
      </header>
      <main>
        {error && (
          <div className="alert" role="alert">
            {error}
            <button
              className="text-button"
              onClick={() => setError("")}
              aria-label="Dismiss error"
            >
              ×
            </button>
          </div>
        )}
        {gameId && access ? (
          <GamePage
            key={`${role}/${gameId}`}
            access={access}
            run={run}
            busy={busy}
            secret={secret?.gameId === gameId ? secret.code : null}
            hideSecret={() => setSecret(null)}
            onForget={() => {
              forgetAccess(access);
              setSaved(readAccess());
              location.hash = `/${role}`;
            }}
          />
        ) : gameId ? (
          <section className="panel">
            <h1>Enter your code to return</h1>
            <p>This browser tab does not have access to that game.</p>
            <a
              className="button"
              href={`#/${role === "organizer" ? "organizer" : "player"}`}
            >
              Open game access
            </a>
          </section>
        ) : role === "organizer" ? (
          <OrganizerHome
            run={run}
            busy={busy}
            admitted={admitted}
            saved={saved}
          />
        ) : role === "player" ? (
          <PlayerHome run={run} busy={busy} admitted={admitted} saved={saved} />
        ) : (
          <Home saved={saved} />
        )}
      </main>
      <footer>
        ELIMIN8 <span>Trust no one. Play together.</span>
      </footer>
    </>
  );
}

function Home({ saved }) {
  return (
    <>
      <section className="hero">
        <p className="eyebrow">The game starts with your people</p>
        <h1>
          Gather your crew.
          <br />
          <span>Set the game in motion.</span>
        </h1>
        <p className="intro">
          Create a lobby, invite your players, and bring everyone together. Your
          next game starts here.
        </p>
        <div className="actions">
          <a className="button" href="#/organizer">
            Create a game ↗
          </a>
          <a className="button secondary" href="#/player">
            Join a game →
          </a>
        </div>
      </section>
      <div className="feature-grid">
        <article className="panel">
          <span className="step">01 / ORGANIZE</span>
          <h2>Your game. Your roster.</h2>
          <p>
            Add your players and give each one their own private entry code.
          </p>
        </article>
        <article className="panel">
          <span className="step">02 / GATHER</span>
          <h2>Watch the lobby fill.</h2>
          <p>
            See who has joined as it happens. Start when your crew is ready.
          </p>
        </article>
        <article className="panel">
          <span className="step">03 / BEGIN</span>
          <h2>Everyone in? Let’s go.</h2>
          <p>
            Only players who joined the lobby enter the game when it starts.
          </p>
        </article>
      </div>
      <SavedGames saved={saved} />
    </>
  );
}

function SavedGames({ saved, role }) {
  const games = saved.filter((a) => !role || a.role === role);
  if (!games.length) return null;
  return (
    <section className="saved">
      <h2>Return to a game</h2>
      <p className="muted">
        Access remembered in this browser tab. Your private code works on other
        devices.
      </p>
      <div className="game-links">
        {games.map((a) => (
          <a
            className="panel game-link"
            key={`${a.role}/${a.game.id}`}
            href={`#/${a.role}/${a.game.id}`}
          >
            <span>
              <strong>{a.game.name}</strong>
              <small>
                {a.role === "organizer" ? "Organizer" : "Player"} ·{" "}
                {a.game.invitationCode}
              </small>
            </span>
            <span>↗</span>
          </a>
        ))}
      </div>
    </section>
  );
}

function OrganizerHome({ run, busy, admitted, saved }) {
  return (
    <>
      <PageHeading
        eyebrow="Organizer desk"
        title="Make room for a new game."
        text="Build your roster, share private codes, and start when everyone is ready."
      />
      <div className="two-columns">
        <section className="panel">
          <h2>Create a game</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const data = new FormData(e.currentTarget);
              run(async () =>
                admitted(
                  await request("/api/games", {
                    method: "POST",
                    body: {
                      name: data.get("name"),
                      description: data.get("description"),
                    },
                  }),
                ),
              );
            }}
          >
            <label>
              Game name
              <input
                name="name"
                required
                maxLength={100}
                placeholder="Friday night showdown"
              />
            </label>
            <label>
              Description (optional)
              <textarea
                name="description"
                maxLength={2000}
                rows={3}
                placeholder="Where, when, and anything your players need to know."
              />
            </label>
            <button disabled={busy}>
              {busy ? "Working…" : "Create game →"}
            </button>
          </form>
        </section>
        <section className="panel">
          <h2>Manage an existing game</h2>
          <p>
            Use the invitation code and the management code you received when
            creating the game.
          </p>
          <LoginForm
            role="organizer"
            run={run}
            busy={busy}
            admitted={admitted}
          />
        </section>
      </div>
      <SavedGames role="organizer" saved={saved} />
    </>
  );
}

function PlayerHome({ run, busy, admitted, saved }) {
  return (
    <>
      <PageHeading
        eyebrow="Player entrance"
        title="Your place is waiting."
        text="Get your game invitation code and private player code from the organizer."
      />
      <section className="panel narrow">
        <h2>Join or return to a game</h2>
        <p>
          Your organizer has already added your name. Your private code opens
          your place in the game.
        </p>
        <LoginForm role="player" run={run} busy={busy} admitted={admitted} />
      </section>
      <SavedGames role="player" saved={saved} />
    </>
  );
}

function LoginForm({ role, run, busy, admitted }) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const data = new FormData(form);
        run(async () => {
          const a = await request("/api/access", {
            method: "POST",
            body: {
              invitationCode: data.get("invitationCode"),
              code: data.get("code"),
              role,
            },
          });
          form.reset();
          admitted(a);
        });
      }}
    >
      <label>
        Game invitation code
        <input
          name="invitationCode"
          className="code-input"
          required
          maxLength={16}
          autoCapitalize="characters"
          autoComplete="off"
          spellCheck={false}
          placeholder="ABCD2345"
        />
      </label>
      <label>
        {role === "organizer"
          ? "Organizer management code"
          : "Your private player code"}
        <input
          name="code"
          type="password"
          required
          maxLength={64}
          autoComplete="off"
          spellCheck={false}
          placeholder={
            role === "organizer"
              ? "Your management code"
              : "Your 8-character code"
          }
        />
      </label>
      <button disabled={busy}>
        {busy
          ? "Opening…"
          : role === "organizer"
            ? "Open organizer desk →"
            : "Enter game →"}
      </button>
    </form>
  );
}

function PageHeading({ eyebrow, title, text }) {
  return (
    <div className="page-heading">
      <p className="eyebrow">{eyebrow}</p>
      <h1>{title}</h1>
      <p className="intro">{text}</p>
    </div>
  );
}

function CopyButton({ text, label = "Copy" }) {
  const [status, setStatus] = useState("");
  return (
    <>
      <button
        type="button"
        className="secondary compact"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setStatus("Copied");
          } catch {
            setStatus("Select the text to copy it manually.");
          }
        }}
      >
        {label}
      </button>
      <span className="copy-status" role="status">
        {status}
      </span>
    </>
  );
}

function GamePage({ access, run, busy, secret, hideSecret, onForget }) {
  const { game, connection, error, refresh } = useGame(access);
  const [issued, setIssued] = useState([]);
  const organizer = access.role === "organizer";
  const players = game.players || [];
  const lobby = game.status === "lobby";
  const active = game.status === "active";
  const action = (work) =>
    run(async () => {
      await work();
      await refresh();
    });
  function transition(name) {
    const message =
      name === "start"
        ? `Start with ${game.joinedCount} joined player(s)? ${game.invitedCount - game.joinedCount} invited player(s) have not joined and will be excluded.`
        : "End this game? This cannot be undone.";
    if (!window.confirm(message)) return;
    action(() =>
      request(`/api/games/${game.id}/${name}`, {
        method: "POST",
        token: access.token,
      }),
    );
  }
  if (!game.createdAt) {
    return (
      <section className="panel" aria-live="polite">
        <h1>Opening your game…</h1>
        <p>{error || "Loading the latest game state."}</p>
        {error && <a href={`#/${access.role}`}>Enter your code again</a>}
      </section>
    );
  }
  return (
    <>
      <div className="breadcrumbs">
        <a href={`#/${access.role}`}>
          ← {organizer ? "Organizer desk" : "Your games"}
        </a>
        <button className="text-button" onClick={onForget}>
          Forget access in this tab
        </button>
      </div>
      {error && (
        <div className="alert" role="alert">
          {error}{" "}
          <button className="secondary compact" onClick={() => run(refresh)}>
            Reconnect
          </button>
          <a href={`#/${access.role}`}>Enter your code again</a>
        </div>
      )}
      <div className="game-heading">
        <div>
          <p className="eyebrow">
            {organizer
              ? "Organizer desk"
              : `Playing as ${game.me?.name || "player"}`}
          </p>
          <h1>{game.name}</h1>
          {game.description && (
            <p className="description">{game.description}</p>
          )}
        </div>
        <div className="game-signals">
          <span className={`badge ${game.status}`}>
            {lobby ? "Lobby open" : active ? "Game active" : "Game ended"}
          </span>
          <span className="connection" role="status">
            <i
              className={
                connection === "Live updates connected" ? "online" : ""
              }
            />
            {connection}
          </span>
        </div>
      </div>
      {secret && (
        <section className="secret-panel">
          <h2>Save your organizer management code</h2>
          <p>
            This code is shown once. Keep it private—you’ll need it to manage
            the game from another browser.
          </p>
          <code className="management-code">{secret}</code>
          <div className="actions">
            <CopyButton
              text={`Game: ${game.invitationCode}\nOrganizer management code: ${secret}`}
              label="Copy organizer credentials"
            />
            <button className="text-button" onClick={hideSecret}>
              I’ve saved it · hide
            </button>
          </div>
        </section>
      )}
      <div className="stats">
        <div>
          <span>{lobby ? "Joined the lobby" : "Game participants"}</span>
          <strong>
            {lobby ? game.joinedCount : game.participantCount}
            <small>{lobby ? ` / ${game.invitedCount}` : ""}</small>
          </strong>
        </div>
        <div>
          <span>Game invitation code</span>
          <div className="invitation">
            <code>{game.invitationCode}</code>
            <CopyButton text={game.invitationCode} />
          </div>
        </div>
        <div>
          <span>
            {lobby ? "Ready when you are" : active ? "Started" : "Ended"}
          </span>
          <strong className="stat-text">
            {lobby
              ? "Waiting in the lobby"
              : formatDate(active ? game.startedAt : game.endedAt)}
          </strong>
        </div>
      </div>
      {organizer ? (
        <>
          <div className="section-heading">
            <div>
              <h2>Player roster</h2>
              <p className="muted">
                {lobby
                  ? "Add your players, then give each one their private code."
                  : "The roster is closed. Only players who joined before the start participate."}
              </p>
            </div>
            <div className="actions">
              {lobby && (
                <button disabled={busy} onClick={() => transition("start")}>
                  Start game →
                </button>
              )}
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
                                  ? "Participant"
                                  : "Not included"}
                            </span>
                          </td>
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
      ) : (
        <section className="panel player-card">
          <div className="player-symbol">
            {lobby ? "◎" : active ? "↗" : "✓"}
          </div>
          <p className="eyebrow">{game.me?.name}, you’re in</p>
          <h2>
            {lobby
              ? "Your place in the lobby is confirmed."
              : active
                ? "The game is on."
                : "This game has ended."}
          </h2>
          <p>
            {lobby
              ? "Your organizer will start the game when everyone is ready. You can close this page and return with your codes—your place stays joined."
              : active
                ? "You’re part of the active game. No missions or victims are assigned yet."
                : "Thanks for being part of it. You can return to your games or join another lobby."}
          </p>
          {lobby && (
            <p className="muted">
              This page updates automatically when your organizer starts or ends
              the game.
            </p>
          )}
        </section>
      )}
    </>
  );
}

function formatDate(value) {
  return value
    ? new Date(value).toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : "—";
}
createRoot(document.getElementById("root")).render(<App />);
