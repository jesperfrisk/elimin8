import React from "react";
import { useGame } from "./useGame";
import { CopyButton, formatDate } from "./common";
import { OrganizerView } from "./OrganizerView";
import { PlayerView } from "./PlayerView";
import { PanelSlot, lobbyPanels, inGamePanels, gameOverPanels } from "./slots";

export function GamePage({ access, run, busy, secret, hideSecret, onForget }) {
  const { game, connection, error, refresh } = useGame(access);
  const organizer = access.role === "organizer";
  const lobby = game.status === "lobby";
  const active = game.status === "active";
  const action = (work) =>
    run(async () => {
      await work();
      await refresh();
    });
  if (!game.createdAt) {
    return (
      <section className="panel" aria-live="polite">
        <h1>Opening your game…</h1>
        <p>{error || "Loading the latest game state."}</p>
        {error && <a href={`#/${access.role}`}>Enter your code again</a>}
      </section>
    );
  }
  return (
    <>
      <div className="breadcrumbs">
        <a href={`#/${access.role}`}>
          ← {organizer ? "Organizer desk" : "Your games"}
        </a>
        <button className="text-button" onClick={onForget}>
          Forget access in this tab
        </button>
      </div>
      {error && (
        <div className="alert" role="alert">
          {error}{" "}
          <button className="secondary compact" onClick={() => run(refresh)}>
            Reconnect
          </button>
          <a href={`#/${access.role}`}>Enter your code again</a>
        </div>
      )}
      <div className="game-heading">
        <div>
          <p className="eyebrow">
            {organizer
              ? "Organizer desk"
              : `Playing as ${game.me?.name || "player"}`}
          </p>
          <h1>{game.name}</h1>
          {game.description && (
            <p className="description">{game.description}</p>
          )}
        </div>
        <div className="game-signals">
          <span className={`badge ${game.status}`}>
            {lobby ? "Lobby open" : active ? "Game active" : "Game ended"}
          </span>
          <span className="connection" role="status">
            <i
              className={
                connection === "Live updates connected" ? "online" : ""
              }
            />
            {connection}
          </span>
        </div>
      </div>
      {secret && (
        <section className="secret-panel">
          <h2>Save your organizer management code</h2>
          <p>
            This code is shown once. Keep it private—you’ll need it to manage
            the game from another browser.
          </p>
          <code className="management-code">{secret}</code>
          <div className="actions">
            <CopyButton
              text={`Game: ${game.invitationCode}\nOrganizer management code: ${secret}`}
              label="Copy organizer credentials"
            />
            <button className="text-button" onClick={hideSecret}>
              I’ve saved it · hide
            </button>
          </div>
        </section>
      )}
      <div className="stats">
        <div>
          <span>{lobby ? "Joined the lobby" : "Game participants"}</span>
          <strong>
            {lobby ? game.joinedCount : game.participantCount}
            <small>{lobby ? ` / ${game.invitedCount}` : ""}</small>
          </strong>
        </div>
        <div>
          <span>Game invitation code</span>
          <div className="invitation">
            <code>{game.invitationCode}</code>
            <CopyButton text={game.invitationCode} />
          </div>
        </div>
        <div>
          <span>
            {lobby ? "Ready when you are" : active ? "Started" : "Ended"}
          </span>
          <strong className="stat-text">
            {lobby
              ? "Waiting in the lobby"
              : formatDate(active ? game.startedAt : game.endedAt)}
          </strong>
        </div>
      </div>
      <div className="board-link">
        <span>Show this game on a TV:</span>
        <a href={`#/board/${game.id}`} target="_blank" rel="noreferrer">Open the live board ↗</a>
        <CopyButton text={`${location.origin}/#/board/${game.id}`} label="Copy board link" />
      </div>
      {organizer ? (
        <OrganizerView
          game={game}
          access={access}
          run={run}
          busy={busy}
          action={action}
          refresh={refresh}
        />
      ) : (
        <PlayerView game={game} access={access} action={action} busy={busy} />
      )}
      <PanelSlot
        panels={lobby ? lobbyPanels : active ? inGamePanels : gameOverPanels}
        game={game}
        access={access}
        action={action}
        busy={busy}
      />
    </>
  );
}
