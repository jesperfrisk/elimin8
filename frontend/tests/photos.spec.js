import { test, expect } from "@playwright/test";
import { fitWithin, checkPhotoFile, MAX_SIDE, MAX_SOURCE_BYTES } from "../src/photoImage.js";

// ---- Pure helpers: no browser, no backend. ----

test("fitWithin shrinks to the longest side and never enlarges", () => {
  expect(fitWithin(4000, 3000)).toEqual({ width: 1200, height: 900 });
  expect(fitWithin(3000, 4000)).toEqual({ width: 900, height: 1200 });
  expect(fitWithin(8000, 6000, 1200)).toEqual({ width: 1200, height: 900 });
  expect(fitWithin(800, 600)).toEqual({ width: 800, height: 600 }); // already small
  expect(fitWithin(1200, 1200)).toEqual({ width: 1200, height: 1200 });
  expect(fitWithin(5000, 1)).toEqual({ width: 1200, height: 1 }); // never collapses to zero
  expect(fitWithin(0, 100)).toEqual({ width: 0, height: 0 });
  expect(MAX_SIDE).toBe(1200);
});

test("checkPhotoFile gives a friendly message for unusable files", () => {
  const file = (type, size, name = "x") => ({ type, size, name });
  expect(checkPhotoFile(file("image/jpeg", 1000))).toBeNull();
  expect(checkPhotoFile(file("image/png", 1000))).toBeNull();
  expect(checkPhotoFile(file("image/webp", 1000))).toBeNull();
  expect(checkPhotoFile(file("", 1000))).toBeNull(); // some browsers report no type
  expect(checkPhotoFile(file("application/pdf", 1000))).toMatch(/JPEG, PNG or WebP/);
  expect(checkPhotoFile(file("image/gif", 1000))).toMatch(/JPEG, PNG or WebP/);
  expect(checkPhotoFile(file("image/jpeg", 0))).toMatch(/empty/);
  expect(checkPhotoFile(file("image/jpeg", MAX_SOURCE_BYTES + 1))).toMatch(/too large/);
  expect(checkPhotoFile(null)).toMatch(/Choose a photo/);
});

// ---- The photo UI in a browser with the API and MQTT broker stubbed. ----

// A valid 1x1 PNG, served for every photo read.
const PNG = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mP8z8BQDwAEhQGAhKmMIQAAAABJRU5ErkJggg==", "base64");

function jpegSize(buffer) {
  expect(buffer[0]).toBe(0xff);
  expect(buffer[1]).toBe(0xd8);
  let i = 2;
  while (i < buffer.length) {
    if (buffer[i] !== 0xff) {
      i++;
      continue;
    }
    const marker = buffer[i + 1];
    if (marker >= 0xc0 && marker <= 0xcf && ![0xc4, 0xc8, 0xcc].includes(marker)) {
      return { height: buffer.readUInt16BE(i + 5), width: buffer.readUInt16BE(i + 7) };
    }
    i += 2 + buffer.readUInt16BE(i + 2);
  }
  throw new Error("no JPEG frame header found");
}

async function bigJpeg(page, width = 3000, height = 2000) {
  const url = await page.evaluate(
    ({ width, height }) => {
      const canvas = document.createElement("canvas");
      canvas.width = width;
      canvas.height = height;
      const ctx = canvas.getContext("2d");
      ctx.fillStyle = "#c33";
      ctx.fillRect(0, 0, width, height);
      ctx.fillStyle = "#fff";
      ctx.fillRect(width / 4, height / 4, width / 2, height / 2);
      return canvas.toDataURL("image/jpeg", 0.95);
    },
    { width, height },
  );
  return Buffer.from(url.split(",")[1], "base64");
}

async function installBroker(page) {
  const sockets = [];
  await page.routeWebSocket("**/api/mqtt", (ws) => {
    sockets.push(ws);
    ws.onMessage((message) => {
      const b = Buffer.from(message);
      const type = b[0] >> 4;
      if (type === 1) ws.send(Buffer.from([0x20, 0x02, 0x00, 0x00]));
      else if (type === 8) ws.send(Buffer.from([0x90, 0x03, b[2], b[3], 0x01]));
      else if (type === 12) ws.send(Buffer.from([0xd0, 0x00]));
    });
  });
  return sockets;
}

// A signed-in player or organizer with a stubbed game and photo API.
async function openGame(page, { role = "player", status = "lobby", photoUrl = null, included = true, players } = {}) {
  const state = { role, status, photoUrl, puts: [], deletes: [], photoReads: [], putResponse: null, revision: 1, players };
  const access = {
    token: "t".repeat(48),
    expiresAt: new Date(Date.now() + 3600e3).toISOString(),
    role,
    clientPrefix: "c-",
    topic: role === "player" ? "games/g1/players/p0" : "games/g1/organizer",
    game: { id: "g1", name: "Photo game", invitationCode: "ABCD2345", status, revision: 0 },
  };
  await page.addInitScript((a) => sessionStorage.setItem("elimin8.access.v1", JSON.stringify([a])), access);
  await installBroker(page);
  await page.route("**/api/games/g1", (route) => {
    const base = {
      id: "g1",
      name: "Photo game",
      description: "",
      invitationCode: "ABCD2345",
      status: state.status,
      revision: state.revision,
      createdAt: new Date().toISOString(),
      startedAt: state.status === "lobby" ? null : new Date().toISOString(),
      endedAt: state.status === "ended" ? new Date().toISOString() : null,
      joinedCount: 2,
      invitedCount: 3,
      participantCount: 2,
      mode: "individual",
      teams: [],
      missions: [],
      timerMinutes: null,
      endsAt: null,
      remainingSeconds: null,
      endReason: null,
      winner: null,
    };
    const body =
      state.role === "player"
        ? { ...base, me: { id: "p0", name: "Alice", included, joinedAt: new Date().toISOString(), ...(state.photoUrl ? { photoUrl: state.photoUrl } : {}) } }
        : { ...base, players: state.players };
    return route.fulfill({ contentType: "application/json", body: JSON.stringify(body) });
  });
  await page.route("**/api/games/g1/players/*/photo", async (route) => {
    const request = route.request();
    const player = new URL(request.url()).pathname.split("/").at(-2);
    if (request.method() === "PUT") {
      state.puts.push({ player, headers: request.headers(), body: request.postDataBuffer() });
      if (state.putResponse) return route.fulfill({ status: state.putResponse.status, contentType: "application/json", body: JSON.stringify(state.putResponse.body) });
      state.photoUrl = `/api/games/g1/photos/NEW${state.puts.length}`;
      state.revision++;
      if (state.players) state.players = state.players.map((p) => (p.id === player ? { ...p, photoUrl: state.photoUrl } : p));
      return route.fulfill({ contentType: "application/json", body: JSON.stringify({ photoUrl: state.photoUrl }) });
    }
    state.deletes.push({ player, headers: request.headers() });
    state.photoUrl = null;
    state.revision++;
    if (state.players) state.players = state.players.map((p) => (p.id === player ? { ...p, photoUrl: undefined } : p));
    return route.fulfill({ status: 204, body: "" });
  });
  await page.route("**/api/games/g1/photos/*", (route) => {
    state.photoReads.push({ url: route.request().url(), auth: route.request().headers().authorization || null });
    return route.fulfill({ contentType: "image/png", body: PNG });
  });
  await page.goto(`/#/${role}/g1`);
  await expect(page.getByText("Live updates connected", { exact: true })).toBeVisible();
  return state;
}

test("player: preview, then confirm; the photo is shrunk on the device before upload", async ({ page }) => {
  const state = await openGame(page);
  const panel = page.getByRole("region", { name: "Your photo", exact: true });
  await expect(panel).toBeVisible();
  // Privacy copy is shown up front.
  await expect(panel).toContainText("shown on the public game board");
  await expect(panel).toContainText("deleted 24 hours after the game ends");
  // Two inputs: the camera one opens the front camera on phones, the other the gallery.
  await expect(panel.getByRole("button", { name: "Take a photo" })).toBeVisible();
  await expect(panel.getByRole("button", { name: "Choose a photo" })).toBeVisible();
  await expect(page.getByTestId("photo-camera")).toHaveAttribute("capture", "user");
  await expect(page.getByTestId("photo-gallery")).toHaveAttribute("accept", "image/jpeg,image/png,image/webp");
  await expect(panel.getByRole("button", { name: "Remove photo" })).toHaveCount(0);

  // Pick a large photo: a preview appears and nothing is uploaded yet.
  const original = await bigJpeg(page, 3000, 2000);
  await page.getByTestId("photo-gallery").setInputFiles({ name: "big.jpg", mimeType: "image/jpeg", buffer: original });
  await expect(panel.getByRole("img", { name: "Preview of your new photo" })).toBeVisible();
  await expect(panel).toContainText("This is how you will look on the board");
  expect(state.puts).toHaveLength(0);
  // Cancel goes back with nothing uploaded.
  await panel.getByRole("button", { name: "Cancel" }).click();
  await expect(panel.getByRole("img", { name: "Preview of your new photo" })).toHaveCount(0);
  expect(state.puts).toHaveLength(0);

  // Choose again, confirm: exactly one upload, shrunk, as multipart "photo".
  await page.getByTestId("photo-gallery").setInputFiles({ name: "big.jpg", mimeType: "image/jpeg", buffer: original });
  await panel.getByRole("button", { name: "Use this photo" }).click();
  await expect.poll(() => state.puts.length).toBe(1);
  const put = state.puts[0];
  expect(put.player).toBe("p0");
  expect(put.headers.authorization).toBe(`Bearer ${"t".repeat(48)}`);
  expect(put.headers["content-type"]).toMatch(/^multipart\/form-data; boundary=/);
  const text = put.body.toString("latin1");
  expect(text).toContain('name="photo"');
  expect(text).toContain("filename=");
  expect(text).toContain("image/jpeg");
  const start = put.body.indexOf(Buffer.from([0xff, 0xd8]));
  const jpeg = put.body.subarray(start);
  const { width, height } = jpegSize(jpeg);
  expect(Math.max(width, height)).toBeLessThanOrEqual(1200);
  expect(width / height).toBeCloseTo(3 / 2, 1); // proportions kept
  expect(put.body.length).toBeLessThan(original.length); // smaller than what the camera produced
  expect(put.body.length).toBeLessThan(1024 * 1024);

  // The page refreshes: the new photo shows (fetched with the player's credentials) and can be removed.
  await expect(panel.getByRole("button", { name: "Remove photo" })).toBeVisible();
  await expect(panel.locator("img.avatar-photo")).toBeVisible();
  expect(state.photoReads.at(-1).url).toContain("/api/games/g1/photos/NEW1");
  page.once("dialog", (dialog) => dialog.accept());
  await panel.getByRole("button", { name: "Remove photo" }).click();
  await expect.poll(() => state.deletes.length).toBe(1);
  await expect(panel.getByRole("button", { name: "Remove photo" })).toHaveCount(0);
  await expect(panel.locator("img.avatar-photo")).toHaveCount(0);
});

test("player: bad files and server rejections show a clear message and keep the page usable", async ({ page }) => {
  const state = await openGame(page);
  const panel = page.getByRole("region", { name: "Your photo", exact: true });
  // A file that is not an image: refused on the device, nothing uploaded.
  await page.getByTestId("photo-gallery").setInputFiles({ name: "notes.pdf", mimeType: "application/pdf", buffer: Buffer.from("%PDF-1.4") });
  await expect(panel.getByRole("alert")).toContainText("Choose a JPEG, PNG or WebP photo.");
  // An image type that cannot be decoded.
  await page.getByTestId("photo-gallery").setInputFiles({ name: "broken.jpg", mimeType: "image/jpeg", buffer: Buffer.from("not really a jpeg") });
  await expect(panel.getByRole("alert")).toContainText("couldn’t be read");
  expect(state.puts).toHaveLength(0);

  // The server refuses it: the message is shown and the preview stays so they can retry.
  state.putResponse = { status: 413, body: { error: "photo exceeds the 5 MiB or 25 megapixel limit" } };
  await page.getByTestId("photo-gallery").setInputFiles({ name: "big.jpg", mimeType: "image/jpeg", buffer: await bigJpeg(page, 1600, 1200) });
  await panel.getByRole("button", { name: "Use this photo" }).click();
  await expect(panel.getByRole("alert")).toContainText("5 MiB or 25 megapixel limit");
  await expect(panel.getByRole("button", { name: "Use this photo" })).toBeVisible();
  state.putResponse = { status: 409, body: { error: "game has ended" } };
  await panel.getByRole("button", { name: "Use this photo" }).click();
  await expect(panel.getByRole("alert")).toContainText("game has ended");
});

test("player: no photo controls once the game has ended, or for someone not in the started game", async ({ page, browser }) => {
  await openGame(page, { status: "ended" });
  await expect(page.getByRole("heading", { name: "This game has ended." })).toBeVisible();
  await expect(page.getByRole("region", { name: "Your photo", exact: true })).toHaveCount(0);
  const second = await browser.newContext();
  const other = await second.newPage();
  await openGame(other, { status: "active", included: false });
  await expect(other.getByRole("region", { name: "Your photo", exact: true })).toHaveCount(0);
  await second.close();
});

test("organizer: avatars in the roster, with add, replace and remove", async ({ page }) => {
  const players = [
    { id: "p0", name: "Alice", joinedAt: new Date().toISOString(), included: false, photoUrl: "/api/games/g1/photos/AAA" },
    { id: "p1", name: "Bob", joinedAt: null, included: false },
  ];
  const state = await openGame(page, { role: "organizer", players });
  const roster = page.getByRole("region", { name: "Player roster" });
  const alice = roster.getByRole("row", { name: /Alice/ });
  const bob = roster.getByRole("row", { name: /Bob/ });
  // Alice has a photo (fetched with the organizer's credentials, so invitees work too); Bob shows initials.
  await expect(alice.locator("img.avatar-photo")).toBeVisible();
  expect(state.photoReads[0].auth).toBe(`Bearer ${"t".repeat(48)}`);
  await expect(bob.locator("img.avatar-photo")).toHaveCount(0);
  await expect(bob.locator(".avatar-initials")).toHaveText("BO");
  await expect(alice.getByRole("button", { name: "Replace photo" })).toBeVisible();
  await expect(alice.getByRole("button", { name: "Remove photo" })).toBeVisible();
  await expect(bob.getByRole("button", { name: "Add photo" })).toBeVisible();
  await expect(bob.getByRole("button", { name: "Remove photo" })).toHaveCount(0);

  // Replace is uploaded straight away, shrunk, for that player.
  await bob.getByLabel("Photo file for Bob").setInputFiles({ name: "bob.jpg", mimeType: "image/jpeg", buffer: await bigJpeg(page, 2400, 1800) });
  await expect.poll(() => state.puts.length).toBe(1);
  expect(state.puts[0].player).toBe("p1");
  const body = state.puts[0].body;
  expect(Math.max(...Object.values(jpegSize(body.subarray(body.indexOf(Buffer.from([0xff, 0xd8]))))))).toBeLessThanOrEqual(1200);
  await expect(bob.locator("img.avatar-photo")).toBeVisible();

  // Remove an inappropriate photo, after a confirmation.
  page.once("dialog", (dialog) => dialog.accept());
  await alice.getByRole("button", { name: "Remove photo" }).click();
  await expect.poll(() => state.deletes.length).toBe(1);
  expect(state.deletes[0].player).toBe("p0");
  await expect(alice.locator("img.avatar-photo")).toHaveCount(0);

  // Declining the confirmation changes nothing.
  page.once("dialog", (dialog) => dialog.dismiss());
  await bob.getByRole("button", { name: "Remove photo" }).click();
  expect(state.deletes).toHaveLength(1);
});

test("organizer: no photo controls after the game has ended", async ({ page }) => {
  await openGame(page, { role: "organizer", status: "ended", players: [{ id: "p0", name: "Alice", joinedAt: new Date().toISOString(), included: true, photoUrl: "/api/games/g1/photos/AAA" }] });
  await expect(page.getByRole("region", { name: "Player roster" })).toBeVisible();
  await expect(page.getByRole("button", { name: /photo/i })).toHaveCount(0);
});

test("board: photos fill the tiles, fall back to initials, dim when eliminated, and never shift the layout", async ({ page }) => {
  const mk = (over = {}) => ({
    gameId: "g1",
    name: "Photo board",
    status: "active",
    activeCount: 2,
    timer: null,
    players: [
      { id: "p0", number: 1, name: "Alice", status: "active", photoUrl: "/api/games/g1/photos/A" },
      { id: "p1", number: 2, name: "Bob", status: "active" },
      { id: "p2", number: 3, name: "Cy", status: "eliminated", photoUrl: "/api/games/g1/photos/C" },
      { id: "p3", number: 4, name: "Di", status: "active", photoUrl: "/api/games/g1/photos/EXPIRED" },
    ],
    ...over,
  });
  let data = mk();
  const sockets = await installBroker(page);
  const requests = [];
  page.on("request", (r) => requests.push({ path: new URL(r.url()).pathname, auth: !!r.headers().authorization }));
  let release;
  const gate = new Promise((resolve) => (release = resolve));
  await page.route("**/api/board/g1", (route) => route.fulfill({ contentType: "application/json", body: JSON.stringify(data) }));
  await page.route("**/api/games/g1/photos/*", async (route) => {
    await gate; // hold every photo until the layout has been measured
    if (route.request().url().endsWith("/EXPIRED")) return route.fulfill({ status: 404, contentType: "application/json", body: "{}" });
    return route.fulfill({ contentType: "image/png", body: PNG });
  });
  await page.goto("/#/board/g1");
  const tile = (name) => page.locator(`.tile[aria-label^="${name},"]`);
  await expect(page.locator(".tile")).toHaveCount(4);
  const boxes = async () => Promise.all([0, 1, 2, 3].map((i) => page.locator(".tile").nth(i).boundingBox()));
  const before = await boxes();
  // While photos are still loading, the initials show.
  await expect(tile("Alice").locator(".avatar-initials")).toBeVisible();

  release();
  await expect(tile("Alice").locator("img.avatar-photo")).toHaveAttribute("alt", "Alice");
  await expect(tile("Alice").locator("img.avatar-photo")).toHaveAttribute("loading", "lazy");
  await expect(tile("Cy").locator("img.avatar-photo")).toBeVisible();
  await expect(tile("Bob").locator("img.avatar-photo")).toHaveCount(0); // no photo: initials only
  // An expired or missing photo falls back to initials without a broken image.
  await expect(tile("Di").locator("img.avatar-photo")).toHaveCount(0);
  await expect(tile("Di").locator(".avatar-initials")).toBeVisible();
  // No layout shift: every tile keeps exactly its size and position.
  expect(await boxes()).toEqual(before);
  // An eliminated tile keeps its photo, dimmed, with the X over it.
  const face = await tile("Cy").locator(".tile-face").evaluate((el) => getComputedStyle(el).filter);
  expect(face).toContain("grayscale");
  await expect(tile("Cy")).toContainText("Eliminated");
  // Photos load for board viewers without credentials, and only the public endpoints are called.
  for (const r of requests.filter((r) => r.path.startsWith("/api/"))) {
    expect(r.path).toMatch(/^\/api\/(board\/[^/]+|mqtt|games\/g1\/photos\/[^/]+)$/);
    expect(r.auth).toBe(false);
  }

  // A photo added while the board is open appears live.
  data = mk({
    players: mk().players.map((p) => (p.id === "p1" ? { ...p, photoUrl: "/api/games/g1/photos/B" } : p)),
  });
  const topic = Buffer.from("boards/g1/updates");
  const body = Buffer.from(JSON.stringify({ gameId: "g1", type: "board.updated" }));
  const packet = Buffer.concat([Buffer.from([0x30, 2 + topic.length + body.length, 0, topic.length]), topic, body]);
  sockets.forEach((ws) => ws.send(packet));
  await expect(tile("Bob").locator("img.avatar-photo")).toBeVisible();
  expect(await boxes()).toEqual(before);
});
