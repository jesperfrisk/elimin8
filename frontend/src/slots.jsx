import React from "react";
import { Withdrawal } from "./Withdrawal";
import { Results } from "./Results";

// Extension points for new game panels.
//
// To add a panel, create `src/MyPanel.jsx` exporting a component and add two
// lines to this file: an import and one entry in the array for the game phase
// it belongs to. Nothing else in the game page needs to change.
//
// Each panel is rendered below the existing game content for every viewer
// (organizer and player) in that phase and receives:
//   game    latest game state for the current role (from useGame)
//   access  { role, token, game } for the current tab
//   action  action(async work): run work with the busy flag, then refresh
//   busy    true while any action is in flight
// A panel decides for itself what each role may see; return null to render
// nothing. Server responses are already scoped per role, so never rely on the
// UI to hide private data.

// game.status === "lobby"
import { Timer } from "./Timer";
import { Teams } from "./Teams";
import { KillBoard } from "./KillBoard";

export const lobbyPanels = [Timer, Teams];

// game.status === "active"
export const inGamePanels = [Timer, Teams, Withdrawal, KillBoard];

// game.status === "ended"
export const gameOverPanels = [Results];

export function PanelSlot({ panels, ...props }) {
  return panels.map((Panel, i) => (
    <Panel key={Panel.displayName || Panel.name || i} {...props} />
  ));
}
