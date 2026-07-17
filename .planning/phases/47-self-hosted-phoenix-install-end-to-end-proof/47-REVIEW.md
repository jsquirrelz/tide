---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
reviewed: 2026-07-17T00:00:00Z
depth: standard
files_reviewed: 20
files_reviewed_list:
  - charts/tide/templates/dashboard-deployment.yaml
  - charts/tide/templates/deployment.yaml
  - charts/tide/templates/NOTES.txt
  - charts/tide/values.yaml
  - cmd/manager/main.go
  - docs/INSTALL.md
  - docs/observability.md
  - hack/helm/assert-otlp-headers-env.py
  - hack/helm/assert-telemetry-render.sh
  - hack/helm/augment-tide-chart.sh
  - hack/helm/tide-values.yaml
  - internal/controller/dispatch_helpers.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/reporter_jobspec_test.go
  - internal/controller/reporter_jobspec.go
  - internal/controller/task_controller.go
  - Makefile
  - images/tide-reporter/Dockerfile
findings:
  critical: 2
  warning: 3
  info: 2
  total: 7
status: issues_found
---

# Phase 47: Code Review Report

**Reviewed:** 2026-07-17T00:00:00Z
**Depth:** standard
**Files Reviewed:** 20 (+1 test file)
**Status:** issues_found

## Summary

Reviewed the Phase 47 diff: `OTLPHeaders` threading from manager env through
`PlannerReconcilerDeps`/`TaskReconcilerDeps` into all five reporter-spawn
sites, the chart's guarded `OTEL_EXPORTER_OTLP_HEADERS` env on the
manager+dashboard Deployments (via the `augment-tide-chart.sh` pipeline), the
`NOTES.txt` tracing-dark nudge and its two new offline render gates, the
Phoenix install docs, and the `tide-reporter` Dockerfile COPY-list fix.

The OTLPHeaders threading itself is clean and uniform across all five spawn
sites (project/milestone/phase/plan/task), the chart wiring is
Secret-sourced with a comprehensive assertion script, and the Dockerfile fix
is verified complete against the reporter's actual import closure.

Two real defects surfaced during this review, both bearing directly on the
phase's own live-proof findings:

1. **A pre-existing, TTL-GC re-entry bug in the reporter-Job spawn gate
   causes duplicate reporter dispatch and divergent `ReporterOptions`** —
   this is the concrete mechanism behind the phase's already-known "115/386
   spans enriched, project-level reporter dispatching ×2" live-proof finding
   (CR-01).
2. **Phase 47's own OTLP-headers change forwards a live auth credential as a
   plaintext literal into the reporter Job's PodSpec**, widening its
   exposure to any principal with `get`/`list` on Jobs in the project
   namespace — directly contradicting both the chart's own "never a literal
   value" invariant (enforced for the Deployments) and this codebase's
   established pattern of never placing a raw provider credential in a
   Job/Pod spec (CR-02).

## Critical Issues

### CR-01: Reporter Job's 300s TTL-GC reopens the "first completion" spawn gate — duplicate reporter dispatch (explains the live-proof finding of partial/divergent span enrichment)

**File:** `internal/controller/reporter_jobspec.go:318` (`ttlVal := int32(300)` / `TTLSecondsAfterFinished: &ttlVal`)
**Also:** `internal/controller/project_controller.go:1898-1942`, `internal/controller/dispatch_helpers.go:80-141` (`spawnReporterIfNeeded`, used by `milestone_controller.go:653` and `phase_controller.go:606`), `internal/controller/plan_controller.go:645-684`, `internal/controller/task_controller.go:1094-1121` (`spawnTaskTraceReporterIfNeeded`)

**Issue:** Every one of the five reporter-spawn sites gates the `Create` on a
`Get`-by-deterministic-name → `NotFound` → spawn check. The reporter Job
carries `TTLSecondsAfterFinished: 300` (`reporter_jobspec.go:318`), so once
it completes and 300 seconds elapse, Kubernetes garbage-collects it — after
which the *same* `Get`-by-name check on the *next* reconcile again returns
`NotFound`, and the spawn site re-`Create`s a **second** reporter Job under
the same deterministic name with **freshly recomputed** `ReporterOptions`
(`SessionID`/`MetadataJSON`/`Tags` via `buildLevelEnrichment`, and
`TraceParent`). This re-dispatches the reporter binary against the same
`out.json`/`events.jsonl`, re-synthesizing and re-exporting the level's LLM
message spans a second time.

This exact re-entry window is **already known and explicitly documented in
this codebase** — but only for its effect on budget rollup, which was fixed
with a durable per-attempt marker:

- `project_controller.go:1953-1957`: *"The old isFirstCompletion signal
  (reporter-Job-IsNotFound) flips true again after the reporter Job's 300s
  TTL expires, causing double-count on halt→resume. The PlannerRolledUpUID
  marker survives halt ... and remains the authoritative idempotency
  guard."*
- Identical comments exist at `milestone_controller.go:673-675`
  (`MilestoneRolledUpUID`), `phase_controller.go:623-625`
  (`PhaseRolledUpUID`), and `plan_controller.go:689-693`
  (`PlanRolledUpUID`).

Each of those four sites added a **durable `.status` marker** so budget
rollup happens exactly once regardless of TTL-GC. **No equivalent durable
marker exists for the reporter-Job spawn itself** — the spawn is still
governed purely by Job-name existence, which is exactly the signal the
codebase's own comments say is unreliable across a TTL-GC window.

This is the concrete mechanism behind Phase 47's own live-proof finding
(`47-EVIDENCE.md` §6.2): *"Only 115/386 LLM spans carry session.id +
metadata.* + tag.tags ... the runlog independently records the project-level
[...] reporter dispatching ×2."* Any condition that keeps a Project/
Milestone/Phase/Plan reconciling for more than 5 minutes after its planner
Job completes (the evidence file names Defect #1's boundary-push retry storm
as the likely amplifier, but any sustained watch/requeue traffic reaches the
same window) reopens the spawn gate and produces a second, independently-
computed reporter dispatch — hence spans split between two populations with
different enrichment.

**Fix:** Add a durable per-attempt "reporter spawned" marker to each level's
`.status` (mirroring `*RolledUpUID`), keyed on the planner Job UID (or
`completedJob.UID` for the Task trace-only path), and gate `Create` on that
marker instead of — or in addition to — the `Get`-by-name check:

```go
// e.g. project_controller.go, mirroring PlannerRolledUpUID:
if project.Status.Reporter.SpawnedUID != plannerJobName {
    // ... build ReporterOptions + Create ...
    // then durably patch project.Status.Reporter.SpawnedUID = plannerJobName
}
```

Apply the same pattern at all five spawn sites (`project_controller.go`,
`dispatch_helpers.go`'s `spawnReporterIfNeeded` used by milestone/phase, `plan_controller.go`,
and `task_controller.go`'s `spawnTaskTraceReporterIfNeeded`).

---

### CR-02: OTLP auth header is forwarded to the reporter Job as a plaintext literal, contradicting the codebase's own "never a literal value" secret-handling invariant

**File:** `internal/controller/reporter_jobspec.go:299-307`
**Also:** `internal/controller/reporter_jobspec.go:104-110` (doc comment), `charts/tide/values.yaml:419-425`, `docs/observability.md:170-179`

**Issue:** The chart-level wiring is correct and Secret-sourced: both the
manager and dashboard Deployments receive `OTEL_EXPORTER_OTLP_HEADERS` via
`valueFrom.secretKeyRef` (`charts/tide/templates/deployment.yaml:98-106`,
`charts/tide/templates/dashboard-deployment.yaml:57-65`), and
`hack/helm/assert-otlp-headers-env.py` correctly asserts this shape and
fails on any literal `value` field.

But the manager then forwards its own **already-resolved** header value
(`os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")` in `cmd/manager/main.go:289`) into
every reporter Job it spawns as a **plaintext `corev1.EnvVar.Value`**, not a
Secret reference:

```go
// reporter_jobspec.go:299-307
if opts.OTLPEndpoint != "" {
    env = []corev1.EnvVar{
        {Name: "OTEL_EXPORTER_OTLP_ENDPOINT", Value: opts.OTLPEndpoint},
        {Name: "OTEL_BSP_MAX_EXPORT_BATCH_SIZE", Value: "6"},
    }
    if opts.OTLPHeaders != "" {
        env = append(env, corev1.EnvVar{Name: "OTEL_EXPORTER_OTLP_HEADERS", Value: opts.OTLPHeaders})
    }
}
```

This means the decoded `Authorization: Bearer <phoenix-token>` string is
written verbatim into the reporter `batchv1.Job`'s spec, in the *project*
namespace, readable by anyone with `get`/`list` on `jobs` in that namespace —
**no `get` on `Secrets` required**. The code and doc comments justify this
with a threat-model argument (`reporter_jobspec.go:104-110`,
`docs/observability.md:176-179`): *"Job-read RBAC in a project namespace
already implies access to tide-secrets' ANTHROPIC_API_KEY, a strictly more
sensitive credential."* This equivalence does not hold: reading a Job spec
that references a Secret via `envFrom`/`secretKeyRef` reveals only the
*Secret's name*, never its decoded value — you need separate `get` on
`Secrets` (and to decode it) to obtain the actual key. This code instead
writes the *decoded* value directly, which is a strictly larger exposure
than a `secretKeyRef`, not an equivalent one.

This also breaks with this codebase's own established pattern for every
other credential it handles. `internal/dispatch/podjob/jobspec.go:343-356`
threads `ANTHROPIC_API_KEY` only via `EnvFrom: SecretRef`, and the subagent
container never even receives the raw key — only a short-lived HMAC-signed
token (`jobspec.go:384-385,443-444`: *"D-C4: raw API key is NOT in
subagent's env or EnvFrom"*). TIDE goes out of its way, via an entire
credproxy-sidecar/signed-token architecture, to avoid ever placing a raw
secret value in a Job/Pod spec. The Phase 47 change reintroduces exactly the
pattern that architecture exists to prevent, for the credential that gates
access to a self-hosted Phoenix instance holding — per this same phase's own
docs (`docs/observability.md:255-257`) — "full (redacted-but-substantial)
prompt/completion history."

**Fix:** Mirror the Secret across project namespaces (the same pattern
`docs/INSTALL.md` already documents for `tide-signing-key`) and have the
reporter Job reference it via `valueFrom.secretKeyRef` instead of a literal,
or — if the cross-namespace mirror is out of scope for this phase — narrow
the claim in the doc/code comments to accurately describe the actual
exposure delta instead of asserting a false equivalence, and scope the
Phoenix token to the minimum practical TTL/permission in the install docs.

## Warnings

### WR-01: `hack/helm/assert-otlp-headers-env.py` hard-requires both `manager` and `dashboard` containers, failing spuriously against a supported `dashboard.enabled=false` render

**File:** `hack/helm/assert-otlp-headers-env.py:32,88-96`
**Issue:** `CONTAINER_NAMES = ("manager", "dashboard")` is treated as
mandatory — `_find_containers` builds a `missing` list against both names
and the script exits 1 if either is absent. `dashboard.enabled=false` is a
documented, supported install mode (`values.yaml:322-326`: *"operators who
want the controller-only install set `--set dashboard.enabled=false`"*),
and `dashboard-deployment.yaml:1` wraps the entire Deployment in `{{- if
.Values.dashboard.enabled }}`. Run this script by hand against such a
render (outside the Makefile target, which always forces
`--set dashboard.enabled=true`) and it fails with "no Deployment with a
container named ['dashboard']" even though the manager's header env is
perfectly correct.
**Fix:** Treat `dashboard` as present-if-rendered rather than mandatory —
e.g. only require it in `CONTAINER_NAMES` when the caller passes a flag (or
detect `dashboard.enabled` from the rendered manifest set) — so the script
degrades gracefully on a controller-only render instead of asserting a
container that was never supposed to exist.

### WR-02: `assert-telemetry-render.sh` permutations are out of lettered order (I inserted before H), hurting future maintainability

**File:** `hack/helm/assert-telemetry-render.sh:274-341`
**Issue:** Permutation H (`OTEL_TRACES_SAMPLER_ARG` default, Phase 46
OBS-01) was already the last block in the file; Phase 47 inserted the new
Permutation I ("NOTES.txt tracing-dark warning") *before* H rather than
appending it after, so the file now reads A, B, C, D, E, F, G, I, H. This is
purely a labeling/readability issue (the script still runs correctly and the
final "9 permutations" count is accurate), but a future contributor adding
"Permutation J" after H will produce an even more confusing G, I, H, J
sequence, and anyone skimming top-to-bottom for "Permutation H" will find it
after I.
**Fix:** Move the Permutation I block after Permutation H (or renumber to
G→H→I in file order) the next time this script is touched.

### WR-03: `docs/observability.md`'s "Job-read RBAC already implies access to tide-secrets" claim is stated as settled fact, not a caveat

**File:** `docs/observability.md:170-179`
**Issue:** Same underlying claim as CR-02, but flagged separately here
because it is presented to *operators* as an accepted-and-closed security
argument ("accepted because Job-read RBAC there already implies access to
the project's `tide-secrets`, a strictly more sensitive credential than an
ingest-scoped Phoenix token") with no caveat that this depends entirely on
the operator's own RBAC posture in that namespace (nothing in the chart
enforces that Job-read and Secret-read are bundled together). An operator
who has scoped `jobs` read more broadly than `secrets` read in a project
namespace (a common pattern for CI/observability tooling) will not learn
from this doc that the Phoenix token is now readable by that broader
audience.
**Fix:** Once CR-02 is resolved (or if the plaintext-forwarding design is
kept deliberately), rewrite this paragraph to state the actual exposure
delta plainly (a decoded bearer token becomes visible to anyone with
`get`/`list` on Jobs in the project namespace, independent of Secret RBAC)
rather than the RBAC-equivalence framing.

## Info

### IN-01: `ReporterOptions.OTLPHeaders` doc comment overstates chart-only guarantee

**File:** `internal/controller/reporter_jobspec.go:104-110`
**Issue:** The doc comment says "never a literal value" is the invariant,
immediately followed by "The value is a literal on the Job spec in the
project namespace" in the very next sentence — technically consistent once
read carefully (it's distinguishing the *chart's* env injection from the
*reporter Job's* env injection), but the juxtaposition reads as
self-contradictory on a skim and is worth tightening once CR-02 is
resolved.
**Fix:** Reword to lead with the distinction, e.g. "Chart-rendered env vars
are Secret-sourced only; this reporter-Job env is the one exception —
carried as a literal because …".

### IN-02: `values.yaml`/`tide-values.yaml` comment says the manager "forwards the same header pair" — singular resolved string, not a pair

**File:** `charts/tide/values.yaml:423-425`, `hack/helm/tide-values.yaml:423-425`
**Issue:** Minor wording nit — `OTEL_EXPORTER_OTLP_HEADERS` is a single
comma-joined string (per the OTel spec), and the manager forwards that one
resolved string value, not a "pair." Doesn't affect behavior or tests.
**Fix:** "forwards the same resolved header value" reads more precisely than
"header pair" — cosmetic only.

---

_Reviewed: 2026-07-17T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
