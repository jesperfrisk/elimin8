import React, { useEffect, useRef, useState } from "react";
import mqtt from "mqtt";
import { request } from "./api";
import { formatRemaining, useCountdown } from "./Timer";
import { fitGrid, initials, avatarHue, statusLabel } from "./boardLayout";
import "./board.css";

// The public live board at #/board/<gameId>: read-only, no login. It calls only
// the public endpoint (GET /api/board/{id}) and subscribes anonymously to the
// board topic; it never reads stored access tokens or private game state.

function useBoard(gameId) {
  const [board, setBoard] = useState(null);
  const [missing, setMissing] = useState(false);
  const [failed, setFailed] = useState(false);
  const [live, setLive] = useState("connecting");
  useEffect(() => {
    let stopped = false,
      running = false,
      again = false,
      client,
      retryTimer,
      retryDelay = 1000;
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
          const next = await request(`/api/board/${encodeURIComponent(gameId)}`, {
            signal: abort.signal,
          });
          if (stopped) return;
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
        retryTimer = setTimeout(safelyRefresh, retryDelay);
        retryDelay = Math.min(retryDelay * 2, 30000);
      } finally {
        running = false;
      }
    }
    const safelyRefresh = () => refresh().catch(() => {});
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
        if (event.gameId === gameId && event.type === "board.updated") safelyRefresh();
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
  return { board, missing, failed, live };
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

function Tile({ player, ended }) {
  const label = statusLabel(player.status, ended);
  const number = String(player.number).padStart(2, "0");
  return (
    <li
      className={`tile ${player.status}${ended && player.status === "active" ? " survivor" : ""}`}
      style={{ "--hue": avatarHue(player.id) }}
      aria-label={`${player.name}, number ${player.number}, ${label}`}
      title={player.name}
      data-status={player.status}
    >
      {/* The avatar slot: the photo ticket fills .tile-face; initials are the fallback. */}
      <div className="tile-art" aria-hidden="true">
        <div className="tile-face">
          <span className="tile-initials">{initials(player.name)}</span>
        </div>
      </div>
      <span className="tile-number" aria-hidden="true">{number}</span>
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

function Grid({ board }) {
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
          {board.players.map((p) => (
            <Tile key={p.id} player={p} ended={ended} />
          ))}
        </ul>
      )}
    </main>
  );
}

export function Board({ gameId }) {
  const { board, missing, failed, live } = useBoard(gameId);
  const fullscreen = useFullscreen();
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
  return (
    <div className="board" data-state={board.status}>
      <header className="board-header">
        <div className="board-title">
          <span className="board-brand">elimin<b>8</b></span>
          <h1>{board.name}</h1>
        </div>
        <div className="board-stats">
          {board.status !== "lobby" && (
            <div className="board-stat board-count">
              <span>{board.status === "ended" ? "Survivors" : "Active"}</span>
              <strong data-testid="board-active">
                {board.activeCount}
                <small> / {board.players.length}</small>
              </strong>
            </div>
          )}
          <BoardTimer board={board} />
        </div>
        <div className="board-tools">
          <LiveBadge live={live} failed={failed} />
          {fullscreen.supported && (
            <button className="secondary" onClick={fullscreen.toggle}>
              {fullscreen.active ? "Exit fullscreen" : "Fullscreen"}
            </button>
          )}
        </div>
      </header>
      {board.status === "ended" && <EndedBanner board={board} />}
      <Grid board={board} />
    </div>
  );
}
