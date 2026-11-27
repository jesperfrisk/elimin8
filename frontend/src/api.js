const key = "elimin8.access.v1";
export function readAccess() {
  try {
    const value = JSON.parse(sessionStorage.getItem(key) || "[]");
    return Array.isArray(value) ? value : [];
  } catch {
    return [];
  }
}
export function rememberAccess(access) {
  // Private codes and the fetched roster are never persisted in the browser.
  const { token, expiresAt, role, clientPrefix, topic, game } = access;
  const entry = {
    token,
    expiresAt,
    role,
    clientPrefix,
    topic,
    game: {
      id: game.id,
      name: game.name,
      invitationCode: game.invitationCode,
      status: game.status,
      revision: 0,
    },
  };
  const previous = readAccess().filter(
    (a) => !(a.game.id === game.id && a.role === role),
  );
  sessionStorage.setItem(key, JSON.stringify([entry, ...previous]));
}
export function forgetAccess(access) {
  sessionStorage.setItem(
    key,
    JSON.stringify(readAccess().filter((a) => a.token !== access.token)),
  );
}
export async function request(
  path,
  { method = "GET", token, body, signal } = {},
) {
  // FormData (photo uploads) is sent as is, so the browser sets the multipart boundary.
  const form = typeof FormData !== "undefined" && body instanceof FormData;
  const response = await fetch(path, {
    method,
    signal,
    headers: {
      ...(body && !form ? { "Content-Type": "application/json" } : {}),
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
    },
    body: form ? body : body ? JSON.stringify(body) : undefined,
  });
  const data = await response.json().catch(() => ({}));
  if (!response.ok) {
    const error = new Error(
      data.error || "Unable to reach the game service. Please try again.",
    );
    error.status = response.status;
    throw error;
  }
  return data;
}
