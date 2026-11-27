import { useEffect, useRef, useState } from "react";
import mqtt from "mqtt";
import { request } from "./api";
import { exchangeSession, sessionFailure, nextRenewalDelay, RETRY_MS } from "./rememberedGames";

// A player's open game keeps itself signed in: shortly before the admission ends, or
// when the server rejects it, the remembered cookie is exchanged for a fresh one
// (onRenew). If that sign-in is gone the page is told (onLost) and asks for the code.
export function useGame(access, { onRenew, onLost } = {}) {
  const [game, setGame] = useState({
    ...access.game,
    joinedCount: 0,
    invitedCount: 0,
    participantCount: 0,
  });
  const [connection, setConnection] = useState("Connecting live updates…");
  const [error, setError] = useState("");
  const refreshRef = useRef(async () => {});
  const callbacks = useRef({});
  callbacks.current = { onRenew, onLost };
  useEffect(() => {
    let stopped = false,
      client,
      running = false,
      again = false,
      retryTimer,
      renewTimer,
      renewing = false,
      renewFailures = 0,
      renewTimes = [];
    const player = access.role === "player";
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
        if (player && e.status === 401) {
          // The admission was rejected (expired, or revoked). Try the remembered sign-in once.
          client?.end(true);
          setConnection("Renewing access…");
          renew();
        } else if (e.status === 401 || e.status === 403) {
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
    // Exchange the remembered cookie for a fresh admission. The App stores it, which
    // changes access.token and restarts this effect (a new MQTT connection, same state).
    async function renew() {
      if (stopped || renewing || !player) return;
      // A renewed admission that is rejected again straight away would loop: give up.
      const now = Date.now();
      renewTimes = renewTimes.filter((t) => now - t < 60000);
      if (renewTimes.length >= 3) {
        callbacks.current.onLost?.();
        return;
      }
      renewing = true;
      try {
        renewTimes.push(now);
        const admission = await exchangeSession(access.game.id, abort.signal);
        if (stopped) return;
        renewFailures = 0;
        // Pass the replaced expiry so a barely longer admission (the 30-day limit) is not renewed in a loop.
        admission.previousExpiresAt = access.expiresAt;
        callbacks.current.onRenew?.(admission);
      } catch (e) {
        if (stopped || e.name === "AbortError") return;
        if (sessionFailure(e) === "ended") {
          callbacks.current.onLost?.();
        } else {
          // Offline or rate limited: say so and keep trying until it works or the sign-in ends.
          setConnection("Couldn’t renew access · retrying…");
          renewTimer = setTimeout(renew, RETRY_MS[Math.min(renewFailures, RETRY_MS.length - 1)]);
          renewFailures++;
        }
      } finally {
        renewing = false;
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
    if (player) {
      const delay = nextRenewalDelay(access.expiresAt, Date.now(), access.previousExpiresAt ?? null);
      if (delay !== null) renewTimer = setTimeout(renew, delay);
    }
    const expiryTimer = setTimeout(
      () => {
        if (stopped) return;
        if (player) {
          // Last chance: the remembered sign-in may still be good.
          client.end(true);
          setConnection("Renewing access…");
          renew();
          return;
        }
        client.end(true);
        setConnection("Access expired");
        setError("Your access has expired. Enter your private code again.");
      },
      Math.max(0, new Date(access.expiresAt).getTime() - Date.now()),
    );
    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      clearTimeout(renewTimer);
      clearTimeout(expiryTimer);
      abort.abort();
      client.end(true);
    };
  }, [access.token]);
  return { game, connection, error, refresh: () => refreshRef.current() };
}
