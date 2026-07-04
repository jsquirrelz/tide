# Phase 35: Git Base Ref - Research

**Researched:** 2026-07-04
**Domain:** Kubernetes CRD schema evolution + go-git ref resolution + Job-envelope failure classification
**Confidence:** HIGH

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

#### Accepted ref forms (the documented contract)
- **D-01:** Resolution is an **explicit chain**: `refs/heads/<ref>` → `refs/tags/<ref>` (peel annotated tags to the commit) → full 40-hex SHA. NOT go-git `ResolveRevision`. `HEAD`, short SHAs, and `~`/`^` suffixes are rejected — the docs enumerate exactly three short forms.
- **D-02:** A value starting with `refs/` is resolved **verbatim, before the chain** — the explicit disambiguation escape hatch for branch/tag name collisions, and it avoids the Argo Workflows `refs/heads/*`-rejection surprise (argo-workflows#5629).
- **D-03:** A SHA not reachable from any fetched ref (the bare clone fetches all heads + tags) is **unresolvable — same typed condition**, with the message noting SHAs must be reachable from a branch or tag. No targeted-fetch machinery, no host-dependent behavior.

#### Failure timing & surface
- **D-04:** The unresolvable-ref check fires **in the clone Job only** (`EnsureRunBranch` is the single resolution site). No controller-side ls-remote preflight — no new manager→git-remote egress, no second resolution site to drift. Failure still surfaces before any subagent spend.
- **D-05:** Clone mode adopts the **existing push-mode envelope contract**: small JSON envelope written to the PVC and `/dev/termination-log`; the ProjectReconciler parses the pod termination message. New `envelope.reason: baseref-unresolvable` extends the existing exit-code/reason taxonomy (exit 2 invariant / 10 leak-detected / 11 lease-rejected / 12 auth-failed / 13 network-timeout). Clone mode currently writes NO envelope (`cmd/tide-push/main.go:257`) — adding one is in scope.
- **D-06:** The controller **classifies** the clone-Job failure: `baseref-unresolvable` → typed condition (e.g. `CloneFailed`/`BaseRefUnresolvable`), and the current delete-and-re-dispatch-forever arm (`internal/controller/project_controller.go:610-628`) must NOT re-dispatch for this class. Condition message follows the Argo CD canonical wording: names the bad ref, "unable to resolve '<ref>' to a commit SHA".

#### Recovery & mutability
- **D-07:** Recovery is **edit-spec-and-re-attempt**: the condition halts clone re-dispatch for the current generation; a spec edit (new generation) clears it and re-runs the clone. Classify-don't-retry holds — the same bad ref is never hot-looped. A typo costs one `kubectl edit`, not a Project recreate.
- **D-08:** **No CEL immutability rule on baseRef** — it would block the D-07 recovery edit (spec CEL can't see status) and research P10 warns `oldSelf` transition rules are never ratcheted and fire on adopted/imported objects.
- **D-09:** baseRef edits **after a successful clone are inert, documented only** — `EnsureRunBranch` idempotency (existing run branch untouched) makes this true mechanically. CRD field comment + operator docs state it; NO edit-detection logic, NO as-used-ref stamp in status. (User explicitly chose docs-only over an observable event/condition signal.)
- **D-10:** Adoption/import path needs **no special case**: an adopted Project's run branch already exists, so baseRef is inert there — same documented semantics as D-09.

#### baseSHA stamping
- **D-11:** `status.git.baseSHA` is stamped **on every run**, including default-HEAD runs with no baseRef set — reproducibility provenance (the ref can move after run start; Argo CD `status.sync.revision` pattern). Transport: the clone **success** envelope carries the resolved SHA back to the controller (the manager cannot mount project PVCs). Annotated tags stamp the peeled commit SHA.

### Claude's Discretion
- CEL safe-charset validation on the field (PITFALLS security note: reject argument-injection-shaped refs) — shape and strictness at planning/implementation time; must guard absence (`!has(...) || ...`).
- Exit-code assignment for `baseref-unresolvable` within the tide-push taxonomy.
- Exact condition type/reason identifiers, and where the docs for accepted forms live (CRD field comment vs INSTALL/usage doc — likely both).
- Stamping timing (same status patch as `CloneComplete` is the natural spot).

### Folded Todos
- **`2026-07-03-git-baseref-run-branch.md` — "Add spec.git.baseRef so runs can branch off a non-default ref."** Original problem: `EnsureRunBranch` (`pkg/git/branch.go:40`) always creates the run branch at the bare clone's HEAD; the first external run (2026-07-03) needed a run based on an unmerged hotfix branch and had no option but merging it to main first. This IS the phase — its solution sketch (CRD field, plumb through clone Job env, resolve in `EnsureRunBranch`, reject unresolvable at reconcile with a clear condition) is refined by D-01..D-11 above.

### Deferred Ideas (OUT OF SCOPE)
- **Targeted SHA fetch for unreachable commits** (e.g. PR-head SHAs via `uploadpack.allowAnySHA1InWant`) — rejected for v1.0.7 as host-dependent behavior; revisit only if operators hit the documented limit in practice
- **Observable signal on post-clone baseRef edits** (event/condition + as-used-ref stamp) — considered and explicitly declined in favor of docs-only (D-09); a future ergonomics pass could revisit
- **`tide apply --base-ref` CLI flag** — not raised in discussion, not in BASE-01..03; CRD-only this phase
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BASE-01 | Operator can set `spec.git.baseRef` (branch, tag, or SHA) to base a run on a non-default ref; absent field keeps current HEAD behavior (no default marker) | Verified plumbing chain (§Architecture Patterns); resolution algorithm corrected for go-git's actual bare-clone ref layout (§Pitfall 1 — the load-bearing finding); field markers pattern (§Code Examples) |
| BASE-02 | An unresolvable baseRef fails fast with a typed condition (classify-don't-retry), not a cryptic worktree-add failure | Envelope contract to replicate (`writePushEnvelope`, `readPushEnvelope`, buildPushJob termination-message wiring — all verified); dispatch-halt mechanics incl. the TTL-GC trap (§Pitfall 2); billing-halt classification precedent |
| BASE-03 | The resolved base SHA is stamped in `status.git.baseSHA`; the field exists in both API versions with conversion round-trip and CRD upgrade-path tests | Set-on-success arm at `project_controller.go:594-600` (natural stamping site); conversion reality documented (§Pitfall 4 — no webhook exists; strategy is None); existing schema-parity + helm-render test patterns identified |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **GSD workflow enforcement** — all edits through GSD; `charts/tide/values.yaml` is a FIXED contract (binary catches up to chart, never reverse). Phase 35 touches the **CRD chart** (`charts/tide-crds`), regenerated via `make helm-crds`; no `charts/tide/values.yaml` change is needed for this phase.
- **CRD `.status` only; minimal resumption state** — `baseSHA` is a provenance stamp (a tally-class fact), not a derived schedule; PERSIST-02 clean.
- **Don't accept a wave list / don't cache schedules** — untouched by this phase.
- **All Anthropic-specific code behind the Subagent interface** — untouched.
- **Verify before claiming:** `make test-int` exit ≠ Ginkgo green — the kind package bundles plain go-tests (helm-template contract tests) beside Ginkgo specs; read `MAKE_EXIT` and grep `^--- FAIL`.
- **Constrained-VM recipe** for kind runs: fresh cluster per heavy run, one heavy run at a time, never concurrent with acceptance clusters.
- **CEL CRD validation, not admission webhooks** — this phase's field validation uses kubebuilder markers (Pattern/MaxLength or XValidation), never a webhook.
- **Don't predict "chain terminator"; observe first** — clone-arm behavior verified by reading the actual reconciler source, not assumed.

## Summary

Phase 35 is a clone-path-only feature riding the milestone's first CRD schema change. Every integration point named in CONTEXT.md was verified against the working tree: the plumbing chain (`GitConfig` → clone dispatch at `project_controller.go:571-580` → `buildCloneJob` args at `push_helpers.go:292-303` → `runClone` at `cmd/tide-push/main.go:259-326` → `EnsureRunBranch` at `pkg/git/branch.go:40`) exists exactly as documented, clone mode writes no envelope today, and the WR-03 terminal-failed clone arm (`project_controller.go:610-628`) deletes-and-re-dispatches unconditionally.

Research surfaced **one load-bearing correction to D-01's implementation** (not its contract): go-git's non-mirror bare clone does NOT store remote branches at `refs/heads/<branch>`. Verified against go-git v5.19.0 source: the default clone refspec is `+refs/heads/*:refs/remotes/origin/*`, and only the default branch gets a local `refs/heads/<name>` ref. So the branch step of the resolution chain must consult `refs/remotes/origin/<ref>` as well — otherwise the phase's primary use case (base off an unmerged hotfix branch) fails with "unresolvable" for every non-default branch. Tags are safe: `CloneOptions.Validate()` defaults `Tags` to `AllTags`, so `refs/tags/*` arrives fully.

Second key finding: "don't re-dispatch" cannot be implemented as "don't delete the failed Job" — the clone Job has `TTLSecondsAfterFinished: 300`, so K8s GC's the failed Job and the existing `IsNotFound`-gated dispatch arm re-fires anyway. The halt must be an explicit condition-check on the dispatch guard, keyed to `ObservedGeneration` so a spec edit (D-07) releases it. Also verified: there is **no conversion webhook in this codebase** — v1alpha1 is `+kubebuilder:unservedversion` (served: false) and conversion strategy is None; "conversion round-trip" for this repo means struct/schema parity in both API versions plus JSON round-trip tests, following the established `api/v1alpha1/phase3_schema_test.go` static-test pattern.

**Primary recommendation:** Extend `EnsureRunBranch` with a resolution chain of `refs/` verbatim → `refs/heads/<ref>` **then `refs/remotes/origin/<ref>`** → `refs/tags/<ref>` (peel via `TagObject`) → 40-hex `CommitObject` check; adopt the push-envelope contract for clone mode with exit 2 + reason `baseref-unresolvable`; gate clone dispatch on a generation-scoped `CloneFailed/BaseRefUnresolvable` condition.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| `spec.git.baseRef` / `status.git.baseSHA` schema | API (CRD, both versions) | CRD chart (`charts/tide-crds`) | Field definition + validation markers live in `api/v1alphaN`; chart is generated output (`make manifests` → `make helm-crds`) |
| Field charset validation | API server (kubebuilder Pattern/CEL) | — | Reject injection-shaped refs at admission; existence of the ref CANNOT be validated here (CEL can't see the remote — P10) |
| Ref resolution + run-branch creation | Clone Job (`tide-push --mode=clone` → `pkg/git.EnsureRunBranch`) | — | D-04: single resolution site, PVC-local, before any subagent spend |
| Failure envelope transport | Clone Job → pod termination message + PVC | Controller (parse) | D-05: manager cannot mount project PVCs; termination-log is the established channel |
| Failure classification + dispatch halt | ProjectReconciler | — | D-06: typed condition, generation-scoped halt; controller owns retry policy |
| baseSHA stamping | ProjectReconciler (set-on-success arm) | Clone Job (produces SHA in success envelope) | D-11: same status patch as `CloneComplete` |
| Accepted-forms documentation | CRD field comment + `docs/project-authoring.md` | `docs/INSTALL.md` (CRD-chart-first upgrade order) | Discretion: both, per CONTEXT.md leaning |

## Standard Stack

### Core

**No new dependencies.** [CITED: .planning/research/STACK.md — scope note: existing stack "covers … `spec.git.baseRef` … with zero additions"; "Features needing NO stack additions" table lists `spec.git.baseRef` with "no new dep"]

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| go-git v5 | v5.19.0 (pinned, `go.mod:7`) | Ref lookup, tag peel, commit existence | Already the repo's only git library [VERIFIED: go.mod] |
| controller-runtime | v0.24.x | Status patch, condition helpers | Existing stack |
| apimachinery `meta` | (bundled) | `SetStatusCondition` with `ObservedGeneration` | Existing pattern (`billing_halt.go:126`) [VERIFIED: internal/controller/billing_halt.go] |

### go-git v5.19.0 API surface for the resolution chain (all verified against tagged source)

| API | Signature | Use |
|-----|-----------|-----|
| `plumbing.NewBranchReferenceName` | `func(name string) ReferenceName` | `refs/heads/<ref>` lookup [VERIFIED: go-git v5.19.0 plumbing/reference.go] |
| `plumbing.NewTagReferenceName` | `func(name string) ReferenceName` | `refs/tags/<ref>` lookup [VERIFIED: go-git v5.19.0 plumbing/reference.go] |
| `plumbing.NewRemoteReferenceName` | already used at `pkg/git/fetch_test.go:114` | `refs/remotes/origin/<ref>` lookup [VERIFIED: codebase] |
| `repo.Reference` | `func(name plumbing.ReferenceName, resolved bool) (*plumbing.Reference, error)` | verbatim `refs/` lookup (D-02) [VERIFIED: go-git v5.19.0 repository.go] |
| `repo.TagObject` | `func(h plumbing.Hash) (*object.Tag, error)` | detect + peel annotated tags (succeeds iff hash is a tag object) [VERIFIED: go-git v5.19.0 repository.go] |
| `repo.CommitObject` | `func(h plumbing.Hash) (*object.Commit, error)` | SHA existence check; already used at `cmd/tide-push/main.go:513` [VERIFIED: codebase + source] |
| `plumbing.IsHash` | `func(s string) bool` — exact 40-hex check | gate the SHA arm of the chain [VERIFIED: go-git v5.19.0 plumbing/hash.go] |

`repo.ResolveRevision` exists but is **excluded by D-01** (it accepts `HEAD`, short SHAs, `~`/`^` — exactly the forms the contract rejects).

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Adding `refs/remotes/origin/<ref>` to the branch arm | `CloneOptions.Mirror: true` (refspec `+refs/*:refs/*`, gives git-CLI-style `refs/heads/*` in the bare repo) | Mirror changes the fetch surface for every existing run (pulls `refs/pull/*`, notes, etc. on GitHub remotes), alters remote config, and risks regressing the stable clone/integrate path — too invasive for a clone-path-only phase. Rejected. |
| Exit 2 + reason `baseref-unresolvable` | New dedicated exit code (e.g. 14) | Phase 34 is already claiming exit 14 for `integration-incomplete` [CITED: .planning/research/ARCHITECTURE.md §Q7]; ARCHITECTURE.md §Q3 explicitly sketches "exit 2 invariant with a distinct reason". Controller classifies on `envelope.reason`, not exit code (billing-halt precedent). Recommend exit 2. |
| Field-level `Pattern` marker | Struct-level `XValidation` CEL with `!has(...)` guard | Field-level Pattern/MaxLength only evaluate when the field is present — absence-guarding is automatic, zero CEL cost budget. Recommend Pattern (see §Code Examples); CEL only if the rule needs cross-field logic (it doesn't). |

**Installation:** none. No new Go modules, no npm packages.

## Package Legitimacy Audit

No new packages are installed by this phase. All work uses dependencies already pinned in `go.mod` (go-git v5.19.0, controller-runtime v0.24.x). **Packages removed due to [SLOP] verdict:** none. **Packages flagged as suspicious [SUS]:** none.

## Architecture Patterns

### System Architecture Diagram

```
Operator                    API server                     ProjectReconciler
   │  kubectl apply             │                                │
   │  spec.git.baseRef ────────►│ Pattern/MaxLength check        │
   │                            │ (charset only — NOT existence) │
   │                            │──────── watch event ──────────►│
   │                                                             │ dispatch guard (571):
   │                                                             │  !CloneComplete && IsNotFound
   │                                                             │  && !baseRefHalted(gen-scoped)   ◄── NEW
   │                                                             ▼
   │                                              buildCloneJob(+ --base-ref=<ref>)
   │                                                             │
   │                                                   ┌─────────▼──────────┐
   │                                                   │ clone Job (tide-push│
   │                                                   │  --mode=clone)      │
   │                                                   │ 1. bare clone (all  │
   │                                                   │    heads→remotes/*, │
   │                                                   │    tags→tags/*)     │
   │                                                   │ 2. resolve baseRef  │ ◄── NEW (in EnsureRunBranch)
   │                                                   │    chain D-01/D-02  │
   │                                                   │ 3. run branch @ SHA │
   │                                                   │ 4. worktree add     │
   │                                                   │ 5. write envelope   │ ◄── NEW (success AND failure)
   │                                                   └─────────┬──────────┘
   │                                          termination-log + PVC envelope
   │                                                             │
   │                              ┌──── success (exit 0) ────────┴──── failure (exit 2, reason=
   │                              ▼                                     baseref-unresolvable)
   │                 set-on-success arm (594-600):                      ▼
   │                 CloneComplete=true                    WR-03 arm (610-628) classifies:  ◄── CHANGED
   │                 + status.git.baseSHA=<resolved>  ◄── NEW  reason==baseref-unresolvable?
   │                              │                            YES → CloneFailed=True/
   │                              ▼                                  BaseRefUnresolvable
   │                    executor waves dispatch                      (ObservedGeneration=gen),
   │                                                                 NO delete, NO re-dispatch
   │                                                           NO  → delete + re-dispatch (unchanged)
   │  kubectl edit spec (typo fix) ──► new generation ──► halt releases ──► fresh clone dispatch
```

### Recommended Change Map (no new packages/dirs — all MODIFY except tests)

```
api/v1alpha2/project_types.go        # GitConfig.BaseRef (+markers), GitStatus.BaseSHA
api/v1alpha1/project_types.go        # identical twins (P9)
api/v1alpha2/shared_types.go         # reason constant if needed (ConditionCloneFailed exists at :108)
internal/controller/push_helpers.go  # CloneOptions.BaseRef; buildCloneJob --base-ref arg +
                                     #   TerminationMessagePath/Policy (clone container lacks it today)
internal/controller/project_controller.go  # dispatch guard halt-check; WR-03 arm classification;
                                            #   set-on-success arm reads clone envelope, stamps BaseSHA
cmd/tide-push/main.go                # pushConfig.BaseRef; --base-ref flag; clone-mode envelope writes
pkg/git/branch.go                    # EnsureRunBranch gains baseRef param + resolution chain
config/crd/bases/, charts/tide-crds/ # generated: make manifests && make helm-crds
docs/project-authoring.md, docs/INSTALL.md  # accepted forms; CRD-chart-first upgrade order
```

### Pattern 1: Resolution chain (the D-01 contract, corrected for go-git's ref layout)

**What:** Ordered lookup implementing exactly three short forms + one qualified escape hatch.
**When to use:** Inside `EnsureRunBranch` (or a helper it calls) — the single resolution site (D-04).

Order:
1. `strings.HasPrefix(ref, "refs/")` → `repo.Reference(plumbing.ReferenceName(ref), true)` verbatim (D-02). **Nuance:** if verbatim lookup fails AND the ref starts with `refs/heads/`, retry as `refs/remotes/origin/<short>` — otherwise the disambiguation escape hatch fails for every non-default branch (see Pitfall 1). Document behavior; the error message still names the ref as given.
2. Branch: `repo.Reference(plumbing.NewBranchReferenceName(ref), true)`, then on failure `repo.Reference(plumbing.NewRemoteReferenceName("origin", ref), true)` — the second lookup is where ALL non-default branches live in TIDE's bare clone [VERIFIED: go-git v5.19.0 `cloneRefSpec` default = `config.DefaultFetchRefSpec` = `+refs/heads/*:refs/remotes/%s/*`; `updateReferences` creates local `refs/heads/<name>` only for the resolved default branch].
3. Tag: `repo.Reference(plumbing.NewTagReferenceName(ref), true)`. Peel: if `repo.TagObject(hash)` succeeds it's an annotated tag → use `tag.Commit()` (stamps the **peeled** commit SHA per D-11); if it fails with not-found, the hash is already the commit (lightweight tag).
4. SHA: `plumbing.IsHash(ref)` (exact 40-hex) → `repo.CommitObject(plumbing.NewHash(ref))`; any error → unresolvable with the D-03 reachability message. Object presence in a full clone ≈ reachable from an advertised head/tag at clone time.
5. All arms fail → typed error carrying the Argo CD wording: `unable to resolve '<ref>' to a commit SHA` [CITED: .planning/research/FEATURES.md §1 — Argo CD canonical].

After any successful arm: sanity-verify the resolved hash is a commit (`CommitObject`) — a `refs/`-verbatim ref could name a non-commit object.

### Pattern 2: Clone-mode envelope (adopt push contract, D-05)

Replicate `writePushEnvelope` (`cmd/tide-push/main.go:662-703`) for clone mode: JSON to `/dev/termination-log` (4096-byte cap; envelope is <1 KB) + `<workspace>/envelopes/push/<uid>.json` best-effort. Extend the envelope struct with a `baseSHA` field (success carries the resolved SHA per D-11; failure carries `reason: baseref-unresolvable`). **Required companion change:** `buildCloneJob`'s container must gain `TerminationMessagePath: "/dev/termination-log"` + `TerminationMessagePolicy: FallbackToLogsOnError` — buildPushJob has it (`push_helpers.go:257-258`), buildCloneJob does NOT [VERIFIED: push_helpers.go:343-364 — clone container has no termination-message wiring]. Write the success envelope on EVERY clone-mode success, including default-HEAD runs (D-11: baseSHA always stamped).

**Success-path envelope-read failure (plan MUST pin this):** the set-on-success arm (`project_controller.go:594-600`) is gated on `!CloneComplete` and flips it exactly once — if the success envelope is unreadable at the moment `Succeeded > 0` is first observed and the arm flips anyway, `status.git.baseSHA` stays permanently empty, undermining D-11/SC3 "stamped on every run". Recommended contract: the set-on-success arm attempts the envelope read FIRST; on `ok` → stamp `baseSHA` and flip `CloneComplete` in the same status patch. On `ok=false` → do NOT flip `CloneComplete`; return `ctrl.Result{RequeueAfter: ~10s}` — the succeeded pod normally outlives the Job by the 300 s TTL, so the read converges within a reconcile or two. Bound the wait statelessly via the Job's `status.completionTime`: once `time.Since(completionTime)` exceeds a cutoff, flip `CloneComplete` with empty `baseSHA` and log the degraded provenance. **The cutoff must be well under the 300 s Job TTL** (e.g. 60 s): if the requeue loop outlives TTL GC, `IsNotFound` fires and the dispatch guard re-clones — but the retried clone hits the run-branch-exists early return (Pitfall 6) without re-resolving, so its envelope carries no fresh baseSHA and the stall recurs. Whatever variant the planner picks, the plan must state explicitly which arm reads the envelope and what happens on `ok=false`.

### Pattern 3: Controller classification (billing-halt precedent)

Mirror `readPushEnvelope` (`project_controller.go:85-109`) for the clone Job (`job-name=tide-clone-<uid>` label). In the WR-03 terminal-failed arm: parse envelope → `reason == "baseref-unresolvable"` → set `CloneFailed=True` with `Reason: "BaseRefUnresolvable"`, `ObservedGeneration: project.Generation`, message naming the ref — and return WITHOUT deleting the Job (deletion is irrelevant to the halt; the condition is the halt). All other reasons (or no parseable envelope) → existing delete-and-re-dispatch behavior unchanged. Recommend reusing the existing `ConditionCloneFailed` type (`api/v1alpha2/shared_types.go:108`) with a new Reason rather than a new condition type — one condition type per failure site, reasons distinguish classes (matches `metav1.Condition` conventions and D-06's "e.g. CloneFailed/BaseRefUnresolvable").

### Pattern 4: Generation-scoped dispatch halt (D-07)

The dispatch guard at `project_controller.go:571` gains a third gate: skip dispatch when `CloneFailed=True` with `Reason==BaseRefUnresolvable` AND `condition.ObservedGeneration == project.Generation`. A spec edit bumps `metadata.generation` → the guard releases → fresh clone dispatches → on success, set-on-success arm clears/overwrites the condition (set `CloneFailed=False` in the same patch that flips `CloneComplete`). `meta.SetStatusCondition` uses the `ObservedGeneration` you pass — set it explicitly from `project.Generation` (the billing-halt call site does not set it; this site must).

### Pattern 5: Both-versions schema + None-strategy "conversion"

**There is no conversion webhook in this codebase.** No `ConvertTo`/`ConvertFrom`/`Hub` implementations exist anywhere [VERIFIED: grep across api/ + internal/]; v1alpha1 carries `+kubebuilder:unservedversion` (`api/v1alpha1/project_types.go:428`), rendering `served: false, storage: false` in the CRD; the CRD has no `conversion:` stanza → strategy None (apiVersion rewrite, schema-pruned). "Survive v1alpha1⇄v1alpha2 conversion round-trip" therefore means: identical field shape in both Go type packages AND both version blocks of the generated CRD schema, locked by:
- Static source-parity tests following `api/v1alpha1/phase3_schema_test.go` (regex over source + generated CRD YAML — the repo's established convention for exactly this)
- A JSON round-trip unit test: marshal `v1alpha1.Project{BaseRef,BaseSHA set}` → unmarshal into `v1alpha2.Project` → assert fields survive, and reverse (this is literally what strategy-None conversion does at the API server)
- A helm-template render test in `test/integration/kind/projects_pvc_test.go` style asserting `baseRef`/`baseSHA` appear under BOTH `v1alpha1` and `v1alpha2` blocks of the rendered `tide-crds` chart (the P8 chart-skew lock)

### Anti-Patterns to Avoid
- **`ResolveRevision` for the chain** — accepts `HEAD`/short-SHA/`~^`, violating D-01's exact-three-forms contract.
- **`+kubebuilder:default` on baseRef** — P10 trap: two encodings for "use HEAD", SSA field-ownership contention. Absent is the only HEAD encoding (binding constraint in STATE.md).
- **CEL immutability (`self == oldSelf`)** — explicitly rejected (D-08).
- **Controller-side ls-remote preflight** — rejected (D-04); one resolution site.
- **New transport for the SHA hand-back** — the termination-log envelope is the established channel; the manager cannot mount project PVCs.
- **Halting by not-deleting the failed Job** — TTL GC defeats it (Pitfall 2).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Hex-SHA validation | regex / manual length check | `plumbing.IsHash` | exact `hash.HexSize` + hex decode, matches go-git's own semantics [VERIFIED: v5.19.0 plumbing/hash.go] |
| Annotated-tag peeling | manual object-type sniffing | `repo.TagObject(h)` + `tag.Commit()` | go-git's typed API; lightweight vs annotated distinction falls out of the error |
| Condition management | manual conditions-slice surgery | `meta.SetStatusCondition` | already the repo pattern; handles LastTransitionTime/dedup |
| Job→controller result transport | new ConfigMap/annotation channel | termination-log envelope (`writePushEnvelope` / `readPushEnvelope`) | contract exists, tested, and D-05 locks it |
| Ref-existence validation at admission | CEL calling out / webhook | reconcile-time typed condition | CEL cannot see the remote (P10); the design already rejects admission-time existence checks |

**Key insight:** every mechanism this phase needs already exists in the codebase for the push path — the work is replicating proven contracts onto the clone path, not inventing.

## Common Pitfalls

### Pitfall 1: go-git bare clone does NOT put remote branches under `refs/heads/*` (load-bearing for D-01)
**What goes wrong:** Implementing the branch arm as only `refs/heads/<ref>` makes every non-default branch "unresolvable" — the exact hotfix-branch use case that motivated the phase fails.
**Why it happens:** go-git's non-mirror clone uses `config.DefaultFetchRefSpec` = `+refs/heads/*:refs/remotes/%s/*` [VERIFIED: v5.19.0 config/config.go + repository.go `cloneRefSpec`]. `updateReferences` creates a local `refs/heads/<name>` + HEAD symref **only for the resolved default branch** [VERIFIED: v5.19.0 repository.go]. TIDE's existing tests never noticed because nothing resolves non-default remote branches today (`EnsureRunBranch` reads `repo.Head()`; task branches are created locally).
**How to avoid:** Branch arm = `refs/heads/<ref>` then `refs/remotes/origin/<ref>`. Unit-test with a fixture repo that has a second branch (extend the `seedBareRepo` helper in `pkg/git/clone_test.go`).
**Warning signs:** unit test "resolve non-default branch" fails; kind medium-http test resolves `main` but not a feature branch.

### Pitfall 2: TTL-GC defeats "just don't re-dispatch" (D-06/D-07 mechanics)
**What goes wrong:** Implementing the halt as "WR-03 arm skips deletion for baseref-unresolvable" stalls only 300 s: `TTLSecondsAfterFinished: 300` GC's the failed Job, `IsNotFound(cloneErr)` fires, and the guard at :571 re-dispatches the same bad ref — a slow-motion hot loop.
**Why it happens:** the dispatch guard's only failure-awareness is Job existence, which is TTL-unreliable — the same class of bug `CloneComplete` was introduced to fix (BYPASS-02, comment at :568-570).
**How to avoid:** the halt is the **condition**, checked in the dispatch guard, scoped by `ObservedGeneration == project.Generation`. Envtest it: failed clone Job with baseref envelope → condition set → delete the Job manually (simulating TTL GC) → assert NO new clone Job is created; then bump the spec → assert a new Job IS created.
**Warning signs:** kind test shows a second `tide-clone-*` Job ~5 min after a baseref failure.

### Pitfall 3: Classification evidence is perishable (pod termination message)
**What goes wrong:** The envelope lives on the failed pod's `Status...Terminated.Message`; pods are GC'd with the Job (TTL 300 s). If the manager is down through that window, evidence is gone and the failure classifies as generic → re-dispatch.
**Why it happens:** same perishability as the push envelope path; acceptable there because re-push converges.
**How to avoid:** For the FAILURE path, accept it — re-dispatch of a bad baseRef re-fails in seconds and re-writes the envelope (clone itself is idempotent via `ErrRepositoryAlreadyExists` → open+fetch [VERIFIED: pkg/git/clone.go:56-60]), so the system converges back to the halted condition after one wasted Job. Document; don't build PVC-envelope readback into the manager (it cannot mount project PVCs). For the SUCCESS path, acceptance is NOT enough — the set-on-success arm flips `CloneComplete` exactly once, so an unread success envelope loses baseSHA forever; follow Pattern 2's success-path contract (read-before-flip, short requeue, bounded sub-TTL fallback to flip-with-empty-baseSHA).
**Warning signs:** none in practice; note in the plan so the verifier doesn't flag the window as a bug.

### Pitfall 4: P8/P9/P10 CRD-evolution traps [CITED: .planning/research/PITFALLS.md §P8-P10]
- **P8 chart skew:** stale `tide-crds` silently prunes `baseRef` — runs branch from HEAD with no error. Lock with the helm-render both-versions test + INSTALL.md "CRD chart first" upgrade step. The success criterion's "upgrade-path test" = render + apply the regenerated CRD and assert a Project with baseRef round-trips through the API server unpruned (envtest applies the fresh CRD from `config/crd/bases` automatically; the kind suite installs the chart CRDs — both layers cover it).
- **P9 both-versions drop:** add the fields to BOTH `api/v1alpha1` and `api/v1alpha2` in the same task; the static parity tests make omission loud.
- **P10 defaulting/CEL:** no `+kubebuilder:default`; field-level Pattern markers auto-guard absence; no `oldSelf` rules (D-08).

### Pitfall 5: `FallbackToLogsOnError` yields non-JSON termination messages
**What goes wrong:** when the container dies before writing `/dev/termination-log`, K8s substitutes the log tail — `json.Unmarshal` on stderr text.
**How to avoid:** already handled by the `readPushEnvelope` pattern (parse failure → `ok=false` → generic path). Keep that contract; never assume the message parses.

### Pitfall 6: Resolution ordering vs. existing idempotency (D-09/D-10)
**What goes wrong:** placing resolution before the existing run-branch existence check in `EnsureRunBranch` would re-resolve (and possibly fail) on Job retries after the branch already exists, breaking the idempotency that makes D-09/D-10 free.
**How to avoid:** keep the `:54` early-return (branch exists → no-op) FIRST; resolve baseRef only on the create path. A retried clone Job with a now-bad ref but existing run branch must still exit 0.

## Code Examples

### Field markers (spec + status, identical in both API versions)

```go
// Source: existing repo conventions (RepoURL Pattern marker at project_types.go:211) + P10
// In GitConfig (api/v1alpha2/project_types.go ~:223, twin in v1alpha1):

// BaseRef optionally names the ref the run branch is created from: an
// existing branch name, tag name, full 40-hex commit SHA, or a fully
// qualified refs/... path. Absent means the remote default branch (HEAD).
// Resolution happens in the clone Job; an unresolvable value surfaces as
// CloneFailed/BaseRefUnresolvable. Edits after a successful clone are
// inert — the run branch is created once (see docs/project-authoring.md).
// +kubebuilder:validation:MinLength=1
// +kubebuilder:validation:MaxLength=250
// +kubebuilder:validation:Pattern=`^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`
// +optional
BaseRef string `json:"baseRef,omitempty"`

// In GitStatus (~:256):

// BaseSHA is the commit SHA the run branch was created from, stamped on
// every run (annotated tags record the peeled commit). Provenance only.
// +optional
BaseSHA string `json:"baseSHA,omitempty"`
```

Rationale: field-level Pattern evaluates only when the field is set (absence auto-guarded — satisfies the discretion note without CEL); first-char class rejects leading `-` (argument-injection shape), leading `.`/`/`; charset excludes space, `~ ^ : ? * [ \` (git-forbidden) and shell metacharacters. `refs/`-qualified values pass (`r` is alnum). 40-hex SHAs pass. MaxLength bounds validation cost. Note baseRef never transits a shell today (exec-array Job args + pure go-git resolution) — the pattern is defense-in-depth per the PITFALLS security note.

### Resolution chain skeleton (pkg/git/branch.go)

```go
// Source: go-git v5.19.0 verified API surface (see Standard Stack table)
func resolveBaseRef(repo *gogit.Repository, ref string) (plumbing.Hash, error) {
    lookup := func(name plumbing.ReferenceName) (plumbing.Hash, bool) {
        r, err := repo.Reference(name, true)
        if err != nil { return plumbing.ZeroHash, false }
        return r.Hash(), true
    }
    var h plumbing.Hash
    var found bool
    switch {
    case strings.HasPrefix(ref, "refs/"): // D-02 verbatim, before the chain
        h, found = lookup(plumbing.ReferenceName(ref))
        if !found && strings.HasPrefix(ref, "refs/heads/") {
            // go-git bare clones store non-default branches under
            // refs/remotes/origin/* (default fetch refspec) — map the
            // qualified branch form there so the escape hatch works.
            h, found = lookup(plumbing.NewRemoteReferenceName("origin",
                strings.TrimPrefix(ref, "refs/heads/")))
        }
    default:
        h, found = lookup(plumbing.NewBranchReferenceName(ref))
        if !found {
            h, found = lookup(plumbing.NewRemoteReferenceName("origin", ref))
        }
        if !found {
            h, found = lookup(plumbing.NewTagReferenceName(ref))
        }
        if !found && plumbing.IsHash(ref) {
            h, found = plumbing.NewHash(ref), true
        }
    }
    if !found {
        return plumbing.ZeroHash, fmt.Errorf(
            "unable to resolve %q to a commit SHA: baseRef must be an existing branch, tag, or full 40-hex commit SHA reachable from a branch or tag", ref)
    }
    // Peel annotated tags (D-11: stamp the peeled commit).
    if tag, err := repo.TagObject(h); err == nil {
        c, cerr := tag.Commit()
        if cerr != nil { return plumbing.ZeroHash, fmt.Errorf("peel tag %q: %w", ref, cerr) }
        return c.Hash, nil
    }
    // Verify the hash names a commit that exists locally (D-03 reachability).
    if _, err := repo.CommitObject(h); err != nil {
        return plumbing.ZeroHash, fmt.Errorf(
            "unable to resolve %q to a commit SHA: object %s is not a commit reachable from a fetched branch or tag", ref, h)
    }
    return h, nil
}
```

(Signature/plumb-through of `EnsureRunBranch(bareRepoPath, branch, baseRef string)` and its two callers — `cmd/tide-push/main.go:298` and the doc references — is a mechanical follow-on; keep the existing `:54` idempotency early-return ahead of resolution per Pitfall 6.)

### Condition stamp (WR-03 arm, classified branch)

```go
// Source: existing WR-03 arm (project_controller.go:615-625) + billing-halt precedent
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               tidev1alpha2.ConditionCloneFailed,
    Status:             metav1.ConditionTrue,
    Reason:             "BaseRefUnresolvable",          // new reason constant
    Message:            fmt.Sprintf("unable to resolve '%s' to a commit SHA; fix spec.git.baseRef to re-attempt the clone", baseRef),
    ObservedGeneration: project.Generation,             // D-07 scope — MUST be set explicitly
    LastTransitionTime: metav1.Now(),
})
// then: return ctrl.Result{}, statusPatch — NO Job deletion, NO requeue-loop
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Run branch always at bare-clone HEAD (`repo.Head()` hardcoded) | Operator-selectable base via `spec.git.baseRef`, ecosystem-standard single revision-string field | this phase | Matches Argo CD `targetRevision` shape; the ecosystem consensus (Dependabot/Renovate/Argo/Tekton) is fail-fast + no silent fallback [CITED: .planning/research/FEATURES.md §1] |
| Clone failures: delete-and-re-dispatch forever (WR-03) | Classify-don't-retry for config-class failures | this phase | Extends the billing-halt classification posture to the clone path |
| Clone mode: no envelope | Clone adopts the push envelope contract | this phase | Uniform Job→controller result transport across both tide-push modes |

**Deprecated/outdated:** nothing removed; `ConditionCloneFailed` semantics extend (existing `CloneJobFailed` reason keeps its delete-and-re-dispatch meaning; new `BaseRefUnresolvable` reason halts).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | With `Tags: AllTags` (verified default in `CloneOptions.Validate()`), fetched tags are stored locally at `refs/tags/<name>` [ASSUMED — mechanism from training; the default itself is VERIFIED against v5.19.0 options.go] | Pattern 1 / Pitfall 1 | Tag arm of the chain fails — caught immediately by the pkg/git unit test that seeds an annotated + lightweight tag (write that test first) |
| A2 | Chart `version:` bump (1.0.6→1.0.7) is deferred to Phase 36's batch; Phase 35 regenerates `charts/tide-crds` templates via `make helm-crds` without bumping `Chart.yaml` [ASSUMED — interpretation of "its chart bump batches with Phase 36's"] | Open Questions | If wrong, a one-line Chart.yaml edit in either phase; no code impact |
| A3 | `meta.SetStatusCondition` honors an explicitly-set `ObservedGeneration` on the passed condition [ASSUMED — apimachinery standard behavior] | Pattern 4 | Generation-scoped halt fails to release on spec edit — caught by the envtest in Pitfall 2's test plan |
| A4 | Go toolchain + kind are available in the execution environment (devcontainer / dev Docker VM) even though `go` is absent from this macOS host's PATH [ASSUMED — every prior phase ran `make test-int`; `.devcontainer/` exists] | Environment Availability | Executor cannot run tests on the host — must run them where prior phases did |
| A5 | Reconcile is triggered promptly on clone-Job status transitions (controller watches owned Jobs), so the termination-message envelope is read before TTL GC in the normal case [ASSUMED — standard controller-runtime `Owns()` wiring; not re-verified this session] | Pitfall 3 | BaseSHA unstamped after manager downtime spanning the pod-GC window — degraded provenance; Pattern 2's read-before-flip + bounded-fallback contract makes this a logged, bounded outcome rather than a silent permanent one |

## Open Questions (RESOLVED)

All three questions were resolved during planning; each resolution lives in executable plan text.

1. **Chart version bump timing (A2)** — RESOLVED: adopted the recommendation in plan 35-01 (objective scope guard defers the `version:` bump to Phase 36's batch and instructs the verifier not to flag the unbumped chart; Phase 35 regenerates templates only).
   - What we know: STATE.md binding constraint — "Phase 35's CRD change and Phase 36's agent-identity CRD/chart config batch into one chart version bump"; phases execute 35 → 36.
   - What's unclear: whether the single bump lands in Phase 35 (and 36 rides it) or Phase 36 (and 35's regenerated templates sit at 1.0.6 in-tree between phases — harmless pre-release).
   - Recommendation: Phase 35 regenerates templates only; Phase 36 performs the one `version: 1.0.7` bump for both charts. Planner should state this explicitly so the verifier doesn't flag the unbumped chart.
2. **Success-envelope kind string** — RESOLVED: plan 35-02 fixes `envelopeKindClone = "CloneResult"` for clone mode (controller still parses by reason/fields, not kind).
   - What we know: push envelope uses `Kind: "PushResult"`; clone adopts the contract (D-05).
   - Recommendation: keep one struct, add `baseSHA` field; either keep `PushResult` kind (least change) or emit `CloneResult` for clone mode (clearer). Cosmetic — planner's choice; the controller should parse by `reason`/fields, not kind.
3. **Should `auth-failed`/`network-timeout` clone envelopes also halt?** — RESOLVED: out of scope per the recommendation; plan 35-03 must_haves truth 4 pins delete-and-re-dispatch for every other clone reason as an explicit non-goal (35-04's objective scope guard restates it).
   - What we know: adding clone envelopes makes these reasons visible for the first time; D-06 only requires halting `baseref-unresolvable`.
   - Recommendation: out of scope — keep delete-and-re-dispatch for every other reason (network is transient; auth-halt is a separate design decision). State as an explicit non-goal in the plan.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| docker | image builds, kind | ✓ (host) | 29.5.3, daemon up (minikube container running) | — |
| kubectl | cluster ops | ✓ (host) | /opt/homebrew/bin/kubectl | — |
| helm | chart render tests | ✓ (host) | /opt/homebrew/bin/helm | — |
| git | everything | ✓ (host) | /opt/homebrew/bin/git | — |
| go | build + unit tests | ✗ on host PATH | — | Devcontainer / dev Docker VM (`.devcontainer/` present; all prior phases ran `make test`/`make test-int`) [ASSUMED A4] |
| kind | Layer B tests | ✗ on host PATH | — | Same environment as go; constrained-VM recipe in CLAUDE.md applies |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** `go`, `kind` — run all `make` targets in the established dev environment, not the bare macOS host. Plans should express verification as `make` targets, unchanged from prior phases.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` (plain go-tests) + Ginkgo v2.28/Gomega + envtest (Layer A) + kind (Layer B) |
| Config file | Makefile targets (no separate config); envtest via `setup-envtest` |
| Quick run command | `go test ./pkg/git/... ./cmd/tide-push/... ./api/...` (seconds; runs in dev env) |
| Full suite command | `make test` (unit tier) → `make test-int-fast` (Layer A envtest) → `make test-int` (full, incl. kind Layer B) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BASE-01 | Chain resolves branch (default + non-default), tag (annotated peeled + lightweight), 40-hex SHA, `refs/` verbatim; rejects `HEAD`/short-SHA/`~^`; absent baseRef → HEAD behavior unchanged | unit | `go test ./pkg/git/ -run TestEnsureRunBranch` | ❌ new cases in existing `pkg/git/branch_test.go` |
| BASE-01 | `--base-ref` plumb-through: buildCloneJob args + clone Job env; run branch created at resolved SHA | unit + envtest | `go test ./internal/controller/ -run TestBuildCloneJob`; Layer A spec | ❌ Wave 0/with-task |
| BASE-01 | End-to-end: Project with baseRef=feature-branch produces run branch based on it (hermetic git-http-server fixture — seed a second branch + tag) | kind (Layer B) | `make test-int` | ❌ extend `test/integration/kind/medium_http_test.go` fixture or sibling |
| BASE-02 | Clone-mode envelope written on failure (exit 2, reason `baseref-unresolvable`) and success (baseSHA present) | unit | `go test ./cmd/tide-push/ -run TestRunClone` | ❌ new cases in `cmd/tide-push/main_test.go` |
| BASE-02 | Controller classifies: condition set with ObservedGeneration, NO re-dispatch after Job GC; spec edit releases halt; other reasons keep re-dispatch | envtest | `make test-int-fast` (targeted Ginkgo spec) | ❌ new Layer A spec |
| BASE-03 | Fields present in both API versions (source + generated CRD YAML parity) | static unit | `go test ./api/...` | ❌ extend `api/v1alpha1/phase3_schema_test.go` pattern |
| BASE-03 | JSON round-trip v1alpha1⇄v1alpha2 preserves baseRef + baseSHA | unit | `go test ./api/...` | ❌ new |
| BASE-03 | Rendered `tide-crds` chart carries fields under BOTH version blocks (P8 lock); baseSHA stamped on running Project | plain go-test + envtest | `make test-int` (helm-render test in kind pkg runs without a cluster) | ❌ extend `test/integration/kind/projects_pvc_test.go` pattern |

### Sampling Rate
- **Per task commit:** `go test ./pkg/git/... ./cmd/tide-push/... ./api/... ./internal/controller/...` (targeted packages)
- **Per wave merge:** `make test` + `make test-int-fast`
- **Phase gate:** `make test-int` green (read `MAKE_EXIT` AND `grep -nE '^--- FAIL|^FAIL\s'` per CLAUDE.md — Ginkgo summary alone is insufficient) before `/gsd-verify-work`

### Wave 0 Gaps
- None structural — framework, envtest harness, kind suite, fixture helpers (`seedBareRepo`, `withGit`, git-http-server image) all exist. New test files/cases land with their implementation tasks (TDD). The only fixture extension is seeding a non-default branch + annotated/lightweight tags in the pkg/git and kind fixtures.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | (PAT handling unchanged; baseRef adds no auth surface) |
| V3 Session Management | no | — |
| V4 Access Control | no | (no new RBAC; clone Job SA unchanged) |
| V5 Input Validation | yes | kubebuilder `Pattern` + `MaxLength` on baseRef (charset allowlist, no leading `-`); resolution failure → typed condition, never raw stderr into status |
| V6 Cryptography | no | — |

### Known Threat Patterns for this change

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Argument injection via ref starting with `-` (e.g. `--upload-pack=...` class) | Tampering / Elevation | Pattern marker rejects leading `-`; baseRef only reaches exec-array Job args (never a shell) and pure go-git lookups — no `git` CLI receives it on the new path [VERIFIED: runClone's only CLI calls take fixed args + cfg.RunBranch, not baseRef] |
| Malicious ref name landing in condition message → log/UI injection | Tampering | Charset allowlist excludes control chars/newlines; message uses `%q`/fixed template |
| PAT leakage via new error paths | Information disclosure | Reuse `redactPAT` on any clone-path stderr (existing pattern at `main.go:272`); resolution errors contain only the ref and hash, never the URL/PAT |
| DoS via pathological pattern/CEL cost | DoS | MaxLength=250 bounds regex evaluation; no CEL rule added |

## Sources

### Primary (HIGH confidence)
- Working-tree code, all line-verified this session: `pkg/git/branch.go`, `pkg/git/clone.go`, `pkg/git/clone_test.go`, `cmd/tide-push/main.go` (flags, runClone, writePushEnvelope, classify*), `internal/controller/project_controller.go` (:58-109 envelope read, :560-628 clone arms), `internal/controller/push_helpers.go` (:205-374 both Job builders), `api/v1alpha{1,2}/project_types.go`, `api/v1alpha2/shared_types.go`, `api/v1alpha1/phase3_schema_test.go`, `test/integration/kind/{projects_pvc,medium_http,fixtures}_test.go`, `Makefile`, `charts/tide-crds/templates/project-crd.yaml`, `go.mod`
- go-git v5.19.0 tagged source (raw.githubusercontent.com): `plumbing/reference.go` (NewTag/NewBranchReferenceName, IsTag/IsBranch), `plumbing/hash.go` (IsHash, NewHash), `repository.go` (TagObject/CommitObject/Reference/ResolveRevision signatures; `cloneRefSpec`; `updateReferences`), `options.go` (TagMode constants; `Validate()` Tags defaulting; Mirror doc), `config/config.go` (`DefaultFetchRefSpec = "+refs/heads/*:refs/remotes/%s/*"`)

### Secondary (MEDIUM confidence)
- `.planning/research/ARCHITECTURE.md` §Q3/§Q7, `.planning/research/PITFALLS.md` §P8-P10, `.planning/research/FEATURES.md` §1, `.planning/research/STACK.md` — milestone research, itself source-verified 2026-07-03; spot-reverified against code this session (one divergence found and documented: the refs/remotes/origin layout, which Q3's "all heads + tags arrive" glossed over)

### Tertiary (LOW confidence)
- Assumptions A1, A3, A5 (training-knowledge mechanisms with cheap in-phase verification paths — see Assumptions Log)

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — zero new deps; every go-git API verified against the pinned tag's source
- Architecture: HIGH — all integration points read directly; the one CONTEXT.md gloss (ref layout) was caught and corrected with primary-source evidence
- Pitfalls: HIGH for 1-2 and 4-6 (source-verified); MEDIUM for 3 (timing-dependent, mitigations verified)

**Research date:** 2026-07-04
**Valid until:** ~2026-08-03 (stable domain; re-verify only if go-git or the clone path changes before planning executes)
