import { test, expect } from "@playwright/test";
import mqtt from "mqtt";

test("mobile navigation and entry forms fit the viewport", async ({ page }, testInfo) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await expect(page.getByRole("link", { name: "Create a game" })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.screenshot({ path: testInfo.outputPath("mobile-home.png"), fullPage: true });
  await page.getByRole("link", { name: "Join a game" }).click();
  await expect(page.getByLabel("Your private player code")).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await page.getByRole("link", { name: "Organize", exact: true }).click();
  await expect(page.getByLabel("Game name", { exact: true })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
});

test("organizer roster, private entry, live start/end, and excluded invitees", async ({
  page,
  browser,
  request,
  baseURL,
}) => {
  const consoleErrors = [];
  page.on("pageerror", (error) => consoleErrors.push(error.message));
  await page.goto("/#/organizer");
  const name = `Lobby integration ${Date.now()}`;
  await page.getByLabel("Game name", { exact: true }).fill(name);
  await page
    .getByLabel("Description (optional)")
    .fill("A real game lifecycle, without assignments.");
  await page.getByRole("button", { name: "Create game" }).click();
  await expect(page.getByRole("heading", { name, exact: true })).toBeVisible();
  await expect(
    page.getByText("Live updates connected", { exact: true }),
  ).toBeVisible();
  const invitation = await page.locator(".invitation code").textContent();
  const managementCode = await page.locator(".management-code").textContent();
  expect(managementCode).toHaveLength(24);
  await page.getByLabel("Player names").fill("Alice\nBob");
  await page.getByRole("button", { name: "Add to roster" }).click();
  await expect(page.locator(".issued-code")).toHaveCount(2);
  const aliceCode = await page
    .locator(".issued-code")
    .filter({ hasText: "Alice" })
    .locator("code")
    .textContent();
  const bobCode = await page
    .locator(".issued-code")
    .filter({ hasText: "Bob" })
    .locator("code")
    .textContent();
  const playerContext = await browser.newContext();
  const player = await playerContext.newPage();
  player.on("pageerror", (error) => consoleErrors.push(error.message));
  try {
    await player.goto(`${baseURL}/#/player`);
    await player.getByLabel("Game invitation code").fill(invitation);
    await player.getByLabel("Your private player code").fill(aliceCode);
    await player.getByRole("button", { name: "Enter game" }).click();
    await expect(
      player.getByRole("heading", {
        name: "Your place in the lobby is confirmed.",
      }),
    ).toBeVisible();
    await expect(
      page
        .getByRole("row")
        .filter({ hasText: "Alice" })
        .getByText("Joined", { exact: true }),
    ).toBeVisible();
    // Disconnect before the start: reconnect must recover state without polling.
    await playerContext.setOffline(true);
    page.once("dialog", (dialog) => dialog.accept());
    await page.getByRole("button", { name: "Start game" }).click();
    await expect(page.getByText("Game active", { exact: true })).toBeVisible();
    await playerContext.setOffline(false);
    await expect(
      player.getByRole("heading", { name: "The game is on." }),
    ).toBeVisible({ timeout: 20000 });
    await expect(
      page
        .getByRole("row")
        .filter({ hasText: "Bob" })
        .getByText("Not included"),
    ).toBeVisible();
    const lateJoin = await request.post("/api/access", {
      data: { invitationCode: invitation, code: bobCode, role: "player" },
    });
    expect(lateJoin.status()).toBe(409);
    await player.reload();
    await expect(
      player.getByRole("heading", { name: "The game is on." }),
    ).toBeVisible();
    page.once("dialog", (dialog) => dialog.accept());
    await page.getByRole("button", { name: "End game", exact: true }).click();
    await expect(
      player.getByRole("heading", { name: "This game has ended." }),
    ).toBeVisible();
    expect(consoleErrors).toEqual([]);
  } finally {
    await playerContext.close();
  }
});

test("MQTT topic isolation, code reset, and API permission checks", async ({
  request,
  baseURL,
}) => {
  const create = await request.post("/api/games", {
    data: { name: `MQTT permissions ${Date.now()}` },
  });
  expect(create.status()).toBe(201);
  const owner = await create.json();
  const auth = { Authorization: `Bearer ${owner.token}` };
  const roster = await request.post(`/api/games/${owner.game.id}/players`, {
    headers: auth,
    data: { names: ["Private player"] },
  });
  const [issued] = (await roster.json()).players;
  const login = await request.post("/api/access", {
    data: {
      invitationCode: owner.game.invitationCode,
      code: issued.code,
      role: "player",
    },
  });
  const player = await login.json();
  const websocketURL = baseURL.replace(/^http/, "ws") + "/api/mqtt";
  const client = await mqtt.connectAsync(websocketURL, {
    username: player.token,
    clientId: player.clientPrefix + crypto.randomUUID(),
    clean: true,
    reconnectPeriod: 0,
    connectTimeout: 5000,
  });
  try {
    const granted = await client.subscribeAsync(player.topic, { qos: 1 });
    expect(granted[0].qos).toBe(1);
    await expect(client.subscribeAsync(owner.topic)).rejects.toThrow();
    await expect(client.subscribeAsync("games/#")).rejects.toThrow();
    const forbidden = await request.post(`/api/games/${owner.game.id}/start`, {
      headers: { Authorization: `Bearer ${player.token}` },
    });
    expect(forbidden.status()).toBe(403);
    const view = await request.get(`/api/games/${owner.game.id}`, {
      headers: { Authorization: `Bearer ${player.token}` },
    });
    const data = await view.json();
    expect(data.players).toBeUndefined();
    expect(data.me.name).toBe("Private player");
    expect(JSON.stringify(data)).not.toContain(issued.code);
    const reset = await request.post(
      `/api/games/${owner.game.id}/players/${issued.id}/code`,
      { headers: auth },
    );
    const replacement = await reset.json();
    expect(reset.status()).toBe(200);
    const revoked = await request.get(`/api/games/${owner.game.id}`, {
      headers: { Authorization: `Bearer ${player.token}` },
    });
    expect(revoked.status()).toBe(401);
    const oldLogin = await request.post("/api/access", {
      data: {
        invitationCode: owner.game.invitationCode,
        code: issued.code,
        role: "player",
      },
    });
    expect(oldLogin.status()).toBe(401);
    const newLogin = await request.post("/api/access", {
      data: {
        invitationCode: owner.game.invitationCode,
        code: replacement.code,
        role: "player",
      },
    });
    expect(newLogin.status()).toBe(200);
    const end = await request.post(`/api/games/${owner.game.id}/end`, {
      headers: auth,
    });
    expect(end.status()).toBe(200);
  } finally {
    await client.endAsync(true);
  }
});
