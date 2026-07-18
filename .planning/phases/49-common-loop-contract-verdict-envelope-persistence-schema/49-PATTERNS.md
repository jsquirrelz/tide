# Phase 49: Common Loop Contract + Verdict/Envelope/Persistence Schema - Pattern Map

**Mapped:** 2026-07-18
**Files analyzed:** 8 (5 new, 3 modified) across Go (`api/v1alpha3`, `pkg/dispatch`, `internal/controller`, `cmd/tide-push`) and Python (`cmd/tide-langgraph-verifier/verifier`)
**Analogs found:** 8 / 8 — every file has a direct, verified in-repo analog. This is a schema/contract phase; every new type is a same-package sibling of an existing type, not a cross-domain borrow.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `api/v1alpha3/loop_types.go` (NEW) | model (CRD-embeddable API type) | CRUD (status-summary projection) | `api/v1alpha3/task_types.go` (`Caps`, `TaskStatus`) + `api/v1alpha3/project_types.go` (`Gates`, `BudgetConfig`) | exact — same package, same struct-tag/kubebuilder-marker idiom |
| `api/v1alpha3/loop_types_test.go` (NEW) | test | transform (round-trip + structural) | `pkg/dispatch/envelope_test.go` (`TestTerminationStub_NoForbiddenFields`) + `api/v1alpha3/schema_test.go` (reflect-based structural assertion) | role-match |
| `pkg/dispatch/verdict.go` (NEW) | model + utility (wire-format type + fail-closed classifier) | transform (JSON in → typed verdict out) | `pkg/dispatch/envelope.go` (`EnvelopeOut`, `TerminationStub`) + `pkg/dispatch/errors.go` (`UnknownAPIVersionError`/`UnknownKindError`) | exact — explicitly designed (D-01) to sit alongside `EnvelopeOut` in the same package |
| `pkg/dispatch/verdict_test.go` (NEW) | test | transform (golden-fixture round trip + fail-closed regression) | `pkg/dispatch/envelope_test.go` (`assertRoundTripOut` :118, `TestNewTerminationStub_StaysSmall` :674) | exact |
| `pkg/dispatch/envelope.go` (MODIFY) | model (wire-format struct) | request-response (envelope seam) | itself — extend existing `EnvelopeIn`/`TerminationStub`/`NewTerminationStub` in place | exact (same-file extension) |
| `cmd/tide-langgraph-verifier/verifier/verdict.py` (NEW) | model + utility (Pydantic mirror + classifier) | transform | `cmd/tide-langgraph-verifier/verifier/envelope.py` (module-level docstring convention, `EnvelopeError`, `write_termination_stub`) | exact — explicitly the hand-authored-pair discipline this file already establishes |
| `cmd/tide-langgraph-verifier/verifier/envelope.py` (MODIFY) | model | request-response | itself — extend existing `EnvelopeIn` dataclass + `write_termination_stub` | exact |
| `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` + `conftest.py` factory (NEW/MODIFY) | test | transform | `verifier/tests/test_envelope.py` (`@pytest.mark.parametrize`, `pytest.raises`) + `verifier/tests/conftest.py` (`envelope_in_dict` factory :61-72) | exact |
| `cmd/tide-push/main.go` `stageEnvelopeArtifacts` (MODIFY) | utility (artifact staging) | file-I/O / batch | itself — generalize the existing `*.md` glob hard-fail (:1169-1183) | exact (same-function extension) |
| `cmd/tide-push/main_test.go` (MODIFY — new case) | test | file-I/O | `TestStageEnvelopesEmptyDirFailsLoud` (:1377), `TestStageEnvelopesHappyPath` (:1240) | exact |
| `internal/controller/artifact_push.go` `collectStageEnvelopes` (NOT modified this phase — plumbing only) | service | CRUD (K8s List) | n/a — Phase 51 adds the Task entry; this phase touches only the `tide-push` consumer side | n/a — correctly out of scope, see below |

## Pattern Assignments

### `api/v1alpha3/loop_types.go` (model, CRUD/status-summary)

**Analogs:** `api/v1alpha3/task_types.go:23-30` (two-homes decoupling doc-comment), `api/v1alpha3/project_types.go:34-51,95-115` (enum + Ref + BudgetConfig field-type conventions), `api/v1alpha3/shared_types.go:382-396` (condition/reason vocabulary idiom), `api/v1alpha3/groupversion_info.go:17-20` (package-level deepcopy marker).

**Package-level deepcopy marker — no per-type marker needed** (`api/v1alpha3/groupversion_info.go:17-20`, verified):
```go
// Package v1alpha3 contains API Schema definitions for the tideproject v1alpha3 API group.
// +kubebuilder:object:generate=true
// +groupName=tideproject.k8s
package v1alpha3
```
`loop_types.go` needs **zero** additional kubebuilder markers on `LoopPolicy`/`LoopStatus`/`EvaluationSummary` to get `DeepCopy`/`DeepCopyInto` — confirmed by `Caps` (`task_types.go:31`) and `Gates` (`project_types.go:40`), neither of which carries a per-type marker, both of which appear in `zz_generated.deepcopy.go:87-110`.

**Two-homes / bounded-projection precedent — the exact doc-comment shape to copy** (`api/v1alpha3/task_types.go:23-30`, verified):
```go
// Caps declares resource caps applied to the subagent pod executing a Task (Phase 2+).
//
// Design note: api/v1alpha3.Caps and pkg/dispatch.Caps are intentionally two
// separate types that serve different layers — this struct is CEL-validated at
// the CRD admission boundary, while pkg/dispatch.Caps is the Go-only public API
// used by the dispatcher. Plan 09's TaskReconciler.buildEnvelopeIn translates
// one to the other at dispatch time, keeping the CRD schema and the dispatch
// interface decoupled.
type Caps struct { ... }
```
`EvaluationSummary` (the `LoopStatus.LastEvaluation` bounded projection of `pkg/dispatch.GateDecision`) must carry the same "Design note" framing — locally-scoped `Decision string`, NOT `pkg/dispatch.GateDecision` imported directly (D-01 explicitly rejects that import).

**Ref-field convention — plain string, never `corev1.LocalObjectReference`** (grep-verified zero hits for `corev1.LocalObjectReference`/`corev1.ObjectReference` in `api/v1alpha3/*.go`; 10 matches for the plain-string idiom, e.g. `project_types.go:224` `CredsSecretRef string`):
```go
// EvaluatorRef names the evaluator config this LoopPolicy resolves against
// (same-namespace name ref, mirroring PlanRef/PhaseRef/MilestoneRef — plain
// string, not corev1.LocalObjectReference).
EvaluatorRef string `json:"evaluatorRef,omitempty"`
```

**Enum-like policy string idiom** (`api/v1alpha3/shared_types.go:382-396`, verified — copy this exact three-part shape: typed string + kubebuilder Enum marker + const block with doc-commented values):
```go
// FailureProfileType is the failure-propagation policy for this Project.
// +kubebuilder:validation:Enum=strict;conservative
type FailureProfileType string

const (
	// FailureProfileStrict (default): non-dependent tasks in later waves
	// continue dispatching when an earlier task fails. ...
	FailureProfileStrict FailureProfileType = "strict"

	// FailureProfileConservative: first task execution failure halts all
	// new dispatch project-wide (ConditionFailureHalt) ...
	FailureProfileConservative FailureProfileType = "conservative"
)
```
Also mirrored by `api/v1alpha3/project_types.go:34-37` (`GatePolicy` — `"auto"|"approve"|"pause"`). `LoopPolicy.Autonomy` / `LoopStatus.ExitReason` should follow this exact shape (typed string + `+kubebuilder:validation:Enum=...` + doc-commented consts) — **but per RESEARCH Assumption A2, `ExitReason`'s value vocabulary is NOT locked by CONTEXT.md**; treat the concrete enum values as Claude's Discretion, not this pattern.

**Numeric field-type table** (grep-verified across every `api/v1alpha3/*.go` numeric field — reuse verbatim, do not invent a new convention):

| Semantic | Type | Precedent |
|---|---|---|
| Cost in cents | `int64` | `BudgetConfig.AbsoluteCapCents` (`project_types.go:99`), `pkg/dispatch.Usage.EstimatedCostCents` (`envelope.go:312`) — every cents field in the codebase is `int64` |
| Bounded duration (optional) | `*metav1.Duration` | `BudgetConfig.RollingWindowDuration` (`project_types.go:114`) |
| Iteration/attempt counter | `int32` | `Caps.Iterations` (`task_types.go:40`), `MaxAttemptsPerTask` (`project_types.go:425`) — `TaskStatus.Attempt` bare `int` (`task_types.go:157`) is a documented pre-existing outlier, not a pattern to follow |
| `[]metav1.Condition` | `+listType=map` / `+listMapKey=type` | `TaskStatus.Conditions` (`task_types.go:151-154`) |

**`TaskSpec`/`TaskStatus` field-doc conventions to mirror for `LoopStatus`'s "stays small" framing** (`api/v1alpha3/task_types.go:145-147`, verified):
```go
// TaskStatus defines the observed state of Task.
// Stays small per PERSIST-02 / Pitfall 4.
type TaskStatus struct { ... }
```
`LoopStatus`'s type-doc should use the equivalent framing plus the five-element-test language (D-06) — this is the doc-comment discipline LOOP-02 requires; there is no runtime check for it, only the compile-time structural-literal test (see `loop_types_test.go` below).

**Kubebuilder object root markers are NOT needed on `LoopPolicy`/`LoopStatus` this phase** — they are plain embeddable structs, not a `Kind`. `make manifests` produces zero diff (RESEARCH.md, verified corollary of the package-level marker mechanism); only `make generate` (deepcopy) runs.

---

### `api/v1alpha3/loop_types_test.go` (test, structural + round-trip)

**Analog:** `pkg/dispatch/envelope_test.go:698-716` (`TestTerminationStub_NoForbiddenFields`) — the compile-time struct-literal guard pattern.

**Compile-time forbidden-field guard — copy this shape exactly, applied to `LoopStatus`** (`envelope_test.go:703-713`, verified):
```go
// TestTerminationStub_NoForbiddenFields asserts (at compile time via struct
// literal) that TerminationStub carries no ChildCRDs, Result, or Artifacts
// field — those live on the PVC out.json, not in the tiny termination message.
// This test exists to pin the contract: if a forbidden field is ever added the
// literal below will fail to compile.
func TestTerminationStub_NoForbiddenFields(t *testing.T) {
	_ = TerminationStub{
		ExitCode:   0,
		Reason:     "",
		Usage:      Usage{},
		HeadSHA:    "",
		ChildCount: 0,
	}
	// Runtime assertion: marshalled JSON must not contain these keys.
	stub := NewTerminationStub(fullyPopulatedEnvelopeOut())
	data, err := json.Marshal(stub)
	...
	for _, forbidden := range []string{`"childCRDs"`, `"result"`, `"artifacts"`} {
		if strings.Contains(string(data), forbidden) {
			t.Errorf(...)
		}
	}
}
```
This is the Success-Criterion-#5 structural guard (RESEARCH Pitfall 3): a `_ = v1alpha3.LoopStatus{...}` literal naming every field, so a later PR adding `PreviousEvaluations []EvaluationSummary` fails to **compile**, not just fails a size assertion.

**External-test-package + reflect structural assertion convention** (`api/v1alpha3/schema_test.go:28,40-41`, verified):
```go
package v1alpha3_test
...
func TestProjectSpecV1alpha3(t *testing.T) {
	projectSpecType := reflect.TypeFor[tidev1alpha3.ProjectSpec]()
	...
```
Use this same `package v1alpha3_test` + `reflect.TypeFor` convention if `loop_types_test.go` needs a field-enumeration assertion (e.g. asserting `LoopStatus` has exactly N fields) rather than hand-rolling a new reflection helper.

---

### `pkg/dispatch/verdict.go` (model + utility, transform)

**Analog:** `pkg/dispatch/envelope.go:170-238,394-437` (`EnvelopeOut`, `TerminationStub`, `NewTerminationStub`), `pkg/dispatch/errors.go:21-43` (typed-error convention), `pkg/dispatch/doc.go` (package-doc + import-firewall banner).

**Doc-comment discipline to copy at file top** (`pkg/dispatch/doc.go:17-61`, verified) — every new exported type/const in `pkg/dispatch` gets a `[TypeName]`-linked doc comment in the same register as `EnvelopeIn`/`EnvelopeOut`; the package doc already states the import firewall (`sigs.k8s.io/controller-runtime`, `github.com/anthropics/*`, any `internal/*` forbidden) so `verdict.go` automatically satisfies it by using only `encoding/json` + stdlib.

**Typed-error convention — copy this exact two-part shape for any verdict-specific error** (`pkg/dispatch/errors.go:21-43`, verified):
```go
// UnknownAPIVersionError is returned by [ValidateAPIVersionKind] when the
// envelope's apiVersion field does not match [APIVersionV1Alpha1]. Consumers
// should use errors.As to distinguish this from [*UnknownKindError].
type UnknownAPIVersionError struct {
	APIVersion string
}

func (e *UnknownAPIVersionError) Error() string {
	return fmt.Sprintf("envelope: unknown apiVersion %q (expected %s)", e.APIVersion, APIVersionV1Alpha1)
}
```
(D-04's `ClassifyVerdict` should NOT return `(Verdict, error)` per RESEARCH Pitfall 2 — this error-type pattern is for a *different* verdict-adjacent failure mode if one is needed, not the classifier itself.)

**Fail-closed classifier — the exact 4-branch shape (verified pattern from RESEARCH, grounded in `ValidateAPIVersionKind`'s strict-equality-first discipline, `envelope.go:446-454`):**
```go
func ClassifyVerdict(raw json.RawMessage) Verdict {
	var parsed struct {
		Verdict string `json:"verdict"`
	}
	if len(raw) == 0 {
		return VerdictBlocked // empty JSON
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return VerdictBlocked // malformed
	}
	switch Verdict(parsed.Verdict) {
	case VerdictApproved, VerdictRepairable, VerdictBlocked:
		return Verdict(parsed.Verdict)
	default:
		return VerdictBlocked // missing/unrecognized verdict field
	}
}
```
`ValidateAPIVersionKind` (`envelope.go:439-454`, verified) is the existing strict-equality-first idiom this classifier's discipline extends — return-value-can't-express-unknown-as-unsafe is the load-bearing shape (RESEARCH Pitfall 2: a `(Verdict, error)` signature invites a caller to forget the error-is-BLOCKED mapping).

**`Finding`/`GateDecision` field types** — per RESEARCH Open Question 2 and CONTEXT.md specifics ("coverage-not-conservatism: every deviation is tagged, and policy decides what blocks, not the finder"): `Dimension`/`Severity`/`Confidence` are plain `string` fields (not a Go-const enum) — `pkg/dispatch` carries zero `+kubebuilder:` markers anywhere (grep-confirmed), and locking a severity vocabulary now risks Phase 51 prompt-rubric churn.

---

### `pkg/dispatch/verdict_test.go` (test, golden-fixture round trip + fail-closed regression)

**Analog:** `pkg/dispatch/envelope_test.go:118` (`assertRoundTripOut` helper shape), `:674-696` (`TestNewTerminationStub_StaysSmall` table-free single-scenario style).

**Round-trip test shape to mirror** — read fixture, unmarshal, assert against canonical values (NOT re-marshal-and-byte-compare — RESEARCH Anti-Patterns explicitly flags key-order non-determinism across Go/Python serializers as the trap):
```go
func TestGateDecision_GoldenFixtureRoundTrip(t *testing.T) {
	golden, err := os.ReadFile("testdata/gate_decision_golden.json")
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var decoded GateDecision
	if err := json.Unmarshal(golden, &decoded); err != nil {
		t.Fatalf("unmarshal golden fixture: %v", err)
	}
	if decoded.Verdict != VerdictRepairable {
		t.Errorf("Verdict = %q, want %q", decoded.Verdict, VerdictRepairable)
	}
	...
}
```

**Fail-closed regression table — the exact 3 named shapes D-04 requires, plus a positive control:**
```go
func TestClassifyVerdict_FailsClosed(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want Verdict
	}{
		{"EmptyJSON", ``, VerdictBlocked},
		{"MissingVerdictField", `{"summary":"looks fine","findings":[]}`, VerdictBlocked},
		{"Malformed", `{not valid json`, VerdictBlocked},
		{"ValidApproved", `{"verdict":"APPROVED","summary":"ok","findings":[]}`, VerdictApproved},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ClassifyVerdict([]byte(c.raw)); got != c.want {
				t.Errorf("ClassifyVerdict(%q) = %q, want %q", c.raw, got, c.want)
			}
		})
	}
}
```

**`TestNewTerminationStub_StaysSmall` worst-case-input shape to extend** (`envelope_test.go:674-696`, verified) — the file to modify (`envelope.go`) already has this test; extend it in place with a worst-case `GateDecision` (50 findings, all high-severity) and re-assert `< 4096`:
```go
func TestNewTerminationStub_StaysSmall(t *testing.T) {
	out := fullyPopulatedEnvelopeOut()
	out.Result = strings.Repeat("x", 10*1024)
	out.ChildCRDs = make([]ChildCRDSpec, 50)
	...
	stub := NewTerminationStub(out)
	data, err := json.Marshal(stub)
	...
	if len(data) >= 4096 {
		t.Errorf("TerminationStub JSON size = %d bytes, want < 4096", len(data))
	}
}
```

**Golden-fixture path resolution (Go side is trivial; flag for the Python side, see verdict.py below):** `go test` always sets cwd to the package directory, so `os.ReadFile("testdata/gate_decision_golden.json")` just works. `pkg/dispatch/` has **no existing `testdata/` directory** (grep-confirmed) — this is the first; create `pkg/dispatch/testdata/gate_decision_golden.json`.

---

### `pkg/dispatch/envelope.go` (MODIFY — add `VerifyContext`, extend `TerminationStub`)

**Analog:** itself — `pkg/dispatch/envelope.go:146,152` (`Dispatch`/`Dev` pointer+omitempty fields), `:349-377` (`DispatchMeta`/`Dev` struct defs), `:394-437` (`TerminationStub`/`NewTerminationStub`).

**Pointer+omitempty envelope field — the EXACT idiom `Verify *VerifyContext` copies** (`pkg/dispatch/envelope.go:146,152`, verified):
```go
Dispatch *DispatchMeta `json:"dispatch,omitempty"`

// Dev carries test-fixture-only metadata injected by integration tests. ...
// The field is omitted from JSON when nil so production envelopes are not
// polluted with "dev: null".
Dev *Dev `json:"dev,omitempty"`
```
Add immediately after `Dev` (`:152`), same struct (`EnvelopeIn`), same doc-comment register as `DispatchMeta` (`:349-362`):
```go
// Verify carries verify-dispatch-specific input data. Populated only when
// Role=="verifier" (D-03); omitted from JSON otherwise, mirroring Dispatch/Dev.
Verify *VerifyContext `json:"verify,omitempty"`
```
`VerifyContext` itself is declared alongside `DispatchMeta`/`Dev` (`:349-377`), not in `verdict.go` — `verdict.go` is reserved for the OUTPUT/verdict schema (D-01).

**`Role` field doc needs updating, not a type change** (`envelope.go:58-59`, verified) — `Role` is `string`, no Go enum; grep across `pkg/dispatch/*.go` + `internal/controller/dispatch_helpers.go` confirms every use is a bare string literal (`"planner"`/`"executor"`), never a typed const. Adding `"verifier"` needs only:
```go
// Role is the planner/executor/verifier selector: "planner", "executor", or "verifier".
```

**`TerminationStub` extension — extend the EXISTING struct and constructor, do not create a parallel type** (`pkg/dispatch/envelope.go:394-437`, verified — this is the file/function to edit in place):
```go
type TerminationStub struct {
	ExitCode int    `json:"exitCode"`
	Reason   string `json:"reason"`
	Usage    Usage  `json:"usage"`
	HeadSHA  string `json:"headSHA,omitempty"`
	ChildCount int  `json:"childCount"`

	// NEW (EVAL-05 / D-05a) — bounded verdict summary. GateDecision is the
	// enum string only (never a free-text summary). Empty/zero on any
	// non-verify dispatch, matching ChildCount's zero-for-non-planner convention.
	GateDecision      string `json:"gateDecision,omitempty"`
	FindingsCount     int    `json:"findingsCount,omitempty"`
	HighSeverityCount int    `json:"highSeverityCount,omitempty"`
}
```
`NewTerminationStub` extends the same nil-safe-pointer idiom it already uses for `out.Git`:
```go
func NewTerminationStub(out EnvelopeOut) TerminationStub {
	headSHA := ""
	if out.Git != nil {
		headSHA = out.Git.HeadSHA
	}
	stub := TerminationStub{
		ExitCode: out.ExitCode, Reason: out.Reason, Usage: out.Usage,
		HeadSHA: headSHA, ChildCount: len(out.ChildCRDs),
	}
	if out.Verdict != nil { // EnvelopeOut.Verdict *GateDecision — Phase 51 populates it
		stub.GateDecision = string(out.Verdict.Verdict)
		stub.FindingsCount = len(out.Verdict.Findings)
		for _, f := range out.Verdict.Findings {
			if f.Severity == "blocker" { stub.HighSeverityCount++ }
		}
	}
	return stub
}
```
**Anti-pattern to avoid** (RESEARCH, verified against `envelope.go`'s actual `Reason` field, which has NO Go-side truncation — only the Python `write_termination_stub` truncates): do not add a `Summary string` free-text field to `TerminationStub`. Keep the new fields bounded-by-construction (enum + ints), matching `TestTerminationStub_NoForbiddenFields`'s compile-time-literal guard which must also be updated to include the 3 new fields.

---

### `cmd/tide-langgraph-verifier/verifier/verdict.py` (NEW, model + utility)

**Analog:** `cmd/tide-langgraph-verifier/verifier/envelope.py` (whole-file convention — module docstring, `EnvelopeError`, `write_termination_stub`).

**Module docstring convention to copy verbatim in structure** (`verifier/envelope.py:1-9`, verified):
```python
"""Python re-implementation of the TIDE dispatch envelope wire contract.

Field-for-field port of pkg/dispatch/envelope.go's JSON shapes (D-03). The
Python image cannot import the Go package (import-firewalled, see
pkg/dispatch/doc.go), so this module independently re-implements the JSON
wire shapes...
"""
```
`verdict.py`'s docstring should reference `pkg/dispatch/verdict.go` the same way, and note it is a `pydantic.BaseModel` pair (not `@dataclass` like `envelope.py`'s `EnvelopeIn`) — per RESEARCH Assumption A4, Phase 51's `create_agent(response_format=GateDecision)` (LangChain structured output) requires Pydantic-compatible schema; building it as a dataclass now means Phase 51 has to convert it later for zero present benefit.

**Wire-contract discriminator constants — same placement, same naming register** (`verifier/envelope.py:19-28`, verified):
```python
API_VERSION = "dispatch.tideproject.k8s/v1alpha1"
KIND_IN = "TaskEnvelopeIn"
KIND_OUT = "TaskEnvelopeOut"
TERMINATION_STUB_MAX_BYTES = 4096
```

**`write_termination_stub`'s truncation-loop idiom to extend** (`verifier/envelope.py:149-175`, verified — this is the function `gate_decision`/`findings_count`/`high_severity_count` params get added to):
```python
def write_termination_stub(
    path: str | os.PathLike[str],
    *,
    exit_code: int,
    reason: str = "",
) -> None:
    stub: dict[str, Any] = {"exitCode": exit_code, "reason": reason}
    data = json.dumps(stub).encode("utf-8")
    while len(data) > TERMINATION_STUB_MAX_BYTES and reason:
        overflow = len(data) - TERMINATION_STUB_MAX_BYTES
        keep = max(0, len(reason) - overflow - len("...(truncated)"))
        reason = reason[:keep] + "...(truncated)"
        stub["reason"] = reason
        data = json.dumps(stub).encode("utf-8")
    ...
```
Extended signature per RESEARCH Architecture Patterns diagram: `write_termination_stub(path, *, exit_code, reason="", gate_decision="", findings_count=0, high_severity_count=0)` — the new fields are bounded ints/enum-string, so they join the dict unconditionally (no truncation loop needed for them; only `reason` is unbounded free text).

**Fail-closed classifier — Python mirror, identical 3-branch shape so test names line up 1:1 across languages** (pattern, per D-04 + RESEARCH Code Example §2):
```python
def classify_verdict(raw: str | bytes) -> Verdict:
    if not raw:
        return Verdict.BLOCKED  # empty JSON
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError:
        return Verdict.BLOCKED  # malformed
    verdict_str = parsed.get("verdict") if isinstance(parsed, dict) else None
    if verdict_str in (Verdict.APPROVED, Verdict.REPAIRABLE, Verdict.BLOCKED):
        return Verdict(verdict_str)
    return Verdict.BLOCKED  # missing/unrecognized verdict field
```

**Golden-fixture path resolution (Python side — the one genuinely new plumbing this phase introduces, no existing precedent in-repo to cite):**
```python
def _repo_root() -> Path:
    """Walk up from this file until go.mod is found (repo root marker)."""
    p = Path(__file__).resolve()
    for parent in p.parents:
        if (parent / "go.mod").exists():
            return parent
    raise RuntimeError("could not locate repo root (no go.mod found above " + str(p) + ")")

GOLDEN_FIXTURE = _repo_root() / "pkg" / "dispatch" / "testdata" / "gate_decision_golden.json"
```
Required because `make test-langgraph-verifier` `cd`s into `cmd/tide-langgraph-verifier` before invoking pytest — a fixed `Path(__file__).parents[N]` is fragile against directory-depth changes; walk to `go.mod` instead.

---

### `cmd/tide-langgraph-verifier/verifier/envelope.py` (MODIFY — add `VerifyContext` field)

**Analog:** itself — `EnvelopeIn` dataclass (`:41-58`) already has the exact "typed fields the runtime consumes + `raw` catch-all for accept-and-ignore" idiom.

**`EnvelopeIn` dataclass shape to extend** (`verifier/envelope.py:41-58`, verified):
```python
@dataclass
class EnvelopeIn:
    """Field-for-field port of pkg/dispatch/envelope.go's EnvelopeIn (:45).
    ...
    """
    api_version: str
    kind: str
    task_uid: str
    role: str
    level: str
    prompt: str
    provider_vendor: str
    provider_model: str
    raw: dict[str, Any] = field(default_factory=dict)
```
Add `verify: dict[str, Any] | None = None` (or a typed `VerifyContext` dataclass, mirroring the `provider_vendor`/`provider_model` extraction-from-nested-dict style at `read_envelope_in:97-113`) — the **existing unknown-field tolerance test already anticipates this exact phase**:
```python
# verifier/tests/test_envelope.py:78-86, verified
def test_read_envelope_in_tolerates_unknown_fields(tmp_path, envelope_in_dict) -> None:
    payload = envelope_in_dict(futureField="something-phase-49-adds")
    ...
    assert env.raw["futureField"] == "something-phase-49-adds"
```
Any new `EnvelopeIn` field must preserve this accept-and-ignore contract for *other* unknown keys — extracting `verify` explicitly (like `provider_vendor`/`provider_model` today) is fine; it does not weaken this test since `raw` still carries everything.

**`read_envelope_in`'s validate-then-extract idiom to mirror for the nested `verify` object** (`verifier/envelope.py:97-103`, verified — this is the pattern to copy for extracting `Verify`):
```python
provider = raw.get("provider")
if provider is None:
    provider = {}
elif not isinstance(provider, dict):
    raise EnvelopeError(
        f"read envelope {path!s}: 'provider' must be a JSON object, got {type(provider).__name__}"
    )
```
This is also the WR-01 fail-closed precedent (`test_envelope.py:63-75`, "a non-object `provider` must raise EnvelopeError, never an uncaught AttributeError") — apply the identical guard to `verify`.

---

### `cmd/tide-langgraph-verifier/verifier/tests/test_verdict.py` + `conftest.py` (NEW/MODIFY)

**Analog:** `verifier/tests/test_envelope.py` (parametrize + `pytest.raises` convention), `verifier/tests/conftest.py:61-72` (`envelope_in_dict` factory).

**Parametrize convention to copy** (`test_envelope.py:63-66`, verified):
```python
@pytest.mark.parametrize("bad_provider", ["anthropic", 42, ["anthropic"]])
def test_read_envelope_in_rejects_non_object_provider(tmp_path, envelope_in_dict, bad_provider) -> None:
```
Apply the same shape to the fail-closed classifier test (RESEARCH Code Example §2 already gives the exact 4-row parametrize table).

**Factory-fixture convention to copy for a new `gate_decision_dict`/`verify_context_dict` fixture** (`conftest.py:61-72`, verified):
```python
@pytest.fixture
def envelope_in_dict() -> Callable[..., dict[str, Any]]:
    """Factory returning a valid TaskEnvelopeIn dict using the verbatim field
    names and discriminator values pkg/dispatch/envelope.go defines (D-03 —
    the Python image re-implements this JSON shape independently since it
    cannot import the Go package).
    """
    def _build(**overrides: Any) -> dict[str, Any]:
        envelope: dict[str, Any] = { "apiVersion": API_VERSION, "kind": KIND_TASK_ENVELOPE_IN, ... }
        envelope.update(overrides)
        return envelope
    return _build
```
Add a sibling `gate_decision_dict` fixture in `conftest.py` with the same factory-with-overrides shape, seeded with the canonical `APPROVED`/`REPAIRABLE`/`BLOCKED` + `findings[]` fields.

---

### `cmd/tide-push/main.go` `stageEnvelopeArtifacts` (MODIFY — glob generalization)

**Analog:** itself — the function already handles the Milestone/Phase/Plan/Project `*.md`+`children/*.json` shape; this phase generalizes it, additively, to accept a Task-shaped (`findings.json`-only, no `*.md` requirement) entry without breaking the existing path.

**The exact hard-fail this phase must generalize (not remove)** (`cmd/tide-push/main.go:1169-1183`, verified — read directly, confirms RESEARCH's citation exactly):
```go
mdMatches, gerr := filepath.Glob(filepath.Join(srcDir, "*.md"))
if gerr != nil {
	writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
	fmt.Fprintf(stderr, "tide-push: stage-envelopes: glob *.md in %s: %v\n", srcDir, gerr)
	return exitGenericFail
}
if len(mdMatches) == 0 {
	// A planner-completed level always emits at least one planning *.md;
	// an empty set means the envelope is incomplete — fail loudly (D-03).
	writePushEnvelope(cfg, "", exitGenericFail, "artifact-stage-failed", nil, 0, "")
	fmt.Fprintf(stderr,
		"tide-push: stage-envelopes: no *.md under %s (a planner-completed level must have at least one)\n",
		srcDir)
	return exitGenericFail
}
```
**`EnvelopeStage` is the struct to extend** (`cmd/tide-push/main.go:137-145`, verified):
```go
// EnvelopeStage maps one on-PVC envelope directory (UID-keyed, the same key the
// dispatch/reporter path writes under envelopes/<uid>/) to the human-readable
// destination prefix it lands at on the run branch: .tide/planning/<DestPrefix>/
// (DASH-02, D-02). DestPrefix is `<kind>/<name>` with kind ∈
// {project, milestone, phase, plan}.
type EnvelopeStage struct {
	UID        string
	DestPrefix string
}
```
Per RESEARCH's recommendation (Assumption A3 — mechanism choice is Claude's Discretion, but generalizing at all is NOT optional): derive the expected glob from `DestPrefix`'s first path segment (`strings.Cut(es.DestPrefix, "/")`) — `"task"` → require `findings.json` only, no `*.md`; every other kind (`project`/`milestone`/`phase`/`plan`) → unchanged behavior. **Do not add a Task entry to `collectStageEnvelopes` this phase** (that stays Phase 51's job — nothing produces `findings.json` yet); only make `stageEnvelopeArtifacts` capable of handling one when it eventually arrives, so the existing 4-kind path stays byte-identical.

**`parseStageEnvelopes`'s validation-then-append idiom** (`cmd/tide-push/main.go:164-201`, verified) — if a `Glob`/`kind`-derivation field is added to `EnvelopeStage`, follow this function's existing fail-closed-per-token discipline (non-empty checks, pattern match, traversal-containment check) rather than adding a second validation pass elsewhere.

---

### `cmd/tide-push/main_test.go` (MODIFY — new regression case)

**Analog:** `TestStageEnvelopesEmptyDirFailsLoud` (`:1377-1412`, verified — full test read) and `TestStageEnvelopesHappyPath` (`:1240`).

**Exact test shape to add a sibling of** (`main_test.go:1377-1412`, verified):
```go
func TestStageEnvelopesEmptyDirFailsLoud(t *testing.T) {
	base := t.TempDir()
	bareSrc, _ := seedBareRepo(t, base)
	branch := perRunBranch(t, "stage-emptymd")
	ws := setupWorkspace(t, bareSrc, branch)

	// Dir exists (via a child json) but has NO top-level *.md → incomplete envelope.
	writeEnvelopeFile(t, ws, "emptymd", "children/task-1.json", []byte(`{"kind":"Task"}`))

	cfg := pushConfig{
		Mode: "push", Branch: branch, CommitMessage: "tide: stage empty-md envelope",
		StageEnvelopes: "emptymd:phase/p9", Workspace: ws, ProjectUID: "p-stage-emptymd",
	}
	...
	exit, stderr := stderrAndRun(t, ctx, cfg, "test-pat")
	if exit == 0 { t.Fatalf("exit=0, want nonzero for an envelope dir with no *.md; stderr=%s", stderr) }
	pr := readPushEnvelope(t, ws, "p-stage-emptymd")
	if pr.Reason != "artifact-stage-failed" { ... }
}
```
The new regression case (per RESEARCH's Test Map) proves the OPPOSITE: a `task/<name>` `DestPrefix` with only `findings.json` (no `*.md`) now **succeeds** where a `phase/`-kind entry with the same shape would still fail. Use `writeEnvelopeFile(t, ws, "<uid>", "findings.json", []byte(...))` + `StageEnvelopes: "<uid>:task/t1"` and assert `exit == 0`.

---

## Shared Patterns

### Strict apiVersion/kind equality first
**Source:** `pkg/dispatch/envelope.go:439-454` (`ValidateAPIVersionKind`) + its Python mirror `verifier/envelope.py:61-95` (`read_envelope_in`'s discriminator check, performed BEFORE any other field read).
**Apply to:** Any new envelope-seam consumer — `VerifyContext` extraction on both sides rides this existing discriminator; do not add a second version-check mechanism.
```go
func ValidateAPIVersionKind(apiVersion, kind, expectedKind string) error {
	if apiVersion != APIVersionV1Alpha1 {
		return &UnknownAPIVersionError{APIVersion: apiVersion}
	}
	if kind != expectedKind {
		return &UnknownKindError{Kind: kind}
	}
	return nil
}
```

### Pointer + omitempty for optional envelope payload structs
**Source:** `pkg/dispatch/envelope.go:146,152` (`Dispatch`/`Dev`).
**Apply to:** `EnvelopeIn.Verify *VerifyContext` and `EnvelopeOut`'s eventual `Verdict *GateDecision` field (Phase 51 populates it, but the pointer+omitempty shape belongs on `EnvelopeOut` now per the Architecture diagram in RESEARCH) — non-verify dispatches serialize nothing.

### Fail-closed classification (security-relevant control, V5 Input Validation)
**Source:** D-04 + `ValidateAPIVersionKind`'s strict-equality-first discipline.
**Apply to:** `pkg/dispatch.ClassifyVerdict` and `verifier.classify_verdict` — both must map empty/malformed/missing-field to `BLOCKED`, never fall through to a caller-decided default. This is the direct structural mitigation for the 2026-07-03 silent-fail-open incident this milestone exists to fix.

### Two homes, two types, translated at the boundary (never a shared Go import across the seam)
**Source:** `api/v1alpha3/task_types.go:23-30` (`Caps`/`pkg/dispatch.Caps` decoupling doc-comment) — the established precedent D-01 explicitly cites and extends.
**Apply to:** `api/v1alpha3.EvaluationSummary` (bounded projection) vs. `pkg/dispatch.GateDecision` (full wire type) — declare independently, translate at reconcile/read time (Phase 50/51's job), never `import "pkg/dispatch"` into `api/v1alpha3`.

### Hand-authored Go↔Python parity under the import firewall
**Source:** `pkg/dispatch/doc.go:44-55` (the firewall rule) + `verifier/envelope.py:1-9` (the docstring naming it explicitly).
**Apply to:** Every new `verdict.go`/`verdict.py` field — the JSON tag / Pydantic alias is the contract; the golden fixture (`pkg/dispatch/testdata/gate_decision_golden.json`) is the proof, exercised identically by both languages' test suites.

### Compile-time structural guard against `.status` history creep
**Source:** `pkg/dispatch/envelope_test.go:698-716` (`TestTerminationStub_NoForbiddenFields`).
**Apply to:** `LoopStatus` (LOOP-03 / Success Criterion #5) — a `_ = v1alpha3.LoopStatus{...}` struct literal naming every field, so a later PR adding a history slice fails to compile against the test.

## No Analog Found

None — every file in this phase's scope has a direct, verified in-repo analog (this is a schema-extension phase; every new type is a same-package sibling of an existing, actively-maintained type).

## Explicitly Out of Scope (do not create analogs for these)

Per CONTEXT.md's `<deferred>` block and RESEARCH's confirmed phase-boundary — do NOT map or plan work for:
- `ConditionVerifyHalt` / `setVerifyHaltIfNeeded` / resume time-fence / dispatch-gate wiring (Phase 50)
- `TaskReconciler` verifier dispatch, concurrency-gate accounting, `LoopPolicy.BudgetCents` reservation, `onExhaustion` escalation (Phase 51)
- Embedding `LoopPolicy`/`LoopStatus` into `TaskSpec`/`TaskStatus` (Phase 51 TASK-01) — this phase's types are standalone/unreferenced by any Kind
- Adding a Task entry to `collectStageEnvelopes` (`internal/controller/artifact_push.go:84`) — Phase 51's job once `findings.json` actually exists; this phase only makes the `tide-push` consumer side (`stageEnvelopeArtifacts`) capable of handling one
- `role="verifier"` orchestrator-side Go prompt templates (Phase 51 EVAL-04)
- `"langgraph"` vendor sentinel / `SelfInstruments` / `EVALUATOR`-kind span (Phase 51 OBS-03)

## Metadata

**Analog search scope:** `api/v1alpha3/` (all `*_types.go` + `groupversion_info.go` + `schema_test.go`), `pkg/dispatch/` (all `.go` files incl. `errors.go`, `doc.go`, `envelope_test.go`), `cmd/tide-langgraph-verifier/verifier/` (`envelope.py`, `tests/conftest.py`, `tests/test_envelope.py`), `internal/controller/` (`artifact_push.go`, `push_helpers.go`, `artifact_push_test.go`), `cmd/tide-push/main.go` + `main_test.go`.
**Files scanned (direct Read):** 13 source files, 4 test files — all excerpts above are verified against the current tree in this session (no excerpt is copied from RESEARCH.md without independent confirmation; two citations were corrected in the process — `collectStageEnvelopes` lives at `internal/controller/artifact_push.go:84`, not `push_helpers.go:81` as CONTEXT.md's canonical_refs stated; `push_helpers.go:81-88` is `PushOptions.StageEnvelopes`'s doc comment, a related but distinct symbol).
**Pattern extraction date:** 2026-07-18
