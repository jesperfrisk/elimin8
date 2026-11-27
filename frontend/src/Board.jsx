import React, { useEffect, useRef, useState, useSyncExternalStore } from "react";
import mqtt from "mqtt";
import { request } from "./api";
import { formatRemaining, useCountdown } from "./Timer";
import { fitGrid, initials, avatarHue, statusLabel } from "./boardLayout";
import { diffNewlyOut, momentDuration, advanceRun, EMPTY_RUN } from "./boardMotion";
import { getBoardSound } from "./boardSound";
import "./board.css";

// The public live board at #/board/<gameId>: read-only, no login. It calls only
// the public endpoint (GET /api/board/{id}) and subscribes anonymously to the
// board topic; it never reads stored access tokens or private game state.

function useBoard(gameId) {
  const [board, setBoard] = useState(null);
  const [missing, setMissing] = useState(false);
  const [failed, setFailed] = useState(false);
  const [live, setLive] = useState("connecting");
  // Changes that arrived over a live update and should get their moment.
  const [feed, setFeed] = useState({ seq: 0, items: [] });
  useEffect(() => {
    let stopped = false,
      running = false,
      again = false,
      client,
      retryTimer,
      previous = null,
      nextCause = "catchup",
      retryDelay = 1000;
    const abort = new AbortController();
    // cause "live": an MQTT message said the board changed, so changes since the
    // last snapshot are animated. Anything else (first load, subscribe or
    // reconnect catch-up, waking a sleeping tab, a retry) renders the final
    // state silently.
    async function refresh(cause = "catchup") {
      if (stopped) return;
      if (cause === "live") nextCause = "live";
      if (running) {
        again = true;
        return;
      }
      running = true;
      try {
        do {
          again = false;
          const thisCause = nextCause;
          nextCause = "catchup";
          const next = await request(`/api/board/${encodeURIComponent(gameId)}`, {
            signal: abort.signal,
          });
          if (stopped) return;
          // A hidden tab can't show a moment, and replaying a backlog when it
          // returns would be wrong, so hidden counts as catch-up.
          if (thisCause === "live" && previous && document.visibilityState === "visible") {
            const items = diffNewlyOut(previous.players, next.players);
            if (previous.status === "active" && next.status === "ended") items.push({ id: "finale", kind: "finale" });
            if (items.length) setFeed((f) => ({ seq: f.seq + 1, items }));
          }
          previous = next;
          setBoard(next);
          setMissing(false);
          setFailed(false);
        } while (again && !stopped);
        retryDelay = 1000;
        clearTimeout(retryTimer);
      } catch (e) {
        if (stopped || e.name === "AbortError") return;
        if (e.status === 404) {
          setMissing(true);
          client?.end(true);
          return;
        }
        // Retry a failed synchronization only. No periodic polling.
        setFailed(true);
        clearTimeout(retryTimer);
        retryTimer = setTimeout(() => safelyRefresh(), retryDelay);
        retryDelay = Math.min(retryDelay * 2, 30000);
      } finally {
        running = false;
      }
    }
    const safelyRefresh = (cause) => refresh(cause).catch(() => {});
    const topic = `boards/${gameId}/updates`;
    const ws = `${location.protocol === "https:" ? "wss:" : "ws:"}//${location.host}/api/mqtt`;
    client = mqtt.connect(ws, {
      // Anonymous, read-only: no username, and a client id the broker
      // recognises as a board viewer. getRandomValues also works over plain
      // HTTP on a LAN, unlike randomUUID.
      clientId:
        "board-" +
        Array.from(crypto.getRandomValues(new Uint8Array(12)), (b) => b.toString(16).padStart(2, "0")).join(""),
      clean: true,
      protocolVersion: 4,
      keepalive: 30,
      connectTimeout: 10000,
      reconnectPeriod: 2000,
      resubscribe: false,
    });
    client.on("connect", () => {
      if (stopped) return;
      client.subscribe(topic, { qos: 1 }, (err, granted) => {
        if (stopped) return;
        if (err || granted?.some((g) => g.qos === 128)) {
          setLive("unavailable");
          return;
        }
        setLive("connected");
        // Subscribed: fetch now so nothing between the first load and the
        // subscription (or during a reconnect) is missed.
        safelyRefresh();
      });
    });
    client.on("message", (t, payload) => {
      if (t !== topic) return;
      try {
        const event = JSON.parse(payload.toString());
        if (event.gameId === gameId && event.type === "board.updated") safelyRefresh("live");
      } catch {
        /* A message only says "refetch"; it carries no board data. */
      }
    });
    const reconnecting = () => !stopped && setLive("reconnecting");
    client.on("offline", reconnecting);
    client.on("reconnect", reconnecting);
    client.on("error", reconnecting);
    // A TV or phone that slept may have missed messages: catch up on wake.
    const onVisible = () => document.visibilityState === "visible" && safelyRefresh();
    document.addEventListener("visibilitychange", onVisible);
    safelyRefresh();
    return () => {
      stopped = true;
      clearTimeout(retryTimer);
      abort.abort();
      document.removeEventListener("visibilitychange", onVisible);
      client.end(true);
    };
  }, [gameId]);
  return { board, missing, failed, live, feed };
}

const without = (object, key) => {
  const { [key]: _removed, ...rest } = object;
  return rest;
};

// Plays the live changes one after another, so each elimination is seen. A
// player's tile keeps showing "active" until its moment starts, and the winner
// flourish waits for the last elimination. A burst speeds up (see boardMotion).
function useMoments(feed) {
  const sound = getBoardSound();
  const [pending, setPending] = useState({ ids: {}, finale: false });
  const [active, setActive] = useState(null);
  const queue = useRef([]);
  const running = useRef(false);
  const run = useRef(EMPTY_RUN);
  const timer = useRef(null);
  const seen = useRef(0);
  useEffect(() => () => clearTimeout(timer.current), []);
  function next() {
    const item = queue.current.shift();
    if (!item) {
      running.current = false;
      run.current = EMPTY_RUN;
      setActive(null);
      return;
    }
    running.current = true;
    const reduced = !!window.matchMedia?.("(prefers-reduced-motion: reduce)").matches;
    const ms = momentDuration(item.kind, run.current, reduced);
    run.current = advanceRun(run.current, item.kind, ms);
    setPending((p) => (item.kind === "finale" ? { ...p, finale: false } : { ...p, ids: without(p.ids, item.id) }));
    setActive({ ...item, ms });
    // Eliminations get the gong, the end of the game the chord; withdrawals are silent.
    if (item.kind === "eliminated") sound.play("elimination", { id: item.id });
    else if (item.kind === "finale") sound.play("winner");
    timer.current = setTimeout(next, ms);
  }
  useEffect(() => {
    if (!feed.seq || seen.current === feed.seq) return;
    seen.current = feed.seq;
    queue.current.push(...feed.items);
    setPending((p) => ({
      ids: { ...p.ids, ...Object.fromEntries(feed.items.filter((i) => i.kind !== "finale").map((i) => [i.id, i.kind])) },
      finale: p.finale || feed.items.some((i) => i.kind === "finale"),
    }));
    if (!running.current) next();
  }, [feed.seq]);
  return { pending, active };
}

// The "Tap to enable sound" gate (browsers block audio until a tap) and the mute toggle.
function useSoundControls(sound) {
  const state = useSyncExternalStore(sound.subscribe, sound.getState);
  const [dismissed, setDismissed] = useState(false);
  if (!state.supported) return { gate: null, toggle: null };
  return {
    gate:
      !state.unlocked && !dismissed ? (
        <section className="board-gate" aria-label="Sound">
          <button className="board-gate-button" onClick={() => sound.enable()}>
            Tap to enable sound
          </button>
          <button className="text-button" onClick={() => setDismissed(true)}>
            Keep silent
          </button>
        </section>
      ) : null,
    toggle: state.unlocked ? (
      <button className="secondary" aria-pressed={!state.muted} onClick={() => sound.toggleMute()}>
        {state.muted ? "Sound off" : "Sound on"}
      </button>
    ) : null,
  };
}

// Measures the area available to the grid so tiles can be sized to fit it.
function useBox(ref) {
  const [box, setBox] = useState({ width: 0, height: 0 });
  useEffect(() => {
    const element = ref.current;
    if (!element) return undefined;
    const observer = new ResizeObserver(([entry]) => {
      const { width, height } = entry.contentRect;
      setBox((current) =>
        Math.round(current.width) === Math.round(width) && Math.round(current.height) === Math.round(height)
          ? current
          : { width, height },
      );
    });
    observer.observe(element);
    return () => observer.disconnect();
  }, [ref]);
  return box;
}

function useFullscreen() {
  const [active, setActive] = useState(!!document.fullscreenElement);
  useEffect(() => {
    const change = () => setActive(!!document.fullscreenElement);
    document.addEventListener("fullscreenchange", change);
    return () => document.removeEventListener("fullscreenchange", change);
  }, []);
  const supported = !!document.fullscreenEnabled;
  const toggle = async () => {
    try {
      if (document.fullscreenElement) await document.exitFullscreen();
      else await document.documentElement.requestFullscreen();
    } catch {
      /* Fullscreen can be refused (iframes, some TV browsers); nothing to do. */
    }
  };
  return { active, supported, toggle };
}

function LiveBadge({ live, failed }) {
  const ok = live === "connected" && !failed;
  const text = ok
    ? "Live"
    : live === "unavailable"
      ? "Live updates unavailable"
      : "Reconnecting…";
  return (
    <span className={`board-live ${ok ? "ok" : "warn"}`} role="status">
      <i aria-hidden="true" />
      {text}
    </span>
  );
}

function BoardTimer({ board }) {
  const timer = board.timer;
  const running = board.status === "active" && timer?.endsAt && timer.remainingSeconds != null;
  const left = useCountdown(timer?.remainingSeconds, timer?.endsAt);
  if (running) {
    return (
      <div className="board-stat board-timer">
        <span>Time left</span>
        <strong role="timer" data-testid="board-countdown">{formatRemaining(left)}</strong>
      </div>
    );
  }
  if (board.status === "lobby" && timer?.minutes) {
    return (
      <div className="board-stat board-timer">
        <span>Time limit</span>
        <strong>{timer.minutes} min</strong>
      </div>
    );
  }
  if (board.status === "ended" && timer?.endReason === "timer") {
    return (
      <div className="board-stat board-timer">
        <span>Timer</span>
        <strong>Time’s up</strong>
      </div>
    );
  }
  return null;
}

function Tile({ player, ended, moment }) {
  const label = statusLabel(player.status, ended);
  return (
    <li
      className={`tile ${player.status}${ended && player.status === "active" ? " survivor" : ""}${moment ? ` moment-${moment.kind}` : ""}`}
      style={{ "--hue": avatarHue(player.id), ...(moment ? { "--moment": `${moment.ms}ms` } : null) }}
      aria-label={`${player.name}, ${label}`}
      title={player.name}
      data-status={player.status}
    >
      {/* The avatar slot: the photo ticket fills .tile-face; initials are the fallback. */}
      <div className="tile-art" aria-hidden="true">
        <div className="tile-face">
          <span className="tile-initials">{initials(player.name)}</span>
        </div>
      </div>
      <div className="tile-caption" aria-hidden="true">
        <span className="tile-name">{player.name}</span>
        <span className="tile-status">{label}</span>
      </div>
    </li>
  );
}

// Slot for the post-game recap (ticket #42 replaces the body of this banner).
function EndedBanner({ board }) {
  const survivors = board.players.filter((p) => p.status === "active").length;
  return (
    <section className="board-ended" aria-label="Game over">
      <strong>Game over</strong>
      <span>
        {survivors} survivor{survivors === 1 ? "" : "s"}
      </span>
    </section>
  );
}

function Grid({ board, active }) {
  const mainRef = useRef(null);
  const box = useBox(mainRef);
  const gap = box.width > 1800 ? 20 : box.width > 900 ? 14 : 8;
  const fit = fitGrid(board.players.length, box.width, box.height, { gap });
  const ended = board.status === "ended";
  return (
    <main className={`board-main${fit.scroll ? " scroll" : ""}`} ref={mainRef}>
      {board.status === "lobby" ? (
        <div className="board-waiting" role="status">
          <strong>Waiting for the game to start</strong>
          <span>The players appear here as soon as the organizer starts.</span>
        </div>
      ) : (
        <ul
          className="board-grid"
          aria-label="Players"
          style={{
            gridTemplateColumns: `repeat(${fit.columns}, ${fit.tileWidth}px)`,
            "--tile": `${fit.tileWidth}px`,
            gap: `${gap}px`,
            visibility: box.width ? "visible" : "hidden",
          }}
          data-columns={fit.columns}
          data-rows={fit.rows}
          data-scroll={fit.scroll}
        >
          {board.players.map((p) => {
            // Which tile is in its moment: the one leaving, or the survivors at the finale.
            const moment =
              active && (active.id === p.id || (active.kind === "finale" && ended && p.status === "active")) ? active : null;
            return <Tile key={p.id} player={p} ended={ended} moment={moment} />;
          })}
        </ul>
      )}
    </main>
  );
}

export function Board({ gameId }) {
  const { board, missing, failed, live, feed } = useBoard(gameId);
  const fullscreen = useFullscreen();
  const { pending, active } = useMoments(feed);
  const sound = getBoardSound();
  const soundControls = useSoundControls(sound);
  useEffect(() => {
    if (board?.name) document.title = `${board.name} · Elimin8 board`;
    return () => {
      document.title = "Elimin8";
    };
  }, [board?.name]);
  if (missing) {
    return (
      <div className="board board-center">
        <h1>Board not found</h1>
        <p>This link doesn’t match a game. Check the link with the organizer.</p>
        <a className="button" href="#/">Go to Elimin8</a>
      </div>
    );
  }
  if (!board) {
    return (
      <div className="board board-center" role="status">
        <h1>{failed ? "Can’t reach the game" : "Loading the board…"}</h1>
        {failed && <p>Retrying automatically.</p>}
      </div>
    );
  }
  // What the screen shows: a player whose moment hasn't started yet still looks
  // active, and the ended state waits for the winner flourish.
  const players = board.players.map((p) => (pending.ids[p.id] ? { ...p, status: "active" } : p));
  const ended = board.status === "ended" && !pending.finale;
  const view = { ...board, players, status: board.status === "ended" && !ended ? "active" : board.status };
  const shownActive = players.filter((p) => p.status === "active").length;
  return (
    <div className="board" data-state={view.status}>
      <header className="board-header">
        <div className="board-title">
          <span className="board-brand">elimin<b>8</b></span>
          <h1>{board.name}</h1>
        </div>
        <div className="board-stats">
          {view.status !== "lobby" && (
            <div className="board-stat board-count">
              <span>{view.status === "ended" ? "Survivors" : "Active"}</span>
              {/* Re-keyed on every change so the count visibly ticks down during a moment. */}
              <strong key={shownActive} className={active ? "tick" : undefined} data-testid="board-active">
                {shownActive}
                <small> / {players.length}</small>
              </strong>
            </div>
          )}
          <BoardTimer board={view} />
        </div>
        <div className="board-tools">
          <LiveBadge live={live} failed={failed} />
          {soundControls.toggle}
          {fullscreen.supported && (
            <button className="secondary" onClick={fullscreen.toggle}>
              {fullscreen.active ? "Exit fullscreen" : "Fullscreen"}
            </button>
          )}
        </div>
      </header>
      {soundControls.gate}
      {view.status === "ended" && <EndedBanner board={view} />}
      <Grid board={view} active={active} />
    </div>
  );
}
