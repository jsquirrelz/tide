# Phase 2: Dispatch & Plan Validation — Pattern Map

**Mapped:** 2026-05-12
**Files analyzed:** 47 new + 11 extended
**Analogs found:** 47 / 47 (100% — Phase 1 ships every shape Phase 2 needs)

This document maps every Phase 2 new/extended file to its closest Phase 1 analog, extracts the load-bearing patterns to mirror, and enumerates the conventions the planner must preserve across the phase.

---

## File Classification

### Public envelope contract (NEW package, leaf, stdlib-only)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `pkg/dispatch/envelope.go` | public types | transform | `pkg/dag/kahn.go` + `pkg/dag/errors.go` (leaf public package, stdlib-only) | exact |
| `pkg/dispatch/envelope_test.go` | test | transform | `pkg/dag/kahn_test.go` (stdlib `testing`, table-driven, fixture pinning) | exact |
| `pkg/dispatch/doc.go` (extend) | package docstring | — | `pkg/dag/doc.go` (worked-example + forbidden-import contract) | exact |

### Dispatch backend (NEW internal package)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/dispatch/dispatcher.go` | interface | request-response | `internal/dispatch/doc.go` (Phase 1 placeholder — direct replacement) | exact |
| `internal/dispatch/podjob/backend.go` | service | request-response | `internal/pool/pool.go` (internal helper pkg, ctor + methods, K8s-aware) | role-match |
| `internal/dispatch/podjob/jobspec.go` | utility | transform | `internal/owner/owner.go` (small helper, returns K8s object + error) | role-match |
| `internal/dispatch/podjob/names.go` | utility | transform | (no direct analog — invent; pattern follows `internal/pool/pool.go` ctor style) | partial |
| `internal/dispatch/podjob/backend_test.go` | test | request-response | `internal/pool/pool_test.go` (stdlib `testing`, fake client) | exact |

### Stub subagent (NEW cmd + image)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `cmd/stub-subagent/main.go` | cmd entry point | file-I/O | `cmd/tide-lint/main.go` (small `main`, focused responsibility, doc-comment driven) | role-match |
| `cmd/stub-subagent/main_test.go` | test | file-I/O | `internal/config/config_test.go` (stdlib `testing`, tmpdir + file I/O, no K8s) | role-match |
| `images/stub-subagent/Dockerfile` | config | — | (no analog — Dockerfile is fresh; mirror kubebuilder's root `Dockerfile`) | partial |

### Credential proxy (NEW internal package + cmd + image)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/credproxy/proxy.go` | service | request-response | `internal/pool/pool.go` (internal helper pkg shape) | role-match |
| `internal/credproxy/token.go` | utility | transform | `internal/owner/owner.go` (small helper, doc-comment driven) | role-match |
| `internal/credproxy/token_test.go` | test | transform | `internal/finalizer/finalizer_test.go` (stdlib `testing`, table-driven fixtures) | exact |
| `internal/credproxy/cert.go` | utility | transform | `internal/owner/owner.go` (helper signature shape) | partial |
| `cmd/credproxy/main.go` | cmd entry point | request-response | `cmd/manager/main.go` (flag parsing + signal handling + run loop) | role-match |
| `images/credproxy/Dockerfile` | config | — | (mirror kubebuilder root `Dockerfile`) | partial |

### Subagent harness (NEW internal package)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/harness/harness.go` | service | event-driven | `internal/finalizer/finalizer.go` (bounded-deadline, context-aware) | role-match |
| `internal/harness/caps.go` | utility | event-driven | `internal/finalizer/finalizer.go` (context.WithTimeout pattern) | role-match |
| `internal/harness/caps_test.go` | test | event-driven | `internal/finalizer/finalizer_test.go` (stdlib `testing`, fake client) | exact |
| `internal/harness/redact/redact.go` | utility | streaming | `pkg/dag/kahn.go` (leaf, stdlib-only, deterministic transform) | role-match |
| `internal/harness/redact/redact_test.go` | test | streaming | `pkg/dag/kahn_test.go` (stdlib `testing`, fixture-pinned table) | exact |
| `internal/harness/output.go` | utility | file-I/O | `internal/owner/owner.go` (small helper, validation-style errors) | role-match |
| `internal/harness/output_test.go` | test | file-I/O | `internal/finalizer/finalizer_test.go` (tmpdir + table-driven) | role-match |

### Budget + rate limiter (NEW internal package, controller-side)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/budget/bucket.go` | service | event-driven | `internal/pool/pool.go` (semaphore-style, ctor + Acquire/Release pair, sync primitives) | exact |
| `internal/budget/bucket_test.go` | test | event-driven | `internal/pool/pool_test.go` (stdlib `testing`, fake client) | exact |
| `internal/budget/tally.go` | utility | CRUD | `internal/owner/owner.go` (small helper, K8s object Patch) | role-match |

### Plan admission webhook (EXTEND existing Phase 1 Plan 07 scaffold)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/webhook/v1alpha1/plan_webhook.go` (extend) | webhook | request-response | `internal/webhook/v1alpha1/plan_webhook.go` (Phase 1 no-op — fill body; signature stays) | exact (self-extend) |
| `internal/webhook/v1alpha1/plan_webhook_test.go` (extend) | test | request-response | `internal/controller/plan_webhook_test.go` (Ginkgo, envtest, k8sClient) | exact (self-extend) |
| `internal/webhook/v1alpha1/strict_mode.go` (NEW) | utility | transform | `internal/owner/owner.go` (small helper, validation/lookup) | role-match |

### Reconciler Phase 2 logic (EXTEND existing reconciler bodies)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `internal/controller/task_controller.go` (extend) | controller | request-response | itself (line 54 Dispatcher field + line 113 `if r.Dispatcher != nil` seam) | exact (self-extend) |
| `internal/controller/wave_controller.go` (extend) | controller | request-response | itself (same six-step pattern) | exact (self-extend) |
| `internal/controller/plan_controller.go` (extend) | controller | request-response | itself (Plan owner watches Wave creation) | exact (self-extend) |
| `internal/controller/project_controller.go` (extend) | controller | request-response | itself (gains init Job creation + budget halt) | exact (self-extend) |
| `internal/controller/suite_test.go` (extend) | test bootstrap | event-driven | itself (Plan 07 already folded webhook into BeforeSuite — extend, not split) | exact (self-extend) |

### Project.Spec / TaskSpec schema additions (EXTEND existing types)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `api/v1alpha1/project_types.go` (extend) | model | CRUD | itself (ProjectSpec + nested struct + CEL `XValidation`) | exact (self-extend) |
| `api/v1alpha1/task_types.go` (extend, add `Dev.TestMode`) | model | CRUD | itself (TaskSpec + `// +optional` sub-struct convention) | exact (self-extend) |
| `api/v1alpha1/aggregates_guard_test.go` (extend) | test | — | itself (regex denylist + clean-fixture sanity) | exact (self-extend) |

### Provider firewall lint analyzer (NEW analyzer)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `tools/analyzers/providerfirewall/analyzer.go` | analyzer | transform | `tools/analyzers/dagimports/analyzer.go` (import-firewall analyzer, path-prefix denylist) | exact |
| `tools/analyzers/providerfirewall/analyzer_test.go` | test | transform | `tools/analyzers/dagimports/analyzer_test.go` (analysistest with `valid`/`violation` fixtures) | exact |
| `tools/analyzers/providerfirewall/testdata/src/{valid,violation}/...` | test fixtures | — | `tools/analyzers/dagimports/testdata/src/{valid,violation}/pkg/dag/dag.go` | exact |
| `cmd/tide-lint/main.go` (single-line change) | cmd entry point | — | itself (`singlechecker` → `multichecker` flip; commented in line 5) | exact (self-extend) |

### Integration test tier (NEW test packages)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `test/integration/envtest/suite_test.go` | test bootstrap | event-driven | `internal/controller/suite_test.go` (Ginkgo BeforeSuite, envtest, CRD paths) | exact |
| `test/integration/envtest/{admission,indegree,budget}_test.go` | test | event-driven | `internal/controller/plan_webhook_test.go` (Ginkgo Describe + Eventually) | exact |
| `test/integration/kind/suite_test.go` | test bootstrap | event-driven | `test/e2e/e2e_suite_test.go` (kind-based suite — referenced via existing `test/e2e/` directory) | partial |
| `test/integration/kind/cluster.yaml` | config | — | (no analog — invent; STACK.md prescribes SHA-pinned `kindest/node`) | partial |
| `test/integration/kind/dispatch_test.go` | test | event-driven | `internal/controller/plan_webhook_test.go` (Ginkgo Describe + Eventually with `kubectl`-equivalent k8sClient operations) | role-match |

### Helm chart additions (EXTEND existing values + augment layer)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `charts/tide/values.yaml` (extend) | config | — | itself (top-level tunables + helmify-emitted blocks; see `hack/helm/tide-values.yaml`) | exact (self-extend) |
| `charts/tide/templates/signing-secret.yaml` (NEW) | template | — | `charts/tide/templates/serviceaccount.yaml` (small hand-authorable template referenced by augment script) | role-match |
| `hack/helm/augment-tide-chart.sh` (extend) | shell script | — | itself (Phase 1 Plan 11 augment script) | exact (self-extend) |
| `hack/helm/tide-values.yaml` (extend) | config | — | itself (Phase 1 source-of-truth values) | exact (self-extend) |

### Makefile targets (EXTEND existing Makefile)

| File | Role | Data Flow | Closest Analog | Match Quality |
|------|------|-----------|----------------|---------------|
| `Makefile` (add `test-int`, `test-int-fast`, `test-int-kind-prep`, `verify-import-firewall`) | build config | — | itself (Phase 1 `test`/`test-only`/`verify-*` naming convention) | exact (self-extend) |

---

## Pattern Assignments

### `pkg/dispatch/envelope.go` (public types, transform)

**Analog:** `pkg/dag/kahn.go` + `pkg/dag/errors.go` (leaf, stdlib-only, public Go contract)

**File-header convention** — Phase 1 leaf packages OMIT the Apache header (`pkg/dag/kahn.go` line 1: bare `package dag`). The header is reserved for kubebuilder-templated files under `api/`, `cmd/manager/`, `internal/controller/`, `internal/webhook/`. New leaf packages follow the lean style:

```go
// pkg/dag/kahn.go lines 1–7
package dag

import (
	"fmt"
	"sort"
)
```

**Public-type style** — see `pkg/dag/kahn.go:11-18`:

```go
// NodeID is the unique identifier of a node in the DAG. Generic strings —
// callers project domain identifiers (Task names, artifact names) into this
// type.
type NodeID = string

// Edge expresses "From must complete before To."
type Edge struct {
	From NodeID
	To   NodeID
}
```

**Apply to envelope:** every exported type carries a doc-comment that explains the role + the consumer (the spec for `NodeID`, the contract for `Edge`). For `EnvelopeIn` / `EnvelopeOut`, doc-comment names the producer (orchestrator) and the consumer (subagent harness) explicitly. JSON tags pinned per D-A3 (`apiVersion: tideproject.k8s/v1alpha1`).

**Sibling error file pattern** — `pkg/dag/errors.go:1-16`:

```go
package dag

import "fmt"

// CycleError is returned by ComputeWaves when the input graph contains a cycle.
// InvolvedNodes lists every node whose indegree never reached zero …
type CycleError struct {
	InvolvedNodes []NodeID
}

func (e *CycleError) Error() string {
	return fmt.Sprintf("cyclic DAG: nodes with unresolvable indegrees: %v", e.InvolvedNodes)
}
```

**Apply to envelope:** any envelope-decode error type (e.g., `UnknownAPIVersionError`) lives in a sibling file (`pkg/dispatch/errors.go`) with the same `func (e *X) Error() string` shape and one-sentence doc-comment per field.

**Forbidden-import declaration pattern** — `pkg/dag/doc.go:18-23`:

```go
// Per DAG-05, this package MUST NOT import:
//   - k8s.io/*       (any)
//   - sigs.k8s.io/*  (any)
//   - github.com/anthropics/* (any)
//
// Enforced by the `make verify-dag-imports` Makefile target wired into CI.
```

**Apply to envelope:** `pkg/dispatch/doc.go` extension MUST declare the public-API stability contract explicitly (out-of-tree subagent authors import this) + reuse the same forbidden-import block (envelope is a leaf — no k8s.io/, no controller-runtime, no Anthropic SDK).

---

### `pkg/dispatch/envelope_test.go` (test, transform)

**Analog:** `pkg/dag/kahn_test.go`

**Test framework:** **stdlib `testing`** (NOT Ginkgo). Phase 1 D-C1 / 01-CONTEXT.md "Claude's Discretion" pins this: "pkg/dag may use stdlib testing with t.Run table tests since it has no async-poll requirements (Ginkgo is overkill there)." Same applies to `pkg/dispatch` — it's a stdlib-only leaf with no controller interaction.

**Table-driven + individually-named test pattern** — `pkg/dag/kahn_test.go:45-119` + `:183-235`:

```go
func TestComputeWaves(t *testing.T) {
	type tc struct {
		name      string
		nodes     []NodeID
		// …
	}
	cases := []tc{ /* … */ }
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			assertComputeWavesCase(t, c.nodes, c.edges, c.want, c.wantCycle, c.wantErr)
		})
	}
}

// Re-exposed top-level for selectable `-run`:
func TestComputeWaves_AlphaThroughTheta(t *testing.T) { … }
```

**Apply:** envelope round-trip tests (`Test_EnvelopeIn_RoundTrip`, `Test_EnvelopeIn_UnknownAPIVersion`, `Test_EnvelopeOut_RoundTrip`) follow this dual-shape (table + individual-name) so `go test -run TestEnvelope_UnknownAPIVersion` selects directly. Fixture inputs include explicit `apiVersion: tideproject.k8s/v1alpha1` (D-A3 contract). The `assertX` shared helper pattern is preserved.

---

### `pkg/dispatch/doc.go` (extend Phase 1 placeholder)

**Analog:** `internal/dispatch/doc.go` (the file being extended) + `pkg/dag/doc.go` (worked-example shape)

**Phase 1 placeholder structure to evolve** — `internal/dispatch/doc.go:1-30`:

```go
// Package dispatch is reserved for Phase 2's Subagent interface (REQ-SUB-01).
// …
// The Phase 2 contract (per 01-RESEARCH.md and PROJECT.md) is roughly:
//
//	type Dispatcher interface {
//	    Run(ctx context.Context, in EnvelopeIn) (EnvelopeOut, error)
//	}
package dispatch

type Dispatcher interface{}
```

**Note:** the public Phase 2 envelope types land at `pkg/dispatch/`, while the `Dispatcher` interface (orchestrator-side, K8s-aware) stays at `internal/dispatch.Dispatcher`. The reconciler struct field `Dispatcher dispatch.Dispatcher` on lines 54-55 of every controller resolves to `internal/dispatch.Dispatcher` — that import path does NOT change in Phase 2.

**Apply:** `pkg/dispatch/doc.go` (NEW, leaf-package style) declares the out-of-tree consumer contract explicitly:

```
// Package dispatch declares TIDE's public Subagent envelope contract.
//
// Out-of-tree subagent image authors import this package directly:
//   import "github.com/jsquirrelz/tide/pkg/dispatch"
// to decode the input envelope and produce the output envelope. No vendoring,
// no copy-paste. This is what makes the spec's "pluggable Subagent runtime
// via a documented container image contract" actually pluggable (D-A1).
//
// Per the leaf-package convention (mirroring pkg/dag), this package MUST NOT
// import:
//   - k8s.io/*       (any)
//   - sigs.k8s.io/*  (any)
//   - github.com/anthropics/* (any)
//   - any internal/ packages
```

`internal/dispatch/doc.go` shrinks to a one-sentence cross-reference: "See pkg/dispatch for the public envelope types; this package holds the orchestrator-side Dispatcher interface implemented by internal/dispatch/podjob."

---

### `internal/dispatch/dispatcher.go` (new — real interface body)

**Analog:** `internal/dispatch/doc.go:24-30` (Phase 1 `type Dispatcher interface{}` placeholder — direct replacement)

**Pattern:** the Phase 1 placeholder already documents the target signature in its doc-comment (lines 12-16). Phase 2 simply moves it from doc-comment to code:

```go
type Dispatcher interface {
	Run(ctx context.Context, in pkg.EnvelopeIn) (pkg.EnvelopeOut, error)
}
```

where `pkg` is the alias for `github.com/jsquirrelz/tide/pkg/dispatch`.

**Critical preservation rule:** the field declaration `Dispatcher dispatch.Dispatcher` on `internal/controller/{project,milestone,phase,plan,wave,task}_controller.go:54` MUST continue to resolve to this `internal/dispatch.Dispatcher` interface. The planner replaces the interface body, NOT the import path or field type.

---

### `internal/dispatch/podjob/backend.go` (new service)

**Analog:** `internal/pool/pool.go` (internal helper pkg with ctor + methods; K8s-aware via `client.Client`)

**Internal-package doc-comment style** — `internal/pool/pool.go:1-12`:

```go
// Package pool implements TIDE's two separately-sized parallelism budgets
// (planner pool, executor pool) as chan struct{} semaphores with a PreCharge
// helper that consumes slots equal to live Jobs at Manager startup.
//
// Per POOL-01: chan-based semaphore with Acquire/Release.
// Per POOL-02: PreCharge from live Job count via client.List.
//
// Pitfall 6 prevention (unified worker pool) is enforced separately by the
// crosspool analyzer in tools/analyzers/crosspool/. Phase 1 constructs both
// pools and calls PreCharge at Manager startup; Phase 2 is the first to call
// Acquire/Release from within the WaveReconciler dispatch path.
package pool
```

**Ctor + methods pattern** — `internal/pool/pool.go:27-95`:

```go
type Pool struct {
	sem  chan struct{}
	name string
}

func New(capacity int, name string) *Pool { … }

func (p *Pool) Acquire(ctx context.Context) error { … }
func (p *Pool) Release() { … }
func (p *Pool) PreCharge(ctx context.Context, c client.Client, labelSelector string) error { … }
```

**Apply to PodJobBackend:** declare `PodJobBackend` struct with `client.Client`, `scheme *runtime.Scheme`, image refs, signing secret, fsGroup, and `New(c, scheme, opts) *PodJobBackend` constructor. `Run(ctx, in)` is the Dispatcher interface satisfier — it Create-or-AlreadyExists the Job (deterministic name from `names.go`), watches for completion, reads `out.json`, returns `EnvelopeOut`.

**Error wrapping convention** — `internal/pool/pool.go:75,79,89`:

```go
return fmt.Errorf("pool %s: parse label selector %q: %w", p.name, labelSelector, err)
```

Phase 1's pattern: `fmt.Errorf("<pkg>: <action> %q: %w", arg, err)` with operator-helpful context. Apply throughout `PodJobBackend.Run`.

---

### `internal/dispatch/podjob/names.go` (new — deterministic Job names)

**Analog:** no direct Phase 1 analog (no naming helper exists yet). Use the shape of `internal/owner/owner.go` (small helper, function-only, returns string).

**Pattern:** a single exported function with a doc-comment naming the SUB-03 idempotency contract:

```go
// JobName returns the deterministic Job name for a Task's nth dispatch attempt
// per D-B5 / SUB-03. The name is the watch-lag dedup key — a duplicate Create
// on the same (taskUID, attempt) returns AlreadyExists and the reconciler
// treats that as success (Pitfall 11 prevention).
//
// Format: "tide-task-{taskUID}-{attempt}"
func JobName(taskUID types.UID, attempt int) string { … }
```

**Naming-prefix convention:** Phase 1 D-G1 commits to `tide-*` as the OSS-style image/Job prefix (CONTEXT.md "Established Patterns"). All Phase 2 Job names follow:
- `tide-task-{taskUID}-{attempt}` (Task dispatch)
- `tide-init-{projectUID}` (Project init — D-G1)
- `tide-stub-subagent` (image — D-F2)
- `tide-credproxy` (sidecar — D-C1)

---

### `internal/dispatch/podjob/backend_test.go` (test)

**Analog:** `internal/pool/pool_test.go` (NOT yet read in full but exists; mirrors `internal/finalizer/finalizer_test.go` style — confirmed by file listing).

**Pattern from `internal/finalizer/finalizer_test.go:1-33`:**

```go
package finalizer

import (
	"context"
	"errors"
	"testing"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func newFakeClient(t *testing.T, objs ...client.Object) client.Client {
	t.Helper()
	s := runtime.NewScheme()
	if err := scheme.AddToScheme(s); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	return fake.NewClientBuilder().WithScheme(s).WithObjects(objs...).Build()
}
```

**Apply:** unit tests for `PodJobBackend.Run` use `sigs.k8s.io/controller-runtime/pkg/client/fake` (NOT envtest) for the job-spec builder assertions. Envtest exercise lives in `test/integration/envtest/`.

---

### `cmd/stub-subagent/main.go` (new cmd)

**Analog:** `cmd/tide-lint/main.go` (small, doc-comment-driven main)

**File structure** — `cmd/tide-lint/main.go:1-30`:

```go
// Command tide-lint runs TIDE's custom go/analysis Passes against the
// module. Phase 1 ships exactly one analyzer — crosspool (POOL-03 /
// Pitfall 6 prevention, see tools/analyzers/crosspool). Phase 2+ may
// register additional analyzers; when that happens, swap
// singlechecker.Main for multichecker.Main and pass all Analyzers.
//
// Invocation:
//
//	go run ./cmd/tide-lint ./...
//	make tide-lint            # convenience target wiring the same call
package main

import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/jsquirrelz/tide/tools/analyzers/crosspool"
)

func main() {
	singlechecker.Main(crosspool.Analyzer)
}
```

**Apply:** `cmd/stub-subagent/main.go` opens with the same doc-comment pattern (purpose + invocation + load-bearing decision context). The body imports `github.com/jsquirrelz/tide/pkg/dispatch` (proves D-F2's "stub reuses pkg/dispatch envelope types" claim), reads `EnvelopeIn` from `--envelope` flag, dispatches by `in.Dev.TestMode` (D-F3 enum), writes `EnvelopeOut`, exits.

For `flag` parsing + signal handling, larger pattern is `cmd/manager/main.go:52-65`:

```go
func main() {
	var configPath string
	flag.StringVar(&configPath, "config", "/etc/tide/config.yaml", "…")
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")
	// …
}
```

But `stub-subagent` is leaf-package style (no controller-runtime) — keep it stdlib-only + `pkg/dispatch`. Imitates `cmd/tide-lint`'s minimalism.

---

### `cmd/credproxy/main.go` (new cmd)

**Analog:** `cmd/manager/main.go` (flag parsing + signal handling + serve loop)

**Pattern:** `cmd/manager/main.go:52-65, 191-195`:

```go
func main() {
	var leaderElect bool
	flag.BoolVar(&leaderElect, "leader-elect", true, "…")
	// …
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))
	setupLog := ctrl.Log.WithName("setup")
	// …

	setupLog.Info("starting manager")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		setupLog.Error(err, "problem running manager")
		os.Exit(1)
	}
}
```

**Apply:** credproxy main parses flags (`--listen-addr`, `--cert-dir`, `--upstream-url`, `--signing-secret-env`), constructs the HTTPS server via `internal/credproxy.New`, blocks on `srv.ListenAndServeTLS(...)` under `ctrl.SetupSignalHandler()` context. Reuses zap logger setup. Exits with `os.Exit(1)` on bootstrap failure — same pattern as manager's line 91-93.

---

### `internal/credproxy/proxy.go`, `token.go`, `cert.go` (new internal package)

**Analog:** `internal/pool/pool.go` (internal helper pkg shape, doc-comment driven, ctor + methods)

**Package doc-comment template** — apply `internal/pool/pool.go:1-12` style:

```go
// Package credproxy implements the sidecar HTTPS reverse-proxy with HMAC
// signed-token verification (HARN-03 / D-C1, D-C2, D-C3).
//
// Per D-C3: HMAC-SHA256 over (nonce || taskUID || expiry) with an
// installation-wide signing secret. Subagent container's env, filesystem,
// and process tree never touch the real ANTHROPIC_API_KEY.
//
// Pitfall 18 prevention (secret leakage) is enforced at this layer; the
// redaction io.Writer (internal/harness/redact) is the defense-in-depth.
package credproxy
```

**Token sign/verify pattern** — `internal/credproxy/token.go` mirrors `internal/owner/owner.go:34-48`'s small-helper shape:

```go
// SignToken returns the HMAC-SHA256 signed token per D-C3.
// Format: base64( nonce || expiry || mac ) where mac = HMAC(secret, nonce || taskUID || expiry).
//
// Returns an error if:
//   - secret is empty
//   - taskUID is empty
func SignToken(secret []byte, taskUID string, expiry time.Time) (string, error) { … }

// VerifyToken returns nil iff the token's HMAC validates and expiry has not passed.
// On failure returns a structured error naming the failed check (Pitfall 18).
func VerifyToken(secret []byte, taskUID string, token string) error { … }
```

**Test pattern** — `internal/credproxy/token_test.go` uses stdlib `testing` table-driven (mirror `pkg/dag/kahn_test.go:45-119`):

```go
type tc struct {
	name       string
	taskUID    string
	tamper     func([]byte) []byte // mutate token bytes
	wantErr    string              // substring
}
cases := []tc{
	{name: "Valid", taskUID: "uid-1", tamper: nil, wantErr: ""},
	{name: "TamperedMAC", taskUID: "uid-1", tamper: flipLastByte, wantErr: "invalid mac"},
	{name: "WrongTaskUID", taskUID: "uid-2", tamper: nil, wantErr: "task uid mismatch"},
	{name: "Expired", taskUID: "uid-1", tamper: nil, wantErr: "expired"},
}
```

---

### `internal/harness/harness.go`, `caps.go`, `output.go` (new harness package)

**Analog:** `internal/finalizer/finalizer.go` (context-bounded execution with deadline, structured error contract)

**Bounded-deadline pattern to mirror** — `internal/finalizer/finalizer.go:44-77`:

```go
func HandleDeletion(
	ctx context.Context,
	c client.Client,
	obj client.Object,
	finalizerName string,
	cleanup func(context.Context) error,
	timeout time.Duration,
) (ctrl.Result, error) {
	if !controllerutil.ContainsFinalizer(obj, finalizerName) {
		return ctrl.Result{}, nil
	}

	cleanupCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	if err := cleanup(cleanupCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			log.FromContext(ctx).Error(err, "…")
			controllerutil.RemoveFinalizer(obj, finalizerName)
			return ctrl.Result{}, c.Update(ctx, obj)
		}
		return ctrl.Result{Requeue: true}, err
	}
	// …
}
```

**Apply to caps.go:** `EnforceCaps(ctx, in.Caps, fn)` derives `capsCtx := context.WithTimeout(ctx, in.Caps.WallClockSeconds)`, runs `fn(capsCtx)`, returns structured `CapHitError{Reason: "wall-clock"|"iterations"|"input-tokens"|"output-tokens"}`. Same `errors.Is(err, context.DeadlineExceeded)` → emit cap-hit envelope shape pattern.

**Apply to output.go:** small validator helper like `internal/owner/owner.go:34-48`. Signature:

```go
// ValidateOutputPath asserts actualWrite resolves under one of declaredOutputPaths
// per HARN-05. Uses filepath.Rel + filepath.EvalSymlinks (Pitfall 7 prevention:
// symlink-out-of-scope writes).
//
// Returns an error naming the violating path on failure; nil on success.
func ValidateOutputPath(declared []string, actualWrite string) error { … }
```

---

### `internal/harness/redact/redact.go` (new — streaming redactor)

**Analog:** `pkg/dag/kahn.go` (leaf, stdlib-only, deterministic transform)

**Leaf-package style** — bare `package redact`, no Apache header (Phase 1 convention for `pkg/dag/*.go` files), stdlib `regexp` import + nothing else.

**Pitfall A (RESEARCH.md line 552) explicit warning:** regex match straddling chunk boundary in streaming redactor. Solution: tail-keep buffer of N bytes (longest regex pattern in the set). Doc-comment names the pitfall explicitly per `pkg/dag/kahn.go:19-30` style (which names DAG-04 fixture pinning + O(V+E) complexity in the doc-comment).

**Test pattern** — `internal/harness/redact/redact_test.go` mirrors `pkg/dag/kahn_test.go`:
- stdlib `testing`
- Table of `{name, input, want}` with boundary-straddle fixtures
- Each subtest re-exposed as `TestRedact_BoundaryStraddle_SkAnt` for `-run` selection

---

### `internal/budget/bucket.go` (new — rate limiter)

**Analog:** `internal/pool/pool.go` (sync primitive ctor + ops pair, package-doc explains pitfall it prevents)

**Pattern:** `internal/pool/pool.go:1-12` package doc-comment style — name FAIL-03 / Pitfall 9 prevention up front, declare the in-memory-only contract (D-D1).

```go
// Package budget implements per-secret-UID token-bucket rate limiting
// (FAIL-03 / Pitfall 9) and per-Project budget tally (D-D1, D-D2).
//
// State durability split per the two-cache rule:
//   - Bucket state: in-memory sync.Map[secretUID]*rate.Limiter (D-D1).
//     Rederived at Manager restart by listing active Jobs in the last
//     bucket-window and pre-charging — same purity rule as POOL-02
//     PreCharge in internal/pool.
//   - Budget tally: Project.Status.budget rolled up at Task completion
//     (D-D2). One Status write per Task completion, NOT per dispatch.
package budget
```

**Apply:** `Bucket` struct with `sync.Map[types.UID]*rate.Limiter`, `New(defaults Config) *Bucket`, `Acquire(ctx, secretUID) error`, `PreCharge(ctx, c client.Client) error` (mirrors `internal/pool/pool.go:72-95` `PreCharge` shape — also runs against `batchv1.JobList` with a label selector).

---

### `internal/webhook/v1alpha1/plan_webhook.go` (extend — fill body)

**Analog:** itself (Phase 1 no-op scaffold from Plan 07)

**Critical preservation rule:** the signatures `ValidateCreate`, `ValidateUpdate`, `ValidateDelete` on `PlanCustomValidator` MUST NOT change. The planner replaces only the body. Phase 1's doc-comment on `ValidateCreate` (line 62-73) already names the Phase 2 fill-in pseudocode:

```go
// Phase 2 wires the cycle-detection seam (D-B3 / REQ-PLAN-01):
//
//	tasks, err := r.listTasksForPlan(ctx, plan)
//	if err != nil { return nil, fmt.Errorf("plan rejected: failed to list tasks: %w", err) }
//	nodeIDs, edges := tasksToDAG(tasks)
//	if _, err := dag.ComputeWaves(nodeIDs, edges); err != nil {
//	    var cyclic *dag.CycleError
//	    if errors.As(err, &cyclic) {
//	        return nil, fmt.Errorf("plan %s/%s rejected: cyclic task DAG involving %v", plan.Namespace, plan.Name, cyclic.InvolvedNodes)
//	    }
//	    return nil, err
//	}
```

**Apply:** the body fill is exactly this pseudocode plus the PLAN-02 file-touch ↔ dependsOn reconciliation (D-E2) + the strict/warn-mode resolver from the new `strict_mode.go` (D-E3). Phase 2 ALSO requires the validator struct to gain a `client.Client` field (so it can list Tasks owned by the Plan) — this is the only struct shape change.

**Pattern for `PlanCustomValidator` becoming stateful** — mirror `internal/controller/task_controller.go:44-58`:

```go
type PlanCustomValidator struct {
	client.Client
	FileTouchMode string // "strict" | "warn" — Helm-supplied default
	Recorder      record.EventRecorder // for the audit Event (D-E4)
}
```

The `SetupPlanWebhookWithManager(mgr)` signature (line 40) gains a config struct or option args. Same kubebuilder marker comment on line 46 stays.

---

### `internal/webhook/v1alpha1/strict_mode.go` (new — precedence resolver)

**Analog:** `internal/owner/owner.go` (small helper, validation/precedence logic with structured errors)

**Pattern:** `internal/owner/owner.go:34-48` is the closest shape — a single exported function with explicit failure modes documented in the doc-comment.

```go
// ResolveFileTouchMode returns the active mode per D-E3 precedence:
//   1. Plan annotation `tideproject.k8s/file-touch-mode=strict|warn`
//   2. Project.Spec.planAdmission.fileTouchMode
//   3. Helm chart default (clusterDefault)
//
// Returns "warn" if no value is set anywhere (defense-in-depth — v1 chart
// ships "warn" per D-E3).
func ResolveFileTouchMode(plan *v1alpha1.Plan, project *v1alpha1.Project, clusterDefault string) string { … }
```

---

### `internal/controller/task_controller.go` (extend dispatch body)

**Analog:** itself (Phase 1 stub — line 113-115)

**Critical preservation rule:** the struct field `Dispatcher dispatch.Dispatcher` on line 54 STAYS. The Reconcile six-step pattern (lines 68-130) STAYS. The Phase 2 fill goes inside the existing seam:

```go
// internal/controller/task_controller.go:112-115 (Phase 1 stub)
// 5. Phase 1: dispatcher seam nil-guarded for Phase 2 body fill (REQ-SUB-01).
if r.Dispatcher != nil {
	// Phase 2 fills.
}
```

**Apply:** the Phase 2 body inside that `if` block:
1. Compute indegree by listing sibling Tasks in the same Plan (D-B3) — uses `r.List(ctx, &siblings, client.MatchingLabels{...})` (label index pattern).
2. If indegree==0 AND status.Phase != Succeeded: compute deterministic Job name via `podjob.JobName(task.UID, task.Status.Attempt+1)`, attempt `r.Create(ctx, &job)` (AlreadyExists is the SUB-03 idempotency contract — log + treat as success).
3. On Job termination watch event: read `out.json` from PVC, transition `task.Status.Phase` to `Succeeded`/`Failed`, increment `task.Status.Attempt`, patch Project.Status budget tally.

**RBAC marker addition pattern** — `internal/controller/task_controller.go:60-65`:

```go
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

**Apply:** Phase 2 needs additional RBAC for the dispatch logic. New markers MUST follow per-Kind discipline (AUTH-03 / Pitfall 15) — never wildcards. Likely additions:
- `// +kubebuilder:rbac:groups="",resources=persistentvolumeclaims,verbs=get;list;watch` (Task reads PVC for envelope I/O — only if needed at reconciler layer; harness does the I/O)
- `// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch` (Project.Spec.providerSecretRef lookup — but read-only)
- `// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete` (UPGRADE the existing batch/jobs verbs — Task gains create authority for dispatch)

`make verify-rbac-marker-discipline` (Makefile:359-371) catches violations at CI time. The augmented markers MUST pass the same regex.

---

### `internal/controller/wave_controller.go` (extend — Wave roll-up)

**Analog:** itself (`internal/controller/wave_controller.go`)

**Apply:** the Phase 2 fill at line 114 (`if r.Dispatcher != nil { /* Phase 2 fills */ }`) per D-B2, D-B4:
1. List owned Tasks via owner-ref label selector (Wave→Plan→Tasks).
2. Aggregate `task.Status.Phase` across members: `Wave.Status.phase = Succeeded` iff every member is Succeeded; `Failed` iff any member is Failed; else `Running`.
3. Patch `Wave.Status.taskRefs` with member names (already declared in `api/v1alpha1/wave_types.go:46-47`).
4. NO Job creation — that's TaskReconciler's sole job (D-B1).

**Watch shape:** the existing `Owns(&batchv1.Job{})` on line 144 is the right hook for Task→Wave indirect events. Phase 2 may also need to add `Watches(&v1alpha1.Task{}, handler.EnqueueRequestForOwner(...))` — same pattern as the `Owns()` but for the Task→Wave aggregation.

---

### `internal/controller/plan_controller.go` (extend — materialize Waves)

**Analog:** itself

**Apply:** at line 114 seam, on transition to Validated (set by the admission webhook), call `pkg/dag.ComputeWaves` on the Plan's Tasks, then `r.Create(ctx, &wave)` for each layer with owner-ref to the Plan (D-B1). Wave name is deterministic per Phase 1 D-B1: `tide-wave-{plan-uid}-{index}`. Per Phase 1 D-B2, only `planRef + waveIndex` go into `Wave.Spec`.

**`Plan.Status.phase` transition vocabulary** — Phase 2 adds `Validated` to the existing `Pending/Ready/Reconciling/Failed` vocabulary from `api/v1alpha1/shared_types.go:26-33`. New constant addition to `shared_types.go`:

```go
// shared_types.go (extend) — Phase 2 additions per 02-CONTEXT.md "Established Patterns"
const (
	// ConditionValidated — Plan passed admission webhook cycle + file-touch checks.
	ConditionValidated = "Validated"
	// ConditionBudgetExceeded — Project's budget cap was reached; new dispatches halted.
	ConditionBudgetExceeded = "BudgetExceeded"
	// ConditionRunning — Task/Wave actively dispatching.
	ConditionRunning = "Running"
	// ConditionSucceeded — Task/Wave terminated with exit 0 + valid envelope.
	ConditionSucceeded = "Succeeded"
)
```

---

### `internal/controller/project_controller.go` (extend — init Job + budget)

**Analog:** itself

**Apply at line 111 seam:**
1. Init-Job creation (D-G1): if `Project.Status.phase != Initialized`, compute deterministic name `tide-init-{project.UID}`, attempt `r.Create(ctx, &job)`, set status to Initialized on success.
2. Budget halt (D-D2): before allowing dispatches, check `project.Status.budget.tokensSpent` vs `project.Spec.budget.absoluteCap`; if exceeded, set `Status.phase = BudgetExceeded` + Condition `BudgetExceeded=True`.
3. Bypass-annotation watch (D-D4): on `tideproject.k8s/bypass-budget=true` annotation, clear `BudgetExceeded` condition and remove the annotation (or honor TTL via `tideproject.k8s/bypass-budget-until=<RFC3339>`).

**finalizerCleanupTimeout reuse** — line 43 declares `5 * time.Minute` in this file. Phase 2 init-Job cleanup callback (extending the no-op on line 92-96) MUST use the same constant.

---

### `internal/controller/suite_test.go` (extend — register Phase 2 bodies in envtest)

**Analog:** itself (Phase 1 Plan 07 folded webhook into single BeforeSuite)

**Critical preservation rule (from CONTEXT.md "Established Patterns" referencing Phase 1 D-H1):** Phase 2's new admission tests live IN THIS PACKAGE, not a new suite. Spinning up a second envtest cold-start violates TEST-01 budget.

**The BeforeSuite plumbing to extend** — lines 66-150:

```go
var _ = BeforeSuite(func() {
	// …
	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}
	// …

	// Register both Phase 1 no-op webhooks (Plan 07 Task 1).
	Expect(webhookv1alpha1.SetupPlanWebhookWithManager(mgr)).To(Succeed())
	Expect(webhookv1alpha1.SetupWaveWebhookWithManager(mgr)).To(Succeed())
	// …
})
```

**Apply:** Phase 2 extends this BeforeSuite to:
- Register the now-stateful `PlanCustomValidator{Client: mgr.GetClient(), FileTouchMode: "warn"}` (with client injection — the Phase 1 stub takes no args).
- Register the six reconcilers with their Phase 2 bodies (so admission tests can exercise the end-to-end "apply Plan → reconciler materializes Waves → Tasks dispatch" chain in envtest where Jobs are admitted but no kubelet runs them).
- Add a TaskReconciler `Dispatcher` field injection — for envtest, use a fake/in-memory dispatcher OR rely on the existing `if r.Dispatcher != nil` guard to leave the dispatch path unwired in admission-only tests.

---

### `api/v1alpha1/project_types.go` (extend — Phase 2 schema additions)

**Analog:** itself (Phase 1 ProjectSpec shape from lines 66-84)

**Nested-struct convention** — `api/v1alpha1/project_types.go:24-32, 36-45, 53-64`:

```go
// SecretRefs declares the K8s Secret names that carry credentials.
type SecretRefs struct {
	// AnthropicAPIKey is the Secret name carrying the LLM API key.
	// +optional
	AnthropicAPIKey string `json:"anthropicAPIKey,omitempty"`
	// GitCredentials is the Secret name carrying git push credentials (PAT or SSH key).
	// +optional
	GitCredentials string `json:"gitCredentials,omitempty"`
}
```

**Apply:** Phase 2 adds five nested structs to `ProjectSpec` per RESEARCH.md "Project.Spec additions":
- `ProviderSecretRef string` (rename of existing AnthropicAPIKey? — planner reconciles with existing `SecretRefs` field)
- `Providers []ProviderConfig` (each: `Name string`, `RequestsPerMinute *int32`, `TokensPerMinute *int32`)
- `Budget BudgetConfig` (`AbsoluteCap *int64`, `RollingWindowCap *int64`, `WindowDurationSeconds *int32`)
- `PlanAdmission PlanAdmissionConfig` (`FileTouchMode string` enum `strict|warn`)
- `MaxAttemptsPerTask *int32` (default 3 via CEL)

**CEL marker convention** — `api/v1alpha1/project_types.go:67`:

```go
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@')",message="targetRepo must be a valid http(s) or SSH git URL"
type ProjectSpec struct { … }
```

**Apply:** Each Phase 2 nested struct's enum/range constraints land as `+kubebuilder:validation:Enum=...` or `+kubebuilder:validation:Minimum=...` markers. CEL `XValidation` for cross-field rules (e.g., "if rollingWindowCap set, windowDurationSeconds must be set").

**Aggregates-guard discipline** — `Makefile:312-320`:

```makefile
verify-no-aggregates: ## Assert api/v1alpha1 declares no aggregate schedule fields (PERSIST-02 / Pitfall 4).
	@MATCHES=$$(grep -nE 'Schedule|Waves *\[\]|IndegreeMap|CachedDag|DerivedDag' api/v1alpha1/*_types.go || true); \
	if [ -n "$$MATCHES" ]; then \
		echo "PERSIST-02 violation: aggregate schedule fields detected:"; …
```

**Apply:** Phase 2's `Project.Status.budget` field is **NOT** an aggregate schedule field (PERSIST-02 / Pitfall 4 forbids `Schedule`, `Waves []`, `IndegreeMap`, `CachedDag`, `DerivedDag` — not `budget`). The new field is fine. But the planner MUST extend `api/v1alpha1/aggregates_guard_test.go` (lines 31, 65-85) to assert the regex still flags violations AND silently passes against the Phase 2 schema additions.

---

### `api/v1alpha1/task_types.go` (extend — add Dev.TestMode)

**Analog:** itself (TaskSpec lines 24-40)

**Pattern:** dev-only sub-struct mirrors `Gates` / `ModelSelection` in `shared_types.go:53-64`:

```go
// TaskDev holds dev/test-only fields. Real Claude-backed image in Phase 3
// ignores this struct entirely (D-F1). Tests author Task fixtures with
// `spec.dev.testMode=fail-exit-1` and TaskReconciler copies into
// EnvelopeIn.Dev.TestMode at dispatch.
type TaskDev struct {
	// TestMode selects stub-subagent behavior per D-F1, D-F3.
	// +kubebuilder:validation:Enum=success;fail-exit-1;hang;exceed-output-paths
	// +optional
	TestMode string `json:"testMode,omitempty"`
}

// TaskSpec carries the executor envelope per D-F1, D-F2.
type TaskSpec struct {
	// … existing fields …

	// Dev holds dev/test-only fields (D-F1). Real Claude-backed image ignores this.
	// +optional
	Dev TaskDev `json:"dev,omitempty"`
}
```

The `// +kubebuilder:validation:Enum=...` marker mirrors `shared_types.go:49-50` (`GatePolicy`).

---

### `tools/analyzers/providerfirewall/analyzer.go` (new analyzer)

**Analog:** `tools/analyzers/dagimports/analyzer.go` (import-firewall analyzer with path-prefix denylist — EXACT match)

**Pattern to mirror precisely** — `tools/analyzers/dagimports/analyzer.go:1-52`:

```go
// Package dagimports implements an analyzer that rejects forbidden imports
// (k8s.io/*, sigs.k8s.io/*, github.com/anthropics/*) from any file whose
// package import path contains "/pkg/dag/" or ends with "/pkg/dag".
package dagimports

import (
	"strings"

	"golang.org/x/tools/go/analysis"
)

var forbiddenPrefixes = []string{
	"k8s.io/",
	"sigs.k8s.io/",
	"github.com/anthropics/",
}

var Analyzer = &analysis.Analyzer{
	Name: "dagimports",
	Doc:  "rejects k8s.io/*, sigs.k8s.io/*, github.com/anthropics/* imports inside pkg/dag (DAG-05 fixture mirror)",
	Run:  run,
}

func run(pass *analysis.Pass) (interface{}, error) {
	path := pass.Pkg.Path()
	if !strings.Contains(path, "/pkg/dag") && !strings.HasSuffix(path, "pkg/dag") {
		return nil, nil
	}
	for _, f := range pass.Files {
		for _, imp := range f.Imports {
			importPath := strings.Trim(imp.Path.Value, "\"")
			for _, prefix := range forbiddenPrefixes {
				if strings.HasPrefix(importPath, prefix) {
					pass.Reportf(imp.Pos(),
						"DAG-05 violation: forbidden import %q in pkg/dag (forbidden prefix %q)",
						importPath, prefix)
				}
			}
		}
	}
	return nil, nil
}
```

**Apply with SUB-05 mods:**
- `forbiddenPrefixes = []string{"github.com/anthropics/", "github.com/openai/", "github.com/sashabaranov/go-openai", "github.com/google/generative-ai-go"}` (LLM SDK denylist).
- Scope: package paths matching `pkg/controller/...`, `pkg/dispatch/...`, `pkg/dag/...` (the SUB-05 boundary per CONTEXT.md). NOT `internal/subagent/anthropic/` — that's where the real Anthropic-bound impl is allowed to live.
- Diagnostic message: `"SUB-05 violation: forbidden LLM SDK import %q in %s (Pitfall 14: vendor lock-in creep)"`.

---

### `tools/analyzers/providerfirewall/analyzer_test.go` (new test)

**Analog:** `tools/analyzers/dagimports/analyzer_test.go` (EXACT)

**Pattern** — lines 1-22:

```go
package dagimports

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

func TestDagImports(t *testing.T) {
	testdata := analysistest.TestData()
	analysistest.Run(t, testdata, Analyzer, "valid/pkg/dag")
	analysistest.Run(t, testdata, Analyzer, "violation/pkg/dag")
}
```

**Apply:** identical shape. Fixtures under `tools/analyzers/providerfirewall/testdata/src/{valid,violation}/pkg/dispatch/` (synthetic pkg/dispatch — stdlib imports valid; an `_ "github.com/anthropics/anthropic-sdk-go"` import flagged with `// want \`SUB-05 violation: forbidden LLM SDK import "github.com/anthropics/anthropic-sdk-go"\`` directive).

**Critical preservation:** the Makefile `manifests`/`generate` targets (lines 47-65) explicitly scope to `./api/...`, `./internal/controller/...`, `./internal/webhook/...` to AVOID descending into `tools/analyzers/*/testdata/`. Phase 2's new providerfirewall testdata MUST sit under the same `testdata/src/...` GOPATH-style layout so the same scoping rule continues to exclude it from kubebuilder codegen.

---

### `cmd/tide-lint/main.go` (single-line change)

**Analog:** itself (Phase 1 commitment in the doc-comment, line 5: "Phase 2+ may register additional analyzers; when that happens, swap singlechecker.Main for multichecker.Main and pass all Analyzers.")

**Phase 1 body** — `cmd/tide-lint/main.go:22-30`:

```go
import (
	"golang.org/x/tools/go/analysis/singlechecker"

	"github.com/jsquirrelz/tide/tools/analyzers/crosspool"
)

func main() {
	singlechecker.Main(crosspool.Analyzer)
}
```

**Phase 2 single-line flip** — Phase 2 changes exactly:

```go
import (
	"golang.org/x/tools/go/analysis/multichecker"

	"github.com/jsquirrelz/tide/tools/analyzers/crosspool"
	"github.com/jsquirrelz/tide/tools/analyzers/providerfirewall"
)

func main() {
	multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer)
}
```

The doc-comment at the top of the file (`Phase 2+ may register additional analyzers; when that happens, swap singlechecker.Main for multichecker.Main and pass all Analyzers.`) is now realized — planner updates the doc-comment past-tense to "Phase 2 added providerfirewall.Analyzer; this is the multichecker form."

**Note:** `tools/analyzers/dagimports/` exists but is invoked via `make verify-dag-imports` (transitive coverage via `go list -deps`), NOT via `cmd/tide-lint`. Phase 2's providerfirewall analyzer goes through `cmd/tide-lint` per CONTEXT.md "Phase 1 preparation" commitment.

---

### `test/integration/envtest/suite_test.go` (new)

**Analog:** `internal/controller/suite_test.go` (Ginkgo BeforeSuite + envtest + webhook plumbing — EXACT)

**Pattern to mirror precisely** — lines 53-150 of `internal/controller/suite_test.go`:

```go
var (
	ctx       context.Context
	cancel    context.CancelFunc
	testEnv   *envtest.Environment
	cfg       *rest.Config
	k8sClient client.Client
)

func TestControllers(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controller Suite")
}

var _ = BeforeSuite(func() {
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))
	ctx, cancel = context.WithCancel(context.TODO())

	err := tideprojectv1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())
	utilruntime.Must(admissionv1.AddToScheme(scheme.Scheme))

	By("bootstrapping test environment")
	testEnv = &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
		WebhookInstallOptions: envtest.WebhookInstallOptions{
			Paths: []string{filepath.Join("..", "..", "config", "webhook")},
		},
	}
	// …
})
```

**Apply:** identical shape; `CRDDirectoryPaths` references `filepath.Join("..", "..", "..", "config", "crd", "bases")` (one more level up from `test/integration/envtest/`). Ginkgo `--label-filter=envtest` per D-H1.

---

### `test/integration/envtest/{admission,indegree,budget}_test.go` (new tests)

**Analog:** `internal/controller/plan_webhook_test.go` (Ginkgo Describe + envtest k8sClient + Eventually)

**Pattern** — `internal/controller/plan_webhook_test.go:37-89`:

```go
var _ = Describe("PlanCustomValidator (Phase 1 no-op)", func() {
	const namespace = "default"

	AfterEach(func() {
		plans := &tideprojectv1alpha1.PlanList{}
		_ = k8sClient.List(ctx, plans, client.InNamespace(namespace))
		for i := range plans.Items {
			_ = k8sClient.Delete(ctx, &plans.Items[i])
		}
	})

	It("allows ValidateCreate (Phase 1 no-op)", func() {
		plan := &tideprojectv1alpha1.Plan{ … }
		Expect(k8sClient.Create(ctx, plan)).To(Succeed(), "…")
	})

	It("allows ValidateUpdate (Phase 1 no-op)", func() {
		// …
		Eventually(func() error {
			fresh := &tideprojectv1alpha1.Plan{}
			if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(plan), fresh); err != nil {
				return err
			}
			fresh.Spec.PhaseRef = "phase-b"
			return k8sClient.Update(ctx, fresh)
		}, "5s", "100ms").Should(Succeed(), "…")
	})
})
```

**Apply:** Phase 2 admission tests follow this Describe/It/Eventually shape. Tests assert structured rejection (`apierrors.IsInvalid(err)` for CEL, `apierrors.IsForbidden(err)` for admission cycle rejection) — see line 121-122 for the apierrors check pattern.

**Ginkgo label discipline** — Phase 2 tests use `Label("envtest")` per D-H1:

```go
var _ = Describe("Plan cycle detection", Label("envtest"), func() { … })
```

Plus `--label-filter=envtest` / `--label-filter=kind` per Makefile `test-int` / `test-int-fast` targets.

---

### `Makefile` (extend — add Phase 2 targets)

**Analog:** itself (Phase 1 conventions established)

**Naming convention** — Phase 1 Makefile targets:

```makefile
test:           # runs prerequisites + test
test-only:      # runs test only (prereqs done by CI)
test-leader-election:  # slow targeted suite
verify-dag-imports:    # CI gate, regex-based
verify-no-aggregates:  # CI gate, regex-based
verify-no-sqlite-dep:  # CI gate, regex-based
verify-no-rbac-wildcards:        # CI gate
verify-rbac-marker-discipline:   # CI gate
verify-no-blocking:    # CI gate
tide-lint:      # analyzer run
helm helm-controller helm-crds:  # chart generation
```

**Apply Phase 2 targets:**

```makefile
##@ Integration tests (TEST-02 — Phase 2)

.PHONY: test-int test-int-fast test-int-kind-prep

test-int: manifests generate fmt vet setup-envtest test-int-kind-prep ## Run both integration tiers (Layer A envtest + Layer B kind, <5min budget).
	KUBEBUILDER_ASSETS="$(shell ...)" go test ./test/integration/... -timeout 5m -v -ginkgo.v

test-int-fast: manifests generate fmt vet setup-envtest ## Run envtest layer only (~90s; Layer A).
	KUBEBUILDER_ASSETS="$(shell ...)" go test ./test/integration/envtest/... -timeout 2m -v -ginkgo.v --label-filter=envtest

test-int-kind-prep: ## Build + load stub-subagent + credproxy images into kind.
	# Build images, kind load docker-image …

##@ Import firewall (SUB-05 — Phase 2 / Pitfall 14)

.PHONY: verify-import-firewall

verify-import-firewall: ## Run providerfirewall analyzer via tide-lint multichecker (SUB-05).
	go run ./cmd/tide-lint ./...   # multichecker invokes BOTH crosspool + providerfirewall
```

**CI integration:** `.github/workflows/ci.yaml` (lines 56-66 today wire `make tide-lint`) automatically picks up the providerfirewall analyzer because `cmd/tide-lint` is now a multichecker. New job for `make test-int` runs in matrix.

---

### Helm chart additions

**`charts/tide/values.yaml` (extend)** — analog is `hack/helm/tide-values.yaml` (lines 1-97, the canonical source-of-truth)

**Pattern:** add at the top-level (NOT inside `controllerManager.*`), mirroring `plannerConcurrency: 16` on line 77 — these are TIDE-specific tunables that the helmify augment script copies over the helmify-emitted block:

```yaml
# Phase 2 additions (RESEARCH.md "Helm values additions"):
planAdmission:
  fileTouchMode: warn   # strict | warn (D-E3 — default warn)
rateLimits:
  defaults:
    requestsPerMinute: 50
    tokensPerMinute: 40000
images:
  stubSubagent:
    repository: ghcr.io/jsquirrelz/tide-stub-subagent
    tag: v0.1.0-dev
  credProxy:
    repository: ghcr.io/jsquirrelz/tide-credproxy
    tag: v0.1.0-dev
signingKey:
  secretName: tide-signing-key  # D-C3 — auto-generated on first install
```

**`charts/tide/templates/signing-secret.yaml` (NEW)** — analog is `charts/tide/templates/serviceaccount.yaml` (existing hand-authored-style template).

**Pattern:** the template uses Helm's `lookup` to skip regeneration on upgrade (the Phase 2 D-C3 commitment is "auto-generated on first install if not provided"):

```yaml
{{- $secret := lookup "v1" "Secret" .Release.Namespace .Values.signingKey.secretName }}
{{- if not $secret }}
apiVersion: v1
kind: Secret
metadata:
  name: {{ .Values.signingKey.secretName }}
  namespace: {{ .Release.Namespace }}
  annotations:
    helm.sh/resource-policy: keep
  labels:
    {{- include "tide.labels" . | nindent 4 }}
type: Opaque
data:
  signing-key: {{ randAlphaNum 64 | b64enc | quote }}
{{- end }}
```

The `helm.sh/resource-policy: keep` annotation + the `lookup` guard prevent regenerating the secret on `helm upgrade` (would invalidate every in-flight signed token).

**`hack/helm/augment-tide-chart.sh` (extend)** — Phase 1 script at lines 1-103 already handles the augment-after-helmify pattern. Phase 2 adds:
1. Extension to `tide-values.yaml` copy block (line 35) — already covered: `cp "${HACK_DIR}/tide-values.yaml" "${CHART_DIR}/values.yaml"` propagates the new keys.
2. New template copy: `cp "${HACK_DIR}/signing-secret.yaml" "${CHART_DIR}/templates/signing-secret.yaml"` (NEW step after line 67 ConfigMap heredoc).

The augment-script idempotency contract (line 22: "Idempotent: running this script multiple times produces the same output.") is preserved — Phase 2 additions are also idempotent.

**CI reproducibility gate** — `.github/workflows/ci.yaml:140-151`:

```yaml
- name: Verify chart tree is reproducible
  run: |
    if ! git diff --quiet charts/; then
      echo "charts/ tree drifted from a fresh `make helm` regeneration:"
      git diff --stat charts/
      exit 1
    fi
```

**Apply:** Phase 2 chart additions MUST flow through `make helm` → augment script → committed `charts/tide/` tree. Hand-editing `charts/tide/` directly will fail the CI gate.

---

## Shared Patterns

### Apache header convention (split by package type)

**Source files needing the Apache 2.0 header** (every line is a copyright block matching `internal/controller/task_controller.go:1-15`):
- `api/v1alpha1/*.go` (CRD type files)
- `cmd/manager/main.go`, `cmd/credproxy/main.go` (controller-side cmds)
- `internal/controller/*_controller.go` (reconcilers)
- `internal/webhook/v1alpha1/*.go` (webhooks)
- `internal/controller/*_test.go`, `internal/controller/suite_test.go` (ginkgo tests)
- `internal/controller/rbac_guard_test.go`, `api/v1alpha1/aggregates_guard_test.go` (regex-gate tests)

**Source files OMITTING the header** (Phase 1 lean leaf-package convention):
- `pkg/dag/*.go` (leaf package — see `pkg/dag/kahn.go:1` — bare `package dag`)
- `internal/owner/owner.go`, `internal/finalizer/finalizer.go`, `internal/pool/pool.go`, `internal/config/config.go` (internal helper packages)
- `tools/analyzers/*/*.go` (analyzer packages)
- `cmd/tide-lint/main.go` (lint cmd — package-doc-comment driven)
- `cmd/stub-subagent/main.go`, `internal/dispatch/*`, `internal/credproxy/*`, `internal/harness/*`, `internal/budget/*`, `pkg/dispatch/*` (Phase 2 — match the Phase 1 convention)

**Rule of thumb:** if the file is created by `kubebuilder` scaffolding (api/, controller/, webhook/, cmd/manager/) it gets the header. Hand-authored helper packages omit it.

---

### Vocabulary discipline (tide- prefix + condition vocabulary)

**Names follow the tide metaphor (`CLAUDE.md` vocabulary section + 02-CONTEXT.md "Established Patterns"):**
- `tide-credproxy` (sidecar) — NOT `auth-broker`, NOT `proxy-sidecar`
- `tide-init` (init Job) — NOT `bootstrap-job`, NOT `setup-job`
- `tide-stub-subagent` (image) — accepted convention per D-F2
- `tide-task-{uid}-{n}` (Task Job names — D-B5)
- `tide-wave-{plan-uid}-{n}` (Wave names — Phase 1 D-B1)
- `tide-signing-key` (signing secret — D-C3)

**Condition vocabulary** — from `api/v1alpha1/shared_types.go:26-33`:

Phase 1 constants (preserve):
```go
const (
	ConditionPending     = "Pending"
	ConditionReady       = "Ready"
	ConditionReconciling = "Reconciling"
	ConditionFailed      = "Failed"

	ReasonInitialized            = "Initialized"
	ReasonAwaitingDispatch       = "AwaitingDispatch"
	ReasonFinalizerTimedOut      = "FinalizerTimedOut"
	ReasonSubagentDispatchFailed = "SubagentDispatchFailed"
)
```

Phase 2 additions (extend `shared_types.go`):
```go
const (
	ConditionValidated      = "Validated"        // Plan
	ConditionBudgetExceeded = "BudgetExceeded"   // Project
	ConditionRunning        = "Running"          // Task, Wave
	ConditionSucceeded      = "Succeeded"        // Task, Wave

	ReasonCycleDetected    = "CycleDetected"
	ReasonFileTouchMismatch = "FileTouchMismatch"
	ReasonCapHit            = "CapHit"
	ReasonRateLimitHit      = "RateLimitHit"
	ReasonBypassApplied     = "BypassApplied"
)
```

---

### API group + finalizer + label/annotation key convention

**From Phase 1 D-A3 (preserve exactly):**
- API group: `tideproject.k8s` (NOT `tide.io`, NOT `tide.local`, NOT `example.com`)
- Finalizer keys: `tideproject.k8s/<kind>-cleanup` — `task_controller.go:40`, `wave_controller.go:40`, `plan_controller.go:40`, `project_controller.go:41`. Phase 2 may add (with same shape): `tideproject.k8s/job-cleanup` if Task gains a Job-specific finalizer (the existing task-cleanup finalizer already covers cascade).
- Label keys: `tideproject.k8s/role=planner`, `tideproject.k8s/role=executor` (already used in `cmd/manager/main.go:113,116` for `PreCharge` label selector); `tideproject.k8s/task-uid=<uid>` (for sibling-Task list queries — D-B3); `tideproject.k8s/plan-uid=<uid>` (for Wave→Plan index)
- Annotation keys: `tideproject.k8s/bypass-budget=true` + `tideproject.k8s/bypass-budget-until=<RFC3339>` (D-D4); `tideproject.k8s/file-touch-mode=strict|warn` (D-E3); `tideproject.k8s/requests-per-minute=<int>`, `tideproject.k8s/tokens-per-minute=<int>` (D-D3 per-Secret rate-limit override)

---

### RBAC marker discipline (per-Kind, no wildcards)

**Pattern** — `internal/controller/task_controller.go:60-65`:

```go
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=tideproject.k8s,resources=tasks/finalizers,verbs=update
// +kubebuilder:rbac:groups=tideproject.k8s,resources=plans,verbs=get;list;watch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch
```

**Apply:** every Phase 2 reconciler extension that needs new permissions adds an explicit per-Kind marker. NEVER `verbs=*`, NEVER `resources=*`. The CI gate at `Makefile:359-371` (`verify-rbac-marker-discipline`) catches violations.

Phase 2 new RBAC additions (per RESEARCH.md "New RBAC markers" section, scoped per-Kind):
- ProjectReconciler: `groups="",resources=secrets,verbs=get;list;watch` (read provider Secret for budget tally context; READ-ONLY)
- TaskReconciler: `groups="",resources=persistentvolumeclaims,verbs=get;list;watch` (PVC bind check)
- WaveReconciler: `groups=tideproject.k8s,resources=tasks,verbs=get;list;watch` (member roll-up)

---

### No-blocking-I/O contract in Reconcile bodies

**Phase 1 Pitfall 1 gate** — `Makefile:336-344`:

```makefile
verify-no-blocking: ## Assert no time.Sleep or <-time.After in reconciler bodies (Pitfall 1).
	@MATCHES=$$(grep -nE 'time\.Sleep|<-time\.After' internal/controller/*_controller.go || true); \
	if [ -n "$$MATCHES" ]; then …
```

**Apply:** Phase 2 reconciler extensions MUST NOT add `time.Sleep` or `<-time.After`. Backoff timing for 429 absorption (D-D3 / FAIL-03 — RESEARCH.md "Pattern 1 reservation-based rate limiting with RequeueAfter") goes via `ctrl.Result{RequeueAfter: backoff}` — that's a return value, NOT a blocking sleep.

---

### POOL-03 rule (never select on both pools)

**Phase 1 gate** — `tools/analyzers/crosspool/analyzer.go:47-76`:

```go
// Detects: select { case <-plannerPool.sem: …; case <-executorPool.sem: …; }
// Diagnostic: "cross-pool wait: select waits on both planner and executor pools"
```

**Apply:** Phase 2's `internal/dispatch/podjob/backend.go` Run method may need to wait on the executor pool. NEVER write a select that adds a planner-pool case alongside. The analyzer fires before merge. If a Phase 2 reconciler appears to need both pools, that's a design smell — surface to the planner.

---

### Owner-ref cascade convention

**Pattern** — `internal/owner/owner.go:34-48`:

```go
func EnsureOwnerRef(child, parent metav1.Object, scheme *runtime.Scheme) error {
	// … same-namespace enforcement …
	return controllerutil.SetControllerReference(parent, child, scheme,
		controllerutil.WithBlockOwnerDeletion(true))
}
```

**Apply:** Every Phase 2 child resource (Job created by TaskReconciler, init-Job created by ProjectReconciler, Wave created by PlanReconciler) calls `owner.EnsureOwnerRef(child, parent, r.Scheme)` — NOT raw `controllerutil.SetControllerReference`. The cross-namespace rejection clause (`Pitfall 23`) is uniform across the codebase.

---

### Internal helper package usage (always prefer over inline)

When Phase 2 reconciler bodies need standard operations, route through Phase 1 helpers:

| Operation | Helper | File:line |
|-----------|--------|-----------|
| Set controller owner ref | `owner.EnsureOwnerRef(child, parent, scheme)` | `internal/owner/owner.go:34` |
| Bounded-deadline cleanup on delete | `finalizer.HandleDeletion(ctx, c, obj, fName, cleanupFn, timeout)` | `internal/finalizer/finalizer.go:44` |
| Acquire pool slot | `executorPool.Acquire(ctx)` / `Release()` | `internal/pool/pool.go:45,60` |
| Load runtime config | `config.Load(path)` | `internal/config/config.go:80` |
| Compute wave layers | `dag.ComputeWaves(nodes, edges)` | `pkg/dag/kahn.go:30` |

Phase 2's `internal/dispatch/podjob/backend.go.Run` MUST use:
- `owner.EnsureOwnerRef` when stamping owner-ref on the Job
- `executorPool.Acquire`/`Release` to enforce the executor parallelism budget (POOL-01)

---

## Patterns to Mirror (cross-cutting Phase 1 conventions Phase 2 must preserve)

1. **Six-step Standard-depth Reconcile pattern** (fetch → finalizer-deletion → finalizer-ensure → owner-ref → dispatcher-seam → status-update) — preserved in every Phase 2 reconciler extension. Body fills go INSIDE the `if r.Dispatcher != nil` seam on line ~113-115 of each controller.
2. **Leaf-package style for `pkg/dispatch`** — bare `package dispatch`, stdlib + JSON only, no Apache header, doc-comment names the public-API stability contract + forbidden-import rules. Same shape as `pkg/dag`.
3. **Internal-helper-package style for `internal/{dispatch,credproxy,harness,budget}`** — package doc-comment names the requirement ID (REQ-SUB-XX / HARN-XX / FAIL-XX) and the pitfall it prevents up front; structs are constructed via `New(...)` factory; methods receive `context.Context` as first arg; errors wrap with `fmt.Errorf("<pkg>: <action> %q: %w", arg, err)`.
4. **Ginkgo for controller-runtime tests, stdlib `testing` for leaf packages** — `internal/controller/*_test.go`, `internal/webhook/v1alpha1/*_test.go`, `test/integration/{envtest,kind}/` use Ginkgo. `pkg/dispatch/`, `internal/budget/`, `internal/credproxy/`, `internal/harness/redact/` use stdlib `testing` table-driven with shared `assertX` helpers (mirror `pkg/dag/kahn_test.go:121-168`).
5. **Single envtest BeforeSuite for the whole controller package** — Phase 2 admission tests extend `internal/controller/suite_test.go`, do NOT create a parallel suite (TEST-01 budget protection). Phase 2's new `test/integration/envtest/suite_test.go` is a SEPARATE binary tier exercising end-to-end reconciler+webhook chains.
6. **POOL-03 boundary preservation** — `internal/dispatch/podjob` calls `executorPool.Acquire/Release` only. Never adds a planner-pool case alongside. The crosspool analyzer fires at CI time.
7. **SUB-05 import-firewall additivity** — `cmd/tide-lint/main.go` flips from `singlechecker` to `multichecker`; `tools/analyzers/providerfirewall/` mirrors `tools/analyzers/dagimports/` structure exactly (analyzer.go + analyzer_test.go + testdata/src/{valid,violation}/...).
8. **Vocabulary discipline (tide- prefix, water metaphor)** — `tide-credproxy`, `tide-init`, `tide-stub-subagent`, `tide-task-{uid}-{n}`, `tide-wave-{plan-uid}-{n}`, `tide-signing-key`. Never `auth-broker`, `bootstrap`, `subagent-stub-proxy`.
9. **API group `tideproject.k8s` everywhere (D-A3)** — never `tide.io`, never placeholders. Finalizers + label/annotation keys all use this prefix.
10. **Conditions vocabulary** — Phase 2 extends `shared_types.go` with `Validated`, `BudgetExceeded`, `Running`, `Succeeded` while preserving `Pending`, `Ready`, `Reconciling`, `Failed`. All reconcilers use `meta.SetStatusCondition` (lines 118-124 of every Phase 1 controller).
11. **No `time.Sleep` / `<-time.After` in `Reconcile()`** — 429 backoff goes via `ctrl.Result{RequeueAfter: backoff}`. `verify-no-blocking` (Makefile:336-344) gates this.
12. **Per-Kind RBAC markers, no wildcards** — `verify-rbac-marker-discipline` (Makefile:359-371) gates this. Phase 2 reconciler RBAC additions are scoped per resource group.
13. **Aggregates guard discipline (PERSIST-02 / Pitfall 4)** — `Project.Status.budget` is a tally object, NOT a `Schedule` / `Waves []` / `IndegreeMap` / `CachedDag` / `DerivedDag`. The regex denylist in `verify-no-aggregates` (Makefile:312-320) stays clean. Phase 2 does NOT add a `Plan.Status.Schedule` even though waves are now materialized — Waves are separate Kind objects (D-B1, D-B2).
14. **DAG-05 forbidden imports** — `pkg/dispatch` joins `pkg/dag` under the same forbidden-import rule (no k8s.io/, sigs.k8s.io/, github.com/anthropics/). `Makefile:292-301`'s `verify-dag-imports` extends naturally to cover `pkg/dispatch` (planner adds a parallel `verify-dispatch-imports` target OR extends the existing target).
15. **α…θ worked-example thread-through** — Phase 2 Layer B kind tests use a 3-Task subset of α…θ (e.g., α, β, ε with edges α→β, β→ε) as the FAIL-01 sibling-continues fixture per 02-CONTEXT.md "Reusable Assets". Config fixtures live under `test/integration/kind/testdata/` mirroring `config/samples/` shape.
16. **Helmify-driven chart pair + augment-script idempotency** — Phase 2 chart additions flow through `make helm` → `hack/helm/augment-tide-chart.sh` → committed `charts/tide/` tree. The CI reproducibility gate (`.github/workflows/ci.yaml:140-151`) requires `git diff --quiet charts/` after a fresh `make helm` regen.
17. **Makefile target naming convention** — `test-*` for test runners, `verify-*` for CI gates, `helm-*` for chart generation. Phase 2 adds `test-int`, `test-int-fast`, `verify-import-firewall` following this pattern.

---

## No Analog Found

Every Phase 2 file has a Phase 1 analog or self-extends an existing file. The two partial-analog cases:

| File | Reason | Mitigation |
|------|--------|-----------|
| `images/{stub-subagent,credproxy}/Dockerfile` | No hand-authored multi-stage Dockerfile in Phase 1 (kubebuilder scaffolded the root `Dockerfile`) | Mirror kubebuilder's root `Dockerfile` shape; planner consults `STACK.md` for the Go 1.26 base image |
| `test/integration/kind/cluster.yaml` | No kind config exists in Phase 1 (Phase 1 ran envtest only per 01-CONTEXT D-H1) | RESEARCH.md prescribes SHA-pinned `kindest/node` image per STACK.md; planner copies the shape from kind's upstream example + pins by `@sha256` |

---

## Metadata

**Analog search scope:** `api/v1alpha1/`, `cmd/`, `internal/{controller,webhook,dispatch,owner,finalizer,pool,config}/`, `pkg/dag/`, `tools/analyzers/`, `hack/helm/`, `charts/`, `Makefile`, `.github/workflows/`
**Files scanned:** 47 Go files, 1 Makefile, 5 Helm template files, 1 augment-script, 2 CI workflow files, 1 hack/helm values file
**Pattern extraction date:** 2026-05-12

---

*Phase: 02-dispatch-plan-validation-innermost-reconcilers-harness*
*Pattern map authored: 2026-05-12*
