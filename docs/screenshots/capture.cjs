// From the repository root: node docs/screenshots/capture.cjs
// Requires npm ci && npx playwright install chromium. Uses demo data only.
const { chromium, expect } = require("@playwright/test");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");
const { createHash } = require("node:crypto");

const root = resolve(__dirname, "../..");
const digest = name => "sha256:" + createHash("sha256").update(name).digest("hex");
const MiB = 1024 * 1024;
const activity = { lastPushedAt: "2026-11-06T08:30:00Z", lastPulledAt: "2026-11-06T09:15:00Z" };
const layers = [5, 18, 9].map((size, i) => ({
  digest: digest(`demo-layer-${i}`), size: size * MiB,
  mediaType: "application/vnd.oci.image.layer.v1.tar+gzip",
}));
const manifest = {
  schemaVersion: 2,
  mediaType: "application/vnd.oci.image.manifest.v1+json",
  config: { mediaType: "application/vnd.oci.image.config.v1+json", digest: digest("demo-config"), size: 2048 },
  layers,
};
const manifestBytes = Buffer.byteLength(JSON.stringify(manifest));
const imageDigest = digest(JSON.stringify(manifest));
const imageSize = 32 * MiB + 2048 + manifestBytes;
const repositories = [
  { name: "team/api", tags: ["latest", "v1.8.0", "stable"], sizeBytes: imageSize, pullCount: 1284 },
  { name: "team/worker", tags: ["latest", "v1.8.0"], sizeBytes: 24 * MiB, pullCount: 862 },
  { name: "platform/base", tags: ["alpine", "2026.11"], sizeBytes: 18 * MiB, pullCount: 2436 },
  { name: "team/docs", tags: ["latest", "v2.3.0"], sizeBytes: 8 * MiB, pullCount: 196 },
].map(repo => ({ ...repo, ...activity, imageCount: 1 }));
const fixtures = {
  "/api/session": { csrfToken: "demo-session" },
  "/api/repositories": {
    items: repositories, next: "",
    summary: { ...activity, imageCount: 4, sizeBytes: repositories.reduce((sum, repo) => sum + repo.sizeBytes, 0), pullCount: 4778 },
  },
  "/api/tags": {
    items: repositories[0].tags.map(name => ({
      name, digest: imageDigest, manifestBytes, sizeBytes: imageSize,
      ...activity, pullCount: 1284, platforms: [{ os: "linux", architecture: "amd64" }],
    })),
    next: "", repository: repositories[0],
  },
  "/api/manifest": { digest: imageDigest, manifestBytes, sizeBytes: imageSize, mediaType: manifest.mediaType, manifest, layers },
  "/api/tokens": { items: [
    { id: "demo-ci", name: "CI / release pipeline", grants: [{ repository: "team/api", permission: "write" }, { repository: "team/worker", permission: "write" }], createdAt: "2026-11-01T10:00:00Z" },
    { id: "demo-deploy", name: "Production / pull only", grants: [{ repository: "team/api", permission: "read" }], createdAt: "2026-11-02T10:00:00Z" },
  ] },
};

async function capture() {
  const browser = await chromium.launch();
  try {
    const page = await browser.newPage({ viewport: { width: 1280, height: 640 }, deviceScaleFactor: 2, colorScheme: "light", locale: "en-GB", timezoneId: "UTC" });
    const errors = [];
    page.on("pageerror", error => errors.push(error.message));
    await page.route("http://registry.example.test/**", route => {
      const pathname = new URL(route.request().url()).pathname;
      if (pathname.startsWith("/api/")) {
        if (route.request().method() !== "GET" || !fixtures[pathname]) return route.abort();
        return route.fulfill({ json: fixtures[pathname] });
      }
      const name = pathname === "/" ? "index.html" : pathname.slice(1);
      const types = { "index.html": "text/html", "app.js": "text/javascript", "style.css": "text/css", "chevron-down.svg": "image/svg+xml" };
      if (!types[name]) return route.abort();
      return route.fulfill({ contentType: types[name], body: readFileSync(resolve(root, "internal/web/static", name)) });
    });
    const screenshot = async name => {
      await expect(page.locator("#status")).toBeEmpty();
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true);
      await page.screenshot({ path: resolve(__dirname, name), animations: "disabled" });
    };

    await page.goto("http://registry.example.test/");
    await expect(page.getByRole("button", { name: "Open tags", exact: true })).toHaveCount(4);
    await screenshot("images.png");

    await page.getByRole("button", { name: "Open tags", exact: true }).first().click();
    await page.getByRole("button", { name: "Inspect", exact: true }).first().click();
    await expect(page.getByRole("dialog").getByRole("heading", { name: "Layers", exact: true })).toBeVisible();
    await page.emulateMedia({ colorScheme: "dark" });
    await screenshot("inspect.png");

    await page.getByRole("button", { name: "Close", exact: true }).click();
    await page.emulateMedia({ colorScheme: "light" });
    await page.getByRole("button", { name: "Tokens", exact: true }).click();
    await page.getByRole("button", { name: "Create token", exact: true }).click();
    await page.getByLabel("Name", { exact: true }).fill("Staging / deploy");
    await page.getByLabel("One grant per line: repository read|write").fill("team/api read\nteam/worker read");
    await screenshot("tokens.png");
    expect(errors).toEqual([]);
    console.log("Saved images.png, inspect.png and tokens.png (demo data).");
  } finally {
    await browser.close();
  }
}

capture().catch(error => { console.error(error); process.exitCode = 1; });
