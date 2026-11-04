# Compile on the builder's native CPU, including multi-platform builds.
FROM --platform=$BUILDPLATFORM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
ARG TARGETOS
ARG TARGETARCH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags='-s -w' -o /out/easycr ./cmd/easycr \
    && mkdir -p /out/data && chmod 0700 /out/data && chown 65532:65532 /out/data

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/easycr /easycr
COPY --from=build --chown=65532:65532 /out/data /data
# Default only: Compose `user: "UID:GID"` or docker run --user overrides this.
# Provision /data ownership for that UID:GID before starting; no runtime chown.
USER 65532:65532
VOLUME ["/data"]
EXPOSE 5000
ENTRYPOINT ["/easycr"]
WORKDIR /
CMD ["serve"]
