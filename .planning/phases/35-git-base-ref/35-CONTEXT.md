# Phase 35: Git Base Ref - Context

**Gathered:** 2026-07-03
**Status:** Ready for planning

<domain>
## Phase Boundary

Operators can base a run on any branch, tag, or SHA via an optional `spec.git.baseRef` — an unresolvable ref fails fast with a typed condition (classify-don't-retry), and the resolved base SHA is stamped in `status.git.baseSHA` across both API versions with conversion round-trip and CRD upgrade-path tests (BASE-01, BASE-02, BASE-03). Clone-path-only change; the milestone's first CRD schema change, whose chart bump batches with Phase 36's signing config per the FIXED-contract rule.

</domain>

<decisions>
## Implementation Decisions

### Accepted ref forms (the documented contract)
- **D-01:** Resolution is an **explicit chain**: `refs/heads/<ref>` → `refs/tags/<ref>` (peel annotated tags to the commit) → full 40-hex SHA. NOT go-git `ResolveRevision`. `HEAD`, short SHAs, and `~`/`^` suffixes are rejected — the docs enumerate exactly three short forms.
- **D-02:** A value starting with `refs/` is resolved **verbatim, before the chain** — the explicit disambiguation escape hatch for branch/tag name collisions, and it avoids the Argo Workflows `refs/heads/*`-rejection surprise (argo-workflows#5629).
- **D-03:** A SHA not reachable from any fetched ref (the bare clone fetches all heads + tags) is **unresolvable — same typed condition**, with the message noting SHAs must be reachable from a branch or tag. No targeted-fetch machinery, no host-dependent behavior.

### Failure timing & surface
- **D-04:** The unresolvable-ref check fires **in the clone Job only** (`EnsureRunBranch` is the single resolution site). No controller-side ls-remote preflight — no new manager→git-remote egress, no second resolution site to drift. Failure still surfaces before any subagent spend.
- **D-05:** Clone mode adopts the **existing push-mode envelope contract**: small JSON envelope written to the PVC and `/dev/termination-log`; the ProjectReconciler parses the pod termination message. New `envelope.reason: baseref-unresolvable` extends the existing exit-code/reason taxonomy (exit 2 invariant / 10 leak-detected / 11 lease-rejected / 12 auth-failed / 13 network-timeout). Clone mode currently writes NO envelope (`cmd/tide-push/main.go:257`) — adding one is in scope.
- **D-06:** The controller **classifies** the clone-Job failure: `baseref-unresolvable` → typed condition (e.g. `CloneFailed`/`BaseRefUnresolvable`), and the current delete-and-re-dispatch-forever arm (`internal/controller/project_controller.go:610-628`) must NOT re-dispatch for this class. Condition message follows the Argo CD canonical wording: names the bad ref, "unable to resolve '<ref>' to a commit SHA".

### Recovery & mutability
- **D-07:** Recovery is **edit-spec-and-re-attempt**: the condition halts clone re-dispatch for the current generation; a spec edit (new generation) clears it and re-runs the clone. Classify-don't-retry holds — the same bad ref is never hot-looped. A typo costs one `kubectl edit`, not a Project recreate.
- **D-08:** **No CEL immutability rule on baseRef** — it would block the D-07 recovery edit (spec CEL can't see status) and research P10 warns `oldSelf` transition rules are never ratcheted and fire on adopted/imported objects.
- **D-09:** baseRef edits **after a successful clone are inert, documented only** — `EnsureRunBranch` idempotency (existing run branch untouched) makes this true mechanically. CRD field comment + operator docs state it; NO edit-detection logic, NO as-used-ref stamp in status. (User explicitly chose docs-only over an observable event/condition signal.)
- **D-10:** Adoption/import path needs **no special case**: an adopted Project's run branch already exists, so baseRef is inert there — same documented semantics as D-09.

### baseSHA stamping
- **D-11:** `status.git.baseSHA` is stamped **on every run**, including default-HEAD runs with no baseRef set — reproducibility provenance (the ref can move after run start; Argo CD `status.sync.revision` pattern). Transport: the clone **success** envelope carries the resolved SHA back to the controller (the manager cannot mount project PVCs). Annotated tags stamp the peeled commit SHA.

### Claude's Discretion
- CEL safe-charset validation on the field (PITFALLS security note: reject argument-injection-shaped refs) — shape and strictness at planning/implementation time; must guard absence (`!has(...) || ...`).
- Exit-code assignment for `baseref-unresolvable` within the tide-push taxonomy.
- Exact condition type/reason identifiers, and where the docs for accepted forms live (CRD field comment vs INSTALL/usage doc — likely both).
- Stamping timing (same status patch as `CloneComplete` is the natural spot).

### Folded Todos
- **`2026-07-03-git-baseref-run-branch.md` — "Add spec.git.baseRef so runs can branch off a non-default ref."** Original problem: `EnsureRunBranch` (`pkg/git/branch.go:40`) always creates the run branch at the bare clone's HEAD; the first external run (2026-07-03) needed a run based on an unmerged hotfix branch and had no option but merging it to main first. This IS the phase — its solution sketch (CRD field, plumb through clone Job env, resolve in `EnsureRunBranch`, reject unresolvable at reconcile with a clear condition) is refined by D-01..D-11 above. Verified clean of private/company data before folding.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap
- `.planning/REQUIREMENTS.md` §Git Base Ref — BASE-01/02/03 acceptance shape
- `.planning/ROADMAP.md` §Phase 35 — goal, success criteria, chart-bump batching with Phase 36

### Milestone research (v1.0.7)
- `.planning/research/ARCHITECTURE.md` §Q3 — the verified plumbing chain: `GitConfig` field → clone dispatch (`project_controller.go:571-580`) → `buildCloneJob` args (`push_helpers.go:292-303`) → `runClone` (`cmd/tide-push/main.go:259-326`) → `EnsureRunBranch` (`pkg/git/branch.go:40`); the no-envelope + infinite-re-dispatch findings
- `.planning/research/PITFALLS.md` §P8/P9/P10 — CRD chart skew, conversion round-trip drop, CEL/defaulting traps (no `+kubebuilder:default`; absent = HEAD, one encoding); plus the baseRef argument-injection security note
- `.planning/research/FEATURES.md` §1 — ecosystem survey (Dependabot/Renovate/Argo CD/Tekton), Argo CD `targetRevision` as the canonical shape, no-silent-fallback anti-feature
- `.planning/research/STACK.md` — go-git v5.19.0 covers everything; no new deps for this phase

### Origin capture
- `.planning/todos/pending/2026-07-03-git-baseref-run-branch.md` — folded todo (see Folded Todos)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `pkg/git/branch.go:40` `EnsureRunBranch` — the single resolution site to extend; today hardcodes `repo.Head()` at :58; already idempotent (existing run branch untouched), which is what makes D-09/D-10 free
- `cmd/tide-push/main.go` push-mode envelope (`pushResult` JSON → PVC `envelopes/push/<uid>.json` + `/dev/termination-log`) — the contract clone mode adopts (D-05); exit-code/reason taxonomy documented at `main.go:41-46`
- `internal/controller/push_helpers.go:250-258` — `TerminationMessagePath`/`FallbackToLogsOnError` container wiring to replicate on the clone container
- `api/v1alpha2/project_types.go:205` `GitConfig` / `:234` `GitStatus` — where `BaseRef` (spec) and `BaseSHA` (status) land; v1alpha1 twins at `api/v1alpha1/project_types.go:205/:234`

### Established Patterns
- Envelope-on-termination-log is how Job results reach the controller (the manager cannot mount project PVCs) — do not invent a new transport
- Both-API-versions + conversion functions + round-trip test for every new field (P9); v1alpha2 is storage version
- Chart is FIXED contract: the CRD schema addition rides a chart version bump, batched with Phase 36's (one bump for both)
- Typed-condition classification of Job failures (billing-400 halt precedent) — extend, don't bypass

### Integration Points
- `internal/controller/project_controller.go:571-580` — clone dispatch gains the baseRef plumb-through (Job env/args)
- `internal/controller/project_controller.go:610-628` — the terminal-failed clone arm that must learn to classify (D-06) instead of delete-and-re-dispatch forever
- `pkg/git/clone.go:44` — full bare clone (all heads + tags), no CloneOptions change needed for branch/tag refs

</code_context>

<specifics>
## Specific Ideas

- Error wording modeled on Argo CD: `unable to resolve '<ref>' to a commit SHA` — named canonical during discussion
- Docs must enumerate the accepted forms explicitly (branch, tag, full SHA, `refs/`-qualified) — the Argo Workflows qualified-ref rejection was cited as the surprise to avoid

</specifics>

<deferred>
## Deferred Ideas

- **Targeted SHA fetch for unreachable commits** (e.g. PR-head SHAs via `uploadpack.allowAnySHA1InWant`) — rejected for v1.0.7 as host-dependent behavior; revisit only if operators hit the documented limit in practice
- **Observable signal on post-clone baseRef edits** (event/condition + as-used-ref stamp) — considered and explicitly declined in favor of docs-only (D-09); a future ergonomics pass could revisit
- **`tide apply --base-ref` CLI flag** — not raised in discussion, not in BASE-01..03; CRD-only this phase

### Session logistics (parallel discuss agents)
- This phase's artifacts land on local branch `worktree-discuss-phase-35`; the operator merges all per-phase discuss branches locally. **STATE.md was deliberately not updated** by this session (shared-write conflict across parallel agents) — run one `state.record-session` (or let the first `/gsd-plan-phase` do it) after merging.

</deferred>

---

*Phase: 35-Git Base Ref*
*Context gathered: 2026-07-03*
