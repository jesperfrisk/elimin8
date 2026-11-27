import React from "react";
import { Eliminations } from "./Eliminations";
import { EliminationNotice } from "./EliminationNotice";

export function PlayerView({ game, access, action, busy }) {
  const lobby = game.status === "lobby";
  const active = game.status === "active";
  return (
    <>
      <section className="panel player-card">
        <div className="player-symbol">
          {lobby ? "◎" : active ? "↗" : "✓"}
        </div>
        <p className="eyebrow">{game.me?.name}, you’re in</p>
        <h2>
          {lobby
            ? "Your place in the lobby is confirmed."
            : game.me?.status === "withdrawn"
              ? "You have withdrawn."
            : game.me?.eliminatedAt
              ? "You have been eliminated."
            : active
              ? "The game is on."
              : "This game has ended."}
        </h2>
        <p>
          {lobby
            ? "Your organizer will start the game when everyone is ready. You can close this page and return with your codes—your place stays joined."
            : game.me?.status === "withdrawn"
              ? "Your withdrawal is confirmed and your hunter has taken over your victim and mission. You can return here to see the game outcome."
            : game.me?.eliminatedAt
              ? "Your elimination is confirmed. You can return here to see the game outcome."
            : active
              ? "The game is underway. Keep your assignments secret."
              : "Thanks for being part of it. You can return to your games or join another lobby."}
        </p>
        {!lobby && game.me?.victim && (
          <section className="private-mission" aria-label="Your victim">
            <h3>Your victim</h3>
            <p>{game.me.victim.name}</p>
            <small>Complete your mission with this player.</small>
          </section>
        )}
        {!lobby && game.me?.mission && (
          <section className="private-mission" aria-label="Your mission">
            <h3>Your mission</h3>
            <p>{game.me.mission}</p>
            <small>Keep this mission to yourself.</small>
          </section>
        )}
        {lobby && (
          <p className="muted">
            This page updates automatically when your organizer starts or ends
            the game.
          </p>
        )}
      </section>
      <EliminationNotice game={game} />
      <Eliminations game={game} access={access} action={action} busy={busy} />
    </>
  );
}
