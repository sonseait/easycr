# Implementation verification — 2026-11-04

Starting commit: `2cfea75`. All work is local. No push or external deployment was
performed. Generated passwords and tokens are not included in this report.

## Observed passing checks

Toolchain: Go 1.26.5, darwin/arm64. Commands used
`GOCACHE=/tmp/easycr-go-cache` because the managed workspace restricts the normal
user cache. This changes the cache location, not test behavior.

| Command | Actual result |
| --- | --- |
| `go test -race ./... -count=1` | Exit 0; cmd/easycr 8.868s, auth 16.636s, config 1.649s, httpapi 18.110s, registry 14.174s, state 2.711s, web 1.950s, tests/e2e 1.972s. No race reported. Docker tests opt out without EASYCR_E2E. testutil has no standalone tests. |
| `go vet ./...` | Exit 0 after fixing four unkeyed external grant literals in tests. |
| `go mod verify` | Exit 0: all modules verified. |
| `go mod tidy` | Exit 0; direct dependency declarations updated; versions remain pinned. |
| `go build -trimpath -o bin/easycr ./cmd/easycr` | Exit 0; native standalone binary built with embedded UI. |
| `GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -o /tmp/easycr-linux-amd64 ./cmd/easycr` | Exit 0; Linux static build succeeds. |
| `docker compose config --quiet` | Exit 0; Compose configuration resolves. |
| `node --check internal/web/static/app.js` | Exit 0; syntax only, not browser acceptance. Node is not part of the runtime/build. |
| `git diff --check` | Exit 0. |
| Strict YAML parse of docs/openapi.yaml using pinned go.yaml.in/yaml/v2 | Exit 0; OpenAPI 3.1.0, 11 paths parsed. This is syntax validation, not a full OpenAPI semantic validator. |

## Tests and fixes actually exercised

New tests were run against compiling placeholders and produced assertion failures
before implementation for sessions, tokens, exact/global permissions, Distribution
access records, repository paging, retagging, repository deletion, HTTP login and
CSRF boundaries, token API, security headers, login rate limiting, CLI listener
ordering and maintenance locks, and password reset. RED outputs were observed;
compile/setup failures were not counted as behavioral RED evidence.

Later failing regression tests exposed and drove fixes for:

- Lost metadata buckets being silently recreated on reopen.
- Invalid manifest digests incorrectly producing HTTP 500 instead of 400.
- Fresh-store GC failing on missing filesystem enumeration roots.
- A repository pushed only by digest incorrectly returning 404 from tag listing.
- Missing platform metadata for single-platform manifests.
- Schema-invalid sessions being accepted without their CSRF binding.
- Complete loss of admin settings being mistaken for a fresh deployment. A
  first-start marker is now explicitly consumed in the bootstrap transaction.

The Go tests drive the real Distribution v3.1.2 HTTP handler and filesystem driver,
not a mock registry. They cover OCI and Docker schema2 payload round trips, blob
uploads/downloads, exact repository isolation, read/write/global policies,
revocation, mount source and target permissions, retag overwrite conflicts,
untagging one alias without deleting another, nested repository removal protection,
shared blobs, and administrative image API pagination/statuses.

The GC fixture checks two untagged platform child manifests under a tagged OCI
index, their configs/layers, a separately tagged image in another repository,
and an orphan. Dry-run preserves all file bytes; real GC removes the orphan
manifest/blob and keeps live index children and shared data readable. This fixture
passed. The GC fixture was added after the adapter and passed initially; it is
coverage evidence rather than an asserted RED/GREEN cycle for that adapter.

Auth tests check persistent password/token/session recovery after DB reopen,
hash-only credential storage, fake-clock expiration, reset invalidation of all
sessions while retaining Docker tokens, failed-bootstrap silence, corrupt-state
rejection, and 20 concurrent bootstrap calls emitting one event. HTTP tests check
Origin, session-bound CSRF, strict JSON/body size, cookie flags, token/UI separation,
login throttling/expiry/global/bounded-map behavior, and private-data cache headers.

The CLI's listener injection proves bootstrap persistence and the single log event
precede listener creation. A net.Pipe listener serves actual HTTP through net/http,
including UI assets and the authenticated registry challenge, then proves graceful
shutdown releases the DB lock. This is **in-memory HTTP evidence**, not TCP evidence.
A stopped-data backup/restore test checks original password, token, and manifest.

## Blocked gates and exact failures

- `EASYCR_E2E=1 go test ./tests/e2e -v -count=1`: **exit 1**. Both
  `TestDockerWorkflow` and `TestContainerDeployment` fail at `docker info` with
  `permission denied while trying to connect to the docker API at
  unix:///Users/eric/.docker/run/docker.sock`. Neither test skips when opted in.
  No real Docker login/push/pull or nonroot container execution was verified.
- `docker compose build`: **exit 1**, initially unable to update restricted
  buildx activity state. Repeating with `BUILDX_CONFIG=/tmp/easycr-buildx` reaches
  the daemon socket and fails with the same permission denial. No container image
  build, builder tag pull, ownership copy-up, or volume restart is claimed verified.
- Starting the native binary on `127.0.0.1:5105` fails with
  `bind: operation not permitted`. Its disposable log showed the one bootstrap
  event before that failure; the generated value is withheld. Live TCP service and
  the browser checklist could not be exercised in this restricted session.
- Browser automation rejects execution because it requires approval and the
  session approval policy is `never`. No browser interaction, visual layout,
  accessibility, clipboard, focus, or destructive-dialog acceptance is claimed.
- `postman status`: exits nonzero with `ERROR: SecItemCopyMatching failed -50`.
  A local OpenAPI contract and Postman collection are supplied, but no Postman run
  or workspace publication was performed.
- `git add` / `git commit` in the repository fail because `.git` is read-only:
  `Unable to create .../.git/index.lock: Operation not permitted`. The original
  branch cannot be updated in this environment. The implementation is preserved
  as a local commit in an alternate writable Git store and exported as
  `easycr-implementation.bundle`, without changing the repository's protected
  metadata. Import it outside these restrictions with:

  ```sh
  git fetch ./easycr-implementation.bundle easycr-implementation
  git log --oneline FETCH_HEAD -1
  ```

  The original working tree already contains the implementation; do not blindly
  cherry-pick onto its uncommitted changes. Review/import the bundle in a clean
  checkout or reconcile the working tree before updating the branch.

## Parent verification outside the coding sandbox

The parent ran the final native code with normal host permissions:

- `go vet ./... && go test -race ./... -count=1 && go build -trimpath -o bin/easycr ./cmd/easycr && git diff --check`: exit 0.
- A disposable live TCP server passed startup-log capture before any HTTP request, health, admin login/session, scoped token creation, token list secret redaction, revocation, and logout. The process was terminated after testing.
- The embedded login screen rendered in a real browser at loopback; password input is masked, has a label, assets are same-origin, and the screen explains startup password generation. Authenticated browser journeys and accessibility audit remain unverified.
- `EASYCR_E2E=1 go test ./tests/e2e -run '^TestDockerWorkflow$' -v -count=1`: exit 1 at Docker login; Docker Desktop daemon timed out requesting `https://localhost:<port>/v2/`. Host HTTP service was reachable independently. No successful Docker push/pull is claimed.
- `docker compose build` and the opt-in `TestContainerDeployment` reached the Docker daemon but failed during `go mod download`: container DNS could not resolve `proxy.golang.org` using `8.8.8.8:53`. Container probe also failed to resolve `host.docker.internal`. The parent did not change global Docker networking/proxy settings.

These observations supersede sandbox-only claims that host TCP/browser or Docker daemon access are unavailable. The remaining blockers are Docker networking/DNS and full authenticated browser acceptance. The parent also successfully committed the implementation in the original repository as `1d3cfd2`; the alternate Git bundle is only an older sandbox recovery artifact and is not the authoritative final revision.

Review note: an in-flight login/reset race reported against the auth service is not reachable through the shipped CLI: reset is offline and requires the same exclusive metadata lock held for the entire server lifetime. Do not expose reset as an online endpoint without transactional credential-generation checking.

## Final hardening and browser follow-up

- Added a failing regression reproducing parent repository deletion destroying an unpublished nested upload. The adapter now inspects the pinned filesystem driver's child entries and rejects removal when nested data exists; the regression passes and the unfinished upload remains resumable. This check deliberately depends on Distribution v3.1.2 layout and must be revisited on engine upgrades.
- Persisted the registry HTTP signing secret; upload-resume-after-restart regression passes.
- Bounded registry requests to one hour, verified a stalled request releases the mutation lock. This is a maximum request duration, not idle-progress timeout; global serialization remains a throughput limitation documented in README.
- Re-ran `go test -race ./... -count=1`, `go vet ./...`, native build, and `git diff --check`: exit 0 after these fixes.
- Authenticated browser smoke using a private disposable test session passed Images empty state, Tokens navigation, global read-token creation, masked one-time secret input, safe literal rendering of an HTML-like token name, native revoke confirmation and list refresh, and sign out returning to the password form. No credentials were printed. Browser password entry itself, image mutation journeys, visual baselines, and accessibility remain unverified.
- Repeated real Docker workflow still fails at daemon login to host loopback with timeout. No successful Docker push/pull is claimed.

## Remaining acceptance work

Run the opt-in Docker tests and Compose build in an environment with daemon and
listener permissions, then run the browser checklist in README.md. These remain
required to claim every acceptance criterion. The product implementation and local
Go checks are complete, but the Docker/browser gates are not verified.
