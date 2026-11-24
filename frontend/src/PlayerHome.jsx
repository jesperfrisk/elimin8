import React from "react";
import { PageHeading } from "./common";
import { SavedGames } from "./Home";
import { LoginForm } from "./LoginForm";

export function PlayerHome({ run, busy, admitted, saved }) {
  return (
    <>
      <PageHeading
        eyebrow="Player entrance"
        title="Your place is waiting."
        text="Get your game invitation code and private player code from the organizer."
      />
      <section className="panel narrow">
        <h2>Join or return to a game</h2>
        <p>
          Your organizer has already added your name. Your private code opens
          your place in the game.
        </p>
        <LoginForm role="player" run={run} busy={busy} admitted={admitted} />
      </section>
      <SavedGames role="player" saved={saved} />
    </>
  );
}
