# Phase 20: SharedContext Injection + Cache Verification Spike — Pattern Map

**Mapped:** 2026-06-15
**Files analyzed:** 12 new/modified files
**Analogs found:** 11 / 12

---

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `pkg/dispatch/envelope.go` | model | transform | itself (additive fields) | exact (same file) |
| `pkg/dispatch/childcrd.go` | model | transform | itself (additive field) | exact (same file) |
| `api/v1alpha1/task_types.go` | model | CRUD | `api/v1alpha1/phase_types.go` | role-match |
| `api/v1alpha1/phase_types.go` | model | CRUD | `api/v1alpha1/task_types.go` | role-match |
| `api/v1alpha1/plan_types.go` | model | CRUD | `api/v1alpha1/task_types.go` | role-match |
| `api/v1alpha1/milestone_types.go` | model | CRUD | `api/v1alpha1/task_types.go` | role-match |
| `internal/controller/dispatch_helpers.go` | utility | request-response | itself (`BuildPlannerEnvelope`) | exact (same function) |
| `internal/subagent/common/templates/milestone_planner.tmpl` | config | transform | itself (slot replacement) | exact (same file) |
| `internal/subagent/common/templates/project_planner.tmpl` | config | transform | `milestone_planner.tmpl` | exact |
| `internal/subagent/common/templates/phase_planner.tmpl` | config | transform | `milestone_planner.tmpl` | exact |
| `internal/subagent/common/templates/plan_planner.tmpl` | config | transform | `milestone_planner.tmpl` | exact |
| `cmd/tide-spike/main.go` | utility | request-response | `cmd/tide-eval/main.go` | exact |

---

## Pattern Assignments

### `pkg/dispatch/envelope.go` (model, transform) — additive fields on `EnvelopeIn` and `EnvelopeOut`

**Analog:** itself — the existing `PromptPath`/`Branch` (executor-only omitempty) and `Dev`/`Dispatch` (pointer+omitempty) fields are the direct precedent for the inverse: a planner-only omitempty field.

**Existing omitempty executor-only fields pattern** (lines 75–83):
```go
// PromptPath is the workspace-relative path of the executor prompt artifact
// on the per-Project namespace PVC ... Non-empty only for
// executor dispatches (role="executor"). ...
PromptPath string `json:"promptPath,omitempty"`

// Branch is the per-run git branch ... Non-empty only for executor dispatches
// (role="executor"); planner dispatches short-circuit worktree creation.
Branch string `json:"branch,omitempty"`
```

**Add to `EnvelopeIn` after the `Dev` field (line 146):**
```go
// SharedContext is the wave-scoped shared context string emitted by the
// parent planner and stamped byte-identically onto all wave siblings by
// the controller at child dispatch time (Phase 20 CACHE-02/D-05). Grows
// the stable prefix toward the provider's cacheable minimum (≥1,024 tokens
// for Sonnet/Opus; 4,096 for Haiku — see PROJECT.md provider floor table).
//
// Planner templates reference {{.SharedContext}} in the D-07 reserved slot.
// Executor dispatches (role="executor") never populate this field (CACHE-02
// lock) — the executor template does not reference it.
SharedContext string `json:"sharedContext,omitempty"`
```

**Existing `EnvelopeOut.ChildCRDs` carry-field precedent** (lines 188–196):
```go
// ChildCRDs is the slice of typed child-CRD declarations a planner
// subagent emits for the orchestrator to materialize server-side (D-A1).
// ...Omitted from JSON when empty so executor-level EnvelopeOut
// documents stay small.
ChildCRDs []ChildCRDSpec `json:"childCRDs,omitempty"`
```

**Add to `EnvelopeOut` after `ChildCount` (line 213):**
```go
// SharedContext is the curated wave-scoped shared context string the
// parent planner emits for the orchestrator to stamp byte-identically onto
// each sibling child's EnvelopeIn.SharedContext at dispatch time (D-05).
// Empty for executor-level dispatches and genuine leaf planners (no
// children to annotate). omitempty keeps executor out.json documents small.
SharedContext string `json:"sharedContext,omitempty"`
```

**Impact on existing tests:** `omitempty` ensures `SharedContext=""` never appears in serialized JSON. `TestNewTerminationStub_StaysSmall` passes without change (SharedContext is NOT on TerminationStub — see Anti-Patterns). Existing golden fixtures and envelope round-trip tests that do not set SharedContext continue to pass without modification.

---

### `pkg/dispatch/childcrd.go` (model, transform) — additive `SharedContext` carry field

**Analog:** the existing `SourcePath` field (lines 59–66) — set by the subagent on the PVC read path and copied by `MaterializeChildCRDs` into `Task.Spec.PromptPath`. `SharedContext` follows the identical pattern: set by the orchestrator on the parent `EnvelopeOut`, and copied by `MaterializeChildCRDs` into each child's `Spec.SharedContext`.

**Existing SourcePath carry-field pattern** (`pkg/dispatch/childcrd.go:59–66`):
```go
// SourcePath is set BY THE SUBAGENT (not the planner JSON) when a child file
// is read off the PVC: the workspace-relative path of the originating
// children/<name>.json artifact...
// The orchestrator copies this into Task.Spec.PromptPath at materialization so
// the executor instruction...can be read fresh from the PVC on every dispatch.
SourcePath string `json:"sourcePath,omitempty"`
```

**Add to `ChildCRDSpec` after `SourcePath`:**
```go
// SharedContext is the wave-scoped shared context string to stamp onto this
// child CRD's Spec.SharedContext at materialization time (Phase 20 D-05).
// Set by the orchestrator from EnvelopeOut.SharedContext; NOT authored by
// the LLM (the model emits ChildCRDSpec.Spec, not this field). The
// materializer copies it into the typed child Spec, mirroring how SourcePath
// is copied into Task.Spec.PromptPath. omitempty so child JSON stays small
// when the parent planner emits no SharedContext.
SharedContext string `json:"sharedContext,omitempty"`
```

**Usage in `internal/reporter/materialize.go` (after decode per Kind, mirroring SourcePath stamp at line 233):**
```go
// After unmarshal into tk.Spec, stamp SharedContext from the ChildCRDSpec
// (mirrors tk.Spec.PromptPath = child.SourcePath at line 233):
tk.Spec.SharedContext = child.SharedContext
// Same stamp for Phase, Plan, Milestone children (D-07 uniform carry):
ph.Spec.SharedContext = child.SharedContext
pl.Spec.SharedContext = child.SharedContext
ms.Spec.SharedContext = child.SharedContext
```

---

### `api/v1alpha1/task_types.go` (model, CRUD) — `SharedContext` on `TaskSpec`

**Analog:** `PromptPath` field on `TaskSpec` (lines 82–94) — a carry field stamped by the orchestrator at materialization time, not authored inline by the model.

**Existing PromptPath carry-field pattern** (`api/v1alpha1/task_types.go:82–94`):
```go
// PromptPath is the PVC-relative path...to the durable children/task-NN.json
// artifact this Task was materialized from...The materializer always sets
// this; a Task without it is undispatchable, so it is required at the API
// boundary (MinLength=1).
// +kubebuilder:validation:MinLength=1
PromptPath string `json:"promptPath"`
```

**Add to `TaskSpec` after `Dev` (line 109):**
```go
// SharedContext is the wave-scoped shared context string stamped by the
// orchestrator at Task creation time (Phase 20 D-05). Populated from the
// parent planner's EnvelopeOut.SharedContext; byte-identical across all
// sibling Tasks in the same wave. The dispatcher reads this at Task
// dispatch time and places it in EnvelopeIn.SharedContext (D-07).
// Empty for Tasks authored before Phase 20 or where the parent planner
// emitted no SharedContext; omitempty keeps older CRD objects small.
// +optional
SharedContext string `json:"sharedContext,omitempty"`
```

---

### `api/v1alpha1/phase_types.go`, `plan_types.go`, `milestone_types.go` (model, CRUD) — uniform `SharedContext`

**Analog:** `PhaseSpec.MilestoneRef` / `PlanSpec` parent-ref fields — the minimal carry data stamped by the orchestrator at materialization. These spec types are currently very thin (2–3 fields each), and SharedContext follows the same stamp-at-create pattern.

**Existing PhaseSpec pattern** (`api/v1alpha1/phase_types.go:24–32`):
```go
type PhaseSpec struct {
    // MilestoneRef is the name of the owning Milestone (same namespace).
    // +kubebuilder:validation:MinLength=1
    MilestoneRef string `json:"milestoneRef"`

    // DependsOn lists sibling Phase names in the same Milestone. Optional.
    // +optional
    DependsOn []string `json:"dependsOn,omitempty"`
}
```

**Add to each of `PhaseSpec`, `PlanSpec`, `MilestoneSpec` — identical pattern:**
```go
// SharedContext is the wave-scoped shared context string stamped by the
// orchestrator at object creation time (Phase 20 D-05). Byte-identical
// across all siblings in the same wave. Read by BuildPlannerEnvelope when
// dispatching this object's planner Job (D-07 uniform path).
// +optional
SharedContext string `json:"sharedContext,omitempty"`
```

---

### `internal/controller/dispatch_helpers.go` (utility, request-response) — `BuildPlannerEnvelope` signature + body

**Analog:** itself — adding one field to the existing struct literal (lines 218–244).

**Existing `BuildPlannerEnvelope` full body** (`dispatch_helpers.go:218–244`):
```go
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project,
    attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string,
    helmDefaults ProviderDefaults) (pkgdispatch.EnvelopeIn, []byte, error) {
    envIn := pkgdispatch.EnvelopeIn{
        APIVersion:    pkgdispatch.APIVersionV1Alpha1,
        Kind:          pkgdispatch.KindTaskEnvelopeIn,
        TaskUID:       string(parent.GetUID()),
        Role:          "planner",
        Level:         level,
        Prompt:        prompt,
        Caps:          caps,
        ProxyEndpoint: proxyEndpoint,
        SignedToken:   token,
        Provider:      ResolveProvider(project, level, helmDefaults),
    }
    envIn.Dispatch = &pkgdispatch.DispatchMeta{ParentName: parent.GetName()}
    data, err := json.Marshal(envIn)
    if err != nil {
        return pkgdispatch.EnvelopeIn{}, nil, fmt.Errorf("marshal planner envelope: %w", err)
    }
    return envIn, data, nil
}
```

**Phase 20 change — add `sharedContext string` parameter, stamp into envIn:**
```go
func BuildPlannerEnvelope(level string, parent metav1.Object, project *tideprojectv1alpha1.Project,
    attempt int, token, prompt string, caps pkgdispatch.Caps, proxyEndpoint string,
    helmDefaults ProviderDefaults, sharedContext string) (pkgdispatch.EnvelopeIn, []byte, error) {
    envIn := pkgdispatch.EnvelopeIn{
        // ... all existing fields unchanged ...
        SharedContext: sharedContext, // NEW: populated from parent CRD Spec.SharedContext
    }
    // ... rest unchanged ...
}
```

**All call sites that must be updated (confirmed by grep):**
- `milestone_controller.go:416` — pass `ms.Spec.SharedContext`
- `phase_controller.go:379` — pass `ph.Spec.SharedContext`
- `plan_controller.go:398` — pass `plan.Spec.SharedContext`
- `dispatch_helpers_test.go:124` — pass `""` (fixture; no SharedContext)
- `dispatch_helpers_test.go:272` — pass `""` (fixture; no SharedContext)

**Executor path is deliberately NOT touched.** `buildEnvelopeIn` in `task_controller.go:1347–1369` does not call `BuildPlannerEnvelope`; it constructs the executor `EnvelopeIn` literal directly and omits `SharedContext` by construction (CACHE-02 lock).

---

### `internal/controller/task_controller.go` — executor path unchanged; child-CRD creation stamped

**Analog for child-CRD carry stamp:** `materialize.go:233` — `tk.Spec.PromptPath = child.SourcePath`.

**Executor `buildEnvelopeIn` — no change required** (`task_controller.go:1347–1369`):
```go
envIn := pkgdispatch.EnvelopeIn{
    APIVersion:          pkgdispatch.APIVersionV1Alpha1,
    Kind:                pkgdispatch.KindTaskEnvelopeIn,
    TaskUID:             string(task.UID),
    Role:                "executor",
    Level:               "task",
    PromptPath:          task.Spec.PromptPath,
    Branch:              project.Status.Git.BranchName,
    // ... all other fields unchanged ...
    // SharedContext intentionally absent — CACHE-02 lock (executor ignores it)
}
```

The `SharedContext` field defaults to `""` and serializes as absent due to `omitempty`. No code change needed; the CACHE-02 lock is enforced by omission.

---

### Four planner templates (config, transform) — slot replacement

**Analog:** the existing `{{.Provider.Vendor}}` / `{{.Provider.Model}}` / `{{.TaskUID}}` interpolations in the same templates — scalar string fields rendered conditionally into the stable prefix.

**Existing reserved slot in all four planner templates:**
- `milestone_planner.tmpl:42` — `{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}`
- `project_planner.tmpl:44` — same
- `phase_planner.tmpl:49` — same
- `plan_planner.tmpl:96` — same

**Slot context in `milestone_planner.tmpl` (lines 40–44):**
```
Markdown is the human-review surface; the JSON files are the machine contract.
{{/* SharedContext slot — populated in Phase 20 (CACHE-02/03) */}}
TaskUID: {{.TaskUID}}
```

**Phase 20 replacement (apply identically to all four planner templates):**
```
Markdown is the human-review surface; the JSON files are the machine contract.
{{- if .SharedContext}}
{{.SharedContext}}
{{end -}}
TaskUID: {{.TaskUID}}
```

The `{{- if .SharedContext}}` form produces no bytes when `SharedContext=""` — preserving existing golden byte-identity for the eval fixture which does not set SharedContext. The ratchet must be updated deliberately when SharedContext content is added to the fixture.

**`task_executor.tmpl` — slot marker at line 24 must NOT be interpolated.** Leave the comment marker or remove it. Do NOT add `{{.SharedContext}}` (CACHE-02 lock).

**Post-template-change discipline (established Phase 18/19 pattern):**
```bash
# Regenerate goldens after template change:
go test ./internal/eval/ -update -run TestGoldenRender
# Verify ratchet (will fail if byte count changed):
go test ./internal/eval/ -run TestByteRatchet
# Update ratchet manually in testdata/ratchets/<name>.txt, then confirm:
go test ./internal/eval/
```
Current ratchet values (from Phase 19): milestone=1862, phase=1974, plan=3985, project=2193, task=1566. These remain unchanged when `SharedContext=""` (the fixture default), because `{{- if .SharedContext}}...{{end -}}` emits nothing.

---

### `cmd/tide-spike/main.go` (utility, request-response) — cache probe harness

**Analog:** `cmd/tide-eval/main.go` — exact structural match. The spike replaces `count_tokens` HTTP POST calls with `claude -p --bare` exec calls, but the credential plumbing, build tag, flag parsing, and credproxy wiring are identical.

**Build tag pattern** (`cmd/tide-eval/main.go:1`):
```go
//go:build eval
```
Spike uses:
```go
//go:build spike
```

**Flag + credential pattern** (`cmd/tide-eval/main.go:121–125`):
```go
var (
    proxyEndpoint = flag.String("proxy", os.Getenv("TIDE_PROXY_ENDPOINT"), "credproxy base URL (e.g. https://127.0.0.1:8443)")
    signedToken   = flag.String("token", os.Getenv("TIDE_SIGNED_TOKEN"), "HMAC signed token for credproxy")
    model         = flag.String("model", "claude-sonnet-4-6", "model for token counting")
)
```

**HTTP client pattern** (`cmd/tide-eval/main.go:142`):
```go
client := &http.Client{Timeout: 30 * time.Second}
```

**Required header pattern** (`cmd/tide-eval/main.go:203–205`):
```go
req.Header.Set("content-type", "application/json")
req.Header.Set("x-api-key", token)
req.Header.Set("anthropic-version", "2023-06-01")
```

**`requireFlag` fail-closed pattern** (`cmd/tide-eval/main.go:232–238`):
```go
func requireFlag(name, val string) string {
    if val == "" {
        fmt.Fprintf(os.Stderr, "tide-eval: required flag/env -%s not set\n", name)
        os.Exit(1)
    }
    return val
}
```

**Spike-specific: `claude -p --bare` exec pattern** (from `internal/subagent/anthropic/subagent.go:285–305`):
```go
args := []string{
    "-p",
    "--model", modelName,
    "--output-format", "stream-json",
    "--verbose",
    "--include-partial-messages",
    "--permission-mode", "acceptEdits",
    "--add-dir", eventsDir,  // per-dispatch, use different UIDs to simulate two pods
    "--bare",
}
cmd := exec.CommandContext(ctx, claudeBinary, args...)
cmd.Stdin = strings.NewReader(probePrompt)
cmd.Env = append(os.Environ(),
    "ANTHROPIC_BASE_URL="+proxyEndpoint,
    "ANTHROPIC_API_KEY="+signedToken,
    "NODE_EXTRA_CA_CERTS="+nodeExtraCACertsPath,
)
// Note: cmd.Dir is NOT set (matches production behavior — no cmd.Dir in subagent.go)
```

**Spike verdict reading pattern** (`internal/subagent/anthropic/stream_parser.go` — already parses cache fields):
```go
// ParseStream already maps:
//   cache_read_input_tokens     → Usage.CacheReadTokens
//   cache_creation_input_tokens → Usage.CacheCreationTokens
usage, _, err := ParseStream(stdout, eventsFile)
// Spike verdict:
//   dispatch #1: usage.CacheCreationTokens > 0  → cache write confirmed
//   dispatch #2: usage.CacheReadTokens > 0       → PASS (cross-pod hit confirmed)
//              : usage.CacheReadTokens == 0       → FAIL (need body diff)
```

**Makefile target pattern** (modeled on `eval` target at `Makefile:205–220`):
```makefile
.PHONY: spike
spike: ## cross-pod cache prefix spike (online, requires TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN; dispatches two real claude -p --bare calls, reports cache hit verdict).
    @if [ -z "$$TIDE_PROXY_ENDPOINT" ]; then \
        echo "ERROR: TIDE_PROXY_ENDPOINT env not set"; exit 1; \
    fi
    @if [ -z "$$TIDE_SIGNED_TOKEN" ]; then \
        echo "ERROR: TIDE_SIGNED_TOKEN env not set"; exit 1; \
    fi
    go run -tags spike ./cmd/tide-spike/ \
        -proxy "$(TIDE_PROXY_ENDPOINT)" \
        -token "$(TIDE_SIGNED_TOKEN)" \
        -model "$(or $(EVAL_MODEL),claude-sonnet-4-6)"
```

---

## Shared Patterns

### Additive `omitempty` field (planner-only complement to executor-only `PromptPath`/`Branch`)
**Source:** `pkg/dispatch/envelope.go:75–83` (`PromptPath`, `Branch`)
**Apply to:** `EnvelopeIn.SharedContext`, `EnvelopeOut.SharedContext`, `ChildCRDSpec.SharedContext`, and all four CRD Spec types
- Use `json:"sharedContext,omitempty"` — zero-value `""` suppresses the field in JSON
- No CEL validation (`+optional` kubebuilder marker only) — the field is unconstrained plain text
- No `+kubebuilder:validation:MinLength` — empty is valid (planner can emit no SharedContext)

### CRD Spec carry-field stamp at materialization
**Source:** `internal/reporter/materialize.go:233` (`tk.Spec.PromptPath = child.SourcePath`)
**Apply to:** `MaterializeChildCRDs` Task/Phase/Plan/Milestone branches — add `*.Spec.SharedContext = child.SharedContext` after each `json.Unmarshal` call, mirroring the SourcePath stamp pattern exactly.

### Fail-closed credential check
**Source:** `cmd/tide-eval/main.go:232–238` (`requireFlag`)
**Apply to:** `cmd/tide-spike/main.go` — copy `requireFlag` verbatim; never log the token value (T-18-03-01).

### Golden + ratchet update discipline
**Source:** `internal/eval/render_test.go:114–126` (`goldenAssert`), `201–234` (`ratchetAssert`)
**Apply to:** Every template commit — goldens regenerated with `-update`, ratchet `.txt` file updated manually in same commit. Both steps in the same commit as the template change (Phase 18/19 established protocol).

### credproxy env wiring (never raw key)
**Source:** `internal/subagent/anthropic/subagent.go:301–305`
**Apply to:** `cmd/tide-spike/main.go` — `ANTHROPIC_BASE_URL` + `ANTHROPIC_API_KEY` (signed token only) + `NODE_EXTRA_CA_CERTS`. Raw key stays in credproxy sidecar.

---

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `internal/credproxy/server.go` (FAIL-path tee flag) | middleware | request-response | No existing body-tee feature; check log depth at implementation (A2 assumption). If DEBUG logging already captures request bodies, no new flag needed. Otherwise a `--tee-body-dir` flag is the addition. Pattern: add a runtime flag (not a build tag) guarding a `io.TeeReader` on the incoming body. No analog exists in the credproxy today; implement from scratch guided by the Open Question in RESEARCH.md. |

---

## Metadata

**Analog search scope:** `pkg/dispatch/`, `api/v1alpha1/`, `internal/controller/`, `internal/subagent/anthropic/`, `internal/subagent/common/templates/`, `internal/eval/`, `internal/reporter/`, `cmd/tide-eval/`, `Makefile`
**Files scanned:** 15 source files read directly
**Pattern extraction date:** 2026-06-15
