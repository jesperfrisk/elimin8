import React, { useEffect, useState } from "react";
import { request } from "./api";

// In-game panel: a player can leave the game in two deliberate steps. The
// first call only asks the server for a short-lived confirmation token; the
// player is withdrawn only by the second, explicit call.
export function Withdrawal({ game, access, action, busy }) {
  const [pending, setPending] = useState(null);
  const [expired, setExpired] = useState(false);
  const expiresAt = pending?.expiresAt;
  useEffect(() => {
    if (!expiresAt) return undefined;
    const timer = setTimeout(
      () => {
        setPending(null);
        setExpired(true);
      },
      Math.max(0, new Date(expiresAt) - Date.now()),
    );
    return () => clearTimeout(timer);
  }, [expiresAt]);
  if (
    access.role !== "player" ||
    game.status !== "active" ||
    game.me?.status !== "active"
  )
    return null;
  const base = `/api/games/${game.id}/withdrawals`;
  return (
    <section className="panel withdrawal" aria-label="Withdrawal">
      {!pending ? (
        <>
          <h2>Leave the game</h2>
          <p>
            Withdrawing is not a kill. Your hunter takes over your victim and
            mission, and you cannot rejoin.
          </p>
          {expired && (
            <p role="status">
              Your withdrawal request expired. Nothing changed.
            </p>
          )}
          <button
            className="secondary danger"
            disabled={busy}
            onClick={() =>
              action(async () => {
                setExpired(false);
                const result = await request(base, {
                  method: "POST",
                  token: access.token,
                });
                setPending({
                  token: result.withdrawalToken,
                  expiresAt: result.expiresAt,
                });
              })
            }
          >
            Withdraw from game
          </button>
        </>
      ) : (
        <section
          className="confirmation-request"
          aria-label="Withdrawal confirmation"
          aria-live="assertive"
        >
          <h2>Withdraw from the game?</h2>
          <p>
            You will leave the game without a kill and your hunter will inherit
            your victim and mission. This cannot be undone.
          </p>
          <div className="actions">
            <button
              className="danger"
              disabled={busy}
              onClick={() =>
                action(async () => {
                  try {
                    await request(`${base}/confirm`, {
                      method: "POST",
                      token: access.token,
                      body: { withdrawalToken: pending.token },
                    });
                  } catch (e) {
                    // A rejected or stale request cannot be retried; start over.
                    if (e.status === 409) setPending(null);
                    throw e;
                  }
                  setPending(null);
                })
              }
            >
              Confirm withdrawal
            </button>
            <button
              className="secondary"
              disabled={busy}
              onClick={() => setPending(null)}
            >
              Keep playing
            </button>
          </div>
        </section>
      )}
    </section>
  );
}
