// A failed request may have reached the server. Connection/server errors must
// prompt synchronization before retrying, rather than claim the action failed.
export function gameActionError(error, action) {
  const verb = action === "start" ? "start" : "end";
  switch (error.status) {
    case 400:
      return `The game could not ${verb} because its data is invalid or too large. Review the lobby setup and try again.`;
    case 401:
      return "Your organizer access is invalid or expired. Reopen this game with its invitation and organizer management codes, then try again.";
    case 403:
      return `Only this game's organizer can ${verb} it. Open the organizer desk using the management code.`;
    case 404:
      return "This game could not be found. Return to the organizer desk and open the game again.";
    case 409:
      return error.message || "The game changed while you were working. Refresh the game and try again.";
    case 429:
      return "Too many requests. Wait a minute before trying again.";
    default:
      if (error.status) {
        return `The server could not confirm whether the game ${action === "start" ? "started" : "ended"}. Refresh the game to check its state before trying again.`;
      }
      return `Could not reach the game service. Check your connection and refresh the game to see whether it ${action === "start" ? "started" : "ended"} before trying again.`;
  }
}
