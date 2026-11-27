import React, { useEffect, useRef, useState } from "react";
import { request } from "./api";
import { PlayerAvatar } from "./PlayerAvatar";
import { shrinkPhoto, checkPhotoFile } from "./photoImage";

const photoPath = (game, playerId) => `/api/games/${game.id}/players/${playerId}/photo`;

async function uploadPhoto(game, playerId, blob, token) {
  const form = new FormData();
  form.append("photo", blob, "photo.jpg");
  return request(photoPath(game, playerId), { method: "PUT", token, body: form });
}

// Player panel (lobby and active game): add, replace or remove your own photo.
// Nothing is uploaded, or shown publicly, until you confirm the preview.
export function PhotoUpload({ game, access, action, busy }) {
  const me = game.me;
  const [picked, setPicked] = useState(null); // { blob, previewUrl }
  const [error, setError] = useState("");
  const [preparing, setPreparing] = useState(false);
  const camera = useRef(null);
  const gallery = useRef(null);
  useEffect(
    () => () => {
      if (picked) URL.revokeObjectURL(picked.previewUrl);
    },
    [picked],
  );
  const eligible =
    access.role === "player" &&
    me &&
    (game.status === "lobby" || (game.status === "active" && me.included));
  if (!eligible) return null;

  async function choose(event) {
    const file = event.target.files?.[0];
    event.target.value = ""; // so picking the same file again still fires
    if (!file) return;
    setError("");
    const problem = checkPhotoFile(file);
    if (problem) {
      setError(problem);
      return;
    }
    setPreparing(true);
    try {
      const blob = await shrinkPhoto(file);
      setPicked({ blob, previewUrl: URL.createObjectURL(blob) });
    } catch (e) {
      setError(e.message);
    } finally {
      setPreparing(false);
    }
  }
  const confirm = () =>
    action(async () => {
      setError("");
      try {
        await uploadPhoto(game, me.id, picked.blob, access.token);
        setPicked(null);
      } catch (e) {
        setError(e.message); // too large, unsupported, game ended, ...
      }
    });
  const remove = () => {
    if (!window.confirm("Remove your photo? Your initials will show on the board instead.")) return;
    action(async () => {
      setError("");
      try {
        await request(photoPath(game, me.id), { method: "DELETE", token: access.token });
      } catch (e) {
        setError(e.message);
      }
    });
  };
  const disabled = busy || preparing;
  return (
    <section className="panel photo-panel" aria-label="Your photo">
      <h2>Your photo</h2>
      <p className="muted">
        Optional. Your photo is shown on the public game board, so anyone with the board link can see it. It is deleted
        24 hours after the game ends.
      </p>
      <div className="photo-row">
        <div className="photo-poster">
          {picked ? (
            <img src={picked.previewUrl} alt="Preview of your new photo" />
          ) : (
            <PlayerAvatar className="avatar-fill" id={me.id} name={me.name} photoUrl={me.photoUrl} token={access.token} />
          )}
        </div>
        <div className="photo-actions">
          {picked ? (
            <>
              <p role="status">This is how you will look on the board.</p>
              <button disabled={disabled} onClick={confirm}>
                Use this photo
              </button>
              <button className="secondary" disabled={disabled} onClick={() => gallery.current?.click()}>
                Choose another
              </button>
              <button className="text-button" disabled={disabled} onClick={() => setPicked(null)}>
                Cancel
              </button>
            </>
          ) : (
            <>
              <button disabled={disabled} onClick={() => camera.current?.click()}>
                Take a photo
              </button>
              <button className="secondary" disabled={disabled} onClick={() => gallery.current?.click()}>
                Choose a photo
              </button>
              {me.photoUrl && (
                <button className="text-button" disabled={disabled} onClick={remove}>
                  Remove photo
                </button>
              )}
              {preparing && <p role="status">Preparing your photo…</p>}
            </>
          )}
          {error && (
            <p className="photo-error" role="alert">
              {error}
            </p>
          )}
        </div>
      </div>
      {/* Two inputs so nobody is stuck with camera-only: capture opens the front camera on phones. */}
      <input ref={camera} type="file" accept="image/*" capture="user" hidden onChange={choose} data-testid="photo-camera" />
      <input
        ref={gallery}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        hidden
        onChange={choose}
        data-testid="photo-gallery"
      />
    </section>
  );
}

// Organizer controls for one roster row: add or replace a photo (uploaded straight
// away) and remove an inappropriate one.
export function RosterPhotoControls({ game, access, player, action, busy }) {
  const input = useRef(null);
  const replace = (event) => {
    const file = event.target.files?.[0];
    event.target.value = "";
    if (!file) return;
    action(async () => {
      const blob = await shrinkPhoto(file); // a rejected or unreadable photo shows as the page alert
      await uploadPhoto(game, player.id, blob, access.token);
    });
  };
  const remove = () => {
    if (!window.confirm(`Remove ${player.name}’s photo?`)) return;
    action(() => request(photoPath(game, player.id), { method: "DELETE", token: access.token }));
  };
  return (
    <>
      <button className="text-button" disabled={busy} onClick={() => input.current?.click()}>
        {player.photoUrl ? "Replace photo" : "Add photo"}
      </button>
      {player.photoUrl && (
        <button className="text-button" disabled={busy} onClick={remove}>
          Remove photo
        </button>
      )}
      <input
        ref={input}
        type="file"
        accept="image/jpeg,image/png,image/webp"
        hidden
        onChange={replace}
        aria-label={`Photo file for ${player.name}`}
      />
    </>
  );
}
