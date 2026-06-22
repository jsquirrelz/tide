# Phase Brief — phase-01-provider-switch

**Phase:** `phase-01-provider-switch`
**Milestone:** `milestone-03-hetero-integration`
**Wave position:** Milestone wave 1 — no upstream phase dependencies; runs in
parallel with any other milestone-wave-1 phase.

---

## Objective

Extend the TIDE controller's subagent resolution machinery with a
**per-level vendor field** — so a single Project CRD can declare that its
planner levels run Claude and its executor levels run Codex, and
`ResolveProvider` delivers the correct `ProviderSpec.Vendor` to each
dispatched Job without any Codex-specific code touching the controller or
`cmd/manager`.

Today `ResolveProvider` (`internal/controller/dispatch_helpers.go`) returns
`Vendor:"anthropic"` unconditionally — a non-Anthropic image at a level is
*selected* by `resolveImage` but then *rejected* at runtime by the image's
vendor sentinel check (`vendorSentinel != "anthropic"` → fail-fast refuse).
This phase closes that gap. It is a pure controller-side refactor: no Codex
CLI, no container image, no usage/pricing code — those belong to
phase-02-codex-subagent-core and phase-03-codex-image-entrypoint.

---

## Scope

**In scope:**

- Add `Vendor string` to `LevelConfig` and `SubagentConfig` in
  `api/v1alpha1/project_types.go`; add CEL validation marker (`+kubebuilder:validation:Enum=anthropic;openai`)
  so the webhook rejects unknown vendors at admission time.
- Re-run `make manifests generate` to propagate the new field into
  `zz_generated.deepcopy.go` and the CRD YAML under `config/crd/bases/`.
- Add `Vendor string` to `ProviderDefaults`; rewire `ResolveProvider` to
  resolve vendor down the precedence chain:
  `Levels.<level>.Vendor → Spec.Subagent.Vendor → helmDefaults.Vendor → "anthropic"`.
- Thread the Helm-chart vendor default through `cmd/manager/env.go`
  (a `resolveDefaultVendor()` helper, `tideHelmProviderDefaults` extended) and
  the manager Deployment template (`TIDE_DEFAULT_VENDOR` env var).
- Add `subagent.defaults.vendor` (and optional per-level vendor keys) to
  `charts/tide/values.yaml`; wire them into `charts/tide/templates/deployment.yaml`.

**Out of scope (later phases):**

- The Codex subagent package itself (`internal/subagent/codex/`).
- The Codex container image and entrypoint (`cmd/codex-subagent/`).
- Helm chart Secret wiring for `OPENAI_API_KEY` — that is
  phase-05-chart-secret-wiring.
- The heterogeneous integration example and end-to-end test — phase-06.

---

## Deliverables

| Deliverable | Verification |
|---|---|
| `LevelConfig.Vendor` + `SubagentConfig.Vendor` fields with CEL enum marker | `make manifests generate` exits 0; deepcopy regenerated |
| `ResolveProvider` vendor precedence chain | Unit test: Codex-pinned level → `"openai"`, Claude-pinned level → `"anthropic"`, default → `"anthropic"` |
| `ProviderDefaults.Vendor` + `resolveDefaultVendor()` in `cmd/manager/env.go` | `env_test.go` round-trip: `TIDE_DEFAULT_VENDOR=openai` → `ProviderDefaults.Vendor=="openai"` |
| `subagent.defaults.vendor` Helm value + `deployment.yaml` `TIDE_DEFAULT_VENDOR` env | `helm template` renders correct env value |
| `make verify-import-firewall` + `make verify-dispatch-imports` green | No `openai` or `codex` package appears in `internal/controller/` or `cmd/manager/` |
| `make test` exits 0 | All existing + new unit tests pass |

---

## Verification Gates

1. `make test` exits 0 — all existing unit tests plus the new vendor-resolution
   suite pass offline with no network calls.
2. `make manifests generate` exits 0 — CRD YAML and deepcopy are regenerated
   from the updated types; no manual edits to generated files.
3. `make verify-import-firewall` and `make verify-dispatch-imports` are green —
   zero Codex/OpenAI imports visible in `internal/controller/` or `cmd/manager/`.
4. Unit test explicitly proves: in one Project, a `Levels.Task.Vendor="openai"`
   override resolves `Vendor:"openai"` while a `Levels.Phase` with no vendor
   override resolves `Vendor:"anthropic"`.
5. `helm template` renders the `TIDE_DEFAULT_VENDOR` env var in the manager
   Deployment under both the default path and an explicit `--set` override.

---

## Plan Decomposition

Three Plans in a linear dependency chain — each plan has a clean,
non-overlapping file set enforced by the admission webhook.

```
plan-01-vendor-api-types
        ↓
plan-02-resolve-provider-vendor
        ↓
plan-03-manager-helm-vendor
```

**Wave assignment (illustrative — authoritative wave derived by Kahn over
the Task DAG):**

| Plan | Wave | Depends on |
|---|---|---|
| `plan-01-vendor-api-types` | 1 | — |
| `plan-02-resolve-provider-vendor` | 2 | `plan-01-vendor-api-types` |
| `plan-03-manager-helm-vendor` | 3 | `plan-02-resolve-provider-vendor` |
