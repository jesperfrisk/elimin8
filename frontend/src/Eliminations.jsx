import React, { useEffect, useRef } from "react";
import { request } from "./api";

export function Eliminations({ game, access, action, busy }) {
  const promptRef = useRef(null);
  const participants = game.participants || [];
  const claims = game.eliminations || [];
  const me = game.me;
  const incoming = claims.find(c => c.status === "pending" && c.victimId === me?.id);
  const outgoing = claims.find(c => c.status === "pending" && c.killerId === me?.id);
  const name = id => participants.find(p => p.id === id)?.name || "A player";
  const canReport = game.status === "active" && me?.included && !me.eliminatedAt && !outgoing;
  const target = me?.victim;
  useEffect(() => {
    if (incoming) {
      promptRef.current?.focus({ preventScroll: true });
      promptRef.current?.scrollIntoView({ block: "nearest", behavior: "smooth" });
    }
  }, [incoming?.id]);
  if (game.status === "lobby" || access.role !== "player") return null;
  return (
    <section className="panel eliminations" aria-label="Eliminations">
      {incoming && game.status === "active" && (
        <section className="confirmation-request" aria-label="Elimination confirmation" aria-live="assertive" tabIndex={-1} ref={promptRef}>
          <h2>Were you eliminated?</h2>
          <p><strong>{name(incoming.killerId)}</strong> says they have killed you. Is this correct?</p>
          <p>Accepting eliminates you and transfers your victim and mission to them.</p>
          <div className="actions">
            <button disabled={busy} onClick={() => respond(incoming.id, true)}>Accept elimination</button>
            <button className="secondary" disabled={busy} onClick={() => respond(incoming.id, false)}>Decline elimination</button>
          </div>
        </section>
      )}
      {me?.eliminatedAt && <p role="status">You have been eliminated. Your victim and mission have been transferred.</p>}
      {game.winnerId && <p role="status"><strong>{name(game.winnerId)}</strong> is the last player standing!</p>}
      {outgoing && <p role="status">Waiting for {name(outgoing.victimId)} to confirm your elimination report.</p>}
      {canReport && (
        <>
          <h2>Report an elimination</h2>
          <p>You can only report your assigned target. They must accept before any assignments change.</p>
          {target && <p>Report that you killed <strong>{target.name}</strong>?</p>}
          <button disabled={busy || !target} onClick={() => action(async () => {
            await request(`/api/games/${game.id}/eliminations`, {
              method: "POST", token: access.token, body: { victimId: target.id },
            });
          })}>Confirm elimination report</button>
        </>
      )}
      {claims.some(c => c.status !== "pending") && (
        <section className="elimination-history" aria-label="Your elimination reports">
          <h3>Your reports</h3>
          <ul>{claims.filter(c => c.status !== "pending").slice().reverse().map(c => (
            <li key={c.id}>{c.killerId === me?.id ? `Your report for ${name(c.victimId)}` : `${name(c.killerId)}'s report for you`}: {c.status}.</li>
          ))}</ul>
        </section>
      )}
    </section>
  );

  function respond(id, accept) {
    action(() => request(`/api/games/${game.id}/eliminations/${id}/response`, {
      method: "POST", token: access.token, body: { accept },
    }));
  }
}
