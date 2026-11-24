import React, { useState } from "react";

export function PageHeading({ eyebrow, title, text }) {
  return (
    <div className="page-heading">
      <p className="eyebrow">{eyebrow}</p>
      <h1>{title}</h1>
      <p className="intro">{text}</p>
    </div>
  );
}

export function CopyButton({ text, label = "Copy" }) {
  const [status, setStatus] = useState("");
  return (
    <>
      <button
        type="button"
        className="secondary compact"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
            setStatus("Copied");
          } catch {
            setStatus("Select the text to copy it manually.");
          }
        }}
      >
        {label}
      </button>
      <span className="copy-status" role="status">
        {status}
      </span>
    </>
  );
}

export function formatDate(value) {
  return value
    ? new Date(value).toLocaleString(undefined, {
        dateStyle: "medium",
        timeStyle: "short",
      })
    : "—";
}
