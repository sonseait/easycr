FROM golang:1.26.5-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags='-s -w' -o /out/easycr ./cmd/easycr \
    && mkdir -p /out/data && chmod 0700 /out/data && chown 65532:65532 /out/data

FROM scratch
COPY --from=build /out/easycr /easycr
COPY --from=build --chown=65532:65532 /out/data /data
USER 65532:65532
VOLUME ["/data"]
EXPOSE 5000
ENTRYPOINT ["/easycr"]
CMD ["serve", "--data", "/data", "--listen", "0.0.0.0:5000", "--public-url", "http://localhost:5000", "--allow-public-http"]
