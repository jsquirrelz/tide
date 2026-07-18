# Phase 42: Trace-Context Foundation + Planner-Level Span Emission - Pattern Map

**Mapped:** 2026-07-15
**Files analyzed:** 12 (3 new, 9 modified — 4 controllers, 4 CRD status types, 3 pkg/otelai files collapse to attrs.go+attrs_test.go+doc.go)
**Analogs found:** 12 / 12

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `pkg/otelai/tracecontext.go` (NEW) | utility | transform | `pkg/otelai/attrs.go` | exact (same package, same pure-function shape) |
| `pkg/otelai/tracecontext_test.go` (NEW) | test | transform | `pkg/otelai/attrs_test.go` | exact |
| `internal/controller/span_emission_test.go` (NEW) | test | event-driven | `internal/controller/child_rollup_idempotency_test.go` | exact (identical direct-call envtest shape) |
| `pkg/otelai/attrs.go` (MODIFY) | utility | transform | self (existing helpers being swapped in place) | exact |
| `pkg/otelai/attrs_test.go` (MODIFY) | test | transform | self | exact |
| `pkg/otelai/doc.go` (MODIFY) | utility (doc) | transform | self | exact |
| `internal/controller/milestone_controller.go` — `handleJobCompletion` (MODIFY, line 501) | controller | event-driven | self, lines 567-610 (`MilestoneRolledUpUID` marker block) + `resolveAgentIdentity` 2nd-call precedent | role-match, load-bearing precedent is in-file |
| `internal/controller/phase_controller.go` — `handleJobCompletion` (MODIFY, line 454) | controller | event-driven | `internal/controller/milestone_controller.go` (twin handler) | exact (near-identical structure) |
| `internal/controller/plan_controller.go` — `handlePlannerJobCompletion` (MODIFY, line 488) | controller | event-driven | `internal/controller/milestone_controller.go` + self lines 570-611 (`PlanRolledUpUID`) | exact |
| `internal/controller/project_controller.go` — `handleProjectJobCompletion` (MODIFY, line 1779) | controller | event-driven | self, lines 1847-1870+ (`Status.Budget.PlannerRolledUpUID` marker block) | role-match (Budget-nested marker is project-specific) |
| `api/v1alpha3/{milestone,phase,plan}_types.go` — new span-emitted marker field (MODIFY) | model | CRUD | same file's existing `*RolledUpUID` field | exact (mechanical mirror) |
| `api/v1alpha3/project_types.go` — new span-emitted marker field (MODIFY) | model | CRUD | same file's `BudgetStatus.PlannerRolledUpUID` (line 323-327) | exact |
| `go.mod` / `go.sum` (MODIFY) | config | N/A | N/A — dependency addition, no code pattern | n/a |

## Pattern Assignments

### `pkg/otelai/tracecontext.go` (NEW — utility, transform)

**Analog:** `pkg/otelai/attrs.go` (same package — sibling file, not cross-package)

**File header + package doc convention** (`pkg/otelai/attrs.go` lines 1-23):
```go
/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
...
*/

package otelai

import (
	"strconv"

	"go.opentelemetry.io/otel/attribute"
)
```
`tracecontext.go` needs the same header, `package otelai`, and a comment banner citing the requirement/decision IDs it implements (D-05/ATTR-03 style — see `keyLLMInputMessagesPrefix` block below for the convention of one comment block per const group, with a `// Reference: <url>` line).

**Pure-function doc-comment convention** (`pkg/otelai/attrs.go` lines 109-127, `TokenCount`):
```go
// TokenCount returns the four token-accounting attributes per the
// OpenInference spec's `llm.token_count.*` family. The four are:
//
//   - llm.token_count.prompt                          — uncached prompt tokens
//   - llm.token_count.completion                       — completion tokens
//   - llm.token_count.prompt_details.cache_read        — Anthropic cache HITS
//   - llm.token_count.prompt_details.cache_write       — Anthropic cache MISSES
//
// `llm.token_count.total` is INTENTIONALLY OMITTED — consumers can sum the
// four parts. ...
func TokenCount(prompt, completion, cacheRead, cacheWrite int) []attribute.KeyValue {
```
**D-08 reverses this doc comment exactly** — the modified `TokenCount` must drop the "INTENTIONALLY OMITTED" framing and instead document the `total = prompt + completion` formula (prompt already carries cache_read/cache_write as subsets per the re-mapped call-site convention in Pattern Assignments below).

`TraceIDFromUID`, `FormatTraceparent`, `ExtractRemoteParent` should each get a doc comment in this same style: one-line summary, then a `//   - key = value` bullet list or worked example, ending with a `// Reference:` or `// Source:` citation line matching `attrs.go`'s `// Reference: https://github.com/Arize-ai/openinference/...` convention (line 43).

**No K8s imports** — confirm `tracecontext.go` stays as dependency-light as `attrs.go` (only stdlib + `go.opentelemetry.io/otel/...` packages — never `sigs.k8s.io/controller-runtime`, never `k8s.io/api/...`). This is what RESEARCH.md's Architecture Pattern explicitly calls out ("pure, zero K8s deps — build first").

---

### `pkg/otelai/tracecontext_test.go` (NEW — test, transform)

**Analog:** `pkg/otelai/attrs_test.go`

**Table-driven / exact-value assertion convention** (`pkg/otelai/attrs_test.go` lines 37-54, `TestLLMInputMessages`):
```go
func TestLLMInputMessages(t *testing.T) {
	got := LLMInputMessages([]Message{
		{Role: "user", Content: "hi"},
		{Role: "assistant", Content: "hello"},
	})
	want := []attribute.KeyValue{
		attribute.String("llm.input_messages.0.message.role", "user"),
		attribute.String("llm.input_messages.0.message.content", "hi"),
		attribute.String("llm.input_messages.1.message.role", "assistant"),
		attribute.String("llm.input_messages.1.message.content", "hello"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("LLMInputMessages = %v, want %v", got, want)
	}
	if len(got) != 4 {
		t.Errorf("LLMInputMessages returned %d entries, want exactly 4", len(got))
	}
}
```
Plain `testing.T` (not Ginkgo) — pure functions never need envtest. Mirror this exact-value `reflect.DeepEqual` style for `TraceIDFromUID`/`FormatTraceparent`/`ExtractRemoteParent` (e.g. assert the deterministic hex TraceID for a fixed UID string, assert the exact `traceparent` header string for a fixed trace/span ID pair, assert round-trip `Format → Extract`).

**Nil/empty-input defensive test convention** (`pkg/otelai/attrs_test.go` lines 148-163, `TestEmptyInputsNoPanic`):
```go
func TestEmptyInputsNoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("LLMInputMessages(nil) panicked: %v", r)
		}
	}()
	if got := LLMInputMessages(nil); len(got) != 0 {
		t.Errorf("LLMInputMessages(nil) returned %d entries, want 0", len(got))
	}
	...
}
```
Apply the same "no panic on zero-value/malformed input" discipline to `ExtractRemoteParent` on a malformed `traceparent` string and to `TraceIDFromUID` on an empty UID.

**Repo-root-walking helper for source-grep guard tests** (`pkg/otelai/attrs_test.go` lines 165-185, `findRepoRoot`) — reuse directly if a guard test needs to read `tracecontext.go` source (e.g. asserting no `k8s.io/` import appears, mirroring the `internal/otelinit` `TestNoWithSamplerInSource` guard-test family — see Shared Patterns).

---

### `internal/controller/span_emission_test.go` (NEW — test, event-driven)

**Analog:** `internal/controller/child_rollup_idempotency_test.go` (full file read; this is the exact shape RESEARCH.md names as the precedent)

**Direct-call envtest shape, bypassing full `Reconcile()`** (`child_rollup_idempotency_test.go` lines 106-154):
```go
It("ADOPT-02+04: milestone rollup accrues on first call and is idempotent on second (TTL-GC simulation)", func() {
	ms := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: crMSName, Namespace: "default"},
		Spec:       tideprojectv1alpha3.MilestoneSpec{ProjectRef: crMSProjName},
	}
	Expect(k8sClient.Create(ctx, ms)).To(Succeed())
	waitForCacheSync(crMSName, "default", &tideprojectv1alpha3.Milestone{})
	Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, ms)).To(Succeed())

	statusPatch := client.MergeFrom(ms.DeepCopy())
	ms.Status.Phase = "Running"
	Expect(k8sClient.Status().Patch(ctx, ms, statusPatch)).To(Succeed())
	...

	envReader.SetOut(string(ms.UID), pkgdispatch.EnvelopeOut{
		TaskUID:    string(ms.UID),
		ExitCode:   0,
		ChildCount: 0,
		Usage: pkgdispatch.Usage{
			InputTokens:        inputTokens,
			OutputTokens:       300,
			EstimatedCostCents: costCents,
		},
	})

	r := &MilestoneReconciler{
		Client: mgrClient,
		Scheme: k8sClient.Scheme(),
		Deps: PlannerReconcilerDeps{
			Dispatcher:     &stubDispatcher{},
			EnvReader:      envReader,
			CredproxyImage: testCredproxyImage,
			SigningKey:     testSigningKey,
			HelmProviderDefaults: ProviderDefaults{Image: testSubagentImage},
		},
		PlannerPool: newPlannerPoolForTest(),
		// ReporterImage deliberately empty: spawnReporterIfNeeded returns
		// (true, nil) → isFirstCompletion=true on every call without a PVC.
	}

	// First call: accrual.
	_, err := r.handleJobCompletion(ctx, ms, nil)
	Expect(err).NotTo(HaveOccurred())

	expectedJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
	Eventually(func(g Gomega) {
		var fresh tideprojectv1alpha3.Milestone
		g.Expect(mgrClient.Get(ctx, types.NamespacedName{Name: crMSName, Namespace: "default"}, &fresh)).To(Succeed())
		g.Expect(fresh.Status.MilestoneRolledUpUID).To(Equal(expectedJobName), "...")
	}, 5*time.Second, 100*time.Millisecond).Should(Succeed())

	// Second call: idempotency — marker prevents a repeat side effect.
	_, err = r.handleJobCompletion(ctx, ms, nil)
	Expect(err).NotTo(HaveOccurred())
	...
})
```
Copy this shape exactly for span-emission specs, swapping the assertion from `Project.Status.Budget.CostSpentCents` to an in-memory span exporter's recorded spans:
1. `Describe("SpanEmission — <Level> level", Label("envtest", "heavy"), func() { ... })` per-level blocks, mirroring `child_rollup_idempotency_test.go`'s four `Describe` blocks (Milestone at line 81, Phase at line 198, and Plan/Project further down the same file).
2. Set the global TracerProvider to `sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))` where `exp := tracetest.NewInMemoryExporter()`, in `BeforeEach`, and restore/reset in `AfterEach` — this is new plumbing RESEARCH.md's Wave-0-Gaps section calls out explicitly (no existing analog for the exporter wiring itself; the *test structure* around it is 1:1 `child_rollup_idempotency_test.go`).
3. Call `r.handleJobCompletion(ctx, ms, completedJob)` directly (or the level-appropriate `handlePlannerJobCompletion`/`handleProjectJobCompletion`), not a full reconcile loop — same as the analog.
4. Assert `len(exp.GetSpans()) == 1` after the first call, `== 1` still (not 2) after a second call with the same `completedJob` (idempotency, mirrors `Consistently(...CostSpentCents...)` at line 187-192) — this is the D-02/Pitfall-2 test the phase requires.
5. A `completedJob == nil` regression case per level: `r.handleJobCompletion(ctx, ms, nil)` (exact call already exercised at line 153 of the analog) — assert **zero** spans recorded (Pattern 3 finding — RESEARCH.md explicitly flags this as a required Wave-0 test).
6. A Failed-Job case per level: build a `*batchv1.Job` with a `JobFailed` condition and `CompletionTime == nil`, assert the recorded span's end timestamp derives from the condition's `LastTransitionTime` (Pitfall 1), not a panic or zero-value.

**Supporting stubs already available** (no need to re-invent): `mapEnvReader`/`newMapEnvReader()`, `stubDispatcher{}`, `newPlannerPoolForTest()`, `waitForCacheSync(name, ns, obj)`, `testCredproxyImage`/`testSigningKey`/`testSubagentImage` constants — all referenced in the analog and already defined elsewhere in the `internal/controller` test package (`suite_test.go` / sibling test-helper files).

---

### `internal/controller/{milestone,phase,plan,project}_controller.go` — completion handlers (MODIFY, controller, event-driven)

**Analog (primary, in-file):** the existing `*RolledUpUID` marker-gated budget-rollup block inside each of these same four functions. This is the load-bearing precedent RESEARCH.md's Pattern 2 identifies — span-emission idempotency must copy this exact "durable marker in `.status`, stamped only after the guarded side effect succeeds" shape, NOT reuse the existing marker (it is `envReadOK`-gated; span emission must not be).

**Marker-gated once-per-Job-attempt side effect** (`internal/controller/milestone_controller.go` lines 567-610):
```go
// Plan 09-08 Defect C: roll up planner-level Usage to Project.Status.Budget
// exactly once per planner Job completion (guarded by isFirstCompletion).
//
// Phase 31 D-03 / T-31-07: isFirstCompletion flips true again after the reporter
// Job's 300s TTL-GC window, causing double-count on halt→resume. Gate on the
// durable MilestoneRolledUpUID marker (lives in CRD .status, survives restart)
// to guarantee exactly-once rollup regardless of TTL-GC (ADOPT-04).
milestoneJobName := fmt.Sprintf("tide-milestone-%s-1", ms.UID)
if isFirstCompletion && envReadOK && project != nil {
	if ms.Status.MilestoneRolledUpUID != milestoneJobName {
		if rollErr := budget.RollUpUsage(ctx, r.Client, project, out.Usage); rollErr != nil {
			logger.Error(rollErr, "milestone planner budget rollup failed (non-fatal)", "milestone", ms.Name)
		} else {
			// Stamp the durable marker only after a successful rollup (mirrors project-level
			// Pitfall-2 ordering: leaving the marker unset on error lets the next reconcile retry).
			if mErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
				latest := &tideprojectv1alpha3.Milestone{}
				if err := r.Get(ctx, client.ObjectKeyFromObject(ms), latest); err != nil {
					return err
				}
				if latest.Status.MilestoneRolledUpUID == milestoneJobName {
					return nil // already set by a concurrent reconcile — idempotent
				}
				markerPatch := client.MergeFromWithOptions(latest.DeepCopy(), client.MergeFromWithOptimisticLock{})
				latest.Status.MilestoneRolledUpUID = milestoneJobName
				return r.Status().Patch(ctx, latest, markerPatch)
			}); mErr != nil {
				return ctrl.Result{}, fmt.Errorf("patch MilestoneRolledUpUID: %w", mErr)
			}
		}
	}
}
```
**Span emission must copy this exact skeleton but with two deliberate deviations required by D-04**:
1. Gate on `completedJob != nil` only (NOT `envReadOK`, NOT `isFirstCompletion` — span emission fires on every Job attempt observation regardless of reporter-spawn state).
2. Stamp the new marker field (e.g. `ms.Status.MilestoneSpanEmittedUID`) unconditionally after span synthesis succeeds, independent of whether the envelope was readable.

**Model + provider re-resolution at completion time — second-call precedent** (`internal/controller/plan_controller.go` line 424, dispatch-time call; `internal/controller/boundary_push.go` line 169/245 and `internal/controller/artifact_push.go` line 229, completion-time second calls of the sibling nil-safe resolver `resolveAgentIdentity`):
```go
// dispatch time (plan_controller.go:424):
agentName, agentEmail := resolveAgentIdentity(project, r.Deps.HelmProviderDefaults)
...
// completion time, a different call site in the same file family (boundary_push.go:169):
agentName, agentEmail := resolveAgentIdentity(project, helmDefaults)
```
Use the identical shape for `ResolveProvider` at completion time (`internal/controller/dispatch_helpers.go:263`, already read in full):
```go
provider := ResolveProvider(project, "milestone", r.Deps.HelmProviderDefaults)
if provider.Model != "" {
	span.SetAttributes(attribute.String(semconv.LLMModelName, provider.Model))
}
span.SetAttributes(
	attribute.String(semconv.LLMProvider, provider.Vendor),
	attribute.String(semconv.LLMSystem, provider.Vendor),
)
```
`ResolveProvider(nil, level, helmDefaults)` is nil-safe (verified in full at `dispatch_helpers.go:263-310`) — safe to call even when `project == nil` on the degraded path.

**Job-outcome branching helpers to reuse directly, unmodified** (`internal/controller/project_controller.go` lines 2136-2154):
```go
// isJobSucceeded returns true if the Job has a Complete condition with ConditionTrue.
func isJobSucceeded(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

// isJobFailed returns true if the Job has a Failed condition with ConditionTrue.
func isJobFailed(job *batchv1.Job) bool {
	for _, c := range job.Status.Conditions {
		if c.Type == batchv1.JobFailed && c.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}
```
These are package-level (unexported) helpers already visible to all four controller files in the `controller` package — call directly, do not duplicate. Combine with the `spanEndTime` fallback RESEARCH.md's Code Examples section already worked out (success → `completedJob.Status.CompletionTime.Time`; failure → the `JobFailed` condition's `LastTransitionTime.Time`, since `CompletionTime` is `nil` on every failed Job per the K8s API's own doc comment).

**Envelope-degraded-read discipline (`envReadOK` two-phase pattern) — reuse the existing branch verbatim as the "is the envelope usable" gate** (`internal/controller/plan_controller.go` lines 504-536, `internal/controller/milestone_controller.go` lines 525-551) — span emission attaches `ExitCode`/`Reason` attributes ONLY inside the existing `if envReadOK { ... }` branches already present in each handler; it does not introduce a new envelope-read call.

**Failure-path span status (D-01/D-03), grounded in the reused helpers above:**
```go
if isJobFailed(completedJob) {
	desc := out.Reason // "forced-failure" | "cap-hit" | "output-path-violation" |
	                    // "token-expired" | "claude exit N: <stderr>" | ""
	span.SetStatus(codes.Error, desc)
	if envReadOK {
		span.SetAttributes(
			attribute.Int(tideExitCodeKey, out.ExitCode), // tide.* — no module key exists
			attribute.String(tideReasonKey, out.Reason),
		)
	}
}
```

**Tracer acquisition — zero new Deps plumbing required** (`cmd/manager/main.go` lines 266-273, already wires `otel.SetTracerProvider` globally):
```go
tracer := otel.Tracer("tide.dispatch")
ctx, span := tracer.Start(ctx, "tide.dispatch.milestone", trace.WithTimestamp(startTime))
...
span.End(trace.WithTimestamp(endTime)) // explicit End() per branch — do NOT defer with a
                                        // pre-branch-computed timestamp, since endTime differs
                                        // per success/failure/skip outcome
```
No `Deps` struct field addition needed — `otel.Tracer(...)` resolves the process-global TracerProvider set once at manager startup (`cmd/manager/main.go:273`), exactly the same mechanism the four reconcilers already rely on implicitly (none of them currently call it, confirmed zero production call sites via `grep -rn "otel.Tracer("`).

**Per-controller notes:**
- `milestone_controller.go` (line 501) and `phase_controller.go` (line 454) are near-identical twins — build the milestone version first, then port 1:1 to phase.
- `plan_controller.go` (line 488) has the `plan_controller.go:427-428` dispatch-time log line CONTEXT.md's D-04 cites verbatim ("D-02 / T-40-12: log the resolved model at dispatch — previously the resolved model appeared nowhere outside the PVC envelope") — this confirms `ResolveProvider` re-invocation at completion is the correct fix, not a new envelope field.
- `project_controller.go` (line 1779) is the outlier: its rollup marker lives at `project.Status.Budget.PlannerRolledUpUID` (nested under `BudgetStatus`, not directly on `.Status` like the other three — see `api/v1alpha3/project_types.go:323-327`), and it has an extra `project.Spec.ImportSource != nil` suppression branch (lines 1858-1861) that has no equivalent at the other three levels. The new span-emitted marker for Project should follow whichever placement (`.Status.SpanEmittedUID` vs `.Status.Budget.SpanEmittedUID`) the planner judges more consistent — RESEARCH.md leaves this as an explicit open naming/placement question, not a design fork.

---

### `api/v1alpha3/{milestone,phase,plan,project}_types.go` — new marker field (MODIFY, model, CRUD)

**Analog:** the file's own existing `*RolledUpUID` field.

**Field definition + doc-comment convention** (`api/v1alpha3/milestone_types.go` lines 61-67):
```go
// MilestoneRolledUpUID is the name of this Milestone's planner Job whose Usage
// was successfully rolled up into the Project budget. Prevents double-counting
// when the reporter Job has TTL-GC'd before a reconcile re-observes it.
// Mirrors the project-level budget rollup marker at the Milestone level
// per the D-03 level-specific marker pattern. Phase 31 ADOPT-04 / D-03.
// +optional
MilestoneRolledUpUID string `json:"milestoneRolledUpUID,omitempty"`
```
Same shape at `api/v1alpha3/phase_types.go:57-63` (`PhaseRolledUpUID`) and `api/v1alpha3/plan_types.go:113-119` (`PlanRolledUpUID`). Mirror this exactly for the new field — plain `string` scalar, `+optional` marker, `omitempty` JSON tag, doc comment naming the phase/decision that introduced it (e.g. "Phase 42 D-02/Pitfall-2: gates one-span-per-Job-attempt emission, independent of envReadOK").

**Project's alternate placement — nested under a status sub-struct** (`api/v1alpha3/project_types.go` lines 323-327, inside `BudgetStatus`):
```go
// PlannerRolledUpUID is the name of the most recent planner Job whose Usage
// was successfully rolled up into CostSpentCents. Prevents double-counting
// when the reporter Job has TTL-GC'd during a halt→resume cycle (BYPASS-03 / Phase 27).
// +optional
PlannerRolledUpUID string `json:"plannerRolledUpUID,omitempty"`
```
Note the `MilestoneStatus` doc comment's explicit constraint (`api/v1alpha3/milestone_types.go:51`, `// PERSIST-02 enforced: NO aggregate fields.`) — the new marker is a single scalar, not an aggregate, so it satisfies this constraint at all four levels regardless of which placement is chosen.

**Consequence (generated artifact, no hand-pattern needed):** adding this field requires `make manifests` (controller-gen) to regenerate `config/crd/bases/*.yaml` — do not hand-edit the CRD YAML; regenerate and diff.

## Shared Patterns

### Durable, once-per-attempt idempotency marker (state-transition-edge gating)
**Source:** `internal/controller/milestone_controller.go:567-610`, `plan_controller.go:570-611`, `project_controller.go:1847-1870`
**Apply to:** All four completion-handler modifications (Pattern Assignment above); this is THE load-bearing shared pattern for the whole phase per D-02/D-04/Pitfall 2. Key rule: **do not reuse `*RolledUpUID`** for span gating — it is `envReadOK`-gated and a degraded envelope would cause infinite re-emission. Add a dedicated, `envReadOK`-independent marker per level.

### Second-call reuse of a nil-safe, pure resolver function
**Source:** `internal/controller/dispatch_helpers.go:263` (`ResolveProvider`) and `:437` (`resolveAgentIdentity`); second-call sites at `boundary_push.go:169,245` and `artifact_push.go:229`
**Apply to:** Model + provider attribute resolution (ATTR-01/D-04/D-07) in all four completion handlers — call `ResolveProvider(project, level, r.Deps.HelmProviderDefaults)` a second time at completion; never add a new envelope field for this.

### Job terminal-state branching (`isJobSucceeded`/`isJobFailed` + `CompletionTime` vs `JobFailed.LastTransitionTime`)
**Source:** `internal/controller/project_controller.go:2136-2154` (helpers, package-visible to all four controllers)
**Apply to:** Span end-timestamp resolution and span status (`codes.Ok`/`codes.Error`) in all four handlers. `CompletionTime` is `nil` on every failed Job — always branch through the `JobFailed` condition's `LastTransitionTime` on the failure path.

### `envReadOK` two-phase degraded-envelope handling
**Source:** `internal/controller/plan_controller.go:504-536`, `milestone_controller.go:525-551` (near-identical across all four)
**Apply to:** Gating `ExitCode`/`Reason` span attributes (D-03) — attach only inside the existing `if envReadOK { ... }` branch already present in each handler; degraded spans (D-04) still emit outside that branch, just without those two attributes.

### Source-grep guard test convention
**Source:** `internal/otelinit/provider_test.go:113-124` (`TestNoWithSamplerInSource`) and `pkg/otelai/attrs_test.go:123-146` (`TestNoPayloadHelperOnPublicSurface`)
```go
func TestNoWithSamplerInSource(t *testing.T) {
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "internal", "otelinit", "provider.go"))
	if err != nil {
		t.Fatalf("read internal/otelinit/provider.go: %v", err)
	}
	stripped := stripGoComments(string(data))
	if strings.Contains(stripped, "WithSampler(") {
		t.Errorf("Pitfall 24 violation: ...")
	}
}
```
**Apply to:** ATTR-03's key-sourcing guard test (RESEARCH.md's Test Map names this `TestKeysUseSemconvModule`) — source-grep `attrs.go` for the OLD hand-rolled string literals (e.g. `"llm.token_count.total"`) and fail if found outside a comment, forcing all payload-bearing keys through `semconv.*` constants. Reuse `findRepoRoot`/`stripGoComments` verbatim (already defined in `pkg/otelai/attrs_test.go` and `internal/otelinit/provider_test.go` respectively — pick whichever helper set lives closer to the new test file, or promote to a shared testutil if both packages need it). **Do NOT add a module-version drift-guard test** — D-06 explicitly declines this.

### `.status` marker stamping dance (RetryOnConflict + optimistic-lock merge patch)
**Source:** `internal/controller/milestone_controller.go:588-602` (full block shown in Pattern Assignments above)
**Apply to:** Stamping the new span-emitted marker field — reuse `retry.RetryOnConflict(retry.DefaultRetry, func() error { ... re-fetch latest, check-then-set, client.MergeFromWithOptions(..., client.MergeFromWithOptimisticLock{}) ... })` verbatim, swapping only the field name and parent type.

## No Analog Found

None. Every file in the expected surface has a strong, directly-cited in-repo analog — this phase is explicitly scoped (per RESEARCH.md) as composition on top of already-established controller and `pkg/otelai` conventions, with zero genuinely novel architectural shapes. The two items with no *prior* precedent in the repo are noted inline above rather than listed here because they compose known primitives:
- The in-memory span exporter wiring (`tracetest.NewInMemoryExporter()` + `sdktrace.WithSyncer`) inside `span_emission_test.go` — the SDK helper itself is a pinned, off-the-shelf dependency (zero new go.mod entries per RESEARCH.md's Standard Stack), only its *use* inside this repo's test package is new; the surrounding test structure is a 1:1 copy of `child_rollup_idempotency_test.go`.
- The `openinference-semantic-conventions` Go module import in `attrs.go` — a new go.mod dependency, not a code pattern; see RESEARCH.md's Package Legitimacy Audit for the verification trail (`checkpoint:human-verify` recommended before `go get`).

## Metadata

**Analog search scope:** `pkg/otelai/`, `internal/otelinit/`, `internal/controller/` (all four planner-level completion handlers + `dispatch_helpers.go` + `boundary_push.go`/`artifact_push.go` + `child_rollup_idempotency_test.go`), `api/v1alpha3/` (all four `*_types.go` status structs), `cmd/manager/main.go`
**Files scanned:** 15 (full or targeted reads): `pkg/otelai/{attrs,attrs_test,doc}.go`, `internal/otelinit/{provider,provider_test}.go`, `internal/controller/{milestone,phase,plan,project}_controller.go` (completion-handler bodies), `internal/controller/dispatch_helpers.go`, `internal/controller/child_rollup_idempotency_test.go`, `internal/controller/dispatch_helpers_test.go` (resolveAgentIdentity tests), `api/v1alpha3/{milestone,phase,plan,project}_types.go` (status structs), `pkg/dispatch/envelope.go` (`Usage`/`EnvelopeOut` shape), `cmd/manager/main.go` (OTel bootstrap)
**Pattern extraction date:** 2026-07-15
