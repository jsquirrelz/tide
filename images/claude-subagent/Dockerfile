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
RUN go mod download

# Copy only what the binary needs (see .dockerignore for exclusions).
COPY pkg/dispatch/ pkg/dispatch/
COPY internal/harness/ internal/harness/
COPY internal/subagent/ internal/subagent/
COPY pkg/git/ pkg/git/
COPY cmd/claude-subagent/ cmd/claude-subagent/

# Static binary: no CGO, stripped symbols for a smaller image.
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
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
FROM node:22-slim

RUN npm install -g @anthropic-ai/claude-code@2.1.142

COPY --from=builder /out/claude-subagent /usr/local/bin/claude-subagent

# Run as non-root UID 1000 — matches D-G3 subagent harness UID requirement.
USER 1000

ENTRYPOINT ["/usr/local/bin/claude-subagent"]
