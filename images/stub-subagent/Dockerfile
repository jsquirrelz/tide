# syntax=docker/dockerfile:1
# Build the stub-subagent binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-stub-subagent:test \
#                -f images/stub-subagent/Dockerfile .
#
# Stage 1: compile a static Linux binary.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:a6a091eac01ceac4b97496fe2957a49b6cdd83365337d5f46f6f73710424e805 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy only what the binary needs (see .dockerignore for exclusions).
# internal/harness + pkg/git back the executor git contract
# (EnsureWorktree/CommitWorktree — mirrors cmd/claude-subagent).
COPY pkg/dispatch/ pkg/dispatch/
COPY pkg/git/ pkg/git/
COPY internal/harness/ internal/harness/
COPY cmd/stub-subagent/ cmd/stub-subagent/

# Static binary: no CGO, stripped symbols for a smaller image.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/stub-subagent \
      ./cmd/stub-subagent

# Stage 2: minimal runtime image WITH the git binary.
# The stub now mirrors the real executor's git contract
# (harness.EnsureWorktree `git worktree add` + harness.CommitWorktree),
# both of which shell out to the git CLI — same reason claude-subagent and
# tide-push run on alpine + apk git (cascade-C fix, 314afd8). We recreate
# uid 1000 to keep the D-G3 subagent harness UID + PVC fsGroup expectations
# intact.
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache git \
    # The per-run repo/worktree on the shared PVC may be owned by a different
    # uid than 1000 (clone runs as 65532); git 2.35+ refuses such repos as
    # "dubious ownership". Trust all paths at the system level (mirrors the
    # claude-subagent cascade-D fix, 5b9d4ca).
    && git config --system --add safe.directory '*' \
    && adduser -D -H -u 1000 stub

COPY --from=builder /out/stub-subagent /usr/local/bin/stub-subagent

# Run as non-root UID 1000 — matches D-G3 subagent harness UID requirement.
USER 1000

ENTRYPOINT ["/usr/local/bin/stub-subagent"]
