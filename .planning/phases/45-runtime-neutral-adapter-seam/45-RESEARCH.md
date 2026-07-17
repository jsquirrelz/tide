# Phase 45: Runtime-Neutral Adapter Seam - Research

**Researched:** 2026-07-16
**Domain:** Go/Kubernetes controller — vendor-capability data flow across a manager/reporter process boundary (no new external dependencies)
**Confidence:** HIGH

## Summary

This phase wraps Phase 44's `events.jsonl`→spans synthesizer (`internal/reporter/tracesynth.go`) in a data-only capability seam so a future self-instrumenting runtime can skip synthesis without any call site branching on runtime identity. Every claim below was verified directly against the current `main` tree (not the milestone research snapshot, which predates Phases 42–44 landing) — line numbers, signatures, and call-site literals are re-confirmed in this session, not carried over from `.planning/research/`.

The milestone research (`ARCHITECTURE.md` Pattern 4, `PITFALLS.md` Pitfall 7) correctly anticipated the shape: a `pkg/dispatch.SelfInstruments(vendor string) bool` lookup, computed manager-side from the already-resolved `ProviderSpec.Vendor`, carried as a reporter Job CLI arg, consulted at exactly one point in `cmd/tide-reporter/main.go`'s `synthesizeSpans`. What changed since that research was written: `ResolveProvider` (dispatch_helpers.go:271), `ReporterOptions`/`BuildReporterJob` (reporter_jobspec.go:74/181), `spawnReporterIfNeeded` (dispatch_helpers.go:93), `spawnTaskTraceReporterIfNeeded` (task_controller.go:1057), and `synthesizeSpans` (main.go:316) are all now real, shipped code with exact signatures this research pins below — the seam threads through 5 concrete call sites, not a hypothetical architecture.

The single most load-bearing discovery this session adds beyond the milestone research: **every one of the 5 completion call sites already computes `ResolveProvider(project, level, helmDefaults)` at that exact point** — either directly (task_controller.go, twice) or inside the shared `synthesizePlannerSpan` helper (span_emission.go:176, called identically by all 5 levels with the literal level string `"milestone"`/`"phase"`/`"plan"`/`"project"`/`"task"`). The codebase's own established precedent (span_emission.go:122-125 doc comment: "a SECOND, envelope-independent call to ResolveProvider... never read from the envelope") is to call the pure, nil-safe `ResolveProvider` again rather than thread a value out through an existing function's return signature. This phase's capability-flag computation should follow the identical pattern: a fresh `ResolveProvider(project, level, helmDefaults).Vendor` call at each of the 5 reporter-spawn sites, using the SAME level literal each site already passes to `synthesizePlannerSpan`/`ResolveProvider` for span synthesis.

**Primary recommendation:** Add `pkg/dispatch/vendor_capabilities.go` (`SelfInstruments(vendor string) bool`, default-false/fail-closed), one new `ReporterOptions` boolean field + one new reporter CLI bool flag (Args-based, mirroring the existing `--trace-only` bareword-flag convention, NOT the `--traceparent=X` value-flag convention), one early-return guard as the literal first statement of `synthesizeSpans` (before the sentinel-stat, matching D-05's "no sentinel write on skip"), and 5 one-line call-site edits computing the flag from a freshly-called `ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults).Vendor`. Zero new go.mod dependencies, zero chart changes, zero RBAC changes — `pkg/dispatch` is already imported under the `pkgdispatch` alias by both `internal/controller` and `cmd/tide-reporter`.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Vendor→capability lookup (`SelfInstruments`) | Backend (manager, `pkg/dispatch`) | — | Pure data table; must be consulted by both the manager (spawn-time) and the reporter binary (parse-time) without either trusting the subagent pod — `pkg/dispatch` is the only package already imported by both without a layering violation |
| Capability-flag computation (which vendor is "this" dispatch) | Backend (manager reconcilers) | — | `ResolveProvider` is manager-side-only (reads `Project.Spec`, a K8s object); the reporter binary never resolves vendor itself — it only receives the already-computed boolean as a Job arg (D-02 trust posture) |
| Flag transport (manager → reporter process boundary) | Backend (K8s Job spec / Args) | — | Job Args is the tamper-proof channel (manager-authored, not pod-writable) — the existing 100%-Args convention in `reporter_jobspec.go` already established this pattern for `--traceparent`/`--trace-only` |
| Skip decision (whether to run the synthesizer) | Backend (`cmd/tide-reporter`, one-shot binary) | — | `synthesizeSpans` is the sole point that has both the flag and about-to-call `ReconstructConversation` — single source of truth per D-05/Pitfall 7 |
| Future self-instrumenting span emission | Backend (a future `Subagent.Run()` implementation, in-pod) | — | Out of scope this phase (D-07: no Go interface extraction) — `Subagent.Run()` is synchronous and pod-local, so live `tracer.Start()`/`defer span.End()` needs no synthesis hook |

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** New `pkg/dispatch/vendor_capabilities.go` — a data-shaped `SelfInstruments(vendor string) bool` lookup. The manager computes it from the same resolved `ProviderSpec.Vendor` that `ResolveProvider` (`internal/controller/dispatch_helpers.go:271`) already produces. No per-runtime `if vendor == ...` branch appears in any reporter or controller call site — call sites consult the lookup and carry the boolean.
- **D-02 (trust posture):** The flag is manager-computed and travels on the reporter Job spec (manager-controlled), never derived from pod-writable data. `in.json` on the PVC carries `Provider.Vendor` but is writable by the semi-trusted subagent pod — the reporter must not trust it for a skip decision. The Job args/env are the tamper-proof channel.
- **D-03 (default-safe polarity):** Unknown vendor, absent table entry, or absent flag defaults to "synthesize" — a false "native" assumption silently produces zero spans, while a false "synthesize" produces (worst-case) duplicates that are at least visible.
- **D-04:** The flag travels as a new `ReporterOptions` field → CLI arg on the reporter Job (matching `reporter_jobspec.go`'s 100% Args-based convention). Wire it at both builder consumers: `spawnReporterIfNeeded` (combined/materialization shape, 4 planner levels) and `spawnTaskTraceReporterIfNeeded` (trace-only shape, Task success + failure paths).
- **D-05 (single skip point):** The reporter's `synthesizeSpans` (`cmd/tide-reporter/main.go:316`) is the SOLE skip point — it returns before `ReconstructConversation` when the flag says skip (no sentinel write). No heuristic detection ever. Combined-mode planner reporters still run materialization unconditionally; the flag disables only the synth step, uniformly for both Job shapes.
- **D-06 (no spawn-gating this phase):** The manager still spawns trace-only reporter Jobs for self-instrumenting vendors (zero such vendors exist today). Deferred to the LangGraph milestone.
- **D-07:** The seam is the capability flag + the W3C `traceparent` env contract — NOT a Go interface. Do NOT extract a `TraceSynthesizer` interface, do NOT move `tracesynth.go` into `internal/subagent/anthropic/`.
- **D-08 (legibility):** Update doc contracts: `tracesynth.go`'s package comment states it is the anthropic-CLI runtime's trace adapter; `vendor_capabilities.go` documents the default-safe rule (D-03) and trust rationale (D-02).
- **D-09:** One in-process contract test (`tracetest.SpanRecorder`/`tracetest.InMemoryExporter`, plain Go test) with a stub self-instrumenting runtime proving BOTH: (a) env-carrier extraction — a synthetic `TRACEPARENT` becomes the active context before any span starts; (b) zero duplicates — with a valid `events.jsonl` present and the flag set, the reporter emits ZERO synthesized spans.
- **D-10 (default-safe direction pinned):** Test suite pins the inverse: flag absent/vendor unknown → synthesis proceeds. `SelfInstruments("anthropic") == false` is asserted directly.
- **D-11 (spawn-site coverage):** A cheap unit assertion on `BuildReporterJob` (and the two spawn helpers if worthwhile) that the flag rides the Job args as computed from the vendor lookup.

### Claude's Discretion

- Exact flag name and polarity encoding (`--emit-message-spans` vs `--skip-synthesis` etc.), within D-03's absent-means-synthesize rule.
- `SelfInstruments` shape: bare func over a package-level table vs a tiny `Capabilities` struct; how the stub vendor is injected for tests.
- Whether `ReporterOptions` carries the boolean or the vendor string (boolean recommended).
- Placement/wording of the D-08 doc-contract updates.

### Deferred Ideas (OUT OF SCOPE)

- Manager-side trace-only spawn-gating on the capability flag (skip spawning the trace-only reporter Job entirely for self-instrumenting vendors) — belongs to the LangGraph beachhead milestone.
- The actual LangGraph adapter + native-emission activation — vNext milestone by explicit requirement (REQUIREMENTS.md "LangGraph native-emission flag-flip").

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ADAPT-01 | The events.jsonl→spans synthesizer is a per-runtime adapter behind the Subagent seam: a self-instrumenting capability flag travels as data (via the manager's resolved `Provider.Vendor`), the reporter skips synthesis when set, and a contract test proves a self-instrumenting runtime produces no duplicate spans | All three success criteria mapped 1:1 to D-01/D-02 (flag-as-data), D-04/D-05 (transport + skip point), D-09/D-10 (contract test) below — see Code Examples and Common Pitfalls sections for exact insertion points |

## Project Constraints (from CLAUDE.md)

- **GSD Workflow Enforcement**: no direct repo edits outside a GSD workflow — this phase's implementation must route through `/gsd:execute-phase` (already the active flow).
- **`charts/tide/values.yaml` is a FIXED contract** — confirmed no chart change is needed this phase (see "Chart/RBAC surface" below); do not touch it even incidentally.
- **Never hardcode secrets / no per-runtime branch anti-pattern**: matches D-01 exactly — "Don't hard-code one LLM provider... in the controller. All Anthropic-specific code lives behind the `Subagent` interface" (Stack Anti-patterns section) — `SelfInstruments` must be a lookup table, never an `if vendor == "anthropic"` branch at a call site.
- **Layered-Kahn / import-firewall conventions**: `pkg/dispatch` must stay import-cycle-free from `internal/controller`/`internal/subagent` — confirmed both already import it safely (see "Go module/package placement" below).
- **Subagent model tuning section** (Opus 4.x prompting) is not directly relevant to this phase's code surface — no template/prompt changes in scope.
- **Verify Before Claiming**: any planner-authored task claiming "the flag reaches the reporter" must be verified via `TestBuildReporterJob_*`-style assertions on the actual `Args` slice, not asserted from reading the diff.

## Standard Stack

### Core

No new dependencies. This phase is 100% internal Go code using primitives already vendored and already used identically elsewhere in this codebase:

| Component | Location | Purpose | Why Standard (existing precedent) |
|-----------|----------|---------|--------------------------------|
| `go.opentelemetry.io/otel/propagation` (pinned) | `pkg/otelai/tracecontext.go:93` (`ExtractRemoteParent`) | W3C traceparent extraction | Already used identically by `synthesizeSpans` today (main.go:327) — the D-09 contract test exercises the SAME function, not a new one |
| `go.opentelemetry.io/otel/sdk/trace/tracetest` (pinned) | `cmd/tide-reporter/main_test.go`, `internal/reporter/tracesynth_test.go`, `internal/controller/span_emission_test.go` | In-memory span recording for tests | House convention across all three existing OTel test suites in this repo — `tracetest.NewInMemoryExporter()` + `sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))` |
| stdlib `flag` package | `cmd/tide-reporter/main.go:114` (`flag.NewFlagSet`) | CLI arg parsing | Already the reporter's only flag mechanism — the new flag is one more `fs.Bool(...)` call |

**Installation:** None. No `go get`, no `go.mod` edit.

## Package Legitimacy Audit

Not applicable — this phase introduces zero new external packages (Go or otherwise). All primitives used (`go.opentelemetry.io/otel/*`, stdlib `flag`) are already pinned in `go.mod` and already imported by the exact files this phase edits. The Package Legitimacy Gate protocol is skipped per its own scope ("whenever this phase installs external packages").

## Architecture Patterns

### System Architecture Diagram

```
Manager reconcile (one of 5 completion call sites, e.g. TaskReconciler.spawnTaskTraceReporterIfNeeded)
    │
    │ 1. provider := ResolveProvider(project, "<level>", r.Deps.HelmProviderDefaults)
    │    (SAME literal already passed to synthesizePlannerSpan at this exact call site —
    │     "milestone"|"phase"|"plan"|"project"|"task")
    │
    │ 2. skip := pkgdispatch.SelfInstruments(provider.Vendor)   ← NEW, pkg/dispatch/vendor_capabilities.go
    │
    ▼
BuildReporterJob(..., ReporterOptions{ ..., SkipMessageSpans: skip })
    │
    │ 3. if opts.SkipMessageSpans { args = append(args, "--skip-message-spans") }
    │    (bareword bool flag, mirrors existing --trace-only convention —
    │     reporter_jobspec.go, appended AFTER the TraceOnly/materialization
    │     branch split, alongside the existing --traceparent append)
    │
    ▼
tide-reporter Job pod (cmd/tide-reporter/main.go)
    │
    │ 4. parseFlags: fs.Bool("skip-message-spans", false, ...) → cfg.SkipMessageSpans
    │    (Go zero-value false = D-03 default-safe: absent flag → synthesize)
    │
    ▼
synthesizeSpans(ctx, cfg, stderr)          ← D-05 SOLE skip point (main.go:316)
    │
    │ 5. if cfg.SkipMessageSpans { fmt.Fprintf(stderr, "...self-instrumenting vendor, skip..."); return }
    │    (FIRST statement — before the sentinel os.Stat check; skip writes NO sentinel)
    │
    ▼ (only when NOT skipped)
ReconstructConversation(...) → EmitSpans(...) → sentinel write   [unchanged Phase 44 behavior]
```

### Recommended Project Structure

```
pkg/
└── dispatch/
    ├── provider.go                 # UNCHANGED — ProviderSpec.Vendor
    ├── vendor_capabilities.go      # NEW — SelfInstruments(vendor string) bool
    └── vendor_capabilities_test.go # NEW — D-10 guard test
internal/
├── controller/
│   ├── dispatch_helpers.go         # MODIFY spawnReporterIfNeeded: +1 bool param
│   ├── reporter_jobspec.go         # MODIFY ReporterOptions: +1 field; BuildReporterJob: +1 arg append
│   ├── reporter_jobspec_test.go    # MODIFY: +1 test mirroring TestBuildReporterJob_TraceparentArg (D-11)
│   ├── milestone_controller.go     # MODIFY: 1-line flag computation before spawnReporterIfNeeded call
│   ├── phase_controller.go         # MODIFY: same
│   ├── plan_controller.go          # MODIFY: 1-line flag computation + ReporterOptions literal field (inline spawn)
│   ├── project_controller.go       # MODIFY: same (inline spawn)
│   └── task_controller.go          # MODIFY: spawnTaskTraceReporterIfNeeded — flag computation + ReporterOptions field
└── reporter/
    └── tracesynth.go                # MODIFY: package doc + line-613 comment (D-08 legibility only — no logic change)
cmd/
└── tide-reporter/
    ├── main.go                      # MODIFY: parseFlags (+1 flag), reporterConfig (+1 field), synthesizeSpans (+1 guard)
    ├── main_test.go                 # MODIFY: +tests for flag parse + skip behavior
    └── adapter_seam_test.go         # NEW — D-09 contract test (stub runtime + reporter, shared InMemoryExporter)
```

### Pattern 1: Recompute the flag at each completion site via a fresh `ResolveProvider` call (don't thread a return value)

**What:** Every one of the 5 reporter-spawn call sites already has `project *tideprojectv1alpha3.Project`, `r.Deps.HelmProviderDefaults`, and a literal level string in scope at the exact point it builds `ReporterOptions`. `ResolveProvider` is pure and nil-safe (`internal/controller/dispatch_helpers.go:271`). The established codebase precedent (`span_emission.go:122-125`) is a SECOND, independent call to `ResolveProvider` rather than threading a value out through an unrelated function's signature.

**When to use:** Exactly this phase's 5 call sites.

**Example (TaskReconciler.spawnTaskTraceReporterIfNeeded, task_controller.go:1057-1094):**
```go
// Source: internal/controller/task_controller.go:1079 (current), extended
provider := ResolveProvider(project, "task", r.Deps.HelmProviderDefaults) // NEW line
traceOnlyJob := BuildReporterJob(task, project, r.sharedPVCName(), string(task.UID), "Task",
    ReporterOptions{
        ReporterImage:      r.Deps.ReporterImage,
        OTLPEndpoint:       r.Deps.OTLPEndpoint,
        TraceOnly:          true,
        TraceOnlyJobKey:    string(completedJob.UID),
        TraceParent:        traceparentForLevel(project, task.Status.TaskTraceSpanID),
        SkipMessageSpans:   pkgdispatch.SelfInstruments(provider.Vendor), // NEW field
    }, r.Scheme)
```
Note: `task_controller.go` does NOT currently import `pkgdispatch` under that alias in this function's file scope for this purpose — verify the existing import block (`pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` is already imported at the top of `dispatch_helpers.go`; confirm `task_controller.go`'s own import list before assuming it's already present).

**Confidence:** HIGH — `ResolveProvider`'s nil-safety and the exact level literals per site are directly read from source this session (dispatch_helpers.go:271-318; milestone_controller.go:607, phase_controller.go:564, plan_controller.go:608, project_controller.go:1857, task_controller.go:1006 all confirmed via grep).

### Pattern 2: Bareword bool flag on the Job Args, mirroring `--trace-only` (not `--traceparent=X`)

**What:** `reporter_jobspec.go`'s `BuildReporterJob` (lines 181-328) has two existing flag-encoding conventions on the same `args []string` slice: (a) branch-selecting barewords appended unconditionally within an `if` (`"--trace-only"`, no `=value` suffix, parsed via `fs.Bool("trace-only", false, ...)`), and (b) value-bearing flags appended only when non-default (`"--traceparent=" + opts.TraceParent`, appended only `if opts.TraceParent != ""`). Since the skip flag is a pure boolean that composes with EITHER Job shape (unlike `--trace-only`, which selects the shape itself), it should follow convention (b)'s "append only when non-default" discipline but convention (a)'s bareword (no `=value`) syntax — i.e. `args = append(args, "--skip-message-spans")` only `if opts.SkipMessageSpans`, parsed via `fs.Bool("skip-message-spans", false, ...)`.

**When to use:** This phase's flag design (Claude's Discretion per CONTEXT D-04, but this pattern is the closest fit to existing conventions and satisfies D-03's absent-means-synthesize rule for free via Go's zero-value default).

**Example:**
```go
// Source: internal/controller/reporter_jobspec.go, extending the existing
// TraceParent append block (~line 211-215)
if opts.TraceParent != "" {
    args = append(args, "--traceparent="+opts.TraceParent)
}
if opts.SkipMessageSpans {
    args = append(args, "--skip-message-spans")
}
```
```go
// Source: cmd/tide-reporter/main.go, extending parseFlags (~line 113-141)
skipMessageSpans := fs.Bool("skip-message-spans", false,
    "skip LLM message-array-span synthesis (set for self-instrumenting vendors, D-03 default-safe: absent = synthesize)")
// ... return reporterConfig{ ..., SkipMessageSpans: *skipMessageSpans }
```

**Confidence:** HIGH on the two existing conventions being accurately described (both verified directly against reporter_jobspec.go:181-215 and main.go:113-141 this session). MEDIUM on "bareword is the best choice" — this is design synthesis, not an observed requirement; the planner may reasonably choose `--emit-message-spans=<bool>` instead per CONTEXT's explicit discretion grant. Either satisfies D-03 as long as the flag's Go zero-value (unset) resolves to "synthesize."

### Pattern 3: Skip guard as the literal first statement of `synthesizeSpans` — before the sentinel check

**What:** `synthesizeSpans` (main.go:316-356) currently opens with path construction, then an `os.Stat(sentinelPath)` idempotency check. D-05 requires the skip decision to write NO sentinel (a skipped run "emits nothing," so there's nothing to dedupe against on a hypothetical retry). Placing the guard before the sentinel check also means a self-instrumenting Task's reporter never even touches the PVC's sentinel file — zero incidental I/O, and the guard reads cleanly as "is this run's job to do anything at all," which the sentinel check (a narrower "have I already done my job") composes under.

**Example:**
```go
// Source: cmd/tide-reporter/main.go:316, synthesizeSpans — NEW first statement
func synthesizeSpans(ctx context.Context, cfg reporterConfig, stderr io.Writer) {
    if cfg.SkipMessageSpans {
        fmt.Fprintf(stderr, "tide-reporter: self-instrumenting vendor — skipping message-span synthesis (D-05)\n")
        return
    }
    eventsPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "events.jsonl")
    // ... unchanged
}
```

**Confidence:** HIGH — `synthesizeSpans`'s current body read directly this session (main.go:316-356); this is the exact function CONTEXT D-05 names as the sole skip point.

### Pattern 4: The D-09 contract test — stub runtime + reporter, sharing one `tracetest.InMemoryExporter`

**What:** `cmd/tide-reporter/main_test.go` already has every mechanic the D-09 contract test needs: `installStubTracerProvider(t, exp)` (main_test.go:396-411) swaps the `newTracerProvider` package seam to point `otel.SetTracerProvider` at an `sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))`, and `writeTraceOnlyFixture(t, workspace, taskUID)` (main_test.go:417-439) writes a real 2-call `events.jsonl` + `in.json` fixture. `TestRunTraceOnly_EmitsSpans` (main_test.go:532-569) already proves env-carrier extraction works end-to-end today (asserts every emitted span's TraceID matches a synthetic injected traceparent) — this is the closest existing test and should be mirrored, not reinvented.

The NEW contract test adds one thing `TestRunTraceOnly_EmitsSpans` doesn't: a "stub self-instrumenting runtime" that ALSO emits its own span via the identical `otelai.ExtractRemoteParent` primitive, sharing the SAME `exp` exporter as the reporter run, then asserts the exporter's total span count is exactly the stub's own span count (proving `synthesizeSpans` contributed zero spans).

**Example (new file `cmd/tide-reporter/adapter_seam_test.go`):**
```go
// Source: synthesizes cmd/tide-reporter/main_test.go:396-439, 532-569 patterns
func TestAdapterSeam_SelfInstrumentingRuntimeNoDuplicateSpans(t *testing.T) {
    exp := tracetest.NewInMemoryExporter()
    installStubTracerProvider(t, exp)

    traceID, _ := trace.TraceIDFromHex("0af7651916cd43dd8448eb211c80319c")
    spanID, _ := trace.SpanIDFromHex("b7ad6b7169203331")
    traceParent := otelai.FormatTraceparent(traceID, spanID, true)

    // (a) env-carrier extraction: stub runtime extracts TRACEPARENT and
    // parents its OWN span under it — generic mechanics only, no
    // LangGraph-specific span shape.
    stubCtx := otelai.ExtractRemoteParent(context.Background(), traceParent)
    _, stubSpan := otel.Tracer("stub-self-instrumenting-runtime").Start(stubCtx, "stub.graph.invoke")
    stubSpan.End()

    // (b) zero duplicates: a REAL events.jsonl is present on disk, flag is set.
    workspace := t.TempDir()
    taskUID := "task-self-instrumenting"
    writeTraceOnlyFixture(t, workspace, taskUID) // real-shaped fixture, would normally produce 2 spans
    cfg := reporterConfig{
        TraceOnly: true, TaskUID: taskUID, Workspace: workspace,
        TraceParent: traceParent, SkipMessageSpans: true, // the flag under test
    }
    var stderr bytes.Buffer
    code := runWithClient(context.Background(), cfg, nil, &stderr, nil)
    if code != exitSuccess {
        t.Fatalf("runWithClient exit=%d, want exitSuccess; stderr=%q", code, stderr.String())
    }

    spans := exp.GetSpans()
    if len(spans) != 1 {
        t.Fatalf("got %d spans, want exactly 1 (the stub runtime's own — zero synthesized)", len(spans))
    }
    if spans[0].Name != "stub.graph.invoke" {
        t.Errorf("unexpected span in exporter: %q (synthesis was not skipped)", spans[0].Name)
    }
    if spans[0].SpanContext.TraceID() != traceID {
        t.Errorf("stub span TraceID = %s, want %s (env-carrier extraction did not activate)", spans[0].SpanContext.TraceID(), traceID)
    }

    // No sentinel written on skip (D-05).
    sentinelPath := filepath.Join(workspace, "envelopes", taskUID, ".spans-emitted")
    if _, err := os.Stat(sentinelPath); err == nil {
        t.Error("sentinel file written on a skipped run — D-05 violation")
    }
}
```

**Confidence:** HIGH on the reused mechanics (`installStubTracerProvider`, `writeTraceOnlyFixture`, `otelai.ExtractRemoteParent`, `otelai.FormatTraceparent` all read directly from source this session). MEDIUM on "this exact test shape is what the planner will author" — this is a worked example synthesizing existing conventions, not a verbatim quote of code that exists yet.

### Anti-Patterns to Avoid

- **A per-runtime `if vendor == "langgraph"` branch anywhere in `cmd/tide-reporter` or `internal/controller`:** D-01 explicitly forbids this; the CLAUDE.md Stack Anti-patterns section forbids it project-wide ("Don't hard-code one LLM provider... in the controller"). `SelfInstruments` is the sole decision point.
- **Trusting `in.json`'s `Provider.Vendor` for the skip decision:** `in.json` is on the subagent-writable PVC (confirmed: `internal/reporter/tracesynth.go`'s `seedPrompt` function explicitly treats `in.json`-derived paths as "attacker-populatable" and confines reads via `os.Root`). The flag must originate from the manager's `ResolveProvider` call, never be re-derived by the reporter from PVC content.
- **Writing the sentinel on a skipped run:** D-05 is explicit — "no sentinel write." Placing the skip guard before the `os.Stat(sentinelPath)` check (Pattern 3) makes this structurally impossible rather than relying on a later conditional.
- **Extracting a `TraceSynthesizer` Go interface or moving `tracesynth.go`:** D-07 explicitly locks this out — `Subagent.Run()` is synchronous/pod-local and needs no synthesis hook; the seam is data (the flag) + the existing W3C traceparent env contract, not a new abstraction.
- **Defaulting `SelfInstruments` to `true` for an unrecognized vendor string:** Pitfall 7's warning sign verbatim: "an unset flag should default to 'synthesize,' never to 'assume native.'" The `switch` in `SelfInstruments` must have every case return `false`, including `default`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| W3C traceparent parsing/extraction | A hand-rolled `strings.Split` on the `traceparent` header | `otelai.ExtractRemoteParent` (already exists, `pkg/otelai/tracecontext.go:93`) | Already the reporter's own extraction path (main.go:327) — the contract test proves the SAME function, not a reimplementation |
| In-memory span capture for tests | A custom span-collecting `trace.SpanExporter` | `go.opentelemetry.io/otel/sdk/trace/tracetest.InMemoryExporter` | House convention across 3 existing test suites in this repo; `GetSpans()` + `.Reset()` already do everything the D-09 test needs |
| Vendor-string equality checks scattered across call sites | Repeated `if provider.Vendor == "anthropic" { ... }` | `pkgdispatch.SelfInstruments(vendor)` (this phase's single new function) | The entire point of D-01 — one table, consulted everywhere, so a second vendor entry is a one-line diff instead of an N-site grep-and-edit |

**Key insight:** This phase adds no new abstraction layer, no new package, and no new dependency — its entire job is threading one already-computed boolean through 5 already-existing call sites and one already-existing function's control flow. The risk is NOT "hard to build," it's "easy to introduce a 6th call site nobody threads the flag through" (see Common Pitfalls).

## Common Pitfalls

### Pitfall 1: Missing one of the 5 flag-computation call sites

**What goes wrong:** `spawnReporterIfNeeded` is shared by exactly 2 of the 5 levels (Milestone, Phase — confirmed via grep this session: `milestone_controller.go:639`, `phase_controller.go:592`). Plan and Project build `ReporterOptions{}` INLINE, not via the shared helper (confirmed: `plan_controller.go:646-651`, `project_controller.go:1901-1906`). Task uses a THIRD shape (`spawnTaskTraceReporterIfNeeded`, task_controller.go:1057-1094, itself called from 2 separate places — the `EnvelopeReadFailed` failure path at line 1124 and the standard success path at line 1153 — both must carry the SAME flag, but only need ONE computation since they're in the same function body). A plan that only threads the flag through `spawnReporterIfNeeded`'s shared signature (assuming it's "the" reporter spawn point) will silently miss Plan, Project, and Task — 3 of 5 levels.

**Why it happens:** `spawnReporterIfNeeded` LOOKS like the canonical spawn path because it's a named, reusable function — but `ARCHITECTURE.md`'s own "Anti-Pattern 3" section and this session's direct grep confirm Plan/Project never call it; they duplicate its logic inline (likely because Plan/Project's reporter spawn predates the helper's extraction, or because their surrounding control flow — `isFirstCompletion` budget-rollup gating — didn't fit the helper's return shape cleanly).

**How to avoid:** Enumerate exactly 5 edit sites by function, not by "call spawnReporterIfNeeded": `internal/controller/milestone_controller.go` (via `spawnReporterIfNeeded`), `internal/controller/phase_controller.go` (via `spawnReporterIfNeeded`), `internal/controller/plan_controller.go:646` (inline `ReporterOptions{}` literal), `internal/controller/project_controller.go:1901` (inline `ReporterOptions{}` literal), `internal/controller/task_controller.go:1079` (inline `ReporterOptions{}` literal inside `spawnTaskTraceReporterIfNeeded`, itself called twice at lines 1124/1153 but requiring only one flag computation).

**Warning signs:** A `grep -n "ReporterOptions{" internal/controller/*.go` after the change shows fewer than 5 struct literals/call sites carrying the new field.

### Pitfall 2: Computing the flag with the WRONG level string

**What goes wrong:** `ResolveProvider`'s `level` parameter maps through `levelOverrideKey` (dispatch_helpers.go:240-255) to a DIFFERENT `Levels.<X>` config slot than the literal passed in (e.g. `ResolveProvider(project, "milestone", ...)` internally resolves against `project.Spec.Subagent.Levels.Phase`, per the D-02 semantic rename documented at dispatch_helpers.go:220-239). If the flag computation accidentally passes the WRONG literal (e.g. computing "phase"'s vendor when spawning Milestone's reporter), it would silently consult the wrong config slot — invisible today (Vendor is hardcoded `"anthropic"` regardless of level, so no test would catch a level-string swap), but a real bug once a second vendor exists.

**Why it happens:** The level string passed to `ResolveProvider` at each site is idiosyncratic per-reconciler (each reconciler passes its OWN identity literal — Milestone passes `"milestone"`, not `"phase"` — even though `levelOverrideKey` remaps it internally). It's easy to reason "the flag belongs to the NEXT level down" (since the reporter is about to parse the events.jsonl of the Job that JUST authored the artifact for the level BELOW) and pass the wrong literal.

**How to avoid:** Use the EXACT SAME level literal each site already passes to its neighboring `synthesizePlannerSpan(ctx, "<level>", ...)` call (confirmed identical per site this session: milestone_controller.go:607 uses `"milestone"`, phase_controller.go:564 uses `"phase"`, plan_controller.go:608 uses `"plan"`, project_controller.go:1857 uses `"project"`, task_controller.go:1006 uses `"task"`). This is not a coincidence — both calls describe the SAME dispatch (the Job that just completed at THIS reconciler's own level), so reusing the literal is correct by construction, not just convenient.

**Warning signs:** A code review diff where the level string passed to the new `ResolveProvider` call for flag computation differs from the level string passed to the neighboring `synthesizePlannerSpan` call in the same function.

### Pitfall 3: Adding the flag to `parseFlags` but forgetting `reporterConfig` struct field wiring (crash-loop risk)

**What goes wrong:** `parseFlags` is unit-tested specifically because "an Args entry without a registered flag would crash-loop every reporter Job in the cluster" (main.go:108-112 doc comment, confirmed this session). The inverse failure — a registered flag whose parsed value is never copied into the returned `reporterConfig` struct — doesn't crash-loop, but silently no-ops the entire feature: the flag parses successfully, `*skipMessageSpans` holds the correct value, but if the `return reporterConfig{...}` literal omits the field, `cfg.SkipMessageSpans` is always the Go zero-value `false` regardless of what was passed on the CLI.

**How to avoid:** The existing `TestParseFlagsTraceparent` (main_test.go:336) is the exact test shape to mirror — assert the PARSED `reporterConfig` struct's new field, not just that `fs.Parse` succeeds.

**Warning signs:** A new flag registered in `parseFlags` with no corresponding assertion in a `TestParseFlags*` test.

### Pitfall 4: `task_controller.go` missing the `pkgdispatch` import alias

**What goes wrong:** `dispatch_helpers.go` imports `pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"` (confirmed line 61), but `task_controller.go` is a SEPARATE file in the same package — Go import aliases are per-file, not per-package. If `task_controller.go` doesn't already import `pkg/dispatch` under this (or any) alias, adding `pkgdispatch.SelfInstruments(...)` there will fail to compile until the import is added.

**How to avoid:** Check `task_controller.go`'s own import block before assuming the alias is already in scope; `go build ./...` (or the planner's own compile-check task) will surface this immediately if missed — low-severity, fast to fix, but worth flagging as a near-certain first-compile error rather than a design risk.

**Warning signs:** `undefined: pkgdispatch` compile error scoped specifically to `task_controller.go`.

## Code Examples

### `pkg/dispatch/vendor_capabilities.go` (new file, D-01/D-03/D-08)

```go
// Source: synthesizes ARCHITECTURE.md Pattern 4 + this session's D-02/D-03 verification
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

// SelfInstruments reports whether vendor's Subagent implementation emits
// OpenInference spans natively during Run(), making
// internal/reporter/tracesynth.go's events.jsonl-based synthesis redundant
// for that vendor's dispatches. Every vendor returns false today — no
// self-instrumenting runtime exists yet (the LangGraph beachhead milestone
// is the first candidate). Unknown/unrecognized vendor strings also return
// false (fail-closed default — D-03).
func SelfInstruments(vendor string) bool {
	switch vendor {
	case "anthropic", "openai", "google", "xai", "opencode":
		return false // CLI/wrapper-shimmed — no in-process OTel SDK
	default:
		return false // fail-closed: unknown vendor never skips synthesis
	}
}
```

### `pkg/dispatch/vendor_capabilities_test.go` (new file, D-10)

```go
// Source: mirrors pkg/dispatch/provider_test.go's plain-func style (no testify)
package dispatch

import "testing"

func TestSelfInstruments_KnownVendorsDefaultFalse(t *testing.T) {
	for _, v := range []string{"anthropic", "openai", "google", "xai", "opencode"} {
		if SelfInstruments(v) {
			t.Errorf("SelfInstruments(%q) = true, want false (no self-instrumenting runtime exists yet)", v)
		}
	}
}

func TestSelfInstruments_UnknownVendorDefaultsFalse(t *testing.T) {
	// D-03/Pitfall 7: an unrecognized vendor must default to "synthesize"
	// (false), never "assume native" (true).
	if SelfInstruments("some-future-unregistered-vendor") {
		t.Error("SelfInstruments on an unknown vendor = true, want false (D-03 fail-closed default)")
	}
	if SelfInstruments("") {
		t.Error("SelfInstruments(\"\") = true, want false")
	}
}
```

### `internal/controller/reporter_jobspec_test.go` addition (D-11)

```go
// Source: mirrors TestBuildReporterJob_TraceparentArg (reporter_jobspec_test.go:146-194) exactly
func TestBuildReporterJob_SkipMessageSpansArg(t *testing.T) {
	project := &tideprojectv1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "proj", Namespace: "ns-c", UID: "project-uid-5"},
	}
	parent := &tideprojectv1alpha3.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: "ms-4", Namespace: "ns-c", UID: "parent-uid-5"},
	}
	scheme := newTestScheme()

	t.Run("present when set", func(t *testing.T) {
		opts := controller.ReporterOptions{
			ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev", SkipMessageSpans: true,
		}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-5", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		var found bool
		for _, a := range args {
			if a == "--skip-message-spans" {
				found = true
			}
		}
		if !found {
			t.Errorf("expected --skip-message-spans arg not present in %v", args)
		}
	})

	t.Run("absent when false (D-03 default-safe)", func(t *testing.T) {
		opts := controller.ReporterOptions{ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev"}
		job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-5", "Milestone", opts, scheme)
		args := job.Spec.Template.Spec.Containers[0].Args
		for _, a := range args {
			if a == "--skip-message-spans" {
				t.Errorf("did not expect --skip-message-spans arg when SkipMessageSpans is false, got %v", args)
			}
		}
	})
}
```

## State of the Art

| Old Approach (pre-Phase-45) | Current Approach (this phase) | When Changed | Impact |
|--------------------------|-------------------------------|---------------|--------|
| `internal/reporter/tracesynth.go` runs unconditionally for every completed dispatch | Runs conditionally, gated by a manager-computed capability flag | This phase | Zero behavioral change TODAY (every vendor resolves `false`) — the change is purely additive scaffolding for the LangGraph beachhead milestone |
| `tracesynth.go`'s package comment describes itself as "the Phase 44 LLM message-array-span synthesizer" with no runtime-neutrality framing | Package comment (D-08) states it is specifically the anthropic-CLI runtime's trace adapter, with `pkg/dispatch.SelfInstruments` named as the routing datum | This phase | Legibility only — a future contributor reading this file understands WHY it exists and where the seam is, without needing to read PROJECT.md |

**Deprecated/outdated:** Nothing in this phase deprecates prior code — Phase 44's synthesis logic (`ReconstructConversation`, `EmitSpans`, the D-08/D-09 truncation pipeline) is unchanged; this phase only adds a guard clause in front of the entry point that calls it.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The recommended flag name `--skip-message-spans` (bareword, presence=true) is the best fit vs. `--emit-message-spans=<bool>` | Architecture Patterns, Pattern 2 | LOW — CONTEXT.md D-04 explicitly grants this as Claude's Discretion; either choice satisfies D-03 as long as the Go zero-value resolves to "synthesize." Purely a style preference, not a correctness risk. |
| A2 | `ReporterOptions`'s new field should be named `SkipMessageSpans` (vs. e.g. `EmitMessageSpans` with inverted polarity, or `SelfInstrumenting`) | Code Examples, Architecture Patterns | LOW — naming only; CONTEXT.md explicitly leaves this to planner discretion. Inverted polarity (`EmitMessageSpans bool` defaulting to `true`) is equally valid but requires an explicit `true` default in the flag registration rather than relying on Go's zero-value, which is a slightly larger deviation from this file's existing "append arg only when non-default" convention. |
| A3 | The D-09 contract test's best home is a new file `cmd/tide-reporter/adapter_seam_test.go` rather than `internal/reporter` or a `pkg/otelai` addition | Architecture Patterns, Pattern 4 | LOW — this is a structural placement suggestion grounded in "reuses `installStubTracerProvider`/`writeTraceOnlyFixture` without duplication," but the planner could equally place it inline in `main_test.go` itself. No functional risk either way. |

**All claims above are LOW risk** — they are implementation-detail recommendations within explicitly-granted discretion, not load-bearing architectural or security claims. Every claim NOT listed here (function signatures, line numbers, call-site literals, existing test conventions) was verified directly against `main` this session via `Read`/`grep` and is tagged `[VERIFIED: codebase]` in effect throughout this document.

## Open Questions

1. **Should `task_controller.go`'s two `spawnTaskTraceReporterIfNeeded` CALL sites (lines 1124, 1153) each recompute the flag, or should the ONE flag computation happen inside `spawnTaskTraceReporterIfNeeded` itself?**
   - What we know: `spawnTaskTraceReporterIfNeeded` is a single function called from 2 places in `handleJobCompletion` (the `EnvelopeReadFailed` path and the standard success path) — both need the identical flag value for the identical Task.
   - What's unclear: Nothing structurally — `ResolveProvider(project, "task", r.Deps.HelmProviderDefaults)` is nil-safe and idempotent, so computing it ONCE inside `spawnTaskTraceReporterIfNeeded`'s own body (not at each of its 2 call sites) is strictly simpler and avoids duplication.
   - Recommendation: Compute inside `spawnTaskTraceReporterIfNeeded` itself, immediately before the existing `BuildReporterJob` call at line 1079 — this requires editing exactly 1 function body, not 2 call sites.

2. **Does adding `pkgdispatch.SelfInstruments` calls to `milestone_controller.go`/`phase_controller.go` (for the `spawnReporterIfNeeded` param) require also updating `spawnReporterIfNeeded`'s doc comment (dispatch_helpers.go:80-92), which currently doesn't mention any capability-flag param?**
   - What we know: The function's doc comment is fairly detailed about its existing params (pvcName, isFirstCompletion return semantics) but predates this phase.
   - What's unclear: Whether the planner should treat doc-comment currency as in-scope for this phase's diff, or defer to normal code-review hygiene.
   - Recommendation: Update the doc comment as part of the same diff that changes the signature — this codebase's convention (seen throughout `dispatch_helpers.go`, `reporter_jobspec.go`) is exhaustive doc comments citing the Phase/D-number that introduced each param; leaving a stale comment would be inconsistent with house style.

## Environment Availability

Skipped — this phase has no external tool/service/runtime dependencies beyond the already-pinned Go toolchain and already-vendored `go.opentelemetry.io/otel` modules, both confirmed present and in use by the exact files this phase edits.

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (plain `go test`) for `pkg/dispatch` and `cmd/tide-reporter`; Ginkgo v2.28 + Gomega + envtest for `internal/controller` (only if the optional D-11 spawn-helper coverage is added) |
| Config file | None dedicated — `go.mod` at repo root; envtest bootstrap in `internal/controller/suite_test.go` |
| Quick run command | `go test ./pkg/dispatch/... ./cmd/tide-reporter/... ./internal/reporter/...` (no envtest needed — none of these packages require `KUBEBUILDER_ASSETS`) |
| Full suite command | `make test` (unit tier: `-short`, excludes Ginkgo `Label("heavy")` specs per `suite_test.go:130`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| ADAPT-01 (criterion 1: flag-as-data, never a branch) | `SelfInstruments` is a pure lookup; every vendor + unknown vendor default false | unit | `go test ./pkg/dispatch/... -run TestSelfInstruments -v` | ❌ Wave 0 — new file `vendor_capabilities_test.go` |
| ADAPT-01 (criterion 1: flag threads through both Job shapes) | `BuildReporterJob` emits `--skip-message-spans` only when the new `ReporterOptions` field is true | unit | `go test ./internal/controller/... -run TestBuildReporterJob_SkipMessageSpansArg -v` | ❌ Wave 0 — addition to existing `reporter_jobspec_test.go` |
| ADAPT-01 (criterion 1: manager computes flag from resolved vendor, not hardcoded) | `parseFlags` correctly parses `--skip-message-spans` into `reporterConfig.SkipMessageSpans` | unit | `go test ./cmd/tide-reporter/... -run TestParseFlags -v` | ❌ Wave 0 — addition to existing `main_test.go` (mirrors `TestParseFlagsTraceparent`) |
| ADAPT-01 (criterion 2: reporter skips synthesis when flag set) | `synthesizeSpans` returns before `ReconstructConversation` when `cfg.SkipMessageSpans` is true, no sentinel written | unit | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly -v` (extend with a skip-specific case) | ❌ Wave 0 — addition to existing `main_test.go` |
| ADAPT-01 (criterion 2 inverse, D-10: absent flag still synthesizes) | Existing `TestRunTraceOnly_EmitsSpans` continues passing unmodified — proves default (unset) flag still produces spans | unit | `go test ./cmd/tide-reporter/... -run TestRunTraceOnly_EmitsSpans -v` | ✅ existing, must stay green |
| ADAPT-01 (criterion 3: contract test, zero duplicates + env-carrier extraction) | Stub self-instrumenting runtime emits 1 span; reporter with flag set + real events.jsonl emits 0 additional spans; combined exporter shows exactly 1 span with the expected TraceID | unit | `go test ./cmd/tide-reporter/... -run TestAdapterSeam -v` | ❌ Wave 0 — new file `adapter_seam_test.go` |
| Regression: Phase 44 behavior byte-identical | All existing `tracesynth_test.go`/`main_test.go`/Phase-44 controller tests stay green with the default (unset) flag | unit + envtest | `make test` | ✅ existing suite, must stay green |

### Sampling Rate

- **Per task commit:** `go test ./pkg/dispatch/... ./cmd/tide-reporter/... ./internal/controller/... ./internal/reporter/...` (fast — none of the new/modified tests require envtest; existing `internal/controller` Ginkgo specs run under `-short` by default per `suite_test.go:130`, which is what `go test` invokes without extra flags)
- **Per wave merge:** `make test` (full unit tier including `vet`/`fmt`/`manifests`/`generate` prerequisites)
- **Phase gate:** `make test` green, plus a manual `grep -rn "if.*[Vv]endor.*==.*\"anthropic\"\|if.*[Vv]endor.*==.*\"langgraph\"" internal/controller cmd/tide-reporter internal/reporter` returning zero hits outside `pkg/dispatch/vendor_capabilities.go` and the pre-existing `internal/subagent/anthropic/subagent.go:218` fail-fast check (which is a DIFFERENT, already-existing vendor-firewall pattern, not a new violation) — confirms D-01's "no per-runtime branch" criterion by direct source inspection, not just test-passing.

### Wave 0 Gaps

- [ ] `pkg/dispatch/vendor_capabilities_test.go` — covers ADAPT-01 criterion 1 (D-10)
- [ ] `internal/controller/reporter_jobspec_test.go` addition — covers ADAPT-01 criterion 1 flag-threading (D-11)
- [ ] `cmd/tide-reporter/main_test.go` additions (`TestParseFlags*` extension, a `TestRunTraceOnly_SkipsSynthesisWhenFlagSet`-style case) — covers ADAPT-01 criterion 2
- [ ] `cmd/tide-reporter/adapter_seam_test.go` — covers ADAPT-01 criterion 3 (D-09), the phase's headline proof
- [ ] Framework install: none — all frameworks already present and pinned; zero new test tooling needed

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | This phase touches no authentication surface |
| V3 Session Management | No | N/A |
| V4 Access Control | Yes | The flag's trust boundary (D-02): the reporter (running under the least-privilege `tide-reporter` SA) MUST NOT derive the skip decision from any pod-writable source (`in.json`, `events.jsonl`, or a self-reported `EnvelopeOut` field) — only from the manager-authored Job Arg. This is an access-control decision about WHO is authoritative for "did this dispatch self-instrument," not a traditional authn/authz surface. |
| V5 Input Validation | Yes | `flag.Bool`'s stdlib parser rejects malformed values for the new `--skip-message-spans`/`--emit-message-spans` flag automatically (same validation the existing `--trace-only`/`--traceparent` flags already receive — no new validation code needed) |
| V6 Cryptography | No | N/A |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Semi-trusted subagent pod spoofing "I already self-instrumented" to suppress its own conversation's synthesis (hiding LLM activity from Phoenix/audit) | Spoofing / Repudiation | D-02: the flag is computed and carried EXCLUSIVELY by the manager on the Job spec (Args), which the subagent pod cannot write to after Job creation — this is the SAME trust boundary the codebase already uses for `Provider` resolution generally (`ResolveProvider` runs manager-side; the subagent pod only receives the resolved `EnvelopeIn.Provider`, it never resolves its own). Anti-Pattern 4 in the milestone's own `ARCHITECTURE.md` names this exact threat and its mitigation explicitly — this phase implements that mitigation, it does not introduce the threat. |
| A future vendor's capability entry silently flipped to `true` without the corresponding runtime actually emitting spans (config drift → silent span loss) | Tampering (of config, not of a live request) | D-03's fail-closed default bounds the blast radius: `SelfInstruments` is a small, reviewable, compiled-in Go switch (not a runtime-configurable value, not read from a CRD/ConfigMap) — a bad entry requires a code change + review + redeploy, not a live config mutation. This phase does not add any external-facing mutability to the capability table. |

## Sources

### Primary (HIGH confidence — direct source reads this session)

- `internal/controller/dispatch_helpers.go` (full file read) — `ResolveProvider` (line 271), `spawnReporterIfNeeded` (line 93), `PlannerReconcilerDeps` (line 184), `levelOverrideKey` (line 240)
- `internal/controller/reporter_jobspec.go` (full file read) — `ReporterOptions` (line 74), `BuildReporterJob` (line 181), existing Args/Env conventions
- `internal/controller/task_controller.go` (lines 1030-1190, plus targeted greps) — `spawnTaskTraceReporterIfNeeded` (line 1057), `TaskReconcilerDeps` (line 93), call sites at lines 1124/1153, `ResolveProvider(project, "task", ...)` at line 827/1710
- `internal/controller/span_emission.go` (full file read) — `synthesizePlannerSpan` (line 136), confirming the `ResolveProvider(project, level, helmDefaults)` call at line 176 and the "second, envelope-independent call" precedent doc comment (lines 122-125)
- `internal/controller/milestone_controller.go`, `phase_controller.go`, `plan_controller.go`, `project_controller.go` (targeted reads around reporter-spawn blocks + `synthesizePlannerSpan` call sites) — confirmed exact level literals and inline-vs-helper spawn shapes
- `pkg/dispatch/provider.go` (full file read) — `ProviderSpec.Vendor` (line 40)
- `pkg/dispatch/provider_test.go` (full file read) — test-style precedent for `vendor_capabilities_test.go`
- `pkg/otelai/tracecontext.go` (full file read) — `ExtractRemoteParent` (line 93), `FormatTraceparent` (line 69)
- `internal/reporter/tracesynth.go` (full file read) — package doc, `EmitSpans` D-07 hardcoded-"anthropic" comment (line 613)
- `cmd/tide-reporter/main.go` (full file read) — `parseFlags` (line 113), `reporterConfig` (line 87), `run`/`runWithClient` (lines 159-293), `synthesizeSpans` (line 316)
- `cmd/tide-reporter/main_test.go` (targeted reads) — `installStubTracerProvider` (line 396), `writeTraceOnlyFixture` (line 417), `TestRunTraceOnly_EmitsSpans` (line 532)
- `internal/controller/reporter_jobspec_test.go` (targeted reads) — `TestBuildReporterJob_TraceparentArg` (line 146), full test-function inventory
- `internal/controller/task_traceonly_reporter_test.go` (header + partial read) — confirms Ginkgo `Label("envtest", "heavy")` pattern for spawn-site coverage
- `internal/controller/suite_test.go` (targeted grep) — confirms `testing.Short()` + `heavy` label skip logic (line 130)
- `internal/subagent/anthropic/subagent.go` (targeted read) — `vendorSentinel` fail-fast (line 61), `Run()`'s vendor check (line 218)
- `internal/reporter/materialize.go` (header read) — "no `internal/controller` back-edge" package-doc precedent
- `pkg/otelai/attrs.go`/`doc.go` (targeted reads) — D-07 `llm.system` caller-supplied comment (attrs.go:232)
- `internal/otelinit/provider_test.go` (targeted read) — `TestNoWithSamplerInSource` guard-test convention (line 113)
- `charts/tide/values.yaml` (grep) — confirmed no per-vendor image/selection surface exists to update
- `Makefile` (targeted read) — `test`/`test-heavy` target definitions (lines 84-98)
- `.planning/config.json` — confirmed `nyquist_validation: true`, no `security_enforcement` key (default enabled)

### Secondary (MEDIUM confidence)

- `.planning/research/ARCHITECTURE.md` §Pattern 4, §Component Responsibilities, §Suggested Build Order step 6 — the original design synthesis this phase verifies and refines; written 2026-07-15, BEFORE Phases 42-44 landed the concrete signatures this document pins
- `.planning/research/PITFALLS.md` §Pitfall 7 — double-instrumentation failure mode reasoning (inferred from general OTel env-carrier spec, not observed against a real LangGraph adapter, since none exists)

### Tertiary (LOW confidence)

- None — every claim in this document is either a direct codebase read (Primary) or explicit synthesis from the milestone research cross-checked against current source (Secondary, clearly labeled).

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new dependencies; every primitive used is already pinned and already used identically elsewhere in this exact codebase.
- Architecture: HIGH — all 5 call sites, their exact signatures, and their exact level-literal arguments were read directly from `main` this session, not inferred from the (pre-Phase-42-44) milestone research.
- Pitfalls: HIGH for the 4 pitfalls above (all codebase-grounded, verified via direct grep/read this session); the milestone research's Pitfall 7 (double-instrumentation) remains MEDIUM since no self-instrumenting runtime exists yet to observe against — but this phase's own D-09 contract test is explicitly designed to close that gap generically.

**Research date:** 2026-07-16
**Valid until:** Next codebase-touching phase in this milestone (Phase 46) — the call-site line numbers pinned here will drift the moment Phase 45's own diff lands; treat line numbers as "current as of this research," not durable across the phase's own implementation.
