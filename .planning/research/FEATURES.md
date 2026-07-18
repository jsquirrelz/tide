# Feature Research

**Domain:** In-cluster LLM verify tier for a K8s agentic-coding orchestrator (TIDE v1.0.9 "Slack Tide")
**Researched:** 2026-07-18
**Confidence:** MEDIUM-HIGH (GSD prior art read directly from source = HIGH; external LLM-as-judge/agentic-workflow grounding = MEDIUM, 3 web sources cross-checked; LangGraph-specific mechanics = MEDIUM, flagged for plan-phase re-verification per the milestone's own open questions)

## Feature Landscape

### Table Stakes (Users Expect These)

Any verify tier that closes the 2026-07-03 incident class ("Complete" stamped with a missing deliverable and an unexecuted pass criterion) must have these or the tier is theater.

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Real command execution, not LLM self-report | The incident itself: "pytest green" was never run — only the model's/human's belief that it would pass. GSD's `verify-phase.md` calls this "behavioral_verification" and treats it as separate from static/structural checks | MEDIUM | Verifier pod runs the declared gate command via bash in a checked-out worktree of the run branch; parses the real exit code. Never asks the LLM "would this pass?" |
| Deliverable-existence check against the actual run branch | Declared-deliverable-missing-from-pushed-branch is the exact incident. Must check the pushed/mergeable branch tip, not the ephemeral task PVC | LOW | `git ls-tree` / worktree file check at the run-branch HEAD, after Phase 34's `merge-base --is-ancestor` gate has already run |
| `gate_decision` enforced by the reconciler, not merely logged | An advisory-only verdict a human might miss reproduces the exact governance gap (the only verifier in the loop was a human diffing `filesTouched` by hand) | MEDIUM | Reconciler reads `gate_decision` and drives state transitions / halt conditions directly — same pattern as existing gate-policy honoring |
| Severity-tagged findings, persisted and reviewable | A blob of prose the operator has to re-parse doesn't scale past the first run | LOW | Small summary + counts in a `.status` condition; full findings list as an artifact per the envelopes-as-artifacts size×locality rule (never a blob in etcd) |
| do-not-touch / constraint-violation detection | Declared as an explicit requirement in the milestone (level-verify "confirm 'do-not-touch' constraints held") | LOW-MEDIUM | `git diff` the run branch against `baseRef`, restricted to the declared constraint paths; any hit is a finding |
| A first-class halt condition with resume discipline | Wave-boundary failure semantics require any new stop condition to be a real halt class, not a silent state flip — mirrors `ConditionBillingHalt`/`ConditionFailureHalt` | MEDIUM | `ConditionVerifyHalt`, project-level, following the same resume-ordering lessons Phase 25 already learned the hard way (clear the halt condition *and* reset the underlying resource correctly, or resume becomes a no-op) |
| Read-only verifier — never edits, commits, or pushes | Table stakes for trust: a "verifier" that can silently rewrite the thing it's grading is not a verifier | LOW (as a constraint; enforced by container image capability, not prompt discipline alone) | No file-edit tools, no git-write creds, no child-CRD authoring in the image |

### Differentiators (Competitive Advantage)

Features that make TIDE's verify tier more trustworthy than the two most common alternatives observed in prior art: GSD's host-side Markdown verifier (thorough but non-enforcing) and generic AI code-review bots (enforcing but generation-time-filtered, which the research shows costs recall).

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Bounded plan-check re-plan loop with stall detection | Catches a bad plan before any task spends money — cheapest place in the pipeline to reject work — while a hard, config-driven bound (not "keep trying") prevents runaway re-planning cost | MEDIUM | Directly modeled on GSD's `revision-loop.md` Check-Revise-Escalate pattern: track BLOCKER+WARNING count per attempt; if it doesn't strictly decrease, stop early and halt rather than burn remaining attempts |
| Coverage-not-conservatism finding generation, filtered downstream | Opus-family models are documented (repo `CLAUDE.md`) to honor "only report high-severity" literally and silently drop real lower-severity findings. Splitting "find everything, tag it" from "decide what blocks" into two separate steps (prompt-time vs. reconciler/gate-policy-time) avoids that failure mode entirely | LOW-MEDIUM | Prompt instructs the verifier to emit a finding for every deviation observed, tagged `severity` + `confidence`; gate policy (config, not the model) decides which severities force `BLOCKED`/`REJECT` |
| Distinct integration-check tier (cross-level, not just cross-task) | Phase 34 already closed cross-*task* integration mechanically (git-verified merge). Cross-*phase*/cross-*milestone* composition — "do sibling outputs actually work together" — is a different failure class the mechanical gate structurally cannot see, and is exactly the class the 2026-07-03 wave-parallel-integration-miss bug belongs to, one level up | HIGH | Needs a full run-branch build/test, not a single level's slice — real infra cost (whole-suite runs), not just a grep-and-diff pass |
| Goal-backward plan-check rubric | Verifies "will these tasks achieve the level objective," not "does the plan look well-formed" — catches plans that are internally consistent but don't actually serve the level's declared goal | MEDIUM | Rubric dimensions modeled on GSD's plan-checker: goal alignment, declared-vs-plausible file-touch sanity, dependency/wave correctness, and verifiability of each task's acceptance criterion |
| Same `Subagent` seam, new runtime | The verifier ships on a Python/LangGraph image behind the *existing* `pkg/dispatch.Subagent` interface — zero controller change to add a second concrete subagent runtime, and it seeds the LangGraph successor-runtime ladder for free | MEDIUM-HIGH | New image + envelope-in/out + `with_structured_output` for the verdict; no five-template parity, no file-edit tools |
| Fresh-evaluation discipline (no cached verdicts) | Mirrors the Phase 34 lesson verbatim: a cached completeness verdict can go stale; recomputing from git (and re-running the gate command) is the only thing that can't lie | LOW | `gate_decision` is always recomputed against current worktree state at verify time — never memoized in `.status` across reconciles |

### Anti-Features (Commonly Requested, Often Problematic)

Things a verify tier could plausibly grow into, given how naturally "verifier" invites "and also fix it" — explicitly rejected for Slack Tide.

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|------------------|-------------|
| Auto-fixing findings | "It already read the code, just have it patch the bug" | Collapses the verify/author boundary the milestone locks (Pillar 2: never commits, pushes, or authors); an auto-fixing verifier can no longer verify its own fix without a second, independent check | Emit findings only. A human, or the explicitly-deferred future debug-stage subagent, decides and authors the fix |
| Verifier re-authors the rejected plan directly | Feels efficient — skip the round-trip through the planner | The verifier and the planner would then share authorship of the same artifact with no independent check between them, defeating the point of a *separate* checking stage (this is precisely GSD's plan-checker/planner separation) | `REJECT` + findings appended to the **existing planner template's** next dispatch prompt; the planner (already-built authoring surface) re-authors |
| Unbounded / silent retry-until-pass on plan-check | "Just keep trying until it's good" | No stall detection means a stuck planner burns the full re-plan budget (and real $) on a plan that structurally can't pass; GSD's own revision-loop explicitly guards against this with issue-count-must-decrease | Bounded ≤N attempts (config), stall detection (non-decreasing BLOCKER+WARNING count halts early), then `ConditionVerifyHalt` for a human — never loop indefinitely |
| Auto-clearing a BLOCKED level-verify/integration-check to keep the run moving | Feels like it preserves throughput | Post-execution rework discards *paid* work — Pillar 3 is explicit that this decision belongs to the operator, not an auto-retry | `BLOCKED` halts immediately via `ConditionVerifyHalt`; only an explicit human action (the `tide resume`-style verb, following the `BillingHalt`/`FailureHalt` recovery discipline) clears it |
| Flake-tolerant gate-command execution (retry the test run N times, accept first green) | Real CI suites do have flaky tests | Repo doctrine (v1.0.7) is explicit: **no flake tolerance** anywhere in the verification path — a non-deterministic gate command is a bug to root-cause, and masking it with retries is exactly the failure mode that hid a real regression for days | Run the gate command once; a failure is a real finding. Flakiness itself becomes a `WARNING`/`BLOCKER` finding for the operator to root-cause, not something the verifier smooths over |
| Trusting the model's textual claim about test outcome instead of executing it | Cheaper (no bash execution, no worktree checkout) | This is the literal incident being fixed — the whole value of the tier is that it *runs* the declared command | Bash execution is a first-class, mandatory capability of the verifier image; `with_structured_output` reports the *parsed result* of a real run, never a self-reported guess |
| Scope creep into code-review / security-audit / general critique | "While it's reading the diff anyway, have it comment on style/security too" | Explicitly deferred per the seed's staged sequencing (verify tier first, quality tier — research/review/debug — second, compounding tier — learnings — third, cost-multiplier tier — tournament — last); conflating them muddies what `gate_decision` even means | Stay strictly inside plan-check / level-verify / integration-check for this milestone; code-review is its own future stage with its own report-only-by-default posture |
| Full authoring-parity image (file edits, commits, child-CRD emission) "since we're building a new subagent runtime anyway" | Efficient reuse of the new LangGraph image for future authoring roles | Directly contradicts Pillar 2's locked scope and reopens the "read-only" trust boundary that makes a verifier's verdict meaningful | Ship the minimal read-only surface now (envelope + git-read + bash + structured-output); authoring parity is an explicit later rung on the same successor-runtime ladder, gated by its own eval-harness evidence |

## Verdict Shape (all three stages share one schema)

Modeled directly on GSD's plan-checker/verifier YAML issue blocks (`references/few-shot-examples/plan-checker.md`, `templates/verification-report.md`) and on the LLM-as-judge literature's convention of a `{verdict, confidence, evidence, reason}` tuple, extended with TIDE's severity taxonomy:

```yaml
gate_decision: APPROVED | REJECT | BLOCKED   # REJECT = plan-check only (routes to re-plan); BLOCKED = level-verify/integration-check (terminal, human-gated)
stage: plan-check | level-verify | integration-check
level: milestone | phase | plan | project
findings:
  - id: F1
    dimension: goal_alignment | file_touch_plausibility | dependency_correctness
             | deliverable_existence | gate_command_result | constraint_violation
             | cross_level_composition
    severity: BLOCKER | WARNING | INFO
    confidence: HIGH | MEDIUM | LOW
    finding: "what was observed"
    evidence: "grep output / exit code / file diff / command transcript"
    affected_field: "which declared artifact/constraint/task this concerns"
    suggested_fix: "actionable next step (not auto-applied)"
summary: "N blockers, M warnings, K info"
```

**Enforcement split (the coverage-not-conservatism mechanism):** the verifier's prompt instructs it to emit a `finding` entry for *every* deviation it observes, tagged with severity + confidence — it never decides on its own to omit a low-severity finding. Gate *policy* (config, read by the reconciler) is what decides which severities force `REJECT`/`BLOCKED`. This is the explicit fix for the documented failure mode: Opus-family models told "only flag high-severity issues" comply literally and quietly drop real findings; splitting "observe and tag everything" from "decide what blocks" into two different actors (model vs. reconciler config) removes the model's ability to under-report by instruction-following too well.

## Stage Behaviors

### 1. plan-check — goal-backward, pre-dispatch

| Aspect | Behavior |
|---|---|
| Trigger | After a level's planner authors its plan/child-spec artifact; before any child (task/phase/etc.) is dispatched |
| Reads | Level objective (from the parent CRD's outcome/goal), the authored plan artifact, declared file-touch list, read-only worktree of the target repo |
| Rubric (adapted from GSD's plan-checker dimensions) | goal_alignment ("will these declared tasks actually achieve the level objective, goal-backward"), file_touch_plausibility (declared files vs. what's plausible given the objective and existing repo structure), dependency_correctness (wave/depends-on sanity — no same-wave file collisions without a declared dependency), verification_derivability (does every child have a checkable, non-trivial acceptance criterion — GSD's own negative example flags `echo "done"`-style unfalsifiable verify commands as a BLOCKER) |
| Verdict | `APPROVED` → proceed to dispatch. `REJECT` → bounded re-plan loop (below) |
| Cost posture | Cheapest place in the pipeline to reject — no task has spent yet |

**Bounded re-plan loop (concrete mechanism, adapted from GSD's Check-Revise-Escalate):**

```
attempt = 0
prev_blocker_warning_count = ∞

LOOP:
  run plan-check verifier → gate_decision, findings
  if gate_decision == APPROVED: proceed to dispatch, exit loop
  attempt += 1
  if attempt > N (config; default 1–2 per open question — recommend 2):
     → ConditionVerifyHalt (human must intervene; the loop does NOT silently
        proceed with an unvetted plan, unlike GSD's "proceed anyway" option —
        there is no live human prompt mid-reconcile, so the safe default is halt)
  count = count(findings where severity in {BLOCKER, WARNING})
  if count >= prev_blocker_warning_count:
     → ConditionVerifyHalt immediately ("re-plan loop stalled — issue count
        not decreasing"), do not consume remaining attempts
  prev_blocker_warning_count = count
  re-dispatch the SAME planner template (no new template) with findings
     appended verbatim to its prompt
  go to LOOP
```

Own counter, not shared with `maxAttemptsPerTask`: a rejected *plan* and a failed *task execution* are different failure classes (authoring quality vs. runtime flakiness) and should not share a budget or the two failure modes will mask each other in telemetry.

### 2. level-verify — post-execution, pre-Succeeded/pre-push

| Aspect | Behavior |
|---|---|
| Trigger | After all of a level's children report Succeeded; before the level itself stamps `Succeeded` and before its boundary push. Runs **alongside/after** Phase 34's mechanical `merge-base --is-ancestor` completeness gate — that gate is structural (is the work actually merged), this stage is semantic (does the merged work actually satisfy the level) |
| Reads | Level's declared deliverables list, declared "do-not-touch" constraint paths, the declared gate command (e.g. `pytest -q`, `make test`), a checked-out worktree at the run-branch tip |
| Executes | The declared gate command for real via bash in the worktree; parses the actual exit code and output — this is the direct fix for the incident ("pytest green" was declared as a pass criterion but never run in-cluster) |
| Checks | Every declared deliverable exists on the run branch (not just the task PVC); `git diff` against `baseRef` restricted to constraint paths shows no unauthorized touches |
| Verdict | `APPROVED` → level stamps `Succeeded`, boundary push proceeds. `BLOCKED` → terminal, no auto-retry |
| Failure semantics | `BLOCKED` fires `ConditionVerifyHalt` immediately for a human — Pillar 3 is explicit that post-execution rework discards already-paid-for work, so that call belongs to the operator, not an automatic loop |

### 3. integration-check — milestone/project boundary

| Aspect | Behavior |
|---|---|
| Trigger | At milestone/project boundaries — one level up from level-verify's per-level scope |
| Reads | The *entire* run branch, all sibling levels' declared deliverables collectively, cross-level composition claims (does phase A's output actually get consumed/exercised by phase B) |
| Executes | Build/test the full run branch as a whole (not one level's slice) — mirrors GSD's `gsd-integration-checker`, which is spawned separately from the per-phase verifier specifically to check cross-phase wiring and E2E flows that a single phase's own verification cannot see |
| Verdict | `APPROVED` / `BLOCKED`, same terminal halt semantics as level-verify |
| Distinct failure class it catches | The 2026-07-03 wave-parallel-integration-miss bug's *class*, one level up: siblings can each individually satisfy their own level-verify while still not composing (e.g., phase A's declared API shape silently drifts from what phase B actually calls) |
| Open design question (flagged in the milestone doc, not resolved here) | Whether this is a genuinely distinct template/rubric or `level-verify` parameterized at milestone/project scope. GSD's own precedent argues for **distinct**: `gsd-integration-checker` is a separate agent from `gsd-verifier` with different inputs (phase exports, API routes, milestone-wide requirement IDs) and a different question ("do these compose?" vs. "does this one thing exist and pass?"). Recommend: share the verifier image/dispatch machinery, but give integration-check its own rubric (cross-component wiring, E2E flow tracing) rather than reusing level-verify's per-deliverable checklist unmodified |

## Feature Dependencies

```
ConditionVerifyHalt
    └──requires──> BillingHalt/FailureHalt halt-and-resume pattern (Phase 13/25 precedent)
                       └──requires──> Phase 25's resume-ordering lesson: clear the halt
                                       condition AND reset the underlying resource state
                                       together, or resume becomes a no-op

level-verify gate-command execution
    └──requires──> read-only worktree checkout of the run branch
                       └──requires──> git-as-artifact-store surface (Phase 37) — the
                                       run branch is already the durable source of truth

level-verify deliverable-existence check
    └──requires──> Phase 34's mechanical `merge-base --is-ancestor` completeness gate
                       (structural prerequisite — level-verify checks semantics of what
                       that gate already proved is actually merged)

plan-check bounded re-plan loop
    └──requires──> existing 4 planner templates (re-dispatch target; no new authoring
                       template) + per-level stage-dispatch config surface

integration-check
    └──enhances──> level-verify (catches a class level-verify structurally cannot see:
                       cross-level composition, not single-level deliverable existence)

gate_decision persistence
    └──requires──> envelopes-as-artifacts size×locality rule — small summary in a
                       `.status` condition, full findings list as an artifact, never a
                       blob in etcd

Per-level verify stage config (on/off, model tier, gate policy severities)
    └──requires──> existing per-level gate/model config pattern on the Project CRD
                       (same mechanism, new leaf)

Read-only LangGraph specialist image
    └──requires──> pkg/dispatch.Subagent interface (UNCHANGED) + credproxy gating
                       (existing mechanism, second concrete runtime)

Coverage-not-conservatism prompting + severity/confidence tagging
    └──conflicts with──> naive "only report high-severity" prompting (documented
                       Opus-family failure mode: literal instruction-following silently
                       drops real lower-severity findings — CLAUDE.md subagent-tuning note)
```

### Dependency Notes

- **`ConditionVerifyHalt` requires the BillingHalt/FailureHalt pattern:** this milestone is explicitly told to mirror that pattern "including its resume/recovery discipline" — Phase 25 already paid the cost of learning that a resume verb must reset the underlying resource, not just clear the condition flag, or the verb becomes a silent no-op.
- **level-verify requires Phase 34's mechanical gate as a prerequisite, not a replacement:** Phase 34 proves the level's work is actually merged onto the run branch (structural). level-verify then checks whether that merged work is *semantically* sufficient (deliverables present, gate command green, constraints held). Running level-verify without Phase 34 first would let it "verify" a level whose work was never actually integrated.
- **integration-check enhances rather than duplicates level-verify:** the two must stay distinct in the rubric even if they share dispatch machinery, because siblings can each pass their own level-verify while still failing to compose — that gap is exactly what the incident's bug class demonstrated one level down.
- **gate_decision persistence conflicts with any "cache in .status" instinct:** the same size×locality rule that already governs planning artifacts applies here; a verify verdict must never be treated as a derivable aggregate stored wholesale in etcd.

## MVP Definition

### Launch With (v1 — this milestone, Slack Tide)

- [ ] **level-verify** — closes the headline gap (declared pass criterion never executed in-cluster); highest-value single feature in this milestone
- [ ] **plan-check with bounded re-plan loop** — cheapest place to reject bad work, before any spend
- [ ] **`ConditionVerifyHalt` + resume discipline** — required so a `BLOCKED`/exhausted-re-plan verdict actually stops the run rather than silently degrading
- [ ] **`gate_decision` schema with severity+confidence findings** — the machine-readable contract every stage and the reconciler share
- [ ] **Read-only LangGraph specialist image behind the unchanged `Subagent` seam** — the only way any of the above ships without breaking the "no vendor lock-in / pluggable runtime" constraint
- [ ] **Per-level stage-dispatch config (on/off + gate-policy severities)** — gate policy stays in config, per repo-wide principle; needed from day one so cost is controllable, not bolted on later
- [ ] **integration-check** — locked in scope by the milestone (Pillar 1 names all three stages); ships alongside the other two even though it is the highest-complexity item, because the incident's bug class is precisely a cross-level composition miss

### Add After Validation (v1.x)

- [ ] Broaden default stage-dispatch posture from milestone/project-only toward phase/plan-level, once level-verify has run against real cost and false-positive data — *trigger: at least one full external-repo run with the verify tier active and a false-positive rate low enough to justify wider default coverage*
- [ ] Per-stage model-tier tuning distinct from planner-tier defaults (cheap-model verify vs. planner-tier verify) — *trigger: cost data from the first runs shows verify-tier spend is a meaningful fraction of total run cost*
- [ ] Findings-artifact dashboard surfacing (beyond the `.status` condition summary) — *trigger: operators start needing to browse findings across runs, not just react to the current halt*

### Future Consideration (v2+ — explicitly deferred per the seed's staged sequencing)

- [ ] Debug-level subagent (diagnosis on task failure, replacing blind `maxAttemptsPerTask` retry) — *defer: quality tier, sequenced after the safety tier this milestone ships*
- [ ] Code-review stage (severity-tagged diff review, report-only by default) — *defer: quality tier; reuses this milestone's coverage-not-conservatism posture verbatim when it lands*
- [ ] Research/grounding stage (pre-planning dispatch producing a shared grounding artifact) — *defer: quality tier; inline planner grounding is "working well" per the seed, so this is lower urgency than verify*
- [ ] Extract-learnings stage (project-scoped + cluster-wide persistence) — *defer: compounding tier; only valuable once debug/verify/research exist as consumers of the learnings it would produce*
- [ ] Tournament-style candidate-plan judging — *defer: cost-multiplier tier, strictly config-gated (default N=1/off); most plausible at the plan level once verify-tier evidence justifies the extra spend*

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| level-verify (gate command execution + deliverable check) | HIGH | MEDIUM | P1 |
| plan-check (goal-backward rubric) | HIGH | MEDIUM | P1 |
| Bounded re-plan loop + stall detection | HIGH | LOW-MEDIUM | P1 |
| `ConditionVerifyHalt` + resume discipline | HIGH | MEDIUM (mirrors existing pattern) | P1 |
| `gate_decision` schema + severity/confidence tagging | HIGH | LOW | P1 |
| Read-only LangGraph specialist image | HIGH (unblocks everything above + seeds the ladder) | MEDIUM-HIGH (new runtime, new image, SSL/CA-bundle question open) | P1 |
| Per-level stage-dispatch config + gate policy | MEDIUM-HIGH | LOW | P1 |
| integration-check (cross-level composition) | HIGH | HIGH (whole-branch build/test, distinct rubric) | P1 (locked in scope) |
| Findings-artifact dashboard view | MEDIUM | MEDIUM | P2 |
| Per-stage model-tier tuning | MEDIUM | LOW | P2 |
| Wider default stage-dispatch posture (all levels) | MEDIUM | LOW (config only, once evidence exists) | P2 |
| Debug / code-review / research / learnings / tournament stages | deferred by design | — | P3 (out of this milestone) |

## Prior-Art Comparison

| Aspect | GSD (bootstrap host, Markdown-based) | Generic AI code-review agents (e.g. Cloudflare's multi-agent reviewer) | TIDE Slack Tide |
|---|---|---|---|
| Verdict | `PASSED / gaps_found / human_needed` (verifier); `PASSED / ISSUES FOUND` (plan-checker) — a human-read report | approve / request-changes + inline PR comments | `APPROVED / BLOCKED` (level-verify, integration-check); `APPROVED / REJECT` (plan-check) — machine-enforced by the reconciler, not just a report a human might skip |
| Bounded revision loop | max 3 iterations, issue-count-must-decrease stall detection, escalate to a live human prompt after 3 | typically none — single-pass, human merges or doesn't | plan-check only, ≤N (config, default 1–2), same stall-detection principle, escalates to `ConditionVerifyHalt` (no live mid-reconcile human prompt exists, so halt is the safe default rather than GSD's "proceed anyway?") |
| Finding-generation vs. filtering split | Checker both finds and judges in one pass; no documented separate coverage/filter step | Exclusion-at-generation (tell the reviewer up front what NOT to flag) *plus* a downstream "reasonableness filter" that drops speculative/false-positive findings | Coverage-at-generation (tag severity + confidence on everything observed, suppress nothing) *plus* downstream filtering entirely in gate-policy config — chosen specifically because Opus-family models are documented to under-report when told "only flag high severity" at generation time |
| Execution vs. self-report | Runs the real test suite and real CLI commands ("behavioral_verification") — does not trust static claims | Typically static diff review only; no execution step | Runs the declared gate command for real in a worktree — the direct fix for the incident that motivated this milestone |
| Persistence | Markdown files in the repo, human-reviewed manually | Inline PR comments, ephemeral to the review tool | `gate_decision` status condition (small, structured) + findings artifact via git-as-artifact-store (large), both consumed by the reconciler automatically |
| Distinct integration tier | Yes — `gsd-integration-checker` is a separate agent from the per-phase verifier, spawned by `audit-milestone`, with cross-phase-specific inputs (phase exports, API routes, milestone requirement IDs) | Not applicable — no cross-repo/cross-phase composition concept | Locked as its own stage, one level up from level-verify, following GSD's own precedent that this needs a distinct rubric even when it shares dispatch machinery |

## Sources

- `.planning/PROJECT.md` — v1.0.9 "Slack Tide" scope, prior Key Decisions (BillingHalt/FailureHalt pattern, Phase 34 mechanical completeness gate, git-as-artifact-store, resume-ordering lessons from Phase 25)
- `.planning/seeds/verify-level-subagent.md` — the stage inventory, gap maps, and staged-sequencing rationale this milestone implements
- `.planning/milestones/vnext-specialist-verify-MILESTONE.md` — locked architecture pillars (verify-tier-only scope, read-only LangGraph image, failure semantics, open questions)
- `~/.claude/get-shit-done/workflows/verify-phase.md` — GSD's goal-backward post-execution verifier (level-verify analog): must-haves derivation, behavioral_verification (real test/CLI execution), anti-pattern scanning, test-quality audit, status decision tree, deferred-item filtering against later phases
- `~/.claude/get-shit-done/workflows/audit-milestone.md` and `references/agent-contracts.md` — GSD's integration-checker (integration-check analog): distinct agent from the per-phase verifier, spawned separately with cross-phase inputs; 3-source requirement cross-reference; orphan detection
- `~/.claude/get-shit-done/references/revision-loop.md` — the Check-Revise-Escalate bounded-loop pattern (max 3, issue-count-must-decrease stall detection, escalate-after-N) this milestone's plan-check re-plan loop is modeled on
- `~/.claude/get-shit-done/references/few-shot-examples/plan-checker.md` and `few-shot-examples/verifier.md` — concrete positive/negative calibration examples for the goal-backward and file-touch/dependency rubrics, and for what a lazy/insufficient verifier looks like
- `~/.claude/get-shit-done/templates/verification-report.md` — the verdict/evidence/severity table shape (`✓ VERIFIED / ✗ FAILED / ? UNCERTAIN`, `🛑 Blocker / ⚠️ Warning / ℹ️ Info`) this milestone's `gate_decision` schema generalizes into a machine-readable form
- Anthropic, ["Building Effective AI Agents"](https://www.anthropic.com/research/building-effective-agents) — the evaluator-optimizer workflow pattern (one call generates, another evaluates and feeds back in a loop) that plan-check/level-verify/integration-check all instantiate
- Cloudflare Engineering, ["Orchestrating AI Code Review at scale"](https://blog.cloudflare.com/ai-code-review/) — three-tier severity taxonomy (critical/warning/suggestion) driving structured, machine-parsed findings; the "reasonableness filter" downstream-filtering precedent; explicit confirmation that this class of tool does NOT commonly use a confidence field, and typically filters at generation time rather than at policy time (the point of divergence TIDE's coverage-not-conservatism posture deliberately corrects)
- LLM-as-judge survey sources (evidentlyai.com, futureagi.com, Weights & Biases) — confirms `{verdict, confidence, evidence, reason}` as the common structured-judge schema shape, and that self-reported LLM confidence is known to correlate weakly with correctness (informs treating `confidence` as a routing/triage signal, not a certainty guarantee)
- LangGraph/LangChain structured-output documentation search — confirms `.with_structured_output()` + Pydantic validation with automatic retry-on-validation-failure is the standard mechanism for a reliable `gate_decision` verdict; version-pin re-verification explicitly deferred to plan-phase per the milestone doc

---
*Feature research for: TIDE v1.0.9 "Slack Tide" — in-cluster verify tier*
*Researched: 2026-07-18*
