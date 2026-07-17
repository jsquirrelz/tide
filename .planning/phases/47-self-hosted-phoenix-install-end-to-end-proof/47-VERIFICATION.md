---
phase: 47-self-hosted-phoenix-install-end-to-end-proof
verified: 2026-07-17T17:18:18Z
status: gaps_found
score: 3/3 must-haves verified (phase goal met on its letter; named defects + human gate routed below)
overrides_applied: 0
gaps:
  - truth: "Phase 47's own OTLP-headers wiring must not reintroduce the raw-secret-in-a-Job-spec pattern the codebase's signed-token architecture exists to prevent (CR-02 — THIS phase's wiring)"
    status: partial
    reason: >
      internal/controller/reporter_jobspec.go:305 writes the decoded
      OTEL_EXPORTER_OTLP_HEADERS value (an `Authorization: Bearer <phoenix-token>`
      literal) directly into every reporter Job's PodSpec as a corev1.EnvVar.Value,
      in the project namespace. This contradicts the chart's own "never a literal
      value" invariant (enforced for the manager/dashboard Deployments via
      secretKeyRef) and the codebase's established pattern: provider credentials are
      threaded via EnvFrom:SecretRef (internal/dispatch/podjob/jobspec.go:342-352)
      and the subagent receives only an ephemeral signed token, never the raw key
      (D-C4, jobspec.go:444). Threat T-47-02 accepted this at planning on the
      rationale that "Job-read RBAC already implies access to tide-secrets" — but a
      secretKeyRef reveals only the Secret NAME, not the decoded value, so this is a
      strictly larger exposure, not an equivalent one. The acceptance rationale is
      factually unsound; a human must decide accept-via-override vs root-fix.
    artifacts:
      - path: "internal/controller/reporter_jobspec.go"
        issue: "line 305 forwards decoded bearer header as plaintext EnvVar.Value on the reporter Job spec"
      - path: "docs/observability.md"
        issue: "lines 170-179 present the RBAC-equivalence claim to operators as settled fact, not a caveat (WR-03)"
    missing:
      - "Either mirror tide-otlp-headers into project namespaces and have the reporter reference it via valueFrom.secretKeyRef (mirroring tide-signing-key), OR record a corrected T-47-02 override that states the actual exposure delta and scopes the Phoenix token to minimum TTL/permission"
  - truth: "Live LLM message spans carry consistent OBS-02/OBS-03 enrichment (session.id + metadata + tags) — surfaced by this phase's proof as an OBS-02 (Phase 46) letter violation"
    status: failed
    reason: >
      Only 115/386 live LLM spans carry session.id + metadata.* + tag.tags; 271/386
      carry only llm.provider + llm.model_name. OBS-02's letter ("every span carries
      session.id") does not hold on the live tree. Root cause confirmed (CR-01): the
      reporter-spawn gate is purely name-based (dispatch_helpers.go:44-50,
      Get→IsNotFound→Create); the reporter Job TTL is 300s
      (reporter_jobspec.go:318,352); after TTL-GC a sustained-reconcile parent
      re-enters the gate and re-Creates a second reporter with freshly-recomputed
      ReporterOptions. The budget-rollup path guards this exact window with durable
      *RolledUpUID markers (25 refs), but NO equivalent durable marker exists for the
      reporter spawn itself. This is a Phase 46 (OBS-02) requirement regression the
      live proof caught; envtest fixtures pass. Does NOT break PROOF-01's letter (the
      six AGENT dispatch-chain spans are complete, correctly parented, and queryable).
    artifacts:
      - path: "internal/controller/dispatch_helpers.go"
        issue: "spawnReporterIfNeeded gates Create on name-existence only; reopens after 300s TTL-GC"
      - path: "internal/controller/reporter_jobspec.go"
        issue: "TTLSecondsAfterFinished=300 (line 318,352) makes the name-based gate re-openable"
    missing:
      - "Add a durable per-attempt 'reporter spawned' marker to each level's .status (keyed on planner Job UID / completedJob.UID, mirroring *RolledUpUID) and gate Create on that marker at all five spawn sites"
  - truth: "Boundary-push --force-with-lease retry refreshes the lease from the real remote tip (cross-phase / pre-existing run-integrity defect surfaced by the proof)"
    status: failed
    reason: >
      Defect #1 (47-04/47-EVIDENCE §6.1): Status.Git.LastPushedSHA is set once at the
      project-descent boundary push and never refreshed when a later wave-level push
      advances the same branch; every subsequent --force-with-lease retry re-asserts
      the stale lease and fails identically (non-fast-forward), causing
      medium-project.status.phase to flap (visibly captured in the deep-link
      screenshot: project node "Running", empty artifacts panel). Pre-existing code in
      internal/controller/project_controller.go, NOT introduced by Phase 47; no data
      loss (authored content verified intact). Does NOT affect Phoenix trace evidence
      (span emission is per-Job-completion, independent of the git boundary push).
    artifacts:
      - path: "internal/controller/project_controller.go"
        issue: "boundary-push retry re-asserts stale LastPushedSHA; bypass annotation clears state without refreshing the lease"
    missing:
      - "Re-read the actual remote tip before each --force-with-lease retry (and in the bypass path); regression test simulating a wave-level push landing between two boundary-push attempts"
  - truth: "examples/projects/medium fixtures apply cleanly on kind (cross-phase example-fixture gap surfaced by the proof)"
    status: failed
    reason: >
      Defect #3: examples/projects/medium/per-namespace-resources.yaml ships the
      tide-projects PVC as ReadWriteMany, which deadlocks WaitForFirstConsumer on
      kind's rancher.io/local-path (RWO-only) provisioner. The file's own comment says
      RWO is needed on kind but the medium README's apply sequence has no override
      step. Worked around operationally in 47-04 (delete/recreate as RWO). Small
      example-fixture fix, cross-phase, does not block the phase goal.
    artifacts:
      - path: "examples/projects/medium/per-namespace-resources.yaml"
        issue: "tide-projects PVC defaults RWX; deadlocks on kind"
    missing:
      - "Add an inline RWO override step to the medium sample's apply sequence (mirroring hack/scripts/acceptance-v1.sh's small-sample pattern)"
human_verification:
  - test: >
      Review the four committed evidence PNGs and 47-EVIDENCE.md against the PROOF-01
      milestone acceptance bar. The 47-05 Task 3 checkpoint:human-verify (gate=blocking)
      was auto-resolved via its named-gaps branch — no human has actually reviewed the
      evidence. PROOF-01 IS the milestone acceptance bar; its own plan mandates human
      sign-off.
    expected: >
      Human confirms: (a) the trace-tree PNG shows all five AGENT levels
      (project→milestone→phase→plan→task) with LLM spans under Task; (b) the query PNG
      shows a working DSL filter over the enrichment; (c) the deep-link PNG lands on the
      right trace. Then explicitly accepts PROOF-01 as met OR converts a shortfall to a
      named gap.
    why_human: "Milestone acceptance-bar sign-off; visual/UX judgment against the bar; the plan's own blocking human gate never fired."
  - test: >
      Judge whether the LLM-span capture (47-evidence-llm-span-redacted.png) satisfies
      PROOF-01's "including redacted message arrays at the Task level" clause. The
      screenshot shows a real multi-role message array with NO visible redaction/elision
      markers. 47-EVIDENCE §4 honestly discloses why: the redaction pass runs
      unconditionally but this run's content had zero secret-pattern matches and the
      largest message attribute was 21,573 B (< 32 KiB elision cap), so pass-through is
      the boundary's correct output; redaction was proven held by a 0-hit key-material
      search across all 392 spans.
    expected: >
      Human decides whether pass-through content (with redaction proven via 0-hit search)
      satisfies "redacted message arrays," or requests a supplementary capture from a run
      containing secret-bearing or over-cap content that visibly exercises the redaction/
      elision markers.
    why_human: "Interpretation of the milestone bar's 'redacted' clause; cannot be resolved programmatically."
---

# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof — Verification Report

**Phase Goal:** An operator can stand up a self-hosted Phoenix from documented, non-default-safe overrides, point TIDE's existing `otel.exporter.endpoint` chart value at it, and see a real run's complete five-level trace tree — including redacted message arrays — rendered and queryable. This is the milestone's acceptance bar.
**Verified:** 2026-07-17T17:18:18Z
**Status:** gaps_found
**Re-verification:** No — initial verification

## Goal Achievement

The phase goal is **achieved on its letter** — all three roadmap Success Criteria (PHX-01, PHX-02, PROOF-01) verify against the codebase and the live evidence. The `gaps_found` status is driven by real, named defects that must feed gap-closure planning (one introduced by this phase's own wiring, one a Phase-46/OBS-02 regression the proof surfaced, two cross-phase/pre-existing findings) **and** by the milestone-acceptance-bar human gate that was auto-resolved without any human review. Judge the goal met; do not close the milestone silently on the routed items.

### Observable Truths (Roadmap Success Criteria)

| # | Truth | Status | Evidence |
| - | ----- | ------ | -------- |
| 1 | INSTALL.md/observability.md walks an operator through a self-hosted Phoenix install covering BOTH storage paths and calls out the `auth.enableAuth=true` default (PHX-01) | ✓ VERIFIED | `docs/INSTALL.md:217` §"Enable tracing (Phoenix)" (SQLite-on-PVC quickstart, pinned chart `10.0.1`, auth default called out as opposite of raw image). `docs/observability.md:192` §"Self-hosted Phoenix" documents both storage strategies (SQLite-on-PVC + bundled Postgres, with sizing/retention/exclusivity), auth posture, weak-default call-outs |
| 2 | `otel.exporter.endpoint` documented end-to-end in bare `host:port` form; NOTES.txt nudges toward the Phoenix step when tracing is dark (PHX-02) | ✓ VERIFIED | INSTALL.md:264 bare `host:port` warning + 4317/6006 distinction + silent-rejection failure mode. Render gate `make helm-telemetry-assert` Permutation I PASS: NOTES nudge present by default, absent when `otel.exporter.endpoint` set. Go+chart wiring threads `OTEL_EXPORTER_OTLP_HEADERS` via secretKeyRef on both containers + reporter-Job forward |
| 3 | A live run's complete five-level trace tree — including redacted message arrays at Task level — is visible and queryable in self-hosted Phoenix; screenshots + trace IDs captured (PROOF-01) | ✓ VERIFIED (letter) | Trace `e9124906f6ee4aeba650a6fdd93b86fd` (392 spans, 6 AGENT correctly parented project→milestone→phase→plan→task×2) in auth-ON Phoenix 18.1.0. Four valid PNGs verified by direct visual inspection; DSL query `metadata['level']=='phase'` returns results; deep link resolves to the correct span; redaction held (0 key-material hits across all 392 spans). See human_verification #2 on the "redacted" pass-through nuance |

**Score:** 3/3 truths verified (phase goal met on its letter)

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/controller/reporter_jobspec.go` | `OTLPHeaders` field + guarded env | ✓ VERIFIED | field at :111; guarded append at :304-305 (only when both `OTLPEndpoint` and `OTLPHeaders` non-empty). Unit tests `TestBuildReporterJob_OTLPHeaders{Env,WithoutEndpointNoEnv}` PASS |
| 5× `*_controller.go` spawn sites | forward `r.Deps.OTLPHeaders` | ✓ VERIFIED | milestone:657, phase:610, plan:665, project:1925, task:1113 — all five present |
| `cmd/manager/main.go` | env read + Deps threading | ✓ VERIFIED | `os.Getenv("OTEL_EXPORTER_OTLP_HEADERS")` at :289; assigned into plannerDeps (:443) and TaskReconcilerDeps (:555) |
| `charts/tide/values.yaml` + `hack/helm/tide-values.yaml` | `headersSecretRef` key | ✓ VERIFIED | :426 both files (byte-identical via augment `cp`) |
| `charts/tide/templates/{deployment,dashboard-deployment}.yaml` | secretKeyRef env, no literal | ✓ VERIFIED | render: 2× `name: OTEL_EXPORTER_OTLP_HEADERS` via `valueFrom.secretKeyRef`, zero literal `value:`; `assert-otlp-headers-env.py` PASS both ways |
| `charts/tide/templates/NOTES.txt` | tracing-dark nudge | ✓ VERIFIED | present in tracked template + augment heredoc; Permutation I gate confirms both-ways |
| `hack/helm/assert-otlp-headers-env.py` | offline secretKeyRef gate | ✓ VERIFIED | exists; PASS on secretref render, PASS on `--expect-absent` default (WR-01: hard-requires dashboard container — degrades poorly on `dashboard.enabled=false` outside the Makefile target) |
| `docs/INSTALL.md`, `docs/observability.md` | Phoenix recipe + reference | ✓ VERIFIED | headings present; placeholder discharged; no scheme-prefixed OTLP endpoint in the new sections |
| 4× `47-evidence-*.png` | evidence screenshots | ✓ VERIFIED | 4 valid PNGs; visually inspected — each matches its described content |
| `47-EVIDENCE.md`, `47-PROOF-RUNLOG.md` | evidence record + runlog | ✓ VERIFIED | trace ID present; known-limitations block present; 563-line runlog; zero `sk-ant` in either |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | -- | --- | ------ | ------- |
| `cmd/manager/main.go` | Deps structs | `OTLPHeaders: otlpHeaders` | ✓ WIRED | :443, :555 |
| 5× reconcilers | `reporter_jobspec.go` | `OTLPHeaders: r.Deps.OTLPHeaders` | ✓ WIRED | all 5 spawn sites |
| `charts/tide/values.yaml` | Deployment env | guarded secretKeyRef template | ✓ WIRED | renders 2× on set, 0× default |
| `augment-tide-chart.sh` | `NOTES.txt` | heredoc overwrite | ✓ WIRED | "tracing is dark" in both sites |
| manager env → reporter Job | reporter TracerProvider | literal `EnvVar.Value` | ⚠️ WIRED-BUT-UNSAFE | works, but forwards a decoded bearer credential as a plaintext literal (CR-02 gap) |
| dashboard deep link → Phoenix | `/redirects/spans/{id}` | live exercise | ✓ WIRED | deeplink PNG resolves to plan AGENT span `4d7f2bda9aee8d57` |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| OTLPHeaders unit tests | `go test ./internal/controller/... -run TestBuildReporterJob_OTLPHeaders -v` | 2/2 PASS | ✓ PASS |
| Full render-gate suite | `make helm-telemetry-assert` | all 9 permutations PASS (incl. I = D-10 both-ways) | ✓ PASS |
| secretKeyRef shape (set) | `assert-otlp-headers-env.py … --expect-secretref` | PASS manager + dashboard, no literal value | ✓ PASS |
| absence (default) | `assert-otlp-headers-env.py … --expect-absent` | PASS both containers | ✓ PASS |
| touched-package build | `go build ./cmd/manager/... ./internal/controller/... ./internal/dispatch/...` | exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| PHX-01 | 47-03 | Self-hosted Phoenix recipe, both storage paths, auth default call-out | ✓ SATISFIED | INSTALL.md + observability.md verified. (Traceability table still marks it "Pending" — bookkeeping lag, INFO) |
| PHX-02 | 47-01/02/03 | `otel.exporter.endpoint` end-to-end, bare host:port, NOTES.txt nudge | ✓ SATISFIED | Go+chart wiring + docs + Permutation I gate verified. (Table marks "Pending" — bookkeeping lag) |
| PROOF-01 | 47-04/05 | Live five-level tree with redacted message arrays, visible + queryable, evidence captured | ✓ SATISFIED (letter) | Live trace + 4 PNGs + EVIDENCE.md; human acceptance gate outstanding (see human_verification) |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| `internal/controller/reporter_jobspec.go` | 305 | Decoded bearer credential written as plaintext `EnvVar.Value` on a Job spec | 🛑 Blocker | CR-02 — violates codebase's own SecretRef/signed-token invariant; gap #1 |
| `internal/controller/dispatch_helpers.go` | 44-50 | Name-only spawn gate re-openable after 300s TTL-GC; no durable marker | 🛑 Blocker | CR-01 — root cause of the OBS-02 partial-enrichment regression; gap #2 |
| `docs/observability.md` | 170-179 | Security-equivalence claim stated to operators as settled fact | ⚠️ Warning | WR-03 — masks the real exposure delta; tied to gap #1 |
| `hack/helm/assert-otlp-headers-env.py` | 32,88-96 | dashboard container hard-required | ⚠️ Warning | WR-01 — spurious failure on `dashboard.enabled=false` render outside the Makefile target |
| `hack/helm/assert-telemetry-render.sh` | 274-341 | Permutations out of lettered order (I before H) | ℹ️ Info | WR-02 — cosmetic/maintainability |

### Human Verification Required

1. **Review evidence vs the PROOF-01 milestone acceptance bar.** The 47-05 Task 3 `checkpoint:human-verify` (gate=blocking) was auto-resolved via its named-gaps branch — no human reviewed the evidence. PROOF-01 IS the milestone acceptance bar. Confirm the four PNGs and 47-EVIDENCE.md meet the bar, then explicitly accept or convert shortfalls to gaps.
2. **Judge the "redacted message arrays" clause.** `47-evidence-llm-span-redacted.png` shows a real multi-role message array with NO visible redaction/elision markers; §4 honestly explains this is correct pass-through (no secret matches; largest message 21,573 B < 32 KiB cap) with redaction proven by a 0-hit key search across all 392 spans. Decide whether pass-through satisfies the clause or a redaction-exercising supplementary capture is wanted.

### Gaps Summary

The phase goal is met on its letter (3/3 roadmap SCs verified against real code and live evidence). Four defects are routed for gap-closure planning, precisely classified by ownership:

- **THIS phase's own wiring (BLOCKER):** CR-02 — 47-01's change forwards the decoded OTLP bearer header as a plaintext literal into the reporter Job PodSpec (`reporter_jobspec.go:305`), diverging from the codebase's SecretRef/signed-token invariant. Registered threat T-47-02 accepted this at planning, but on a factually unsound RBAC-equivalence rationale (a secretKeyRef exposes only the Secret name, not the decoded value). This is an **override candidate** — see below — but the human must either root-fix (cross-namespace Secret mirror + secretKeyRef) or record a corrected override.
- **Phase-46 / OBS-02 regression surfaced by the proof (BLOCKER):** Defect #2 / CR-01 — 115/386 live LLM spans enriched; root-caused to the name-only reporter-spawn gate re-opening after the 300s TTL-GC with no durable spawn marker (the budget path has `*RolledUpUID`, the spawn path has nothing). Does not break PROOF-01's letter but is a real correctness regression against a Complete Phase-46 requirement.
- **Cross-phase / pre-existing (surfaced, not owned by Phase 47):** Defect #1 (boundary-push stale-lease flap, `project_controller.go`, run-integrity class — no trace-evidence impact) and Defect #3 (medium-sample RWX PVC deadlock on kind, example-fixture gap).

All were honestly named in 47-EVIDENCE.md §6 and the SUMMARYs and routed (not worked around) per D-14's escape hatch. No later milestone phase exists to defer them to (Phase 47 is the acceptance-bar phase), so none are deferrable.

**Override candidate (CR-02 / T-47-02).** This deviation was intentional at planning. To accept it instead of root-fixing, add to this file's frontmatter — but only after correcting the rationale to state the real exposure (the reviewer showed the original "RBAC-equivalence" justification is false):

```yaml
overrides:
  - must_have: "Phase 47's own OTLP-headers wiring must not reintroduce the raw-secret-in-a-Job-spec pattern"
    reason: "Reporter Job runs in the project namespace where a decoded bearer literal is an accepted exposure for an ingest-scoped Phoenix token; token scoped to minimum TTL/permission and doc corrected to state the actual exposure delta (not RBAC-equivalence)"
    accepted_by: "{your name}"
    accepted_at: "{ISO timestamp}"
```

---

_Verified: 2026-07-17T17:18:18Z_
_Verifier: Claude (gsd-verifier)_
