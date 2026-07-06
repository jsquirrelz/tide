# Phase 40: Deprecate v1alpha1 API - Context

**Gathered:** 2026-07-06
**Status:** Ready for planning

<domain>
## Phase Boundary

Phase 40 is a **full API-version lifecycle turn**, not a soft deprecation. It delivers:

1. **v1alpha3 introduced** as a new API version carrying the already-DECIDED `subagent.levels` semantic rename (each `levels.X` key = "level X is planned by this model"), plus any batchable schema fixes the user approves at plan time.
2. **v1alpha1 AND v1alpha2 removed** — Go packages, CRD version blocks, chart copies, scheme registrations, stale comments, docs, and samples. End state: v1alpha3 is the **sole served + storage version** for all 6 CRDs.
3. **Envelope contract decoupled** from the CRD group: `tideproject.k8s/v1alpha1` → `dispatch.tideproject.k8s/v1alpha1`.
4. **Version-crank machinery generalized** (SchemaRevision guard) so v1alpha4 someday is mechanical.

Context that shaped scope: the operator is currently TIDE's only user, so no deprecation grace period, no conversion machinery, no multi-release soft-landing is needed. v1alpha1 is ALREADY `served: false, storage: false` (Phase 23) — "deprecation" in the K8s sense already happened; this phase is removal plus the next crank.

</domain>

<decisions>
## Implementation Decisions

### Scope shape
- **D-01: Full lifecycle turn.** Introduce v1alpha3, migrate all consumers, then remove v1alpha1 and v1alpha2 within this phase ("remove v1alpha2 as well once v1alpha3 is ready" — sequencing inside the phase: v1alpha3 lands first, removals follow). No `deprecated: true` marker period — single-user reality makes it dead weight.
- **D-02: The `subagent.levels` rename is folded in** (supersedes the 2026-07-03 "own milestone" routing). The rename's target semantics are ALREADY DECIDED in the todo — do not re-litigate: each `levels.X` key means "level X is planned by this model"; do NOT add a `levels.project` key; do NOT keep the current dispatching-CR semantics. The todo's target mapping table (spec key → dispatch surface) is the authority.

### Migration / upgrade path
- **D-03: Reinstall-only.** Consistent with Phase 23 D-09 (conversion webhook retired). No storedVersions prune recipes, no preflight automation, no conversion webhooks. Migration doc instructs: delete CRDs + re-apply Projects under v1alpha3 with `schemaRevision: v1alpha3`. The kind-tide-dogfood cluster (still pre-Spring-Tide v1alpha1) gets rebuilt, not upgraded — build zero compatibility for it.

### Runtime guards
- **D-04: Generalize the SchemaRevision guard; it is the permanent crank mechanism.** `checkSchemaRevisionGuard` (project_controller.go) now expects `schemaRevision: v1alpha3`; parameterize the expected revision + message text so a future v1alpha4 crank is a one-line change. Keep it fail-closed (Ready=False / RequiresReinstall / TerminalError).
- **D-05: Drop the owner-ref dual-accepts.** `task_controller.go:1205` and `internal/dispatch/podjob/backend.go:377` currently accept `tideproject.k8s/v1alpha1 || v1alpha2` owner refs. Under reinstall-only migration, stale refs cannot exist — simplify to the current GroupVersion only.

### Docs & samples
- **D-06: Deep accuracy pass, not a mechanical bump.** All user-facing docs land on v1alpha3: `docs/INSTALL.md` (quickstart example is broken TODAY — v1alpha1 is not served), `docs/gates.md`, `docs/git-hosts.md`, `docs/project-authoring.md` (its header claims a "v1alpha1 schema lock" — re-lock to v1alpha3). All 11 `config/samples/tide_v1alpha1_*.yaml` files: contents to v1alpha3 and filenames renamed to match (`tide_v1alpha3_*`). Migration guide gains the v1alpha2→v1alpha3 chapter including a levels-remap table (old key meaning → new key meaning) so an operator can re-author their Project correctly.
- **D-07: Sweep the stale comments.** ~10 files carry comment-only v1alpha1 references, several factually wrong (e.g. `cmd/manager/main.go:65-66,303-309` claims v1alpha1 is still scheme-registered; it is not — the import is gone). Comments must describe the v1alpha3 world.

### Envelope contract
- **D-08: Decouple to `dispatch.tideproject.k8s/v1alpha1`** (kubeadm pattern: K8s-shaped document that is not a served resource gets its own subdomain group). Version component stays `v1alpha1` — pure group decoupling, no stability claim. This SUPERSEDES `pkg/dispatch/doc.go`'s documented plan to bump the envelope to `tideproject.k8s/v1beta1` — that plan is what created the collision. If the levels rename forces envelope field/value changes (the `Level` field's `"project"` mismatch is part of the folded todo's bug surface), both breaks ride this one crank. All in-repo subagent images and the contract doc update together.

### v1alpha3 schema content
- **D-09: Research audits for batchable breaking changes.** The phase researcher inventories known v1alpha2 schema warts (deprecated fields, CEL validation gaps, naming inconsistencies) beyond the levels rename; the user picks what rides the crank at PLAN approval. This is an ASK-FIRST checkpoint — present the inventory, do not silently batch.

### Plan-time decisions (D-09 ASK-FIRST resolved 2026-07-06, post-research)
- **D-10: Drop the dead `ProjectSpec.ModelSelection` field in v1alpha3** (research wart W1 — zero readers outside `api/`; duplicates the wired `subagent.levels`). Document the removal in the migration guide. W3/W5 rejected as cosmetic; W4 was a false positive.
- **D-11: The `levels.plan` collapse is accepted** — after the rename, PLAN.md-authoring (phase_controller dispatch) and task-DAG-authoring (plan_controller dispatch) both resolve `Subagent.Levels.Plan`. No 5th key; the 4-key operator ladder stands.
- **D-12: Bonus staleness — fix living docs, preserve snapshots.** `SECURITY.md:40` and `docs/rbac.md:213` (stale conversion-webhook prose from pre-Phase-23) fold into the D-06 sweep; `docs/audit/*.md` stay untouched as dated historical records.
- **Mandatory scope (not optional):** the `verify-no-aggregates` Makefile gate hardcodes `api/v1alpha1|v1alpha2` globs and silently stops checking anything once those packages are deleted — it MUST be repointed (harden to a version-agnostic `api/v1alpha*` glob) in the same plan that deletes the packages.

### Claude's Discretion
- Envelope `Level` string values (whether `"project"` dispatch level is renamed as part of the mapping fix) — within the todo's decided mapping.
- Plan sequencing/decomposition (v1alpha3-first then removals is the natural order; how many plans is the planner's call).
- New REQUIREMENTS.md requirement IDs for this phase (bookkeeping at plan time).
- Whether `api/v1alpha3` starts as a copy of v1alpha2 (Phase 23 precedent) or kubebuilder-scaffolded fresh.

### Folded Todos
- **`2026-07-03-project-level-subagent-override-slot.md` — Rename subagent.levels semantics.** Original problem: `levels.milestone` reads as "the model that authors the milestone" but actually means "the model the Milestone CR uses to author phase briefs"; the MILESTONE.md dispatch (level `"project"`) matches no key and silently falls back. Fits here because the rename is a breaking schema change requiring a SchemaRevision bump — exactly what v1alpha3 is. The todo's DECIDED mapping table is the implementation spec.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### The folded rename (implementation spec)
- `.planning/todos/pending/2026-07-03-project-level-subagent-override-slot.md` — DECIDED target mapping table (spec key → dispatch surface); rejected alternatives documented. Frontmatter pins the three code sites: `internal/controller/project_controller.go:1213`, `internal/controller/dispatch_helpers.go:138`, `api/v1alpha2/project_types.go:165`.

### Migration pattern
- `docs/migration/v1alpha1-to-v1alpha2.md` — the existing guide; its structure (what changed and why → reinstall recipe) is the template for the v1alpha2→v1alpha3 chapter. Records D-09 (conversion webhook retired).

### Envelope contract
- `pkg/dispatch/envelope.go` — `APIVersionV1Alpha1`, `ValidateAPIVersionKind` discipline, EnvelopeIn/Out shapes.
- `pkg/dispatch/doc.go` — D-A3 versioning contract + import firewall (MUST NOT import controller-runtime/anthropic/internal). Its v1beta1-bump plan is superseded by D-08.

### CRD/schema surfaces
- `config/crd/bases/` — 6 generated CRD manifests, each carrying a dead v1alpha1 block (`served: false, storage: false`).
- `charts/tide-crds/templates/` — chart copies of the same 6; chart and binary move together (chart version/appVersion bump rules apply).
- `api/v1alpha2/` — source schema for v1alpha3; `+kubebuilder:storageversion` markers move.
- `internal/webhook/v1alpha2/` — webhooks are already v1alpha2-only; they migrate to v1alpha3.

### Docs to sweep
- `docs/INSTALL.md:210`, `docs/gates.md:14`, `docs/git-hosts.md:68`, `docs/project-authoring.md:5,132,210,320` — v1alpha1 Project examples that fail against the served:false API today.
- `config/samples/tide_v1alpha1_*.yaml` — 11 files, contents + filenames.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`checkSchemaRevisionGuard` (project_controller.go ~1674):** the fail-closed guard from Phase 23 Plan 23-03 — generalize, don't reinvent. Already sets Ready=False/RequiresReinstall + TerminalError.
- **Phase 23's v1alpha2 introduction** is the procedural precedent for introducing v1alpha3 (new api/ package, storageversion marker flip, controller re-import, envtest migration).
- **Import-firewall analyzer rules** (providerfirewall; credproxy MUST NOT import api/) — the crank must not violate them; they also catch accidental v1alpha3 leaks into pkg/dispatch.

### Established Patterns (facts that reframe effort)
- v1alpha1 is already `served: false, storage: false` in all 6 CRDs — no API-server-facing deprecation step remains.
- `api/v1alpha1/` has exactly ONE external importer: `internal/controller/project_controller_v2_guard_test.go`. Package removal is nearly free; the cost is the v1alpha2→v1alpha3 consumer migration (~16 live files import v1alpha2).
- ~20 test files reference v1alpha1 (mostly strings/comments); `api/v1alpha1/` itself contains 6 test files that die with the package.
- Envelope string constants also live at `cmd/tide-push/main.go:116` and `cmd/tide-eval/main.go:83`.

### Integration Points
- Scheme registrations: `cmd/manager/main.go`, `cmd/dashboard/main.go`, `cmd/tide/root_flags.go` (CLI typed client).
- Dispatch: `internal/dispatch/podjob/{backend,jobspec}.go`, `internal/controller/task_controller.go`.
- Chart: `charts/tide-crds/` (CRDs) + `charts/tide/` (values.yaml is the FIXED contract — binary catches up to chart, never reverse; the crank bumps chart version + appVersion as release step one).

</code_context>

<specifics>
## Specific Ideas

- **Levels remap table** (from the folded todo, DECIDED): `levels.milestone` → authors MILESTONE.md (current dispatch level `"project"`); `levels.phase` → authors phase briefs (current `"milestone"`); each key shifts one level down from current semantics. The migration-doc chapter must show old-vs-new meaning side by side.
- **kubeadm precedent** for the envelope group (`kubeadm.k8s.io/v1beta4` config files vs core resource APIs) — cite it in the pkg/dispatch doc comment so the decoupling rationale survives.
- End state to verify: 6 single-version CRD manifests (roughly half their current YAML), zero `v1alpha1`/`v1alpha2` grep hits outside `docs/migration/` and `.planning/`.

</specifics>

<deferred>
## Deferred Ideas

- **Envelope stability declaration (`dispatch.tideproject.k8s/v1`):** deliberately NOT taken — revisit once the post-rename contract has soaked.
- **`subagent.levels` "own milestone" routing:** superseded by the fold into Phase 40 (user decision 2026-07-06).

### Reviewed Todos (not folded)
- Dashboard log-stream drawer empty / Planning-DAG artifact view — owned by Phase 37.
- `spec.git.baseRef` — owned by Phase 35.
- Claude 5 pricing table / Prometheus setup step — owned by Phase 38.
- GPG-signed Verified badge — descoped from v1.0.7 (Future Requirements).

</deferred>

---

*Phase: 40-deprecate-v1alpha1-api*
*Context gathered: 2026-07-06*
