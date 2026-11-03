# EasyCR

A private, single-admin OCI / Docker registry in one Go binary, with an embedded
Images and Tokens UI, local filesystem storage, and offline garbage collection.
No Node build, external database, or separate registry process is required.

## Run locally

Requires Go 1.26+ (tested toolchain: 1.26.5).

```sh
go build -trimpath -o bin/easycr ./cmd/easycr
./bin/easycr serve --data ./data --listen 127.0.0.1:5000 --public-url http://localhost:5000
```

Open <http://localhost:5000>. Use **the exact public URL hostname** in the browser:
admin mutations require its Origin. `localhost` and `127.0.0.1` are different
origins. No password or token is supplied as a CLI argument.

The first `serve` startup creates and commits a bcrypt admin password hash,
then prints one `bootstrap_admin_password password=...` event to stderr **before
opening the HTTP listener**. No HTTP request or UI visit triggers generation.
Restrict access to these logs. Restarting with the same data directory keeps the
password and does not print it again. If the event is lost, use offline reset.
The UI never returns the admin password. Bootstrap or DB errors prevent listening.
If binding fails after bootstrap, the password remains persisted: reuse the log
or explicitly reset it rather than expecting another bootstrap event.

## Docker Compose

```sh
docker compose build
docker compose up -d
docker compose logs easycr
```

The container runs as UID/GID 65532, writes only its named `/data` volume, and
publishes port 5000 on the host's loopback interface. Named-volume copy-up gives
it the required ownership. A bind mount must be provisioned with ownership
65532:65532 and mode 0700 first. Never run two instances on one data directory.
The image uses a pinned Go 1.26.5 builder and a scratch runtime; it contains the
binary and no shell. `GET /healthz` returns `{"status":"ok"}` without private data.
`docker compose down` preserves data; **`down -v` destroys it**.

For public use, terminate HTTPS at a reverse proxy, set `EASYCR_PUBLIC_URL` to the
exact HTTPS origin, and recreate the service. Keep the backend on loopback or a
private Docker network. Forward request bodies without buffering large blobs,
allow long uploads, and preserve the Host header. EasyCR does not trust
X-Forwarded-For for login rate limits. The application serves HTTP internally;
`--allow-public-http` explicitly permits its non-loopback container listener.
Never publish this plaintext backend directly to the Internet. HTTPS public URLs
cause the session cookie to use Secure. No SSO, scanning, replication, object
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
./bin/easycr gc --data ./data --dry-run
./bin/easycr gc --data ./data
./bin/easycr serve --data ./data --public-url http://localhost:5000
```

GC uses Distribution mark-and-sweep with RemoveUntagged. The test fixture checks
live OCI index children, their configs/layers, cross-repository sharing, orphan
reclamation, and byte-for-byte nonmutating dry-run. There is no background GC or
upload purger. Abandoned incomplete upload directories are not a retention system
and are not automatically cleaned by this MVP.

Reset a lost password offline (prints a new password once to stderr):

```sh
./bin/easycr admin reset-password --data ./data
```

Reset invalidates every UI session and preserves Docker tokens and images.
It requires an existing bootstrapped store and does not repair corrupt metadata.

For Compose, stop the service and run maintenance in a one-off container sharing
its named volume:

```sh
docker compose stop easycr
docker compose run --rm --no-deps easycr gc --data /data --dry-run
docker compose run --rm --no-deps easycr gc --data /data
# If recovery is needed instead:
docker compose run --rm --no-deps easycr admin reset-password --data /data
docker compose up -d
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
docker compose build
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
