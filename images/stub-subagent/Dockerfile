# syntax=docker/dockerfile:1
# Build the stub-subagent binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-stub-subagent:test \
#                -f images/stub-subagent/Dockerfile .
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
COPY pkg/dispatch/ pkg/dispatch/
COPY cmd/stub-subagent/ cmd/stub-subagent/

# Static binary: no CGO, stripped symbols for a smaller image.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/stub-subagent \
      ./cmd/stub-subagent

# Stage 2: minimal runtime image.
# scratch has no shell or CA bundle; the stub makes zero outbound calls so
# neither is needed.  distroless/static:nonroot is used instead of scratch
# because it ships a /etc/passwd and lets the container runtime enforce
# USER 1000 cleanly across all OCI runtimes.
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/stub-subagent /usr/local/bin/stub-subagent

# Run as non-root UID 1000 — matches D-G3 subagent harness UID requirement.
USER 1000

ENTRYPOINT ["/usr/local/bin/stub-subagent"]
