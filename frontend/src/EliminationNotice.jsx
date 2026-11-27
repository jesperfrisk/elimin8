import React from "react";

// Persistent private notice for the eliminated player. Rendered from the
// role-scoped game view (me.elimination), so it survives reloads and reconnects.
export function EliminationNotice({ game }) {
  const receipt = game.me?.elimination;
  if (!receipt) return null;
  const when = new Date(receipt.confirmedAt).toLocaleString();
  return (
    <section className="panel private-mission" aria-label="Elimination notice" aria-live="polite">
      <h3>You were eliminated</h3>
      <p><strong>{receipt.killer.name}</strong> eliminated you.</p>
      <p>
        {receipt.mission
          ? <>Mission used: <strong>{receipt.mission}</strong></>
          : "Mission details are unavailable for this elimination."}
      </p>
      <small>Confirmed <time dateTime={receipt.confirmedAt}>{when}</time>. Only you can see this notice.</small>
    </section>
  );
}
