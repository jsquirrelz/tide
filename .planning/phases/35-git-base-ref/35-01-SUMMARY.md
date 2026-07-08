---
phase: 35-git-base-ref
plan: 01
subsystem: api-crd
tags: [crd-schema, git-base-ref, both-versions, conversion-round-trip, chart-render-lock]
requires: []
provides:
  - "GitConfig.BaseRef (spec.git.baseRef) — both API versions, charset-validated, no default"
  - "GitStatus.BaseSHA (status.git.baseSHA) — both API versions, provenance stamp"
  - "Regenerated config/crd/bases + charts/tide-crds carrying both fields under both version blocks"
  - "Source-parity, JSON round-trip, and helm-render test locks (BASE-03 schema half)"
affects:
  - "plan 35-02 (pkg/git resolveBaseRef, tide-push --base-ref, clone envelope)"
  - "plan 35-03 (controller classification, baseSHA stamping, dispatch halt)"
  - "plan 35-04 (kind e2e, docs)"
tech-stack:
  added: []
  patterns:
    - "Both-versions CRD field parity (no conversion webhook; strategy None) locked by static + JSON round-trip tests"
    - "Field-level kubebuilder Pattern/MaxLength for charset validation (absence auto-guarded, no CEL, no default)"
key-files:
  created:
    - api/v1alpha1/phase35_schema_test.go
    - test/integration/kind/baseref_crd_render_test.go
  modified:
    - api/v1alpha2/project_types.go
    - api/v1alpha1/project_types.go
    - config/crd/bases/tideproject.k8s_projects.yaml
    - charts/tide-crds/templates/project-crd.yaml
decisions:
  - "Shipped Pattern `^[A-Za-z0-9][A-Za-z0-9._+@/-]*$` + MaxLength=250 + MinLength=1 on baseRef; NO +kubebuilder:default (P10 — absent is the only HEAD encoding); NO XValidation/oldSelf (D-08)"
  - "Chart version intentionally NOT bumped — batches with Phase 36 per RESEARCH A2 / D-06 (verifier: do not flag the unbumped chart)"
  - "v1alpha1 GitStatus has no CloneComplete field — BaseSHA landed after LeaseFailureCount there; after CloneComplete in v1alpha2"
metrics:
  tasks_completed: 2
  files_created: 2
  files_modified: 4
  completed_date: 2026-07-06
status: complete
---

# Phase 35 Plan 01: Git Base Ref CRD Schema Summary

Added operator-selectable `spec.git.baseRef` (charset-validated, no default) and `status.git.baseSHA` (provenance stamp) to BOTH v1alpha1 and v1alpha2 Project types, regenerated the CRD artifacts, and landed the source-parity / JSON round-trip / helm-render test locks that BASE-03's schema half requires — with the chart version deliberately left unbumped for Phase 36's batch.

## What Was Built

**Task 1 — API fields + regenerated CRD artifacts** (commit `3e0ed68`)
- `api/v1alpha2/project_types.go`: `GitConfig.BaseRef` (after `LeaksConfigRef`) with markers `MinLength=1`, `MaxLength=250`, `Pattern=` ``^[A-Za-z0-9][A-Za-z0-9._+@/-]*$``, `+optional` and NO default; `GitStatus.BaseSHA` (after `CloneComplete`), `+optional`.
- `api/v1alpha1/project_types.go`: identical `BaseRef` twin (after `LeaksConfigRef`); `BaseSHA` after `LeaseFailureCount` (v1alpha1 GitStatus has no `CloneComplete` — none added).
- Regenerated `config/crd/bases/tideproject.k8s_projects.yaml` via `make manifests generate` and `charts/tide-crds/templates/project-crd.yaml` via `make helm-crds`. No hand-edits to generated files.

**Task 2 — parity / round-trip / render locks** (commit `d5ffc05`)
- `api/v1alpha1/phase35_schema_test.go` (package `v1alpha1_test`, reuses phase3 `findRepoRoot`/`readProjectCRD` helpers): both-versions source parity, no-default/no-XValidation marker guard scoped to the BaseRef region, config/crd YAML both-block presence (`baseRef:`/`baseSHA:` count == 2) + Pattern presence, and JSON round-trip v1alpha1⇄v1alpha2 (both directions).
- `test/integration/kind/baseref_crd_render_test.go` (package `kind_integration`): `helm template tide-crds` both-version-block lock — asserts `baseRef:`/`baseSHA:` and the Pattern each appear exactly twice; plain go-test, runs without a cluster (P8 chart-skew lock).

## Exact Pattern Regex Shipped

```
^[A-Za-z0-9][A-Za-z0-9._+@/-]*$
```

First-char class rejects leading `-` (argument-injection shape, T-35-01), leading `.` and `/`; charset excludes spaces, control chars, and git-forbidden metacharacters (`~ ^ : ? * [ \`) while admitting `refs/`-qualified values and 40-hex SHAs. `MaxLength=250` bounds regex-evaluation cost (T-35-04). No CEL rule added.

## Generated-File Diff Summary

`git diff --name-only` for the code commits touched exactly four tracked files plus two new test files:
- `config/crd/bases/tideproject.k8s_projects.yaml`: `baseRef:` ×2, `baseSHA:` ×2 (one per version block), `pattern: ^[A-Za-z0-9][A-Za-z0-9._+@/-]*$` ×2, `maxLength: 250` present.
- `charts/tide-crds/templates/project-crd.yaml`: `baseRef:` ×2, `baseSHA:` ×2 (helmify pass preserved the Pattern in both blocks).
- **No `charts/tide-crds/Chart.yaml`, `charts/tide/Chart.yaml`, or `charts/tide/values.yaml` diff** — chart version untouched (Phase 36 batches the single bump).

## Verification (observed)

| Command | Result |
|---|---|
| `go build ./api/...` | exit 0 |
| `make manifests generate` | OK (controller-gen regenerated CRD + deepcopy) |
| `make helm-crds` | OK (helmify augmented tide-crds) |
| `grep -c '^ *baseRef:' config/crd/bases/...` | `2` |
| `grep -c '^ *baseSHA:' config/crd/bases/...` | `2` |
| `grep -c '^ *baseRef:'/'^ *baseSHA:' charts/tide-crds/...` | `2` / `2` |
| `go test ./api/... -count=1` | ok (v1alpha1, v1alpha2) |
| `go test ./api/... -run 'BaseRef\|BaseSHA\|Phase35'` | ok |
| `go test ./test/integration/kind/ -run TestHelmTideCRDsRenderBaseRefBothVersions` | ok (no cluster) |
| `go vet ./api/... ./test/integration/kind/` | clean |
| `go build ./api/... ./internal/... ./pkg/... ./cmd/tide-push/...` | exit 0 |

## Deviations from Plan

None — plan executed exactly as written. No Rule 1/2/3 auto-fixes were required.

## Deferred Issues

**[Out of scope] `go build ./...` fails on `cmd/tide-demo-init`** — `//go:embed all:fixture` needs the gitignored, build-time-populated `cmd/tide-demo-init/fixture/` dir, absent in a fresh worktree. Identical failure on base `main` (624e770); package untouched by Phase 35. Logged to `deferred-items.md`; not fixed (scope boundary). All in-scope packages build clean.

## Notes for the Verifier / Next Wave

- **Do NOT flag the unbumped chart version** — deferring the `Chart.yaml`/`hack/helm/tide-chart.yaml` bump to Phase 36 is the locked decision (RESEARCH A2, objective scope guard).
- **`make test-int` reminder (CLAUDE.md):** the kind package bundles plain go-tests beside Ginkgo — read `MAKE_EXIT` and grep `^--- FAIL` rather than trusting the Ginkgo summary. The new render test is a plain go-test in that package.
- **No CEL / no default / no immutability** on baseRef by design (P10 / D-08). The marker-guard test (`TestBaseRefHasNoDefaultMarker`) will fail if a later plan adds any of these.
- Plans 35-02/35-03 consume these fields: `resolveBaseRef` + `EnsureRunBranch(baseRef)` signature change (35-02), and controller-side baseSHA stamping + `BaseRefUnresolvable` classification (35-03).

## Self-Check: PASSED
- `api/v1alpha1/phase35_schema_test.go` — FOUND
- `test/integration/kind/baseref_crd_render_test.go` — FOUND
- Commit `3e0ed68` — FOUND
- Commit `d5ffc05` — FOUND
