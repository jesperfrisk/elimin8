import React, { useEffect, useState } from "react";
import { request } from "./api";
import { formatOffset, recapWinners, rankStandings } from "./recapData";
import "./recap.css";

// Post-game recap. Everything comes from the server's recap payload
// (GET /api/games/{id}/recap or the public GET /api/board/{id}/recap). The
// payload names subjects only, so this UI cannot show who eliminated whom.

const reasonText = {
  survival: "The game ended with a survival win.",
  timer: "Time ran out. The most confirmed kills wins.",
  organizer: "The organizer ended the game, so there is no winner.",
};

export function useRecap(path, token, enabled) {
  const [recap, setRecap] = useState(null);
  const [failed, setFailed] = useState(false);
  useEffect(() => {
    if (!enabled) return undefined;
    const controller = new AbortController();
    setFailed(false);
    request(path, { token, signal: controller.signal })
      .then((data) => {
        // Anything that is not a recap payload counts as a failed load.
        if (!data || !data.summary || !data.awards) throw new Error("not a recap");
        setRecap(data);
      })
      .catch(() => {
        if (!controller.signal.aborted) setFailed(true);
      });
    return () => controller.abort();
  }, [path, token, enabled]);
  return { recap, failed };
}

const step = (i) => ({ "--i": i });

function Hero({ recap }) {
  const { summary } = recap;
  const winners = recapWinners(recap);
  return (
    <section className="recap-hero" aria-labelledby="recap-hero-title" style={step(0)}>
      <h2 id="recap-hero-title">
        {winners.length === 0 ? "No winner" : winners.length > 1 ? "Winners" : "Winner"}
      </h2>
      {winners.length > 0 && (
        <ul className="recap-winners">
          {winners.map((w) => (
            <li key={w.id}>
              {/* Avatar slot: initials stand in until the shared avatar component lands. */}
              <span className="recap-avatar" aria-hidden="true">{(w.name || "?").slice(0, 2).toUpperCase()}</span>
              <strong>{w.name}</strong>
            </li>
          ))}
        </ul>
      )}
      <p className="recap-reason">{reasonText[summary.reason] || ""}</p>
      {summary.tie && (
        <p className="recap-tie" role="status">
          <strong>It’s a tie.</strong> The top score is shared.
        </p>
      )}
    </section>
  );
}

function Leaderboard({ recap }) {
  const rows = rankStandings(recap.leaderboard || []);
  const max = Math.max(1, ...rows.map((r) => r.kills));
  return (
    <section className="recap-card" aria-labelledby="recap-lb-title" style={step(1)}>
      <h3 id="recap-lb-title">Leaderboard</h3>
      {rows.length === 0 ? (
        <p className="recap-empty">No standings for this game.</p>
      ) : rows.every((r) => r.kills === 0) ? (
        <p className="recap-empty">Nobody scored a confirmed kill.</p>
      ) : (
        <ol className="recap-bars">
          {rows.map((r) => (
            <li key={r.key}>
              <span className="recap-rank">
                {r.tied ? "=" : ""}
                {r.rank}
              </span>
              <span className="recap-name">{r.name}</span>
              <span className="recap-bar" aria-hidden="true">
                <i style={{ width: `${(r.kills / max) * 100}%` }} />
              </span>
              <span className="recap-count">
                {r.kills} kill{r.kills === 1 ? "" : "s"}
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function Timeline({ recap }) {
  const events = recap.timeline || [];
  const total = Math.max(1, recap.summary.durationSeconds);
  return (
    <section className="recap-card recap-wide" aria-labelledby="recap-tl-title" style={step(2)}>
      <h3 id="recap-tl-title">Elimination timeline</h3>
      {events.length === 0 ? (
        <p className="recap-empty">Nobody was eliminated or withdrew.</p>
      ) : (
        <ol className="recap-timeline">
          {events.map((e, i) => (
            <li key={`${e.playerId}/${i}`} className={e.event}>
              <time className="recap-time">{formatOffset(e.atSeconds)}</time>
              <span className="recap-name">{e.name}</span>
              <span className="recap-event">{e.event === "withdrew" ? "Withdrew" : "Eliminated"}</span>
              <span className="recap-track" aria-hidden="true">
                <i style={{ left: `${Math.min(100, (e.atSeconds / total) * 100)}%` }} />
              </span>
            </li>
          ))}
        </ol>
      )}
    </section>
  );
}

function Awards({ recap }) {
  const a = recap.awards;
  const cards = [];
  if (a.longestSurvivor) cards.push(["Longest survivor", a.longestSurvivor.name, formatOffset(a.longestSurvivor.atSeconds)]);
  if (a.firstElimination) cards.push(["First elimination", a.firstElimination.name, formatOffset(a.firstElimination.atSeconds)]);
  if (a.fastestElimination) cards.push(["Fastest elimination", a.fastestElimination.name, formatOffset(a.fastestElimination.atSeconds)]);
  if (a.mostKills?.length) {
    cards.push([
      "Most kills",
      a.mostKills.map((s) => s.name).join(", "),
      `${a.mostKills[0].kills} kill${a.mostKills[0].kills === 1 ? "" : "s"}${a.mostKills.length > 1 ? " each" : ""}`,
    ]);
  }
  if (a.lastElimination) cards.push(["Last elimination", a.lastElimination.name, formatOffset(a.lastElimination.atSeconds)]);
  return (
    <section className="recap-wide" aria-labelledby="recap-aw-title" style={step(3)}>
      <h3 id="recap-aw-title">Awards</h3>
      {cards.length === 0 ? (
        <p className="recap-empty">No awards were earned in this game.</p>
      ) : (
        <ul className="recap-awards">
          {cards.map(([title, who, detail]) => (
            <li key={title} className="recap-card">
              <span className="recap-award-title">{title}</span>
              <strong>{who}</strong>
              <span className="recap-award-detail">{detail}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function Summary({ recap }) {
  const s = recap.summary;
  const items = [
    ["Duration", formatOffset(s.durationSeconds)],
    ["Players", s.players],
    ["Eliminations", s.eliminations],
    ["Withdrawals", s.withdrawals],
    ["Declined reports", s.declinedReports],
  ];
  return (
    <section className="recap-card" aria-labelledby="recap-sum-title" style={step(1)}>
      <h3 id="recap-sum-title">Game summary</h3>
      <dl className="recap-summary">
        {items.map(([label, value]) => (
          <div key={label}>
            <dt>{label}</dt>
            <dd>{value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

function DataTables({ recap }) {
  return (
    <details className="recap-wide recap-tables">
      <summary>Show the recap as tables</summary>
      <div className="table-scroll">
        <table>
          <caption>Leaderboard</caption>
          <thead>
            <tr><th>Name</th><th>Confirmed kills</th></tr>
          </thead>
          <tbody>
            {(recap.leaderboard || []).map((r, i) => (
              <tr key={i}><td>{r.name}</td><td>{r.kills}</td></tr>
            ))}
          </tbody>
        </table>
        <table>
          <caption>Eliminations and withdrawals</caption>
          <thead>
            <tr><th>Name</th><th>Event</th><th>Time</th></tr>
          </thead>
          <tbody>
            {(recap.timeline || []).map((e, i) => (
              <tr key={i}><td>{e.name}</td><td>{e.event === "withdrew" ? "Withdrew" : "Eliminated"}</td><td>{formatOffset(e.atSeconds)}</td></tr>
            ))}
          </tbody>
        </table>
      </div>
    </details>
  );
}

export function RecapView({ recap, variant }) {
  return (
    <div className={`recap${variant ? ` recap-${variant}` : ""}`} aria-label="Game recap" role="region" data-testid="recap">
      <Hero recap={recap} />
      <div className="recap-grid">
        <Leaderboard recap={recap} />
        <Summary recap={recap} />
        <Timeline recap={recap} />
        <Awards recap={recap} />
        <DataTables recap={recap} />
      </div>
    </div>
  );
}

// Game page panel (slots.jsx): the same recap for the organizer and players.
export function Recap({ game, access }) {
  const ended = game.status === "ended";
  const { recap, failed } = useRecap(`/api/games/${game.id}/recap`, access.token, ended);
  if (!ended) return null;
  if (failed)
    return (
      <section className="panel" aria-label="Game recap">
        <p role="status">The recap could not be loaded. Refresh the page to try again.</p>
      </section>
    );
  if (!recap) return null;
  return (
    <section className="panel recap-panel">
      <RecapView recap={recap} variant="panel" />
    </section>
  );
}
