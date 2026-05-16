# Build the tide-push binary.
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-push:test \
#                -f images/tide-push/Dockerfile .
#
# Stage 1: compile a static Linux binary.
FROM golang:1.26-alpine AS builder

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN go mod download

# Copy only what the binary needs (see .dockerignore for exclusions).
# pkg/dispatch is brought in transitively by pkg/git / internal/gitleaks
# imports but is not directly used by cmd/tide-push.
COPY pkg/git/ pkg/git/
COPY internal/gitleaks/ internal/gitleaks/
COPY cmd/tide-push/ cmd/tide-push/

# Static binary: no CGO, stripped symbols for a smaller image.
# go-git/v5 is pure-Go and works on distroless/static (no /bin/git
# shell-out, no CA bundle needed — go-git uses Go's crypto/tls).
# gitleaks v8 is also pure-Go.
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /out/tide-push \
      ./cmd/tide-push

# Stage 2: minimal runtime image.
# distroless/static:nonroot lets the container runtime enforce USER 1000
# cleanly across all OCI runtimes (matches images/stub-subagent/Dockerfile
# pattern).
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /out/tide-push /usr/local/bin/tide-push

# Run as non-root UID 1000 — matches D-G3 subagent harness UID.
USER 1000

ENTRYPOINT ["/usr/local/bin/tide-push"]
