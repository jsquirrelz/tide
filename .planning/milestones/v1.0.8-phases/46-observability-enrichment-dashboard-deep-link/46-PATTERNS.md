# Phase 46: Observability Enrichment + Dashboard Deep Link - Pattern Map

**Mapped:** 2026-07-17
**Files analyzed:** 17 (14 modified, 0 net-new Go files, 1 net-new TS file + 1 net-new TS test file, docs)
**Analogs found:** 17 / 17 (every file has a same-repo, same-phase-family analog — this phase is additive to infrastructure Phases 42-45 already built)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|
| `pkg/otelai/attrs.go` (+`SessionID`/`Metadata`/`Tags`) | utility (attribute helper) | transform | same file's `AgentInvocation`/`LLMIdentity`/`ArtifactPath`/`LLMSpanKind` helpers (lines 225-321) | exact |
| `pkg/otelai/attrs_test.go` (+3 tests) | test | transform | same file's `TestAgentInvocation`/`TestLLMIdentity`/`TestArtifactPath` (lines 100-268) | exact |
| `internal/controller/span_emission.go` (attribute block + D-02/D-03) | controller (span synthesis) | event-driven | itself — `synthesizePlannerSpan` lines 136-215, `traceparentForLevel` lines 232-247 | exact (self-modify) |
| `internal/controller/span_emission_unit_test.go` (+cases) | test | event-driven | `TestSynthesizePlannerSpanSucceededComplete` (lines 235-316), `TestTraceparentForLevel` (line 661) | exact |
| `internal/controller/reporter_jobspec.go` (`ReporterOptions` + Args) | controller (Job builder) | request-response | itself — `TraceParent`/`SkipMessageSpans` fields (lines 81-121) + Args assembly (lines 219-231) | exact (self-modify) |
| `internal/controller/reporter_jobspec_test.go` (+cases) | test | request-response | `TestBuildReporterJob_TraceParentArg` / `TestBuildReporterJob_SkipMessageSpansArg` (lines 165-230) | exact |
| `internal/controller/dispatch_helpers.go` (`spawnReporterIfNeeded` signature) | controller (Job spawn helper) | event-driven | itself — lines 98-146 | exact (self-modify) |
| `internal/controller/task_controller.go` (`spawnTaskTraceReporterIfNeeded` + wave-index read) | controller | event-driven | itself — lines 1057-1096 (`ReporterOptions{...}` call site) | exact (self-modify) |
| `internal/controller/{milestone,phase,plan,project}_controller.go` (call-site wiring) | controller | event-driven | `plan_controller.go:648` / `project_controller.go:1903` `ReporterOptions{...}` call sites | exact |
| `internal/reporter/tracesynth.go` (`EmitSpans` attribute block) | service (span emitter) | streaming | itself — `EmitSpans` lines 587-646 | exact (self-modify) |
| `internal/reporter/tracesynth_test.go` (+cases) | test | streaming | existing `EmitSpans` test table (same file) | exact |
| `cmd/tide-reporter/main.go` (`parseFlags`/`reporterConfig`/`synthesizeSpans`) | CLI entrypoint | request-response | itself — `parseFlags` lines 114-145, `synthesizeSpans` lines 320-364 | exact (self-modify) |
| `charts/tide/values.yaml` (`otel.tracesSamplerArg` flip + new `phoenix:` block) | config | batch (render-time) | same file's `prometheus:` block (lines 352-395) for the new-value-block shape; `otel:` block (lines 401-415) for the flip site | exact |
| `charts/tide/templates/deployment.yaml` (sampler unaffected — verify only) | config | batch | itself — lines 91-94 | exact |
| `charts/tide/templates/dashboard-deployment.yaml` (+`PHOENIX_BASE_URL` env) | config | batch | itself — `PROM_ENDPOINT`/`PROMETHEUS_ENABLED` block, lines 57-75 | exact |
| `hack/helm/assert-prometheus-env.py` → new `hack/helm/assert-phoenix-env.py` (or extend `assert-telemetry-render.sh`) | test (helm-template contract) | batch | `hack/helm/assert-prometheus-env.py` (whole file) + `hack/helm/assert-telemetry-render.sh` Permutations A/B/E/F | exact |
| `docs/observability.md` (sampler table + opt-down honesty + `phoenix.baseURL`) | docs | — | itself — table at lines 159-166 | exact |
| `cmd/dashboard/main.go` (+`phoenixBaseURLFromEnv`) | CLI entrypoint (config resolution) | request-response | itself — `telemetryEnabledFromEnv` lines 243-259, `Dependencies{...}` construction lines 159-169 | exact (self-modify) |
| `cmd/dashboard/router.go` (`Dependencies` + `ConfigHandler` wiring) | provider (DI wiring) | request-response | itself — `PrometheusEndpoint`/`TelemetryEnabled` fields (lines 74-95) + `configHandler := &dashboardapi.ConfigHandler{...}` (lines 212-215) | exact (self-modify) |
| `cmd/dashboard/api/config.go` (`ConfigHandler` + `configResponse.phoenixBaseURL`) | controller (REST handler) | request-response | itself — whole file (57 lines) | exact (self-modify) |
| `cmd/dashboard/api/projects.go` / `plans.go` / `tasks.go` (+trace-identity fields) | controller (REST handler) | CRUD (read-only) | `projects.go`'s `projectSummary`/`projectDetail`/`childRef` structs (lines 68-105) | exact |
| `dashboard/web/src/lib/phoenixLink.ts` (new) | utility (URL assembly) | transform | `dashboard/web/src/lib/pluralize.ts` / `clsx.ts` (small pure-function lib module shape) — no direct precedent, follows package conventions | role-match |
| `dashboard/web/src/lib/phoenixLink.test.ts` (new) | test | transform | `dashboard/web/src/lib/projects.test.ts` / `tasks.test.ts` (co-located `lib/*.test.ts` convention) | exact |
| `dashboard/web/src/lib/api.ts` (+`phoenixBaseURL` on config fetch / project payload types) | utility (typed REST client) | request-response | itself — `ProjectSummary`/`ProjectDetail` type mirrors (lines 30-54) | exact (self-modify) |
| `dashboard/web/src/components/NodeDetailPanel.tsx` (or `App.tsx`'s `nodePanelContent`) | component | request-response | `App.tsx` lines 606-655 (`nodePanelContent` construction — the D-09 mount point) | exact |
| `dashboard/web/src/components/TaskDetailDrawer.tsx` (+ Phoenix link row) | component | request-response | itself — `MetaRow` usage block lines 308-334, `MetaRow` helper lines 395-431 | exact (self-modify) |
| `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx` (+case) | test | request-response | itself — existing `describe("NodeDetailPanel composition...")` blocks | exact |
| `dashboard/web/src/components/__tests__/TaskDetailDrawer.test.tsx` (extend or new) | test | request-response | sibling `node-panel-integration.test.tsx` conventions (mock `../../lib/api`, `render`/`screen`/`within`) | role-match |

## Pattern Assignments

### `pkg/otelai/attrs.go` (utility, transform) — new `SessionID`/`Metadata`/`Tags` helpers

**Analog:** same file, `AgentInvocation`/`LLMIdentity`/`ArtifactPath`/`LLMSpanKind` (lines 225-321)

**Imports pattern** (lines 17-24, unchanged — no new imports needed beyond `encoding/json` for `Metadata`):
```go
package otelai

import (
	"fmt"

	semconv "github.com/Arize-ai/openinference/go/openinference-semantic-conventions"
	"go.opentelemetry.io/otel/attribute"
)
```

**Core pattern — single-attribute helper shape** (`ArtifactPath`, lines 261-263 — the shape `SessionID` copies):
```go
func ArtifactPath(path string) attribute.KeyValue {
	return attribute.String(keyArtifactPath, path)
}
```
`SessionID` is byte-for-byte this shape with `semconv.SessionID` (module-backed, not `tide.*`) as the key:
```go
func SessionID(projectUID string) attribute.KeyValue {
	return attribute.String(semconv.SessionID, projectUID)
}
```

**Pitfall 4 (CRITICAL — verified in RESEARCH.md, do not deviate):** `metadata` and `tag.tags` have DIFFERENT OTel encodings despite reading like siblings.
- `Metadata(map[string]string) (attribute.KeyValue, error)` — `json.Marshal` then `attribute.String(semconv.Metadata, string(b))`. This is the ONLY helper in the package that returns `(attribute.KeyValue, error)` instead of a bare value — `LLMInputMessages`/`TokenCount` never error today, so this is new shape; document why in the doc comment (JSON marshal of a `map[string]string]` cannot fail in practice but the signature should not silently swallow it).
- `Tags(tags ...string) attribute.KeyValue` — `attribute.StringSlice(semconv.TagTags, tags)` directly, NO `json.Marshal`. A `Tags()` helper that JSON-encodes is the exact regression Pitfall 4 warns about.

**Key-source enforcement (ATTR-03, load-bearing):** `TestKeysUseSemconvModule` (attrs_test.go lines 342-354) source-greps `attrs.go` for any hand-rolled `"llm.` / `"openinference.` / `"gen_ai.` / `"agent.` string literal outside a comment. `semconv.SessionID`, `semconv.Metadata`, `semconv.TagTags` MUST be used — verified present in `openinference-semantic-conventions@v0.1.1`. Do not hand-type `"session.id"` / `"metadata"` / `"tag.tags"` as literals.

**Discretion (per CONTEXT.md):** exact metadata JSON key names (level kind, level name, wave index, gate profile, failure-halt state) and the D-08 encoding for Project-level's absent gate-profile case (sentinel `"n/a"` or `"root"` — pick one, document in the helper's doc comment per Pitfall 5).

---

### `pkg/otelai/attrs_test.go` (test) — `TestSessionID`, `TestMetadata`, `TestTags`

**Analog:** `TestArtifactPath` (lines 229-237), `TestLLMIdentity` (lines 239-268)

**Core pattern:**
```go
// TestArtifactPath — single attribute.KeyValue (NOT a slice).
func TestArtifactPath(t *testing.T) {
	got := ArtifactPath("/workspace/envelopes/abc.jsonl")
	want := attribute.String("tide.artifact_path", "/workspace/envelopes/abc.jsonl")
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ArtifactPath = %v, want %v", got, want)
	}
}
```

**Pitfall 4's regression guard — the load-bearing new assertion for `TestTags`:**
```go
// Assert the TYPE is STRINGSLICE, not STRING (would mean someone JSON-encoded).
if got.Value.Type() != attribute.STRINGSLICE {
	t.Errorf("Tags().Value.Type() = %v, want attribute.STRINGSLICE", got.Value.Type())
}
```
And for `TestMetadata`, assert `.Value.Type() == attribute.STRING` and that the string round-trips as valid JSON (`json.Unmarshal` back into a map and compare).

---

### `internal/controller/span_emission.go` (controller, event-driven) — session/metadata/tags attribute block + D-02/D-03

**Analog:** itself, `synthesizePlannerSpan` (lines 136-215)

**D-03 fix — the load-bearing change, not "just add attributes":** locate the existing conditional token-count block:
```go
// span_emission.go:191-202 (current)
if envReadOK {
	promptTokens := out.Usage.InputTokens + out.Usage.CacheReadTokens + out.Usage.CacheCreationTokens
	span.SetAttributes(otelai.TokenCount(
		int(promptTokens),
		int(out.Usage.OutputTokens),
		int(out.Usage.CacheReadTokens),
		int(out.Usage.CacheCreationTokens),
	)...)
} else {
	span.SetAttributes(otelai.EnvelopeDegraded())
}
```
**CORRECTED AT PLANNING — 46-04-PLAN.md's `<planner_correction>` block is the authoritative instruction; it supersedes RESEARCH.md's Task-only scope (Summary, Pitfall 2, and the anti-pattern bullet).** DELETE the `otelai.TokenCount(...)` emission from ALL FIVE levels' AGENT spans, keeping the `EnvelopeDegraded` else-branch marker. The Task-only premise ("only Task has a sibling LLM-span source") was disproven by direct code read: `cmd/tide-reporter/main.go:229` calls `synthesizeSpans` on the combined-mode path too, so the reporter Jobs spawned at all four planner-level completions ALSO emit per-call LLM message spans (deliberate Phase 44 D-05 behavior). Per-call LLM spans are the sole `llm.token_count.*` carriers at every level; rolled-up totals on any AGENT span double-count in Phoenix's SpanCost rollup. See the plan's `<planner_correction>` for the full evidence chain the removal-site code comment must cite.

**D-02 fix — narrow, same-reconcile only (Pitfall 3 warns against over-scoping):**
```go
// traceparentForLevel — CURRENT (line 246): hardcoded true
return otelai.FormatTraceparent(traceID, spanIDFromHexOrZero(spanIDHex), true)
```
Research recommends extending `synthesizePlannerSpan`'s return to also carry the real `span.SpanContext().IsSampled()` bit (it already returns `(trace.SpanID, bool)` at line 214 — add a third `sampled bool` return), and threading that into the Task-level `traceparentForLevel` call at `task_controller.go`'s trace-only reporter spawn site ONLY. Do not add a persisted `{Level}TraceSampled` status field across all five `api/v1alpha3/*_types.go` — that is the explicitly-rejected scope (see RESEARCH.md "Alternatives Considered" + Pitfall 3).

**Where session.id/metadata/tags land:** immediately after the existing `span.SetAttributes(otelai.LLMIdentity(...)...)` call (line 189), before the token-count block:
```go
span.SetAttributes(otelai.SessionID(string(project.UID)))
// metadata, err := otelai.Metadata(map[string]string{...}); if err == nil { span.SetAttributes(metadata) }
// span.SetAttributes(otelai.Tags(...))
```
`project` is already in scope (parameter). Gate profile / failure-halt / wave-index values: Gate profile reads `project.Spec.Gates.{Milestone,Phase,Plan,Task}` (`api/v1alpha3/project_types.go:40-50`) keyed on `level`; failure-halt reads `project.Status.Conditions` for `tideprojectv1alpha3.ConditionFailureHalt` (`api/v1alpha3/shared_types.go:324`) plus `project.Spec.FailureProfile` (`FailureProfileStrict`/`FailureProfileConservative`, `shared_types.go:390/395`). Wave index (Task only, D-07): read from the Task CR's `owner.LabelWaveIndex` label (stamped by `project_controller.go`'s `stampGlobalTaskLabels`, lines 2569-2610) — this function does NOT have the Task object in scope today (it takes `project`, not `task`); the caller (`task_controller.go`'s `emitTaskSpanOnce`) has `task` and must pass the label value in, or `synthesizePlannerSpan`'s signature grows an optional `waveIndex string` parameter used only when `level == "task"`.

**Anti-pattern warning (explicit in RESEARCH.md):** do not forward `OTEL_TRACES_SAMPLER`/`_ARG` env to the reporter Job to "fix" D-02 — unnecessary, the reporter's own `otelinit` already defaults correctly once the incoming `--traceparent`'s sampled bit is correct.

---

### `internal/controller/span_emission_unit_test.go` (test) — Task-drops-TokenCount + SessionID/Metadata/Tags cases

**Analog:** `TestSynthesizePlannerSpanSucceededComplete` (lines 235-316) — the exact fixture/assertion shape to extend

**Test fixture conventions to reuse (verified, do not reinvent):**
- `exp := setupSpanExporter(t)` — in-memory span exporter fixture
- `spanEmissionFixtureProject(testProjectUID, "claude-test-model")` — Project fixture builder
- `attrValue(span.Attributes, key)` — attribute lookup helper (returns `(attribute.Value, bool)`)
- Assertion pattern: build a `map[attribute.Key]string` of `wantStringAttrs`, loop `attrValue` + compare

**New test — the D-03 regression guard, corrected at planning to cover ALL FIVE levels (46-04-PLAN.md `<planner_correction>` supersedes RESEARCH.md Pitfall 2's Task-only scope):**
```go
// TestSynthesizePlannerSpanOmitsTokenCount — 46 D-03 (all-five-level drop per
// 46-04-PLAN.md <planner_correction>): NO level's AGENT span may carry
// llm.token_count.* — per-call LLM spans are the sole SpanCost source.
// Loop every level in {project, milestone, phase, plan, task}.
func TestSynthesizePlannerSpanOmitsTokenCount(t *testing.T) {
	exp := setupSpanExporter(t)
	// ... build job/project/out fixtures identical to TestSynthesizePlannerSpanSucceededComplete ...
	_, gotOK := synthesizePlannerSpan(context.Background(), "task", project, ProviderDefaults{}, job, out, true, trace.SpanID{})
	span := exp.GetSpans()[0]
	if _, ok := attrValue(span.Attributes, "llm.token_count.prompt"); ok {
		t.Errorf("task-level span carries llm.token_count.prompt; want omitted (D-03 double-count fix)")
	}
}
```

---

### `internal/controller/reporter_jobspec.go` (controller, request-response) — `ReporterOptions` new fields + Args

**Analog:** itself — `TraceParent`/`SkipMessageSpans` fields (lines 81-121) and Args assembly (lines 219-231)

**Struct field pattern** (lines 115-121, the shape to copy for `SessionID`/`MetadataJSON`/`Tags`):
```go
// SkipMessageSpans (ADAPT-01/Phase 45): set true when ... Zero value
// (false) is the existing behavior, byte-identical pre-Phase-45: every
// vendor resolves false today (D-03 default-safe).
SkipMessageSpans bool
```

**Args assembly pattern** (lines 219-231 — 100% Args-based convention, Pitfall 3 in that file's own doc comment):
```go
// Phase 43 PROP-01: --traceparent Arg, not Env (Pitfall 3 — this file is
// 100% Args-based via stdlib flag; zero Env entries on the reporter container).
if opts.TraceParent != "" {
	args = append(args, "--traceparent="+opts.TraceParent)
}
// Phase 45 ADAPT-01/D-04: bareword flag (not "=value" — matches
// --trace-only's shape), appended only when true so absence resolves to
// synthesize (D-03).
if opts.SkipMessageSpans {
	args = append(args, "--skip-message-spans")
}
```
Per D-05/RESEARCH Pattern 2, new fields follow this EXACT shape:
```go
if opts.SessionID != "" {
	args = append(args, "--session-id="+opts.SessionID)
}
if opts.MetadataJSON != "" {
	args = append(args, "--metadata="+opts.MetadataJSON) // pre-JSON-encoded by the manager
}
if len(opts.Tags) > 0 {
	args = append(args, "--tags="+strings.Join(opts.Tags, ",")) // reporter splits on comma
}
```
**Never Env for these values** — `OTLPEndpoint` is the file's one documented Env exception (targets the reporter's own TracerProvider bootstrap, not per-span attribute values); session/metadata/tags are per-span attribute values, so they follow `TraceParent`'s Args precedent, not `OTLPEndpoint`'s.

**Both Job shapes:** per `SkipMessageSpans`'s doc comment ("rides both the materialization and trace-only shapes uniformly, D-05"), place new Args appends AFTER the `if opts.TraceOnly { ... } else { ... }` branch (line 202-218) so they apply uniformly — mirror `SkipMessageSpans`'s placement exactly (line 229).

---

### `internal/controller/reporter_jobspec_test.go` (test) — new field Args assertions

**Analog:** `TestBuildReporterJob_SkipMessageSpansArg` (lines 196-230+) — "present when true" / "absent when false" subtests

**Core pattern:**
```go
t.Run("present when set", func(t *testing.T) {
	opts := controller.ReporterOptions{
		ReporterImage: "ghcr.io/jsquirrelz/tide-reporter:v0.1.0-dev",
		TraceParent:   traceParent,
	}
	job := controller.BuildReporterJob(parent, project, "tide-projects", "task-uid-4", "Milestone", opts, scheme)
	args := job.Spec.Template.Spec.Containers[0].Args
	want := "--traceparent=" + traceParent
	var found bool
	for _, a := range args {
		if a == want {
			found = true
		}
	}
	if !found {
		t.Errorf("expected arg %q not present in %v", want, args)
	}
})
```
Extend for `--session-id=`, `--metadata=`, `--tags=` with matching "absent when empty/zero" subtests (mirrors `TestBuildReporterJob_TraceParentArg`'s "absent when empty" case, lines 184-193).

---

### `internal/controller/dispatch_helpers.go` / `task_controller.go` / `{milestone,phase,plan,project}_controller.go` — call-site wiring

**Analog:** `task_controller.go`'s `spawnTaskTraceReporterIfNeeded` (lines 1057-1096) — the most-recent, most fully-worked call site

**Core pattern** (lines 1079-1088 — resolve-then-construct-ReporterOptions):
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
Add `SessionID: string(project.UID)`, `MetadataJSON: <computed>`, `Tags: <computed>` to this literal and the four other `ReporterOptions{...}` literals (`dispatch_helpers.go:128`, `plan_controller.go:648`, `project_controller.go:1903`). `spawnReporterIfNeeded`'s signature (`dispatch_helpers.go:98-110`) currently takes 10 positional params ending `skipMessageSpans bool` — extending it with 3 more positional bools/strings is consistent with its existing shape, though a struct-param refactor is in scope if the planner judges the positional-param count is getting unwieldy (Claude's Discretion — not locked by CONTEXT.md).

---

### `internal/reporter/tracesynth.go` (service, streaming) — `EmitSpans` attribute block

**Analog:** itself — `EmitSpans` (lines 587-646)

**Core pattern** (lines 619-638 — the exact insertion point for session/metadata/tags, mirroring how `ArtifactPath`/`TimingSynthetic`/`ParseDegraded` markers are added):
```go
span.SetAttributes(otelai.LLMSpanKind())
span.SetAttributes(otelai.LLMIdentity("anthropic", call.Model)...)
span.SetAttributes(inputAttrs...)
span.SetAttributes(outputAttrs...)
span.SetAttributes(otelai.TokenCount(...)...)
span.SetAttributes(otelai.ArtifactPath(artifactPath))
span.SetAttributes(otelai.TimingSynthetic())
if call.Degraded || inputDegraded || outputDegraded {
	span.SetAttributes(otelai.ParseDegraded())
}
```
Add `span.SetAttributes(otelai.SessionID(sessionID))` and the metadata/tags equivalents in this block — `EmitSpans`'s signature (line 587: `func EmitSpans(ctx context.Context, tracer trace.Tracer, calls []CallSpan, artifactPath string) error`) needs new parameters (`sessionID string`, `metadataJSON string`, `tags []string`) threaded from `cmd/tide-reporter/main.go`'s `synthesizeSpans` call site (line 357: `reporter.EmitSpans(parentCtx, tracer, calls, artifactPath)`). **These MUST be identical values to what the SAME level's AGENT span carries** (RESEARCH.md Pattern 2: "Phoenix's `ProjectSession` groups purely on the `session.id` string match — inconsistent values across sibling spans would fragment the session view").

**TokenCount stays unchanged here** — `tracesynth.go`'s per-call `TokenCount` is the D-03-designated AUTHORITATIVE source at every level; `span_emission.go`'s AGENT spans drop `TokenCount` at all five levels (46-04-PLAN.md `<planner_correction>`).

---

### `cmd/tide-reporter/main.go` (CLI entrypoint) — new flags + `reporterConfig` fields

**Analog:** itself — `parseFlags` (lines 114-145), `reporterConfig` struct (lines 87-97), `synthesizeSpans` (lines 320-364)

**Core pattern** (lines 124-144 — the exact flag-then-struct-field shape):
```go
traceParent := fs.String("traceparent", "", "W3C traceparent for this level's own span (consumed starting Phase 44)")
skipMessageSpans := fs.Bool("skip-message-spans", false,
	"skip LLM message-array-span synthesis (self-instrumenting vendor; D-03 default-safe: absent = synthesize)")

if err := fs.Parse(args); err != nil {
	return reporterConfig{}, err
}

return reporterConfig{
	...
	TraceParent:      *traceParent,
	SkipMessageSpans: *skipMessageSpans,
}, nil
```
Add `sessionID := fs.String("session-id", "", ...)`, `metadataJSON := fs.String("metadata", "", ...)`, `tagsCSV := fs.String("tags", "", ...)` (comma-split per `reporter_jobspec.go`'s own comment: `"reporter splits on comma"`), mirrored into `reporterConfig` fields, then threaded into the `reporter.EmitSpans(...)` call at line 357 inside `synthesizeSpans` (lines 320-364).

**Pitfall 4 (Phase 43's own precedent, cited in that file's docstring):** an Args entry without a registered flag crash-loops every reporter Job in the cluster — the flag set and `BuildReporterJob`'s Args MUST stay in sync. Add the flag AND the `BuildReporterJob` arg in the SAME commit/task.

---

### `charts/tide/values.yaml` — sampler flip (D-01) + new `phoenix:` block (D-10)

**Analog for the flip:** itself, `otel:` block (lines 401-415)
```yaml
# Sampler is env-driven (NOT WithSampler in code, per Pitfall 24 mitigation):
# OTEL_TRACES_SAMPLER + OTEL_TRACES_SAMPLER_ARG. Defaults trace 10% of root
# spans (`parentbased_traceidratio` arg `0.1`).
otel:
  exporter:
    endpoint: ""
  tracesSampler: "parentbased_traceidratio"
  tracesSamplerArg: "0.1"
  serviceName: "tide-controller-manager"
```
Change `tracesSamplerArg: "0.1"` → `"1.0"` and REWRITE the comment block (D-01: "a silent leftover reading 'defaults to 10%' is a doc bug") — the comment currently says "Defaults trace 10% of root spans," which must become "Defaults trace 100% of root spans (v1.0.8 OBS-01); opt down for high-volume installs — see docs/observability.md's opt-down section for the sampled-flag coherence caveat (D-02)."

**Analog for the new `phoenix:` block:** itself, `prometheus:` block (lines 352-395) — same "umbrella toggle + endpoint + graceful-degradation doc comment" shape:
```yaml
prometheus:
  enabled: false
  endpoint: ""
  # Empty (default) => graceful degradation: ...
```
New block (D-10, "empty default → SPA renders no link at all"):
```yaml
phoenix:
  # Base URL of a self-hosted Arize Phoenix instance (Phase 47 owns install
  # docs). Empty (default) => the dashboard's GET /api/v1/config reports
  # phoenixBaseURL="" and the SPA renders no deep-link affordance at all —
  # no dead buttons (OBS-04 requirement letter).
  baseURL: ""
```

---

### `charts/tide/templates/dashboard-deployment.yaml` — `PHOENIX_BASE_URL` env (D-10)

**Analog:** itself — `PROM_ENDPOINT` conditional block (lines 57-68), verbatim template per RESEARCH.md Pattern 3

**Core pattern** (lines 64-68):
```yaml
{{- if .Values.prometheus.endpoint }}
- name: PROM_ENDPOINT
  value: {{ quote .Values.prometheus.endpoint }}
{{- end }}
```
New (byte-for-byte shape swap):
```yaml
{{- if .Values.phoenix.baseURL }}
- name: PHOENIX_BASE_URL
  value: {{ quote .Values.phoenix.baseURL }}
{{- end }}
```
Note: `phoenixBaseURL` does NOT need `PROMETHEUS_ENABLED`'s always-rendered umbrella-key twin (RESEARCH.md's own annotation: "Empty string IS the 'no link' sentinel — no separate bool needed, unlike telemetryEnabled's three-state logic, because there's no legacy chart to fall back for"). One env var, one conditional, done.

---

### `hack/helm/assert-prometheus-env.py` / `assert-telemetry-render.sh` — new Phoenix render assertion

**Analog:** `hack/helm/assert-prometheus-env.py` (whole file, 128 lines) — this IS the "helm-template contract test" family referenced in the phase brief (`test/integration/kind/` has NO chart-env-value-pinning Go tests; the real gate lives here + in `.github/workflows/ci.yaml`)

**Core pattern** (the whole script's shape — locate the container, collect matching env entries, assert presence/absence/value):
```python
for doc in docs:
    if doc.get("kind") != "Deployment":
        continue
    containers = doc.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
    for container in containers:
        if container.get("name") == "dashboard":
            found_dashboard_container = True
            dashboard_env = container.get("env") or []
...
prom_entries = [e for e in dashboard_env if e.get("name") == "PROM_ENDPOINT"]
```
Either extend this script to also accept `--env-name PHOENIX_BASE_URL`, or add a sibling `assert-phoenix-env.py` with `PROM_ENDPOINT` replaced by `PHOENIX_BASE_URL` throughout. For the sampler flip (D-01), extend `assert-telemetry-render.sh`'s Permutation-A/E shape (grep `helm template` stdout for `name: OTEL_TRACES_SAMPLER_ARG` followed by `value: "1.0"` on the default render) — mirrors Permutation E's exact grep idiom (lines 182-186 of that file):
```bash
if ! echo "${RENDER_E}" | grep -A1 -E '^[[:space:]]*-?[[:space:]]*name:[[:space:]]*PROMETHEUS_ENABLED[[:space:]]*$' \
    | grep -qE '^[[:space:]]*value:[[:space:]]*"false"[[:space:]]*$'; then
  die "..."
fi
```

---

### `cmd/dashboard/main.go` / `router.go` / `api/config.go` — `phoenixBaseURL` config chain (D-10)

**Analog:** the `telemetryEnabled`/`PrometheusEndpoint` chain verbatim (RESEARCH.md Pattern 3, "byte-for-byte precedent")

**`main.go` resolution** (lines 250-259, the pattern `phoenixBaseURLFromEnv` copies — though simpler, single-state, per RESEARCH.md's note):
```go
func telemetryEnabledFromEnv() bool {
	switch os.Getenv("PROMETHEUS_ENABLED") {
	case "true":
		return true
	case "false":
		return false
	default:
		return os.Getenv("PROM_ENDPOINT") != ""
	}
}
```
New (simpler — no three-state legacy fallback needed):
```go
func phoenixBaseURLFromEnv() string {
	return os.Getenv("PHOENIX_BASE_URL")
}
```
Wire into the `Dependencies{...}` construction (line 159-169) alongside `PrometheusEndpoint: os.Getenv("PROM_ENDPOINT")`.

**`router.go` `Dependencies` field + `ConfigHandler` wiring** (lines 74-95, 212-215):
```go
// PrometheusEndpoint is the server-side Prometheus base URL the PromQL
// proxy forwards to ...
PrometheusEndpoint string
...
configHandler := &dashboardapi.ConfigHandler{
	TelemetryEnabled: deps.TelemetryEnabled,
	Log:              deps.Log,
}
```
Add `PhoenixBaseURL string` field to `Dependencies` and `PhoenixBaseURL: deps.PhoenixBaseURL` to the `ConfigHandler{...}` literal.

**`api/config.go`** — whole file is the analog for itself:
```go
type ConfigHandler struct {
	TelemetryEnabled bool
	Log logr.Logger
}
type configResponse struct {
	TelemetryEnabled bool `json:"telemetryEnabled"`
}
func (h *ConfigHandler) Get(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(configResponse{TelemetryEnabled: h.TelemetryEnabled}); err != nil {
		h.Log.Error(err, "failed to encode config response")
	}
}
```
Add `PhoenixBaseURL string` to both the handler struct and `configResponse` (additive JSON field only — "locked wire contract" per CONTEXT.md D-10, `telemetryEnabled` semantics stay untouched). Where trailing-slash normalization happens is Claude's Discretion (D-10) — RESEARCH.md's `phoenixLink.ts` example does it client-side (`baseURL.replace(/\/$/, "")`); if the planner picks server-side instead, do it here, not in both places.

---

### `cmd/dashboard/api/{projects,plans,tasks}.go` — trace-identity fields (D-11)

**Analog:** `projects.go`'s `projectSummary`/`projectDetail`/`childRef` structs (lines 68-105) — plain-JSON-no-envelope struct shape

**Core pattern** (lines 68-79):
```go
type projectSummary struct {
	Name                 string             `json:"name"`
	Namespace            string             `json:"namespace"`
	Phase                string             `json:"phase"`
	ActiveMilestoneCount int                `json:"activeMilestoneCount"`
	Budget               budgetSummary      `json:"budget"`
	BlockingConditions   []projectCondition `json:"blockingConditions"`
}
```
Add trace-identity fields following this flat-JSON convention — e.g. `TraceID string \`json:"traceId,omitempty"\`` (computed server-side via `otelai.TraceIDFromUID(project.UID)`, zero K8s import needed per that function's doc comment) and per-level `SpanID string \`json:"spanId,omitempty"\`` (read directly from the already-cached CRD's `{Level}TraceSpanID` status field — zero new API calls, the informer cache already has it). Confirmed via grep: **zero `TraceSpanID` references exist anywhere in `cmd/dashboard/` today** — this is genuinely new plumbing, not a rename.

**`dashboard/web/src/lib/api.ts` mirror convention** (lines 30-54 — "TypeScript types here mirror the Go struct fields verbatim so any backend rename surfaces as a compile-time error"):
```typescript
/** Mirrors cmd/dashboard/api/projects.go::projectSummary. */
export type ProjectSummary = {
  name: string;
  namespace: string;
  ...
};
```
Add `traceId?: string` / `spanId?: string` fields to the corresponding TS types (`ProjectDetail`, and the plan/task card types) mirroring whatever field names land server-side.

---

### `dashboard/web/src/lib/phoenixLink.ts` (new file) — URL assembly, the ONE place (D-11/D-12)

**No direct same-shape analog** — this is genuinely new pure-function logic. Follows the package's small-pure-function `lib/*.ts` module convention (`pluralize.ts`, `clsx.ts`).

**Exact shape per RESEARCH.md Code Examples (D-12, Phoenix ≥14.2.0 shareable-URL redirect family — supersedes the milestone research's `/projects/{project}/traces/{trace_id}` prior):**
```typescript
export function phoenixTraceURL(baseURL: string, traceId: string): string {
  return `${baseURL.replace(/\/$/, "")}/redirects/traces/${traceId}`;
}

export function phoenixSpanURL(baseURL: string, spanId: string): string {
  return `${baseURL.replace(/\/$/, "")}/redirects/spans/${spanId}`;
}
```
Security note (RESEARCH.md Security Domain): trace/span IDs are server-derived (never raw user input) but defensively wrap interpolated segments in `encodeURIComponent` — hex IDs are safe today but the helper shouldn't assume that forever.

**Both `NodeDetailPanel`/`App.tsx` content AND `TaskDetailDrawer.tsx` import from this ONE file** — do not reimplement the template string in both components (D-11's explicit anti-pattern).

---

### `dashboard/web/src/lib/phoenixLink.test.ts` (new file)

**Analog:** `dashboard/web/src/lib/projects.test.ts` / `tasks.test.ts` (co-located `lib/*.test.ts`, Vitest, no React Testing Library needed for pure functions)

Assert: trailing-slash normalization (`"http://phoenix/" ` vs `"http://phoenix"` both produce the same URL), exact `/redirects/traces/{id}` and `/redirects/spans/{id}` path shape, `encodeURIComponent` applied to the ID segment.

---

### `dashboard/web/src/components/NodeDetailPanel.tsx` mount + `App.tsx`'s `nodePanelContent` (D-09, project/milestone/phase/plan)

**Analog:** `App.tsx` lines 606-655, the exact D-09 mount point (comment: `"Plan 37-08: build the NodeDetailPanel content for the selected node."`)

**Core pattern** (lines 606-655 — conditional content construction per `selectedNode.kind`):
```tsx
let nodePanelContent: React.ReactNode = null;
if (selectedNode) {
  if (selectedNode.kind === "project") {
    const summary = projects.find((p) => p.name === selectedNode.name) ?? null;
    nodePanelContent = (
      <ProjectSettingsPanel
        projectName={selectedNode.name}
        ...
      />
    );
  } else {
    ...
    nodePanelContent = (
      <>
        <div className="min-h-0 flex-1">
          <ArtifactViewer ... />
        </div>
        {gateParked && <ApproveStrip projectName={selectedProject ?? ""} />}
      </>
    );
  }
}
```
Insert a `<PhoenixTraceLink traceId={...} spanId={...} baseURL={phoenixBaseURL} />` (or equivalent) into BOTH branches — the project branch and the milestone/phase/plan `ArtifactViewer` branch — reading `traceId`/`spanId` off whatever payload shape D-11 lands (`summary`/`projectDetail`/a new per-node lookup). `phoenixBaseURL` itself comes from the `GET /api/v1/config` fetch (mirror `TelemetryView.tsx`'s one-shot fetch pattern, lines 1107-1120, `const res = await fetch("/api/v1/config")`).

**No `ExternalLink` icon precedent exists yet in the SPA** (verified: zero `ExternalLink` usages in `dashboard/web/src/`) — `lucide-react` `^1.16.0` is already a dependency (confirmed in RESEARCH.md's Standard Stack) so import `ExternalLink` fresh; no new npm package.

---

### `dashboard/web/src/components/TaskDetailDrawer.tsx` (D-09 correction — Task nodes do NOT share NodeDetailPanel)

**Analog:** itself — `MetaRow` usage block (lines 308-334) + `MetaRow` helper (lines 395-431)

**Core pattern** (lines 308-334, the Metadata grid `<dl>` + `MetaRow` rows):
```tsx
<dl className="grid grid-cols-2 gap-x-4 gap-y-3 px-6 pb-4" data-testid="drawer-metadata" style={{ fontSize: "13px" }}>
  <MetaRow label="Namespace" value={task.namespace} mono />
  <MetaRow label="Attempt" value={`${task.attempt} of ${task.attemptMax}`} />
  <MetaRow label="Pod name" value={task.podName} mono />
  <MetaRow label="Exit code" value={task.exitCode === null ? "—" : String(task.exitCode)} mono />
  <MetaRow label="Wave index" value={String(task.waveIndex)} />
  <MetaRow label="Scheduled at" value={task.scheduledAt} mono />
  <MetaRow label="Envelope path" value={task.envelopePath} mono title={task.envelopePath} truncate />
</dl>
```
`MetaRow` (lines 395-431) is a plain label/value pair, NOT a link — a Phoenix deep link needs either a new `MetaRow`-adjacent link row (below the `<dl>`, alongside the "Open log stream" button styling at lines 371-389) or a dedicated link component reusing the SAME `<PhoenixTraceLink>`/helper the NodeDetailPanel branch uses. Add `traceId`/`spanId` fields to `TaskDetailData` (line 45-62) — the type currently has no trace fields; extend it and thread the values from wherever the drawer's data-fetch populates `TaskDetailData` today.

**This is Pitfall 1's correction — mandatory, not optional.** RESEARCH.md verified `TaskDetailDrawer` predates `NodeDetailPanel` and was never unified; `NodeDetailPanel`'s own `PlanningNodeKind` type (line 40 of that file) is `"project" | "milestone" | "phase" | "plan"` — Task is structurally excluded. A plan touching only `NodeDetailPanel.tsx` silently drops Task-level deep links, the single most-clicked node.

---

### `dashboard/web/src/components/__tests__/node-panel-integration.test.tsx` (extend)

**Analog:** itself — existing `describe("NodeDetailPanel composition (37-08 App assembly)", ...)` blocks (lines 51-90+)

**Core pattern** (the mock-then-render-then-assert shape):
```tsx
vi.mock("../../lib/api", () => ({
  fetchProjectSettings: (...args: unknown[]) => mockSettings(...args),
  fetchNodeArtifacts: (...args: unknown[]) => mockArtifacts(...args),
}));
...
it("project → hosts ProjectSettingsPanel inside the dialog chrome", async () => {
  mockSettings.mockResolvedValue(SETTINGS);
  render(<ToastProvider><NodeDetailPanel open kind="project" name="my-project" onClose={() => undefined}>...</NodeDetailPanel></ToastProvider>);
  const panel = await screen.findByTestId("node-detail-panel");
  expect(within(panel).getByText("project/my-project")).toBeInTheDocument();
});
```
Add a case asserting the Phoenix link renders when `phoenixBaseURL`/`traceId` are present and is ABSENT when `phoenixBaseURL` is empty (mirrors `TelemetryView.tsx`'s disabled-by-config test convention — no dead buttons).

---

### `dashboard/web/src/components/__tests__/TaskDetailDrawer.test.tsx` (extend or create — Pitfall 1's mandatory second surface)

**Analog:** `node-panel-integration.test.tsx`'s render/assert conventions, applied to `TaskDetailDrawer`'s own existing test file if one exists (grep the directory at plan-authoring time — confirm whether `TaskDetailDrawer.test.tsx` already exists before assuming "new file")

Same link-render/hide-on-empty-config cases as the NodeDetailPanel test, mounted via `<TaskDetailDrawer taskName="t1" task={TASK_DETAIL_DATA_FIXTURE} onClose={...} onOpenLogStream={...} />`.

---

### `docs/observability.md` — sampler table + opt-down honesty + `phoenix.baseURL` doc

**Analog:** itself — table at lines 159-166
```
| `OTEL_TRACES_SAMPLER_ARG`        | `otel.tracesSamplerArg`          | `0.1`                            |
```
Update the default column to `1.0`. Per D-02's finding (Pitfall 3), the opt-down section must state HONESTLY (not imply coherent per-span ratio sampling): "at ratios below 1.0, only the Project-level root span is gated by the ratio sampler; once a run is sampled, every descendant level's AGENT spans export in full — the ratio controls run-level visibility, not per-span volume. If the root is NOT sampled, descendant spans still export as an orphaned/rootless trace fragment in Phoenix." Add a `phoenix.baseURL` row/section documenting the new chart value and its "empty = no deep link" degradation.

## Shared Patterns

### Semconv-module-backed keys (ATTR-03)
**Source:** `pkg/otelai/attrs.go` lines 84-108 (the `tide.*` hand-rolled bucket) + `TestKeysUseSemconvModule` (attrs_test.go lines 330-354)
**Apply to:** `SessionID`/`Metadata`/`Tags` helpers — MUST resolve keys from `semconv.SessionID`/`semconv.Metadata`/`semconv.TagTags`, never hand-rolled string literals. This is a compile-time-adjacent CI gate (source-grep test), not a style preference.

### Manager-authored transport, never pod-writable data (D-05)
**Source:** `internal/controller/reporter_jobspec.go` lines 81-121 (`TraceParent`/`SkipMessageSpans` doc comments) + Phase 45 D-02's trust posture
**Apply to:** every new `ReporterOptions` field carrying session/metadata/tags values — computed by the reconciler (which has `project`, `Gates`, `FailureProfile`, wave-index label all in hand), rendered as CLI Args, never read from `in.json`/PVC data the subagent pod can write.

### `telemetryEnabled` config chain, applied verbatim (D-10)
**Source:** `charts/tide/templates/dashboard-deployment.yaml` lines 57-75, `cmd/dashboard/main.go` lines 243-259, `cmd/dashboard/router.go` lines 74-95/212-215, `cmd/dashboard/api/config.go` whole file
**Apply to:** `phoenix.baseURL` chart value → `PHOENIX_BASE_URL` env → `main.go` resolution → `Dependencies` → `ConfigHandler` → `/api/v1/config` `phoenixBaseURL` field → SPA one-shot fetch → render/hide. Byte-for-byte precedent per RESEARCH.md, not just similar shape.

### Observability never gates (Phase 42 D-04 / 44 D-10)
**Source:** `internal/controller/task_controller.go`'s `spawnTaskTraceReporterIfNeeded` doc comment (lines 1029-1056: "every failure here logs and continues, never a requeue error")
**Apply to:** every new attribute-emission and deep-link code path — a missing wave-index label, empty `phoenixBaseURL`, or malformed metadata map degrades to absent-attribute/no-link, NEVER an error that blocks Task/Plan/Phase/Milestone/Project completion.

### Marker-gated at-most-once span emission (Pitfall 3, RESEARCH.md)
**Source:** `internal/controller/span_emission.go`'s `synthesizePlannerSpan` doc comment (lines 88-101, "mark-then-emit ordering")
**Apply to:** this phase adds NO new emission sites and NO new markers — enrichment attributes ride the EXISTING marker-gated call in `span_emission.go` and the EXISTING per-call loop in `tracesynth.go`'s `EmitSpans`. Do not introduce a second `SetAttributes` call site outside these two functions.

## No Analog Found

None — every file in this phase's scope has at least a role-match analog in the same repo, mostly from Phases 42-45's own infrastructure (the closest possible analog: the same file, one phase earlier). `dashboard/web/src/lib/phoenixLink.ts` is the one genuinely new pure-logic file; it follows the package's small-module convention (`pluralize.ts`/`clsx.ts`) rather than copying a specific sibling's internals.

## Metadata

**Analog search scope:** `pkg/otelai/`, `internal/controller/` (span_emission.go, reporter_jobspec.go, dispatch_helpers.go, task_controller.go, project_controller.go, plan_controller.go), `internal/reporter/`, `cmd/tide-reporter/`, `cmd/dashboard/` (main.go, router.go, api/), `charts/tide/` (values.yaml, templates/), `hack/helm/`, `dashboard/web/src/` (components/, lib/), `docs/observability.md`, `api/v1alpha3/` (project_types.go, shared_types.go)
**Files scanned:** ~30 read directly (full or targeted-range) this session, plus grep sweeps across `test/integration/kind/`, `hack/helm/`, and `dashboard/web/src/`
**Pattern extraction date:** 2026-07-17
