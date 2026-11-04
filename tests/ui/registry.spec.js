const { test, expect } = require("@playwright/test");
const { readFileSync } = require("node:fs");
const { resolve } = require("node:path");

// Isolated UI fixtures: no running registry, credentials, or user data required.
async function openUI(page, signedIn = true) {
  const errors = [];
  page.on("pageerror", error => errors.push(error.message));
  await page.route("http://easycr.test/**", async route => {
    const url = new URL(route.request().url());
    if (url.pathname.startsWith("/api/")) {
      const fixtures = {
        "/api/session": signedIn ? { csrfToken: "ui-test-csrf" } : { error: { message: "Unauthorized" } },
        "/api/repositories": { items: [{ name: "team/app", tags: ["latest", "v1.0", "sha256-deadbeef"], imageCount: 1, sizeBytes: 2048, pullCount: 7 }, { name: "team/library", tags: [], imageCount: 1, sizeBytes: 1024, pullCount: 2 }], next: "", summary: { imageCount: 2, sizeBytes: 3072, pullCount: 9 } },
        "/api/tags": { items: [{ name: "latest", digest: "sha256:" + "a".repeat(64), manifestBytes: 123, sizeBytes: 2048, pullCount: 7, platforms: [] }], next: "", repository: { imageCount: 1, sizeBytes: 2048, pullCount: 7 } },
        "/api/manifest": { digest: "sha256:" + "a".repeat(64), manifestBytes: 123, sizeBytes: 2048, mediaType: "application/vnd.oci.image.manifest.v1+json", manifest: { schemaVersion: 2 }, layers: [{ digest: "sha256:" + "b".repeat(64), size: 1024, mediaType: "application/vnd.oci.image.layer.v1.tar+gzip" }] },
        "/api/tokens": { items: [{ id: "fixture-token", name: "CI read", grants: [{ repository: "team/app", permission: "read" }], createdAt: "2026-01-01T00:00:00Z" }] },
      };
      return route.fulfill({ status: !signedIn && url.pathname === "/api/session" ? 401 : 200, json: fixtures[url.pathname] || {} });
    }
    const name = url.pathname === "/" ? "index.html" : url.pathname.slice(1);
    if (!["index.html", "style.css", "app.js", "chevron-down.svg"].includes(name)) return route.abort();
    return route.fulfill({
      contentType: { "index.html": "text/html", "style.css": "text/css", "app.js": "text/javascript", "chevron-down.svg": "image/svg+xml" }[name],
      body: readFileSync(resolve(__dirname, "../../internal/web/static", name)),
    });
  });
  await page.goto("http://easycr.test/");
  return errors;
}

async function expectNoOverflow(page) {
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true);
}

test("repository list shows named tags without sha256 references", async ({ page }) => {
  await page.addInitScript(() => {
    Object.defineProperty(navigator, "clipboard", { configurable: true, value: {
      writeText: async value => { window.copiedImage = value; },
    } });
  });
  await openUI(page);
  const tags = page.getByLabel("Tags for team/app", { exact: true });
  await expect(tags.locator(".tag-badge")).toHaveText(["latest", "v1.0"]);
  await expect(tags).not.toContainText("sha256");
  await tags.getByRole("button", { name: "latest", exact: true }).click();
  expect(await page.evaluate(() => window.copiedImage)).toBe("easycr.test/team/app:latest");
  await expect(page.locator("#status")).toHaveText("Copied.");
  await expect(page.getByLabel("Tags for team/library", { exact: true })).toHaveText("No named tags");
  await page.getByRole("button", { name: "Open tags", exact: true }).first().click();
  await page.getByRole("button", { name: "Copy image", exact: true }).click();
  expect(await page.evaluate(() => window.copiedImage)).toBe("easycr.test/team/app:latest");
  await expect(page.locator("#status")).toHaveText("Copied.");
  await expectNoOverflow(page);
});

test("image statistics and layer inspection are visible", async ({ page }) => {
  const errors = await openUI(page);
  await expect(page.getByText(/2 images · 3.0 KiB · 9 pulls/)).toBeVisible();
  await page.getByRole("button", { name: "Open tags" }).first().click();
  await expect(page.getByRole("columnheader", { name: "Last push", exact: true })).toBeVisible();
  await expect(page.getByRole("cell", { name: "2.0 KiB", exact: true })).toBeVisible();
  await page.getByRole("button", { name: "Inspect", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog.getByRole("heading", { name: "Layers", exact: true })).toBeVisible();
  await expect(dialog.getByText("sha256:" + "b".repeat(64), { exact: true })).toBeVisible();
  await expect(dialog.getByRole("cell", { name: "1.0 KiB", exact: true })).toBeVisible();
  await expectNoOverflow(page);
  await dialog.getByText("Raw manifest JSON", { exact: true }).click();
  await expect(dialog.locator("pre")).toContainText('"schemaVersion": 2');
  expect(errors).toEqual([]);
});

test("empty repository message has balanced inner spacing", async ({ page }) => {
  await openUI(page);
  await page.getByLabel("Filter loaded repositories").fill("missing-repository");
  const message = page.locator(".list > p");
  await expect(message).toBeVisible();
  await expect(message).toHaveCSS("margin", "0px");
  await expect(message).toHaveCSS("padding", "10px");
  await expectNoOverflow(page);
});

test("technical console stays compact and respects touch targets", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 800 });
  await openUI(page);
  await expect(page.locator("html")).toHaveCSS("font-size", "12px");
  await expect(page.locator("body")).toHaveCSS("font-family", /monospace/);
  await expect(page.getByRole("heading", { name: "Images", exact: true })).toHaveCSS("font-size", "16px");
  const row = page.locator(".row").first();
  const coarsePointer = await page.evaluate(() => matchMedia("(pointer: coarse)").matches);
  expect((await row.boundingBox()).height).toBeLessThanOrEqual(coarsePointer ? 100 : 84);
  if (coarsePointer) {
    expect((await row.getByRole("button", { name: "Open tags", exact: true }).boundingBox()).height).toBeGreaterThanOrEqual(40);
  }
  await expect(page.getByRole("button", { name: "Images", exact: true })).toHaveAttribute("aria-pressed", "true");
  await expectNoOverflow(page);
});

for (const colorScheme of ["light", "dark"]) {
  test(`compact console supports ${colorScheme} and narrow screens`, async ({ page }) => {
    await page.emulateMedia({ colorScheme });
    await page.setViewportSize({ width: 360, height: 740 });
    const errors = await openUI(page);
    await expectNoOverflow(page);
    await page.getByRole("button", { name: "Open tags" }).first().click();
    await expect(page.getByRole("columnheader", { name: "Tag / digest" })).toBeVisible();
    await expectNoOverflow(page);
    await page.getByRole("button", { name: "Create tag", exact: true }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
    await expectNoOverflow(page);
    await page.keyboard.press("Escape");
    await page.getByRole("button", { name: "Tokens", exact: true }).click();
    await page.getByRole("button", { name: "Create token", exact: true }).click();
    await expectNoOverflow(page);
    expect(errors).toEqual([]);
  });
}

test("login uses native form validation", async ({ page }) => {
  const errors = await openUI(page, false);
  await expect(page.getByRole("heading", { name: "Registry console" })).toBeVisible();
  await expect(page.locator("body")).toHaveCSS("font-family", /monospace/);
  await expect(page.locator("header")).toHaveCSS("display", "flex");
  await page.getByRole("button", { name: "Sign in", exact: true }).click();
  expect(await page.getByLabel("Admin password").evaluate(input => input.validity.valueMissing)).toBe(true);
  await expect(page.locator("#status")).toBeEmpty();
  await expect(page.getByLabel("Admin password")).toBeFocused();
  await expectNoOverflow(page);
  expect(errors).toEqual([]);
});

test("repository filtering, tag table, destructive dialog and checkbox", async ({ page }) => {
  const errors = await openUI(page);
  await expect(page.getByRole("heading", { name: "Images", exact: true })).toBeVisible();
  await page.getByLabel("Filter loaded repositories").fill("library");
  await expect(page.getByRole("button", { name: "Open tags" })).toHaveCount(1);
  await page.getByLabel("Filter loaded repositories").fill("app");
  await page.getByRole("button", { name: "Open tags" }).click();
  await expect(page.getByRole("columnheader", { name: "Tag / digest" })).toBeVisible();
  await page.getByRole("button", { name: "Create tag", exact: true }).click();
  await page.getByLabel("Allow replacing an existing tag").check();
  await expect(page.getByLabel("Allow replacing an existing tag")).toBeChecked();
  await page.getByRole("button", { name: "Close", exact: true }).click();
  await page.getByRole("button", { name: "Delete repository", exact: true }).click();
  const dialog = page.getByRole("dialog");
  await expect(dialog).toBeVisible();

  await expect(dialog.getByRole("button", { name: "Delete repository", exact: true })).toBeDisabled();
  await dialog.getByLabel("Type team/app to confirm").fill("team/app");
  await expect(dialog.getByRole("button", { name: "Delete repository", exact: true })).toBeEnabled();
  await expectNoOverflow(page);
  await page.keyboard.press("Escape");
  await expect(dialog).toHaveCount(0);
  expect(errors).toEqual([]);
});

test("token scope uses native selects", async ({ page }) => {
  const errors = await openUI(page);
  await page.getByRole("button", { name: "Tokens", exact: true }).click();
  await page.getByRole("button", { name: "Create token", exact: true }).click();
  const scope = page.getByRole("combobox", { name: "Scope", exact: true });
  const actions = page.locator(".dialog-actions");
  const close = await actions.getByRole("button", { name: "Close", exact: true }).boundingBox();
  const create = await actions.getByRole("button", { name: "Create token", exact: true }).boundingBox();
  const bounds = await actions.boundingBox();
  expect(close.y).toBe(create.y);
  expect(create.x + create.width).toBeCloseTo(bounds.x + bounds.width, 0);
  await expect(scope).toHaveCSS("padding-right", "30px");
  await expect(scope).toHaveCSS("background-position", "calc(100% - 9px) 50%");
  await scope.selectOption("global");
  await expect(scope).toHaveValue("global");
  await expect(page.getByLabel("One grant per line: repository read|write")).toBeHidden();
  await expect(page.getByRole("combobox", { name: "Global permission" })).toBeVisible();
  await scope.selectOption("scoped");
  await expect(page.getByLabel("One grant per line: repository read|write")).toBeVisible();
  await expect(page.getByRole("combobox", { name: "Global permission" })).toBeHidden();
  await expectNoOverflow(page);
  expect(errors).toEqual([]);
});

test("native confirmation can cancel destructive actions", async ({ page }) => {
  await openUI(page);
  await page.getByRole("button", { name: "Tokens", exact: true }).click();
  const confirmation = page.waitForEvent("dialog");
  const click = page.getByRole("button", { name: "Revoke", exact: true }).click();
  const dialog = await confirmation;
  expect(dialog.type()).toBe("confirm");
  expect(dialog.message()).toContain("Revoke token");
  await dialog.dismiss();
  await click;
  await expect(page.getByRole("heading", { name: "CI read" })).toBeVisible();
  await expect(page.locator("dialog")).toHaveCount(0);
});
