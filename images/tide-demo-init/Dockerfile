# Build the tide-demo-init binary (Phase 5 D-B3 / MEDIUM-11 embed-strategy lock).
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-demo-init:test \
#                -f images/tide-demo-init/Dockerfile .
#
# Stage 1: compile a static Linux binary with the medium-sample fixture
# content embedded via //go:embed all:fixture (see cmd/tide-demo-init/main.go).
FROM golang:1.26-alpine@sha256:a6a091eac01ceac4b97496fe2957a49b6cdd83365337d5f46f6f73710424e805 AS builder

WORKDIR /workspace

# Cache the dependency layer before copying source.
COPY go.mod go.sum ./
RUN go mod download

# Copy only what tide-demo-init needs.
# pkg/git is brought in transitively for go-git auth helpers; tide-demo-init
# uses gogit.PlainInit + PushContext directly but lives under the same
# module-rooted build context.
COPY pkg/git/ pkg/git/
COPY cmd/tide-demo-init/ cmd/tide-demo-init/

# MEDIUM-11 LOCK — build-time fixture positioning. The //go:embed all:fixture
# directive in cmd/tide-demo-init/main.go points at a sibling `fixture/`
# directory. The SOT for that content lives at examples/tide-demo-fixture/;
# here we COPY it into the build context at cmd/tide-demo-init/fixture/.
#
# Submodule shim: examples/tide-demo-fixture/ carries its own go.mod / go.sum
# (intentional — it's a tiny standalone module the medium-sample's Claude
# task will modify). Embedding a sibling go.mod would make Go's embed reject
# fixture/ as a different module. Rename go.mod → go.mod.txt + go.sum →
# go.sum.txt here; cmd/tide-demo-init/main.go's restoreShimmedName reverses
# the rename at unpack time so the bare repo's working tree carries the
# canonical filenames.
COPY examples/tide-demo-fixture/ cmd/tide-demo-init/fixture/
RUN if [ -f cmd/tide-demo-init/fixture/go.mod ]; then mv cmd/tide-demo-init/fixture/go.mod cmd/tide-demo-init/fixture/go.mod.txt; fi
RUN if [ -f cmd/tide-demo-init/fixture/go.sum ]; then mv cmd/tide-demo-init/fixture/go.sum cmd/tide-demo-init/fixture/go.sum.txt; fi

# Static binary: no CGO, stripped symbols for a smaller image. The go-git/v5
# HTTP(S)/SSH transports are pure-Go, but its file:// transport is NOT: a
# file:// push/clone shells out to the system git binary (git-upload-pack /
# git-receive-pack) at runtime. tide-demo-init PushContexts the embedded
# fixture to a file:// bare repo on the shared PVC, so the runtime image
# MUST ship /usr/bin/git (see runtime stage below). The old distroless/static
# base carried no git and failed with `exec: "git": executable file not
# found in $PATH` (debug session file-transport-git-missing).
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags="-s -w" \
      -o /out/tide-demo-init \
      ./cmd/tide-demo-init

# Stage 2: minimal runtime image.
# alpine (not distroless/static) because tide-demo-init's file:// push shells
# out to the system git binary; distroless has no package manager and no git.
# alpine's apk-managed git pulls its own helper deps (git-remote-*, templates)
# correctly — far less fragile than hand-assembling a static git into
# distroless. We recreate the non-root UID 1000 contract that distroless
# provided out of the box (D-G3 subagent harness UID).
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache git \
    && adduser -D -u 1000 nonroot

COPY --from=builder /out/tide-demo-init /usr/local/bin/tide-demo-init

# Run as non-root UID 1000 — matches D-G3 subagent harness UID.
USER 1000

ENTRYPOINT ["/usr/local/bin/tide-demo-init"]
