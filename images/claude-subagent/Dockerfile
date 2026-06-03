# syntax=docker/dockerfile:1
# Build the claude-subagent runtime image.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-claude-subagent:test \
#                -f images/claude-subagent/Dockerfile .
#
# Stage 1: compile a static Linux Go binary.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy only what the binary needs (see .dockerignore for exclusions).
COPY pkg/dispatch/ pkg/dispatch/
COPY internal/harness/ internal/harness/
COPY internal/subagent/ internal/subagent/
COPY pkg/git/ pkg/git/
COPY cmd/claude-subagent/ cmd/claude-subagent/

# Static binary: no CGO, stripped symbols for a smaller image.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/claude-subagent \
      ./cmd/claude-subagent

# Stage 2: Claude Code CLI runtime image.
# node:22-slim is required because the claude CLI is a Node binary. The CLI
# version is pinned (Pitfall 3 — RESEARCH.md: never npm install without an
# explicit @<version>); pin tracks the planner's choice on the STACK floor of
# v2.1.139. The signed-token credential is injected at K8s Pod-spec time by
# the credproxy sidecar (Phase 2 D-C1..C4) — never baked into the image.
# Host configuration directories are not bind-mounted (CLAUDE.md anti-pattern).
#
# git: the subagent harness clones the per-Project bare repo over a file://
# URL (pkg/git worktree PlainClone, D-B4) — and go-git's file:// transport is
# NOT pure-Go; it shells out to the system git binary at runtime. node:22-slim
# (Debian) ships no git, so it must be apt-installed here, or the clone fails
# with `exec: "git": executable file not found in $PATH` (debug session
# file-transport-git-missing). --no-install-recommends keeps the layer lean.
FROM node:22-slim

RUN --mount=type=cache,target=/var/cache/apt \
    apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/*

RUN --mount=type=cache,target=/root/.npm npm install -g @anthropic-ai/claude-code@2.1.142

COPY --from=builder /out/claude-subagent /usr/local/bin/claude-subagent

# Run as non-root UID 1000 — matches D-G3 subagent harness UID requirement.
USER 1000

ENTRYPOINT ["/usr/local/bin/claude-subagent"]
