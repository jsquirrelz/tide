# syntax=docker/dockerfile:1
# Build the tide-push binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-push:test \
#                -f images/tide-push/Dockerfile .
#
# Stage 1: compile a static Linux binary.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy only what the binary needs (see .dockerignore for exclusions).
# pkg/dispatch is brought in transitively by pkg/git / internal/gitleaks
# imports but is not directly used by cmd/tide-push.
COPY pkg/git/ pkg/git/
COPY internal/gitleaks/ internal/gitleaks/
COPY cmd/tide-push/ cmd/tide-push/

# Static binary: no CGO, stripped symbols for a smaller image. go-git/v5's
# HTTPS and SSH transports are pure-Go (Go's crypto/tls, no CA bundle, no
# /bin/git shell-out); gitleaks v8 is also pure-Go. tide-push ONLY pushes
# over http(s) or SSH remotes (Phase 8+). The medium sample's remote is an
# in-cluster HTTP git server (images/tide-git-http-server) — no file://
# transport is used by tide-push in production.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/tide-push \
      ./cmd/tide-push

# Stage 2: minimal runtime image.
# distroless/static:nonroot is the correct base for tide-push: go-git's
# HTTP(S) and SSH transports are pure-Go — no system git binary needed at
# runtime. The in-cluster git-http-backend server (images/tide-git-http-server)
# is the demo-only transport bridge; tide-push speaks to it over HTTP (pure-Go).
# distroless/static:nonroot provides a minimal read-only filesystem with no
# shell, no package manager, and runs as UID 65532 (nonroot) by default —
# satisfying D-G3 without an explicit USER directive.
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/tide-push /usr/local/bin/tide-push

ENTRYPOINT ["/usr/local/bin/tide-push"]
