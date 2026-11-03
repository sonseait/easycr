"use strict";
(() => {
  const app = document.querySelector("#app");
  const nav = document.querySelector("#navigation");
  const notice = document.querySelector("#status");
  let csrf = "";
  let generation = 0;
  let current = "images";
  let repositories = [];
  let repoCursor = "";
  let tags = [];
  let tagCursor = "";
  let activeRepository = "";

  function el(tag, text, attributes = {}) {
    const node = document.createElement(tag);
    if (text !== null && text !== undefined) node.textContent = text;
    for (const [key, value] of Object.entries(attributes)) node.setAttribute(key, value);
    return node;
  }
  function status(text = "", kind = "") {
    notice.textContent = text;
    notice.className = kind;
    document.querySelectorAll("dialog .dialog-status").forEach(node => {
      node.textContent = text; node.className = "dialog-status " + kind;
    });
  }
  function button(label, action, kind = "") {
    const b = el("button", label, { type: "button", class: kind });
    b.addEventListener("click", () => work(b, action));
    return b;
  }
  async function work(control, action) {
    control.disabled = true;
    try { await action(); } catch (error) { status(error.message, "error"); }
    finally { control.disabled = false; }
  }
  async function api(path, method = "GET", body) {
    const requestGeneration = generation;
    const headers = {};
    if (method !== "GET") headers["X-CSRF-Token"] = csrf;
    if (body !== undefined) headers["Content-Type"] = "application/json";
    let response;
    try {
      response = await fetch(path, { method, credentials: "same-origin", headers,
        body: body === undefined ? undefined : JSON.stringify(body) });
    } catch (_) { throw new Error("Could not reach EasyCR. Check the connection and retry."); }
    const data = response.status === 204 ? null : await response.json();
    if (requestGeneration !== generation && path !== "/api/login") {
      throw new Error("Session changed. Sign in again to continue.");
    }
    if (!response.ok) {
      if (response.status === 401 && path !== "/api/login") loginView();
      const error = new Error(data?.error?.message || `Request failed (${response.status}).`);
      error.status = response.status;
      throw error;
    }
    return data;
  }
  function field(form, title, type = "text", value = "", required = true) {
    const label = el("label", title);
    const input = el("input", null, { type });
    input.value = value;
    input.required = required;
    label.append(input); form.append(label);
    return input;
  }
  function submit(form, label, action) {
    const b = el("button", label, { type: "submit", class: "primary" });
    form.append(b);
    form.addEventListener("submit", event => {
      event.preventDefault();
      work(b, action);
    });
    return b;
  }
  function modal(title) {
    const dialog = el("dialog");
    const heading = el("h2", title);
    dialog.setAttribute("aria-label", title);
    dialog.append(heading, el("div", null, { class: "dialog-status", role: "status", "aria-live": "polite" }));
    const body = el("div"); dialog.append(body);
    const close = button("Close", () => dialog.close());
    dialog.append(close);
    dialog.addEventListener("close", () => {
      dialog.remove();
    }, { once: true });
    document.body.append(dialog); dialog.showModal();
    return { dialog, body, close };
  }

  async function copy(value) {
    try { await navigator.clipboard.writeText(value); status("Copied.", "success"); }
    catch (_) { throw new Error("Clipboard is unavailable. Select and copy the displayed text."); }
  }
  function command(parent, value) {
    const line = el("div", null, { class: "command" });
    const input = el("input", null, { readonly: "", "aria-label": "Docker command" });
    input.value = value;
    line.append(input, button("Copy", () => copy(value)));
    parent.append(line);
  }
  function loginView() {
    generation++; csrf = ""; repositories = []; tags = []; activeRepository = "";
    document.querySelectorAll("dialog").forEach(d => d.remove());
    nav.replaceChildren(); app.replaceChildren();
    const panel = el("section", null, { class: "login" });
    panel.append(el("h1", "Registry console"), el("p", "Sign in with the admin password from your first deployment log."));
    const form = el("form");
    const password = field(form, "Admin password", "password");
    password.autocomplete = "current-password";
    submit(form, "Sign in", async () => {
      status("Signing in…");
      const session = await api("/api/login", "POST", { password: password.value });
      password.value = ""; csrf = session.csrfToken;
      status(); navigation(); await images();
    });
    panel.append(form, el("p", "Lost access? Stop the server, then run easycr admin reset-password in the same working directory.", { class: "muted recovery" }));
    app.append(panel); password.focus();
  }
  function navigation() {
    const imageTab = button("Images", () => images());
    const tokenTab = button("Tokens", () => tokensView());
    imageTab.setAttribute("aria-pressed", String(current === "images"));
    tokenTab.setAttribute("aria-pressed", String(current === "tokens"));
    nav.replaceChildren(imageTab, tokenTab, button("Sign out", async () => {
      await api("/api/logout", "POST"); loginView(); status("Signed out.");
    }));
  }
  async function images(append = false) {
    current = "images"; navigation(); status("Loading repositories…");
    const data = await api(`/api/repositories?n=50&last=${encodeURIComponent(append ? repoCursor : "")}`);
    repositories = append ? repositories.concat(data.items) : data.items;
    repoCursor = data.next; activeRepository = "";
    app.replaceChildren(el("h1", "Images"), el("p", "Your private image library. Push with Docker to create a repository."));
    const label = el("label", "Filter loaded repositories");
    const filter = el("input", null, { type: "search", placeholder: "team/app" });
    label.append(filter); app.append(label);
    const list = el("div", null, { class: "list" });
    function draw() {
      list.replaceChildren();
      const filtered = repositories.filter(repo => repo.name.includes(filter.value));
      if (!filtered.length) list.append(el("p", repositories.length ? "No matching repositories on the loaded pages." : "No images yet. Create a write token, then docker login and docker push."));
      for (const repo of filtered) {
        const row = el("div", null, { class: "row" });
        row.append(el("code", repo.name), button("Open tags", () => repositoryView(repo.name)));
        list.append(row);
      }
    }
    filter.addEventListener("input", draw); draw(); app.append(list);
    if (repoCursor) app.append(button("Load more repositories", () => images(true)));
    status();
  }
  async function repositoryView(repo, append = false) {
    status("Loading tags…");
    const data = await api(`/api/tags?repository=${encodeURIComponent(repo)}&n=50&last=${encodeURIComponent(append ? tagCursor : "")}`);
    tags = append && activeRepository === repo ? tags.concat(data.items) : data.items;
    tagCursor = data.next; activeRepository = repo;
    app.replaceChildren(button("← Repositories", () => images()), el("h1", repo));
    app.append(el("p", "manifestBytes is the size of the manifest JSON, not total image size or reclaimable disk space.", { class: "muted" }));
    app.append(button("Delete repository", () => removeRepository(repo), "danger"));
    if (!tags.length) app.append(el("p", "This repository has no tags. Untagged digests may still be readable until offline GC."));
    const table = el("table");
    const header = el("tr");
    for (const text of ["Tag / digest", "Manifest bytes", "Platforms", "Actions"]) header.append(el("th", text, { scope: "col" }));
    const thead = el("thead"); thead.append(header); table.append(thead);
    const tbody = el("tbody");
    for (const tag of tags) {
      const row = el("tr"); const name = el("td");
      name.append(el("strong", tag.name), el("code", tag.digest, { class: "digest" }));
      const actions = el("td", null, { class: "actions" });
      actions.append(button("Inspect", () => inspect(repo, tag.name)), button("Copy pull", () => copy(`docker pull ${location.host}/${repo}:${tag.name}`)),
        button("Create tag", () => retag(repo, tag.digest)), button("Delete tag", async () => {
          if (!confirm(`Delete tag ${repo}:${tag.name}? Other tags remain. This digest may become unavailable after offline GC.`)) return;
          status("Deleting tag…"); await api(`/api/tag?repository=${encodeURIComponent(repo)}&tag=${encodeURIComponent(tag.name)}`, "DELETE");
          await repositoryView(repo); status("Tag deleted. Run offline GC later to reclaim unused data.", "success");
        }, "danger"));
      row.append(name, el("td", String(tag.manifestBytes)), el("td", tag.platforms.map(p => `${p.os}/${p.architecture}${p.variant ? "/" + p.variant : ""}`).join(", ") || "Single manifest"), actions);
      tbody.append(row);
    }
    table.append(tbody); const scroll = el("div", null, { class: "table-scroll" }); scroll.append(table); app.append(scroll);
    if (tagCursor) app.append(button("Load more tags", () => repositoryView(repo, true)));
    status();
  }
  async function inspect(repo, ref) {
    status("Loading manifest…");
    const info = await api(`/api/manifest?repository=${encodeURIComponent(repo)}&reference=${encodeURIComponent(ref)}`);
    const { body } = modal(`${repo}:${ref}`);
    body.append(el("p", `${info.mediaType} · ${info.manifestBytes} manifest bytes`), el("pre", JSON.stringify(info.manifest, null, 2)));
    command(body, `docker pull ${location.host}/${repo}@${info.digest}`); status();
  }
  function retag(repo, digest) {
    const { dialog, body } = modal("Create or replace tag");
    body.append(el("p", `Point a tag in ${repo} to the existing manifest ${digest}.`));
    const form = el("form"); const tag = field(form, "Tag name");
    const label = el("label", "Allow replacing an existing tag");
    const overwrite = el("input", null, { type: "checkbox" }); label.prepend(overwrite); form.append(label);
    submit(form, "Save tag", async () => {
      if (overwrite.checked && !confirm(`Replace ${repo}:${tag.value} if it already exists?`)) return;
      status("Saving tag…");
      await api(`/api/tag?repository=${encodeURIComponent(repo)}&tag=${encodeURIComponent(tag.value)}`, "PUT", { sourceDigest: digest, overwrite: overwrite.checked });
      dialog.close(); await repositoryView(repo); status("Tag saved.", "success");
    });
    body.append(form); tag.focus();
  }
  function removeRepository(repo) {
    const { dialog, body } = modal("Delete repository");
    body.append(el("p", `Delete all references in ${repo}. Other repositories remain. Remove any nested repositories first.`), el("p", "Disk space is reclaimed only by offline GC. Untagged digests are not guaranteed to remain available after GC."));
    const form = el("form"); const name = field(form, `Type ${repo} to confirm`);
    const b = submit(form, "Delete repository", async () => {
      if (name.value !== repo) throw new Error("Repository name does not match.");
      status("Deleting repository…"); await api(`/api/repository?repository=${encodeURIComponent(repo)}`, "DELETE");
      dialog.close(); await images(); status("Repository deleted. Unused blobs await offline GC.", "success");
    });
    b.className = "danger"; b.disabled = true;
    name.addEventListener("input", () => { b.disabled = name.value !== repo; });
    body.append(form); name.focus();
  }
  async function tokensView() {
    current = "tokens"; navigation(); status("Loading tokens…");
    const data = await api("/api/tokens");
    app.replaceChildren(el("h1", "Tokens"), el("p", "Scoped credentials for Docker. Write includes read; admin access is never granted."), button("Create token", createToken, "primary"));
    if (!data.items.length) app.append(el("p", "No tokens yet. Create one for a repository or the entire registry."));
    const list = el("div", null, { class: "list" });
    for (const token of data.items) {
      const row = el("section", null, { class: "row" }); const info = el("div");
      info.append(el("h2", token.name), el("p", token.grants.map(g => `${g.repository} · ${g.permission}`).join(", ")), el("small", `Created ${new Date(token.createdAt).toLocaleString()}`));
      row.append(info, button("Revoke", async () => {
        if (!confirm(`Revoke token “${token.name}”? New Docker requests will immediately lose access.`)) return;
        status("Revoking token…"); await api(`/api/tokens/${encodeURIComponent(token.id)}`, "DELETE");
        await tokensView(); status("Token revoked.", "success");
      }, "danger")); list.append(row);
    }
    app.append(list); status();
  }
  function createToken() {
    const { dialog, body, close } = modal("Create token");
    const form = el("form"); const name = field(form, "Name"); name.maxLength = 200;
    const scopeLabel = el("label", "Scope"); const scope = el("select");
    scope.append(el("option", "Exact repositories", { value: "scoped" }), el("option", "Entire registry (*)", { value: "global" }));
    scopeLabel.append(scope); form.append(scopeLabel);
    const grantsLabel = el("label", "One grant per line: repository read|write");
    const grants = el("textarea", null, { rows: "4", placeholder: "team/app write\nteam/library read" }); grants.required = true;
    grantsLabel.append(grants); form.append(grantsLabel);
    const permLabel = el("label", "Global permission"); const permission = el("select");
    permission.append(el("option", "Read", { value: "read" }), el("option", "Write (includes read)", { value: "write" }));
    permLabel.append(permission); permLabel.hidden = true; form.append(permLabel);
    scope.addEventListener("change", () => { const global = scope.value === "global"; grantsLabel.hidden = global; grants.required = !global; permLabel.hidden = !global; });
    const create = submit(form, "Create token", async () => {
      let requested;
      if (scope.value === "global") requested = [{ repository: "*", permission: permission.value }];
      else requested = grants.value.trim().split(/\n/).map(line => {
        const parts = line.trim().split(/\s+/);
        if (parts.length !== 2 || !["read", "write"].includes(parts[1]) || parts[0] === "*") throw new Error("Each line needs an exact repository and read or write. Choose Entire registry for global access.");
        return { repository: parts[0], permission: parts[1] };
      });
      status("Creating token…"); const result = await api("/api/tokens", "POST", { name: name.value, grants: requested });
      dialog.close();
      let refreshFailed = false;
      try { await tokensView(); } catch (_) { refreshFailed = true; }
      const oneTime = modal("Save your token now");
      oneTime.body.append(el("p", "This secret is shown only once. Closing this dialog removes it from the page. If lost, revoke the token and create a replacement."));
      const secretLabel = el("label", "Token secret"); const secret = el("input", null, { type: "password", readonly: "", autocomplete: "off" });
      secret.value = result.secret; secretLabel.append(secret); oneTime.body.append(secretLabel);
      oneTime.body.append(button("Copy token", () => copy(secret.value)), button("Show / hide", () => { secret.type = secret.type === "password" ? "text" : "password"; }));
      command(oneTime.body, `printf '%s' "$EASYCR_TOKEN" | docker login ${location.host} -u token --password-stdin`);
      oneTime.body.append(el("p", "Set EASYCR_TOKEN in your shell or secret manager. Do not place the token directly in command arguments."));
      oneTime.dialog.addEventListener("close", () => { secret.value = ""; result.secret = ""; }, { once: true });
      status(refreshFailed ? "Token created, but refreshing the list failed. Save this secret, then retry the list." : "Token created. Save the secret before closing.", refreshFailed ? "error" : "success");
    });
    const actions = el("div", null, { class: "dialog-actions" });
    actions.append(close, create); form.append(actions);
    body.append(form); name.focus();
  }
  (async () => {
    try { const session = await api("/api/session"); csrf = session.csrfToken; navigation(); await images(); }
    catch (error) { loginView(); if (error.status !== 401) status(error.message, "error"); }
  })();
})();
