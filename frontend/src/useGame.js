import { useEffect, useRef, useState } from "react";
import mqtt from "mqtt";
import { request } from "./api";

export function useGame(access) {
  const [game, setGame] = useState({
    ...access.game,
    joinedCount: 0,
    invitedCount: 0,
    participantCount: 0,
  });
  const [connection, setConnection] = useState("Connecting live updates…");
  const [error, setError] = useState("");
  const refreshRef = useRef(async () => {});
  useEffect(() => {
    let stopped = false,
      client,
      running = false,
      again = false,
      retryTimer;
    let retryDelay = 1000;
    const abort = new AbortController();
    async function refresh() {
      if (stopped) return;
      if (running) {
        again = true;
        return;
      }
      running = true;
      try {
        do {
          again = false;
          const updated = await request(`/api/games/${access.game.id}`, {
            token: access.token,
            signal: abort.signal,
          });
          if (!stopped) {
            setGame((current) =>
              updated.revision >= current.revision ? updated : current,
            );
            setError("");
          }
        } while (again && !stopped);
        retryDelay = 1000;
        clearTimeout(retryTimer);
      } catch (e) {
        if (stopped) return;
        setError(e.message);
        if (e.status === 401 || e.status === 403) {
          client?.end(true);
          setConnection("Access expired · enter your code again");
        } else {
          clearTimeout(retryTimer);
          retryTimer = setTimeout(() => refresh().catch(() => {}), retryDelay);
          retryDelay = Math.min(retryDelay * 2, 30000);
        }
        throw e;
      } finally {
        running = false;
      }
    }
    // Retry a failed synchronization only. No periodic state requests.
    const safelyRefresh = () => refresh().catch(() => {});
    refreshRef.current = refresh;
    const ws = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/mqtt`;
    client = mqtt.connect(ws, {
      username: access.token,
      // getRandomValues also works over plain HTTP on a LAN; randomUUID requires
      // a secure context and would prevent phones from connecting to MQTT.
      clientId: access.clientPrefix + Array.from(
        crypto.getRandomValues(new Uint8Array(16)),
        byte => byte.toString(16).padStart(2, "0"),
      ).join(""),
      clean: true,
      protocolVersion: 4,
      keepalive: 30,
      connectTimeout: 10000,
      reconnectPeriod: 2000,
      resubscribe: false,
    });
    client.on("connect", () => {
      if (stopped) return;
      client.subscribe(access.topic, { qos: 1 }, (err, granted) => {
        if (stopped) return;
        if (err || granted?.some((g) => g.qos === 128)) {
          setError("Live update access denied. Enter your code again.");
          client.end(true);
          return;
        }
        setConnection("Live updates connected");
        safelyRefresh();
      });
    });
    client.on("message", (topic, payload) => {
      if (topic !== access.topic) return;
      try {
        const event = JSON.parse(payload.toString());
        if (event.gameId === access.game.id && event.type === "game.updated")
          safelyRefresh();
      } catch {
        /* Events cannot change game state directly. */
      }
    });
    client.on("offline", () => {
      if (!stopped) {
        setConnection("Disconnected · reconnecting…");
        safelyRefresh();
      }
    });
    client.on(
      "reconnect",
      () => !stopped && setConnection("Reconnecting live updates…"),
    );
    client.on(
      "error",
      () => !stopped && setConnection("Live updates unavailable · retrying…"),
    );
    safelyRefresh();
    const expiryTimer = setTimeout(
      () => {
        if (!stopped) {
          client.end(true);
          setConnection("Access expired");
          setError("Your access has expired. Enter your private code again.");
        }
      },
      Math.max(0, new Date(access.expiresAt).getTime() - Date.now()),
    );
    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      clearTimeout(expiryTimer);
      abort.abort();
      client.end(true);
    };
  }, [access.token]);
  return { game, connection, error, refresh: () => refreshRef.current() };
}
