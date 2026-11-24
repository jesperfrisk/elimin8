import React from "react";
import { request } from "./api";
import { PageHeading } from "./common";
import { SavedGames } from "./Home";
import { LoginForm } from "./LoginForm";

export function OrganizerHome({ run, busy, admitted, saved }) {
  return (
    <>
      <PageHeading
        eyebrow="Organizer desk"
        title="Make room for a new game."
        text="Build your roster, share private codes, and start when everyone is ready."
      />
      <div className="two-columns">
        <section className="panel">
          <h2>Create a game</h2>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              const data = new FormData(e.currentTarget);
              run(async () =>
                admitted(
                  await request("/api/games", {
                    method: "POST",
                    body: {
                      name: data.get("name"),
                      description: data.get("description"),
                    },
                  }),
                ),
              );
            }}
          >
            <label>
              Game name
              <input
                name="name"
                required
                maxLength={100}
                placeholder="Friday night showdown"
              />
            </label>
            <label>
              Description (optional)
              <textarea
                name="description"
                maxLength={2000}
                rows={3}
                placeholder="Where, when, and anything your players need to know."
              />
            </label>
            <button disabled={busy}>
              {busy ? "Working…" : "Create game →"}
            </button>
          </form>
        </section>
        <section className="panel">
          <h2>Manage an existing game</h2>
          <p>
            Use the invitation code and the management code you received when
            creating the game.
          </p>
          <LoginForm
            role="organizer"
            run={run}
            busy={busy}
            admitted={admitted}
          />
        </section>
      </div>
      <SavedGames role="organizer" saved={saved} />
    </>
  );
}
