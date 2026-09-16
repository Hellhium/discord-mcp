# syntax=docker/dockerfile:1

# Build: a static binary cross-compiled on the native builder platform.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build

WORKDIR /src

# Dependencies first: this layer survives every source-only change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/discord-mcp ./cmd/server

# Runtime: alpine, for a shell to exec into, wget for the HEALTHCHECK and
# ca-certificates for https://discord.com.
FROM alpine:3.24

RUN apk add --no-cache ca-certificates

COPY --from=build /out/discord-mcp /usr/local/bin/discord-mcp

# Mount the config read-only:
#   docker run -v ./config.yaml:/etc/discord-mcp/config.yaml:ro ...
EXPOSE 8080

# Assumes the default listen address; drop it if server.listen moves off :8080.
HEALTHCHECK --interval=30s --timeout=3s --start-period=40s \
    CMD wget -q -O /dev/null http://127.0.0.1:8080/healthz || exit 1

ENTRYPOINT ["/usr/local/bin/discord-mcp"]
CMD ["-config", "/etc/discord-mcp/config.yaml"]
