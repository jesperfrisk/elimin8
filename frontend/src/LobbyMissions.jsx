import React, { useState } from "react";
import { request } from "./api";

export function LobbyMissions({ game, access, action, busy }) {
  const [draft, setDraft] = useState("");
  const missions = game.missions || [];
  const enabledCount = missions.filter((m) => m.enabled).length;
  const lobby = game.status === "lobby";

  function addMissions(e) {
    e.preventDefault();
    const texts = draft.split(/\r?\n/).map((text) => text.trim()).filter(Boolean);
    action(async () => {
      if (!texts.length) throw new Error("Enter at least one mission.");
      if (texts.some((text) => [...text].length > 1000)) {
        throw new Error("Each mission can contain up to 1,000 characters.");
      }
      for (let offset = 0; offset < texts.length; offset += 100) {
        await request(`/api/games/${game.id}/missions`, {
          method: "POST",
          token: access.token,
          body: { texts: texts.slice(offset, offset + 100) },
        });
        // Keep only unsaved lines if a later batch fails.
        setDraft(texts.slice(offset + 100).join("\n"));
      }
    });
  }

  return (
    <section className="lobby-missions" aria-label="Lobby missions">
      <div className="section-heading">
        <div>
          <h2>Lobby missions</h2>
          <p className="muted">
            {lobby
              ? "Add tasks for this game. Disable any you want to leave out."
              : "Mission configuration is closed for this game."}
          </p>
        </div>
        <span className="badge">{enabledCount} enabled / {missions.length} total</span>
      </div>
      {lobby && (
        <p className="muted">
          {enabledCount
            ? "At the start, each joined player gets a random mission. Every enabled entry is used before any is reused."
            : "Enable at least one mission before starting with players."}
        </p>
      )}
      <div className={lobby ? "roster-layout" : ""}>
        <section className="panel roster" aria-label="Mission list">
          {!missions.length ? (
            <div className="empty">
              <h3>No missions yet</h3>
              <p>Add a few or paste a whole list to get started.</p>
            </div>
          ) : (
            <ul className="lobby-mission-list">
              {missions.map((mission) => (
                <li key={mission.id} className={mission.enabled ? "" : "mission-disabled"}>
                  <div>
                    <p>{mission.text}</p>
                    <small>{mission.enabled ? "Enabled" : "Disabled"}</small>
                  </div>
                  {lobby && (
                    <button type="button" className="secondary compact" disabled={busy}
                      aria-label={`${mission.enabled ? "Disable" : "Enable"} mission: ${mission.text}`}
                      onClick={() => action(() => request(`/api/games/${game.id}/missions/${mission.id}`, {
                        method: "PATCH", token: access.token,
                        body: { enabled: !mission.enabled },
                      }))}>
                      {mission.enabled ? "Disable" : "Enable"}
                    </button>
                  )}
                </li>
              ))}
            </ul>
          )}
        </section>
        {lobby && (
          <section className="panel">
            <h2>Add missions</h2>
            <p>One mission per line. New missions are enabled automatically.</p>
            <form onSubmit={addMissions}>
              <div className="mission-entry">
                <label htmlFor="lobby-mission-texts">Mission texts</label>
                <textarea id="lobby-mission-texts" name="missions" required disabled={busy} rows={7}
                  value={draft} onChange={(e) => setDraft(e.target.value)}
                  placeholder={"Get your victim to give you a high-five.\nGet your victim to say pineapple.\nGet your victim to take a selfie with you."} />
              </div>
              <button disabled={busy}>Add missions +</button>
            </form>
          </section>
        )}
      </div>
    </section>
  );
}
