---
phase: 51
slug: the-task-loop
status: verified
threats_open: 0
asvs_level: 1
created: 2026-07-19
---

# Phase 51 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Register source: union of the `<threat_model>` blocks across all 8 plans
> (`register_authored_at_plan_time: true`). No `## Threat Flags` sections exist
> in any SUMMARY (verified by grep). Two post-plan changes (commit `076c9637`)
> assessed against the existing register — see "Post-Plan Change Assessment".

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| planner → K8s API (Task spec authoring) | A later actor must not silently mutate a Locked `spec.verification` | verification contract (gateCommand, commands, maxIterations) |
| LLM judge → assembled verdict | Probabilistic verdict must never override the deterministic gate result | GateDecision verdict tier + findings |
| orchestrator env → verifier gate command | `TIDE_GATE_COMMAND` is orchestrator-set from the locked contract, never model-supplied | shell command string |
| verifier prompt → evaluator behavior | Conservatism directive would suppress real low-severity findings | finding set integrity |
| manager → verifier pod filesystem/env | Worktree stays read-only; only `envelopes/<uid>/` is writable; no git-write/push creds | worktree, envelope out.json, credentials |
| executor belief-complete → verifier dispatch | Correctness stamped by the independent verifier, never the executor | verdict authority |
| halt condition → dispatch gate | A halted project stops spending across ALL five dispatch chains uniformly | dispatch authorization |
| attempt diff → evaluator integrity | An attempt must not weaken the evaluator/fixtures to force a pass | RunEvidence.ChangedFiles |
| verifier verdict → Task correctness | Unreadable/ambiguous verdict must never collapse to APPROVED | termination stub / envelope verdict |
| concurrent verifier dispatch → node memory | Unbounded verifier fan-out OOMs a single node (run-2b D3) | Job creation rate |
| verifier pod → controller (verdict relay) | TerminationStub.gateDecision grafted into EnvelopeOut.Verdict (post-plan, `076c9637`) | verdict tier enum, findings counts |

---

## Threat Register

All dispositions are `mitigate` unless marked N/A. All mitigations verified present in code on 2026-07-19 (HEAD lineage of `076c9637`, phase verification `gate_decision: APPROVED`).

| Threat ID | Category | Component | Disposition | Mitigation (verified evidence) | Status |
|-----------|----------|-----------|-------------|-------------------------------|--------|
| T-51-01 | Tampering | `spec.verification` post-lock mutation | mitigate | CEL transition rule `api/v1alpha3/task_types.go:90` — `oldSelf.phase != 'Locked' \|\| self == oldSelf \|\| self.phase == 'Superseded'` | closed |
| T-51-01b | Tampering | invalid `phase` / `onExhaustion` values | mitigate | `task_types.go:97` `Enum=Draft;Locked;Superseded`; `:148` `Enum=escalate;requireApproval` | closed |
| T-51-01c | Repudiation | which contract was dispatched | mitigate | `LockedSHA` status field `task_types.go:332-341`; stamped at dispatch `internal/controller/task_controller.go:1531` | closed |
| T-51-02 | Tampering/Repudiation | LLM judge overriding a red gate (in-pod leg) | mitigate | Out-of-band capture of EACH pass-criterion `cmd/tide-langgraph-verifier/verifier/__main__.py:198` (`_run_commands_out_of_band`, timeout→124 at :111); `_assemble_verdict` :115-154 — dominance only pulls DOWN, empty command set → BLOCKED, gate-command blocker findings carry the structural signal | closed |
| T-51-02b | Elevation/Tampering | command injection via gateCommand | mitigate | `tools.py:148` `del command` discards any model-supplied arg; :56 reads ONLY `TIDE_GATE_COMMAND`; fail-closed when unset (:145); gate runs in the RO verifier pod | closed |
| T-51-02c | Spoofing | wrong image accepting a foreign vendor | mitigate | `__main__.py:40` `SUPPORTED_VENDOR = "langgraph"`; refusal at :181-186 (fail-closed `_fail`) | closed |
| T-51-03 | Repudiation/Info Disclosure | dropped low-severity findings | mitigate | `task_verifier.tmpl:63-69` — "a finding for EVERY deviation … Coverage is your job, not triage", explicit severity + confidence per finding, policy (not the model) decides blocking. Survives the v4 rewrite | closed |
| T-51-03b | Tampering | stale PromptTemplateVersion | mitigate | `prompt_templates.go:48` `PromptTemplateVersion = "v4"` co-bumped v3→v4 in the SAME commit (`076c9637`) as the template rewrite | closed |
| T-51-03c | Repudiation | evaluator provenance missing from trace tree | mitigate | EVALUATOR sibling span first-populates `evaluation.*`/`human_intervention` (`span_emission.go:239-240, 295-397`; `pkg/otelai/attrs.go:251-256`); no double-emission — `SelfInstruments("langgraph")=true` (`pkg/dispatch/vendor_capabilities.go:40`, fail-closed default) skips reporter synthesis | closed |
| T-51-04 | Elevation | verifier gains write/push capability | mitigate | `/workspace` mount `ReadOnly: opts.ReadOnly` (`jobspec.go:453`); `ReadOnlyRootFilesystem` (:549); static test asserts no git-cred env/EnvFrom on ANY container (`jobspec_readonly_test.go:148-176`) | closed |
| T-51-04b | Tampering | verifier writing into the shared worktree | mitigate | Second mount subPath-scoped to `envelopes/<uid>/` only, RW at the matching nested path (`jobspec.go:463-477`); worktree root mount stays RO | closed |
| T-51-04c | Elevation/Tampering | command injection via gate command | mitigate | `TIDE_GATE_COMMAND` stamped only from `opts.GateCommand` (`jobspec.go:497-505`), set by the manager from `task.Spec.Verification.GateCommand` (`task_controller.go:2184`) — never a model tool arg | closed |
| T-51-05 | DoS (spend) | Project planner dispatching under a conservative halt | mitigate | `checkFailureHalt` + `checkVerifyHalt` on the Project chain (`project_controller.go:1549, 1557`) | closed |
| T-51-05b | Tampering (divergence) | different hold firing by level | mitigate | Uniform `checkDispatchHolds` (`dispatch_helpers.go:628`, order Billing→Failure→Verify→Budget→Import) consumed by plan `:335`, phase `:336`, milestone `:339`, task `:474` | closed |
| T-51-05c | Tampering | VerifyHalt re-freezing after resume | mitigate | CR-02 time-fence `verify_halt.go:93-101` — `taskCompletedAt.Before(resumedAt)` no-ops the stale pre-resume straggler | closed |
| T-51-05d | Availability | VerifyHalt mis-reinterpreting Failed semantics | mitigate | ESC-03 distinct-class regression `task_verify_loop_test.go:603-672` — VerifyHalt never stamps conservative FailureHalt; wave sibling untouched | closed |
| T-51-05e | DoS (spend) | migration drops the reservation-headroom hold | mitigate | Task-only `HasHeadroom` gate preserved AFTER `checkDispatchHolds` (`task_controller.go:492-498`) | closed |
| T-51-06 | DoS (cost/concurrency) | unbounded verifier dispatch | mitigate | `verifierInFlightCount` cap-before-acquire (`task_controller.go:2082-2123`); `BudgetCents` reservation (:2089, :1303); committed-flag deferred release (:313-332, :701) | closed |
| T-51-06b | Tampering | dispatching a drifted (non-locked) contract | mitigate | Dispatch requires `Phase == Locked` (:2075); `GateCommand`/`Commands` read from the immutable spec (:2184, :2232); `LockedSHA` stamped (:1531) | closed |
| T-51-06c | Repudiation | evaluator run untraceable/double-emitted | mitigate | `SelfInstruments(...Vendor)` at :1171 routes reporter to skip events.jsonl synthesis for langgraph (`vendor_capabilities.go:40`) | closed |
| T-51-06d | Elevation | verifier dispatched with write capability | mitigate | Verifier `BuildOptions.ReadOnly: true` at the dispatch site (`task_controller.go:2183`) | closed |
| T-51-07 | Tampering/Repudiation | LLM APPROVED over a red gate (controller leg) | mitigate | `hasDeterministicFailure` (:2344-2349) dominates APPROVED in `handleVerifierCompletion` (:2894-2899), layered on fail-closed `ClassifyVerdict` re-derive (:2883-2894); regression `task_verify_loop_test.go:341` "APPROVED can NEVER pass over a red gate-command finding" | closed |
| T-51-07b | Tampering (integrity) | agent games the evaluator/fixtures | mitigate | `intersectsProtected(out.RunEvidence.ChangedFiles, protectedPathsFor(task))` → escalation, never a pass (:1505); prefix-match set :2329-2377; true-positive/negative tests in `task_verify_loop_test.go` (TASK-06) | closed |
| T-51-07c | Availability/DoS | unbounded retry-until-pass | mitigate | `Attempt >= MaxIterations` → onExhaustion → `ConditionVerifyHalt` (:2643-2645 → `verify_halt.go`) | closed |
| T-51-07d | Repudiation | prior-agent context leaking into fresh attempt | mitigate | Fresh attempt seeded ONLY with locked spec + bounded evidence packet — `stageEvidencePacket` (:2678), findings capped at 20 (:2327, :2686), reference-only path via `VerifyContext.EvidencePacketPath` (:2037) | closed |
| T-51-07e | Availability | loop state lost on controller restart | mitigate | `applyLoopStatus` current-iteration-only, no accumulating history (:2431-2445, `TestLoopStatus_NoForbiddenFields`); simulated-restart re-derive test `task_verify_loop_test.go:722-771` | closed |
| T-51-08 | DoS | concurrent verifier dispatch OOMing a node | mitigate | `test/integration/kind/verifier_concurrency_test.go` (cap-hit DEFERS, no slot leak); PASSED live 2026-07-20 — `Ran 1 of 27 Specs … SUCCESS!` (`51-HUMAN-UAT.md:93`) | closed |
| T-51-08b | Elevation | live verifier gaining write/push | mitigate | Live proof PASSED 2026-07-20 on kind-tide-test with a real key: read-only verifier, no git-write creds, both red/green gate paths end-to-end (`51-HUMAN-UAT.md:16-17`, phase verification APPROVED) | closed |
| T-51-SC (×8, one per plan) | Tampering | package installs | N/A | No package installs in any plan — `git log --since=2026-07-18` shows zero changes to `go.mod`/`go.sum`/verifier Python dependency files; verifier image deps remain Phase-48 patch-exact pinned + CI-gated | closed (N/A) |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party) · N/A (not applicable)*

---

## Post-Plan Change Assessment (commit `076c9637`)

Two changes landed after the plans' threat models were authored; assessed against the existing register's categories per the audit constraints.

### 1. `PodStatusEnvelopeReader.ReadVerifierOut` — TerminationStub verdict graft

`internal/dispatch/podjob/backend.go:211-269` grafts `TerminationStub.GateDecision` (written by the verifier pod to its termination message) into `EnvelopeOut.Verdict`.

**Verdict: preserves T-51-02's dominance guarantee.**

- The stub's `gateDecision` is stamped AFTER in-pod dominance: `__main__.py:208` runs `_assemble_verdict` (out-of-band gate results dominate) before `write_termination_stub` receives `verdict_out["verdict"]` (:217-220). An APPROVED stub tier structurally implies zero red gate commands in-pod.
- The controller re-derives the tier through fail-closed `ClassifyVerdict` (`task_controller.go:2883-2894`) — an out-of-vocabulary or malformed stub string collapses to BLOCKED (`pkg/dispatch/verdict_test.go:94-101`). An empty `gateDecision` (the `_fail` stub shape) leaves `out.Verdict == nil` → fail-closed `VerifierVerdictMissing` halt (:2873-2875).
- Trust domain unchanged: the stub and the PVC `out.json` are authored by the same verifier pod; relaying the tier via the stub grants the pod no authority it did not already hold.
- The role-aware selection (`role=verifier` label + highest attempt, `backend.go:217-244`) CLOSES a live-proof defect — the prior `ReadOut` path listed by task-uid alone and could read the EXECUTOR pod's termination message as the verdict (cache-order roulette).
- **Residual (low, consistent with the register's declared residuals):** the stub carries no findings, so on the stub relay path the controller-side `hasDeterministicFailure` leg (T-51-07) is vacuous and protection rests on the in-pod leg (T-51-02) plus fail-closed classification. The two legs were declared defence-in-depth of each other in both plans; one leg fully active per path is within the accepted residual. The controller leg remains fully active for envelope-shaped verdicts carrying findings.

### 2. `task_verifier.tmpl` rewrite (v3→v4: verdict semantics, tool honesty, termination pressure)

**Verdict: preserves T-51-03 and T-51-02b.**

- T-51-03 coverage-not-conservatism: the directive survives intact and explicit — "Report a finding for EVERY deviation you observe, however small," with per-finding severity + confidence, and "Config-driven gate policy, not you, decides which findings actually block" (`task_verifier.tmpl:63-69`). Template package tests pass (`go test ./internal/subagent/common/` → ok).
- T-51-02b command-injection discipline: the commit does not touch `tools.py`; `run_gate_command` still discards any model-supplied argument (`del command`, :148) and reads only the orchestrator-set env. The template renders `{{.Verify.GateCommand}}` as display text only — the model cannot route a command through the tool. `PromptTemplateVersion` co-bumped v3→v4 in the same commit (T-51-03b honored).
- The verdict-semantics change (red gate → REPAIRABLE-vs-BLOCKED repairability judgment, not automatic BLOCKED) does NOT weaken dominance: the template states "A non-zero gate exit means the attempt can never be APPROVED" (:52) and that the orchestrator "structurally enforces that dominance regardless of what you write" (:54-56) — the model's latitude is confined to the non-APPROVED tiers.

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|

No accepted risks. (Declared low residuals on T-51-02/T-51-07 are defence-in-depth notes inside `mitigate` dispositions, not accepted risks.)

---

## Unregistered Flags

None. No `## Threat Flags` sections exist in any of the 8 SUMMARY files. The one piece of post-plan attack surface (the termination-stub verdict relay) maps to existing threats T-51-02/T-51-07 and is assessed above.

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-07-19 | 28 mitigate + 8 N/A | 36 | 0 | gsd-security-auditor |

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer / N/A)
- [x] Accepted risks documented in Accepted Risks Log (none)
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-07-19
