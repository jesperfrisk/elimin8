import React, { useEffect, useState } from "react";
import { request } from "./api";

export function formatRemaining(totalSeconds) {
  const s = Math.max(0, Math.floor(totalSeconds));
  const d = Math.floor(s / 86400);
  const h = Math.floor((s % 86400) / 3600);
  const m = Math.floor((s % 3600) / 60);
  const pad = n => String(n).padStart(2, "0");
  const clock = `${pad(h)}:${pad(m)}:${pad(s % 60)}`;
  return d > 0 ? `${d}d ${clock}` : clock;
}

// The server supplies remaining time; anchor it to the local clock so client
// clock skew cannot distort the display. Shared with the live board.
export function useCountdown(remainingSeconds, resetKey) {
  const [deadline, setDeadline] = useState(() => Date.now() + (remainingSeconds || 0) * 1000);
  const [now, setNow] = useState(Date.now());
  useEffect(() => {
    setDeadline(Date.now() + (remainingSeconds || 0) * 1000);
  }, [resetKey, remainingSeconds]);
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000);
    return () => clearInterval(id);
  }, []);
  return Math.max(0, (deadline - now) / 1000);
}

function Countdown({ game }) {
  const left = useCountdown(game.remainingSeconds, game.revision);
  return (
    <section className="panel timer" aria-label="Game timer">
      <h2>Time remaining</h2>
      <p className="countdown" role="timer" data-testid="countdown">{formatRemaining(left)}</p>
    </section>
  );
}

export function Timer({ game, access, action, busy }) {
  const [minutes, setMinutes] = useState("");
  useEffect(() => {
    setMinutes(game.timerMinutes ? String(game.timerMinutes) : "");
  }, [game.timerMinutes]);
  if (game.status === "active") {
    return game.endsAt ? <Countdown game={game} /> : null;
  }
  if (game.status !== "lobby") return null;
  const set = value => action(async () => {
    await request(`/api/games/${game.id}/timer`, {
      method: "PUT", token: access.token, body: { durationMinutes: value },
    });
  });
  if (access.role !== "organizer") {
    return game.timerMinutes ? (
      <section className="panel timer" aria-label="Game timer">
        <p>This game has a time limit of {game.timerMinutes} minutes, starting when the game starts.</p>
      </section>
    ) : null;
  }
  const value = Number(minutes);
  const valid = Number.isInteger(value) && value >= 1 && value <= 10080;
  return (
    <section className="panel timer" aria-label="Game timer">
      <h2>Time limit (optional)</h2>
      <p>The countdown starts when you start the game. Leave it unset for no limit.</p>
      <label>
        Minutes (1 to 10080)
        <input type="number" min="1" max="10080" value={minutes} onChange={e => setMinutes(e.target.value)} />
      </label>
      <div className="actions">
        <button disabled={busy || !valid} onClick={() => set(value)}>Set time limit</button>
        <button className="secondary" disabled={busy || !game.timerMinutes} onClick={() => set(null)}>Clear time limit</button>
      </div>
      {game.timerMinutes && <p role="status">Current limit: {game.timerMinutes} minutes.</p>}
    </section>
  );
}
