---
phase: 30
slug: resumable-import-partial-tree-resume-adopt-complete-re-plan
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-26
---

# Phase 30 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.
> Resumable import — partial-tree resume (adopt-complete / re-plan-incomplete).

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| salvage bundle → export CLI | Operator-supplied salvaged `out.json` bytes read/parsed by export tooling to decide seed Status | Untrusted envelope bytes (exitCode, childCount, childCRDs) |
| seed manifest → ImportController | Operator-supplied seed manifest drives CR materialization (status-injection surface) | Per-node Status string |
| Project.Status.Conditions → reconciler | Reconciler trusts `ImportComplete` as the adoption signal; condition is controller-owned | Status-only Condition |
| owned-Milestone list → guard | Milestone ownership predicate drives the project-planner skip decision | Owner reference (UID-bound) |
| fixture bundle → import CLI/controller | Repo-controlled test fixture travels the same untrusted-input path a real salvage bundle would | Test envelope/seed bytes |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-30-01-01 | Tampering | export `seedStatusFor` / `processEnvelopesTgz` unmarshalling salvaged out.json | mitigate | Fail-closed: missing envelope / unmarshal error / `ExitCode!=0` / childCount-vs-len mismatch → `Status=""` (re-plannable), never a forged terminal status. Verified on PRE-stamp raw bytes (gap-fix `72c0079`): `processEnvelopesTgz:309-318`, `seedStatusFor:371-387`. | closed |
| T-30-01-02 | Elevation/Tampering | status injection via crafted seed Status | accept | ImportController only stamps a status the seed already carries, inside `if seed.Status != ""` (`import_controller.go:424/471/518`); phase tightens (incomplete → `""`), `ValidationState` stamp at `:533` unchanged. See Accepted Risks. | closed |
| T-30-01-03 | Denial of Service | re-plannable node re-dispatching paid LLM work | mitigate | Empty-status nodes parked by import HOLD (`project_controller.go:1080-1086`) until `ImportComplete=True`; project-planner re-dispatch closed via T-30-02-02. | closed |
| T-30-02-01 | Elevation of Privilege | guard tightening opening a re-dispatch hole | mitigate | New guard arm only ADDS a skip (`return ctrl.Result{},nil` at `project_controller.go:1125`); sits after HOLD, before `PlannerPool.Acquire` (`:1135`). `if listErr == nil` (`:1110`) → List error falls through to dispatch (no fail-open). UID-bound `metav1.IsControlledBy` (`:1119`, gap-fix `8ba705b`). | closed |
| T-30-02-02 | Denial of Service | re-plannable node abused to re-dispatch paid project-level LLM work | mitigate | Same guard suppresses post-`ImportComplete` paid project-planner re-dispatch (the run #2 defect); reduces paid dispatch. | closed |
| T-30-02-03 | Spoofing | crafted `ImportComplete` condition forcing a dispatch skip | accept | `ImportComplete` is a controller-owned status Condition (`shared_types.go:268`, set by `succeedImport` `import_controller.go:702`); only spec field is `ImportSource` (`project_types.go:412`). Forging requires `/status` write (cluster-admin). See Accepted Risks. | closed |
| T-30-03-01 | Tampering | malformed fixture envelope/seed driving status injection | mitigate | Fail-closed export/import path (Plans 01/02); `containedJoin` path-traversal defense intact & unchanged (`cmd/tide-import/main.go:300-316`: absolute reject, `..` reject, prefix-containment double-check). | closed |
| T-30-03-02 | Denial of Service | re-plan cascade re-dispatching paid LLM work | accept (test-scoped) | Tier c kind E2E runs under stub subagents ($0); asserts complete Plan NOT re-planned (bounds re-dispatch to the incomplete node). See Accepted Risks. | closed |
| T-30-0x-SC | Tampering | supply chain (go module deps) | mitigate | `git diff e0f7a8f..HEAD -- go.mod go.sum` is EMPTY — zero new dependencies introduced by phase 30. | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-30-01 | T-30-01-02 | A crafted seed can only stamp a status the operator already wrote into the seed manifest; authoring a seed is itself an operator-trusted export step. Phase 30 tightens (never loosens) the `if seed.Status != ""` path — incomplete nodes get `""`, not a forged `"Succeeded"`. No privilege gained over what the operator already holds. | jsquirrelz | 2026-06-26 |
| AR-30-02 | T-30-02-03 | `ImportComplete` is controller-owned (`succeedImport`); forging `=True` requires Project `/status` write, i.e. cluster-admin RBAC that already exceeds the dispatch-suppression authority this guard governs. An attacker with `/status` write can manipulate reconcile outcomes directly. | jsquirrelz | 2026-06-26 |
| AR-30-03 | T-30-03-02 | Test-scoped: Tier c runs under stub subagents at $0 and asserts the complete Plan is not re-planned. The production cost-containment property (no post-`ImportComplete` project-planner re-dispatch) is mitigated, not accepted (T-30-02-02). Residual is non-billable test-harness compute. | jsquirrelz | 2026-06-26 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-26 | 9 | 9 | 0 | gsd-security-auditor (verify-mitigations mode; register authored at plan time) |

No implementation gaps found; no escalation. Implementation files were not modified. All three SUMMARY threat-surface scans report no new network endpoints, auth paths, file-access patterns, or schema changes at trust boundaries — every flag maps to a registered threat; no unregistered attack surface appeared.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-26
