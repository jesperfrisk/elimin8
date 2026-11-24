import React from "react";

export function Home({ saved }) {
  return (
    <>
      <section className="hero">
        <p className="eyebrow">The game starts with your people</p>
        <h1>
          Gather your crew.
          <br />
          <span>Set the game in motion.</span>
        </h1>
        <p className="intro">
          Create a lobby, invite your players, and bring everyone together. Your
          next game starts here.
        </p>
        <div className="actions">
          <a className="button" href="#/organizer">
            Create a game ↗
          </a>
          <a className="button secondary" href="#/player">
            Join a game →
          </a>
        </div>
      </section>
      <div className="feature-grid">
        <article className="panel">
          <span className="step">01 / ORGANIZE</span>
          <h2>Your game. Your roster.</h2>
          <p>
            Add your players and give each one their own private entry code.
          </p>
        </article>
        <article className="panel">
          <span className="step">02 / GATHER</span>
          <h2>Watch the lobby fill.</h2>
          <p>
            See who has joined as it happens. Start when your crew is ready.
          </p>
        </article>
        <article className="panel">
          <span className="step">03 / BEGIN</span>
          <h2>Everyone in? Let’s go.</h2>
          <p>
            Only players who joined the lobby enter the game when it starts.
          </p>
        </article>
      </div>
      <SavedGames saved={saved} />
    </>
  );
}

export function SavedGames({ saved, role }) {
  const games = saved.filter((a) => !role || a.role === role);
  if (!games.length) return null;
  return (
    <section className="saved">
      <h2>Return to a game</h2>
      <p className="muted">
        Access remembered in this browser tab. Your private code works on other
        devices.
      </p>
      <div className="game-links">
        {games.map((a) => (
          <a
            className="panel game-link"
            key={`${a.role}/${a.game.id}`}
            href={`#/${a.role}/${a.game.id}`}
          >
            <span>
              <strong>{a.game.name}</strong>
              <small>
                {a.role === "organizer" ? "Organizer" : "Player"} ·{" "}
                {a.game.invitationCode}
              </small>
            </span>
            <span>↗</span>
          </a>
        ))}
      </div>
    </section>
  );
}
