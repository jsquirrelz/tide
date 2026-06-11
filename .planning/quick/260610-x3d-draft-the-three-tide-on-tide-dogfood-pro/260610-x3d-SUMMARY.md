---
phase: quick-260610-x3d
plan: 01
subsystem: examples/dogfood
tags: [dogfood, project-crd, manifests, guard-test, tdd]

dependency_graph:
  requires: []
  provides:
    - examples/projects/dogfood/01-analytics-project.yaml
    - examples/projects/dogfood/02-codex-runtime-project.yaml
    - examples/projects/dogfood/03-project-editor-project.yaml
    - examples/projects/dogfood/README.md
    - api/v1alpha1/dogfood_manifests_test.go
  affects:
    - go.mod (sigs.k8s.io/yaml promoted from indirect to direct)

tech_stack:
  added:
    - sigs.k8s.io/yaml v1.6.0 (promoted from indirect to direct dep)
  patterns:
    - multi-doc YAML split on "\n---" boundaries
    - UnmarshalStrict schema-validity guard without live cluster

key_files:
  created:
    - examples/projects/dogfood/01-analytics-project.yaml
    - examples/projects/dogfood/02-codex-runtime-project.yaml
    - examples/projects/dogfood/03-project-editor-project.yaml
    - examples/projects/dogfood/README.md
    - api/v1alpha1/dogfood_manifests_test.go
  modified:
    - go.mod

decisions:
  - sigs.k8s.io/yaml.UnmarshalStrict chosen for strict-decode test (already transitive dep; promoted to direct via go mod tidy)
  - splitYAMLDocs uses bytes.Split on "\n---" boundary — handles leading "---" on first document via TrimPrefix; simpler than yaml.NewYAMLOrJSONDecoder
  - hasTopLevelKey scans line-by-line for zero-indent "key:" prefix — catches data/stringData at top-level only, not nested Secret data keys in test fixtures

metrics:
  duration: ~10min
  completed: 2026-06-11T04:01:15Z
  tasks_completed: 2
  files_created: 5
  files_modified: 1
---

# Phase quick-260610-x3d Plan 01: Draft TIDE-on-TIDE Dogfood Project CR Manifests Summary

Three apply-ready Project CR manifests and a strict-decode guard test for the TIDE-on-TIDE dogfood runs — analytics (Prometheus telemetry), Codex runtime (heterogeneous dispatch), and Project editor (dashboard mutation surface).

## What Was Built

### Task 1: Three dogfood Project CR manifests + README

`examples/projects/dogfood/` directory created with:

**01-analytics-project.yaml** — Namespace `tide-dogfood-analytics` + Project `dogfood-analytics`. outcomePrompt encodes:
- Locked: Prometheus-is-the-DB via prometheus/client_golang; graceful degradation when prometheus.enabled=false; per-task labels FORBIDDEN (cardinality budget); project/phase/wave labels approved; OTel spans remain orthogonal.
- Open questions posed (not answered): PromQL proxy vs direct datasource; cardinality budget enforcement point; Prometheus retention for multi-day charts.

**02-codex-runtime-project.yaml** — Namespace `tide-dogfood-codex` + Project `dogfood-codex-runtime`. outcomePrompt encodes:
- Locked: `internal/subagent/codex/` mirroring anthropic/ layout; all Codex code behind pkg/dispatch.Subagent interface; per-level (not per-task, not per-project) runtime selection; OPENAI_API_KEY via K8s Secret; identical wave-boundary failure semantics for mixed-provider waves.
- Codex CLI facts carried verbatim: `codex exec` headless one-shot, `--output-schema` for JSON child-CRD emission, `--sandbox workspace-write`, API-key env auth.

**03-project-editor-project.yaml** — Namespace `tide-dogfood-editor` + Project `dogfood-project-editor`. outcomePrompt encodes:
- Locked: mutation endpoints on existing chi server (no new service); reference-only credentials (Secret names only, never material); trust-the-perimeter for v1 with docs/dashboard-mutation-auth-hardening.md as required deliverable; RBAC scoped to create/update on Projects only; editing in-flight Projects out of scope.
- Open questions posed (not answered): draft state API shape (spec.paused vs annotation vs phase-gating); edit semantics (server-side apply vs full replace); draft-to-running transition; CEL immutability enforcement.

**README.md** — Run-order table with rationale, per-namespace prerequisites, Secret creation steps (placeholders only), kubectl wait apply recipe, cluster sizing recipe reference.

All three manifests:
- Use `tideproject.k8s/v1alpha1` (never `tide.io`)
- Reference credentials by Secret name only (`tide-secrets`; `openai-secrets` for codex)
- Set `gates.milestone: approve, phase: approve` (human oversight at top two levels)
- Budget: `absoluteCapCents: 10000` (analytics, codex) / `7500` (editor); `rollingWindowDuration: 24h`
- `subagent.image: ghcr.io/jsquirrelz/tide-claude-subagent:1.0.0`, `model: claude-opus-4-7`

### Task 2: Guard test (dogfood_manifests_test.go)

`api/v1alpha1/dogfood_manifests_test.go` in package `v1alpha1_test`:

- **TestDogfoodManifests_GlobFindsThreeFiles** — glob count == 3; all files readable.
- **TestDogfoodManifests_StrictDecode** — UnmarshalStrict into v1alpha1.Project per file; typo'd field names fail here without a live cluster.
- **TestDogfoodManifests_RequiredFields** — apiVersion, spec.targetRepo, spec.outcomePrompt, spec.providerSecretRef, spec.git.credsSecretRef asserted per file.
- **TestDogfoodManifests_NoInlineSecrets** — no top-level `data:` or `stringData:` keys in any YAML document in any file.

`go test ./api/v1alpha1/ -run DogfoodManifests -count=1` exits 0. Full package (`./api/v1alpha1/`) exits 0.

`sigs.k8s.io/yaml` promoted from `// indirect` to direct in go.mod via `go mod tidy`.

## Verification Results

```
ls examples/projects/dogfood/       → README.md + 01- 02- 03-.yaml
go test ./api/v1alpha1/ -run DogfoodManifests -count=1  → PASS
grep -c outcomePrompt 0*.yaml       → 1:1:1 per file
grep -rn 'tide\.io' dogfood/        → (no output) — domain rule honored
grep -il 'open question' 01- 03-    → both files match
```

## Deviations from Plan

None — plan executed exactly as written.

Note on TDD order: the plan specifies Task 1 (manifests) before Task 2 (guard test). Because manifests are created in Task 1, the Task 2 test passes on first run rather than failing (RED). This is expected: the guard test's purpose is regression prevention, not test-first development. The test correctly verifies the manifests and would fail if field names were misspelled, required fields were empty, or inline Secret material appeared.

## Known Stubs

None. All outcomePrompts contain substantive locked decisions and open research questions. No placeholder text flows to any rendering surface.

## Threat Flags

No new threat surface beyond what the plan's threat model documents. T-q260610-01 (information disclosure via inline secrets) verified mitigated — guard test asserts no `data`/`stringData` keys; grep rejects `sk-ant-`/`sk-proj-` patterns.

## Self-Check: PASSED

All created files exist on disk. Both task commits (15de022, 730b960) are present in git log.
