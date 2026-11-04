# EasyCR

A private, single-admin OCI / Docker registry in one Go binary, with an embedded
Images and Tokens UI, local filesystem storage, and offline garbage collection.
No Node build, external database, or separate registry process is required to run
or build the Go binary. The embedded UI uses native HTML controls and plain CSS.

Registry HTTP requests currently have a one-hour maximum duration, including
lock wait time; split larger uploads into resumable chunks. Image mutations are
serialized globally, so a slow transfer can delay other repositories until it
completes or reaches that limit. Upload signing state is persisted so resumable
upload URLs remain valid after restart. This is not per-repository concurrency
isolation or an idle-progress timeout.

## Run locally

Requires Go 1.26+ (tested toolchain: 1.26.5).

```sh
go build -trimpath -o bin/easycr ./cmd/easycr
./bin/easycr
```

Open <http://localhost:5000>. UI mutations require an Origin matching the request
Host and a session CSRF token. No password or token is supplied as a CLI argument.

The first `serve` startup creates and commits a bcrypt admin password hash,
then prints one `bootstrap_admin_password password=...` event to stderr **before
opening the HTTP listener**. No HTTP request or UI visit triggers generation.
Restrict access to these logs. Restarting with the same data directory keeps the
password and does not print it again. If the event is lost, use offline reset.
The UI never returns the admin password. Bootstrap or DB errors prevent listening.
If binding fails after bootstrap, the password remains persisted: reuse the log
or explicitly reset it rather than expecting another bootstrap event.

## Configuration

No environment variables, configuration file, or configuration flags are needed.
Run `./bin/easycr` (or `serve`): it listens on `0.0.0.0:5000` and stores data
in `./data` relative to the working directory. The Docker image works from `/`,
so its data directory is `/data`. Change the external port with Docker mapping.
Optionally set `PORT` to change the application's listening port (default 5000,
valid range 1–65535): `PORT=8080 ./bin/easycr` or
`docker run -e PORT=8080 -p 8080:8080 -v easycr_data:/data easycr:local`.
Use `gc`, `gc-dry-run`, or `admin reset-password` for offline maintenance.

## Make targets

Run `make help` to list commands. Common targets:

```sh
make build
make run
make verify
make test-e2e     # requires a working Docker daemon/network
make docker-build IMAGE=easycr:local
make docker-push IMAGE=registry.example.com/team/easycr:v1
make docker-release IMAGE=registry.example.com/team/easycr:v1 # build then push
```

`make run` starts the server on port 5000 with local `./data` storage.
`make clean` removes the default binary, never registry data.

## UI development

The UI uses native buttons, selects, checkboxes, form validation, dialogs and
confirmation prompts. Plain CSS provides minimal layout and follows the system
light/dark preference. There is no UI framework, CDN, or CSS build step.
Edit `internal/web/static/index.html`, `app.js`, and `style.css`, then run
`make build` to embed the assets. Node.js and npm are only needed for optional
browser regression tests:

```sh
npm ci
npx playwright install chromium
make test-ui       # isolated UI fixtures, desktop and mobile Chromium
```

The browser tests use explicit mocked API fixtures and do not exercise a live
registry; `make test` covers the Go API and embedded asset handler.

## Docker image

### GitHub Container Registry

`.github/workflows/publish-image.yml` builds and pushes a multi-platform image
(`linux/amd64` and `linux/arm64`) to `ghcr.io/<owner>/<repository>` (lowercase).
It runs on pushes to `main`, tags matching `v*`, or manually from the Actions tab
on either of those refs. Tags are:

- `latest` for `main` builds only.
- The exact Git tag for releases, e.g. `v1.0.0`.
- `sha-<full-commit-sha>` for every build.

The workflow uses the built-in `GITHUB_TOKEN` with `packages: write`; no PAT or
additional repository secret is required. Repository/organization policy must
allow Actions to publish packages. New GHCR packages are private by default;
change package visibility to public if anonymous pulls are wanted. For an
existing package, grant this repository Actions write access if needed.

```sh
docker pull ghcr.io/<owner>/<repository>:latest
```

### Local build

`make docker-build IMAGE=easycr:local` builds locally. To publish, authenticate
with your destination registry first, then run `make docker-release
IMAGE=registry.example.com/team/easycr:v1`. This builds before pushing; `make
docker-push IMAGE=...` pushes an already-built image without rebuilding.

```sh
docker run -d --name easycr -p 127.0.0.1:5000:5000 -v easycr_data:/data easycr:local
docker logs easycr
```

The container defaults to UID/GID 65532. Compose `user: "1000:1000"` or
`docker run --user 1000:1000` overrides that default without rebuilding.
Provision the mounted `/data` directory with ownership matching the selected
UID:GID and mode 0700 before starting. A new named volume inherits the image's
65532:65532 ownership, not a custom runtime user; existing volumes retain their
ownership. Distroless has no shell or startup chown, so overriding the user alone
does not fix volume permissions. Never use world-writable permissions as a fix.
Never run two instances on one data directory. Removing the container
does not delete the named volume; deleting the volume destroys registry data.
The image uses a Go 1.26.5 builder and `gcr.io/distroless/static-debian12:nonroot`
runtime, with a static binary and no shell. `GET /healthz` returns `{"status":"ok"}` without private data.

For public use, terminate HTTPS at a reverse proxy. No public URL configuration
is needed. Keep the backend on loopback or a
private Docker network. Forward request bodies without buffering large blobs,
allow long uploads, and preserve the Host header. EasyCR does not trust
X-Forwarded-For for login rate limits. The application serves HTTP internally;
Never publish this plaintext backend directly to the Internet. A validated HTTPS
Origin (or direct TLS) causes the session cookie to use Secure; forwarded headers
are not trusted. The proxy must preserve the original Host and Origin headers. No SSO, scanning, replication, object
storage, multi-replica operation, automatic retention, or NFS support is included.

## Push and pull

Sign into the UI, choose **Tokens → Create token**, and grant `team/app write`.
Multiple exact repository grants are supported; `*` is an explicit global grant.
`write` includes `read`. Repository grants do not include descendants or prefixes.
Tokens have no automatic expiry; revoke and replace them to rotate access.

Save the secret when shown: it cannot be retrieved from the token list. Set
`EASYCR_TOKEN` through your secret manager or a shell input prompt, then use:

```sh
printf '%s' "$EASYCR_TOKEN" | docker login localhost:5000 -u token --password-stdin
docker tag my-local-image:latest localhost:5000/team/app:v1
docker push localhost:5000/team/app:v1
docker pull localhost:5000/team/app:v1
```

Username must be exactly `token`. There is no anonymous pull. A scoped token
cannot list the registry catalog or access another repository. Global read/write
tokens can list the catalog; Docker tokens cannot delete images or use the admin
API. Revocation applies to the next request, including cached Docker credentials;
a request already authorized may finish. The admin password and UI sessions do
not authenticate `/v2/`. Cross-repository mounts require source read and target
write permissions. Docker Desktop or remote daemons must be able to reach the
registry hostname; loopback examples assume a local development daemon.

## Image management

The UI shows compressed image sizes, repository sizes, unique tagged image counts,
last push/pull times, and cumulative manifest pull requests. Sizes deduplicate
shared blobs within the image/repository (and globally in the registry summary),
include manifests/configs, and exclude untagged/orphaned data. They are not the
unpacked image size or reclaimable filesystem space. External layers not stored
locally are excluded. Image counts deduplicate tagged digests within each repo;
the global count sums those per-repository counts.

Activity is persisted in metadata.db starting with this version; historical times
are unknown and shown as a dash. A pull means a successful authenticated manifest
GET, excluding HEAD, blob downloads and admin inspection. Multi-platform clients
may fetch both an index and a child manifest, so this is not a count of completed
Docker pulls. Tags sharing a digest share image activity. Repository/global pull
totals retain historical requests after tags are removed. UI retagging is not a
protocol push. Metrics persistence failures are logged rather than failing an
already completed registry transfer.

Inspect displays ordered layer digests, compressed sizes and media types, plus
expandable raw manifest JSON. For a multi-platform index, open a child manifest
to inspect its layers. No layer archive is downloaded or extracted for inspection.

Push creates repositories. There are no empty repositories to create in the UI.
The UI lists repositories and tags with pagination, digest, media type,
`manifestBytes`, platforms, and manifest JSON. **manifestBytes is only the manifest
JSON size**, not total image size or reclaimable disk space.

Create or replace a tag from a digest already present in the same repository.
Replacing a tag requires explicit overwrite confirmation. Deleting one tag
removes only that tag; other tags at the same digest stay usable. Deleting a
repository removes its references; blobs shared with other repositories remain.
Repository deletion requires typing its exact name in the UI. Nested repositories
must be deleted first: the API returns 409 when descendants exist, because the
pinned Distribution public remover removes a directory tree.

Untagged manifests may still be pulled by digest **before** GC. Availability after
GC is not guaranteed. Deletion does not immediately reclaim physical storage.

## Offline maintenance and recovery

All commands acquire the same exclusive bbolt lock with a one-second timeout.
While the server is running, GC and reset exit nonzero with
`data directory is in use`; locks are never forcibly removed. Stop the server and
make a complete backup first. GC is destructive and does not provide rollback.

```sh
# Stop the binary (Ctrl-C or SIGTERM), then:
cp -a ./data ./data-backup
./bin/easycr gc-dry-run
./bin/easycr gc
./bin/easycr
```

GC uses Distribution mark-and-sweep with RemoveUntagged. The test fixture checks
live OCI index children, their configs/layers, cross-repository sharing, orphan
reclamation, and byte-for-byte nonmutating dry-run. There is no background GC or
upload purger. Abandoned incomplete upload directories are not a retention system
and are not automatically cleaned by this MVP.

Reset a lost password offline (prints a new password once to stderr):

```sh
./bin/easycr admin reset-password
```

Reset invalidates every UI session and preserves Docker tokens and images.
It requires an existing bootstrapped store and does not repair corrupt metadata.

Stop the container and run maintenance in a one-off container sharing
its named volume:

```sh
docker stop easycr
docker run --rm -v easycr_data:/data easycr:local gc-dry-run
docker run --rm -v easycr_data:/data easycr:local gc
# If recovery is needed instead:
docker run --rm -v easycr_data:/data easycr:local admin reset-password
docker start easycr
```

Back up the **entire** data directory while stopped: `metadata.db` plus `registry/`.
For a named volume, use a local backup tool/container that preserves ownership;
keep backup files private. Restore the complete backup into a separate empty
directory/volume, preserving ownership and permissions. Start with that restored
data and verify the original admin login, token login, and image pull before
switching storage. Do not copy just the DB or just the blobs. New directories use
0700; metadata.db uses 0600. Corrupt/missing metadata buckets and invalid bootstrap
schema fail closed instead of being treated as a new deployment.

## API and security

The local contract is in [docs/openapi.yaml](docs/openapi.yaml). A local Postman
collection is in [postman/collections/easycr.json](postman/collections/easycr.json).
The registry protocol is implemented by pinned Distribution v3.1.2; the admin API
uses session cookies, a 12-hour expiry, and an in-memory CSRF token bound to the
session. Mutations require both exact Origin and X-CSRF-Token; login also requires
Origin. Cookies are HttpOnly and SameSite=Strict. Sessions and Docker credentials
are persisted as hashes; token secrets are returned only at creation. Admin
sessions are invalidated by logout or offline reset. The API rejects unknown JSON
fields, trailing JSON, and bodies larger than 64 KiB. Login allows 5 attempts per
real remote IP per minute and 60 globally, with Retry-After and a bounded map.
API responses use no-store; static UI uses a restrictive CSP and no CDN assets.
Secrets are not stored in localStorage or rendered using HTML insertion.

Repository names use query parameters (e.g. `repository=team%2Fapp`), not wildcard
admin paths. A `read` token has no delete permission, and unknown Distribution
resource/actions are denied. The HTTP server has a 10-second header timeout and
60-second idle timeout, with no short body timeout for streaming uploads.

Pinned direct dependencies: Distribution v3.1.2, bbolt v1.5.0, x/crypto v0.58.0;
reference v0.6.0, go-digest v1.0.0, image-spec v1.1.1 (see go.mod/go.sum).

## Verification

```sh
go mod verify
go vet ./...
go test -race ./... -count=1
go build -trimpath -o bin/easycr ./cmd/easycr
EASYCR_E2E=1 go test ./tests/e2e -v -count=1
make docker-build
```

Docker e2e is opt-in. When opted in, a missing/inaccessible daemon is a **failure**,
not a skip. It builds a FROM scratch fixture locally (no public base image), uses
a temporary DOCKER_CONFIG, checks real login/push/pull, read/write/foreign/global
policy, persistence, cached-credential revocation, and GC. It removes the local
tag before pull. Cleanup targets only test-owned images and temp directories,
never `docker system prune` or the user's Docker configuration.

Browser acceptance: use disposable data; observe the one bootstrap event before
opening the UI; test wrong/correct login, reload persistence and logout; push a
nested-name repository; inspect manifest and copy commands; create alias and
verify original-tag deletion preserves alias; cancel each destructive operation;
require the exact repository deletion name; create scoped/global tokens, dismiss
the one-time secret and verify it cannot be retrieved; revoke and check Docker
failure; navigate forms with the keyboard; use `<img src=x onerror=alert(1)>` as a
token name and verify it renders as literal text. Test empty, loading, error, and
success states separately. See [docs/verification.md](docs/verification.md) for
results actually observed in the implementation environment and remaining gates.
