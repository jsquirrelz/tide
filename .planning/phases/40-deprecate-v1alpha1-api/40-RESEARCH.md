# Phase 40: Deprecate v1alpha1 API - Research

**Researched:** 2026-07-06
**Domain:** Kubernetes CRD multi-version lifecycle (kubebuilder/controller-gen), Go package rename/migration, chart-generation reproducibility
**Confidence:** HIGH (mechanics verified against this repo's own Phase 23 precedent commits + live grep of every touch point; a few chart/analyzer-scope questions are MEDIUM — flagged inline)

## Summary

This phase is mechanically a repeat of Phase 23 (v1alpha1→v1alpha2), which already ran in
this exact repo five plans ago and left a clean commit trail (`67cb313` → `c3191d7`,
23-01..23-05). That trail is the load-bearing precedent: it proves controller-gen's directory
convention (`api/v1alpha2/` is a normal Go package; `make manifests`/`make generate` glob
`./api/...` with no version hardcoded) generates a correct multi-version CRD automatically, and
it proves the shape of a safe crank: (1) new schema package, (2) webhooks + scheme +
storageversion flip + migration guide, (3) fail-closed guard, (4) mass consumer re-import, (5)
gap-closure. Phase 40 must run the same five-step shape and then add a sixth: delete both
`api/v1alpha1/` and `api/v1alpha2/` entirely (something Phase 23 never did — it kept v1alpha1
around only for the reinstall guard).

The consumer-migration surface is real but bounded: 47 non-test `.go` files import
`api/v1alpha2` today (verified via `grep -rl`), concentrated in `internal/controller/`,
`internal/dispatch/podjob/`, `internal/webhook/v1alpha2/`, `cmd/manager`, `cmd/dashboard`,
`cmd/tide`, and the `cmd/dashboard/api/*` handlers. `api/v1alpha1` has exactly one remaining
importer outside its own package: `api/v1alpha1/dogfood_manifests_test.go`, which validates the
3 `examples/projects/dogfood/0*.yaml` fixtures against both schema versions — this test's
package dies with `api/v1alpha1/` removal and must be relocated/rewritten to validate only
v1alpha3 fixtures.

The `subagent.levels` semantic rename (D-02) is a **3-line-of-code change**, not a schema
change to `LevelOverrides` — the struct field names (`Milestone`/`Phase`/`Plan`/`Task`) and the
`ResolveProvider`/`resolveImage` switch statements stay byte-identical. What changes is which
literal string each of the four planner reconcilers passes as the `level` argument to
`BuildPlannerEnvelope`/`resolveImage`/`podjob.BuildOptions.Level`. Concretely: `project_controller.go`
shifts `"project"`→`"milestone"`, `milestone_controller.go` shifts `"milestone"`→`"phase"`,
`phase_controller.go` shifts `"phase"`→`"plan"`, and `plan_controller.go` keeps `"plan"`
unchanged (both the old phase-level and plan-level dispatches now share the one `levels.plan`
override slot — this collapse is exactly what CONTEXT.md left as Claude's Discretion). The
chart's `values.yaml` per-level model defaults (`subagent.levels.milestone.model:
claude-opus-4-8`, comment: "heavy planning, lowest fan-out") were **already authored with the
new, intuitive semantics** — they describe what should plan MILESTONE.md, not what the
`MilestoneReconciler` object dispatches today. This is strong independent evidence the rename
target in the folded todo is correct: the chart's intent and the controller's behavior have
been out of sync since the chart value was authored, and the rename finally reconciles them
with **zero values.yaml changes required**.

The research turned up several drift/wart findings beyond what CONTEXT.md's canonical_refs
already named — most importantly a **dead `ProjectSpec.ModelSelection` field** (declared,
never read by any reconciler — confirmed by whole-repo grep), a **duplicate
`tidev1alpha2.AddToScheme(scheme)` call** in `cmd/manager/main.go` (harmless but the adjacent
comment is factually wrong), a **hardcoded-by-version-name Makefile gate**
(`verify-no-aggregates` greps literal `api/v1alpha1/*_types.go api/v1alpha2/*_types.go` — this
silently stops checking anything once those directories are deleted unless updated), a
**stale kubebuilder `PROJECT` bookkeeping file** (never updated in Phase 23; still lists all
six Kinds under `path: .../api/v1alpha1` and claims `Plan` has a live `conversion` webhook —
decorative/non-functional but easy to fix in the same pass), and **two already-stale docs**
(`SECURITY.md:40`, `docs/rbac.md:213`) that describe a "no-op conversion webhook" that Phase 23
already fully retired — these predate this phase and are a separate, larger accuracy gap than
D-06/D-07's named list.

**Primary recommendation:** Mirror Phase 23's 5-plan shape (schema package → webhooks/scheme/
storageversion+migration-doc → guards → mass consumer migration → gap-closure) and add a final
removal plan; do the `subagent.levels` rename as 4 call-site edits inside the "mass consumer
migration" plan, not as a separate schema change; fix `verify-no-aggregates` and the `PROJECT`
file in the same pass as the removal (both are one-line-glob-pattern-scale fixes); treat
`SECURITY.md`/`docs/rbac.md` conversion-webhook staleness as bonus scope the user should be
offered, since it's real but wasn't in CONTEXT.md's named doc list.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01: Full lifecycle turn.** Introduce v1alpha3, migrate all consumers, then remove
  v1alpha1 and v1alpha2 within this phase ("remove v1alpha2 as well once v1alpha3 is ready" —
  sequencing inside the phase: v1alpha3 lands first, removals follow). No `deprecated: true`
  marker period — single-user reality makes it dead weight.
- **D-02: The `subagent.levels` rename is folded in** (supersedes the 2026-07-03 "own
  milestone" routing). The rename's target semantics are ALREADY DECIDED in the todo — do not
  re-litigate: each `levels.X` key means "level X is planned by this model"; do NOT add a
  `levels.project` key; do NOT keep the current dispatching-CR semantics. The todo's target
  mapping table (spec key → dispatch surface) is the authority.
- **D-03: Reinstall-only.** Consistent with Phase 23 D-09 (conversion webhook retired). No
  storedVersions prune recipes, no preflight automation, no conversion webhooks. Migration doc
  instructs: delete CRDs + re-apply Projects under v1alpha3 with `schemaRevision: v1alpha3`. The
  kind-tide-dogfood cluster (still pre-Spring-Tide v1alpha1) gets rebuilt, not upgraded — build
  zero compatibility for it.
- **D-04: Generalize the SchemaRevision guard; it is the permanent crank mechanism.**
  `checkSchemaRevisionGuard` (project_controller.go) now expects `schemaRevision: v1alpha3`;
  parameterize the expected revision + message text so a future v1alpha4 crank is a one-line
  change. Keep it fail-closed (Ready=False / RequiresReinstall / TerminalError).
- **D-05: Drop the owner-ref dual-accepts.** `task_controller.go:1205` and
  `internal/dispatch/podjob/backend.go:377` currently accept `tideproject.k8s/v1alpha1 ||
  v1alpha2` owner refs. Under reinstall-only migration, stale refs cannot exist — simplify to
  the current GroupVersion only.
- **D-06: Deep accuracy pass, not a mechanical bump.** All user-facing docs land on v1alpha3:
  `docs/INSTALL.md` (quickstart example is broken TODAY — v1alpha1 is not served),
  `docs/gates.md`, `docs/git-hosts.md`, `docs/project-authoring.md` (its header claims a
  "v1alpha1 schema lock" — re-lock to v1alpha3). All 11 `config/samples/tide_v1alpha1_*.yaml`
  files: contents to v1alpha3 and filenames renamed to match (`tide_v1alpha3_*`). Migration
  guide gains the v1alpha2→v1alpha3 chapter including a levels-remap table (old key meaning →
  new key meaning) so an operator can re-author their Project correctly.
- **D-07: Sweep the stale comments.** ~10 files carry comment-only v1alpha1 references,
  several factually wrong (e.g. `cmd/manager/main.go:65-66,303-309` claims v1alpha1 is still
  scheme-registered; it is not — the import is gone). Comments must describe the v1alpha3
  world.
- **D-08: Decouple to `dispatch.tideproject.k8s/v1alpha1`** (kubeadm pattern: K8s-shaped
  document that is not a served resource gets its own subdomain group). Version component
  stays `v1alpha1` — pure group decoupling, no stability claim. This SUPERSEDES
  `pkg/dispatch/doc.go`'s documented plan to bump the envelope to `tideproject.k8s/v1beta1` —
  that plan is what created the collision. If the levels rename forces envelope field/value
  changes (the `Level` field's `"project"` mismatch is part of the folded todo's bug surface),
  both breaks ride this one crank. All in-repo subagent images and the contract doc update
  together.
- **D-09: Research audits for batchable breaking changes.** The phase researcher inventories
  known v1alpha2 schema warts (deprecated fields, CEL validation gaps, naming inconsistencies)
  beyond the levels rename; the user picks what rides the crank at PLAN approval. This is an
  ASK-FIRST checkpoint — present the inventory, do not silently batch.

### Claude's Discretion

- Envelope `Level` string values (whether `"project"` dispatch level is renamed as part of the
  mapping fix) — within the todo's decided mapping.
- Plan sequencing/decomposition (v1alpha3-first then removals is the natural order; how many
  plans is the planner's call).
- New REQUIREMENTS.md requirement IDs for this phase (bookkeeping at plan time).
- Whether `api/v1alpha3` starts as a copy of v1alpha2 (Phase 23 precedent) or
  kubebuilder-scaffolded fresh.

### Deferred Ideas (OUT OF SCOPE)

- **Envelope stability declaration (`dispatch.tideproject.k8s/v1`):** deliberately NOT taken —
  revisit once the post-rename contract has soaked.
- **`subagent.levels` "own milestone" routing:** superseded by the fold into Phase 40 (user
  decision 2026-07-06).

</user_constraints>

<phase_requirements>
## Phase Requirements

No REQUIREMENTS.md IDs exist for this phase yet — Phase 40 was added to v1.0.7's roadmap after
the milestone's requirement set was minted (2026-07-06 discussion), and per CONTEXT.md
"Claude's Discretion," new requirement IDs are bookkeeping the planner mints at plan time. The
table below maps the phase's decided scope to the work areas a requirement ID should cover, so
the planner can mint IDs 1:1 against real deliverables instead of guessing granularity.

| Suggested area | Description | Research Support |
|----------------|--------------|-------------------|
| SCHEMA (v1alpha3 introduction) | New `api/v1alpha3` package, storageversion flip, CRD regen | Architecture Patterns § "Introducing v1alpha3"; Phase 23 precedent commits |
| RENAME (subagent.levels) | 4 call-site level-string edits + envelope Level field | Code Examples § "The exact rename edits"; Summary |
| REMOVAL (v1alpha1 + v1alpha2 deletion) | Delete both Go packages, CRD blocks, chart copies, scheme regs | Architecture Patterns § "Removal mechanics"; Don't Hand-Roll |
| GUARD (SchemaRevision generalization) | Parameterize `checkSchemaRevisionGuard` | Code Examples § "Generalizing the guard" |
| OWNERREF (dual-accept removal) | Simplify `task_controller.go:1205`, `podjob/backend.go:377` | Code Examples § "Owner-ref simplification" |
| ENVELOPE (group decoupling) | `dispatch.tideproject.k8s/v1alpha1`, fix push/eval literal drift | Code Examples § "Envelope decoupling" |
| DOCS (accuracy sweep) | INSTALL/gates/git-hosts/project-authoring + 11 samples + migration guide | Docs/Samples Sweep Inventory table |
| DOCS-BONUS (pre-existing staleness) | SECURITY.md:40, docs/rbac.md:213 (already-stale conversion-webhook claims, pre-dates this phase) | Common Pitfalls § "Pre-existing doc staleness" — ASK USER whether in scope |
| BUILD (tooling accuracy) | `verify-no-aggregates` Makefile glob, `PROJECT` kubebuilder metadata | Common Pitfalls § "Hardcoded version-name tooling" |
| WART (D-09 batchable fixes) | User-selected items from the wart inventory table | Package Legitimacy Audit is N/A; see "v1alpha2 Schema Wart Inventory (D-09)" |
| VERIFY (test suite migration) | envtest/kind scheme registration, spec_conformance_test.go, dogfood_manifests_test.go relocation | Validation Architecture |

</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| CRD schema definition (types + markers) | API / Backend (`api/v1alpha3`) | — | Go structs + kubebuilder markers are the single source of truth; CRD YAML is generated, never hand-edited |
| Admission validation (webhooks) | API / Backend (`internal/webhook/v1alpha3`) | — | CEL covers most rules; the Plan cycle-detection webhook is the one Go-code exception (documented in `docs/rbac.md`) |
| Schema-revision fail-closed guard | API / Backend (`internal/controller`) | — | Runtime gate, not admission-time — objects that pre-date the CRD upgrade must still be readable to reject them loudly |
| Envelope contract (dispatch documents) | API / Backend (`pkg/dispatch`), consumed by subagent images (external) | — | Decoupled group (`dispatch.tideproject.k8s`) precisely because it crosses the trust/process boundary into subagent containers — not a served K8s resource |
| Chart/CRD packaging | CDN / Static-equivalent (Helm chart is TIDE's "distribution" tier) | — | `charts/tide-crds/` is helmify-generated from `config/crd/bases/`; never hand-edited (drift caught by `make verify-chart-reproducible`) |
| Dashboard read surfaces | Frontend Server (`cmd/dashboard`) | Browser (`dashboard/web`) | Dashboard backend imports `api/v1alphaN` typed clients for read-only informer caches; the SPA has zero API-version awareness (confirmed: no v1alpha hits in `dashboard/web/src`) |
| Docs/samples | Docs (no runtime tier) | — | Pure accuracy surface; no code path reads `config/samples/*.yaml` or `docs/*.md` at runtime |

## Standard Stack

This phase makes no new library additions — it is a schema/rename/removal operation on the
existing stack. No `## Package Legitimacy Audit` is required (no external packages are
installed). The relevant tool versions, already pinned in this repo:

| Tool | Version | Purpose | Verified |
|------|---------|---------|----------|
| controller-gen | v0.20.1 | CRD/DeepCopy/RBAC generation | `[VERIFIED: Makefile CONTROLLER_TOOLS_VERSION]` |
| kubebuilder (PROJECT cliVersion) | 4.14.0 | Scaffolding metadata (decorative — see Pitfall below) | `[VERIFIED: PROJECT file]` |
| helmify | pinned via `HELMIFY_VERSION` (Makefile) | `config/crd/` → `charts/tide-crds/` chart generation | `[VERIFIED: Makefile]` |
| envtest K8s version | derived from `go.mod` `k8s.io/api` version | Layer A integration test API server | `[VERIFIED: Makefile ENVTEST_K8S_VERSION derivation]` |

## Architectural Responsibility Map — Detail: Introducing v1alpha3

### Phase 23 precedent (exact commit shape to mirror)

Verified via `git log --oneline --all --grep`, this repo already ran this crank once:

| Plan | What it did | Commits |
|------|-------------|---------|
| 23-01 | Created `api/v1alpha2` package (schema reshape), regenerated CRDs, extended the aggregates guard, added v1alpha2 schema tests | `67cb313`, `6196ba7`, `148dcb1` |
| 23-02 | Ported all webhooks to v1alpha2, registered v1alpha2 scheme, **deleted the no-op `Hub()` conversion stub**, regenerated CRDs (storageversion flip), authored the migration guide | `c5f136e`, `2de10b9`, `f8776a0`, `12fae52` |
| 23-03 | SchemaRevision guard (SCHEMA-03), DEPS-03, global wave label (SCHEMA-02) | — |
| 23-04 | **Full consumer migration**: repointed `api/v1alpha1` imports to v1alpha2 across all consumers, migrated envtest suite, resolved v1alpha2 semantic deltas (Wave ProjectRef, webhook relocation, controller GVKs) | `8ec1dbe`, `3071f70`, `bff8df9` |
| 23-05 | Gap-closure from code review (WR-01..04): scoping fixes, a misleading import-alias rename, CEL self-reference additions | `3496fd4`, `d5790ae`, `e590e7f` |

**Phase 40 must add a sixth step this precedent never needed:** deleting the OLD packages
(`api/v1alpha1/`, `api/v1alpha2/`, `internal/webhook/v1alpha2/`) once v1alpha3 consumers are
migrated. Phase 23 kept v1alpha1 alive for the guard; Phase 40's D-03 (reinstall-only, no
conversion, no surviving-object decode path) removes that need — v1alpha3 is the sole
registered version end to end.

### CRD generation mechanics (verified from `config/crd/bases/tideproject.k8s_projects.yaml`)

- `make manifests` invokes `controller-gen ... paths="./api/..." paths="./internal/controller/..." paths="./internal/webhook/..."` — **no version name is hardcoded**; controller-gen discovers every Go package under `api/` that has `+kubebuilder:object:root=true` types and emits one `versions[]` entry per package into the SAME CRD YAML file (grouped by `Kind`).
- Today's `config/crd/bases/tideproject.k8s_projects.yaml` is 1159 lines and contains exactly
  two `versions[].name` entries: `v1alpha1` (`served: false, storage: false`, lines 1-548) and
  `v1alpha2` (`served: true, storage: true`, lines 549-1159). Deleting `api/v1alpha1/` and
  `api/v1alpha2/` and adding `api/v1alpha3/` will regenerate this file with a SINGLE version
  block — confirms CONTEXT.md's "roughly half their current YAML" claim precisely (≈580 lines,
  not "roughly half of 1159" — it's the size of ONE existing version block).
- `+kubebuilder:storageversion` currently sits on all six v1alpha2 Kinds (verified — this is
  fully consistent unlike the stale `docs/audit/README.md`/`operator.md` claim that only `Plan`
  carries it, which describes the OLD v1alpha1-only state and is itself now wrong — see Common
  Pitfalls).
- **Exactly one version may carry `storage: true` when applied to a live API server.** During
  the transitional window where `api/v1alpha2` and `api/v1alpha3` coexist (before v1alpha2 is
  deleted), the marker must move in the SAME commit/step that regenerates manifests — do not
  leave an intermediate state with two versions both claiming storage (controller-gen will
  happily generate it, but `kubectl apply` of that CRD YAML against a real cluster errors).
- **Chart regeneration is NOT manual.** `charts/tide-crds/templates/*.yaml` are helmify-driven:
  `make helm-crds` runs `kustomize build config/crd | helmify charts/tide-crds` then
  `hack/helm/augment-tide-crds-chart.sh`. After `make manifests` regenerates
  `config/crd/bases/`, run `make helm-crds` (or the full `make helm-controller helm-crds` pair)
  to propagate — do NOT hand-edit `charts/tide-crds/templates/*.yaml`. `make
  verify-chart-reproducible` is the CI gate that catches drift (also invoked in
  `.github/workflows/release.yaml`'s `helmify-verify` job) — run it locally before considering
  a plan done.

### Webhook migration

`internal/webhook/v1alpha2/` is a normal Go package (`package v1alpha2`, imports
`tideprojectv1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"`). The migration is a directory
rename + package rename + import repoint: `internal/webhook/v1alpha2/` →
`internal/webhook/v1alpha3/`, `package v1alpha2` → `package v1alpha3`, and the import alias
repointed to `api/v1alpha3`. `cmd/manager/main.go` wires
`webhookv1alpha2.SetupPlanWebhookWithManager` / `SetupWaveWebhookWithManager` /
`SetupProjectWebhookWithManager` — all three call sites need the alias renamed.
`test/integration/envtest/suite_test.go` registers the same three webhooks for the envtest
manager — same rename needed there.

## Consumer Inventory (v1alpha2 → v1alpha3 re-import)

Verified via `grep -rl 'api/v1alpha2' --include='*.go'` (non-test files; test files add ~65 more
that mechanically follow the same rename):

| Area | Files | Notes |
|------|-------|-------|
| `internal/controller/` | 18 files (project/milestone/phase/plan/task/wave controllers, `dispatch_helpers.go`, `depgraph.go`, `billing_halt.go`, `boundary_push.go`, `budget_blocked.go`, `failure_halt.go`, `import_controller.go`, `import_jobspec.go`, `push_helpers.go`, `reporter_jobspec.go`) | Core reconciler surface — the bulk of the migration |
| `internal/dispatch/podjob/` | 3 files (`backend.go`, `caps.go`, `jobspec.go`) | Contains the D-05 owner-ref dual-accept to simplify |
| `internal/webhook/v1alpha2/` | 5 files | Whole-package rename (see above) |
| `internal/gates/` | 2 files (`boundary.go`, `policy.go`) | `EvaluatePolicy`/`DefaultGates` signatures take `tideprojectv1alpha2.Gates` — **do NOT conflate with the subagent.levels rename; Gates has no off-by-one (see Common Pitfalls)** |
| `internal/budget/` | 3 files | Budget tally helpers typed against `v1alpha2.Project`/`BudgetStatus` |
| `internal/reporter/` | 1 file (`materialize.go`) | Child-CRD materialization, typed against v1alpha2 Kinds |
| `cmd/manager/main.go` | 1 file | Scheme registration + webhook wiring + the duplicate-`AddToScheme` bug (see Common Pitfalls) |
| `cmd/dashboard/` | 1 main + 8 `api/*.go` handlers | Read-only informer-backed REST/SSE handlers |
| `cmd/tide/` | 9 files (`root_flags.go` + subcommands: approve, cancel, resume, reject, tail, watch, describe-budget, artifact-get, export-envelopes, inspect-wave) | CLI typed client scheme registration in `root_flags.go`; subcommands consume typed objects |
| `cmd/tide-import/`, `cmd/tide-reporter/` | 2 files | In-namespace Job images that decode CRDs |

**`api/v1alpha1` consumer count: exactly 1** (`api/v1alpha1/dogfood_manifests_test.go`) — this
matches CONTEXT.md's Established Patterns claim precisely. This test's package (`package
v1alpha1_test`) validates the 3 `examples/projects/dogfood/0*.yaml` fixtures against a
`supportedProjectAPIVersions` allowlist (currently v1alpha1 + v1alpha2). When `api/v1alpha1/` is
deleted, this test file must move to a version-neutral location (e.g. a new
`test/integration/schema/` or alongside the fixtures) and its allowlist collapse to v1alpha3
only. **Do not just delete this test** — it is the only automated check that the dogfood
fixtures actually strict-decode against a real schema; losing it silently removes fixture
validation coverage.

## Envelope Decoupling Surface (D-08)

Exact touch points, verified by grep:

| File:Line | Current value | Mechanism |
|-----------|---------------|-----------|
| `pkg/dispatch/envelope.go:24` | `const APIVersionV1Alpha1 = "tideproject.k8s/v1alpha1"` | Single source of truth for `ValidateAPIVersionKind`; changing this constant propagates automatically to every consumer that imports it |
| `pkg/dispatch/doc.go:23-32` | Package doc claims envelope JSON carries `apiVersion: tideproject.k8s/v1alpha1` and documents a **planned v1beta1 bump path** ("Future breaking changes... ride a v1beta1 apiVersion bump via the same hub/spoke conversion path the CRDs use") | **This is the exact plan D-08 supersedes** — the doc comment must be rewritten to describe group decoupling (`dispatch.tideproject.k8s/v1alpha1`), not a version bump within the CRD group |
| `cmd/tide-push/main.go:116` | `envelopeAPIVersion = "tideproject.k8s/v1alpha1"` (local const, does NOT import `pkg/dispatch`) | Independent literal — must be updated by hand, will not follow the `pkg/dispatch` constant change |
| `cmd/tide-eval/main.go:83` | `APIVersion: "tideproject.k8s/v1alpha1"` (literal, even though the file DOES import `pkgdispatch` at line 61 and could reference `pkgdispatch.APIVersionV1Alpha1`) | **This is itself a drift wart** — `tide-eval` already imports the package that owns the constant but hardcodes a duplicate literal instead. Fixing it to reference the constant during this crank removes a future drift vector "for free." |
| `internal/subagent/`, `cmd/claude-subagent/`, `cmd/stub-subagent/` | No literal `tideproject.k8s/v1alpha1` string found by grep | These consume the constant programmatically via `pkg/dispatch.APIVersionV1Alpha1` — confirms the bump propagates to them automatically once the constant changes |

**kubeadm precedent (cited in CONTEXT.md):** `kubeadm.k8s.io/v1beta4` is kubeadm's own
non-served config-file API group, entirely separate from any core K8s API group. The same
pattern applies here: `dispatch.tideproject.k8s/v1alpha1` is a document contract for
subagent-image authors, not a K8s API version — decoupling the group prevents exactly the
"one crank bumps two unrelated things" collision the current doc.go plan would cause.

## v1alpha2 Schema Wart Inventory (D-09 — ASK-FIRST)

Per D-09, this is a menu for the user to pick from at plan approval — **do not silently batch
any of these.** Ordered by confidence/impact:

| # | Wart | Evidence | Fix | Blast radius |
|---|------|----------|-----|---------------|
| W1 | `ProjectSpec.ModelSelection` field is dead code | `grep -rn "ModelSelection"` across the whole repo (excluding `api/`) returns **zero hits** — no controller reads it. It duplicates `Subagent.Levels` (`LevelOverrides`), which IS wired. | Drop `ModelSelection`/`ModelSelection` struct entirely from v1alpha3; document removal in the migration guide (any operator manifest setting `spec.modelSelection` silently no-ops today and would need to move to `spec.subagent.levels`) | Small — no code reads the field; only a schema/doc removal. Zero controller changes. |
| W2 | `subagent.levels` semantic rename (folded todo) | Confirmed via all 4 dispatch call sites (see Code Examples below) — this is the phase's headline change, not really "optional," but scoped here for completeness of the D-09 menu | Already locked (D-02) — not actually optional, listed for completeness | The phase's core scope |
| W3 | `PlanAdmissionConfig.FileTouchMode` uses a bare string enum (`strict`/`warn`) instead of a typed constant like `FailureProfileType` | `api/v1alpha2/project_types.go:131-138` — `FileTouchMode string` vs. the more idiomatic `FailureProfileType string` pattern used elsewhere in the same file | Cosmetic naming-consistency fix; low value, mention but do not recommend batching | Trivial if taken; skippable |
| W4 | No CEL self-reference/empty-string guard on `Wave.Spec` | Every other Kind with a `DependsOn` slice (Phase, Milestone, Plan, Task) has a `!self.exists(d, d == '')` + self-reference XValidation pair (added incrementally, culminating in 23-05's WR-04). `Wave` has no `DependsOn` field at all, so this is **not actually a gap** — confirmed by reading `wave_types.go` in full. | No action — false positive, included to show the audit was exhaustive | N/A |
| W5 | Duplicate `+kubebuilder:validation:Enum=strict;conservative` marker appears on both the `FailureProfileType` type declaration (`shared_types.go:317`) AND the field usage site (`project_types.go:402`) | Confirmed via grep; harmless (controller-gen accepts either or both) but slightly redundant | No action needed — not a functional wart, informational only | None |

**Recommendation to present to the user at plan approval:** only **W1 (drop dead
`ModelSelection`)** is a real, low-risk, high-clarity batchable fix. W2 is already locked scope.
W3/W4/W5 are not worth the churn (W4 is a false positive; W3/W5 are cosmetic). Ask the user: "Drop
the dead `ModelSelection` field as part of the v1alpha3 crank? (zero functional impact, removes
a confusing unused knob from the schema)."

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Multi-version CRD generation | Hand-written CRD YAML version blocks | `controller-gen` via `make manifests` (already directory-convention-driven, zero hardcoding) | Hand-editing `config/crd/bases/*.yaml` guarantees drift from the Go types; controller-gen is already wired and free |
| Chart CRD template sync | Hand-editing `charts/tide-crds/templates/*.yaml` to match new CRD YAML | `make helm-crds` (kustomize + helmify + `augment-tide-crds-chart.sh`) | The chart is helmify-generated; `make verify-chart-reproducible` in CI will catch and reject any hand-edit drift |
| Storage-version transition safety | A custom pre-flight script checking exactly-one-storage-version | `+kubebuilder:storageversion` marker discipline (one per Kind, moved in the same regen step) | K8s API server itself enforces "exactly one storage version" at CRD-apply time; a custom checker is redundant with a mechanism the platform already provides |
| Reinstall migration tooling | A conversion webhook, storedVersions-prune script, or preflight migration checker | The existing `docs/migration/*.md` reinstall recipe pattern (delete + reapply with `schemaRevision` set) | D-03 explicitly rejects conversion machinery — Phase 23's D-09 already established this precedent and it shipped fine |

**Key insight:** every piece of "migration machinery" this phase might be tempted to build
(conversion webhooks, stored-version pruning, custom storage-version validators) has already
been evaluated and rejected once in this exact repo (Phase 23 D-09). The only genuinely new
mechanism this phase adds is generalizing the SchemaRevision guard's revision-string parameter —
everything else is repetition of an established, working pattern.

## Docs/Samples Sweep Inventory

Beyond the exact list CONTEXT.md's canonical_refs already names
(`docs/INSTALL.md:210`, `docs/gates.md:14`, `docs/git-hosts.md:68`,
`docs/project-authoring.md:5,132,210,320`, `config/samples/tide_v1alpha1_*.yaml` × 11), grep
across the whole repo surfaced additional files carrying `v1alpha1`/`v1alpha2`:

| File | What it carries | Recommendation |
|------|------------------|-----------------|
| `README.md:108` | Full `apiVersion: tideproject.k8s/v1alpha1` Project example — **broken today** (v1alpha1 not served) | In scope — bump to v1alpha3, same as D-06's named docs |
| `config/samples/kustomization.yaml` | Comment block + `resources:` list referencing all 11 `tide_v1alpha1_*.yaml` filenames | Must be updated in lockstep with the D-06 sample-file renames — easy to miss since it's a kustomization manifest, not prose |
| `examples/projects/dogfood/02-codex-runtime-project.yaml` | Already-converted-once example: `apiVersion: tideproject.k8s/v1alpha2`, `schemaRevision: v1alpha2`, extensive prose comments about the Phase 23 migration | Needs a SECOND conversion pass to v1alpha3 (this file has already been through one migration cycle — good real-world precedent for what the migration-guide's example should look like) |
| `test/e2e/testdata/live-claude-project.yaml` | `apiVersion: tideproject.k8s/v1alpha2` + a comment claiming sync with `api/v1alpha1/project_types.go` (stale — should reference v1alpha2 today, will need v1alpha3 after this phase) | In scope — used by the live-Claude e2e suite |
| `PROJECT` (kubebuilder metadata, repo root) | All 6 `resources[].api.path` entries still point at `github.com/jsquirrelz/tide/api/v1alpha1` and `version: v1alpha1`; `Plan` entry still claims `webhooks: {conversion: true, validation: true}` | **Never updated in Phase 23** — decorative/non-functional (does not affect `make manifests`/`make generate`, which use explicit `paths=` flags, not PROJECT-file-driven scaffolding), but cheap to fix in the same pass and removes a misleading artifact for future `kubebuilder edit`/plugin invocations |
| `Makefile:553-555` (`verify-no-aggregates`) | Hardcoded glob `api/v1alpha1/*_types.go api/v1alpha2/*_types.go` | **Functional bug if left unfixed** — after both directories are deleted, this CI-gated target (`ci.yaml:60`) silently stops checking anything (`|| true` swallows the "no such file" grep error). MUST update to `api/v1alpha3/*_types.go` as part of the removal step, not left for later cleanup. |
| `SECURITY.md:40` | "`api/v1alpha1/*` schemas, CEL validators, **conversion webhook (no-op for v1)**" | **Pre-existing staleness, NOT caused by this phase** — Phase 23 already retired the conversion webhook entirely (deleted the `Hub()` stub in commit `c5f136e`); this line has been wrong since Phase 23 shipped. Flag to user as bonus scope, since it wasn't in CONTEXT.md's named list. |
| `docs/rbac.md:210-224` | Section header "Conversion webhook (D-X7 — no-op for v1.0)" describing scaffolding that "IS scaffolded but no-op" | Same pre-existing staleness as SECURITY.md — describes a mechanism removed in Phase 23. Bonus scope. |
| `docs/audit/README.md`, `docs/audit/operator.md` | Extensive `api/v1alpha1/*.go:LINE` citations from a point-in-time security/architecture audit (predates Phase 23 entirely — cites `internal/webhook/v1alpha1/plan_webhook.go`, which no longer exists) | **Recommend leaving alone** — these are dated audit *snapshots*, not living docs; treating them as historical record (like a git blame) is more honest than silently rewriting citations to make them look current when the underlying audit was never re-run. Flag as an Open Question for the user rather than silently including or excluding. |
| `AGENTS.md:86,245` | `kubebuilder create api --group example.com --version v1alpha1 --kind Memcached` | **Out of scope — false positive.** This is generic kubebuilder-tutorial boilerplate (verified: the surrounding doc is a project-agnostic "AI Agent Guide" to kubebuilder itself, using `example.com`/`Memcached` as placeholder names, not TIDE's own API group) |

## Common Pitfalls

### Pitfall 1: Conflating `subagent.levels` rename with `Gates` per-level policy
**What goes wrong:** A planner sees `Gates{Milestone, Phase, Plan, Task}` alongside
`LevelOverrides{Milestone, Phase, Plan, Task}` (same field names!) and assumes both need the
same off-by-one shift.
**Why it happens:** The two structs share field names by coincidence of vocabulary
(milestone/phase/plan/task), but `Gates.Milestone` gates the **Milestone CR's own** boundary —
`internal/gates/policy.go`'s `EvaluatePolicy(g, "milestone")` is called BY the
`MilestoneReconciler` about itself, with no parent-child indirection. There is no off-by-one
bug in `Gates` — it was never wired the way `subagent.levels` was.
**How to avoid:** Verified via reading `internal/gates/policy.go` in full — `EvaluatePolicy`'s
switch matches the CURRENT resource's own level, not "who authors my children." Do not touch
`Gates`, `DefaultGates()`, or `EvaluatePolicy` as part of this rename.
**Warning signs:** Any plan task that touches `internal/gates/*.go` under the banner of "the
levels rename" should be treated as scope creep and challenged.

### Pitfall 2: Missing the third rename call site (`podjob.BuildOptions.Level`)
**What goes wrong:** Fixing `BuildPlannerEnvelope("project", ...)` and
`resolveImage(project, "project", ...)` but missing the THIRD site,
`podjob.BuildOptions{Level: "project", ...}` at `project_controller.go:1241`, leaves the K8s Job
label (`tideproject.k8s/level=project`) and the `PlannerJobName(opts.Level, ...)` naming
inconsistent with the renamed envelope `Level` field.
**Why it happens:** All three sites independently hardcode the literal string; there is no
single source of truth today (this IS one of the "naming inconsistencies" D-09 asked to
surface).
**How to avoid:** Grep for the literal `"project"` string near each of the 4 planner dispatch
functions before considering a controller "done" — confirmed exactly 3 sites per reconciler
(`BuildPlannerEnvelope` level arg, `resolveImage`/`ResolveProvider` level arg, `podjob.BuildOptions.Level`).
**Warning signs:** `podjob.BuildOptions.Level`'s own doc comment already states the expected
enum is `"milestone"|"phase"|"plan"|"task"` — `"project"` was already out-of-spec before this
phase; a grep for the literal `"project"` as a bare Level value is the fastest verification.

### Pitfall 3: Hardcoded-by-version-name build tooling silently going dark
**What goes wrong:** `make verify-no-aggregates` globs `api/v1alpha1/*_types.go
api/v1alpha2/*_types.go` directly. Once those directories are deleted, bash's default glob
behavior leaves the unmatched pattern as a literal string, `grep` errors "No such file," and the
recipe's `|| true` swallows that error — the CI-gated target (`ci.yaml:60`) reports success
having checked **nothing**.
**Why it happens:** The target was written when only two version directories existed and was
never designed to auto-discover new ones.
**How to avoid:** Update the glob to `api/v1alpha3/*_types.go` (or, more durably, make the
glob pattern `api/v1alpha*/*_types.go` so future crank phases don't need to touch this target
again — worth flagging as an option to the user, since it directly serves D-04's "generalize
the crank mechanism" spirit).
**Warning signs:** A green `make verify-no-aggregates` run immediately after deleting
`api/v1alpha1`/`api/v1alpha2` but before updating this target is a false negative — confirm
by temporarily inserting a known-bad string (e.g. `Schedule`) into `api/v1alpha3/*_types.go`
and confirming the target actually fails.

### Pitfall 4: `tide-eval`'s pre-existing literal/constant drift
**What goes wrong:** `cmd/tide-eval/main.go` imports `pkg/dispatch` (line 61) yet still
hardcodes `APIVersion: "tideproject.k8s/v1alpha1"` as a literal (line 83) instead of referencing
`pkgdispatch.APIVersionV1Alpha1`. This is a real, currently-live drift risk (if the constant
changes and this literal doesn't, `tide-eval` silently emits stale-tagged envelopes) —
independent of whether D-08's group-decoupling changes the string's shape.
**Why it happens:** Historical — the literal was probably copy-pasted from `cmd/tide-push`
(which has a legitimate reason to duplicate: it doesn't import `pkg/dispatch` at all).
**How to avoid:** While touching this file for the D-08 envelope-version bump anyway, replace
the literal with `pkgdispatch.APIVersionV1Alpha1` (or whatever the new decoupled constant is
named) so future cranks don't need to touch `tide-eval` by hand again.
**Warning signs:** Any grep-and-replace pass that only updates the STRING VALUE without asking
"could this reference the constant instead" perpetuates the drift risk one more cycle.

### Pitfall 5: `providerfirewall`/`metriccardinality` analyzers do not cover this crank's risk surfaces
**What goes wrong:** Assuming the existing custom analyzers (`tools/analyzers/providerfirewall`,
`tools/analyzers/metriccardinality`) will catch a regression if this crank accidentally
introduces an `api/v1alpha3` import into `cmd/credproxy` (violating the "credproxy MUST NOT
import api/" design principle cited in CONTEXT.md's canonical_refs).
**Why it happens:** `providerfirewall`'s `forbiddenScopes` list only covers
`pkg/controller`, `pkg/dispatch`, `pkg/dag`, `internal/controller`, `internal/webhook`,
`internal/dispatch` — and explicitly EXCLUDES `cmd/credproxy` from its firewalled scope (by
design — credproxy legitimately proxies to the LLM vendor and needs network/HTTP freedom).
There is **no analyzer today that would fire if `cmd/credproxy` imported `api/v1alpha3`** — the
"credproxy must not import api/" rule is currently a design convention with zero automated
enforcement, verified by reading `forbiddenScopes` in full and confirming `cmd/credproxy/*.go`
has zero `api/` imports today (grep-confirmed).
**How to avoid:** Do not introduce any `api/v1alpha3` import into `cmd/credproxy/` during the
crank (there is no reason to — credproxy proxies HTTP, it has no CRD-typed dependency need).
Flag to the user as a possible follow-up hardening item (extending `providerfirewall`'s scope
list to also reject `api/` imports in `cmd/credproxy`) — out of this phase's decided scope, but
a legitimate D-09-adjacent finding worth a mention.
**Warning signs:** If a future task adds any `tideprojectv1alpha3` typed reference to
`cmd/credproxy/*.go`, nothing in CI will fail — manual review is the only backstop today.

## Code Examples

### The exact rename edits (D-02 / folded todo)

Verified current call sites (all four planner-dispatch reconcilers), showing the exact
before/after per the todo's DECIDED mapping table:

```go
// project_controller.go:1214, 1241, 1246 — authors MILESTONE.md
// BEFORE: level string "project" (undocumented — not in podjob.BuildOptions.Level's own enum comment)
_, envInJSON, err := BuildPlannerEnvelope("project", project, project, attempt, "", project.Spec.OutcomePrompt, ...)
// ...
Level: "project",
// ...
SubagentImage: resolveImage(project, "project", r.HelmProviderDefaults),

// AFTER: shifts to "milestone" (levels.milestone = "authors MILESTONE.md")
_, envInJSON, err := BuildPlannerEnvelope("milestone", project, project, attempt, "", project.Spec.OutcomePrompt, ...)
// ...
Level: "milestone",
// ...
SubagentImage: resolveImage(project, "milestone", r.HelmProviderDefaults),
```

```go
// milestone_controller.go:450 — authors phase briefs
// BEFORE:
_, envInJSON, err := BuildPlannerEnvelope("milestone", ms, project, attempt, "", plannerPrompt, ...)
// AFTER: shifts to "phase" (levels.phase = "authors phase briefs")
_, envInJSON, err := BuildPlannerEnvelope("phase", ms, project, attempt, "", plannerPrompt, ...)
```

```go
// phase_controller.go:413 — authors PLAN.md
// BEFORE:
_, envInJSON, err := BuildPlannerEnvelope("phase", ph, project, attempt, "", plannerPrompt, ...)
// AFTER: shifts to "plan" (levels.plan = "authors PLAN.md AND the task DAG" — collapses with plan_controller.go below)
_, envInJSON, err := BuildPlannerEnvelope("plan", ph, project, attempt, "", plannerPrompt, ...)
```

```go
// plan_controller.go:440 — authors the task DAG
// BEFORE and AFTER are IDENTICAL — this call site already passes "plan":
_, envInJSON, err := BuildPlannerEnvelope("plan", plan, project, attempt, "", plannerPrompt, ...)
// No functional change to this line. What changes is its MEANING: after the rename,
// this dispatch and phase_controller.go's dispatch (above) now BOTH resolve through
// project.Spec.Subagent.Levels.Plan — the same override slot, serving two different
// physical dispatch sites. This collapse is the exact design point CONTEXT.md left as
// Claude's Discretion ("whether folding PLAN.md + task-DAG under one levels.plan key is
// right, or the task DAG deserves its own key").
```

```go
// task_controller.go:1485 — task execution (already correct, no rename)
Level: "task",  // unchanged — levels.task was never off-by-one
```

**Sites that must NOT be touched by this rename** (verified distinct meaning, same vocabulary):
- `internal/controller/push_helpers.go:402` `buildCommitMessage(boundary, name)` — `"project"`/
  `"milestone"`/`"phase"`/`"plan"` here mean "which git-commit boundary," not "who plans this."
- `internal/gates/policy.go` `EvaluatePolicy` / `DefaultGates` — see Pitfall 1.
- `internal/controller/project_controller.go:846` `buildCommitMessage("project", "")` — same
  git-boundary concept, not a planner-dispatch level.

### Generalizing the SchemaRevision guard (D-04)

Current implementation (`internal/controller/project_controller.go:1662-1704`) hardcodes both
the expected revision string and the message text inline:

```go
func (r *ProjectReconciler) checkSchemaRevisionGuard(
    ctx context.Context,
    project *tidev1alpha2.Project,
) (blocked bool, err error) {
    if project.Spec.SchemaRevision == "v1alpha2" {
        return false, nil
    }
    // ... sets RequiresReinstall condition referencing "v1alpha2" + the v1alpha1-to-v1alpha2 doc path
}
```

Generalize by parameterizing the expected revision and doc-path (constants, not struct fields —
this stays a compile-time crank mechanism per D-04, not a runtime-configurable one):

```go
const (
    expectedSchemaRevision = "v1alpha3"
    migrationGuideDocPath  = "docs/migration/v1alpha2-to-v1alpha3.md"
)

func (r *ProjectReconciler) checkSchemaRevisionGuard(
    ctx context.Context,
    project *tidev1alpha3.Project,
) (blocked bool, err error) {
    if project.Spec.SchemaRevision == expectedSchemaRevision {
        return false, nil
    }
    // message references expectedSchemaRevision + migrationGuideDocPath — a future
    // v1alpha4 crank changes exactly these two constants and nothing else in this function.
}
```

### Owner-ref simplification (D-05)

Both sites currently accept two GroupVersions:

```go
// task_controller.go:1205, internal/dispatch/podjob/backend.go:377 (identical pattern)
if ref.Kind == "Project" && (ref.APIVersion == "tideproject.k8s/v1alpha1" || ref.APIVersion == tideprojectv1alpha2.GroupVersion.String()) {
```

Simplify to the current GroupVersion only (reinstall-only migration means stale refs cannot
exist post-crank):

```go
if ref.Kind == "Project" && ref.APIVersion == tideprojectv1alpha3.GroupVersion.String() {
```

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (Layer A: envtest; Layer B: kind) + plain `go test` for contract tests |
| Config file | none — `make test-int` orchestrates both layers directly |
| Quick run command | `make test-int-fast` (Layer A / envtest only, ~90s) |
| Full suite command | `make test-int` (Layer A + Layer B kind, ~20-40min with `KIND_GO_TEST_TIMEOUT=40m`) |

### Phase Requirements → Test Map

Requirement IDs are TBD (see `<phase_requirements>`); this maps by work area instead.

| Work area | Behavior | Test Type | Automated Command | File Exists? |
|-----------|----------|-----------|--------------------|--------------|
| SCHEMA | v1alpha3 CRD generates correctly, single storage version | unit (schema_test.go pattern) | `go test ./api/v1alpha3/... -run TestSchema` | ❌ Wave 0 — new file, mirror `api/v1alpha2/schema_test.go` |
| SCHEMA | `make manifests` produces single-version CRD YAML | build-check | `make manifests && grep -c 'name: v1alpha' config/crd/bases/tideproject.k8s_projects.yaml` (expect 1) | ✅ mechanism exists, no new test file needed — a plan verification step, not a Ginkgo spec |
| RENAME | Each of the 4 planner dispatch sites resolves through the correct `Levels.*` slot post-rename | integration (envtest) | `go test ./test/integration/envtest/... -run TestPlannerDispatch` (extend existing `planner_dispatch_test.go`) | ⚠️ File exists but needs new assertions — not a Wave 0 gap, a task-level addition |
| REMOVAL | Zero `v1alpha1`/`v1alpha2` references outside `docs/migration/` and `.planning/` | grep-based smoke check | `grep -rl 'v1alpha1\|v1alpha2' --include='*.go' --include='*.yaml' . \| grep -v docs/migration \| grep -v .planning` (expect empty) | ✅ mechanism is a shell one-liner, not a Ginkgo spec — put it in a plan's verification step |
| GUARD | `checkSchemaRevisionGuard` rejects wrong/absent revision, accepts v1alpha3 | unit/integration | existing `project_controller_v2_guard_test.go` pattern — rename/extend for v1alpha3 | ✅ exists (as the v1alpha1→v1alpha2 guard test); extend, don't replace |
| OWNERREF | Simplified owner-ref check still resolves Project via owner chain | unit | existing `task_controller_test.go` / `backend_test.go` coverage | ✅ exists — verify assertions still target the simplified single-GroupVersion check |
| ENVELOPE | `dispatch.tideproject.k8s/v1alpha1` validates correctly; old group string rejected | unit | `pkg/dispatch` has no `_test.go` file listed in current inventory for `envelope.go` specifically — check for one at plan time | ⚠️ verify at plan time whether `pkg/dispatch/envelope_test.go` exists; if not, Wave 0 gap |
| VERIFY | `spec_conformance_test.go` and dogfood-manifest test pass against v1alpha3 | integration | `go test ./test/integration/envtest/... -run TestSpecConformance` | ✅ exists, needs `SchemaRevision: "v1alpha2"` literals (2 occurrences found) bumped to `"v1alpha3"` |
| VERIFY | Relocated dogfood-manifest schema test | integration | new location TBD (see Consumer Inventory) | ❌ Wave 0 — must be created as part of the `api/v1alpha1` removal step, not an afterthought |

### Sampling Rate

- **Per task commit:** `make test-int-fast` (Layer A only — fast enough for per-task iteration)
- **Per wave merge:** `make test-int` (full Layer A + Layer B)
- **Phase gate:** `make test-int` green (with the grep-based zero-reference smoke check above)
  + `make verify-chart-reproducible` + `make verify-no-aggregates` (post-fix) + `make
  verify-dispatch-imports` before `/gsd:verify-work`

### Wave 0 Gaps

- [ ] `api/v1alpha3/schema_test.go` — new schema package needs its own test file (mirror `api/v1alpha2/schema_test.go`)
- [ ] Relocated dogfood-manifest schema test (replaces `api/v1alpha1/dogfood_manifests_test.go`, which dies with package removal) — new location + v1alpha3-only allowlist
- [ ] Verify whether `pkg/dispatch/envelope_test.go` exists; if not, add one asserting `ValidateAPIVersionKind` against the new decoupled group string
- [ ] `verify-no-aggregates` Makefile target glob update (not a test file, but a Wave 0-equivalent build-tooling fix — must land before/with the removal step, not after)

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-------------------|
| V2 Authentication | No | Unchanged by this phase — no auth surface touched |
| V3 Session Management | No | N/A |
| V4 Access Control | Yes | RBAC ClusterRoles are generated from `+kubebuilder:rbac` markers in `internal/controller/` — unaffected by the version rename itself, but the `docs/rbac.md` conversion-webhook staleness (Pitfall/Docs sweep) is adjacent to this category and worth fixing in the same pass if the user takes the bonus scope |
| V5 Input Validation | Yes | CEL `+kubebuilder:validation:XValidation` rules carry forward verbatim from v1alpha2 to v1alpha3 (copy, don't hand-roll); the `SchemaRevision` fail-closed guard IS an input-validation control (rejects malformed/stale schema objects at reconcile time, not just admission time) |
| V6 Cryptography | No | Envelope HMAC signing (`credproxy.Sign`) is untouched by the group-decoupling — the envelope's `apiVersion` field is a discriminator string, not a cryptographic input |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|----------------------|
| Stale-schema object silently running with wrong semantics after a breaking rename | Tampering / Information Disclosure (an operator's Project silently misbehaves with no error) | `checkSchemaRevisionGuard` fail-closed pattern (D-04) — already the existing mitigation, just needs generalizing |
| Import-boundary erosion allowing a trust-sensitive component (credproxy) to gain an unnecessary dependency on CRD-typed internals | Elevation of Privilege (increases the blast radius if credproxy is ever compromised) | Currently a design convention only (see Pitfall 5) — no code changes needed this phase, but worth flagging as a not-currently-enforced gap |
| Envelope contract drift between the constant (`pkg/dispatch`) and independent literal copies (`cmd/tide-push`, `cmd/tide-eval`) causing a subagent image to silently accept/reject the wrong envelope version | Tampering (a stale image could process envelopes it shouldn't) | `ValidateAPIVersionKind` is the existing mitigation (every consumer MUST call it before touching other fields) — this phase's job is to make sure all 3 literal-string copies move together, not to add new validation logic |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `api/v1alpha3` will be structured as a direct copy of `api/v1alpha2` (per Phase 23 precedent), not kubebuilder-scaffolded fresh | Architecture Patterns | Low — this is explicitly left as Claude's Discretion in CONTEXT.md; if the planner chooses the scaffold-fresh path instead, the PROJECT-file staleness finding becomes moot (a fresh scaffold updates PROJECT) but the copy-based consumer-migration inventory above stays equally valid either way |
| A2 | The `levels.plan` key collapsing both `phase_controller.go`'s and `plan_controller.go`'s dispatches into one override slot is acceptable (vs. giving the task-DAG dispatch its own key) | Code Examples | Medium — CONTEXT.md explicitly flags this as an open design point ("Design points to settle at planning time" in the folded todo); if the user wants a 5th key instead of collapsing, the schema (not just the call sites) changes, and `LevelOverrides` gains a new field |
| A3 | `SECURITY.md`/`docs/rbac.md`/`docs/audit/*.md` staleness is OUT of this phase's locked scope (bonus, ask-first) rather than silently included under D-07's "sweep the stale comments" | Docs/Samples Sweep Inventory | Low-Medium — if the user considers these in scope, the phase's docs workload grows; recommend asking rather than guessing, since CONTEXT.md's canonical_refs list is specific and didn't name these files |

**None of the above are compliance/retention/security-standard claims requiring elevated
scrutiny — all three are scope-boundary judgment calls, not factual claims about libraries or
versions.**

## Open Questions (RESOLVED)

1. **RESOLVED — 40-CONTEXT.md D-12 (user decision 2026-07-06): docs/audit/*.md stay untouched as
   dated historical records; only living docs (SECURITY.md, docs/rbac.md) fold into the D-06 sweep.**
   **Should `docs/audit/README.md` and `docs/audit/operator.md` be updated, left as historical
   snapshots, or explicitly annotated as stale?**
   - What we know: both cite `api/v1alpha1/*.go:LINE` extensively and reference
     `internal/webhook/v1alpha1/plan_webhook.go`, which no longer exists (webhooks moved to
     v1alpha2 in Phase 23, well before this phase).
   - What's unclear: whether these are meant to be living documents (updated per crank) or a
     point-in-time audit record (never touched again, like a git tag).
   - Recommendation: ask the user at plan approval; default recommendation is to add a one-line
     header note ("This audit reflects the pre-Phase-23 v1alpha1-only codebase and is not
     maintained") rather than either silently rewriting citations or silently ignoring them.

2. **Should the `verify-no-aggregates` glob be hardened to `api/v1alpha*/*_types.go` (durable
   across future cranks) or literally updated to `api/v1alpha3/*_types.go` (matches D-04's
   "generalize the crank mechanism" spirit less completely, but is a smaller diff)?**
   **RESOLVED — 40-CONTEXT.md D-12 mandatory-scope note (user-confirmed 2026-07-06): harden to the
   version-agnostic `api/v1alpha*` glob, in the SAME commit as the api/ package deletions
   (implemented by plan 40-05).**
   - What we know: D-04 explicitly asks to generalize the SchemaRevision guard as "the
     permanent crank mechanism" — this Makefile target has the identical hardcoding problem but
     wasn't named in CONTEXT.md's decisions.
   - What's unclear: whether the user considers Makefile-tooling generalization in scope for
     "the permanent crank mechanism" or wants it treated as a separate, narrower fix each time.
   - Recommendation: propose the glob (`api/v1alpha*/*_types.go`) at plan approval as a
     low-cost durability improvement; either answer is fine, but it should be a stated choice,
     not an oversight.

## Environment Availability

This phase has no new external tool/service dependencies — it operates entirely on tools already
verified present and pinned in this repo (controller-gen, kustomize, helmify, envtest, kind,
Docker). Skipping the full audit table as a result; nothing here changes from any other phase in
this codebase.

## Sources

### Primary (HIGH confidence — direct repo inspection)
- `.planning/phases/40-deprecate-v1alpha1-api/40-CONTEXT.md` — locked decisions, canonical refs
- `docs/migration/v1alpha1-to-v1alpha2.md` — migration-doc structural template
- `git log --oneline --all --grep` against Phase 23 commits (`67cb313`..`c3191d7`) — exact
  precedent commit shape
- `Makefile` (manifests/generate/helm-crds/verify-* targets) — build mechanics
- `config/crd/bases/tideproject.k8s_projects.yaml`, `charts/tide-crds/templates/project-crd.yaml`
  — CRD generation shape, line counts, version block structure
- `api/v1alpha2/*.go`, `internal/controller/{project,milestone,phase,task}_controller.go`,
  `internal/controller/dispatch_helpers.go`, `internal/gates/policy.go`,
  `internal/dispatch/podjob/{backend,jobspec}.go`, `pkg/dispatch/{envelope,doc}.go` — exact
  code sites for D-02/D-04/D-05/D-08
- `PROJECT` (kubebuilder metadata), `charts/tide/values.yaml`, `SECURITY.md`, `docs/rbac.md`,
  `docs/audit/{README,operator}.md`, `README.md`, `AGENTS.md`, `config/samples/kustomization.yaml`,
  `examples/projects/dogfood/*.yaml`, `test/e2e/testdata/live-claude-project.yaml` — full-repo
  grep sweep for `v1alpha1`/`v1alpha2` references

### Secondary (MEDIUM confidence)
- None — every claim in this document was verified directly against this repository's own
  source, git history, or CI configuration. There was no need to consult external
  documentation: kubebuilder/controller-gen mechanics were verified empirically against this
  repo's existing generated artifacts rather than against upstream docs.

### Tertiary (LOW confidence)
- None.

## Metadata

**Confidence breakdown:**
- Standard stack: N/A — no new packages
- Architecture / mechanics: HIGH — every claim verified against this repo's own generated CRD
  YAML, Makefile targets, and Phase 23's actual commit history (not against training-data
  assumptions about kubebuilder)
- Consumer inventory: HIGH — exhaustive `grep -rl` counts, not estimates
- Wart inventory (D-09): HIGH for W1 (ModelSelection dead-code, whole-repo-grep-confirmed) and
  W4 (false-positive, confirmed by reading the type in full); MEDIUM for W3/W5 (subjective
  "worth batching" judgment calls, not factual claims)
- Security domain: MEDIUM — ASVS category mapping is straightforward given this phase's narrow
  code-only scope, but the "credproxy import firewall has no enforced gate" finding (Pitfall 5)
  is a genuine gap this research surfaced rather than a pre-existing documented control

**Research date:** 2026-07-06
**Valid until:** Should remain valid for the lifetime of this phase's execution (no
fast-moving external dependencies); re-verify the consumer-file-count tables if significant
unrelated commits land on `main` before this phase is planned/executed, since new v1alpha2
imports could appear in the interim.
