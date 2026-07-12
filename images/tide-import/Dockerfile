# syntax=docker/dockerfile:1
# Build the tide-import binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-import:test \
#                -f images/tide-import/Dockerfile .
#
# Stage 1: compile a static Linux binary.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine@sha256:a6a091eac01ceac4b97496fe2957a49b6cdd83365337d5f46f6f73710424e805 AS builder
ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# Copy only what the binary needs.
# tide-import imports: api/v1alpha3, pkg/dispatch, cmd/tide-import.
# NO internal/reporter, NO internal/owner — pure filesystem I/O, no K8s client.
# This preserves the provider firewall (SUB-05 / Phase 28 D-08 layer 2).
COPY api/ api/
COPY pkg/dispatch/ pkg/dispatch/
COPY cmd/tide-import/ cmd/tide-import/

# Static binary: no CGO, stripped symbols for a smaller image. tide-import uses
# only stdlib + api/v1alpha3 + pkg/dispatch — pure-Go, no external C deps.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/tide-import \
      ./cmd/tide-import

# Stage 2: minimal runtime image.
# distroless/static:nonroot is correct for tide-import: pure-Go filesystem I/O,
# no shell, no package manager. Runs as UID 65532 (nonroot) by default —
# satisfying D-G3 without an explicit USER directive.
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

COPY --from=builder /out/tide-import /usr/local/bin/tide-import

ENTRYPOINT ["/usr/local/bin/tide-import"]
