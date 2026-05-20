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
INTEGRATION_TIMEOUT ?= 1800s
KIND_GO_TEST_TIMEOUT ?= 20m

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

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet setup-envtest ## Run tests (TEST-01: -short skips the slow leader-election envtest; budget < 30s).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -short -timeout 60s $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: test-only
test-only: ## Run go test without re-running manifests/generate/fmt/vet/setup-envtest (assumes prep already done). Used by CI's TEST-01 timing assertion.
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test -short -timeout 60s $$(go list ./... | grep -v /e2e) -coverprofile cover.out

.PHONY: test-leader-election
test-leader-election: manifests generate fmt vet setup-envtest ## Run the slow CTRL-03 leader-election envtest (~60s; excluded from `make test`).
	KUBEBUILDER_ASSETS="$(shell "$(ENVTEST)" use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" go test ./internal/controller/... -timeout 180s -v -ginkgo.focus="Leader Election"

# TODO(user): To use a different vendor for e2e tests, modify the setup under 'tests/e2e'.
# The default setup assumes Kind is pre-installed and builds/loads the Manager Docker image locally.
# kubectl kuberc is disabled by default for test isolation; enable with:
# - KUBECTL_KUBERC=true
# CertManager is installed by default; skip with:
# - CERT_MANAGER_INSTALL_SKIP=true
KIND_CLUSTER ?= tide-test-e2e

.PHONY: setup-test-e2e
setup-test-e2e: ## Set up a Kind cluster for e2e tests if it does not exist
	@command -v $(KIND) >/dev/null 2>&1 || { \
		echo "Kind is not installed. Please install Kind manually."; \
		exit 1; \
	}
	@case "$$($(KIND) get clusters)" in \
		*"$(KIND_CLUSTER)"*) \
			echo "Kind cluster '$(KIND_CLUSTER)' already exists. Skipping creation." ;; \
		*) \
			echo "Creating Kind cluster '$(KIND_CLUSTER)'..."; \
			$(KIND) create cluster --name $(KIND_CLUSTER) ;; \
	esac

.PHONY: test-e2e
test-e2e: setup-test-e2e manifests generate fmt vet ## Run the e2e tests. Expected an isolated environment using Kind.
	KIND=$(KIND) KIND_CLUSTER=$(KIND_CLUSTER) go test -tags=e2e ./test/e2e/ -v -ginkgo.v
	$(MAKE) cleanup-test-e2e

.PHONY: cleanup-test-e2e
cleanup-test-e2e: ## Tear down the Kind cluster used for e2e tests
	@$(KIND) delete cluster --name $(KIND_CLUSTER)

##@ Phase 4 kind-harness E2E (plan 04-14)

# test-e2e-kind builds the manager + dashboard + tide CLI binaries, spins up a
# dedicated kind cluster (`tide-e2e-phase4`), helm-installs the chart with
# dashboard.enabled=true, and runs the kind_e2e-tagged specs under test/e2e/.
#
# Separate from `test-e2e` because the two suites use different paradigms:
#   - `test-e2e` (kubebuilder): kustomize-driven `make deploy` against `tide-test-e2e` cluster
#   - `test-e2e-kind` (Phase 4): helm-driven chart install against `tide-e2e-phase4` cluster
#
# Both are tagged `kind`-test work; both honor SKIP_KIND_TESTS=true to short-
# circuit on dev machines without container tooling. The full Phase 4 E2E gate
# runs `test-e2e-kind`; the kubebuilder `test-e2e` continues to cover the
# kustomize install path.
.PHONY: test-e2e-kind
test-e2e-kind: tide-cli ## Phase 4 plan 04-14 kind E2E suite (dashboard + gate-flow + tail cancellation).
	go test -tags=kind_e2e -timeout=15m ./test/e2e/... -v -ginkgo.v

##@ Integration tests (TEST-02 — Phase 2)

.PHONY: test-int test-int-fast test-int-kind-prep

test-int: manifests generate fmt vet setup-envtest test-int-kind-prep ## Run full integration test suite: Layer A (envtest) + Layer B (kind). Requires Docker + kind.
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		timeout $(INTEGRATION_TIMEOUT) go test ./test/integration/... -v -timeout=$(KIND_GO_TEST_TIMEOUT) -ginkgo.v

test-int-fast: manifests generate fmt vet setup-envtest ## Run Layer A integration tests only (envtest; no Docker/kind needed). Target: ~90s.
	KUBEBUILDER_ASSETS="$$($(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir "$(LOCALBIN)" -p path)" \
		go test ./test/integration/envtest/... -v -timeout=2m -ginkgo.v --ginkgo.label-filter='envtest'

test-int-kind-prep: ## Build manager + stub-subagent + credproxy + tide-push Docker images and load them into the tide-test kind cluster.
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-stub-subagent:test -f images/stub-subagent/Dockerfile .
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-credproxy:test -f images/credproxy/Dockerfile .
	$(CONTAINER_TOOL) build -t ghcr.io/jsquirrelz/tide-push:test -f images/tide-push/Dockerfile .
	$(CONTAINER_TOOL) build -t controller:test -f Dockerfile .
	@if ! $(KIND) get clusters 2>/dev/null | grep -q "^tide-test$$"; then \
		$(KIND) create cluster --name tide-test --config test/integration/kind/cluster.yaml; \
	fi
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-stub-subagent:test --name tide-test
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-credproxy:test --name tide-test
	# Phase 3 plan 03-10: preload tide-push (push_lease_test.go's mocked push Jobs
	# still reference the image; preload avoids ImagePullBackoff stalls).
	# Equivalent literal command at make-time: kind load docker-image ghcr.io/jsquirrelz/tide-push:test --name tide-test
	$(KIND) load docker-image ghcr.io/jsquirrelz/tide-push:test --name tide-test
	$(KIND) load docker-image controller:test --name tide-test

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

.PHONY: lint
lint: golangci-lint verify-dag-imports verify-dispatch-imports verify-import-firewall ## Run golangci-lint linter + import firewalls
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
.PHONY: docker-buildx
docker-buildx: ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name tide-builder
	$(CONTAINER_TOOL) buildx use tide-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm tide-builder
	rm Dockerfile.cross

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
	@# pkg/dispatch from importing api/v1alpha1) and its required transitive
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
verify-import-firewall: ## Run providerfirewall analyzer via tide-lint multichecker (SUB-05 / Pitfall 14). Fails on any LLM SDK import inside firewalled boundaries.
	go run ./cmd/tide-lint ./...

##@ PERSIST gates (PERSIST-01, PERSIST-02 / Pitfall 4)

.PHONY: verify-no-aggregates
verify-no-aggregates: ## Assert api/v1alpha1 declares no aggregate schedule fields (PERSIST-02 / Pitfall 4).
	@echo "verifying no aggregate schedule fields on api/v1alpha1 types (PERSIST-02)..."
	@MATCHES=$$(grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' api/v1alpha1/*_types.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-02 violation: aggregate schedule fields detected:"; \
		echo "$$MATCHES"; \
		exit 1; \
	fi
	@echo "OK: no aggregate schedule fields"

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
