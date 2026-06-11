# Phase 13: Dispatch Image Resolution + Provider Halt - Context

**Gathered:** 2026-06-11
**Status:** Ready for planning

<domain>
## Phase Boundary

Subagent-image resolution works via the documented chain at all four reconciler dispatch sites (closing the v1.0.0 stub-image bug), the released chart stops silently forcing the stub, and a provider billing 400 ("credit balance is too low") halts the project instead of burning sessions one at a time. Requirements: DISPATCH-01, DISPATCH-02, HALT-01. Every fix carries a regression test reproducing the run-1 symptom.

</domain>

<decisions>
## Implementation Decisions

### Chart image default posture (DISPATCH-02)
- **D-01: Drop the `--subagent-image` flag from the chart's deployment args.** deployment.yaml:30's unconditional `--subagent-image={{ .Values.images.stubSubagent.* }}` goes away. Resolution becomes: `Levels.<level>.Image` → `Spec.Subagent.Image` → `subagent.defaults.image` helm value (already ships `ghcr.io/jsquirrelz/tide-claude-subagent` via the CLAUDE_SUBAGENT_IMAGE env — currently dead config that this decision brings alive). Production installs dispatch the real subagent out of the box.
- **D-02: Stub becomes opt-in.** Test installs (kind harness, CI, chaos-resume) set the stub explicitly (`--set subagent.defaults.image=...stub` or equivalent). The kind/Layer-B fixtures and acceptance scripts that relied on the implicit stub flag must be updated in the same phase — a green test suite after the chart change is part of the deliverable.
- Detection-symptom note for verification: the v1.0 bug's signature was a milestone pod completing in seconds with termination message `"reason":"planner stub success"` and `stub-*` children — the regression test asserts a Project pinning a real image never produces that.

### Image-chain wiring (DISPATCH-01)
- **D-03: No CRD changes.** `Spec.Subagent.Image` (project_types.go:151) and `Levels.<level>.Image` (:199, "schema-present-but-not-enforced") already exist — this is pure controller wiring in ResolveProvider/dispatch options at the four sites, mirroring the existing Model chain in dispatch_helpers.go ResolveProvider.

### Billing-400 detection (HALT-01)
- **D-04: Detect at BOTH layers.** credproxy (fronts every Anthropic API call) recognizes the 400 credit-exhaustion response and fails the session fast at the FIRST 400 — before the session burns more context and before siblings ramp; reconcilers ALSO classify the billing-failure class from failed-Job envelopes/termination messages as the backstop (and as the cheaply-testable layer). Reporting channel mechanics are planner discretion but MUST follow the envelopes-as-artifacts rules (tiny status via termination message; never blobs in etcd; no new cross-namespace write paths).

### Halt semantics + recovery (HALT-01)
- **D-05: BillingHalt blocks all new dispatch project-wide; in-flight sessions fail fast on their own (no Job killing).** The condition lands on the Project status (visible to kubectl + dashboard); every level reconciler checks it before Job creation (same hold pattern as Phase 12's checkParentApproval/CheckRejected dispatch-entry holds). Affected levels PARK (consistent with Phase 12 D-05 park-not-fail) rather than cascading Failed.
- **D-06: Manual recovery via `tide resume`.** Operator refills credits, runs `tide resume` — which clears BillingHalt and lifts the parks (extends Phase 12's resume semantics; `--retry-failed` already covers any levels that genuinely Failed during the dry-out). NO auto-probe: never spend API calls testing an empty balance.

### Claude's Discretion
- credproxy→controller reporting channel mechanics (termination message vs envelope error code vs both) within the envelopes-as-artifacts constraints.
- Exact condition type/reason naming (`BillingHalt` is the working name; follow api/v1alpha1 condition conventions).
- Whether the dispatch-entry billing hold shares a helper with Phase 12's holds (checkParentApproval/CheckRejected) — a unified "dispatch gate" helper is fine if it stays readable.
- Error-string matching robustness (Anthropic 400 body shape; don't overfit to one message string — classify on status 400 + recognizable credit-exhaustion signature).
- Kind-harness stub wiring mechanism (helm --set in test scripts vs fixture values files).

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Image resolution
- `internal/controller/dispatch_helpers.go` — ResolveProvider (:125-165): the Model precedence chain is the exact analog for Image; the four dispatch sites consume `r.SubagentImage` with HelmProviderDefaults fallback (milestone_controller.go:380-388 shape, mirrored in phase/plan/task).
- `api/v1alpha1/project_types.go` — :143-151 (Spec.Subagent.Image + documented chain), :194-199 (Levels.<level>.Image, schema-present-but-not-enforced).
- `charts/tide/templates/deployment.yaml` — :30 (the flag to drop), :45 (CLAUDE_SUBAGENT_IMAGE env from subagent.defaults.image — the surviving default channel).
- Memory file `project_v1_stub_image_bug.md` — bug signature, run-1 workaround, fix shape.

### Billing halt
- `internal/credproxy/server.go` — the proxy that fronts API calls (D-04 fast-path lands here).
- Memory file `project_envelopes_as_artifacts.md` rules (DECIDED 2026-06-08): tiny status via termination message; API-created CRs for small cross-ns; never blobs in etcd.
- Phase 12 dispatch-entry holds — `internal/controller/dispatch_helpers.go` checkParentApproval + the CheckRejected holds (the pattern D-05's billing hold mirrors); `.planning/phases/12-gate-semantics-reject-resume/12-CONTEXT.md` D-05/D-06 (park-not-fail; resume as recovery verb).

### Test surfaces
- `test/integration/kind/` Layer B fixtures + `hack/` acceptance scripts that install the chart with the implicit stub flag (D-02 updates these; CLAUDE.md test-int exit discipline applies).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- ResolveProvider's Model chain (dispatch_helpers.go) — copy the precedence walk for Image; HelmProviderDefaults already carries an Image field used as fallback.
- CLAUDE_SUBAGENT_IMAGE env plumbing (chart → manager → reconcilers' HelmProviderDefaults.Image) — already wired, just shadowed by the flag.
- Phase 12's dispatch-entry hold pattern (checkParentApproval) — the BillingHalt hold is the third instance of the same shape.
- `tide resume` (cmd/tide/resume.go) — extend to clear BillingHalt; `--retry-failed` already handles Failed levels.

### Established Patterns
- Conditions via `tideprojectv1alpha1.Reason*` constants + MergeFrom status patches.
- Chart is FIXED contract: dropping the flag is a deliberate, documented deploy-surface change (values.yaml comment explains the new resolution chain + how to opt into the stub).
- Provider firewall: all Anthropic-specific classification stays behind the subagent/credproxy boundary (`tools/analyzers/providerfirewall` enforces).

### Integration Points
- Four reconciler dispatch sites (milestone/phase/plan/task) consume the resolved image.
- Project status conditions surface on dashboard (Phase 15 CUTS-05 fixes the chip map; BillingHalt should use existing condition plumbing so it's visible without dashboard changes).
- HALT recovery rides Phase 12's resume verb — keep semantics composable, not bespoke.

</code_context>

<specifics>
## Specific Ideas

- Run-1 symptom for HALT-01's regression test: two credit dry-outs, full fan-out crashing one session at a time, ~$14-16 wasted per dry-out on context ramps. The test asserts: first billing-classified failure → BillingHalt condition on Project → zero new Jobs dispatched at any level while present → `tide resume` clears it and dispatch resumes.
- DISPATCH-01 regression: a Project with `spec.subagent.levels.plan.image` set dispatches exactly that image in the plan-level Job (kubectl-observable assertion per ROADMAP success criterion 1).

</specifics>

<deferred>
## Deferred Ideas

- Provider-key budget surfaced on dashboard (COST-02, Future Requirements) — billing VISIBILITY beyond the halt condition.
- Auto-probe/auto-recovery of billing halts — rejected for v1.0.1 (D-06), revisit only with a probe that doesn't spend tokens.

</deferred>

---

*Phase: 13-Dispatch Image Resolution + Provider Halt*
*Context gathered: 2026-06-11*
