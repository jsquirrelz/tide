# Phase 45: Runtime-Neutral Adapter Seam - Pattern Map

**Mapped:** 2026-07-16
**Files analyzed:** 12 (3 new, 9 modified)
**Analogs found:** 12 / 12 (all in-repo; several are *self-analogs* — the file already contains the exact precedent pattern to extend, one file over)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|--------------------|------|-----------|-----------------|----------------|
| `pkg/dispatch/vendor_capabilities.go` (NEW) | utility (data lookup table) | transform | `pkg/dispatch/provider.go` | role-match (same package, same "pure data, no I/O" shape) |
| `pkg/dispatch/vendor_capabilities_test.go` (NEW) | test | transform | `pkg/dispatch/provider_test.go` | exact (plain-func `testing` style, no testify) |
| `cmd/tide-reporter/adapter_seam_test.go` (NEW) | test (contract test) | event-driven (span emission) | `cmd/tide-reporter/main_test.go` (`installStubTracerProvider`, `writeTraceOnlyFixture`, `TestRunTraceOnly_EmitsSpans`) | exact (same package `main`, reuses the exact helpers unmodified) |
| `internal/controller/reporter_jobspec.go` | config/builder (K8s Job spec builder) | transform | itself — `TraceParent` field + Args-append block | exact (self-analog: extend the existing convention in the same file) |
| `internal/controller/reporter_jobspec_test.go` | test | transform | itself — `TestBuildReporterJob_TraceparentArg` | exact (self-analog, mirror the two-subtest present/absent shape) |
| `internal/controller/dispatch_helpers.go` (`spawnReporterIfNeeded`) | service/helper (spawn orchestration, K8s Get/Create) | CRUD | itself — `ResolveProvider` + the existing `traceParent`/`otlpEndpoint` param-threading in the same function | exact (self-analog) |
| `internal/controller/milestone_controller.go`, `phase_controller.go` (spawn call sites) | controller (reconciler) | event-driven (Job-completion reconcile) | `internal/controller/span_emission.go:176` (`synthesizePlannerSpan`'s fresh `ResolveProvider` call at the same call site) | exact (documented codebase precedent for "recompute, don't thread") |
| `internal/controller/plan_controller.go`, `project_controller.go` (inline spawn) | controller (reconciler) | event-driven | itself — the inline `ReporterOptions{}` literal at the same call site | exact (self-analog; these two levels never call `spawnReporterIfNeeded`) |
| `internal/controller/task_controller.go` (`spawnTaskTraceReporterIfNeeded`) | controller (reconciler helper) | event-driven | itself — the `ReporterOptions{}` literal at line ~1079 | exact (self-analog) |
| `cmd/tide-reporter/main.go` (`parseFlags`, `synthesizeSpans`) | entrypoint / one-shot binary | file-I/O + CLI parsing | itself — `--trace-only` bareword flag (`parseFlags`) + the sentinel-check guard shape (`synthesizeSpans`) | exact (self-analog) |
| `internal/reporter/tracesynth.go` (doc-contract comment only) | service (adapter/parser) | file-I/O | itself — package doc comment (lines 17-30) + the D-07 comment at line 613-616 | exact (self-analog, doc-only change) |
| `cmd/tide-reporter/main_test.go` (additions) | test | file-I/O | itself — `TestParseFlagsTraceparent` (line 336) + `TestRunTraceOnly_EmitsSpans`/`MissingEventsStillExitsZero` | exact (self-analog) |
| `internal/controller/task_traceonly_reporter_test.go` (OPTIONAL, D-11) | test (Ginkgo/envtest) | event-driven | itself — existing spec block asserting Job shape/OTLP-gating | exact (self-analog; only needed if planner adds spawn-helper-level D-11 coverage) |

## Pattern Assignments

### `pkg/dispatch/vendor_capabilities.go` (NEW — utility, transform)

**Analog:** `pkg/dispatch/provider.go` (same package, same "pure data type, doc-comment-heavy, zero imports" convention)

**Full analog file** (`pkg/dispatch/provider.go:17-52`) — package doc + struct shape to match, especially the "Vendor is the provider sentinel string... Canonical values: anthropic, openai, google, xai, opencode" line (line 37-39) that `SelfInstruments`'s vendor vocabulary must key on identically:
```go
package dispatch

// ProviderSpec selects the LLM vendor + model + per-vendor tuning knobs...
type ProviderSpec struct {
	// Vendor is the provider sentinel string the subagent image checks at
	// startup. Canonical values: "anthropic", "openai", "google", "xai",
	// "opencode". Required (no omitempty) — every dispatch declares a vendor.
	Vendor string `json:"vendor"`
	...
}
```

**Core pattern (research-verified, ready to use near-verbatim — D-01/D-03/D-08):**
```go
// vendor_capabilities.go — the ADAPT-01 runtime-neutral adapter seam's routing
// datum. SelfInstruments answers "does this vendor's Subagent implementation
// emit OpenInference spans natively, in-process, during Run()?" — the manager
// consults this at reporter-spawn time (never the reporter itself, which
// trusts only the manager-computed boolean carried on the Job — D-02) to
// decide whether internal/reporter/tracesynth.go's events.jsonl parser
// (the anthropic-CLI runtime's own trace adapter) should run at all.
//
// Default-safe (D-03, Pitfall 7): every current and unrecognized vendor
// returns false. A false "native" assumption silently produces zero spans;
// a false "synthesize" assumption produces (at worst, once a self-
// instrumenting runtime exists) visible duplicates — always fail toward
// visibility, never toward silence.
package dispatch

func SelfInstruments(vendor string) bool {
	switch vendor {
	case "anthropic", "openai", "google", "xai", "opencode":
		return false // CLI/wrapper-shimmed — no in-process OTel SDK
	default:
		return false // fail-closed: unknown vendor never skips synthesis
	}
}
```
Keep the vendor literals identical to `ProviderSpec.Vendor`'s doc comment (`provider.go:37-39`) — this is the "vocabulary this table extends" per RESEARCH's Anti-Pattern warning.

---

### `pkg/dispatch/vendor_capabilities_test.go` (NEW — test, transform)

**Analog:** `pkg/dispatch/provider_test.go` (full file, 98 lines) — plain `testing` style, table-free simple assertions, no testify import, doc comments on every test function explaining the D-number it proves.

**Style to mirror** (`provider_test.go:17-28`):
```go
package dispatch

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProviderSpec_RoundTrip asserts that a fully-populated ProviderSpec
// (vendor + model + params) round-trips through json.Marshal+json.Unmarshal
// without data loss. JSON field tags MUST be `vendor`, `model`,
// `params,omitempty` (D-C3).
func TestProviderSpec_RoundTrip(t *testing.T) {
	...
}
```

**D-10 guard-test shape to author** (mirrors the "explicit unknown/default case" convention already used by `TestProviderSpec_RoundTrip_EmptyVendor`, `provider_test.go:81-97`):
```go
func TestSelfInstruments_KnownVendorsDefaultFalse(t *testing.T) {
	for _, v := range []string{"anthropic", "openai", "google", "xai", "opencode"} {
		if SelfInstruments(v) {
			t.Errorf("SelfInstruments(%q) = true, want false (no self-instrumenting runtime exists yet)", v)
		}
	}
}

func TestSelfInstruments_UnknownVendorDefaultsFalse(t *testing.T) {
	if SelfInstruments("some-future-unregistered-vendor") {
		t.Error("SelfInstruments on an unknown vendor = true, want false (D-03 fail-closed default)")
	}
	if SelfInstruments("") {
		t.Error(`SelfInstruments("") = true, want false`)
	}
}
```

---

### `internal/controller/reporter_jobspec.go` (MODIFIED — builder, transform)

**Analog:** itself — the existing `TraceParent`/`OTLPEndpoint`/`TraceOnly`/`TraceOnlyJobKey` fields on `ReporterOptions` (lines 74-114) are the four precedents for "how data rides Args vs Env." Follow `TraceParent`'s doc-comment shape and the bareword-flag shape of `TraceOnly` (line 105) — not the `=value` shape of `TraceParent`.

**Struct field precedent** (`reporter_jobspec.go:80-105`):
```go
type ReporterOptions struct {
	ReporterImage string

	// TraceParent is the W3C traceparent for the spawning level's OWN
	// just-synthesized span...
	TraceParent string

	// OTLPEndpoint is the manager's own OTEL_EXPORTER_OTLP_ENDPOINT value...
	OTLPEndpoint string

	// TraceOnly selects the Phase 44 trace-only Job shape...
	// Zero value (false) is the existing materialization shape,
	// byte-identical to pre-Phase-44 behavior.
	TraceOnly bool
	...
}
```
Add a new field with the same "zero value is safe" doc-comment discipline, e.g.:
```go
	// SkipMessageSpans (ADAPT-01/D-01..D-05): set true when the manager's
	// pkgdispatch.SelfInstruments(vendor) lookup reports the dispatching
	// vendor emits OpenInference spans natively — the reporter skips
	// tracesynth.go's events.jsonl-based synthesis entirely. Zero value
	// (false) is the existing behavior, byte-identical pre-Phase-45: every
	// vendor resolves false today (D-03 default-safe).
	SkipMessageSpans bool
```

**Args-append pattern to extend** (`reporter_jobspec.go:211-215`, immediately after the existing `TraceParent` block — bareword-flag convention matches `TraceOnly`'s `"--trace-only"` append at line 196-197):
```go
	if opts.TraceParent != "" {
		args = append(args, "--traceparent="+opts.TraceParent)
	}
	if opts.SkipMessageSpans {
		args = append(args, "--skip-message-spans")
	}
```
Both Job shapes (materialization at line 201-210 and trace-only at line 195-200) build the same `args` slice before this block — appending here (after the shape-selecting `if opts.TraceOnly` branch) means the flag composes with either shape uniformly, matching D-05's "disables only the synth step, uniformly for both Job shapes."

---

### `internal/controller/reporter_jobspec_test.go` (MODIFIED — test, transform)

**Analog:** itself — `TestBuildReporterJob_TraceparentArg` (lines 143-194), the exact two-subtest ("present when set" / "absent when empty") shape to mirror for the new bool field (adjust to "present when true" / "absent when false" since this is a bareword not a `=value` flag):
```go
func TestBuildReporterJob_TraceparentArg(t *testing.T) {
	const traceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-c", UID: "project-uid-4"},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-3", Namespace: "ns-c", UID: "parent-uid-4"},
	}
	scheme := newTestScheme()

	t.Run("present when set", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "...", TraceParent: traceParent}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		var found bool
		for _, a := range args {
			if a == "--traceparent="+traceParent {
				found = true
			}
		}
		if !found {
			t.Errorf("expected arg %q not present in %v", "--traceparent="+traceParent, args)
		}
	})

	t.Run("absent when empty", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "..."}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if strings.HasPrefix(a, "--traceparent") {
				t.Errorf("did not expect a --traceparent arg when TraceParent is empty, got %v", args)
			}
		}
	})
}
```
Note: package is `controller_test` (external test package, line 17) — reference the SUT via the imported `controller` package alias (`internal/controller`), and reuse the file-local `newTestScheme()` helper (lines 33-38), same as the existing test. `newTestScheme` and `strings` are already imported (lines 20, 30-31) — no new import needed for a `--skip-message-spans` bareword assertion beyond what `TestBuildReporterJob_TraceparentArg` already uses.

---

### `internal/controller/dispatch_helpers.go` (MODIFIED — `spawnReporterIfNeeded`, CRUD)

**Analog:** itself. `ResolveProvider` (lines 257-318) is the pure, nil-safe function the flag computation calls; `spawnReporterIfNeeded` (lines 93-135) is the function whose signature needs one more bool param (mirrors how `traceParent string, otlpEndpoint string` were already added as trailing params).

**`ResolveProvider` signature + Vendor source** (`dispatch_helpers.go:271-318`, Vendor pinned "anthropic" today):
```go
func ResolveProvider(project *tideprojectv1alpha3.Project, level string, helmDefaults ProviderDefaults) pkgdispatch.ProviderSpec {
	key := levelOverrideKey(level)
	...
	return pkgdispatch.ProviderSpec{
		Vendor: "anthropic",
		Model:  model,
		Params: params,
	}
}
```

**`spawnReporterIfNeeded` current signature and Job-build call** (`dispatch_helpers.go:93-135`) — the trailing-param precedent (`traceParent`, `otlpEndpoint`) to extend with one more bool, and the `ReporterOptions{}` literal to extend with the new field:
```go
func spawnReporterIfNeeded(
	ctx context.Context,
	c client.Client,
	scheme *runtime.Scheme,
	parent metav1.Object,
	project *tideprojectv1alpha3.Project,
	parentKind string,
	reporterImage string,
	pvcName string,
	traceParent string,
	otlpEndpoint string,
) (bool, error) {
	...
	reporterJob := BuildReporterJob(parent, project, pvcName, string(parent.GetUID()), parentKind,
		ReporterOptions{ReporterImage: reporterImage, TraceParent: traceParent, OTLPEndpoint: otlpEndpoint}, scheme)
	...
}
```
Add a `skipMessageSpans bool` trailing param and thread it into the `ReporterOptions{}` literal as `SkipMessageSpans: skipMessageSpans`. Callers (milestone/phase) compute it via `pkgdispatch.SelfInstruments(ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults).Vendor)` immediately before the call — see next section.

**Import already present:** `dispatch_helpers.go:61` already imports `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` — no new import needed in this file.

---

### `internal/controller/{milestone,phase}_controller.go` (MODIFIED — spawn via shared helper)

**Analog:** the codebase's own established "recompute, don't thread a return value" precedent, `internal/controller/span_emission.go:122-125` doc comment + line 176 call, AND the identical-level-literal call already present one statement above the `spawnReporterIfNeeded` call in each of these two files.

**milestone_controller.go — existing neighbor call at line 607 (SAME level literal to reuse) and the spawn call at line 639-640:**
```go
// line 607 (already present, unchanged):
thisSpanID, emitted := synthesizePlannerSpan(ctx, "milestone", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)

// line 639-640 (spawn site to extend):
isFirstCompletion, spawnErr := spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ms, project, "Milestone", r.Deps.ReporterImage, r.sharedPVCName(),
	traceparentForLevel(project, ms.Status.MilestoneTraceSpanID), r.Deps.OTLPEndpoint)
```
**Pattern to add** (one new line immediately before the `spawnReporterIfNeeded` call, reusing the literal `"milestone"` — the SAME literal `synthesizePlannerSpan` already uses at line 607, per RESEARCH Pitfall 2):
```go
skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "milestone", r.Deps.HelmProviderDefaults).Vendor)
isFirstCompletion, spawnErr := spawnReporterIfNeeded(ctx, r.Client, r.Scheme, ms, project, "Milestone", r.Deps.ReporterImage, r.sharedPVCName(),
	traceparentForLevel(project, ms.Status.MilestoneTraceSpanID), r.Deps.OTLPEndpoint, skipMessageSpans)
```
**phase_controller.go** is the exact same shape at `synthesizePlannerSpan(ctx, "phase", ...)` (line 564) and `spawnReporterIfNeeded(...)` (line 592) — swap the literal to `"phase"`.

**Import already present in both files:** verified this session — `milestone_controller.go:53` and `phase_controller.go:53` both already import `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` (used today for `pkgdispatch.Caps{}`/`pkgdispatch.EnvelopeOut`). No import edit needed in either file.

---

### `internal/controller/{plan,project}_controller.go` (MODIFIED — inline spawn, no shared helper)

**Analog:** itself — these two levels build `ReporterOptions{}` INLINE (they do NOT call `spawnReporterIfNeeded`; confirmed via grep this session — `spawnReporterIfNeeded` has zero call sites in `plan_controller.go`/`project_controller.go`).

**plan_controller.go inline spawn** (lines 646-651, neighbor level-literal at line 608):
```go
// line 608 (already present, unchanged):
thisSpanID, emitted := synthesizePlannerSpan(ctx, "plan", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)
...
// line 646-651 (inline spawn to extend):
reporterJob := BuildReporterJob(plan, project, pvcName, string(plan.UID), "Plan",
	ReporterOptions{
		ReporterImage: r.Deps.ReporterImage,
		TraceParent:   traceparentForLevel(project, plan.Status.PlanTraceSpanID),
		OTLPEndpoint:  r.Deps.OTLPEndpoint,
	}, r.Scheme)
```
**Pattern to add:**
```go
skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "plan", r.Deps.HelmProviderDefaults).Vendor)
reporterJob := BuildReporterJob(plan, project, pvcName, string(plan.UID), "Plan",
	ReporterOptions{
		ReporterImage:    r.Deps.ReporterImage,
		TraceParent:      traceparentForLevel(project, plan.Status.PlanTraceSpanID),
		OTLPEndpoint:     r.Deps.OTLPEndpoint,
		SkipMessageSpans: skipMessageSpans,
	}, r.Scheme)
```
**project_controller.go** is the identical shape (lines 1901-1906, neighbor literal `"project"` at line 1857) — swap `"plan"` → `"project"`, `plan` → `project`, `"Plan"` → `"Project"`.

**Import already present in both files:** verified this session — `plan_controller.go:56` and `project_controller.go:57` both already import `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"`. No import edit needed in either file.

---

### `internal/controller/task_controller.go` (MODIFIED — `spawnTaskTraceReporterIfNeeded`)

**Analog:** itself — the `ReporterOptions{}` literal at lines 1079-1086, inside the single function `spawnTaskTraceReporterIfNeeded` (called twice, at lines 1124 and 1153, from `handleJobCompletion`'s failure and success paths — compute the flag ONCE inside the function body, not at each of its 2 call sites).

**Current body** (`task_controller.go:1057-1094`):
```go
func (r *TaskReconciler) spawnTaskTraceReporterIfNeeded(ctx context.Context, task *tideprojectv1alpha3.Task, project *tideprojectv1alpha3.Project, completedJob *batchv1.Job) {
	logger := logf.FromContext(ctx)

	if completedJob == nil || project == nil {
		return
	}
	if r.Deps.OTLPEndpoint == "" {
		return
	}
	if r.Deps.ReporterImage == "" {
		return
	}

	jobName := "tide-reporter-trace-" + string(completedJob.UID)
	var existing batchv1.Job
	if gErr := r.Get(ctx, client.ObjectKey{Namespace: project.Namespace, Name: jobName}, &existing); gErr == nil {
		return
	} else if !apierrors.IsNotFound(gErr) {
		logger.Error(gErr, "get trace-only reporter Job failed (non-fatal); spawn deferred to a later reconcile", "job", jobName)
		return
	}

	traceOnlyJob := BuildReporterJob(task, project, r.sharedPVCName(), string(task.UID), "Task",
		ReporterOptions{
			ReporterImage:   r.Deps.ReporterImage,
			OTLPEndpoint:    r.Deps.OTLPEndpoint,
			TraceOnly:       true,
			TraceOnlyJobKey: string(completedJob.UID),
			TraceParent:     traceparentForLevel(project, task.Status.TaskTraceSpanID),
		}, r.Scheme)
	...
}
```
**Pattern to add** (one line before the `BuildReporterJob` call, using the SAME `"task"` literal `synthesizePlannerSpan` already uses at line 1006, immediately above in `emitTaskSpanOnce`):
```go
	skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "task", r.Deps.HelmProviderDefaults).Vendor)
	traceOnlyJob := BuildReporterJob(task, project, r.sharedPVCName(), string(task.UID), "Task",
		ReporterOptions{
			ReporterImage:    r.Deps.ReporterImage,
			OTLPEndpoint:     r.Deps.OTLPEndpoint,
			TraceOnly:        true,
			TraceOnlyJobKey:  string(completedJob.UID),
			TraceParent:      traceparentForLevel(project, task.Status.TaskTraceSpanID),
			SkipMessageSpans: skipMessageSpans,
		}, r.Scheme)
```

**Import correction vs. RESEARCH.md Pitfall 4:** the research doc flags "verify `task_controller.go`'s own import list before assuming `pkgdispatch` is present" as a near-certain first-compile error. Verified this session — it is **already imported**: `task_controller.go:63` has `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` (used today for `pkgdispatch.EnvelopeOut{}` at line 1121). No import edit needed in this file.

---

### `cmd/tide-reporter/main.go` (MODIFIED — `parseFlags`, `reporterConfig`, `synthesizeSpans`)

**Analog:** itself — the `TraceOnly` bareword-flag precedent in `parseFlags` (lines 113-141) and the `reporterConfig` struct (lines 87-96); the sentinel-check guard shape in `synthesizeSpans` (lines 316-325) is the placement precedent for the new skip guard (D-05 requires it to run BEFORE that check).

**`reporterConfig` struct to extend** (`main.go:87-96`):
```go
type reporterConfig struct {
	Workspace       string
	ProjectUID      string
	TaskUID         string
	ParentName      string
	ParentNamespace string
	ParentKind      string
	TraceParent     string
	TraceOnly       bool
}
```
Add `SkipMessageSpans bool` alongside `TraceOnly`.

**`parseFlags` bareword-flag precedent to mirror** (`main.go:124-140`):
```go
	traceOnly := fs.Bool("trace-only", false,
		"synthesize LLM message-array spans from events.jsonl only; no child-CR materialization (Phase 44 MSG-01)")

	if err := fs.Parse(args); err != nil {
		return reporterConfig{}, err
	}

	return reporterConfig{
		...
		TraceOnly: *traceOnly,
	}, nil
```
Add: `skipMessageSpans := fs.Bool("skip-message-spans", false, "skip LLM message-array-span synthesis (self-instrumenting vendor; D-03 default-safe: absent = synthesize)")` and thread `SkipMessageSpans: *skipMessageSpans` into the returned struct — **Pitfall 3** (RESEARCH): a flag registered but never copied into the returned struct silently no-ops the whole feature without a crash; mirror `TestParseFlagsTraceparent`'s "assert the PARSED struct field" shape, not just that `fs.Parse` succeeds.

**`synthesizeSpans` guard-placement precedent** (`main.go:316-325`, current first two statements — the sentinel check the new guard must precede):
```go
func synthesizeSpans(ctx context.Context, cfg reporterConfig, stderr io.Writer) {
	eventsPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "events.jsonl")
	inJSONPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "in.json")
	sentinelPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, ".spans-emitted")

	if _, err := os.Stat(sentinelPath); err == nil {
		fmt.Fprintf(stderr,
			"tide-reporter: spans already emitted for task %s (sentinel present) — idempotent skip\n", cfg.TaskUID)
		return
	}
	...
```
**Pattern to add** as the literal FIRST statement (D-05 — before path construction, before the sentinel `os.Stat`, so a skipped run touches the PVC's sentinel path not at all):
```go
func synthesizeSpans(ctx context.Context, cfg reporterConfig, stderr io.Writer) {
	if cfg.SkipMessageSpans {
		fmt.Fprintf(stderr, "tide-reporter: self-instrumenting vendor — skipping message-span synthesis (D-05)\n")
		return
	}
	eventsPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "events.jsonl")
	...
```
`synthesizeSpans` is called from exactly 2 sites in this same file — the trace-only branch (line 198) and the combined-mode path (line 225) — both already route through this single function, so no other call site needs editing (D-05's single-skip-point requirement is satisfied automatically).

---

### `cmd/tide-reporter/main_test.go` (MODIFIED — additions)

**Analog:** itself — `TestParseFlagsTraceparent` (lines 336-353) for the flag-parse assertion shape; `TestRunTraceOnly_EmitsSpans` (lines 532-569) and its sibling `TestRunTraceOnly_MissingEventsStillExitsZero` (line ~574+) for the skip-behavior assertion shape.

**`TestParseFlagsTraceparent` shape to mirror for the new flag:**
```go
func TestParseFlagsTraceparent(t *testing.T) {
	const traceParent = "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"

	cfg, err := parseFlags([]string{
		"--traceparent=" + traceParent,
		"--project-uid=x", "--task-uid=t", "--parent-name=p",
		"--parent-namespace=ns", "--parent-kind=Milestone",
	})
	if err != nil {
		t.Fatalf("parseFlags: unexpected error: %v", err)
	}
	if cfg.TraceParent != traceParent {
		t.Errorf("cfg.TraceParent = %q, want %q", cfg.TraceParent, traceParent)
	}
}
```

**`installStubTracerProvider` + `writeTraceOnlyFixture` + `TestRunTraceOnly_EmitsSpans`** (lines 396-411, 417-439, 532-569) — reuse verbatim, unmodified, for the new skip-behavior test (`TestRunTraceOnly_SkipsSynthesisWhenFlagSet`-style): build `cfg` with `SkipMessageSpans: true` plus a real fixture via `writeTraceOnlyFixture`, assert `exp.GetSpans()` is empty and the `.spans-emitted` sentinel is absent (`os.Stat` returns a not-exist error) — this is the inverse of `TestRunTraceOnly_EmitsSpans`'s "assert spans present" shape.

---

### `internal/reporter/tracesynth.go` (MODIFIED — doc-contract only, D-08)

**Analog:** itself — package doc comment (lines 17-30) and the D-07 inline comment (lines 613-616), which ALREADY forward-references this exact phase.

**Package doc to extend** (`tracesynth.go:17-30`, current text):
```go
// tracesynth.go is the Phase 44 LLM message-array-span synthesizer. It reads
// a completed dispatch's events.jsonl (plus in.json for the out-of-band
// call-1 prompt) and emits one redacted, size-bounded OpenInference LLM-kind
// span per API call (D-01).
//
// Like materialize.go, this file is intentionally import-safe from cmd
// binaries: its only dependencies are the standard library,
// go.opentelemetry.io/otel, internal/harness/redact, internal/subagent/common,
// and pkg/otelai — no back-edge into the controller package.
```
Add (per D-08): a sentence stating this file is specifically the **anthropic-CLI runtime's** trace adapter (it parses that runtime's `events.jsonl` stream format), and naming `pkg/dispatch.SelfInstruments` as the routing datum that decides whether this adapter runs at all — e.g. "This is the anthropic-CLI runtime's adapter behind the ADAPT-01 seam (Phase 45); `pkg/dispatch.SelfInstruments` is the routing datum the manager consults to decide whether a dispatch's reporter Job should even invoke this parser — see `cmd/tide-reporter/main.go`'s `synthesizeSpans` skip guard."

**D-07 inline comment already forward-references this phase** (`tracesynth.go:613-616` — verified live in the tree, not hypothetical):
```go
		span.SetAttributes(otelai.LLMSpanKind())
		// D-07: provider is deliberately hardcoded "anthropic" — this
		// synthesizer parses the Anthropic CLI stream format specifically;
		// Phase 45's adapter seam is where runtime-neutral dispatch lands.
		span.SetAttributes(otelai.LLMIdentity("anthropic", call.Model)...)
```
This comment can be tightened now that Phase 45 IS the adapter seam — e.g. replace "Phase 45's adapter seam is where runtime-neutral dispatch lands" with a concrete pointer: "runtime-neutral routing lives one level up, in `pkg/dispatch.SelfInstruments` + the reporter's `--skip-message-spans` skip guard (Phase 45) — this hardcoded literal is correct because this function only ever runs for the anthropic-CLI adapter path." No logic change — comment-only, per D-08/D-07 (do NOT parameterize `LLMIdentity`'s vendor argument; D-07 from Phase 42 already locked this hardcoded literal as intentional for tracesynth specifically).

---

## Shared Patterns

### "Recompute, don't thread a return value" (the single most important cross-cutting pattern this phase applies 5 times)
**Source:** `internal/controller/span_emission.go:122-125` (doc comment) + `:176` (the call) — the codebase's own established precedent for how a SECOND piece of per-dispatch data (here: capability flag) should be obtained at each of the 5 completion call sites: a **fresh, independent call** to the same pure function (`ResolveProvider`) that a neighboring call already makes, using the identical level literal, rather than threading a value out through an unrelated function's return signature.
**Apply to:** `milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go`, `task_controller.go` — all 5 reporter-spawn call sites.
```go
// The established precedent this phase's flag computation mirrors:
// "a SECOND, envelope-independent call to ResolveProvider... never read
// from the envelope" (span_emission.go:122-125)
thisSpanID, emitted := synthesizePlannerSpan(ctx, "<level>", project, r.Deps.HelmProviderDefaults, completedJob, out, envReadOK, parentSpanID)
// this phase adds, at the SAME call site, using the SAME "<level>" literal:
skipMessageSpans := pkgdispatch.SelfInstruments(ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults).Vendor)
```

### Args-based Job-spec data transport (never Env, except the one documented exception)
**Source:** `internal/controller/reporter_jobspec.go` — 100% of `ReporterOptions` fields ride as CLI Args on the reporter Job container, with exactly one documented exception (`OTLPEndpoint`, which targets the reporter's own otelinit bootstrap via `os.Getenv`, not a CLI flag — see `OTLPEndpoint`'s doc comment, lines 88-96).
**Apply to:** `reporter_jobspec.go` (`SkipMessageSpans` → `--skip-message-spans`), `main.go` (`parseFlags`).

### Fail-closed / default-safe boolean posture (D-03, Pitfall 7)
**Source:** `pkg/dispatch/vendor_capabilities.go`'s `default: return false` arm, `reporterConfig.SkipMessageSpans`'s Go zero-value `false`, and `fs.Bool("skip-message-spans", false, ...)`'s explicit default.
**Apply to:** every layer of the seam — the table, the struct field, and the CLI flag registration must all independently resolve "absent/unknown" to "synthesize," not just one of the three.

### Manager-authored, pod-untrusted transport boundary (D-02)
**Source:** the SAME boundary `ResolveProvider`-then-`EnvelopeIn.Provider` already establishes generally — the reporter process (and, before it, the subagent pod) never re-derives vendor/capability itself; it only receives an already-computed value on a manager-controlled channel (Job Args, not the PVC-writable `in.json`).
**Apply to:** `synthesizeSpans` must read `cfg.SkipMessageSpans` (parsed from the Job Arg) — it must NOT open `in.json` to re-derive `Provider.Vendor` itself, even though `in.json` is already read by `ReconstructConversation` for the call-1 seed prompt.

### `tracetest.InMemoryExporter` + package-level `newTracerProvider` seam (test infra)
**Source:** `cmd/tide-reporter/main_test.go:396-411` (`installStubTracerProvider`) — the house convention (also used in `internal/reporter/tracesynth_test.go`, `internal/controller/span_emission_test.go`) for swapping the global `TracerProvider` in tests without touching production `otelinit` code.
**Apply to:** `adapter_seam_test.go` — reuse `installStubTracerProvider` and `writeTraceOnlyFixture` unmodified (same package `main`, both are unexported file-scope helpers already visible).

## No Analog Found

None. Every file in this phase's scope has an exact or role-matched analog already in the repository — most are literal self-analogs (the file already contains the precedent pattern one field/line over). This is expected for a phase RESEARCH.md itself frames as "threading one already-computed boolean through 5 already-existing call sites and one already-existing function's control flow" — no new architectural shape is introduced.

## Metadata

**Analog search scope:** `pkg/dispatch/`, `internal/controller/` (reporter_jobspec*.go, dispatch_helpers.go, {milestone,phase,plan,project,task}_controller.go, span_emission.go, task_traceonly_reporter_test.go), `cmd/tide-reporter/` (main.go, main_test.go), `internal/reporter/tracesynth.go`, `pkg/otelai/tracecontext.go`.
**Files scanned:** 16 read directly this session (all listed above) + targeted greps confirming exact line numbers for `spawnReporterIfNeeded`/`synthesizePlannerSpan` call sites across all 5 controller files.
**Correction to RESEARCH.md Pitfall 4:** RESEARCH.md flags "verify `task_controller.go`'s own import list before assuming `pkgdispatch` is present" as a near-certain first-compile error. Verified this session via direct grep: **all five** reconciler files (`milestone_controller.go:53`, `phase_controller.go:53`, `plan_controller.go:56`, `project_controller.go:57`, `task_controller.go:63`) already import `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` for existing uses (`pkgdispatch.Caps{}`, `pkgdispatch.EnvelopeOut`). No import edits are needed anywhere in `internal/controller` for this phase — the flagged risk does not materialize.
**Pattern extraction date:** 2026-07-16
