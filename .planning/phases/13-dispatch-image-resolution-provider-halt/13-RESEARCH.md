# Phase 13: Dispatch Image Resolution + Provider Halt — Research

**Researched:** 2026-06-11
**Domain:** Go/controller-runtime — CRD reconciler wiring, Helm chart deploy surface, credproxy response interception
**Confidence:** HIGH

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**D-01: Drop the `--subagent-image` flag from the chart's deployment args.**
`deployment.yaml:30`'s unconditional `--subagent-image={{ .Values.images.stubSubagent.* }}` goes away. Resolution becomes: `Levels.<level>.Image` → `Spec.Subagent.Image` → `subagent.defaults.image` helm value (already ships `ghcr.io/jsquirrelz/tide-claude-subagent` via the `CLAUDE_SUBAGENT_IMAGE` env — currently dead config that this decision brings alive). Production installs dispatch the real subagent out of the box.

**D-02: Stub becomes opt-in.** Test installs (kind harness, CI, chaos-resume) set the stub explicitly (`--set subagent.defaults.image=...stub` or equivalent). The kind/Layer-B fixtures and acceptance scripts that relied on the implicit stub flag must be updated in the same phase — a green test suite after the chart change is part of the deliverable.

Detection-symptom note for verification: the v1.0 bug's signature was a milestone pod completing in seconds with termination message `"reason":"planner stub success"` and `stub-*` children — the regression test asserts a Project pinning a real image never produces that.

**D-03: No CRD changes.** `Spec.Subagent.Image` (project_types.go:151) and `Levels.<level>.Image` (:199, "schema-present-but-not-enforced") already exist — this is pure controller wiring in ResolveProvider/dispatch options at the four sites, mirroring the existing Model chain in dispatch_helpers.go ResolveProvider.

**D-04: Detect at BOTH layers.** credproxy (fronts every Anthropic API call) recognizes the 400 credit-exhaustion response and fails the session fast at the FIRST 400 — before the session burns more context and before siblings ramp; reconcilers ALSO classify the billing-failure class from failed-Job envelopes/termination messages as the backstop (and as the cheaply-testable layer). Reporting channel mechanics are planner discretion but MUST follow the envelopes-as-artifacts rules (tiny status via termination message; never blobs in etcd; no new cross-namespace write paths).

**D-05: BillingHalt blocks all new dispatch project-wide; in-flight sessions fail fast on their own (no Job killing).** The condition lands on the Project status (visible to kubectl + dashboard); every level reconciler checks it before Job creation (same hold pattern as Phase 12's checkParentApproval/CheckRejected dispatch-entry holds). Affected levels PARK (consistent with Phase 12 D-05 park-not-fail) rather than cascading Failed.

**D-06: Manual recovery via `tide resume`.** Operator refills credits, runs `tide resume` — which clears BillingHalt and lifts the parks (extends Phase 12's resume semantics; `--retry-failed` already covers any levels that genuinely Failed during the dry-out). NO auto-probe: never spend API calls testing an empty balance.

### Claude's Discretion

- credproxy→controller reporting channel mechanics (termination message vs envelope error code vs both) within the envelopes-as-artifacts constraints.
- Exact condition type/reason naming (`BillingHalt` is the working name; follow api/v1alpha1 condition conventions).
- Whether the dispatch-entry billing hold shares a helper with Phase 12's holds (checkParentApproval/CheckRejected) — a unified "dispatch gate" helper is fine if it stays readable.
- Error-string matching robustness (Anthropic 400 body shape; don't overfit to one message string — classify on status 400 + recognizable credit-exhaustion signature).
- Kind-harness stub wiring mechanism (helm --set in test scripts vs fixture values files).

### Deferred Ideas (OUT OF SCOPE)

- Provider-key budget surfaced on dashboard (COST-02, Future Requirements) — billing VISIBILITY beyond the halt condition.
- Auto-probe/auto-recovery of billing halts — rejected for v1.0.1 (D-06), revisit only with a probe that doesn't spend tokens.

</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| DISPATCH-01 | Subagent image resolves via the documented chain (`Levels.<level>.Image` → `Spec.Subagent.Image` → flag/Helm default) at all four reconciler dispatch sites | Image chain is structurally identical to the existing Model chain in `ResolveProvider`; each dispatch site has a 2-line pattern to replace |
| DISPATCH-02 | A released-chart install with a Project pinning a real image dispatches that image — no silent stub override; chart's `--subagent-image=stub` default posture explicitly decided and documented | D-01 drops the flag from deployment.yaml:30; `CLAUDE_SUBAGENT_IMAGE` env (deployment.yaml:45) becomes the Helm-default channel; kind/acceptance test stubs need updating |
| HALT-01 | A provider billing 400 ("credit balance is too low") halts further dispatch project-wide and surfaces a condition on the Project, instead of crashing the fan-out one session at a time | Two-layer detection: credproxy ModifyResponse classifier + reconciler envelope classifier; hold pattern mirrors checkParentApproval/CheckRejected; resume extends `tide resume` verb |

</phase_requirements>

---

## Summary

Phase 13 closes two independent but dispatch-site-adjacent bugs from dogfood run 1. DISPATCH-01/02 fix the stub-image lock-in: all four reconcilers prefer `r.SubagentImage` (the `--subagent-image` flag) unconditionally, shadowing both the CRD-level image fields and `CLAUDE_SUBAGENT_IMAGE` env. The schema fields already exist and document the intended precedence chain — this is purely missing controller wiring, mirroring the existing `ResolveProvider` Model chain. HALT-01 prevents the "$80 of $140.64 burned by dying sessions" run-1 pattern: a provider billing 400 must halt the project rather than letting the full fan-out exhaust itself session by session.

The image fix is mechanical: extend `ResolveProvider` (or add a parallel `resolveImage` function) to walk `Levels.<level>.Image` → `Spec.Subagent.Image` → `helmDefaults.Image`, then replace the 2-line `r.SubagentImage / HelmProviderDefaults.Image` fallback at each of the five dispatch sites with the resolved value. The chart change drops deployment.yaml:30's `--subagent-image` arg and updates `subagent.defaults.image` comments; test harnesses switch to `--set subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent`.

The billing halt is a new condition type + two detection sites + a hold in each reconciler. The credproxy detection (D-04 fast-path) needs a `ModifyResponse` hook on the reverse proxy that inspects HTTP 400 responses, classifies the Anthropic credit-exhaustion shape, and sets the subagent process's exit reason via an agreed-on `Reason` string in its termination message. The reconciler backstop classifies that `Reason` string from the failed Job envelope and sets `BillingHalt` on the Project status. All five dispatch reconcilers then check `BillingHalt` at the dispatch-entry gate, parking (not failing) the level. `tide resume` clears the condition.

**Primary recommendation:** Implement Image resolution as a new `resolveImage(project, level, helmDefaults)` function parallel to the existing `ResolveProvider`, update the five dispatch sites, drop the chart flag, update test harnesses. Implement BillingHalt as a Project-status condition with credproxy fast-path detection and reconciler backstop, using the existing `checkParentApproval`/`CheckRejected` hold pattern.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Image resolution logic | Controller (internal/controller/dispatch_helpers.go) | — | Pure Go wiring; no CRD changes needed |
| Chart image default (Helm) | Chart deploy surface (charts/tide/templates/) | — | Flag drop + comment update; values.yaml `subagent.defaults.image` becomes the surviving channel |
| Stub opt-in for tests | Test infrastructure (test/integration/kind/, hack/scripts/) | — | Must set `subagent.defaults.image=...stub` via `--set` so the resolved chain picks up the stub |
| Billing 400 detection (fast path) | credproxy (internal/credproxy/server.go) | — | Proxy intercepts before Claude Code even processes the error; ModifyResponse hook on the reverse proxy |
| Billing 400 detection (backstop) | Controller reconcilers (internal/controller/*_controller.go) | — | Classifies `Reason` field from failed Job envelopes; sets Project condition |
| BillingHalt condition | API types + ProjectStatus (api/v1alpha1/shared_types.go) | — | Follows existing condition vocabulary conventions |
| BillingHalt dispatch hold | Controller dispatch gates (internal/controller/dispatch_helpers.go) | — | Reuses `checkParentApproval` pattern; all five dispatch sites |
| BillingHalt recovery | CLI (cmd/tide/resume.go) | — | Extends existing `resumeRun` to clear BillingHalt condition from Project status |

---

## Standard Stack

No new external packages. This phase is pure wiring in existing code.

### Core (all already in go.mod)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| sigs.k8s.io/controller-runtime | v0.24.x | Reconciler wiring, status patches, condition helpers | Project-pinned; all reconcilers use it |
| k8s.io/apimachinery/pkg/api/meta | (controller-runtime transitive) | `meta.SetStatusCondition` for condition management | Existing pattern in every reconciler |
| net/http (stdlib) | Go 1.26 | credproxy `ModifyResponse` hook | No new dep; httputil.ReverseProxy already in use |

### No New Packages

The phase requires zero new package additions. The `providerfirewall` analyzer enforces that Anthropic-specific classification code stays in `internal/credproxy/` or `internal/subagent/anthropic/` — both are already out-of-scope for the firewall boundary.

## Package Legitimacy Audit

> No external packages are added in this phase. This section is not applicable.

---

## Architecture Patterns

### System Architecture Diagram

```
Project CR
    │
    ▼
ProjectReconciler ──► resolveImage(project, "project", helmDefaults)
                                │
                      [Levels.project.Image] → [Spec.Subagent.Image] → [helmDefaults.Image]
                                │
                      opts.SubagentImage = resolved
                                │
                      ┌─────────┴──────────────────────────────────────┐
                      │                                                │
             MilestoneReconciler                              TaskReconciler
             PhaseReconciler                                  (via r.Deps.*)
             PlanReconciler
                      │
                      ▼
              podjob.BuildJobSpec(opts) ──► Job.Spec.Template.Containers[0].Image = resolved

─────────────── BILLING HALT FLOW ─────────────────────────────────

Subagent Pod
    │
    ├── credproxy sidecar (127.0.0.1:8443)
    │       │
    │       ├── request ──► Anthropic API
    │       │                      │
    │       │               HTTP 400 {"type":"error","error":{"type":
    │       │               "invalid_request_error","message":"Your credit balance is too low..."}}
    │       │                      │
    │       └── ModifyResponse ◄──┘
    │               │
    │         classify: status=400 + "credit balance" substring in body
    │               │
    │         set X-Tide-Billing-Halt: true header on forwarded response
    │         (subagent sees 400 → exits non-zero)
    │               │
    │         credproxy writes billing-halt termination message:
    │         {"exitCode":1,"reason":"billing-halt:credit-balance-too-low",...}
    │
    ▼
Job terminal (Failed)
    │
    ▼
Reconciler reads termination message / EnvelopeOut
    │
    ├── classify: out.Reason has "billing-halt" prefix
    │
    ▼
ProjectReconciler patches Project.Status.Conditions:
    BillingHalt=True, Reason=CreditBalanceTooLow
    │
    ▼
All five reconcilers' dispatch gates:
    checkBillingHalt(project) → park (Status.Phase unchanged, requeue 30s)
    │
    ▼
Operator refills credits, runs `tide resume`
    │
    ▼
resumeRun clears BillingHalt condition
    │
    ▼
Dispatch resumes
```

### Recommended Project Structure

No new directories. Changes are in:

```
internal/
├── controller/
│   ├── dispatch_helpers.go     # + resolveImage() function
│   ├── dispatch_helpers_test.go
│   ├── milestone_controller.go # update SubagentImage resolution
│   ├── phase_controller.go     # update SubagentImage resolution
│   ├── plan_controller.go      # update SubagentImage resolution
│   ├── project_controller.go   # update SubagentImage resolution
│   ├── task_controller.go      # update SubagentImage resolution
│   └── *_test.go               # regression tests
├── credproxy/
│   ├── server.go               # + ModifyResponse billing classifier
│   └── server_test.go          # + billing 400 test
api/
└── v1alpha1/
    └── shared_types.go         # + ConditionBillingHalt, ReasonCreditBalanceTooLow
charts/
└── tide/
    └── templates/
        └── deployment.yaml     # remove :30 --subagent-image flag; update comments
cmd/
└── tide/
    └── resume.go               # + BillingHalt condition clear
test/integration/kind/
└── suite_test.go               # helmControllerArgs: --set subagent.defaults.image=stub
hack/
└── scripts/
    └── acceptance-v1.sh        # helm install: --set subagent.defaults.image=stub
```

### Pattern 1: Image Resolution Chain (parallel to ResolveProvider)

**What:** A `resolveImage` function in `dispatch_helpers.go` that walks `Levels.<level>.Image` → `Spec.Subagent.Image` → `helmDefaults.Image`, exactly mirroring the existing `ResolveProvider` Model chain.

**When to use:** At all five dispatch sites, replacing the current 2-line `r.SubagentImage / HelmProviderDefaults.Image` fallback.

**Current pattern (to replace at each site):**
```go
// milestone_controller.go:387-389 — current broken pattern
if opts.SubagentImage == "" {
    opts.SubagentImage = r.HelmProviderDefaults.Image
}
```

**New pattern:**
```go
// dispatch_helpers.go — new function (mirrors ResolveProvider structure)
// Source: ResolveProvider pattern at dispatch_helpers.go:138-183 [VERIFIED: codebase]
func resolveImage(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) string {
    var levelCfg *tideprojectv1alpha1.LevelConfig
    if project != nil {
        switch level {
        case "milestone":
            levelCfg = project.Spec.Subagent.Levels.Milestone
        case "phase":
            levelCfg = project.Spec.Subagent.Levels.Phase
        case "plan":
            levelCfg = project.Spec.Subagent.Levels.Plan
        case "task":
            levelCfg = project.Spec.Subagent.Levels.Task
        }
    }
    switch {
    case levelCfg != nil && levelCfg.Image != "":
        return levelCfg.Image
    case project != nil && project.Spec.Subagent.Image != "":
        return project.Spec.Subagent.Image
    default:
        return helmDefaults.Image  // from CLAUDE_SUBAGENT_IMAGE env
    }
}
```

**At each dispatch site (example — milestone):**
```go
// milestone_controller.go — replaces the 2-line fallback
opts := podjob.BuildOptions{
    ...
    SubagentImage: resolveImage(project, "milestone", r.HelmProviderDefaults),
    ...
}
```

Note: `r.SubagentImage` (the flag field) is REMOVED from the resolution path entirely. Once the chart drops the `--subagent-image` flag, `r.SubagentImage` will always be `""` in production. The field can stay on the reconciler struct for backward compat with tests that wire it directly, but resolution no longer prefers it unconditionally.

### Pattern 2: BillingHalt Condition (mirroring BudgetExceeded)

**What:** A condition on `Project.Status.Conditions` that blocks dispatch at all five reconciler dispatch gates.

**When to use:** Whenever a failed Job envelope carries `Reason` starting with `"billing-halt:"`.

```go
// api/v1alpha1/shared_types.go additions
// Source: existing condition vocabulary pattern [VERIFIED: codebase]
const (
    // ConditionBillingHalt — provider returned a credit-exhaustion 400;
    // new dispatch is halted project-wide until the operator refills credits
    // and runs `tide resume`. Phase 13 HALT-01.
    ConditionBillingHalt = "BillingHalt"

    // ReasonCreditBalanceTooLow — Anthropic API returned HTTP 400 with
    // "credit balance is too low" error. Set on Project by reconciler
    // billing classifier.
    ReasonCreditBalanceTooLow = "CreditBalanceTooLow"
)
```

**Dispatch gate check (mirrors task_controller.go:335-348 BudgetExceeded gate):**
```go
// dispatch_helpers.go — reusable helper (mirrors checkParentApproval shape)
// Source: checkParentApproval pattern at dispatch_helpers.go:254-296 [VERIFIED: codebase]
func checkBillingHalt(project *tideprojectv1alpha1.Project) bool {
    if project == nil {
        return false
    }
    for _, c := range project.Status.Conditions {
        if c.Type == tideprojectv1alpha1.ConditionBillingHalt &&
           c.Status == metav1.ConditionTrue {
            return true
        }
    }
    return false
}
```

**Setting the condition (reconciler backstop — called when envelope Reason has "billing-halt" prefix):**
```go
// In reconciler that processes a failed Job:
if strings.HasPrefix(out.Reason, "billing-halt:") {
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionBillingHalt,
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ReasonCreditBalanceTooLow,
        Message:            "Provider returned credit-exhaustion 400; dispatch halted. Run `tide resume` after refilling credits.",
        LastTransitionTime: metav1.Now(),
    })
    _ = r.Client.Status().Patch(ctx, project, patch)
}
```

### Pattern 3: credproxy ModifyResponse Billing Classifier

**What:** A `ModifyResponse` hook on the `httputil.ReverseProxy` in `internal/credproxy/server.go` that classifies Anthropic credit-exhaustion 400s and reflects the classification in the exit path.

**Key architecture detail:** The credproxy is a pass-through proxy — today it only has a `Director` (request rewrite). To intercept upstream responses, add a `ModifyResponse` function. The subagent (Claude Code CLI) will receive the 400 and exit non-zero regardless; the credproxy just needs to make the billing classification machine-readable in the exit envelope.

**Challenge:** The credproxy does not write the `EnvelopeOut` — the subagent harness does (in `internal/subagent/anthropic/subagent.go`). The credproxy runs as a sidecar, not in-process with the harness. Communication channel options (within envelopes-as-artifacts rules):

1. **Recommended (planner's discretion):** Write a tiny `billing-halt` sentinel file to a well-known tmpfs path that the harness checks before writing its EnvelopeOut. The sentinel indicates the reason; the harness sets `out.Reason = "billing-halt:credit-balance-too-low"`. This is the simplest path and stays within the same-pod boundary (no cross-namespace write).
2. Alternative: credproxy sets a response header `X-Tide-Billing-Halt: true` on the forwarded response; Claude Code CLI ignores it (it only sees the 400 body) but the harness could intercept if it wraps the subprocess. More complex.
3. Alternative: credproxy exits non-zero after writing a termination message, causing the entire Job to fail immediately. This satisfies the "fail the session fast" goal but loses any partial usage accounting.

**The simplest testable option:** Since the reconciler backstop (D-04 second layer) is the cheaply-testable one, and the credproxy fast-path is a best-effort optimization, the planner may scope the credproxy detection to simply logging the billing 400 at the proxy layer (non-fatal — Claude Code still gets the 400 and exits) while relying on the reconciler backstop for the actual halt condition. The reconciler backstop classifies `Reason` that comes naturally from the harness's stderr capture.

**Simplified credproxy addition (ModifyResponse to classify and log):**
```go
// internal/credproxy/server.go — Source: net/http/httputil.ReverseProxy docs [ASSUMED]
rp.ModifyResponse = func(resp *http.Response) error {
    if resp.StatusCode != http.StatusBadRequest {
        return nil
    }
    body, err := io.ReadAll(resp.Body)
    resp.Body.Close()
    if err != nil {
        resp.Body = io.NopCloser(bytes.NewReader(body))
        return nil
    }
    resp.Body = io.NopCloser(bytes.NewReader(body))
    if isCreditExhaustion(body) {
        // Log for operator visibility in pod logs.
        p.Logger.Info("billing-halt: Anthropic credit-exhaustion 400 detected",
            "status", resp.StatusCode)
        // The response passes through unchanged; Claude Code exits non-zero.
        // The reconciler backstop classifies the failure from the termination message.
    }
    return nil
}

// isCreditExhaustion returns true for HTTP 400 + Anthropic "credit balance" body.
// Classify conservatively: status=400 + "credit balance" substring.
// Do NOT match on exact message text — guard against minor wording changes.
func isCreditExhaustion(body []byte) bool {
    lower := strings.ToLower(string(body))
    return strings.Contains(lower, "credit balance")
}
```

**Reconciler backstop (reads stderr via existing harness):** The subagent harness at `internal/subagent/anthropic/subagent.go:307` already captures stderr into `out.Reason = fmt.Sprintf("claude exit %d: %s", ...)`. Claude Code CLI, on receiving a 400 with "credit balance is too low", outputs that error to stderr and exits non-zero. The reconciler backstop classifies `out.Reason` with a `strings.Contains(lower, "credit balance")` check — no sentinel file needed for the backstop path.

**Important:** The provider firewall analyzer (`tools/analyzers/providerfirewall`) allows `internal/credproxy/` to import Anthropic-specific classification logic since credproxy is explicitly out-of-scope (`cmd/credproxy` exclusion at analyzer.go:116). However, the classification function (`isCreditExhaustion`) should be in `internal/credproxy/` not in `internal/controller/` — the controller just inspects the structured `out.Reason` string.

### Anti-Patterns to Avoid

- **Don't match the exact Anthropic error message string.** `"Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."` is the current message, but Anthropic can change the phrasing. Classify on `status == 400 && strings.Contains(lower(body), "credit balance")`. [VERIFIED: Anthropic docs/issue reports]
- **Don't set `Status.Phase = "BillingHalt"` as a new top-level phase enum.** Use the Conditions list only. Existing top-level phases are `PhasePending / PhaseInitialized / PhaseBudgetExceeded / PhaseRunning / PhaseComplete / PhasePushLeaseFailed / PhasePushLeakBlocked / PhaseInitFailed` (project_types.go:369-392). Adding a 9th enum requires CRD regeneration and risks a chart-contract breakage.
- **Don't Fail-mark parked levels during BillingHalt.** D-05 specifies PARK (consistent with Phase 12's park-not-fail). Levels stay at their current Status.Phase; the reconciler simply requeues with a 30s backoff.
- **Don't delete in-flight Jobs.** D-05 explicitly: "In-flight sessions fail fast on their own (no Job killing)." The halt blocks NEW dispatch only.
- **Don't add auto-probe logic.** D-06 explicitly: "NO auto-probe: never spend API calls testing an empty balance." The recovery is 100% operator-triggered via `tide resume`.
- **Don't put the billing classification in `internal/controller/`.** The `providerfirewall` analyzer blocks LLM SDK imports there. However, classification of the `out.Reason` string (which is just string matching, no SDK import) IS legal in the controller — only Anthropic SDK imports are forbidden, not the string "credit balance".
- **Don't leave `r.SubagentImage` wired in the chart.** Once deployment.yaml:30 drops the flag, the `--subagent-image` flag will parse as `""` from flag.Parse(). The reconciler struct fields can stay, but the flag must be removed from the args list in the chart template.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP response body inspection in credproxy | Custom stream reader/buffer | `httputil.ReverseProxy.ModifyResponse` | Standard hook; body already buffered by httputil for response modification |
| Condition management on Project status | Manual slice append | `meta.SetStatusCondition` (k8s.io/apimachinery/pkg/api/meta) | Handles dedup, LastTransitionTime; used by every reconciler already |
| Dispatch gate check | Inline condition loop at each site | Shared helper `checkBillingHalt()` | Mirrors `checkParentApproval`; single testable function |
| Resume clearing a condition | Custom status patch | `meta.RemoveStatusCondition` + standard status subresource patch | Same pattern `tide resume` already uses for ResumedByUser condition |

---

## Common Pitfalls

### Pitfall 1: `r.SubagentImage` Priority Masking the New Chain

**What goes wrong:** The new `resolveImage` function is implemented but each dispatch site still checks `r.SubagentImage != ""` first, meaning the chain is unreachable when the flag is set (which it always is in any non-production deploy that still uses the chart).

**Why it happens:** The flag wiring in `main.go:336` (`SubagentImage: subagentImage`) and reconciler struct fields remain intact. Developers assume the flag should still win.

**How to avoid:** After dropping the flag from deployment.yaml:30, `subagentImage` in main.go will always be `""` in production. The dispatch sites should call `resolveImage(project, level, r.HelmProviderDefaults)` directly, without checking `r.SubagentImage` first. The struct field can stay for legacy tests that wire it directly (unit tests of the reconciler before project is available), but the chart no longer sets it.

**Warning signs:** DISPATCH-01 regression test passes but DISPATCH-02 (chart-level) test fails because the flag shadow is still present in the chart.

### Pitfall 2: BillingHalt Check Position in the Dispatch Gate Order

**What goes wrong:** The BillingHalt check is placed AFTER the pool acquire or AFTER the Job create, allowing in-flight work to proceed before the halt is enforced on subsequent reconcile cycles.

**Why it happens:** Copy-paste from the BudgetExceeded gate (task_controller.go:335) which is placed after `resolveProject` but before `checkReadinessGates`.

**How to avoid:** Place `checkBillingHalt` immediately after the `CheckRejected` check and the `checkParentApproval` hold — before pool acquisition, before Job creation. [VERIFIED: codebase — task_controller.go dispatch-gate order at :305-352]

### Pitfall 3: BillingHalt from a Single Failed Task Halting Unrelated Projects

**What goes wrong:** The reconciler classifies a billing 400 from one Task's envelope and patches the wrong Project, or patches ALL Projects in the namespace.

**Why it happens:** The reconciler resolves the Project via the task's label fast-path (tideproject.k8s/project) or owner-ref chain. If the label is missing or the chain walk fails, the project pointer is nil and the classification is skipped — this is the correct safe default.

**How to avoid:** Only set BillingHalt on the Project resolved from the specific failed Job's task CR. Never scan project list. The nil-guard `if project == nil { return }` at the classification site prevents cross-project contamination.

### Pitfall 4: Chart Upgrade Leaving Old `--subagent-image` Flag in Running Pods

**What goes wrong:** After dropping the flag from deployment.yaml, a `helm upgrade` is applied, but the old manager Pod (with the old flag) is still running. The new resolveImage chain is in the new binary but the old Pod still passes `--subagent-image=ghcr.io/.../tide-stub-subagent:v1.0.0`.

**Why it happens:** Helm upgrade creates a new Deployment spec (triggering a rolling update), but there's a window where both old and new Pods coexist. This is normal rolling behavior; the test assertion checks the NEW Pod's image.

**How to avoid:** The regression test for DISPATCH-02 should apply the chart and wait for the new Deployment to be fully Ready before asserting image behavior. The kind integration harness already uses `--wait` on helm upgrade.

### Pitfall 5: credproxy `ModifyResponse` Consuming Response Body Without Restoring It

**What goes wrong:** The `ModifyResponse` reads `resp.Body` to classify the billing error but forgets to replace `resp.Body` with a new reader containing the original bytes. Claude Code receives an empty body, misclassifies the error, and does not emit the billing-related stderr that the reconciler backstop depends on.

**Why it happens:** `io.ReadAll(resp.Body)` drains the stream. `resp.Body` must be replaced with `io.NopCloser(bytes.NewReader(body))` after reading.

**How to avoid:** Always restore `resp.Body` in ModifyResponse: read → classify → reset. The code example above shows the pattern. [VERIFIED: codebase — confirmed credproxy has no ModifyResponse hook today; httputil pattern is standard]

### Pitfall 6: `make test-int` Exit Discipline

**What goes wrong:** The kind Layer B test (`test/integration/kind/`) includes both Ginkgo specs AND plain go tests (e.g. `TestHelmDeploymentTemplateRendersManagerPodAnnotations`). A go test that checks `deployment.yaml` will fail if the flag removal is done correctly but the plain go test still asserts the old flag's presence.

**Why it happens:** CLAUDE.md explicitly calls this out: "One RED go-test fails the package and trips `make test-int` non-zero even when both layers print `Ran X of Y … SUCCESS!`. Always read the echoed `MAKE_EXIT`."

**How to avoid:** After dropping the flag from deployment.yaml, update (or add) a plain go test in `test/integration/kind/projects_pvc_test.go` that asserts the NEW chart contract: `--subagent-image` does NOT appear in deployment.yaml args, and `CLAUDE_SUBAGENT_IMAGE` env IS present.

---

## Code Examples

### Image Resolution — resolveImage function

```go
// internal/controller/dispatch_helpers.go
// Source: mirrors ResolveProvider at dispatch_helpers.go:138-183 [VERIFIED: codebase]

// resolveImage walks Project.Spec.Subagent precedence chain for the given
// level, returning the resolved subagent container image reference.
//
//   Levels.<level>.Image → Spec.Subagent.Image → helmDefaults.Image → ""
//
// An empty return means no image was configured; callers must surface this
// as a config error rather than dispatching a Job with an empty image field.
func resolveImage(project *tideprojectv1alpha1.Project, level string, helmDefaults ProviderDefaults) string {
    var levelCfg *tideprojectv1alpha1.LevelConfig
    if project != nil {
        switch level {
        case "milestone":
            levelCfg = project.Spec.Subagent.Levels.Milestone
        case "phase":
            levelCfg = project.Spec.Subagent.Levels.Phase
        case "plan":
            levelCfg = project.Spec.Subagent.Levels.Plan
        case "task":
            levelCfg = project.Spec.Subagent.Levels.Task
        }
    }
    switch {
    case levelCfg != nil && levelCfg.Image != "":
        return levelCfg.Image
    case project != nil && project.Spec.Subagent.Image != "":
        return project.Spec.Subagent.Image
    default:
        return helmDefaults.Image
    }
}
```

### Chart Flag Removal (deployment.yaml)

```yaml
# charts/tide/templates/deployment.yaml — BEFORE (line 30 to remove):
#   - --subagent-image={{ .Values.images.stubSubagent.repository }}:{{ .Values.images.stubSubagent.tag | default .Chart.AppVersion }}

# AFTER: the line above is deleted entirely.
# The surviving image-default channel is the CLAUDE_SUBAGENT_IMAGE env var (line 45):
#   - name: CLAUDE_SUBAGENT_IMAGE
#     value: "{{ .Values.images.claudeSubagent.repository }}:{{ .Values.images.claudeSubagent.tag | default .Chart.AppVersion }}"
# Which feeds into helmProviderDefaults.Image via envOrDefault("CLAUDE_SUBAGENT_IMAGE", ...) in main.go.
# Source: deployment.yaml:30 and :45 [VERIFIED: codebase]
```

### Kind Harness Stub Opt-In (suite_test.go)

```go
// test/integration/kind/suite_test.go — helmControllerArgs update
// Source: helmControllerArgs at suite_test.go:469-506 [VERIFIED: codebase]
// Add to return value (replaces the old images.stubSubagent.tag behavior):
"--set", fmt.Sprintf("subagent.defaults.image=ghcr.io/jsquirrelz/tide-stub-subagent:%s", stubTag),
// Remove or keep the old images.stubSubagent.tag=test (it no longer drives dispatch image, only chart values)
```

### BillingHalt Condition Set (reconciler backstop)

```go
// Called from any of the five reconcilers after reading EnvelopeOut
// Source: conditionReasonFromEnvelopeResult pattern at task_controller.go:1192 [VERIFIED: codebase]
func setBillingHaltIfNeeded(ctx context.Context, c client.StatusClient, project *tideprojectv1alpha1.Project, reason string) {
    if project == nil {
        return
    }
    lower := strings.ToLower(reason)
    if !strings.Contains(lower, "credit balance") && !strings.HasPrefix(lower, "billing-halt:") {
        return
    }
    patch := client.MergeFrom(project.DeepCopy())
    meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
        Type:               tideprojectv1alpha1.ConditionBillingHalt,
        Status:             metav1.ConditionTrue,
        Reason:             tideprojectv1alpha1.ReasonCreditBalanceTooLow,
        Message:            "Provider billing 400: credit balance too low. Run `tide resume` after refilling credits.",
        LastTransitionTime: metav1.Now(),
    })
    _ = c.Status().Patch(ctx, project, patch)
}
```

### Resume: Clear BillingHalt (cmd/tide/resume.go)

```go
// cmd/tide/resume.go — extend resumeRun
// Source: resumeRun at cmd/tide/resume.go [VERIFIED: codebase]
// After the existing gates.ConsumeReject + Patch, also clear BillingHalt:
patch2 := client.MergeFrom(proj.DeepCopy())
meta.RemoveStatusCondition(&proj.Status.Conditions, tidev1alpha1.ConditionBillingHalt)
if err := c.Status().Patch(ctx, &proj, patch2); err != nil {
    return fmt.Errorf("patch status (clear BillingHalt): %w", err)
}
```

---

## Key Discoveries — Dispatch Site Inventory

All five dispatch sites found and confirmed:

| Site | File | Current SubagentImage wiring | Notes |
|------|------|------------------------------|-------|
| Project (project-level planner) | `project_controller.go:982-984` | `r.SubagentImage` then `HelmProviderDefaults.Image` | 5th site per Phase 7 D-06; confirmed at :178 SubagentImage field |
| Milestone | `milestone_controller.go:380-389` | `r.SubagentImage` then `HelmProviderDefaults.Image` | Shape confirmed at :380-390 |
| Phase | `phase_controller.go:341-344` | `r.SubagentImage` then `HelmProviderDefaults.Image` | Shape confirmed at :341-344 |
| Plan | `plan_controller.go:373-376` | `r.SubagentImage` then `HelmProviderDefaults.Image` | Shape confirmed at :373-376 |
| Task | `task_controller.go:628` | `r.Deps.SubagentImage` (no fallback in task) | Task has no HelmProviderDefaults fallback — uses the value passed from main.go directly; the resolveImage call should happen in main.go when constructing `TaskReconcilerDeps.SubagentImage`, OR the TaskReconciler needs to access HelmProviderDefaults |

**Critical finding — Task site is different:** The Task reconciler uses `r.Deps.SubagentImage` directly at jobspec construction (task_controller.go:628) with no fallback to `HelmProviderDefaults.Image` at the task site. The `HelmProviderDefaults` IS on `TaskReconcilerDeps` (task_controller.go:94), and the image is pre-resolved from `SubagentImage || HelmProviderDefaults.Image` when `Deps` is constructed at main.go startup. But there's no project-level resolution there. **Correct fix:** The task reconciler DOES have `r.Deps.HelmProviderDefaults` available — use `resolveImage(project, "task", r.Deps.HelmProviderDefaults)` at the dispatch site, replacing `r.Deps.SubagentImage`.

**Confirmed: No code path today consults `project.Spec.Subagent.Image` or `LevelConfig.Image`** (verified by grep across all controller files).

---

## Billing 400 Error Body — Verified Shape

From Anthropic API behavior (verified via WebSearch against github.com/anthropics/claude-code issues):

```json
{
  "type": "error",
  "error": {
    "type": "invalid_request_error",
    "message": "Your credit balance is too low to access the Anthropic API. Please go to Plans & Billing to upgrade or purchase credits."
  },
  "request_id": "req_011CZ47RMqKBKxEPtTzaX2wE"
}
```

**HTTP status:** 400 Bad Request. [VERIFIED: WebSearch — Anthropic claude-code GitHub issues #867, #4207, #5300, LiteLLM issue #24320]

**Classification rule (robust):** `statusCode == 400 && strings.Contains(strings.ToLower(body), "credit balance")` — the phrase "credit balance" is present in all observed variants. Do NOT match `invalid_request_error` alone (used for many non-billing errors).

---

## Test Architecture

### Existing Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (envtest layer) / plain `go test` (kind layer) |
| Config file | `internal/controller/suite_test.go` (envtest setup) |
| Quick run command | `make test-int-fast` (Layer A envtest, ~90s) |
| Full suite command | `make test-int` (Layer A envtest + Layer B kind) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File |
|--------|----------|-----------|-------------------|------|
| DISPATCH-01 | `project.Spec.Subagent.Levels.Plan.Image` flows into plan-level Job spec | unit (envtest) | `make test-int-fast` | `internal/controller/dispatch_helpers_test.go` (new) |
| DISPATCH-01 | `project.Spec.Subagent.Image` flows into all four planner Jobs | unit (envtest) | `make test-int-fast` | `internal/controller/dispatch_helpers_test.go` (new) |
| DISPATCH-02 | Chart deployment.yaml does NOT contain `--subagent-image=` | plain go test | `make test-int` | `test/integration/kind/projects_pvc_test.go` (new assertion) |
| DISPATCH-02 | Chart deployment.yaml DOES contain `CLAUDE_SUBAGENT_IMAGE` env | plain go test | `make test-int` | `test/integration/kind/projects_pvc_test.go` (new assertion) |
| HALT-01 | BillingHalt condition set on Project when envelope reason contains "credit balance" | unit (envtest) | `make test-int-fast` | `internal/controller/project_controller_test.go` (new It block) |
| HALT-01 | All five reconcilers skip Job creation when Project has BillingHalt condition | unit (envtest) | `make test-int-fast` | `internal/controller/*_test.go` (one test per reconciler) |
| HALT-01 | `tide resume` clears BillingHalt from Project status | unit (cmd test) | `go test ./cmd/tide/...` | `cmd/tide/resume_test.go` (new case) |
| HALT-01 | credproxy logs billing 400 (ModifyResponse) | unit | `go test ./internal/credproxy/...` | `internal/credproxy/server_test.go` (new test) |

### Wave 0 Gaps

- [ ] `internal/controller/dispatch_helpers_test.go` — add `TestResolveImage_PrecedenceChain` covering the three-level chain for all four levels
- [ ] `test/integration/kind/projects_pvc_test.go` — add `TestHelmDeploymentTemplateDropsSubagentImageFlag` and `TestHelmDeploymentTemplateHasCLAUDE_SUBAGENT_IMAGE`
- [ ] `internal/controller/project_controller_test.go` — add billing halt condition test (mirrors BudgetExceeded pattern at :450)
- [ ] `internal/controller/task_gates_test.go` — add `BillingHalt_HoldsTaskDispatch` test

No new test framework needed — all tests use the existing envtest + Ginkgo/Gomega infrastructure.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega; plain `go test` for non-Ginkgo packages |
| Config file | `internal/controller/suite_test.go` |
| Quick run command | `make test-int-fast` |
| Full suite command | `make test-int` |

### Sampling Rate
- **Per task commit:** `make test-int-fast` (Layer A envtest; ~90s)
- **Per wave merge:** `make test-int` (Layer A + Layer B kind)
- **Phase gate:** Full suite green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `internal/controller/dispatch_helpers_test.go` — `TestResolveImage_*` unit tests
- [ ] `test/integration/kind/projects_pvc_test.go` — chart contract assertions for flag removal
- [ ] `internal/controller/project_controller_test.go` — BillingHalt condition set/clear tests
- [ ] `cmd/tide/resume_test.go` — BillingHalt clear test case

---

## Security Domain

The provider firewall constraint is the primary security consideration.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes | Billing classifier uses conservative string matching, not exact match; classification is non-destructive (log + pass through) |
| V6 Cryptography | no | No new crypto |
| V2 Authentication | no | Existing credproxy HMAC auth unchanged |
| V4 Access Control | yes | BillingHalt condition is Project-scoped; halt check reads Project in the owning namespace only |

### Known Threat Patterns

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Forged billing-halt reason in EnvelopeOut | Tampering | EnvelopeOut comes from a completed Pod (controller-spawned, HMAC-gated); a forged reason can only DoS the project (halt dispatch unnecessarily) — operator recovers via `tide resume`; no privilege escalation possible |
| Anthropic message wording change causing missed billing halt | Denial | Use conservative substring match ("credit balance") not exact match; if Anthropic drops that phrase, the worst outcome is the old behavior (no halt) not a crash |
| Over-broad classification halting on unrelated 400s | Denial | The "credit balance" substring is specific enough; 400 + this substring has no known false positives in Anthropic's public error vocabulary |

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `--subagent-image` flag always passed | Dropped; `CLAUDE_SUBAGENT_IMAGE` env survives as Helm-default channel | Phase 13 | Production installs dispatch real subagent by default |
| No billing halt | BillingHalt condition + dispatch gate + `tide resume` | Phase 13 | Prevents fan-out credit burn |
| Project.Spec.Subagent.Image documented but dead | Wired into resolveImage precedence chain | Phase 13 | Per-project and per-level image overrides work |

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `httputil.ReverseProxy.ModifyResponse` is available and allows body inspection without breaking the response stream | Code Examples (credproxy) | Low — this is a documented net/http/httputil feature; verified via Go stdlib docs [ASSUMED — not verified via Context7 in this session] |
| A2 | Claude Code CLI prints "credit balance" in stderr when it receives the Anthropic 400 | Architecture Patterns (billing halt) | Medium — if Claude Code outputs the 400 body verbatim, the string will be present; if it translates to a generic error, the backstop won't fire. Mitigation: both the credproxy path AND the direct stderr path are checked |
| A3 | Dropping `--subagent-image` from deployment.yaml causes `subagentImage` in main.go to be `""` via `flag.Parse()` | Architecture | Low — confirmed: `flag.StringVar(&subagentImage, "subagent-image", "", ...)` has empty default; if the arg is absent from the Deployment, it parses as `""` |

---

## Open Questions

1. **Does `tide resume` need a dedicated `--clear-billing-halt` flag for discoverability, or is `tide resume` alone sufficient?**
   - What we know: D-06 says "`tide resume` clears BillingHalt". The current `resumeRun` function only clears the reject annotation.
   - What's unclear: Should `tide resume` always clear BillingHalt (side-effect), or only clear it when `--clear-billing-halt` is passed?
   - Recommendation: Always clear BillingHalt unconditionally in `resumeRun` (like it clears the reject annotation) — the operator already chose recovery by invoking resume. No new flag needed.

2. **Which reconcilers should write the BillingHalt condition to the Project?**
   - What we know: Task, Plan, Phase, Milestone, and Project reconcilers all process Job envelopes. Any of them could classify billing-halt.
   - What's unclear: If a Task reconciler classifies a billing halt and patches the Project, does that reconciler have write access to the Project status in a different namespace?
   - Recommendation: The Project CR is always in the same namespace as the task (or accessible via resolved project name). All five reconcilers already read the Project — they can write it too (the controller's RBAC grants `projects/status` update cluster-wide).

---

## Environment Availability

No new external dependencies. All tools and runtimes used by this phase are already present for Phase 12 tests.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go 1.26 | Build | ✓ | (project-standard) | — |
| kind cluster `tide` | Layer B tests | ✓ (existing cluster) | — | — |
| helm | Layer B tests | ✓ | (project-standard) | — |
| envtest | Layer A tests | ✓ | (setup-envtest already installed) | — |

---

## Sources

### Primary (HIGH confidence)
- `internal/controller/dispatch_helpers.go` — ResolveProvider pattern at :125-183 [VERIFIED: codebase read]
- `internal/controller/milestone_controller.go` — SubagentImage wiring at :380-389 [VERIFIED: codebase read]
- `internal/controller/phase_controller.go` — SubagentImage wiring at :341-344 [VERIFIED: codebase read]
- `internal/controller/plan_controller.go` — SubagentImage wiring at :373-376 [VERIFIED: codebase read]
- `internal/controller/project_controller.go` — SubagentImage wiring at :982-984 [VERIFIED: codebase read]
- `internal/controller/task_controller.go` — Deps.SubagentImage at :628 [VERIFIED: codebase read]
- `charts/tide/templates/deployment.yaml` — line 30 (flag to drop), line 45 (CLAUDE_SUBAGENT_IMAGE env) [VERIFIED: codebase read]
- `charts/tide/values.yaml` and `hack/helm/tide-values.yaml` — subagent.defaults.image channel [VERIFIED: codebase read]
- `api/v1alpha1/project_types.go` — SubagentConfig, LevelConfig, Image field at :151/:199 [VERIFIED: codebase read]
- `api/v1alpha1/shared_types.go` — condition vocabulary [VERIFIED: codebase read]
- `internal/controller/dispatch_helpers.go` — checkParentApproval pattern at :254-296 [VERIFIED: codebase read]
- `internal/controller/task_controller.go` — BudgetExceeded gate pattern at :335-348 [VERIFIED: codebase read]
- `internal/credproxy/server.go` — proxy structure (no ModifyResponse today) [VERIFIED: codebase read]
- `cmd/tide/resume.go` — resumeRun function [VERIFIED: codebase read]
- `test/integration/kind/suite_test.go` — helmControllerArgs at :469-506 [VERIFIED: codebase read]
- `hack/scripts/acceptance-v1.sh` — helm install pattern at :104 [VERIFIED: codebase read]
- `tools/analyzers/providerfirewall/analyzer.go` — firewall boundary rules [VERIFIED: codebase read]
- `.planning/phases/13-dispatch-image-resolution-provider-halt/13-CONTEXT.md` — D-01..D-06 [VERIFIED: context file]
- `.planning/REQUIREMENTS.md` — DISPATCH-01, DISPATCH-02, HALT-01 [VERIFIED: requirements file]

### Secondary (MEDIUM confidence)
- WebSearch: Anthropic API billing 400 error body shape — `{"type":"error","error":{"type":"invalid_request_error","message":"Your credit balance is too low..."}}` [CITED: github.com/anthropics/claude-code/issues/867, BerriAI/litellm/issues/24320]

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — no new packages; all wiring in well-understood code
- Architecture (image chain): HIGH — exact analog to existing ResolveProvider; struct fields already exist
- Architecture (billing halt): HIGH — exact analog to existing BudgetExceeded gate + checkParentApproval pattern
- credproxy ModifyResponse pattern: MEDIUM — `ModifyResponse` is a standard httputil.ReverseProxy field but not used today; marked A1 in assumptions log
- Anthropic 400 error body: MEDIUM — verified via multiple GitHub issue reports; exact message text may change (mitigated by conservative substring match)
- Pitfalls: HIGH — observed from actual codebase patterns (task site difference, chart-upgrade lag)

**Research date:** 2026-06-11
**Valid until:** 2026-07-11 (stable codebase; no fast-moving external dependencies)
