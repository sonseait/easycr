# Container image build

Requires Docker with the Compose plugin and a running Docker daemon. Run these
commands from the repository root:

```sh
cp build/.env.example build/.env
# Edit build/.env with your registry, project, and tag.
docker compose --env-file build/.env -f build/compose.yml config --quiet
docker compose --env-file build/.env -f build/compose.yml build
```

`build/.env` is ignored by Git. `REGISTRY` is the registry host, optionally with a
port, without a URL scheme or trailing slash. `PROJECT` is the registry namespace
or project path. Both are required. `TAG` defaults to `latest` when empty or unset.
The resulting image is `${REGISTRY}/${PROJECT}/easycr:${TAG}`.

Compose resolves `context: ..` relative to `build/compose.yml`, so it builds the
existing root Dockerfile with the repository root as its context. This Compose
file configures image building only; runtime deployment is covered in the README.
It builds both `linux/amd64` and `linux/arm64`, matching the publishing workflow.
The Docker daemon must support storing multi-platform images (for example, the
containerd image store in Docker Desktop).

To publish the built image, authenticate with the destination registry using your
existing Docker credential store, then push:

```sh
# In your shell, set REGISTRY to the same host as in build/.env.
docker login "$REGISTRY"
docker compose --env-file build/.env -f build/compose.yml push
```

Keep registry credentials out of Compose files and tracked environment files.
