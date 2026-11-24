import React, { useEffect, useState } from "react";
import { readAccess, rememberAccess, forgetAccess } from "./api";
import { Home } from "./Home";
import { OrganizerHome } from "./OrganizerHome";
import { PlayerHome } from "./PlayerHome";
import { GamePage } from "./GamePage";

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
