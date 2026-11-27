import React, { useEffect, useState } from "react";
import { avatarHue, initials } from "./boardLayout";
import "./photo.css";

// Photos of players in a started game are readable without credentials (that is
// how the public board shows them). An organizer managing an invitee who has not
// joined needs credentials, so when a token is given the image is fetched with it.
function useImageSource(url, token) {
  const [source, setSource] = useState(token ? null : url || null);
  useEffect(() => {
    if (!url) {
      setSource(null);
      return undefined;
    }
    if (!token) {
      setSource(url);
      return undefined;
    }
    let stopped = false;
    let objectUrl = null;
    const controller = new AbortController();
    fetch(url, { headers: { Authorization: `Bearer ${token}` }, signal: controller.signal })
      .then((response) => (response.ok ? response.blob() : Promise.reject(new Error("photo unavailable"))))
      .then((blob) => {
        if (stopped) return;
        objectUrl = URL.createObjectURL(blob);
        setSource(objectUrl);
      })
      .catch(() => {
        if (!stopped) setSource(null);
      });
    return () => {
      stopped = true;
      controller.abort();
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [url, token]);
  return source;
}

/**
 * A player's picture: their photo when they have one, otherwise (or if the photo
 * fails to load or has expired) initials on a colour derived from their ID. The
 * photo fills the box with a fixed aspect, so loading never shifts the layout.
 * Shared by the live board, the roster, the upload preview and the recap.
 */
export function PlayerAvatar({ id, name, photoUrl, token, className = "" }) {
  const source = useImageSource(photoUrl, token);
  const [broken, setBroken] = useState(false);
  useEffect(() => setBroken(false), [source]);
  return (
    <span className={`avatar ${className}`} style={{ "--hue": avatarHue(id) }}>
      <span className="avatar-initials" aria-hidden="true">
        {initials(name)}
      </span>
      {source && !broken && (
        <img
          className="avatar-photo"
          src={source}
          alt={name}
          loading="lazy"
          decoding="async"
          onError={() => setBroken(true)}
        />
      )}
    </span>
  );
}
