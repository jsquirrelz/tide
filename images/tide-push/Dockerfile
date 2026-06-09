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

# Stage 2: minimal runtime image WITH the git binary.
# The remote clone/push TRANSPORT remains pure-Go go-git (HTTP/SSH) — no git
# binary is needed for that. But Phase 11 added LOCAL git operations that run
# inside tide-push and DO shell out to the git CLI (go-git/v5 exposes neither):
#   - B5 runClone: `git worktree add` to provision the per-run run-branch worktree
#   - B4 IntegrateTaskBranches: `git merge --no-ff` to integrate per-task branches
# distroless/static has no package manager, so the runtime base is alpine + apk
# git (mirrors the claude-subagent cascade-C fix, 314afd8; the static
# CGO_ENABLED=0 binary runs fine on musl). We recreate the uid 65532 (nonroot)
# the prior distroless:nonroot base ran as, to keep the pod securityContext +
# PVC fsGroup expectations intact (D-G3).
FROM alpine:3.21

RUN apk add --no-cache git \
    # The per-run repo/worktree on the shared PVC may be owned by a different
    # uid than 65532 (clone/executor uids differ); git 2.35+ refuses such repos
    # as "dubious ownership". Trust all paths at the system level (mirrors the
    # claude-subagent cascade-D fix, 5b9d4ca).
    && git config --system --add safe.directory '*' \
    && adduser -D -H -u 65532 nonroot

COPY --from=builder /out/tide-push /usr/local/bin/tide-push

USER 65532:65532

ENTRYPOINT ["/usr/local/bin/tide-push"]
