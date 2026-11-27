import { test, expect } from "@playwright/test";
import { formatOffset, recapWinners, rankStandings } from "../src/recapData.js";

// Fixture shaped like GET /api/board/{id}/recap. It names subjects only.
const recap = (over = {}) => ({
  winners: [{ kind: "player", id: "p1", name: "Alice" }],
  summary: { durationSeconds: 3725, players: 4, eliminations: 2, withdrawals: 1, declinedReports: 3, reason: "survival", tie: false },
  leaderboard: [
    { kind: "player", id: "p1", name: "Alice", kills: 2 },
    { kind: "player", id: "p2", name: "Bob", kills: 1 },
    { kind: "player", id: "p3", name: "Carol", kills: 1 },
    { kind: "player", id: "p4", name: "Dana", kills: 0 },
  ],
  timeline: [
    { playerId: "p4", name: "Dana", event: "withdrew", atSeconds: 60 },
    { playerId: "p3", name: "Carol", event: "eliminated", atSeconds: 427 },
    { playerId: "p2", name: "Bob", event: "eliminated", atSeconds: 3600 },
  ],
  awards: {
    longestSurvivor: { playerId: "p1", name: "Alice", atSeconds: 3725 },
    firstElimination: { playerId: "p3", name: "Carol", atSeconds: 427 },
    fastestElimination: { playerId: "p3", name: "Carol", atSeconds: 427 },
    mostKills: [{ kind: "player", id: "p1", name: "Alice", kills: 2 }],
    lastElimination: { playerId: "p2", name: "Bob", atSeconds: 3600 },
  },
  ...over,
});

const emptyRecap = () =>
  recap({
    winners: [],
    summary: { durationSeconds: 0, players: 3, eliminations: 0, withdrawals: 0, declinedReports: 0, reason: "organizer", tie: false },
    leaderboard: [{ kind: "player", id: "p1", name: "Alice", kills: 0 }],
    timeline: [],
    awards: { longestSurvivor: null, firstElimination: null, fastestElimination: null, mostKills: [], lastElimination: null },
  });

test("recap helpers: offsets, winners and tied ranks", () => {
  expect(formatOffset(7)).toBe("00:07");
  expect(formatOffset(75)).toBe("01:15");
  expect(formatOffset(3725)).toBe("1:02:05");
  expect(formatOffset(-4)).toBe("00:00");
  expect(recapWinners(recap())).toEqual([{ kind: "player", id: "p1", name: "Alice" }]);
  expect(recapWinners(emptyRecap())).toEqual([]);
  const timer = recap({ winners: [{kind: "player", id: "a", name: "A"}, {kind: "player", id: "b", name: "B"}], summary: { ...recap().summary, reason: "timer", tie: true }, awards: { ...recap().awards, mostKills: [{ id: "a", name: "A", kills: 2 }, { id: "b", name: "B", kills: 2 }] } });
  expect(recapWinners(timer).map((w) => w.name)).toEqual(["A", "B"]);
  expect(rankStandings(recap().leaderboard).map((r) => [r.rank, r.tied])).toEqual([[1, false], [2, true], [2, true], [4, false]]);
});

const board = (status) => ({
  gameId: "g1",
  name: "Mock game",
  status,
  activeCount: 1,
  timer: null,
  players: [
    { id: "p1", number: 1, name: "Alice", status: "active" },
    { id: "p2", number: 2, name: "Bob", status: "eliminated" },
  ],
});

async function openBoard(page, status, data) {
  let recapRequests = 0;
  await page.route("**/api/board/**", (route) => {
    const isRecap = route.request().url().endsWith("/recap");
    if (isRecap) recapRequests++;
    if (isRecap && status !== "ended") return route.fulfill({ status: 409, contentType: "application/json", body: '{"error":"not ended"}' });
    return route.fulfill({ contentType: "application/json", body: JSON.stringify(isRecap ? data : board(status)) });
  });
  await page.routeWebSocket("**/api/mqtt", () => {});
  await page.goto("/#/board/g1");
  return () => recapRequests;
}

test("board recap: all five sections, no killers, ties and a table fallback", async ({ page }) => {
  await openBoard(page, "ended", recap({ leaderboard: [{ kind: "player", id: "p1", name: "Alice", kills: 1 }, { kind: "player", id: "p2", name: "Bob", kills: 1 }], summary: { ...recap().summary, tie: true } }));
  const view = page.getByTestId("recap");
  await expect(view).toBeVisible();
  await expect(view.getByRole("heading", { name: "Winner", exact: true })).toBeVisible();
  await expect(view.getByText("It’s a tie.")).toBeVisible();
  for (const name of ["Leaderboard", "Elimination timeline", "Awards", "Game summary"]) {
    await expect(view.getByRole("heading", { name })).toBeVisible();
  }
  await expect(view.getByText("Longest survivor")).toBeVisible();
  await expect(view.getByText("Fastest elimination")).toBeVisible();
  await expect(view.getByText("Duration").locator("..")).toContainText("1:02:05");
  await expect(view.locator(".recap-timeline li")).toHaveCount(3);
  await expect(view.locator(".recap-timeline li").nth(1)).toContainText("07:07");
  await expect(view.locator(".recap-timeline li").first()).toContainText("Withdrew");
  // Tied ranks are marked as tied.
  await expect(view.locator(".recap-bars li").first()).toContainText("=1");
  // The data table fallback carries the same rows.
  await view.getByText("Show the recap as tables").click();
  await expect(view.getByRole("table", { name: "Leaderboard" }).getByRole("row")).toHaveCount(3);
  // Subjects only: nothing about killers or pairs anywhere in the recap.
  expect(await view.innerText()).not.toMatch(/killer|killed by|victim|target|mission|eliminated by/i);
  // The players are one click away.
  await page.getByRole("button", { name: "Show players" }).click();
  await expect(page.getByRole("list", { name: "Players" })).toBeVisible();
});

test("board recap: empty states for an organizer-ended game with no kills", async ({ page }) => {
  await openBoard(page, "ended", emptyRecap());
  const view = page.getByTestId("recap");
  await expect(view.getByRole("heading", { name: "No winner" })).toBeVisible();
  await expect(view.getByText("Nobody scored a confirmed kill.")).toBeVisible();
  await expect(view.getByText("Nobody was eliminated or withdrew.")).toBeVisible();
  await expect(view.getByText("No awards were earned in this game.")).toBeVisible();
});

test("board recap: never requested or shown before the game ends", async ({ page }) => {
  const count = await openBoard(page, "active", recap());
  await expect(page.getByRole("list", { name: "Players" })).toBeVisible();
  await expect(page.getByTestId("recap")).toHaveCount(0);
  expect(count()).toBe(0);
});

test("board recap: reduced motion turns the reveal animation off", async ({ page }) => {
  await page.emulateMedia({ reducedMotion: "reduce" });
  await openBoard(page, "ended", recap());
  await expect(page.getByTestId("recap")).toBeVisible();
  const name = await page.locator(".recap-hero").evaluate((el) => getComputedStyle(el).animationName);
  expect(name).toBe("none");
});

for (const [name, winners, reason] of [
  ["survival ignores an eliminated longest-survivor award", [{ kind: "player", id: "z", name: "Zoe" }], "survival"],
  ["team survival shows the winning team", [{ kind: "team", id: "t1", name: "Winning team" }], "survival"],
  ["zero-kill timer shows all tied winners", [{ kind: "player", id: "p1", name: "Alice" }, { kind: "player", id: "p2", name: "Bob" }], "timer"],
]) {
  test(name, async ({ page }) => {
    const data = recap({
      winners,
      summary: { ...recap().summary, reason, tie: winners.length > 1 },
      awards: { ...recap().awards, longestSurvivor: { playerId: "a", name: "Eliminated Alice", atSeconds: 3725 }, mostKills: [] },
    });
    await openBoard(page, "ended", data);
    const hero = page.locator(".recap-hero");
    for (const winner of winners) await expect(hero.getByText(winner.name, { exact: true })).toBeVisible();
    await expect(hero).not.toContainText("Eliminated Alice");
    await expect(hero.getByRole("heading", { name: "No winner" })).toHaveCount(0);
    if (winners.length > 1) await expect(hero.getByText("It\u2019s a tie.")).toBeVisible();
  });
}
