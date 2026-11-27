import React, { useEffect, useState } from "react";
import { readAccess, rememberAccess, forgetAccess } from "./api";
import { readRemembered, rememberGame, forgetRemembered, revokeSession, sessionFailure } from "./rememberedGames";
import { Home } from "./Home";
import { OrganizerHome } from "./OrganizerHome";
import { PlayerHome } from "./PlayerHome";
import { PlayerGateway } from "./PlayerGateway";
import { GamePage } from "./GamePage";
import { Board } from "./Board";

function useRoute() {
  const [route, setRoute] = useState(location.hash.slice(1) || "/");
  useEffect(() => {
    const change = () => setRoute(location.hash.slice(1) || "/");
    window.addEventListener("hashchange", change);
    return () => window.removeEventListener("hashchange", change);
  }, []);
  return route;
}

export function App() {
  const route = useRoute();
  // `saved` is this tab's access (bearer admissions, in sessionStorage). `remembered`
  // is the non-secret metadata of player games this device can sign back in to
  // (localStorage), paired with an HttpOnly cookie that JavaScript never reads.
  const [saved, setSaved] = useState(readAccess);
  const [remembered, setRemembered] = useState(readRemembered);
  // Games whose sign-in ended in this tab: ask for the code instead of retrying the cookie.
  const [ended, setEnded] = useState({});
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
  const clearEnded = (gameId) =>
    setEnded((current) => {
      const { [gameId]: _removed, ...rest } = current;
      return rest;
    });
  function store(access) {
    rememberAccess(access);
    if (access.role === "player") rememberGame(access);
    setSaved(readAccess());
    setRemembered(readRemembered());
    clearEnded(access.game.id);
  }
  function admitted(access) {
    store(access);
    if (access.managementCode)
      setSecret({ gameId: access.game.id, code: access.managementCode });
    location.hash = `/${access.role}/${access.game.id}`;
  }
  // The cookie signed this device's player back in (after a restart or a lost tab).
  const restored = (admission) => store(admission);
  // A fresh admission for the open game: the token changes, the page keeps its state.
  function renewed(admission) {
    rememberAccess(admission);
    setSaved(readAccess());
  }
  // The remembered sign-in is gone: drop this tab's access but keep the saved game,
  // so the code-recovery form can offer to sign the same player back in.
  function accessLost(access) {
    forgetAccess(access);
    setSaved(readAccess());
    setEnded((current) => ({ ...current, [access.game.id]: true }));
  }
  // Sign out revokes the remembered credential on the server first. If that fails
  // the player stays signed in and is told, so a shared device never looks signed
  // out while the cookie still works.
  async function signOut(access) {
    if (access.role === "player") {
      try {
        await revokeSession(access.game.id);
      } catch (e) {
        return sessionFailure(e) === "busy"
          ? "Too many attempts. Wait a minute and try again."
          : "Couldn’t sign out. Check your connection and try again.";
      }
      forgetRemembered(access.game.id);
      setRemembered(readRemembered());
    }
    forgetAccess(access);
    setSaved(readAccess());
    location.hash = `/${access.role}`;
    return "";
  }
  // Used when the sign-in already ended: the cookie is unusable, so removal is local
  // (the server call is a harmless best effort).
  function removeRemembered(gameId) {
    revokeSession(gameId).catch(() => {});
    forgetRemembered(gameId);
    setRemembered(readRemembered());
    clearEnded(gameId);
    location.hash = "/player";
  }
  const parts = route.split("/").filter(Boolean);
  const role = parts[0];
  const gameId = parts[1];
  const access = saved.find((a) => a.role === role && a.game.id === gameId);
  // Everything this device can open: this tab's games plus remembered player games.
  const listed = [
    ...saved,
    ...remembered
      .filter((r) => !saved.some((a) => a.role === "player" && a.game.id === r.gameId))
      .map((r) => ({
        role: "player",
        remembered: true,
        game: { id: r.gameId, name: r.name, invitationCode: r.invitationCode },
      })),
  ];
  // The public live board is full-screen and needs no login or app chrome.
  if (role === "board" && gameId) return <Board key={gameId} gameId={gameId} />;
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
            onRenew={renewed}
            onLost={() => accessLost(access)}
            onForget={() => signOut(access)}
          />
        ) : gameId && role === "player" ? (
          <PlayerGateway
            key={gameId}
            gameId={gameId}
            entry={remembered.find((r) => r.gameId === gameId) || null}
            ended={!!ended[gameId]}
            onRestored={restored}
            onAdmitted={admitted}
            onRemove={() => removeRemembered(gameId)}
            run={run}
            busy={busy}
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
            saved={listed}
          />
        ) : role === "player" ? (
          <PlayerHome run={run} busy={busy} admitted={admitted} saved={listed} />
        ) : (
          <Home saved={listed} />
        )}
      </main>
      <footer>
        ELIMIN8 <span>Trust no one. Play together.</span>
      </footer>
    </>
  );
}
