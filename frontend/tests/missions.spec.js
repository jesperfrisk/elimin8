import { test, expect } from "@playwright/test";

test("selected presets mix with custom missions, retry safely, persist and stay private", async ({ page, request, browser }) => {
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Mission presets ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  const presets = page.getByRole("region", { name: "Mission presets", exact: true });
  const list = page.getByRole("region", { name: "Mission list", exact: true });
  const submit = presets.getByRole("button", { name: "Add selected presets", exact: true });
  await expect(presets.getByRole("checkbox")).toHaveCount(4);
  await expect(presets.getByText("No presets selected. Choose one or more to add.", { exact: true })).toBeVisible();
  await expect(submit).toBeDisabled();
  await expect(list.getByRole("listitem")).toHaveCount(0);
  await expect(presets.getByRole("checkbox", { name: 'Get your assigned victim to say "Cheers" to you.', exact: true })).toBeVisible();
  await expect(presets.getByRole("checkbox", { name: "Get your assigned victim to give you a high-five.", exact: true })).toBeVisible();
  const laugh = presets.getByRole("checkbox", { name: "Make your assigned victim laugh.", exact: true });
  const selfie = presets.getByRole("checkbox", { name: "Get your assigned victim to take a selfie with you.", exact: true });
  // Native checkboxes support keyboard selection as well as touch/click.
  await laugh.focus();
  await page.keyboard.press("Space");
  await expect(laugh).toBeChecked();
  await selfie.check();
  let submissions = 0;
  let rejectRefresh = false;
  await page.route("**/api/games/*", async (route) => {
    if (rejectRefresh && route.request().method() === "GET") {
      return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Refresh temporarily unavailable" }) });
    }
    return route.continue();
  });
  let release;
  const pending = new Promise((resolve) => { release = resolve; });
  await page.route("**/api/games/*/missions", async (route) => {
    if (route.request().method() !== "POST") return route.continue();
    submissions++;
    expect(route.request().postDataJSON()).toEqual({ texts: [
      "Make your assigned victim laugh.", "Get your assigned victim to take a selfie with you.",
    ] });
    if (submissions === 1) {
      return route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Presets temporarily unavailable" }) });
    }
    await pending;
    rejectRefresh = true;
    return route.continue();
  });
  await submit.click();
  await expect(presets.getByRole("alert")).toHaveText("Presets temporarily unavailable");
  await expect(laugh).toBeChecked();
  await expect(selfie).toBeChecked();
  await expect(list.getByRole("listitem")).toHaveCount(0);
  await submit.click();
  await expect(presets.getByRole("button", { name: "Adding presets…", exact: true })).toBeDisabled();
  await expect(laugh).toBeDisabled();
  release();
  await expect(presets.getByRole("status")).toHaveText("Added 2 preset missions to this game.");
  await expect(submit).toBeDisabled();
  await expect(laugh).not.toBeChecked();
  await expect(page.getByRole("alert").filter({ hasText: "Refresh temporarily unavailable" }).first()).toBeVisible();
  expect(submissions).toBe(2);
  rejectRefresh = false;
  await page.unroute("**/api/games/*");
  await page.unroute("**/api/games/*/missions");
  await page.reload();
  await expect(list.getByRole("listitem")).toHaveCount(2);

  const custom = page.getByLabel("Mission texts", { exact: true });
  await custom.fill("Get your victim to say pineapple.\nGet your victim to say pineapple.");
  await page.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(list.getByRole("listitem")).toHaveCount(4);
  await list.getByRole("button", { name: "Disable mission: Get your assigned victim to take a selfie with you.", exact: true }).click();
  await page.reload();
  await expect(list.getByRole("listitem")).toHaveCount(4);
  await expect(list.getByRole("button", { name: "Enable mission: Get your assigned victim to take a selfie with you.", exact: true })).toBeVisible();
  await expect(presets.getByRole("checkbox")).toHaveCount(4);
  await expect(submit).toBeDisabled();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);

  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const isolatedResponse = await request.post("/api/games", { data: { name: "Independent preset catalogue" } });
  expect(isolatedResponse.ok()).toBe(true);
  const isolated = await isolatedResponse.json();
  expect(isolated.game.missions).toEqual([]);
  const isolatedHeaders = { Authorization: `Bearer ${isolated.token}` };
  const added = await request.post(`/api/games/${isolated.game.id}/missions`, {
    headers: isolatedHeaders, data: { texts: ["Get your assigned victim to take a selfie with you."] },
  });
  expect(added.ok()).toBe(true);
  const isolatedView = await (await request.get(`/api/games/${isolated.game.id}`, { headers: isolatedHeaders })).json();
  expect(isolatedView.missions[0].enabled).toBe(true);
  const original = await (await request.get(base, { headers })).json();
  expect(original.missions).toHaveLength(4);
  expect(original.missions.find((m) => m.text.includes("selfie")).enabled).toBe(false);

  const rosterResponse = await request.post(`${base}/players`, { headers, data: { names: ["Preset Alice", "Preset Bob"] } });
  expect(rosterResponse.ok()).toBe(true);
  const roster = await rosterResponse.json();
  const players = [];
  for (const entry of roster.players) {
    const login = await request.post("/api/access", { data: { invitationCode: owner.game.invitationCode, code: entry.code, role: "player" } });
    expect(login.ok()).toBe(true);
    players.push(await login.json());
  }
  expect(players[0].game.missions).toBeUndefined();
  const playerHeaders = { Authorization: `Bearer ${players[0].token}` };
  expect((await request.post(`${base}/missions`, { headers: playerHeaders, data: { texts: ["Make your assigned victim laugh."] } })).status()).toBe(403);
  const playerContext = await browser.newContext({ baseURL: page.url() });
  try {
    await playerContext.addInitScript((access) => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([access])), players[0]);
    const playerPage = await playerContext.newPage();
    await playerPage.goto(`/#/player/${owner.game.id}`);
    await expect(playerPage.getByText("Your place in the lobby is confirmed.", { exact: true })).toBeVisible();
    await expect(playerPage.getByRole("region", { name: "Mission presets", exact: true })).toHaveCount(0);
    page.once("dialog", (dialog) => dialog.accept());
    await page.getByRole("button", { name: "Start game" }).click();
    await expect(page.getByText("Game active", { exact: true })).toBeVisible();
    await expect(presets).toHaveCount(0);
    const view = await (await request.get(base, { headers })).json();
    expect(view.players.every((p) => p.mission !== "Get your assigned victim to take a selfie with you.")).toBe(true);
    const enabled = original.missions.filter((m) => m.enabled).map((m) => m.text);
    expect(view.players.every((p) => enabled.includes(p.mission))).toBe(true);
    const privateView = await (await request.get(base, { headers: playerHeaders })).json();
    expect(privateView.missions).toBeUndefined();
    const board = await (await request.get(`/api/board/${owner.game.id}`)).json();
    expect(JSON.stringify(board)).not.toMatch(/mission|pineapple|selfie|laugh/);
    expect((await request.post(`${base}/missions`, { headers, data: { texts: ["Late preset"] } })).status()).toBe(409);
  } finally {
    await playerContext.close();
    await request.post(`${base}/end`, { headers });
    await request.post(`/api/games/${isolated.game.id}/end`, { headers: isolatedHeaders });
  }
});

test("batch lobby missions, disable and re-enable, recover a failed batch, and lock on start", async ({ page, request }, testInfo) => {
  const errors = [];
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto("/#/organizer");
  await page.getByLabel("Game name", { exact: true }).fill(`Lobby missions ${Date.now()}`);
  await page.getByRole("button", { name: "Create game" }).click();
  const config = page.getByRole("region", { name: "Lobby missions", exact: true });
  const list = config.getByRole("region", { name: "Mission list", exact: true });
  const draft = config.getByLabel("Mission texts", { exact: true });
  await expect(config.getByText("0 enabled / 0 total", { exact: true })).toBeVisible();
  await draft.fill("  Get a high-five.  \n\nSay pineapple.\nGet a high-five.\n");
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(list.getByRole("listitem")).toHaveCount(3);
  await expect(draft).toHaveValue("");
  await expect(list.getByText("Get a high-five.", { exact: true })).toHaveCount(2);

  const [mirror] = await Promise.all([
    page.waitForEvent("popup"),
    page.evaluate(() => window.open(location.href)),
  ]);
  try {
    await expect(mirror.getByText("3 enabled / 3 total", { exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Disable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
    await expect(mirror.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
    await page.reload();
    await expect(list.getByRole("button", { name: "Enable mission: Say pineapple.", exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Enable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("3 enabled / 3 total", { exact: true })).toBeVisible();
    await list.getByRole("button", { name: "Disable mission: Say pineapple.", exact: true }).click();
    await expect(config.getByText("2 enabled / 3 total", { exact: true })).toBeVisible();
  } finally {
    await mirror.close();
  }

  // A later failed batch must retain only unsaved lines, so retry adds no duplicates.
  let batches = 0;
  await page.route("**/api/games/*/missions", async (route) => {
    if (route.request().method() === "POST" && ++batches === 2) {
      await route.fulfill({ status: 503, contentType: "application/json", body: JSON.stringify({ error: "Temporary mission test failure" }) });
    } else {
      await route.continue();
    }
  });
  const many = Array.from({ length: 105 }, (_, i) => `Batch mission ${i + 1}`);
  await draft.fill(many.join("\n"));
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(page.getByRole("alert")).toContainText("Temporary mission test failure");
  await expect(draft).toHaveValue(many.slice(100).join("\n"));
  await expect(list.getByRole("listitem")).toHaveCount(103);
  await config.getByRole("button", { name: "Add missions +", exact: true }).click();
  await expect(draft).toHaveValue("");
  await expect(config.getByText("107 enabled / 108 total", { exact: true })).toBeVisible();
  await expect(list.getByRole("listitem")).toHaveCount(108);

  const owner = await page.evaluate(() => JSON.parse(sessionStorage.getItem("elimin8.access.v1"))[0]);
  const base = `/api/games/${owner.game.id}`;
  const headers = { Authorization: `Bearer ${owner.token}` };
  const view = await (await request.get(base, { headers })).json();
  expect(view.missions.filter((m) => !m.enabled).map((m) => m.text)).toEqual(["Say pineapple."]);
  const missionID = view.missions[0].id;
  const roster = await (await request.post(`${base}/players`, { headers, data: { names: ["Alice", "Bob"] } })).json();
  const player = await (await request.post("/api/access", { data: {
    invitationCode: owner.game.invitationCode, code: roster.players[0].code, role: "player",
  } })).json();
  await request.post("/api/access", { data: { invitationCode: owner.game.invitationCode, code: roster.players[1].code, role: "player" } });
  expect(player.game.missions).toBeUndefined();
  const playerHeaders = { Authorization: `Bearer ${player.token}` };
  expect((await request.post(`${base}/missions`, { headers: playerHeaders, data: { texts: ["Forbidden"] } })).status()).toBe(403);
  expect((await request.patch(`${base}/missions/${missionID}`, { headers: playerHeaders, data: { enabled: false } })).status()).toBe(403);
  expect((await request.post(`${base}/missions`, { data: { texts: ["Anonymous"] } })).status()).toBe(401);
  for (const data of [{}, { enabled: null }, { enabled: "false" }]) {
    expect((await request.patch(`${base}/missions/${missionID}`, { headers, data })).status()).toBe(400);
  }
  const invalid = await request.post(`${base}/missions`, { headers, data: { texts: ["Valid", " "] } });
  expect(invalid.status()).toBe(400);
  const other = await (await request.post("/api/games", { data: { name: "Isolated mission lobby" } })).json();
  expect(other.game.missions).toEqual([]);
  const otherHeaders = { Authorization: `Bearer ${other.token}` };
  expect((await request.patch(`/api/games/${other.game.id}/missions/${missionID}`, { headers: otherHeaders, data: { enabled: false } })).status()).toBe(404);
  expect((await request.patch(`/api/games/${other.game.id}/missions/${missionID}`, { headers, data: { enabled: false } })).status()).toBe(403);

  await page.setViewportSize({ width: 390, height: 844 });
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
  await config.screenshot({ path: testInfo.outputPath("mobile-lobby-missions.png") });
  page.once("dialog", (dialog) => dialog.accept());
  await page.getByRole("button", { name: "Start game" }).click();
  await expect(page.getByText("Game active", { exact: true })).toBeVisible();
  await expect(config.getByRole("button")).toHaveCount(0);
  expect((await request.post(`${base}/missions`, { headers, data: { texts: ["Late"] } })).status()).toBe(409);
  expect((await request.patch(`${base}/missions/${missionID}`, { headers, data: { enabled: false } })).status()).toBe(409);
  for (const [id, auth] of [[owner.game.id, headers], [other.game.id, otherHeaders]]) {
    expect((await request.post(`/api/games/${id}/end`, { headers: auth })).status()).toBe(200);
  }
  expect(errors).toEqual([]);
});
