# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.0.1 — Orchestrator Trustworthiness + Telemetry Completion

**Shipped:** 2026-06-13
**Phases:** 6 (12–17) | **Plans:** 38 | **Tasks:** 46 | **Commits:** 330

### What Was Built
- Gate semantics correctness: approve-at-descent (review the artifact before children spend), no level jumps past incomplete children, dispatch holds while parked, reject parks instead of fail-marking, `tide resume --retry-failed` as the one recovery verb.
- Image-resolution chain (`Levels.<level>.Image` → `Spec.Subagent.Image` → helm default) at all six dispatch sites — closing the v1.0 stub-image bug — plus a provider billing-400 project-wide `BillingHalt`.
- Budget visibility: current model IDs in the pricing table, `BudgetBlocked` on the Project + dashboard, reserve/settle accounting bounding in-flight overshoot, pricing-drift automation.
- Seven run-1 paper cuts (reporter CR labels, clean-tree push no-op, status-flapping convergence, real `artifact-get` inspector Pod, dashboard Complete chip, cross-plan running-waves view, strict file-touch overlap rejection).
- Telemetry foundation end-to-end (`PROM_ENDPOINT`→PromQL proxy, mounted TelemetryView, six locked metrics with labels, panel name alignment, Makefile gate, bounded proxy client).
- Audit tech-debt subset (Phase 17): Plan-level project-label self-heal, reject-before-reporter-spawn, narrowed approve guard, non-fatal envelope-read.

### What Worked
- **Findings-as-tests.** Every requirement carried an implicit acceptance criterion: a regression test that reproduces the dogfood run-1 symptom. Bugs were pinned to behavior, not implementation — and the run-killer (premature `Complete`) can't silently return.
- **Gap-closure waves caught real regressions before ship.** Phases 12, 13, 14, and 16 each spawned a gap-closure wave from their VERIFICATION/REVIEW artifacts rather than shipping on first-green.
- **The audit→closure loop.** The milestone audit held at `tech_debt` for a deliberate accept-or-cleanup decision; Phase 17 executed that decision (closed the in-scope subset, formally deferred the rest) instead of waving the items off.
- **Sibling-pattern mirroring.** Fixes that mirrored an already-shipped in-tree template (milestone/phase label backfill, reject short-circuit, Pitfall-1 non-fatal envelope read) were low-risk and consistent across reconcilers.

### What Was Inefficient
- **Executor under-reported `requirements_completed`.** 17-01/02/03 (and several earlier plans) left the SUMMARY frontmatter `requirements_completed` empty, forcing the auto-extraction to surface raw section headers and the audit to cross-check coverage manually against VERIFICATION file:line evidence. Same pattern flagged at v1.0.0 close — still unfixed.
- **ROADMAP.md was rewritten per-phase to only the active phase.** The milestone-wide phase list/detail was not retained in the live file, so the v1.0.1 archive had to be reconstructed from a prior git revision (`60a2841`) spliced with the Phase 17 block.
- **`milestone.complete` accomplishment extraction is naive** — it grabbed the first line of each SUMMARY (often "Task 1 —" or "One-liner:"), requiring a manual rewrite of the MILESTONES.md entry.

### Patterns Established
- **Dogfood finding → symptom-reproducing regression test** as the unit of trustworthiness work. The finding's run-1 symptom IS the test's red state.
- **Reject-before-reporter-spawn ordering** and **project-label self-heal backfill** are now the canonical shapes across the milestone/phase/plan reconcilers.
- **Audit `tech_debt` is a real gate**, not a rubber stamp — it routes to a closure phase that adjudicates each item IN-scope (fix) or DEFERRED (backlog), with rationale.

### Key Lessons
1. If `requirements_completed` frontmatter keeps coming back empty, the executor SUMMARY contract (or its template) needs a hard prompt — two milestones of manual cross-checking is a process smell, not a one-off.
2. The per-phase ROADMAP rewrite trades milestone-archive fidelity for context economy; the archive step must reconstruct from git. Consider retaining collapsed prior-phase entries in the live ROADMAP so close-out is mechanical.
3. Gate semantics that touch spend (approve/reject/resume) are worth a dedicated phase with a shared test-fixture and a live repro environment — the run-killer lived exactly here.

### Cost Observations
- Model profile: `quality` (`gsd-planner` → fable, `gsd-verifier` → opus, default opus). Per-session token mix not instrumented this milestone.
- Notable: the telemetry that would have measured this milestone's own cost (the six locked metrics) shipped *in* this milestone — next milestone can self-report.

---

## Milestone: v1.0.6 — Adoption-Path Correctness & Dispatch Safety

**Shipped:** 2026-06-29 (tag `v1.0.6`) | **Phases:** 3 (31–33) | **Plans:** 8

### What Was Built
Closed the four code-level defects dogfood run #2b surfaced on the v1.0.5 import/adoption path: D1+D2 (adopted Project advances Initialized→Running on `ImportComplete` with budget rollup + cap enforcement; durable project-planner suppression), D3 (live in-flight planner-count cap before pool acquire, single-node-safe default 4), D4 (failed planner marked `Failed` not `Succeeded` via shared `isPlannerFailure`, ordered before the gate-policy hook). Published 8 images + 2 OCI charts + 5 binaries; audit `tech_debt` (13/13 reqs, 0 blockers).

### What Worked
- **Adversarial code review caught what the verifier + green tests missed — again.** `gsd-code-review` flagged CR-01 (D4 guard placed after the gate-policy hook → a failed milestone parks at `AwaitingApproval` under the default `approve` gate instead of `Failed`). The PLANFAIL-02 envtest had masked it by forcing `Gates{Milestone:"auto"}`. This is the second milestone (after v1.0.2 Phase 25) where the code-review pass found a real blocker post-verification.
- Confirming every subagent finding against source before acting (CR-01, the two audit warnings) — none were taken on faith.

### What Was Inefficient
- **The release pre-push hook failed twice on a test timeout before the cause was found.** The unit-tier `make test` (`-timeout 120s`) tripped because the `internal/controller` envtest suite had grown to ~120–135s. A clean standalone run passed (120.169s, barely under), masking it as a flake; only capturing full hook output revealed the `TestControllers timed out at 2m0s` panic. Lesson: when a pre-push/CI gate fails but a local re-run passes, capture the *full* gate output before assuming flake.

### Patterns Established
- **Green tests that override production defaults prove nothing about the default path.** PLANFAIL-02 used `Gates{Milestone:"auto"}` (non-default) and passed while the default-`approve` path was broken. Fix: exercise the production gate config in the test, and assert a `Running` precondition so a silent dispatch failure can't pass vacuously.
- Release ordering held: STEP-ONE chart/appVersion bump → push main → rc dry-run gate → release tag → close-out (no re-tag).

### Key Lessons
- The `internal/controller` Ginkgo envtest suite has outgrown the "unit" tier (~34s → ~120s across phases). Raised the TEST-01 budget 120→300s and go-test timeout 120→360s as a stopgap; the real fix (move heavy specs to the TEST-02 integration tier) is carried to v1.0.7.
- A failure-classification guard must run before any approval/hold gate — you cannot gate-approve a planner that authored nothing.

### Cost Observations
- Model mix: planning sonnet/opus, executors sonnet, verifier opus, code-review/integration sonnet. Fable still unavailable (planner→opus).
- Notable: the discuss→plan→execute→verify→code-review→secure→audit→release→close-out chain ran end-to-end in one session; the release blocker (test timeout) was the only unplanned detour.

---

## Milestone: v1.0.7 — First-Run Paper Cuts: Run Integrity & Operator Ergonomics

**Shipped:** 2026-07-15 (tag `v1.0.7`) | **Phases:** 8 (34–41, incl. folded-in 39) | **Plans:** 51

### What Was Built
Everything the first external-repo run surfaced short of new subagent stages: the run-integrity gate (serialized flock merges, git-recomputed completeness gate on boundary push, `lastPushedSHA` lease), console-accurate Claude-5 pricing (exact-ID + probed 125/100 cache-write multiplier + observable fallback), git ergonomics (baseRef, uniform agent identity, promptFile), the dashboard as an approve-gate review surface (git-as-artifact-store + gitfetch, settings view, honest log-drawer states), telemetry setup guidance — plus two appended structural turns: the Phase 40 v1alpha3 crank (v1alpha1+v1alpha2 deleted, `subagent.levels` rename, CI-gated) and the Phase 41 12-item refactoring review. Audit `tech_debt`, 44/44, 0 blockers; `make test-int` fully green at close.

### What Worked
- **The milestone audit as a working session, not a report.** It found Phase 37 formally unverified (no VERIFICATION.md despite a passed 8/8 live UAT), spawned the verifier to close it, and surfaced doc drift (BASE checkboxes, the never-propagated DASH-02 git-transport reword) that got fixed on the spot.
- **Root-causing the "flake" instead of relabeling it.** The close-out full-suite run came back 25/26; the red was traced to a real harness race (fire-and-forget `deleteNamespace` + shared namespace constants → recreate-while-Terminating) and fixed in the same session (quick 260715-4jd), after which the suite ran 26/26. The NO-FLAKE-TOLERANCE Makefile doctrine proved itself.
- **Mid-phase design reversal handled cleanly:** DASH-02's ConfigMap display cache was superseded by git-as-artifact-store during discussion (user rejected truncation), and the phase delivered the better design — the only miss was not propagating the reword to REQUIREMENTS/ROADMAP (caught at audit).

### What Was Inefficient
- **Two full kind-suite runs (~35 min each) at close** because the first surfaced the harness race. Unavoidable given the doctrine, but the race itself dated from Phase 04.1-era test scaffolding — a periodic harness-hygiene pass over shared-namespace patterns would have caught it cheaply.
- **Phase 37 closed without its verifier artifact** — 12/12 plans + REVIEW + UAT existed but nobody ran `/gsd:verify-work`-equivalent; the audit had to backfill it. Verify-before-close should be part of phase closeout, not milestone closeout.
- **The SDK's auto-extracted MILESTONES.md accomplishments were unusable** (raw "One-liner:" placeholders and task headers) — hand-curated at close. SUMMARY frontmatter hygiene (`one_liner`, `requirements_completed`) remains unfixed across milestones.

### Patterns Established
- Milestone-close chain that works end-to-end: audit (with inline gap-closure) → tech-debt-weight quick fixes (W-1 baseRef wire) → full-suite green evidence → archive/evolve/tag.
- Evidence-currency rule: the close-out suite run must be on the FINAL tree (post-refactor REFAC-11 PVC wiring was exercised green only because the suite re-ran after Phase 41).

### Key Lessons
- A missing VERIFICATION.md is invisible until milestone close unless the audit checks for it — phase "Complete" status in STATE.md said nothing about verifier artifacts.
- Fixture/harness races hide behind "environmental" labels: the same failure signature (PVC never Bound) had three distinct root causes across this repo's history (provisioner pressure, missing SA, namespace-Terminating race) — grep the harness before blaming the environment.

### Cost Observations
- Model mix: planners fable/opus, executors sonnet, verifier opus, integration-checker + audit inline fable. Fable available again this milestone (planner quick tasks ran on it).
- Notable: the audit→close chain (integration check, Phase-37 verification, race root-cause + fix, 2 full kind runs, 2 quick tasks, archive/evolve) ran autonomously in one session; the only human decision was close-first sequencing.

---

## Milestone: v1.0.8 — Phoenix Rising: OpenInference Trace Emission + Self-Hosted Phoenix

**Completed:** 2026-07-17 (public tag/release pending)
**Phases:** 6 (42–47) | **Plans:** 32 | **Tasks:** 67 | **Commits:** 240

### What Was Built
- Trace-context foundation + five-level AGENT span emission: pure `pkg/otelai/tracecontext.go` (deterministic TraceID from Project UID, W3C traceparent format/extract, retroactive timestamps) wired into all five level-completion handlers — attribute-complete spans (model/provider/token counts) for succeeded and failed Jobs alike.
- One connected trace: correct parent-child nesting across Project→Milestone→Phase→Plan→Task, W3C `traceparent` propagated at both pod hops (subagent + reporter), per-level IDs persisted in `.status.trace`.
- LLM message-array spans (the headline): the reporter's trace-only mode turns a Task's `events.jsonl` into redacted, size-bounded OpenInference LLM spans — redact-before-truncate at a single chokepoint, a D-O5 triple size guard, and TracerProvider `Shutdown` on every one-shot exit path.
- Runtime-neutral adapter seam: fail-closed `SelfInstruments` capability flag + `--skip-message-spans` transport + a stub-runtime contract test (zero duplicate spans) — scaffolding for the LangGraph beachhead, byte-identical today.
- Observability enrichment + dashboard deep link: sampler 1.0, `session.id` = Project UID, metadata/tags, and a `<PhoenixTraceLink>` from every DAG node.
- Self-hosted Phoenix recipe (INSTALL.md / observability.md, both storage paths, auth-ON default, OTLP-headers `secretKeyRef` chain) proven by a live 392-span five-level trace tree.

### What Worked
- **The live proof caught what envtest couldn't.** Phase 47's real $0.88 run surfaced two defects the green envtest suite passed over — a boundary-push stale-lease status flap and ~⅓ enrichment coverage from reporter double-spawn. "A real imperfect test surfaces reality" held again.
- **Root-fix over workaround.** Both live-run defects were root-caused and fixed (remote-tip re-read + ancestry-guarded lease; durable reporter-spawned markers gating all five spawn sites), not papered over — each with an envtest proof.
- **Research on real fixtures overturned a naive framing.** Phase 44's D-O5 boundary: 58 real `events.jsonl` files showed the OTLP 4 MB risk was the *aggregate export batch*, not a single oversized span — the guard was designed to the real failure mode, not the assumed one.
- **Semconv as source of truth.** Backing every attribute key with the official `openinference-semantic-conventions` module (not hand-rolled strings) makes TIDE's spans forward-compatible with LangChain's native emission for free.

### What Was Inefficient
- **The blocking human gate never fired.** Phase 47-05's `checkpoint:human-verify` auto-resolved via its named-gaps branch, so PROOF-01 sign-off stayed outstanding at close until surfaced manually — a gate that can auto-resolve isn't a gate.
- **Gap-closure didn't re-run the paid proof.** Plans 47-06…47-10 fixed the two live-run defects but weren't scoped to re-capture, so the evidence PNGs still show the pre-fix state and the human sign-off had to reason about envtest-verified-but-not-re-proven fixes.
- **Stale-artifact drift at close.** STATE.md frontmatter, REQUIREMENTS.md PHX-01/02 checkboxes, and the traceability table all lagged reality — reconciled at milestone close rather than at phase close.

### Patterns Established
- **Retroactive span synthesis gated on state-transition edges** (not in-memory idempotency): spans created and closed in `handleJobCompletion`, never held open across a `Reconcile()` return.
- **Redact-before-truncate at a single chokepoint** — the sole emission point enforces redaction, so new call sites can't leak.
- **Runtime-neutral trace contract** — manager-injected `traceparent` + a per-runtime adapter seam gated by a capability-flag-carried-as-data.

### Key Lessons
- **A gate that auto-resolves isn't a gate.** The blocking human-verify checkpoint's named-gaps branch let the milestone-acceptance sign-off slip; verify the gate actually fires — don't trust its presence.
- **Envtest-green ≠ live-proven.** The two Phase 47 defects only appeared under a real run; a paid live proof remains the acceptance bar for a milestone whose value is "an operator sees it work."
- **Reconcile artifacts at phase close, not milestone close.** The STATE / requirements / traceability drift was cheap to fix but read as false progress until reconciled.

### Cost Observations
- One paid live run all milestone: $0.88 for the `medium-project` proof (392 spans, `claude-haiku-4-5`).
- Planner override `fable` was unavailable and fell back to Opus; verifier ran on Opus (independent envtest re-runs paid off — the verifier re-ran gates rather than trusting SUMMARY claims).
- Proof cluster left running post-capture for human review, per the one-heavy-workload VM discipline.

## Milestone: v1.0.9 — Slack Tide: The Task Loop (Verification-Driven Quality Iteration)

**Shipped:** 2026-07-21 (milestone complete; release tag pending rc-gated pipeline)
**Phases:** 6 (48–53) | **Plans:** 46 | **Tasks:** 98 | **Timeline:** ~4 days | **Commits:** ~354, +58.6k/−0.9k LOC

### What Was Built

The first real feedback loop: independent verification (read-only LangGraph evaluator image — the successor-runtime beachhead) drives Task iteration with evidence-seeded fresh attempts, parameterized per level by one shared `LoopPolicy` contract and one level-keyed resolver; execution-loop evidence (`TerminalReason`, `RunEvidence`, derived run IDs) + loop-native observability; chart-first config with a sticky install-ON/upgrade-OFF posture; dashboard loop provenance with `VerifyHalt` distinct from `Failed`.

### What Worked

- **Live proofs as phase gates.** Three phases (51, 52, 53) each ended on a real kind-cluster proof; every one surfaced defects green tests missed (the verdict relay ship-blocker, the attempt-blind reporter name, the swallowed exhausted-plan approve). "envtest feeds fakes" is now a known blind-spot class with a known antidote.
- **Adding plans mid-flight when execution surfaces gaps.** Three gap plans (53-10, 53-11, plus Phase 51's D-09 todo folds) were authored inside the run instead of deferred — the findings chain would have shipped broken twice without this.
- **Code review as a distinct pass after green tests.** Across the milestone it caught 1 Critical + 20+ warnings the verifiers and suites missed, including a namespace bug that would have 404'd the headline feature in every real install.
- **Schema-before-consumer sequencing** (49 → 50 → 51) meant the Task loop landed on settled types; `make manifests` zero-diff checks kept the contract phases honest.
- **Parallel-worktree waves with post-merge gates.** The gates caught cross-plan integration issues per-worktree checks cannot (goconst across merged plans, embed-hash rotations).

### What Was Inefficient

- **Worktree environment artifacts recurred** — fresh worktrees failing `go build ./...` on gitignored embed dirs, missing envtest etcd, npm ci churn; each wave's executors rediscovered some of it until pre-warned in prompts. A worktree-bootstrap note (or harness fix) would save repeated diagnosis.
- **The cleanup-wave helper blocks on benign states** (embed-hash rotations read as deletions; untracked kind-suite artifacts) and its re-runs stop at already-cleaned entries — two manual merge interventions this milestone.
- **`verify-dashboard-freshness` flakes under CPU contention** (known since v1.0.8) cost a diagnosis cycle again.
- **Requirement phrasing ambiguity** (CFG-02's "enabled at the milestone+project scope") had to be interpreted at discuss-time; tighter requirement wording at roadmap-time would have avoided carrying an interpretation risk through planning.

### Patterns Established

- Fail-closed as a doctrine, not a feature: verdicts, terminal reasons, config parsing, chart posture — the zero value is never the permissive value.
- Structural enforcement over prompt/document enforcement (read-only at mount/cred/rootfs; anti-gaming via ChangedFiles; model-callable tools never choose their own commands).
- Infra-retry ≠ quality-iteration, grep-distinguishable at every level.
- Iteration history lives in traces/artifacts; etcd carries only the current summary (LOOP-03 held under pressure repeatedly).
- Chart changes author in `hack/helm/` + regenerate same-commit; the chart is generated, never hand-edited.

### Key Lessons

- Nothing counts as done until the shipped path ran end-to-end on a real cluster — twice this milestone the wired-but-never-run path was the broken one (verifier entrypoint, verdict relay).
- A fail-closed consumer (tide-push's findings.json rule) is only safe once every producer exists; check the producer side before activating a new consumer trigger.
- Ambiguous requirement text should be resolved into a named, veto-able decision at discuss-time (CFG-02's D-03 pattern) rather than silently interpreted at plan-time.

### Cost Observations

- Model mix: fable (orchestrator + planner), sonnet (executors/researchers/checkers), opus (verifiers/review-adjacent) — heavier tiers reserved for judgment passes.
- Live-proof spend deliberately bounded: Phase 51/52 billable proofs < $1 combined; Phase 53's kind proofs were $0 (stub subagents).
- Three full kind-suite runs in one phase (53-09) was the wall-clock long pole (~33 min each).

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0.0 | 14 dirs (1–11 + inserts) | 137 | Built the operator end-to-end; rc-gated release with $0 DinD dry-run |
| v1.0.1 | 6 (12–17) | 38 | Trustworthiness pass driven by dogfood run-1 findings; findings-as-regression-tests; audit→closure-phase loop |
| v1.0.6 | 3 (31–33) | 8 | Corrective-patch cadence (~2 days); adversarial code review as standard post-verify gate |
| v1.0.7 | 8 (34–41) | 51 | Audit-as-working-session (inline gap closure + quick fixes); NO-FLAKE-TOLERANCE doctrine proven; full-suite-on-final-tree evidence rule |

### Recurring Friction (verify whether next milestone fixes it)

1. Executor leaves SUMMARY `requirements_completed` empty → manual coverage cross-check at audit (v1.0.0, v1.0.1, and again v1.0.7 — all null; VERIFICATION.md tables used as fallback source).
2. Administrative quick-task status fields never flipped → carried as deferred items at every close (11 at v1.0.0, 15 at v1.0.1, 20 at v1.0.6, 24 at v1.0.7).
3. Phase closes without its VERIFICATION.md unless the phase chain runs the verifier — backfilled at audit for Phase 37 (v1.0.7); consider making verify a hard phase-closeout step.

### Top Lessons (Verified Across Milestones)

1. The milestone audit earns its keep — it caught the dropped `podAnnotations` render block before v1.0 and routed the v1.0.1 tech-debt subset into a real closure phase. Don't skip it.
2. `make test-int` green ≠ ship-ready: read `MAKE_EXIT` and grep `^--- FAIL` — Ginkgo "SUCCESS!" can coexist with a red plain go-test in the same package.
