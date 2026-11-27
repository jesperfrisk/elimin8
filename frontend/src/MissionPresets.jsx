import React, { useRef, useState } from "react";
import { request } from "./api";
import { missionPresets } from "./missionPresets";

export function MissionPresets({ game, access, action, busy }) {
  const [selected, setSelected] = useState([]);
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState("");
  const [notice, setNotice] = useState("");
  const inFlight = useRef(false);
  if (access.role !== "organizer" || game.status !== "lobby") return null;
  const disabled = busy || saving;

  async function addPresets(event) {
    event.preventDefault();
    if (disabled || inFlight.current || !selected.length) return;
    const texts = missionPresets.filter((p) => selected.includes(p.id)).map((p) => p.text);
    inFlight.current = true;
    setSaving(true);
    setError("");
    setNotice("");
    try {
      await action(async () => {
        try {
          await request(`/api/games/${game.id}/missions`, {
            method: "POST", token: access.token, body: { texts },
          });
          // Clear before the state refresh: a failed refresh must not resubmit
          // missions that the server has already saved.
          setSelected([]);
          setNotice(`Added ${texts.length} preset ${texts.length === 1 ? "mission" : "missions"} to this game.`);
        } catch (e) {
          setError(e.message);
        }
      });
    } finally {
      inFlight.current = false;
      setSaving(false);
    }
  }

  return (
    <section className="panel mission-presets" aria-label="Mission presets">
      <h2>Mission presets</h2>
      <p>Choose tasks to add to this game. You can mix them with custom missions and disable any entry later.</p>
      <form onSubmit={addPresets}>
        <fieldset disabled={disabled}>
          <legend>Choose preset missions</legend>
          {missionPresets.map((preset) => (
            <label key={preset.id}>
              <input type="checkbox" checked={selected.includes(preset.id)}
                onChange={(e) => {
                  const checked = e.target.checked;
                  setSelected((previous) => checked
                    ? [...previous, preset.id] : previous.filter((id) => id !== preset.id));
                  setNotice("");
                  setError("");
                }} />
              <span>{preset.text}</span>
            </label>
          ))}
        </fieldset>
        <p className="muted" role="status">{notice || (selected.length ? `${selected.length} selected` : "No presets selected. Choose one or more to add.")}</p>
        {error && <p className="error" role="alert">{error}</p>}
        <button type="submit" disabled={disabled || !selected.length}>
          {saving ? "Adding presets…" : "Add selected presets"}
        </button>
      </form>
    </section>
  );
}
