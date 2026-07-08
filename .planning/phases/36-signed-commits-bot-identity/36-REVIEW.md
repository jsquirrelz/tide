---
phase: 36-signed-commits-bot-identity
reviewed: 2026-07-08T00:00:00Z
depth: standard
files_reviewed: 32
files_reviewed_list:
  - api/v1alpha1/phase3_schema_test.go
  - api/v1alpha1/project_types.go
  - api/v1alpha2/project_types.go
  - api/v1alpha2/schema_test.go
  - charts/tide/templates/deployment.yaml
  - cmd/manager/env.go
  - cmd/manager/env_test.go
  - cmd/manager/main.go
  - cmd/tide-push/main.go
  - cmd/tide-push/main_test.go
  - docs/project-authoring.md
  - hack/helm/augment-tide-chart.sh
  - internal/controller/boundary_push.go
  - internal/controller/dispatch_helpers.go
  - internal/controller/dispatch_helpers_test.go
  - internal/controller/milestone_controller.go
  - internal/controller/phase_controller.go
  - internal/controller/plan_controller.go
  - internal/controller/project_controller.go
  - internal/controller/push_helpers.go
  - internal/controller/push_helpers_test.go
  - internal/dispatch/podjob/backend.go
  - internal/dispatch/podjob/backend_test.go
  - internal/dispatch/podjob/jobspec.go
  - internal/dispatch/podjob/jobspec_test.go
  - internal/harness/commit.go
  - internal/harness/commit_test.go
  - pkg/git/identity.go
  - pkg/git/identity_test.go
  - pkg/git/integrate.go
  - pkg/git/integrate_test.go
  - test/integration/kind/agent_identity_chart_test.go
findings:
  critical: 0
  warning: 1
  info: 2
  total: 3
status: issues_found
---

# Phase 36: Code Review Report

**Reviewed:** 2026-07-08
**Depth:** standard
**Files Reviewed:** 32
**Status:** issues_found

## Summary

SIGN-01 delivers a uniform, configurable TIDE agent commit identity across the harness, integrate, and tide-push commit sites via a clean three-tier precedence chain. The implementation is correct, thoroughly tested (precedence, per-field independence, nil-safety, env-injection on both executor/planner Kinds, go-git committer==author, chart-template drift guard), and consistently wired at every dispatch site. No correctness, security, or data-loss BLOCKERs were found. The findings below are a defense-in-depth validation gap and two maintainability notes.

### Verification traced (no blockers)

- **Precedence logic** (`resolveAgentIdentity`, `AgentIdentity`, backend inline mirror): all three implement `spec.git.agent* → chart value → compiled default` with correct last-write-wins ordering (compiled→chart→spec). Empty-is-unset semantics consistent across `envOrDefault`, `AgentIdentity`, and all three resolvers. Nil-safety on `project` and `project.Spec.Git` (`*GitConfig`) handled and tested.
- **All three commit sites reach the identity**: harness executor pod (`task_controller.go` → `jobspec.go` → `harness.CommitWorktree`), integrate merge + boundary commit (`boundary_push.go` → `push_helpers.go` → `integrate.go` / `cmd/tide-push`). Planner dispatch sites (milestone/phase/plan/project) all resolve too.
- **author==committer preserved on go-git path**: `pkg/git/commit.go` sets only `Author`; go-git copies Author→Committer when Committer is nil. `main_test.go:881` explicitly asserts this.
- **No shell/arg injection on CLI paths**: `-c user.name=`/`-c user.email=` pass as discrete argv elements before the subcommand; ordering correct.
- **CRD validation present in BOTH versions**: identical `Pattern`/`MaxLength` markers on `agentName` (`^[^<>\r\n]+$`, max 100) and `agentEmail` (`^[^<>@\s]+@[^<>@\s]+$`, max 254). Anchoring rejects trailing newline. v1alpha1 is `unservedversion`; v1alpha2 is storage; no conversion webhook, both schemas carry the fields identically → no round-trip data-loss.
- **No manager-process caller of `AgentIdentity()`** — only the three pod-side commit sites — so the manager's own `TIDE_AGENT_NAME` (chart tier) never collides with the per-Project resolved value injected into pods.

## Warnings

### WR-01: Chart-tier and compiled-default identity values bypass the Pattern/MaxLength validation guarding the CRD tier

**File:** `internal/controller/dispatch_helpers.go:320-341`, `charts/tide/templates/deployment.yaml:104-107`, `pkg/git/identity.go:37-42`
**Issue:** Only the untrusted CRD tier (`project.Spec.Git.AgentName/AgentEmail`) carries the `+kubebuilder:validation:Pattern` (`^[^<>\r\n]+$` / `^[^<>@\s]+@[^<>@\s]+$`) and `MaxLength` markers. The chart tier (`.Values.agent.name/email` → `TIDE_AGENT_NAME/EMAIL` env, rendered as `value: "{{ .Values.agent.name }}"`) and the compiled-in defaults are **not** validated, yet `resolveAgentIdentity` routes all three tiers into the identical git commit-header sink (`git -c user.name=<value>` in `commit.go`/`integrate.go`, and the go-git `object.Signature` in `cmd/tide-push`). An operator who sets `agent.name: "Foo<bar"` or a value containing a newline / a `"` in `values.yaml` would inject unvalidated content into commit author lines, producing malformed commit objects (or breaking the Deployment YAML render for a `"`). Because the *untrusted* input tier (Project spec, served/validated by v1alpha2) is validated, this is not remotely exploitable — impact requires operator misconfiguration — hence WARNING rather than BLOCKER. Consistent with the phase threat model (T-36-02 accepts operator-configured identity as low-severity public metadata at chart-install trust tier).
**Fix:** Validate at resolve time so every tier is guarded uniformly, e.g. in `resolveAgentIdentity` (and the `AgentIdentity()` pod-side reader) reject/sanitize values containing `<`, `>`, `\r`, `\n` before they reach the commit sink:
```go
func sanitizeIdentityField(v, fallback string) string {
    if v == "" || strings.ContainsAny(v, "<>\r\n") {
        return fallback // fall back rather than emit a corrupting header
    }
    return v
}
```
Alternatively, document the constraint on the `agent.name`/`agent.email` chart values and quote them defensively in the template.

## Info

### IN-01: Identity precedence chain is triplicated across three sites — drift risk

**File:** `internal/controller/dispatch_helpers.go:320-341`, `internal/dispatch/podjob/backend.go:290-305`, `pkg/git/identity.go:59-69`
**Issue:** The same `spec → chart → compiled default` precedence is implemented three times: `resolveAgentIdentity` (controller), the inline mirror in `PodJobBackend.Run` (fixture path, duplicated to avoid a controller import cycle), and env-based `AgentIdentity()` (pod side). The `backend.go` mirror is acknowledged as fixture-only, but three copies of the same ordering can silently diverge. Maintainability smell, not a current defect — all three are presently correct.
**Fix:** Extract a single pure precedence helper into `pkg/git` (e.g. `ResolveIdentity(specName, specEmail, chartName, chartEmail string)`) that both `resolveAgentIdentity` and the `backend.go` mirror delegate to; keep `AgentIdentity()` as the env-reading wrapper over it.

### IN-02: Merge-conflict in `IntegrateTaskBranches` leaves the worktree in a `MERGE_HEAD` state that would fail retries

**File:** `pkg/git/integrate.go:87-100`
**Issue:** On a conflicting `git merge --no-ff`, the function returns an error (correct — surfaced, tested by `TestIntegrateTaskBranchesConflictFails`), but the integration worktree on the PVC is left mid-merge. A Job retry (backoffLimit=2) would re-enter `IntegrateTaskBranches` and fail immediately with "You have not concluded your merge (MERGE_HEAD exists)" rather than a clean conflict error, obscuring the real cause. Predates SIGN-01 (the phase only added identity resolution to this function), so out of the phase's identity-only scope — flagged INFO for awareness.
**Fix (if addressed later):** Best-effort `git merge --abort` before returning the conflict error so a retry starts from a clean run-branch tip.

---

_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
