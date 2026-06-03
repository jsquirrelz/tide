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
# HTTP(S) transport is pure-Go (uses Go's crypto/tls, no CA bundle needed),
# and gitleaks v8 is pure-Go. BUT go-git's file:// transport is NOT pure-Go:
# it shells out to the system git binary (git-upload-pack / git-receive-pack)
# at runtime. tide-push pushes over whatever remote the Project declares,
# which for the medium sample is a file:// bare repo on the shared PVC, so
# the runtime image MUST ship /usr/bin/git (see runtime stage below). The old
# distroless/static base carried no git and failed with `exec: "git":
# executable file not found in $PATH` (debug session
# file-transport-git-missing).
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/tide-push \
      ./cmd/tide-push

# Stage 2: minimal runtime image.
# alpine (not distroless/static) because tide-push's file:// push shells out
# to the system git binary; distroless has no package manager and no git.
# alpine's apk-managed git pulls its own helper deps (git-remote-*, templates)
# correctly — far less fragile than hand-assembling a static git into
# distroless. We recreate the non-root UID 1000 contract that distroless
# provided out of the box (D-G3 subagent harness UID).
FROM alpine:3.21

RUN apk add --no-cache git \
    && adduser -D -u 1000 nonroot

COPY --from=builder /out/tide-push /usr/local/bin/tide-push

# Run as non-root UID 1000 — matches D-G3 subagent harness UID.
USER 1000

ENTRYPOINT ["/usr/local/bin/tide-push"]
