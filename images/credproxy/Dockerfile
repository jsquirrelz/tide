# syntax=docker/dockerfile:1
# Build the credproxy sidecar binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-credproxy:v0.1.0-dev \
#                -f images/credproxy/Dockerfile .
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
COPY cmd/credproxy/ cmd/credproxy/
COPY internal/credproxy/ internal/credproxy/

# Static binary: no CGO, stripped symbols for a smaller image.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/credproxy \
      ./cmd/credproxy

# Stage 2: minimal runtime image.
# distroless/static:nonroot ships /etc/passwd so the container runtime
# enforces USER 1000 cleanly across all OCI runtimes (D-G3).
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/credproxy /usr/local/bin/credproxy

# Run as non-root UID 1000 — matches D-G3 requirement (T-02-05-07).
USER 1000

ENTRYPOINT ["/usr/local/bin/credproxy"]
