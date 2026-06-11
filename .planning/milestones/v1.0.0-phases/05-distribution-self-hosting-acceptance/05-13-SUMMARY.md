---
phase: 05-distribution-self-hosting-acceptance
plan: 13
subsystem: helm
tags: [helm-template, per-namespace, rolebinding, auth-02-catchup, dist-01, sot-pattern]
requires:
  - charts/tide/templates/manager-rbac.yaml (Phase 1 ClusterRole that this RoleBinding binds to)
  - charts/tide/templates/serviceaccount.yaml (Phase 1 ServiceAccount that this RoleBinding subjects to)
  - hack/helm/augment-tide-chart.sh (chart-augmentation pipeline)
provides:
  - charts/tide/templates/per-namespace-rolebinding.yaml (AUTH-02 catch-up — opt-in per-namespace RBAC for multi-Project installs)
  - `projectNamespaces: []` Helm value (D-X4) — list operator namespaces here to opt into per-namespace RoleBindings
  - `make test-per-ns-rb` render gate (DIST-01 / AUTH-02)
  - `hack/scripts/test-per-ns-rb.sh` (callable from CI or local verification)
affects:
  - Multi-Project install posture: cluster operators can now narrow controller RBAC by listing only the Project namespaces they care about (the default empty list ships zero per-namespace bindings — opt-in by design)
  - Documents/rbac.md (Plan 05-09, already on `main`) references this template; the contract now exists
  - SOT discipline restored on charts/tide/values.yaml + charts/tide/templates/projects-pvc.yaml (Phase 02.2 drift backfilled to SOT)
tech_stack_added: []
patterns_introduced:
  - Per-namespace RoleBinding (Helm template) — Helm `range` over `.Values.projectNamespaces` with `tide.fullname` + `tide.labels` helpers; subjects in operator namespace, RoleBinding in Project namespace (standard central-SA K8s pattern, OQ6 RESOLVED)
key_files_created:
  - hack/helm/per-namespace-rolebinding.yaml (37 lines — SOT for the chart template, mirrored by augment script)
  - charts/tide/templates/per-namespace-rolebinding.yaml (20 lines — derived; produced by `bash hack/helm/augment-tide-chart.sh`)
  - hack/scripts/test-per-ns-rb.sh (90 lines — 4-assertion render gate, executable)
key_files_modified:
  - hack/helm/tide-values.yaml (+8 lines — `projectNamespaces: []` Phase 5 D-X4 block; +2 lines Rule 2 SOT-drift backfill for `podAnnotations: {}` and `accessModes:` line — see Deviations)
  - hack/helm/augment-tide-chart.sh (+7 lines — new step 7a copies the per-namespace-rolebinding SOT into charts/tide/templates/)
  - hack/helm/projects-pvc.yaml (+3 lines / −1 line — Rule 2 SOT-drift backfill for the `accessModes` range template; see Deviations)
  - charts/tide/values.yaml (+8 lines — derived; the new `projectNamespaces: []` block propagated by the augment script)
  - Makefile (+7 lines — new `##@ Per-namespace RBAC render gate` section + `.PHONY: test-per-ns-rb` + target body)
decisions:
  - "roleRef binds to `{fullname}-manager-role` (Phase 1's consolidated controller ClusterRole) per PATTERNS §P3.1 option (c) — single-RoleBinding-per-namespace, manager-role grants the per-Kind verbs (no wildcards per AUTH-03 lock). Alternatives considered: (b) aggregate `tide-orchestrator-namespace` ClusterRole (RESEARCH §canonical-template-line-594), and (a) one RoleBinding per per-Kind admin ClusterRole. Option (c) is minimal-surface-area for v1.0; (a) or (b) are v1.x granularity improvements."
  - "Subjects.namespace = `{{ $.Release.Namespace }}` (resolves to `tide-system` at install time) per Open Question 6 RESOLVED — central-SA pattern, one orchestrator SA, namespace-scoped grants."
  - "Author the new chart template via the SOT pattern (hack/helm/per-namespace-rolebinding.yaml + augment-script `cp`) rather than directly into charts/tide/templates/ — matches the existing signing-secret.yaml / push-rbac.yaml / projects-pvc.yaml convention and honors the CLAUDE.md chart-is-fixed-contract anti-pattern."
  - "Restored SOT discipline on two pre-existing drifts that surfaced when re-running the augment script (Rule 2 deviations — see below)."
metrics:
  duration: "≈25 minutes"
  completed_date: "2026-05-23"
  tasks_completed: 2
  files_created: 3
  files_modified: 5
  commits: 2
---

# Phase 5 Plan 13: per-namespace-rolebinding.yaml (AUTH-02 catch-up) Summary

Two-task plan: SOT-routed Helm template that emits one `RoleBinding` per entry in
`.Values.projectNamespaces` (empty default, opt-in for multi-Project installs),
binding the controller-manager SA in the operator namespace to the consolidated
Phase 1 manager-role ClusterRole. Plus a `make test-per-ns-rb` render gate that
asserts (1) empty default emits zero per-namespace RoleBindings, (2) `--set` emits
N RoleBindings, (3) subjects live in `tide-system` (central-SA pattern), and
(4) roleRef binds to `*-manager-role`. AUTH-02 — deferred from Phase 1 to Phase 5
per REQUIREMENTS.md traceability — is now satisfied as a v1.0 deliverable.

## What Shipped

### Task 1 — `projectNamespaces` value key (commit `d29917e`)

- **`hack/helm/tide-values.yaml`** (SOT): appended an 8-line block at end-of-file
  containing the `projectNamespaces: []` key and the canonical comment block from
  RESEARCH §"Code Examples → Per-Namespace RoleBinding Template" lines 603-610:
  Phase 5 D-X4 reference, AUTH-02 catch-up note, opt-in `projectNamespaces:`
  example with two `tide-customer-*` namespaces.
- **`charts/tide/values.yaml`** (derived): the same block, propagated verbatim by
  `bash hack/helm/augment-tide-chart.sh` (which already mirrors `hack/helm/tide-values.yaml`
  via `cp` at line 35 of the augment script).
- **`hack/helm/projects-pvc.yaml`** + Rule 2 backfill on **`hack/helm/tide-values.yaml`**:
  see Deviations §1 — pre-existing SOT drift surfaced when re-running the augment
  script; restored to keep the augment idempotent and prevent production behavior
  regression.

### Task 2 — chart template + render gate (commit `c6861ee`)

- **`hack/helm/per-namespace-rolebinding.yaml`** (new SOT, 37 lines): Helm template
  body following the PATTERNS §P3.1 canonical shape. `range $ns := .Values.projectNamespaces`
  → for each entry, emits a `RoleBinding` named `tide-orchestrator-<ns>` in namespace
  `<ns>`, labeled via `tide.labels` + `app.kubernetes.io/component: per-namespace-rbac`,
  with `roleRef.name = '{{ include "tide.fullname" $ }}-manager-role'` and
  `subjects[0].name = '{{ include "tide.fullname" $ }}-controller-manager'` /
  `subjects[0].namespace = {{ $.Release.Namespace }}` (central-SA per OQ6 RESOLVED).
  Includes a multi-line header comment explaining the augment-pipeline pedigree,
  the central-SA rationale, and the load-bearing assumption that `tide.fullname` /
  `tide.labels` helpers match the existing manager-rbac.yaml + serviceaccount.yaml
  naming.
- **`hack/helm/augment-tide-chart.sh`** (extended): new step 7a inserted after the
  projects-pvc.yaml copy (step 7). Single `cp` line mirroring the existing
  hand-authored-template copy pattern (signing-secret.yaml line 108, push-rbac.yaml
  line 119, projects-pvc.yaml line 124).
- **`charts/tide/templates/per-namespace-rolebinding.yaml`** (new derived, 20
  effective lines after stripping the SOT-specific header comment): produced by
  re-running the augment script.
- **`hack/scripts/test-per-ns-rb.sh`** (new gate, 90 lines, executable): standard
  `set -euo pipefail` + `REPO_ROOT` preamble per PATTERNS §"Shell-script preamble".
  Four assertions:
  1. Empty default — `helm template charts/tide -n tide-system | grep -c 'name:
     tide-orchestrator-'` must return 0.
  2. Non-empty — `helm template charts/tide --set 'projectNamespaces={tide-acme,tide-globex}'
     -n tide-system | grep -c 'name: tide-orchestrator-'` must return 2.
  3. `subjects[0].namespace == "tide-system"` (extracted via awk window after the
     single-namespace render `tide-orchestrator-tide-acme` block).
  4. `roleRef.name` ends with `-manager-role` (awk window same shape; confirms
     the Phase 1 ClusterRole is bound, no aggregation gap).
- **`Makefile`** (`+7 lines`): new section `##@ Per-namespace RBAC render gate
  (Phase 5 DIST-01 + AUTH-02 — Plan 05-13)` inserted immediately after the
  `##@ Docs coverage gate` section. Body: `.PHONY: test-per-ns-rb` + target body
  `@bash hack/scripts/test-per-ns-rb.sh`.

## Acceptance Criteria — Verified

| Source | Criterion | Command | Observed |
|--------|-----------|---------|----------|
| Task 1 AC1 | SOT key present | `grep -c "^projectNamespaces: \[\]" hack/helm/tide-values.yaml` | `1` ✓ |
| Task 1 AC2 | Phase 5 D-X4 comment | `grep -q "Phase 5 D-X4" hack/helm/tide-values.yaml` | exit `0` ✓ |
| Task 1 AC3 | AUTH-02 comment | `grep -q "AUTH-02" hack/helm/tide-values.yaml` | exit `0` ✓ |
| Task 1 AC4 | Augment script runs | `bash hack/helm/augment-tide-chart.sh` | exit `0` ✓ |
| Task 1 AC5 | Propagated to derived | `grep -c "^projectNamespaces:" charts/tide/values.yaml` | `1` ✓ |
| Task 1 AC6 | Idempotent | re-run + md5 compare | identical ✓ |
| Task 1 AC7 | helm lint clean | `helm lint charts/tide` | `0 chart(s) failed` ✓ |
| Task 2 AC1 | Template non-empty | `test -s charts/tide/templates/per-namespace-rolebinding.yaml` | exit `0` ✓ |
| Task 2 AC2 | `range $ns` present | `grep -F 'range $ns' charts/tide/templates/per-namespace-rolebinding.yaml` | match ✓ |
| Task 2 AC3 | `kind: RoleBinding` present | `grep -q 'kind: RoleBinding' ...` | exit `0` ✓ |
| Task 2 AC4 | `tide-orchestrator-` prefix present | `grep -q 'tide-orchestrator-' ...` | exit `0` ✓ |
| Task 2 AC5 | Uses `tide.fullname` helper | `grep -q 'tide.fullname' ...` | exit `0` ✓ |
| Task 2 AC6 | Uses `tide.labels` helper | `grep -q 'tide.labels' ...` | exit `0` ✓ |
| Task 2 AC7 | helm lint clean (post-template) | `helm lint charts/tide` | `0 chart(s) failed` ✓ |
| Task 2 AC8 | Empty default → 0 bindings | `helm template charts/tide -n tide-system \| grep -c 'tide-orchestrator-'` | `0` ✓ |
| Task 2 AC9 | Non-empty → 2 bindings | `helm template charts/tide --set 'projectNamespaces={ns1,ns2}' -n tide-system \| grep -c 'name: tide-orchestrator-'` | `2` ✓ |
| Task 2 AC10 | Test script executable | `test -x hack/scripts/test-per-ns-rb.sh` | exit `0` ✓ |
| Task 2 AC11 | Test script passes | `bash hack/scripts/test-per-ns-rb.sh` | exit `0`, "PASS:..." ✓ |
| Task 2 AC12 | Makefile target present | `grep -cE '^test-per-ns-rb:' Makefile` | `1` ✓ |
| Task 2 AC13 | `make test-per-ns-rb` passes | `make test-per-ns-rb` | exit `0` ✓ |
| Plan `<verification>` | Full augment idempotency | re-run 2× + md5 compare on derived + new template | both identical ✓ |
| Success criterion 6 | MEDIUM-10 file-modified accuracy | files_modified frontmatter does NOT list charts/tide/values.yaml | confirmed ✓ |

## Helm Template Output (Evidence)

Per-namespace render with `--set 'projectNamespaces={tide-acme,tide-globex}' -n tide-system`:

```yaml
metadata:
  name: tide-orchestrator-tide-acme
  namespace: tide-acme
  labels:
    helm.sh/chart: tide-1.0.0
    app.kubernetes.io/name: tide
    app.kubernetes.io/instance: release-name
    app.kubernetes.io/version: "1.0.0"
    app.kubernetes.io/managed-by: Helm
    app.kubernetes.io/component: per-namespace-rbac
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: 'release-name-tide-manager-role'
subjects:
- kind: ServiceAccount
  name: 'release-name-tide-controller-manager'
  namespace: tide-system
---
metadata:
  name: tide-orchestrator-tide-globex
  namespace: tide-globex
  labels: ...
roleRef:
  name: 'release-name-tide-manager-role'
subjects:
- kind: ServiceAccount
  name: 'release-name-tide-controller-manager'
  namespace: tide-system
```

Two RoleBindings, one per listed namespace. Each binds the central
`release-name-tide-controller-manager` SA (in `tide-system`) to the consolidated
`release-name-tide-manager-role` ClusterRole, scoped to the respective Project
namespace. Standard K8s central-SA pattern verified end-to-end.

## Deviations from Plan

### 1. [Rule 2 — Auto-add missing critical functionality] Backfill latent SOT drift in chart-augmentation pipeline

- **Found during:** Task 1, on the first `bash hack/helm/augment-tide-chart.sh`
  invocation after appending the `projectNamespaces: []` block to SOT.
- **Issue:** Re-running the augment script produced unexpected diffs on
  `charts/tide/values.yaml` (`-    podAnnotations: {}` and `-    accessModes:
  [ReadWriteMany]  # production-default; matches the chart's single-shared-RWX-PVC
  architecture …`) AND on `charts/tide/templates/projects-pvc.yaml` (the
  `accessModes` block reverted from a `range` over `.Values.workspaces.pvc.accessModes`
  to a hardcoded `- ReadWriteMany`). Investigation via `git log -S` traced both
  drifts to Phase 02.2 commit `f70586f` ("feat(02.2-03): expose
  workspaces.pvc.accessModes — unblock kind RWX/RWO mismatch"), which had
  hand-edited the derived chart files WITHOUT updating the corresponding
  `hack/helm/` SOT files. The drift was latent because no plan between Phase 02.2
  and Plan 05-13 had re-run the augment script.
- **Why this is Rule 2:** The `accessModes: [ReadWriteMany]` value is load-bearing
  per its original commit message ("unblock kind RWX/RWO mismatch") — re-augmenting
  without restoring SOT first would have silently regressed production behavior on
  kind clusters with single-node RWX/RWO storage drivers. The
  chart-is-fixed-contract invariant (CLAUDE.md §Anti-patterns) mandates SOT-routed
  authoring; this drift was a latent violation that the plan's augment-script
  invocation surfaced.
- **Fix:** Brought SOT up to date with derived for both files:
  - `hack/helm/tide-values.yaml`: inserted `    podAnnotations: {}` between
    `pullPolicy: IfNotPresent` and `resources:` (the same position as in the
    pre-edit derived file); appended `    accessModes: [ReadWriteMany]  # ...`
    after the `storageClassName:` line in the `workspaces.pvc` block.
  - `hack/helm/projects-pvc.yaml`: replaced the hardcoded
    `    - ReadWriteMany` line with the proper `{{- range
    (.Values.workspaces.pvc.accessModes | default (list "ReadWriteMany")) }}` /
    `    - {{ . | quote }}` / `{{- end }}` block from the derived file.
- **Files modified by the fix:** `hack/helm/tide-values.yaml` (+2 lines),
  `hack/helm/projects-pvc.yaml` (3 inserted, 1 deleted).
- **Verification:** post-fix, `bash hack/helm/augment-tide-chart.sh` produces a
  diff vs `main` containing ONLY the intentional `projectNamespaces: []` block
  in `charts/tide/values.yaml` (no spurious drift cleanup). Second invocation =
  no-op (md5 unchanged). `helm lint charts/tide` passes.
- **Commit:** `d29917e` (rolled into Task 1's commit since the issue surfaced
  inside Task 1's execution scope; documented in the commit body).

### 2. [Rule 3 — Auto-fix blocking issue] Task 2 created `hack/helm/per-namespace-rolebinding.yaml` (SOT) and extended `hack/helm/augment-tide-chart.sh` despite both being absent from Task 2's `<files>` list

- **Found during:** Task 2 setup, after rechecking the Phase 5 invariants in the
  additional-context block ("Chart edits route through `hack/helm/` files via
  augmentation script — NEVER direct `charts/tide/values.yaml` or
  `charts/tide/templates/per-namespace-rolebinding.yaml` hand-edits.").
- **Issue:** Plan 05-13's Task 2 `<files>` list contains only
  `charts/tide/templates/per-namespace-rolebinding.yaml`,
  `hack/scripts/test-per-ns-rb.sh`, `Makefile`. The plan's Task 2 `<action>` Step
  1 instructs `Create charts/tide/templates/per-namespace-rolebinding.yaml` — a
  direct edit of the derived chart file. But the CLAUDE.md chart-is-fixed-contract
  anti-pattern + the plan's own frontmatter `files_modified` (which DOES include
  `hack/helm/augment-tide-chart.sh`) + the existing pattern for hand-authored
  templates (signing-secret.yaml, push-rbac.yaml, projects-pvc.yaml all have
  `hack/helm/` SOT files copied by the augment script) all require the SOT-routed
  approach. A direct write to `charts/tide/templates/per-namespace-rolebinding.yaml`
  would be silently destroyed by the next `make helm-controller` invocation if/when
  helmify is re-run with the augment script in its current shape.
- **Why this is Rule 3:** Following the strict `<files>` list as-written would
  produce a chart template that doesn't survive the augment pipeline — a blocking
  issue for the plan's stated invariant ("The chart can be re-augmented … and
  produces identical output (idempotent).").
- **Fix:**
  - Created `hack/helm/per-namespace-rolebinding.yaml` (37 lines) as the
    canonical SOT for the new chart template, matching the existing convention
    for hand-authored chart templates copied by the augment script.
  - Extended `hack/helm/augment-tide-chart.sh` with a new step 7a (single `cp`
    line + 5-line comment header) that mirrors the SOT into `charts/tide/templates/per-namespace-rolebinding.yaml`.
  - Ran `bash hack/helm/augment-tide-chart.sh` to materialize the chart template.
- **Files modified by the fix:** `hack/helm/per-namespace-rolebinding.yaml` (new,
  37 lines), `hack/helm/augment-tide-chart.sh` (+7 lines).
- **Verification:** the resulting `charts/tide/templates/per-namespace-rolebinding.yaml`
  is byte-identical to a fresh `cp` from SOT. Augment script idempotency confirmed
  on both new and existing artifacts (md5 stable across consecutive invocations).
- **Commit:** `c6861ee` (Task 2's commit; documented in the commit body under
  "Rule 3").

## Threat Model Coverage

Plan 05-13's `<threat_model>` lists four threats; each is now mitigated as planned:

| Threat ID | Disposition | Realization |
|-----------|-------------|-------------|
| T-05-13-01 (EoP, RoleBinding scope) | mitigate | `roleRef.name` binds to the Phase 1 `*-manager-role` ClusterRole, which has per-Kind verbs only (no wildcards per AUTH-03 lock — verified by reading manager-rbac.yaml). Render gate Assertion 4 confirms the binding by grepping `-manager-role`. Render gate Assertion 3 confirms `subjects[0].namespace == "tide-system"` so the grant lands on the central SA, not on a per-namespace SA that might be granted other privileges. |
| T-05-13-02 (Tampering, default values) | mitigate | Default in SOT is `projectNamespaces: []`; render gate Assertion 1 confirms zero per-namespace RoleBindings ship under the default. Operators opt in by populating the list. Documented in the SOT comment block + (already on `main`) docs/rbac.md. |
| T-05-13-03 (Tampering, augment idempotency) | mitigate | Augment script + the new template are idempotent — verified by md5 compare across two consecutive `bash hack/helm/augment-tide-chart.sh` invocations on both `charts/tide/values.yaml` and `charts/tide/templates/per-namespace-rolebinding.yaml`. Task 1's Rule 2 backfill restored idempotency on the pre-existing drifted artifacts. |
| T-05-13-04 (EoP, roleRef.name aggregation choice) | accept | The `manager-role` binding is documented in the SUMMARY decisions block. Granular per-Kind RoleBinding aggregation is a v1.x improvement; v1.0 ships the minimal-surface-area binding per PATTERNS §P3.1 option (c). |

## Forward References / Wire-up Notes

- **`docs/rbac.md`** (Plan 05-09, already on `main`) references this template by
  name. The contract now exists; no docs update required for v1 — the doc reader
  who follows the install path to a real chart install will find the template
  available.
- **`make test-per-ns-rb`** is a standalone Make target — not yet wired into
  CI (e.g., `.github/workflows/ci.yaml`'s `helm-lint` job). A future closeout plan
  for Phase 5 may add it; Plan 05-13 does not modify CI per scope discipline.
- **Re-augmenting after `make helm-controller`** is a single `bash
  hack/helm/augment-tide-chart.sh` invocation — already wired into the helmify
  pipeline via the existing `make helm-controller` target (line 555 onwards in
  Makefile). No new wiring needed; the augment script's step 7a runs alongside
  the existing 8 augment steps automatically on every chart regeneration.

## Self-Check: PASSED

- `hack/helm/per-namespace-rolebinding.yaml`: FOUND
- `charts/tide/templates/per-namespace-rolebinding.yaml`: FOUND
- `hack/scripts/test-per-ns-rb.sh`: FOUND (executable)
- `hack/helm/tide-values.yaml`: contains `^projectNamespaces: \[\]` (1 match)
- `hack/helm/augment-tide-chart.sh`: contains the new step 7a `cp` line
- `Makefile`: contains `.PHONY: test-per-ns-rb` + the target body
- `charts/tide/values.yaml`: contains the `projectNamespaces: []` block (1 match)
- Commit `d29917e`: FOUND in `git log` (Task 1)
- Commit `c6861ee`: FOUND in `git log` (Task 2)
- `make test-per-ns-rb`: exits 0
- `helm lint charts/tide`: exits 0
- Augment script idempotency: verified across 2× invocations (md5 stable)
