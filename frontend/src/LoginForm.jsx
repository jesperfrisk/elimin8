import React from "react";
import { request } from "./api";

export function LoginForm({ role, run, busy, admitted }) {
  return (
    <form
      onSubmit={(e) => {
        e.preventDefault();
        const form = e.currentTarget;
        const data = new FormData(form);
        run(async () => {
          const a = await request("/api/access", {
            method: "POST",
            body: {
              invitationCode: data.get("invitationCode"),
              code: data.get("code"),
              role,
            },
          });
          form.reset();
          admitted(a);
        });
      }}
    >
      <label>
        Game invitation code
        <input
          name="invitationCode"
          className="code-input"
          required
          maxLength={16}
          autoCapitalize="characters"
          autoComplete="off"
          spellCheck={false}
          placeholder="ABCD2345"
        />
      </label>
      <label>
        {role === "organizer"
          ? "Organizer management code"
          : "Your private player code"}
        <input
          name="code"
          type="password"
          required
          maxLength={64}
          autoComplete="off"
          spellCheck={false}
          placeholder={
            role === "organizer"
              ? "Your management code"
              : "Your 8-character code"
          }
        />
      </label>
      <button disabled={busy}>
        {busy
          ? "Opening…"
          : role === "organizer"
            ? "Open organizer desk →"
            : "Enter game →"}
      </button>
    </form>
  );
}
