---
phase: 14
slug: budget-enforcement-pricing
status: verified
threats_open: 0
asvs_level: 1
created: 2026-06-12
---

# Phase 14 — Security

> Per-phase security contract: threat register, accepted risks, and audit trail.

---

## Trust Boundaries

| Boundary | Description | Data Crossing |
|----------|-------------|---------------|
| Helm values → manager flag → Job env | Operator-supplied pricing JSON crosses into billing math | Pricing overrides (public list rates) |
| Job container env → claude-subagent | Env readable/settable by anyone with Job-edit RBAC in the namespace | Pricing JSON |
| Job labels → ReservationStore | Rederivation trusts labels stamped by the manager on Jobs it owns | Estimated-cost cents |
| Project.Status.Conditions | Condition writable by anyone with status RBAC; dispatch gates trust it | BudgetBlocked / BillingHalt conditions |
| Task/Job events → dispatch gate | Gate decisions ride on Project status conditions + in-process store | Budget headroom state |
| Bypass annotation | Operator-controlled escape hatch over the cap | Dispatch authorization |
| External pricing page → CI runner | Untrusted fetched markdown flows into issue bodies | Scraped pricing text |
| Workflow → GitHub API | issues:write token scope | Drift issue bodies |
| values.yaml → manager flags | Operator config crosses into billing enforcement | Cap / estimate cents |
| K8s apiserver → dashboard informer cache | Controller-stamped condition data enters the dashboard process (trusted, read-only client) | Condition messages |
| Dashboard REST API → browser / React render | Condition `message` strings cross to any dashboard viewer and enter the DOM (tooltip) | Budget arithmetic strings |
| SSE stream → refetch trigger | Untrusted event payloads parsed client-side (existing surface, unchanged) | Event payloads |

---

## Threat Register

| Threat ID | Category | Component | Disposition | Mitigation | Status |
|-----------|----------|-----------|-------------|------------|--------|
| T-14-01 | Tampering | Pricing overrides (zero/negative prices) | mitigate | `pkg/dispatch/pricing.go:59-77` rejects `InputCentsPerMTok <= 0`, `OutputCentsPerMTok <= 0`, negative cache cents; `cmd/manager/main.go:216-219` fails fast (`os.Exit(1)`) at startup; `cmd/claude-subagent/main.go:49-51` loud stderr warning + fallback to compiled table | closed |
| T-14-02 | Tampering | Package-level priceTable concurrent mutation | mitigate | `internal/subagent/anthropic/subagent.go:138` — `maps.Clone(priceTable)` per instance in `New()`; package-level table never written | closed |
| T-14-03 | Information Disclosure | Pricing JSON in Job env | accept | Prices are public list rates, not secrets; no PII | closed |
| T-14-04 | Tampering | Estimated-cost Job label edited to shrink rederived reservations | accept | Same trust domain as existing provider-secret-uid PreCharge label; Job-edit RBAC already implies dispatch control | closed |
| T-14-05a | Denial of Service | Forged BudgetBlocked=True condition halts dispatch | accept | Status-subresource RBAC on Project already gates this; identical exposure to existing BillingHalt; recovery via cap raise/bypass is operator-visible | closed |
| T-14-05b | Denial of Service | Rederivation over-reservation after upgrade | mitigate | `internal/budget/reservation.go:151-161` — `RederiveReservations` filters via `client.HasLabels{reservedCostLabel}`; pre-Phase-14 Jobs lacking the label treated as 0 reserved (under-reserve, never over-reserve); `TestRederiveReservations_SkipsMissingLabel` at `internal/budget/reservation_test.go:248` | closed |
| T-14-06 | Tampering | Check-then-act race between TotalReserved read and Reserve | mitigate (bounded) | By-design doc comment at `internal/budget/reservation.go:47-52` (overshoot bounded to one session's estimate per concurrent reconcile); regression suite `internal/controller/budget_blocked_regression_test.go:209-262` asserts total committed never exceeds cap + one estimate | closed |
| T-14-07 | Elevation of Privilege | Bypass annotation dispatches past cap | accept | Existing D-D4 machinery, deliberately preserved — no second unlock path invented | closed |
| T-14-08 | Denial of Service | Stale BudgetBlocked=True permanently parks dispatch after cap raise | mitigate | Bidirectional clear at `internal/controller/budget_blocked.go:107-117` (`ReasonBudgetCapCleared`); 30s requeue on every budget park (`internal/controller/task_controller.go:389,404,492`); cap-raise recovery test at `budget_blocked_regression_test.go:264-345` | closed |
| T-14-09 | Tampering | Script injection via fetched pricing page into github-script | mitigate | `.github/workflows/pricing-drift.yaml:80-85` — drift body passed via `env: DRIFT_BODY`, consumed only as `process.env.DRIFT_BODY`; zero `${{ }}` interpolation inside script source; randomized heredoc delimiter (line 63) prevents GITHUB_OUTPUT key injection | closed |
| T-14-10 | Elevation of Privilege | Over-scoped workflow token | mitigate | `.github/workflows/pricing-drift.yaml:29` top-level `permissions: {}`; job grants only `contents: read` + `issues: write`; `persist-credentials: false` on checkout | closed |
| T-14-11 | Tampering | Compromised page induces wrong "drift" issue → human merges bad prices | accept | D-03 mandates no auto-PR; a human reviews every billing-math change against the issue's quoted diff | closed |
| T-14-12 | Denial of Service | Huge ReserveEstimateCents blocks all dispatch | accept | Operator self-misconfiguration of own cap math; symptom visible (headroom hold log + no Jobs), recoverable by values change | closed |
| T-14-06-01 | Information Disclosure | `summarize()` blockingConditions exposure | accept | Messages are controller-stamped budget arithmetic (cents, caps) — no secrets, no PII; whitelist limits exposure to exactly 2 condition types | closed |
| T-14-06-02 | Tampering (stored XSS) | `writeJSON` → browser | mitigate | `cmd/dashboard/api/projects.go:392-397` — `json.NewEncoder` with default `SetEscapeHTML=true`; escape behavior asserted in `cmd/dashboard/api/projects_test.go:326-358` | closed |
| T-14-06-03 | Denial of Service (payload bloat) | projectSummary blockingConditions size | mitigate | `cmd/dashboard/api/projects.go:353-361` — explicit whitelist (BudgetBlocked, BillingHalt) + `Status != ConditionTrue` skip bounds blockingConditions to ≤2 entries per project | closed |
| T-14-06-04 | Elevation of Privilege | Dashboard route surface | mitigate | `cmd/dashboard/router_test.go:62-86` — `TestZeroMutationRoutes` walks the chi tree via `chi.Walk`, fails build on any non-GET/HEAD registration | closed |
| T-14-07-01 | Tampering (XSS) | ConditionBadge `title` tooltip | mitigate | `dashboard/web/src/components/ConditionBadge.tsx:107` — `title={condition.message}` JSX attribute binding only (React-escaped); zero `dangerouslySetInnerHTML` in `dashboard/web/src/`, enforced by `src/__tests__/no-dangerous-html.test.ts` | closed |
| T-14-07-02 | Spoofing (vocabulary drift) | ConditionBadge type lookup | mitigate | `dashboard/web/src/components/ConditionBadge.tsx:80-83` — `CONDITION_TABLE[condition.type]` miss → `return null` | closed |
| T-14-07-03 | Denial of Service (render storm) | PlanningDAGView SSE refetch on condition flap | accept | Existing 250ms debounce collapses event bursts; zero new SSE subscriptions or fetch paths added | closed |
| T-14-SC | Tampering (supply chain) | Package installs (all 7 plans) | accept | Zero new dependencies phase-wide: stdlib `maps` only (Go), pinned first-party actions (checkout@v4, github-script@v7), `Wallet`/`CreditCard` from already-pinned lucide-react; package.json unmodified | closed |

*Status: open · closed*
*Disposition: mitigate (implementation required) · accept (documented risk) · transfer (third-party)*

*Note: the plan-time register reused ID T-14-05 for two distinct threats (14-02-PLAN forged-condition vs 14-05-PLAN rederivation); split here as T-14-05a/T-14-05b.*

---

## Accepted Risks Log

| Risk ID | Threat Ref | Rationale | Accepted By | Date |
|---------|------------|-----------|-------------|------|
| AR-14-01 | T-14-03 | Pricing JSON in Job env contains public list rates only — not secrets, no PII | plan 14-01 threat model | 2026-06-12 |
| AR-14-02 | T-14-04 | Label tamper sits in the same trust domain as the existing provider-secret-uid PreCharge label; Job-edit RBAC already implies dispatch control | plan 14-02 threat model | 2026-06-12 |
| AR-14-03 | T-14-05a | Forged BudgetBlocked condition gated by status-subresource RBAC; identical exposure to existing BillingHalt; operator-visible recovery | plan 14-02 threat model | 2026-06-12 |
| AR-14-04 | T-14-07 | Bypass annotation is deliberate existing D-D4 machinery — no second unlock path | plan 14-03 threat model | 2026-06-12 |
| AR-14-05 | T-14-11 | No auto-PR by design (D-03); human reviews every billing-math change against the issue's quoted diff | plan 14-04 threat model | 2026-06-12 |
| AR-14-06 | T-14-12 | Operator self-misconfiguration of own cap math; visible symptom, recoverable via values change | plan 14-05 threat model | 2026-06-12 |
| AR-14-07 | T-14-06-01 | blockingConditions messages are controller-stamped budget arithmetic — no secrets, no PII; whitelist bounds exposure | plan 14-06 threat model | 2026-06-12 |
| AR-14-08 | T-14-07-03 | Condition-flap render storms collapsed by existing 250ms SSE debounce; no new subscriptions | plan 14-07 threat model | 2026-06-12 |
| AR-14-09 | T-14-SC | Zero new dependencies across all 7 plans | all plan threat models | 2026-06-12 |

*Accepted risks do not resurface in future audit runs.*

---

## Security Audit Trail

| Audit Date | Threats Total | Closed | Open | Run By |
|------------|---------------|--------|------|--------|
| 2026-06-12 | 21 | 21 | 0 | gsd-security-auditor |

Audit notes (2026-06-12):
- All 12 mitigate-disposition threats verified in implementation with file:line evidence; 9 accepted risks pre-closed per plan-time register.
- 14-03-SUMMARY.md lacks a `## Threat Flags` section (only summary missing it); its plan's threats (T-14-06, T-14-08) verified closed in code, and its file list introduced no new endpoints/auth paths — informational only.
- `TestZeroMutationRoutes` permits HEAD alongside GET; HEAD is safe/non-mutating, so the zero-mutation-routes intent holds — noted for precision.
- T-14-09 verified at both layers: github-script env-mapping and the GITHUB_OUTPUT randomized-heredoc hardening against delimiter injection from fetched content.

---

## Sign-Off

- [x] All threats have a disposition (mitigate / accept / transfer)
- [x] Accepted risks documented in Accepted Risks Log
- [x] `threats_open: 0` confirmed
- [x] `status: verified` set in frontmatter

**Approval:** verified 2026-06-12
