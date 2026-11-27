import React, { useEffect, useState } from "react";
import { request } from "./api";
import { exchangeSession, sessionFailure } from "./rememberedGames";
import { LoginForm } from "./LoginForm";

// Shown at #/player/<gameId> when this tab has no access to the game. If the
// browser remembers the game, ask the server to sign the same player back in with
// the cookie; if that sign-in is gone (cookie lost, 30 days passed, signed out
// elsewhere, code reset by the organizer), ask for the private code. A code
// recovery returns the same player and sets the cookie again.

function RecoveryForm({ entry, onAdmitted, run, busy }) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const code = new FormData(form).get("code");
        run(async () => {
          const access = await request("/api/access", {
            method: "POST",
            body: { invitationCode: entry.invitationCode, code, role: "player" },
          });
          form.reset(); // the code only ever lives in the input, never in state or storage
          if (access.game?.id !== entry.gameId) throw new Error("That code belongs to a different game.");
          onAdmitted(access);
        });
      }}
    >
      <p>
        Game <strong>{entry.name}</strong>
        {entry.invitationCode && (
          <>
            {" "}
            · invitation code <code>{entry.invitationCode}</code>
          </>
        )}
      </p>
      <label>
        Your private player code
        <input
          name="code"
          type="password"
          required
          maxLength={64}
          autoComplete="off"
          spellCheck={false}
          placeholder="Your 8-character code"
        />
      </label>
      <button disabled={busy}>{busy ? "Opening…" : "Enter game →"}</button>
    </form>
  );
}

const RETRY_MESSAGES = {
  offline: "We couldn’t reach the server. Check your connection and try again.",
  busy: "Too many attempts. Wait a minute and try again.",
  error: "Something went wrong signing you back in. Try again, or enter your code.",
};

export function PlayerGateway({ gameId, entry, ended, onRestored, onAdmitted, onRemove, run, busy }) {
  // "restoring" asks the server; "retry" is a temporary failure; "recover" asks for the code.
  const [phase, setPhase] = useState(entry && !ended ? "restoring" : "recover");
  const [failure, setFailure] = useState("error");
  const [attempt, setAttempt] = useState(0);
  useEffect(() => {
    if (!entry || ended) return undefined; // nothing remembered, or already known to have ended
    let stopped = false;
    setPhase("restoring");
    exchangeSession(gameId)
      .then((admission) => {
        if (!stopped) onRestored(admission);
      })
      .catch((e) => {
        if (stopped) return;
        const kind = sessionFailure(e);
        setFailure(kind);
        setPhase(kind === "ended" ? "recover" : "retry");
      });
    return () => {
      stopped = true;
    };
  }, [gameId, attempt]);

  if (phase === "restoring") {
    return (
      <section className="panel" aria-live="polite">
        <h1>Opening your game…</h1>
        <p role="status">Signing you back in on this device.</p>
      </section>
    );
  }
  if (phase === "retry") {
    return (
      <section className="panel">
        <h1>Couldn’t open your game</h1>
        <p role="alert">{RETRY_MESSAGES[failure] || RETRY_MESSAGES.error}</p>
        <div className="actions">
          <button onClick={() => setAttempt((n) => n + 1)}>Try again</button>
          <button className="secondary" onClick={() => setPhase("recover")}>
            Enter my code instead
          </button>
        </div>
      </section>
    );
  }
  return (
    <section className="panel narrow">
      <h1>{entry ? "Sign in again to continue" : "Enter your code to return"}</h1>
      {entry ? (
        <>
          <p>
            Your sign-in on this device is no longer valid. This happens when cookies were cleared, the 30 days ran
            out, you signed out, or the organizer reset your code. Enter your private code to get back in as the same
            player.
          </p>
          <RecoveryForm entry={entry} onAdmitted={onAdmitted} run={run} busy={busy} />
          <button className="text-button" onClick={onRemove}>
            Remove this game from this device
          </button>
        </>
      ) : (
        <>
          <p>This browser doesn’t remember that game. Enter its invitation code and your private player code.</p>
          <LoginForm role="player" run={run} busy={busy} admitted={onAdmitted} />
        </>
      )}
    </section>
  );
}
