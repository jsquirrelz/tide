# Image URL to use all building/pushing image targets
IMG ?= controller:latest
# YEAR defines the year value used for substituting the YEAR placeholder in the boilerplate header.
YEAR ?= $(shell date +%Y)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Integration test budget knobs. The outer timeout protects the shell command;
# KIND_GO_TEST_TIMEOUT must exceed the kind suite's helm --wait window.
INTEGRATION_TIMEOUT ?= 3300s
KIND_GO_TEST_TIMEOUT ?= 50m

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk command is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	# Scoped paths avoid descending into tools/analyzers/*/testdata fixtures (Plan 02 introduced
	# a violation fixture under tools/analyzers/dagimports/testdata/src/violation/pkg/dag/ that
	# imports k8s.io/apimachinery/pkg/runtime — needed for analysistest's GOPATH resolver but
	# unresolvable to controller-gen's standard module resolution. The api/ + internal/controller/
	# + internal/webhook/ triple is exhaustive: api/ carries the type markers, internal/controller/
	# carries the +kubebuilder:rbac: markers, internal/webhook/ carries the +kubebuilder:webhook: markers.
	"$(CONTROLLER_GEN)" rbac:roleName=manager-role crd webhook \
		paths="./api/..." \
		paths="./internal/controller/..." \
		paths="./internal/webhook/..." \
		output:crd:artifacts:config=config/crd/bases

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	# Scoped to ./api/... — DeepCopy methods only need to be generated for kubebuilder-tagged
	# CRD types, which live exclusively under api/. Walking ./... would also descend into
	# tools/analyzers/*/testdata fixtures, which deliberately host unresolvable imports.
	"$(CONTROLLER_GEN)" object:headerFile="hack/boilerplate.go.txt",year=$(YEAR) paths="./api/..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: demo-fixture
demo-fixture: ## Materialize cmd/tide-demo-init/fixture/ from examples/tide-demo-fixture/ (gitignored SOT lock; required by //go:embed all:fixture).
	go generate ./cmd/tide-demo-init/...

.PHONY: vet
vet: demo-fixture ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run the UNIT tier (TEST-01: -short skips the slow leader-election envtest; excludes the test/integration tier — that runs via test-int-fast/test-int).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -short -timeout 360s $$(go list ./... | grep -v /e2e | grep -v /test/integration) -coverprofile cover.out

.PHONY: test-only
test-only: ## Run the UNIT tier without re-running manifests/generate/fmt/vet/setup-envtest (assumes prep already done). Used by CI's TEST-01 timing assertion. Integration tier excluded (runs via test-int-fast).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -short -timeout 360s $$(go list ./... | grep -v /e2e | grep -v /test/integration) -coverprofile cover.out

.PHONY: test-leader-election
test-leader-election: manifests generate fmt vet setup-envtest ## Run the slow CTRL-03 leader-election envtest (~60s; excluded from `make test`).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test ./internal/controller/... -timeout 180s -v -ginkgo.focus="Leader Election"

.PHONY: test-heavy
test-heavy: manifests generate fmt vet setup-envtest ## Run the heavy controller envtests extracted from the unit tier (Phase 38 DEBT-03; selects Ginkgo Label("heavy")).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test ./internal/controller/... -timeout 20m -v -ginkgo.v -ginkgo.label-filter='heavy'

##@ Phase 4 kind-harness E2E (plan 04-14)

# test-e2e-kind builds the manager + dashboard + tide CLI binaries, spins up a
# dedicated kind cluster (`tide-e2e-phase4`), helm-installs the chart with
# dashboard.enabled=true, and runs the kind_e2e-tagged specs under test/e2e/.
#
# This is TIDE's real e2e suite. It honors SKIP_KIND_TESTS=true to short-circuit
# on dev machines without container tooling. The heavy kind-based coverage runs
# nightly + on-demand via .github/workflows/nightly-integration.yml, not on the
# per-push critical path.
#
# (The former kubebuilder-scaffold `test-e2e` target was retired: its
# kustomize-driven `make deploy` install is incompatible with TIDE's Helm +
# /etc/tide/config.yaml + cert-manager webhook model, so it CrashLooped. The
# kind_e2e suite below supersedes it.)
.PHONY: test-e2e-kind
test-e2e-kind: tide-cli ## Phase 4 plan 04-14 kind E2E suite (dashboard + gate-flow + tail cancellation).
	go test -tags=kind_e2e -timeout=15m ./test/e2e/... -v -ginkgo.v

##@ Integration tests (TEST-02 — Phase 2)

.PHONY: test-int test-int-kind test-int-fast test-int-kind-prep

test-int: manifests generate fmt vet setup-envtest test-int-kind-prep ## Run full integration test suite: Layer A (envtest) + Layer B (kind). Requires Docker + kind.
	# NO FLAKE TOLERANCE. Layer A and Layer B are invoked SEPARATELY (set -e, so a
	# failure in EITHER fails the target) and NEITHER retries. A "flake" is a bug: a
	# non-deterministic spec masks a real defect and delays detection. The Phase
	# 36/37 kind regression (tide-push artifact-stage-failed) hid for days behind
	# -ginkgo.flake-attempts=3 — the retries re-ran each failing spec up to 3×, which
	# tripled its wall time and blew the suite budget so tail specs expired too,
	# turning a deterministic red into noise. If a spec is non-deterministic, fix its
	# root cause (timeout, race, ordering); do not paper over it with a retry.
	#
	# The kind go-test timeout still owns the helm --wait window, with ample headroom
	# (KIND_GO_TEST_TIMEOUT=50m / INTEGRATION_TIMEOUT=55m cover Layer B's grown wall —
	# Phases 36/37 added the agent-identity chart spec + the artifact-staging DASH-02
	# cascade (a live 4-level planner run, ~12m tail) — on top of the ~18m baseline +
	# 3 import resume tiers).
	@set -e; \
	export KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)"; \
	export TIDE_BINARY="$$(pwd)/bin/tide"; \
	echo "=== Layer A (envtest, Ginkgo-only, no retries) ==="; \
	go test ./test/integration/envtest/... -v -timeout=10m -ginkgo.v --ginkgo.label-filter='envtest'; \
	echo "=== Layer A2 (heavy controller envtests, Phase 38 DEBT-03, no retries) ==="; \
	go test ./internal/controller/... -timeout 20m -ginkgo.v -ginkgo.label-filter='heavy'; \
	echo "=== Layer B (kind: Ginkgo specs + plain go-test contract tests, no retries) ==="; \
	timeout $(INTEGRATION_TIMEOUT) go test ./test/integration/kind/... -v -timeout=$(KIND_GO_TEST_TIMEOUT) -ginkgo.v

test-int-kind: manifests generate fmt vet setup-envtest test-int-kind-prep ## Run ONLY the Layer B kind tier (no Layer A envtest). The kind-sensitive + nightly workflows use this so a Layer A envtest flake can't abort the ~40m kind setup before Layer B runs. Layer A stays covered per-push by test-int-fast / the ci + Tests workflows. Requires Docker + kind.
	# NO FLAKE TOLERANCE (see test-int). Layer B only: Layer A envtest needs no kind,
	# so running it inside the expensive kind job only couples Layer B to Layer A
	# flakes and wastes the kind setup when one trips (run 29111456871 burned ~40m of
	# kind setup then failed on a 2-min Layer A RESUME-01 flake, never reaching Layer
	# B). bin/tide + the loaded images come from test-int-kind-prep; KUBEBUILDER_ASSETS
	# is exported for parity with test-int (the kind suite talks to the real cluster).
	@set -e; \
	export KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)"; \
	export TIDE_BINARY="$$(pwd)/bin/tide"; \
	echo "=== Layer B (kind: Ginkgo specs + plain go-test contract tests, no retries) ==="; \
	timeout $(INTEGRATION_TIMEOUT) go test ./test/integration/kind/... -v -timeout=$(KIND_GO_TEST_TIMEOUT) -ginkgo.v

test-int-fast: manifests generate fmt vet setup-envtest ## Run Layer A integration tests only (envtest; no Docker/kind needed). ~90s clean locally; -timeout=10m gives headroom on slow/contended CI runners. NO flake-attempts — a non-deterministic envtest spec is a bug to fix, not retry.
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		go test ./test/integration/envtest/... -v -timeout=10m -ginkgo.v --ginkgo.label-filter='envtest'
	@echo "=== Layer A2 (heavy controller envtests, Phase 38 DEBT-03, no retries) ==="
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		go test ./internal/controller/... -timeout 20m -ginkgo.v -ginkgo.label-filter='heavy'

test-int-kind-prep: ## Build manager + stub-subagent + credproxy + tide-push + tide-reporter + tide-import Docker images and load them into the tide-test kind cluster. Also builds bin/tide for the kind E2E (D-10, 29-04).
	# Phase 29 plan 29-04 (D-10): build the tide CLI binary so the kind E2E can
	# exec it via TIDE_BINARY or PATH. The binary is a stateless host binary;
	# no image loading is needed.
	go build -o bin/tide ./cmd/tide
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-stub-subagent:test -f images/stub-subagent/Dockerfile .
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-credproxy:test -f images/credproxy/Dockerfile .
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-push:test -f images/tide-push/Dockerfile .
	# Phase 9 plan 09-05: build tide-reporter (Option-C in-namespace reader Job image).
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-reporter:test -f images/tide-reporter/Dockerfile .
	# Phase 28 (IMPORT-01) / Phase 29 (29-05 Tier a): build tide-import — the in-namespace
	# UID-rewrite import Job image the ImportController dispatches. Without this the import
	# Job pod ImagePullBackoffs and the imported run never stages envelopes at new-UID paths.
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-import:test -f images/tide-import/Dockerfile .
	$(CONTAINER_TOOL) build -t controller:test -f Dockerfile .
	# SC-5 (medium_http_test.go): build the two Layer-B fixture images the spec
	# references by their :1.0.0 tag — tide-demo-init bootstraps the bare repo on
	# demo-remote-pvc, tide-git-http-server serves it over in-cluster http://.
	# Both are PRIVATE (unpublished) fixtures with imagePullPolicy=IfNotPresent, so
	# their pods 403 on pull unless pre-loaded — they MUST be built+loaded here.
	# Previously only the nightly workflow's SC-1 step built them, so `make test-int`
	# was not self-contained: the spec ImagePullBackoff'd and timed out (read as a
	# "flaky" 2-minute timeout) anywhere else. Tag matches the test's :1.0.0 consts.
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-demo-init:1.0.0 -f images/tide-demo-init/Dockerfile .
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 -f images/tide-git-http-server/Dockerfile .
	@if ! $(KIND) get clusters 2>/dev/null | grep -q "^tide-test$$"; then \
		$(KIND) create cluster --name tide-test --config test/integration/kind/cluster.yaml; \
	fi
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-stub-subagent:test --name tide-test
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-credproxy:test --name tide-test
	# Phase 3 plan 03-10: preload tide-push (push_lease_test.go's mocked push Jobs
	# still reference the image; preload avoids ImagePullBackoff stalls).
	# Equivalent literal command at make-time: kind load docker-image ghcr.io/jsquirrelz/tide-push:test --name tide-test
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-push:test --name tide-test
	# Phase 9 plan 09-05: preload tide-reporter so reporter Job specs resolve without ImagePullBackoff.
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-reporter:test --name tide-test
	# Phase 28/29: preload tide-import so the ImportController's import Job pod runs
	# (helm sets TIDE_IMPORT_IMAGE=...tide-import:test via images.tideImport.tag override).
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-import:test --name tide-test
	$(KIND) load docker-image controller:test --name tide-test
	# SC-5: load the medium_http fixture images so loadRequiredImage finds them.
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-demo-init:1.0.0 --name tide-test
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 --name tide-test

##@ Live nightly E2E (TEST-03 — Phase 3 plan 03-11)

# test-e2e-live runs the live Claude nightly E2E spec under test/e2e/.
# Cost-bearing: ~$0.20-$0.80 per run baseline. Skipped by `make test` /
# `make test-int`; intended ONLY for nightly CI cron runs OR explicit
# operator-driven debug runs.
#
# Double gate (with the in-test BeforeSuite Skip): fail-fast on missing
# ANTHROPIC_API_KEY env, then go test invokes with `-tags=live_e2e` so
# live_claude_test.go is compiled. The build tag uses an underscore (Go
# build-constraint grammar requires identifier-shaped tags; hyphens are
# illegal in `//go:build` lines) while the Makefile target name keeps
# the operator-friendly hyphen (`test-e2e-live`).
#
# Budget cap (third safety net): test fixture sets Project.Spec.budget.
# absoluteCapCents=100 (= $1.00). A runaway dispatch halts at the gate
# with Status.phase=BudgetExceeded; the spec then fails its assertion
# (costSpentCents < 100), surfacing the over-spend in CI.
#
# See docs/live-e2e.md for nightly CI recipe + fixture pinning + cost
# baseline + troubleshooting.
.PHONY: test-e2e-live
test-e2e-live: ## Live nightly E2E (requires ANTHROPIC_API_KEY env) — incurs cost ~$0.20-$0.80 per run.
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
		echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run live E2E"; \
		echo "       See docs/live-e2e.md for the nightly CI recipe."; \
		exit 1; \
	fi
	go test -tags=live_e2e ./test/e2e/... -timeout=15m -v

.PHONY: eval
eval: ## count_tokens pre-flight (online, requires TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN; POSTs to credproxy, reports per-template real token counts + 1024-floor pass/fail).
	@if [ -z "$$TIDE_PROXY_ENDPOINT" ]; then \
		echo "ERROR: TIDE_PROXY_ENDPOINT env not set — refusing to run eval"; \
		echo "       Start a credproxy and export TIDE_PROXY_ENDPOINT=https://127.0.0.1:8443"; \
		exit 1; \
	fi
	@if [ -z "$$TIDE_SIGNED_TOKEN" ]; then \
		echo "ERROR: TIDE_SIGNED_TOKEN env not set — refusing to run eval"; \
		echo "       Export a valid HMAC signed token for the running credproxy"; \
		exit 1; \
	fi
	go run -tags eval ./cmd/tide-eval/ \
		-proxy "$(TIDE_PROXY_ENDPOINT)" \
		-token "$(TIDE_SIGNED_TOKEN)" \
		-model "$(or $(EVAL_MODEL),claude-sonnet-4-6)"

.PHONY: spike
spike: ## cross-pod cache prefix spike (online, requires TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN; dispatches two real claude -p --bare calls with distinct --add-dir paths and reports a PASS/FAIL cache hit verdict).
	@if [ -z "$$TIDE_PROXY_ENDPOINT" ]; then \
		echo "ERROR: TIDE_PROXY_ENDPOINT env not set — refusing to run spike"; \
		echo "       Start a credproxy on kind-tide-dogfood and export TIDE_PROXY_ENDPOINT=https://127.0.0.1:8443"; \
		exit 1; \
	fi
	@if [ -z "$$TIDE_SIGNED_TOKEN" ]; then \
		echo "ERROR: TIDE_SIGNED_TOKEN env not set — refusing to run spike"; \
		echo "       Mint and export a valid HMAC signed token for the running credproxy"; \
		exit 1; \
	fi
	go run -tags spike ./cmd/tide-spike/ \
		-proxy "$(TIDE_PROXY_ENDPOINT)" \
		-token "$(TIDE_SIGNED_TOKEN)" \
		-model "$(or $(EVAL_MODEL),claude-sonnet-4-6)"

.PHONY: lint
lint: demo-fixture golangci-lint verify-dag-imports verify-dispatch-imports verify-import-firewall ## Run golangci-lint linter + import firewalls
	"$(GOLANGCI_LINT)" run

.PHONY: lint-fix
lint-fix: golangci-lint ## Run golangci-lint linter and perform fixes
	"$(GOLANGCI_LINT)" run --fix

.PHONY: lint-config
lint-config: golangci-lint ## Verify golangci-lint linter configuration
	"$(GOLANGCI_LINT)" config verify

##@ Build

.PHONY: build
build: manifests generate fmt vet ## Build manager binary.
	go build -o bin/tide-manager ./cmd/manager

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/manager

.PHONY: tide-cli
tide-cli: ## Build the operator-facing tide CLI binary (Phase 4 D-C1..C4).
	go build -o bin/tide ./cmd/tide

.PHONY: release-snapshot
release-snapshot: ## Dry-run goreleaser locally (no tag, no upload). Plan 04-09 (D-C2).
	@if command -v goreleaser >/dev/null 2>&1; then \
		goreleaser release --snapshot --skip publish --clean; \
	else \
		docker run --rm -v "$(PWD)":/work -w /work goreleaser/goreleaser:latest release --snapshot --skip publish --clean; \
	fi

.PHONY: dashboard-build
dashboard-build: dashboard-frontend ## Build the dashboard backend binary (Phase 4 D-D2; embeds the SPA at cmd/dashboard/embed/dist).
	go build -o bin/dashboard ./cmd/dashboard

.PHONY: dashboard-frontend
dashboard-frontend: ## Build the React SPA bundle, run frontend tests (incl. <500KB bundle gate), copy into cmd/dashboard/embed/dist (Phase 4 D-D5; plan 04-16).
	cd dashboard/web && npm ci && npm run build && npm run test
	rm -rf cmd/dashboard/embed/dist
	cp -r dashboard/web/dist cmd/dashboard/embed/dist

.PHONY: verify-dashboard-freshness
verify-dashboard-freshness: ## Gate: rebuild the SPA into dashboard/web/dist and diff -rq against the committed cmd/dashboard/embed/dist (WR-01: no tree mutation; WR-02: added/removed files caught); also asserts telemetry marker. Does NOT call dashboard-frontend and does NOT write to the tracked embed dir (FIX-01, Phase 22).
	cd dashboard/web && npm ci && npm run build && npm run test
	@if ! diff -rq dashboard/web/dist cmd/dashboard/embed/dist; then \
		echo "FAIL: cmd/dashboard/embed/dist/ diverges from a fresh SPA build — run 'make dashboard-frontend' and commit the result before merging"; \
		exit 1; \
	fi
	@echo "PASS: cmd/dashboard/embed/dist/ matches a fresh SPA build (added/removed/changed files all checked)"
	@MARKER="panel-cache-efficiency"; \
	if grep -qr "$$MARKER" cmd/dashboard/embed/dist/assets/*.js 2>/dev/null; then \
		echo "PASS: embedded bundle contains telemetry marker ($$MARKER)"; \
	else \
		echo "FAIL: embedded bundle missing telemetry marker '$$MARKER' — stale pre-telemetry bundle?"; \
		exit 1; \
	fi

# If you wish to build the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for the manager image be built to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - be able to use docker buildx. More info: https://docs.docker.com/build/buildx/
# - have enabled BuildKit. More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image to your registry (i.e. if you do not set a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To adequately provide solutions that are compatible with multiple platforms, you should consider using this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
# IMAGE_TAG is used by docker-buildx-snapshot for the 6-image snapshot build.
# Default 1.0.0 matches chart appVersion after Phase 06 CHART-01 fix.
IMAGE_TAG ?= 1.0.1
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name tide-builder
	$(CONTAINER_TOOL) buildx use tide-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm tide-builder
	rm Dockerfile.cross

.PHONY: docker-buildx-snapshot
docker-buildx-snapshot: ## Phase 6 IMG-01 — local multi-arch snapshot build of all 7 component images (no push; IMAGE_TAG=$(IMAGE_TAG)).
	@echo "Building all 7 component images for linux/amd64,linux/arm64 (no push; tag=$(IMAGE_TAG))..."
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-controller:$(IMAGE_TAG) -f ./Dockerfile .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-dashboard:$(IMAGE_TAG) -f ./Dockerfile.dashboard .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-stub-subagent:$(IMAGE_TAG) -f images/stub-subagent/Dockerfile .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-credproxy:$(IMAGE_TAG) -f images/credproxy/Dockerfile .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-push:$(IMAGE_TAG) -f images/tide-push/Dockerfile .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-reporter:$(IMAGE_TAG) -f images/tide-reporter/Dockerfile .
	$(CONTAINER_TOOL) buildx build --platform linux/amd64,linux/arm64 \
		-t ghcr.io/jsquirrelz/tide-claude-subagent:$(IMAGE_TAG) -f images/claude-subagent/Dockerfile .

.PHONY: build-installer
build-installer: manifests generate kustomize ## Generate a consolidated YAML with CRDs and deployment.
	mkdir -p dist
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default > dist/install.yaml

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" apply -f -; else echo "No CRDs to install; skipping."; fi

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	@out="$$( "$(KUSTOMIZE)" build config/crd 2>/dev/null || true )"; \
	if [ -n "$$out" ]; then echo "$$out" | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -; else echo "No CRDs to delete; skipping."; fi

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && "$(KUSTOMIZE)" edit set image controller=${IMG}
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" apply -f -

.PHONY: undeploy
undeploy: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	"$(KUSTOMIZE)" build config/default | "$(KUBECTL)" delete --ignore-not-found=$(ignore-not-found) -f -

##@ Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p "$(LOCALBIN)"

## Tool Binaries
KUBECTL ?= kubectl
KIND ?= kind
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOLANGCI_LINT = $(LOCALBIN)/golangci-lint
HELMIFY ?= $(LOCALBIN)/helmify

## Tool Versions
KUSTOMIZE_VERSION ?= v5.8.1
CONTROLLER_TOOLS_VERSION ?= v0.20.1
# HELMIFY_VERSION pinned (Plan 11 / revision Info 11) for reproducible chart generation across executor runs and CI.
HELMIFY_VERSION ?= v0.4.17

#ENVTEST_VERSION is the version of controller-runtime release branch to fetch the envtest setup script (i.e. release-0.20)
ENVTEST_VERSION ?= $(shell v='$(call gomodver,sigs.k8s.io/controller-runtime)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_VERSION manually (controller-runtime replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?([0-9]+)\.([0-9]+).*/release-\1.\2/')

#ENVTEST_K8S_VERSION is the version of Kubernetes to use for setting up ENVTEST binaries (i.e. 1.31)
ENVTEST_K8S_VERSION ?= $(shell v='$(call gomodver,k8s.io/api)'; \
  [ -n "$$v" ] || { echo "Set ENVTEST_K8S_VERSION manually (k8s.io/api replace has no tag)" >&2; exit 1; }; \
  printf '%s\n' "$$v" | sed -E 's/^v?[0-9]+\.([0-9]+).*/1.\1/')

GOLANGCI_LINT_VERSION ?= v2.11.4
.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary.
$(KUSTOMIZE): $(LOCALBIN)
	$(call go-install-tool,$(KUSTOMIZE),sigs.k8s.io/kustomize/kustomize/v5,$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary.
$(CONTROLLER_GEN): $(LOCALBIN)
	$(call go-install-tool,$(CONTROLLER_GEN),sigs.k8s.io/controller-tools/cmd/controller-gen,$(CONTROLLER_TOOLS_VERSION))

.PHONY: setup-envtest
setup-envtest: envtest ## Download the binaries required for ENVTEST in the local bin directory.
	@echo "Setting up envtest binaries for Kubernetes version $(ENVTEST_K8S_VERSION)..."
	@"$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path || { \
		echo "Error: Failed to set up envtest binaries for version $(ENVTEST_K8S_VERSION)."; \
		exit 1; \
	}

.PHONY: envtest
envtest: $(ENVTEST) ## Download setup-envtest locally if necessary.
$(ENVTEST): $(LOCALBIN)
	$(call go-install-tool,$(ENVTEST),sigs.k8s.io/controller-runtime/tools/setup-envtest,$(ENVTEST_VERSION))

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	$(call go-install-tool,$(GOLANGCI_LINT),github.com/golangci/golangci-lint/v2/cmd/golangci-lint,$(GOLANGCI_LINT_VERSION))
	@test -f .custom-gcl.yml && { \
		echo "Building custom golangci-lint with plugins..." && \
		$(GOLANGCI_LINT) custom --destination $(LOCALBIN) --name golangci-lint-custom && \
		mv -f $(LOCALBIN)/golangci-lint-custom $(GOLANGCI_LINT); \
	} || true

.PHONY: helmify
helmify: $(HELMIFY) ## Download helmify locally if necessary (pinned to HELMIFY_VERSION).
$(HELMIFY): $(LOCALBIN)
	$(call go-install-tool,$(HELMIFY),github.com/arttor/helmify/cmd/helmify,$(HELMIFY_VERSION))

# go-install-tool will 'go install' any package with custom target and name of binary, if it doesn't exist
# $1 - target path with name of binary
# $2 - package url which can be installed
# $3 - specific version of package
define go-install-tool
@[ -f "$(1)-$(3)" ] && [ "$$(readlink -- "$(1)" 2>/dev/null)" = "$(1)-$(3)" ] || { \
set -e; \
package=$(2)@$(3) ;\
echo "Downloading $${package}" ;\
rm -f "$(1)" ;\
GOBIN="$(LOCALBIN)" go install $${package} ;\
mv "$(LOCALBIN)/$$(basename "$(1)")" "$(1)-$(3)" ;\
} ;\
ln -sf "$$(realpath "$(1)-$(3)")" "$(1)"
endef

define gomodver
$(shell go list -m -f '{{if .Replace}}{{.Replace.Version}}{{else}}{{.Version}}{{end}}' $(1) 2>/dev/null)
endef

##@ DAG Import Firewall (DAG-05)

.PHONY: verify-dag-imports
verify-dag-imports: ## Assert pkg/dag has no k8s.io/sigs.k8s.io/anthropics imports (DAG-05).
	@echo "verifying pkg/dag has no forbidden imports (DAG-05)..."
	@FORBIDDEN=$$(go list -deps ./pkg/dag/... 2>/dev/null | grep -E '^(k8s\.io/|sigs\.k8s\.io/|github\.com/anthropics/)' || true); \
	if [ -n "$$FORBIDDEN" ]; then \
		echo "DAG-05 violation: pkg/dag transitively depends on forbidden modules:"; \
		echo "$$FORBIDDEN"; \
		exit 1; \
	fi
	@echo "OK: pkg/dag imports are clean"

##@ Dispatch Import Firewall (SUB-01 / DAG-05 mirror)

.PHONY: verify-dispatch-imports
verify-dispatch-imports: ## Assert pkg/dispatch has no controller-runtime/anthropics/internal imports (SUB-01).
	@echo "verifying pkg/dispatch has no forbidden imports (SUB-01 / DAG-05-mirror)..."
	@# Narrow firewall (Phase 3 D-A1 / D-C3 + plan 03-01): pkg/dispatch is
	@# permitted to import k8s.io/apimachinery/pkg/runtime (for ChildCRDSpec's
	@# runtime.RawExtension — typed-but-deferred-decode escape hatch that keeps
	@# pkg/dispatch from importing api/v1alpha*) and its required transitive
	@# closure (sigs.k8s.io/json, sigs.k8s.io/structured-merge-diff,
	@# k8s.io/kube-openapi, k8s.io/klog). Other k8s.io/* and sigs.k8s.io/*
	@# packages (notably sigs.k8s.io/controller-runtime — manager/client/etc.)
	@# remain forbidden; LLM SDKs (github.com/anthropics/*) remain forbidden.
	@FORBIDDEN=$$(go list -deps ./pkg/dispatch/... 2>/dev/null \
		| grep -v '^github\.com/jsquirrelz/tide/pkg/dispatch' \
		| grep -E '^(k8s\.io/|sigs\.k8s\.io/|github\.com/anthropics/)' \
		| grep -v '^k8s\.io/apimachinery/' \
		| grep -v '^k8s\.io/klog/' \
		| grep -v '^k8s\.io/kube-openapi/' \
		| grep -v '^sigs\.k8s\.io/json' \
		| grep -v '^sigs\.k8s\.io/structured-merge-diff/' \
		|| true); \
	if [ -n "$$FORBIDDEN" ]; then \
		echo "SUB-01 / DAG-05-mirror violation: pkg/dispatch transitively depends on forbidden modules:"; \
		echo "$$FORBIDDEN"; \
		exit 1; \
	fi
	@echo "OK: pkg/dispatch imports are clean"

##@ Custom Analyzers (POOL-03 / Pitfall 6 + SUB-05 / Pitfall 14 + OBS-02 / Pitfall 17)

.PHONY: tide-lint
tide-lint: ## Run TIDE custom analyzers (POOL-03 / Pitfall 6 + SUB-05 / Pitfall 14 + OBS-02 / Pitfall 17 enforcement).
	go run ./cmd/tide-lint ./...

##@ Import firewall (SUB-05 / Pitfall 14 — Phase 2)

.PHONY: verify-import-firewall
verify-import-firewall: demo-fixture ## Run providerfirewall analyzer via tide-lint multichecker (SUB-05 / Pitfall 14). Fails on any LLM SDK import inside firewalled boundaries.
	go run ./cmd/tide-lint ./...

##@ PERSIST gates (PERSIST-01, PERSIST-02 / Pitfall 4)

.PHONY: verify-no-aggregates
verify-no-aggregates: ## Assert api/v1alpha* declares no aggregate schedule fields (PERSIST-02 / Pitfall 4).
	@echo "verifying no aggregate schedule fields on api/v1alpha* types (PERSIST-02)..."
	@FILES=$$(ls api/v1alpha*/*_types.go 2>/dev/null); \
	if [ -z "$$FILES" ]; then \
		echo "no api/v1alpha*/*_types.go files found — gate misconfigured"; \
		exit 1; \
	fi; \
	MATCHES=$$(grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' $$FILES || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-02 violation: aggregate schedule fields detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no aggregate schedule fields"

##@ Legacy API-Version Regression Gate (CRANK-07 / Phase 40 close)

# verify-no-legacy-api-refs is the durable, CI-wired successor to Phase 40's
# terminal grep sweep ("zero legacy schema-revision references outside a
# sanctioned set"): it turns a one-off closeout check into a permanent
# regression guard, so a future contributor can't silently reintroduce the
# two prior CRD schema revisions (the same failure class as the dead
# verify-no-aggregates glob this phase also hardened, D-12).
#
# CRITICAL self-match hazard: this recipe must never spell out the literal
# version tokens it hunts for, or it would flag its own definition — every
# pattern below is built at runtime by concatenating a $$PAT variable.
#
# Exclusions (each is a locked decision, not a convenience):
#   - docs/migration/, docs/audit/, docs/superpowers/ (whole dirs) — migration
#     guides legitimately narrate prior schema revisions; audit/superpowers
#     are dated historical snapshots preserved per D-12.
#   - AGENTS.md (whole file) — generic kubebuilder tutorial boilerplate with
#     example.com groups (a RESEARCH false positive, not a real reference).
#   - examples/projects/dogfood/salvage-*/ (whole dirs) — dated historical
#     dogfood-run archives (captured envelopes/events/plan artifacts); same
#     D-12 preservation principle already established for this bundle's
#     events.jsonl transcripts in plan 40-03.
#   - the dispatch-contract envelope group line (D-08) — kept fixed by
#     design, deliberately decoupled from the CRD group.
#   - lines naming the migration-doc path (guard constant, doc links).
#   - the external Krew plugin-manifest API group line (unrelated to TIDE's
#     own CRDs).
#   - the bare pre-decoupling CRD-group string, scoped to its one legal home
#     pkg/dispatch/envelope_test.go (plan 40-02 D-08 drift-boundary test:
#     proves the validator rejects it — this IS the regression coverage for
#     this gate's own concern, not a stray reference; NOT filtered anywhere
#     else in the tree, so a real reintroduction elsewhere still fails).
#   - the wrong-value SchemaRevision literal, scoped to its one legal home
#     internal/controller/project_controller_v2_guard_test.go (D-04 guard
#     test: proves the fail-closed guard rejects a stale revision; NOT
#     filtered anywhere else in the tree).
.PHONY: verify-no-legacy-api-refs
verify-no-legacy-api-refs: ## Assert zero legacy CRD schema-revision references outside the sanctioned set (CRANK-07).
	@echo "verifying no legacy API-version references outside the sanctioned set (CRANK-07)..."
	@PAT=v1alpha; \
	MATCHES=$$(git grep -nIE "$${PAT}1|$${PAT}2" -- . \
		':(exclude,glob)**/.planning/**' ':(exclude,glob)**/.worktrees/**' \
		':(exclude,glob)**/node_modules/**' ':(exclude,glob)**/bin/**' ':(exclude,glob)**/dist/**' \
		':(exclude,glob)**/migration/**' ':(exclude,glob)**/audit/**' ':(exclude,glob)**/superpowers/**' \
		':(exclude,glob)**/salvage-20260618/**' ':(exclude,glob)**/salvage-20260628/**' \
		':(exclude,glob)**/AGENTS.md' \
		2>/dev/null \
		| grep -v "dispatch\.tideproject\.k8s/$${PAT}1" \
		| grep -v "$${PAT}2-to-$${PAT}3" \
		| grep -v "krew\.googlecontainertools\.github\.com/$${PAT}2" \
		| grep -v "envelope_test\.go:[0-9]*:.*\"tideproject\.k8s/$${PAT}1\"" \
		| grep -v "project_controller_v2_guard_test\.go:[0-9]*:.*SchemaRevision: \"$${PAT}2\"" \
		|| true); \
	if [ -n "$$MATCHES" ]; then \
		echo "CRANK-07 violation: legacy API-version references detected outside the sanctioned set:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no legacy API-version references"

.PHONY: verify-no-sqlite-dep
verify-no-sqlite-dep: ## Assert go.mod has no DB driver dependencies (PERSIST-01).
	@echo "verifying no DB driver deps in go.mod (PERSIST-01)..."
	@MATCHES=$$(grep -nE 'database/sql|github.com/mattn/go-sqlite3|gorm\.io|github.com/jackc/pgx' go.mod || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-01 violation: forbidden DB drivers in go.mod:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no DB driver deps"

##@ Reconcile-loop blocking-I/O gate (Pitfall 1)

.PHONY: verify-no-blocking
verify-no-blocking: ## Assert no time.Sleep or <-time.After in reconciler bodies (Pitfall 1).
	@echo "verifying no time.Sleep or <-time.After in Reconcile bodies (Pitfall 1)..."
	@MATCHES=$$(grep -nE 'time\.Sleep|<-time\.After' internal/controller/*_controller.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "Pitfall 1 violation: blocking I/O in reconcile body:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no blocking I/O in reconcile bodies"

##@ RBAC wildcard gates (AUTH-03 / Pitfall 15)

.PHONY: verify-no-rbac-wildcards
verify-no-rbac-wildcards: ## Assert no RBAC wildcards in config/rbac/ generated manifests (AUTH-03 / Pitfall 15).
	@echo "verifying no RBAC wildcards in config/rbac/ (AUTH-03 / Pitfall 15)..."
	@MATCHES=$$(grep -nrE 'verbs:.*"?\*"?|resources:.*"?\*"?' config/rbac/ 2>/dev/null || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "AUTH-03 violation: RBAC wildcards detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no RBAC wildcards"

.PHONY: verify-rbac-marker-discipline
verify-rbac-marker-discipline: ## Assert no wildcard kubebuilder:rbac markers in source (AUTH-03 / Pitfall 15).
	@echo "verifying no RBAC wildcards in source markers (AUTH-03 / Pitfall 15)..."
	@# Scoped to *_controller.go (production reconciler files only). Test files
	@# legitimately contain marker-shaped string literals as fixtures
	@# (e.g. internal/controller/rbac_guard_test.go's TestRBACMarkerDiscipline*).
	@MATCHES=$$(grep -nE 'kubebuilder:rbac.*verbs=\*|kubebuilder:rbac.*resources=\*' internal/controller/*_controller.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "AUTH-03 violation: RBAC wildcard markers detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no RBAC wildcard markers"

##@ Helm Chart Validation (Phase 4 plan 04-14 — D-X3 / T-04-D2)

.PHONY: helm-lint-validate
helm-lint-validate: ## Helm chart sanity: helm lint + helm template renders without error.
	@helm lint charts/tide
	@helm template charts/tide > /dev/null
	@echo "PASS: helm lint + helm template render"

.PHONY: helm-rbac-assert
helm-rbac-assert: ## Assert dashboard ClusterRole verbs are read-only {get, list, watch} (T-04-D2 mitigation).
	@# Walk the rendered chart for any ClusterRole whose name contains "dashboard",
	@# extract every rules[].verbs[] entry, and fail if any verb is not in
	@# {get, list, watch}. The Phase 4 dashboard ClusterRole is the only
	@# "dashboard"-named role in the chart (templates/dashboard-rbac.yaml).
	@# Implementation uses python3 + PyYAML (already required by hack/helm/
	@# augment-tide-chart.sh) so this target has zero new tool dependencies.
	@helm template charts/tide --set dashboard.enabled=true > /tmp/tide-helm-render.yaml
	@python3 hack/helm/assert-dashboard-rbac.py /tmp/tide-helm-render.yaml

.PHONY: helm-telemetry-assert
helm-telemetry-assert: ## Assert PROM_ENDPOINT/PHOENIX_BASE_URL/OTEL_EXPORTER_OTLP_HEADERS env injection and telemetry render gates (Phase 16 TELEM-05 D-13 / Phase 46 OBS-01/OBS-04 / Phase 47 PHX-02/D-08).
	@helm template charts/tide --set dashboard.enabled=true > /tmp/tide-helm-render.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render.yaml --expect-absent
	@helm template charts/tide --set dashboard.enabled=true --set prometheus.endpoint=http://prom:9090 > /tmp/tide-helm-render-prom.yaml
	@python3 hack/helm/assert-prometheus-env.py /tmp/tide-helm-render-prom.yaml --expect-endpoint http://prom:9090
	@python3 hack/helm/assert-phoenix-env.py /tmp/tide-helm-render.yaml --expect-absent
	@helm template charts/tide --set dashboard.enabled=true --set phoenix.baseURL=http://phoenix:6006 > /tmp/tide-helm-render-phoenix.yaml
	@python3 hack/helm/assert-phoenix-env.py /tmp/tide-helm-render-phoenix.yaml --expect-value http://phoenix:6006
	@python3 hack/helm/assert-otlp-headers-env.py /tmp/tide-helm-render.yaml --expect-absent
	@helm template charts/tide --set dashboard.enabled=true --set otel.exporter.headersSecretRef.name=tide-otlp-headers > /tmp/tide-helm-render-otlpheaders.yaml
	@python3 hack/helm/assert-otlp-headers-env.py /tmp/tide-helm-render-otlpheaders.yaml --expect-secretref tide-otlp-headers OTEL_EXPORTER_OTLP_HEADERS
	@bash hack/helm/assert-telemetry-render.sh

.PHONY: helm-assert
helm-assert: helm-rbac-assert helm-telemetry-assert ## Run all Helm render gate assertions (Phase 16 TELEM-05 D-13).

##@ Legal compliance gates (Phase 5 DIST-03 — Plan 05-01)

.PHONY: verify-license
verify-license: ## Phase 5 DIST-03 — verify LICENSE+NOTICE+Go-header coverage.
	@bash hack/scripts/verify-license.sh

##@ Docs coverage gate (Phase 5 DIST-04 — Plan 05-04)

.PHONY: verify-docs
verify-docs: ## Phase 5 DIST-04 — verify docs/README.md index + all referenced docs present.
	@bash hack/scripts/verify-docs-coverage.sh --strict

##@ Per-namespace RBAC render gate (Phase 5 DIST-01 + AUTH-02 — Plan 05-13)

.PHONY: test-per-ns-rb
test-per-ns-rb: ## Phase 5 DIST-01 / AUTH-02 — verify per-namespace-rolebinding.yaml renders correctly.
	@bash hack/scripts/test-per-ns-rb.sh

##@ Phase 5 v1.0 ship gates (DIST-05 dry-run + BOOT-02/BOOT-04 acceptance — Plan 05-15)

# dry-run-v1 (D-D1..D-D4): scripted Docker-in-Docker exercise of the README
# Quickstart against a clean ubuntu:24.04 image, timed end-to-end. Small-sample
# Project Status.Phase=Complete is the timer-stop. Plan 05-16 wires this into
# release.yaml on `v*-rc.*` tags; runs locally without CI integration via this
# target. $0 LLM cost — uses the stub-subagent path.
DRY_RUN_IMAGE ?= ubuntu:24.04
DRY_RUN_TIMEOUT_SECONDS ?= 1800

.PHONY: dry-run-v1
dry-run-v1: ## Phase 5 D-D1 — Docker-in-Docker external-operator dry-run (≤ 30 min, $0 LLM cost).
	@hack/scripts/dry-run-v1.sh

# acceptance-v1 (D-A1..D-A4): maintainer-only ritual on the dev laptop. Fresh
# kind cluster + helm install + applies examples/projects/large/project.yaml +
# watches Project to Complete + captures evidence under .acceptance-runs/<ts>/.
# Hard $25 cap, no bypass. NOT wired into CI — maintainer-driven only.
.PHONY: acceptance-v1
acceptance-v1: ## Phase 5 D-A4 — maintainer ritual ($25 hard cap; requires ANTHROPIC_API_KEY).
	@if [ -z "$$ANTHROPIC_API_KEY" ]; then \
	  echo "ERROR: ANTHROPIC_API_KEY env not set — refusing to run acceptance-v1"; \
	  echo "       See docs/INSTALL.md for Secret setup."; \
	  exit 1; \
	fi
	@hack/scripts/acceptance-v1.sh

.PHONY: acceptance-v1-smoke
acceptance-v1-smoke: ## Phase 6 ACC-01 — $0 BOOT-04 revalidation (stub-subagent, no API key required).
	@ACCEPTANCE_SAMPLE=small hack/scripts/acceptance-v1.sh

##@ Helm Chart Generation (D-E1, D-E2 — Plan 11)

# Two-chart pair, both helmify-driven from kubebuilder's config/ Kustomize output:
#   charts/tide/      — controller (Deployment, RBAC, ServiceAccount, webhook configs, …)
#   charts/tide-crds/ — CRDs as a dedicated subchart for safe `helm upgrade` (REQ-DIST-01).
# Phase 5 inherits this scaffold and adds dashboard templates + ServiceMonitor + LICENSE
# headers; Phase 1 commits to the helmify-driven posture so no structural rework is needed
# at distribution time.
.PHONY: helm helm-controller helm-crds

helm: helm-controller helm-crds ## Generate both Helm charts (controller + CRD subchart).

helm-controller: manifests kustomize helmify ## Generate charts/tide/ (controller-only chart) via helmify, then augment.
	@echo "generating charts/tide/ via helmify (controller-only)..."
	@mkdir -p charts/tide
	@"$(KUSTOMIZE)" build config/default | "$(HELMIFY)" charts/tide
	@# Helmify regenerates values.yaml + Chart.yaml + deployment.yaml on every
	@# run, wiping out project-specific augmentations (ghcr image refs, Phase 1
	@# tunables, deduplicated webhook ports, hand-authored ConfigMap). The
	@# augment script re-applies them from hack/helm/ so `make helm` is
	@# idempotent and reproducible. See hack/helm/augment-tide-chart.sh for the
	@# canonical override sources.
	@bash hack/helm/augment-tide-chart.sh

helm-crds: manifests kustomize helmify ## Generate charts/tide-crds/ (CRD subchart) via helmify, then augment.
	@echo "generating charts/tide-crds/ via helmify (CRD subchart)..."
	@mkdir -p charts/tide-crds
	@"$(KUSTOMIZE)" build config/crd | "$(HELMIFY)" charts/tide-crds
	@bash hack/helm/augment-tide-crds-chart.sh

##@ Dev hooks & release versioning

.PHONY: hooks
hooks: ## Install the repo-committed git hooks (pre-commit + pre-push) from .pre-commit-config.yaml.
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not found — install it: 'brew install pre-commit' or 'pipx install pre-commit'"; exit 1; }
	pre-commit install --install-hooks --hook-type pre-commit --hook-type pre-push
	@echo "OK: pre-commit + pre-push hooks installed. Dry-run with 'make hooks-run'."

.PHONY: hooks-run
hooks-run: ## Run every pre-commit hook against all tracked files (manual dry-run, no commit needed).
	@command -v pre-commit >/dev/null 2>&1 || { echo "pre-commit not found — see 'make hooks'"; exit 1; }
	pre-commit run --all-files --hook-stage pre-commit
	pre-commit run --all-files --hook-stage pre-push

.PHONY: fmt-check
fmt-check: ## Assert gofmt leaves no diffs (pre-push gate; non-mutating counterpart to 'make fmt').
	@echo "checking gofmt..."
	@UNFORMATTED=$$(gofmt -l $$(git ls-files '*.go')); \
	if [ -n "$$UNFORMATTED" ]; then \
		echo "gofmt: not formatted (run 'make fmt' or 'gofmt -w'):"; \
		echo "$$UNFORMATTED"; \
		exit 1; \
	fi
	@echo "OK: all Go files gofmt-clean"

.PHONY: tidy-check
tidy-check: ## Assert 'go mod tidy' leaves go.mod/go.sum unchanged (pre-push gate).
	@echo "checking go.mod/go.sum are tidy..."
	@go mod tidy
	@git diff --exit-code go.mod go.sum || { echo "go.mod/go.sum not tidy — commit the 'go mod tidy' result"; exit 1; }
	@echo "OK: go.mod/go.sum tidy"

.PHONY: verify-chart-reproducible
verify-chart-reproducible: helm ## Regenerate charts via helmify and fail if the committed charts/ tree drifted (chart-source reproducibility; mirrors the ci.yaml helm-lint gate).
	@echo "verifying charts/ tree matches a fresh 'make helm'..."
	@if ! git diff --quiet -- charts/; then \
		echo "charts/ drifted from a fresh 'make helm' regeneration — regenerate and stage charts/:"; \
		git diff --stat -- charts/; \
		exit 1; \
	fi
	@echo "OK: charts/ tree is reproducible from hack/helm/ source"

.PHONY: verify-version-consistency
verify-version-consistency: ## Assert all chart version + appVersion fields agree. Optional: make verify-version-consistency VERSION=X.Y.Z to also pin.
	@bash hack/scripts/verify-version-consistency.sh $(VERSION)

.PHONY: verify-chart-images-published
verify-chart-images-published: ## Assert every ghcr.io/jsquirrelz/* image the chart references is built by the release build-images matrix (matrix<->chart coverage gate).
	@bash hack/scripts/verify-chart-images-published.sh

.PHONY: bump-version
bump-version: ## Set chart version + appVersion across every version-bearing file (release STEP ONE). Usage: make bump-version VERSION=X.Y.Z
	@if [ -z "$(VERSION)" ]; then echo "usage: make bump-version VERSION=X.Y.Z"; exit 2; fi
	@bash hack/scripts/bump-version.sh $(VERSION)

##@ LangGraph Verifier Test Infra (Phase 48 EVAL-01/02)

.PHONY: test-langgraph-verifier
test-langgraph-verifier: ## Idempotently create cmd/tide-langgraph-verifier/.venv, install both lockfiles, and run its pytest suite.
	@echo "setting up cmd/tide-langgraph-verifier/.venv (uv, python3.13)..."
	@cd cmd/tide-langgraph-verifier && \
		uv venv --python 3.13 .venv --allow-existing && \
		uv pip install --python .venv/bin/python --require-hashes -r requirements.txt -r requirements-dev.txt && \
		.venv/bin/python -m pytest verifier/tests/ -x -q

##@ LangGraph Pin Gate (EVAL-02 / Pitfall 3)

# PINS_GLOB is overridable so a negative self-test can point the gate at a
# fixture file without touching the real pin files.
PINS_GLOB ?= cmd/tide-langgraph-verifier/requirements*.in

.PHONY: verify-langgraph-pins
verify-langgraph-pins: ## Assert cmd/tide-langgraph-verifier/requirements*.in pins every direct dependency patch-exact (EVAL-02). Override glob: make verify-langgraph-pins PINS_GLOB=<path>.
	@echo "verifying $(PINS_GLOB) has no range/unpinned specifiers..."
	@FILES=$$(ls $(PINS_GLOB) 2>/dev/null); \
	if [ -z "$$FILES" ]; then \
		echo "no files matched $(PINS_GLOB) — gate misconfigured"; \
		exit 1; \
	fi; \
	VIOLATIONS=""; \
	for f in $$FILES; do \
		V=$$(grep -vE '^\s*(#|$$)' "$$f" | grep -vE '^[A-Za-z0-9_.-]+==[0-9]' || true); \
		if [ -n "$$V" ]; then \
			VIOLATIONS=$$(printf '%s\n%s:\n%s\n' "$$VIOLATIONS" "$$f" "$$V"); \
		fi; \
	done; \
	if [ -n "$$VIOLATIONS" ]; then \
		echo "EVAL-02 violation: range/unpinned specifiers detected:"; \
		echo "$$VIOLATIONS"; \
		exit 1; \
	fi
	@echo "OK: all direct pins are patch-exact"
