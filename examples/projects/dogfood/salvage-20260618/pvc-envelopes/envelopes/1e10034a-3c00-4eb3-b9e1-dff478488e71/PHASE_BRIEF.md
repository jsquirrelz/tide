# Phase Brief — phase-04-per-level-vendor-switch

**Phase:** phase-04-per-level-vendor-switch  
**Milestone:** milestone-03-hetero-integration  
**Status:** Planned  
**Dependencies:** none (this phase is a Wave-1 root)

---

## Objective

Wire the controller-side per-level vendor switch — the missing half of
heterogeneous dispatch. Today `ResolveProvider`
(`internal/controller/dispatch_helpers.go`) returns `Vendor:"anthropic"`
unconditionally; a Codex image configured at a level is *selected* but then
*rejected* at runtime by the image's vendor sentinel. This phase eliminates
that contradiction by threading an optional `Vendor` field down the same
precedence chain already used for `Model` and `Image`, with no Codex imports
entering the controller or `cmd/manager/`.

---

## Scope

### In scope

- **API schema** — add an optional `Vendor` string field to `LevelConfig` and
  `SubagentConfig` in `api/v1alpha1/project_types.go`, with a
  `+kubebuilder:validation:Enum=anthropic;openai` CEL marker (optional →
  empty string is permitted and means "inherit from next tier"). Regenerate
  `zz_generated.deepcopy.go` and `config/crd/bases/tideproject.k8s_projects.yaml`
  via `make manifests generate`.

- **ResolveProvider rewrite** — replace the unconditional `Vendor:"anthropic"`
  in `internal/controller/dispatch_helpers.go` with a resolution chain
  identical in structure to the existing Model chain:
  `Levels.<level>.Vendor → Spec.Subagent.Vendor → helmDefaults.Vendor → "anthropic"`.
  Add a `Vendor string` field to `ProviderDefaults`.

- **Manager env threading** — add `TIDE_DEFAULT_VENDOR` support to
  `cmd/manager/env.go` and thread it into `tideHelmProviderDefaults`, so the
  Helm chart can set a cluster-wide vendor default without touching any Project
  CRD. Default: `"anthropic"` (backward-compatible).

- **Unit tests** — prove a Project where one level pins `Vendor:"openai"` and
  another pins `Vendor:"anthropic"` (or inherits the default) resolves correctly
  through every tier of the chain.

### Out of scope

- No Codex imports — not in `internal/controller/`, not in `cmd/manager/`.
- No chart changes — chart wiring is phase-05's deliverable.
- No per-level *image* routing changes (already works via `resolveImage`).
- No wave-boundary or failure-handling changes.

---

## Expected Deliverables

| # | Artifact | Location |
|---|----------|----------|
| 1 | `Vendor` field on `LevelConfig` + `SubagentConfig` | `api/v1alpha1/project_types.go` |
| 2 | Regenerated deepcopy | `api/v1alpha1/zz_generated.deepcopy.go` |
| 3 | Regenerated CRD YAML with enum validation | `config/crd/bases/tideproject.k8s_projects.yaml` |
| 4 | `ProviderDefaults.Vendor` + rewired `ResolveProvider` | `internal/controller/dispatch_helpers.go` |
| 5 | `TIDE_DEFAULT_VENDOR` env plumbing | `cmd/manager/env.go`, `cmd/manager/main.go` |
| 6 | Unit tests for per-level and mixed-project resolution | `internal/controller/dispatch_helpers_test.go`, `cmd/manager/env_test.go` |

---

## Verification Gates

1. `make manifests generate` exits 0 — CRD YAML and deepcopy are clean.
2. `make test` exits 0 — all existing tests plus new vendor-switch unit tests pass.
3. `make verify-import-firewall` and `make verify-dispatch-imports` are green —
   zero provider imports leak into `internal/controller/` or `cmd/manager/`.
4. A unit test proves: same `Project`, `task` level pinned `Vendor:"openai"`,
   `phase` level unset → `ResolveProvider(project, "task", …)` returns
   `Vendor:"openai"` and `ResolveProvider(project, "phase", …)` returns
   `Vendor:"anthropic"`.
5. A unit test proves: `Spec.Subagent.Vendor:"openai"` propagates to all
   un-overridden levels and is overridden by a level that sets `Vendor:"anthropic"`.
6. Existing `TestResolveProviderPerLevelWins` and siblings continue to pass
   without modification to their assertions — the default still resolves
   `"anthropic"` when no explicit Vendor is configured.

---

## Plan Decomposition (illustrative waves)

```
Wave 1 (root):
  plan-01-vendor-field-schema       — API types + regeneration

Wave 2 (depends on plan-01):
  plan-02-resolve-provider-rewire   — ResolveProvider + ProviderDefaults + env.go

Wave 3 (depends on plan-02):
  plan-03-vendor-switch-tests       — Unit tests + make test green
```

The planning DAG is a strict chain — each plan's interface contract is
determined by the plan before it, so no parallelism is available or needed.
