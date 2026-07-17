# syntax=docker/dockerfile:1
# Build the tide-reporter binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-reporter:test \
#                -f images/tide-reporter/Dockerfile .
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
# cmd/tide-reporter imports: internal/reporter, internal/owner, internal/otelinit,
# internal/harness/redact, internal/subagent/common, api/v1alpha3, pkg/dispatch,
# pkg/otelai (Phase 44-46 trace-synthesis + redaction additions).
COPY api/ api/
COPY internal/reporter/ internal/reporter/
COPY internal/owner/ internal/owner/
COPY internal/otelinit/ internal/otelinit/
COPY internal/harness/redact/ internal/harness/redact/
COPY internal/subagent/common/ internal/subagent/common/
COPY pkg/dispatch/ pkg/dispatch/
COPY pkg/otelai/ pkg/otelai/
COPY cmd/tide-reporter/ cmd/tide-reporter/

# Static binary: no CGO, stripped symbols for a smaller image. The reporter
# uses only the K8s client-go and controller-runtime libraries — pure-Go, no
# external C deps.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build \
      -ldflags="-s -w" \
      -o /out/tide-reporter \
      ./cmd/tide-reporter

# Stage 2: minimal runtime image.
# distroless/static:nonroot is correct for the reporter: pure-Go K8s API client,
# no shell, no package manager. Runs as UID 65532 (nonroot) by default —
# satisfying D-G3 without an explicit USER directive.
FROM gcr.io/distroless/static:nonroot@sha256:963fa6c544fe5ce420f1f54fb88b6fb01479f054c8056d0f74cc2c6000df5240

COPY --from=builder /out/tide-reporter /usr/local/bin/tide-reporter

ENTRYPOINT ["/usr/local/bin/tide-reporter"]
