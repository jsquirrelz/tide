# Phase 35: Git Base Ref - Pattern Map

**Mapped:** 2026-07-04
**Files analyzed:** 10 modified (no new source files; new test cases land in existing/parallel test files)
**Analogs found:** 10 / 10 — every mechanism this phase needs already exists on the push path; the work is replicating proven contracts onto the clone path.

## File Classification

| Modified File | Role | Data Flow | Closest Analog (copy from) | Match Quality |
|---|---|---|---|---|
| `api/v1alpha2/project_types.go` (GitConfig.BaseRef, GitStatus.BaseSHA) | model (CRD types) | CRUD | `RepoURL` Pattern marker (:211) + `GitStatus` optional-field style (:234-256) same file | exact |
| `api/v1alpha1/project_types.go` (identical twins) | model (CRD types) | CRUD | its own v1alpha2 twin — copy verbatim (P9 both-versions rule) | exact |
| `api/v1alpha2/shared_types.go` (new Reason const) | model (constants) | — | `ConditionCloneFailed` block (:103-108) | exact |
| `pkg/git/branch.go` (`EnsureRunBranch` + resolution chain) | utility (git plumbing) | file-I/O | itself — extend, keep :54 idempotency early-return FIRST (Pitfall 6) | exact |
| `cmd/tide-push/main.go` (clone envelope, `--base-ref` flag, exit taxonomy) | CLI entrypoint | file-I/O + request-response (termination-log) | `writePushEnvelope` (:662-703), `pushResult` struct (:105), exit map (:36-46), `runClone` (:259-326) same file | exact |
| `internal/controller/push_helpers.go` (`buildCloneJob` termination wiring + `--base-ref` arg) | controller helper (Job builder) | batch dispatch | `buildPushJob` container termination-message block (:249-258); `buildCloneJob` own arg-append idiom (:298-303) | exact |
| `internal/controller/project_controller.go` (dispatch guard, WR-03 classify, baseSHA stamp) | controller (reconciler) | event-driven | own clone arms (:560-628), `readPushEnvelope` (:85-109), billing-halt condition stamp (`billing_halt.go:125-134`) | exact |
| `pkg/git/branch_test.go`, `pkg/git/clone_test.go` fixture | test | — | `seedBareRepo` helper (`clone_test.go:40`), `TestEnsureRunBranch_*` (`branch_test.go:30,54,75`) | exact |
| `api/*/` parity + round-trip tests | test (static) | — | `api/v1alpha1/phase3_schema_test.go` (regex-over-source + generated CRD YAML convention) | exact |
| `test/integration/kind/` helm-render + e2e | test | — | `projects_pvc_test.go` plain go-test helm-render style (:50-203); `medium_http_test.go` fixture for e2e | exact |
| `charts/tide-crds/`, `config/crd/bases/` | config (generated) | — | generated output — `make manifests && make helm-crds`; never hand-edit | n/a |
| `docs/project-authoring.md`, `docs/INSTALL.md` | docs | — | existing docs voice (declarative, em-dash-heavy per CLAUDE.md) | role-match |

## Pattern Assignments

### `api/v1alpha{1,2}/project_types.go` (model, CRUD)

**Analog:** `RepoURL` in the same `GitConfig` (`api/v1alpha2/project_types.go:206-212`) for a validated spec string; `CloneComplete` (`:251-255`) for a status provenance field.

**Validated spec field pattern** (v1alpha2:206-212):
```go
// RepoURL is the URL of the target git remote. Supports http:// and https://
// ...
// +kubebuilder:validation:Pattern=`^(https?://|git@).+`
RepoURL string `json:"repoURL"`
```
BaseRef adds `+optional`, `MinLength=1`, `MaxLength=250`, and the charset Pattern from RESEARCH.md §Code Examples (`^[A-Za-z0-9][A-Za-z0-9._+@/-]*$` — rejects leading `-`; field-level Pattern auto-guards absence, no CEL needed). NO `+kubebuilder:default` (P10). NO `oldSelf` rule (D-08).

**Status field pattern** (v1alpha2:251-255):
```go
// CloneComplete is true when the clone Job completed successfully.
// ...
// +optional
CloneComplete bool `json:"cloneComplete,omitempty"`
```
`BaseSHA string \`json:"baseSHA,omitempty"\`` follows the same shape. Add both fields to BOTH versions in the same task (v1alpha1 twins at the same struct positions; v1alpha1 is `+kubebuilder:unservedversion` at `api/v1alpha1/project_types.go:428` — no conversion webhook exists, strategy is None).

### `api/v1alpha2/shared_types.go` (constants)

**Analog:** `ConditionCloneFailed` (`shared_types.go:103-108`) — reuse this existing condition Type; add only a new Reason constant (e.g. `ReasonBaseRefUnresolvable`) modeled on `ReasonCreditBalanceTooLow` usage at `billing_halt.go:129`. One condition type per failure site; reasons distinguish classes. Existing `"CloneJobFailed"` reason keeps its delete-and-re-dispatch meaning.

### `pkg/git/branch.go` (utility, file-I/O)

**Analog:** itself. Current shape (lines 40-67): PlainOpen → **:53-56 existence early-return** → resolve HEAD (:58) → `Storer.SetReference` (:63).

**Idempotency pattern to preserve** (:53-56 — this MUST stay ahead of resolution, Pitfall 6; it is what makes D-09/D-10 free):
```go
refName := plumbing.NewBranchReferenceName(branch)
if _, err := repo.Reference(refName, false); err == nil {
    return nil // already exists — idempotent no-op
}
```

**Change:** signature gains `baseRef string`; when non-empty, replace the `repo.Head()` arm with the `resolveBaseRef` chain — the full verified skeleton is in RESEARCH.md §Code Examples (order: `refs/` verbatim w/ `refs/heads/`→`refs/remotes/origin/` fallback → branch local-then-remote → tag with `TagObject` peel → `IsHash` + `CommitObject`). Error wording: `unable to resolve %q to a commit SHA: ...`. Critical: go-git bare clones store non-default branches under `refs/remotes/origin/*` only (Pitfall 1 — load-bearing). Callers to update: `cmd/tide-push/main.go:298` and doc comments.

### `cmd/tide-push/main.go` (CLI, file-I/O + envelope)

**Analog:** push-mode machinery in the same file.

**Config + flag pattern** (`pushConfig` :83-98) — add `BaseRef string` alongside `RunBranch`; wire a `--base-ref` flag like `--run-branch`.

**Exit taxonomy** (file header :36-46): `0 success / 1 generic / 2 invariant / 10 leak / 11 lease / 12 auth / 13 network`. Assign `baseref-unresolvable` → **exit 2** with distinct reason (RESEARCH.md recommendation; exit 14 is claimed by Phase 34's `integration-incomplete`). Controller classifies on `envelope.reason`, not exit code.

**Envelope write pattern** (`writePushEnvelope` :662-703) — replicate for clone mode:
```go
data, err := json.Marshal(pr)
...
// Best-effort to /dev/termination-log (terminationMessagePath const at :655; 4096-byte cap)
if err := os.WriteFile(terminationMessagePath, data, 0o644); err != nil { ... low-signal log ... }
// Also to <workspace>/envelopes/push/<uid>.json, skip if ProjectUID == ""
```
Extend the envelope struct with `BaseSHA string \`json:"baseSHA"\``. Write the SUCCESS envelope on every clone success including default-HEAD runs (D-11). `runClone` (:259-326) currently writes NO envelope — the `// No envelope is written` comment at :257 becomes false; update it. Redact any stderr through `redactPAT` (existing use at :272).

### `internal/controller/push_helpers.go` (Job builder)

**Analog:** `buildPushJob` container block for the missing termination wiring, `buildCloneJob` for the arg idiom.

**Termination-message wiring to copy onto the clone container** (`push_helpers.go:249-258` — buildCloneJob's container at :343-364 lacks it today):
```go
TerminationMessagePath:   "/dev/termination-log",
TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
```

**Conditional-arg idiom** (`buildCloneJob` :298-303):
```go
if opts.RunBranch != "" {
    args = append(args, "--run-branch="+opts.RunBranch)
}
```
Add `BaseRef` to `CloneOptions` and `args = append(args, "--base-ref="+opts.BaseRef)` the same way.

### `internal/controller/project_controller.go` (reconciler, event-driven)

**Analog:** its own clone arms + the push-envelope reader + the billing-halt condition stamp.

**Envelope read pattern** (`readPushEnvelope` :85-109) — mirror for the clone Job (label `job-name=tide-clone-<uid>`): List pods by label → `ContainerStatuses[0].State.Terminated.Message` → `json.Unmarshal`; ANY failure → `(zero, false)` → generic path (this contract already absorbs Pitfall 5's non-JSON `FallbackToLogsOnError` messages).

**Dispatch guard** (:571) gains a third gate — skip dispatch when `CloneFailed=True && Reason==BaseRefUnresolvable && cond.ObservedGeneration == project.Generation` (the halt is the CONDITION, not Job existence — TTL GC defeats Job-existence halts, Pitfall 2). Plumb `cloneOpts.BaseRef = project.Spec.Git.BaseRef` next to the existing `RunBranch` wiring (:577-579).

**Set-on-success arm** (:594-600, `client.MergeFrom` + `r.Status().Patch` idiom) — read clone envelope FIRST; on ok, stamp `Status.Git.BaseSHA` and flip `CloneComplete` in the same patch (and set `CloneFailed=False`). On not-ok, do NOT flip; `RequeueAfter ~10s` with a sub-TTL cutoff (~60s via Job `status.completionTime`) then flip with empty baseSHA + log (RESEARCH.md Pattern 2 contract — the plan must state this explicitly).

**WR-03 classification** (:610-628) — the existing arm deletes + re-dispatches + stamps `CloneJobFailed`. New branch BEFORE that behavior: envelope reason == `baseref-unresolvable` → condition stamp per billing-halt pattern, NO delete, no requeue loop. Condition stamp to copy (`billing_halt.go:125-134`), with one required addition:
```go
patch := client.MergeFrom(project.DeepCopy())
meta.SetStatusCondition(&project.Status.Conditions, metav1.Condition{
    Type:               tidev1alpha2.ConditionCloneFailed,
    Status:             metav1.ConditionTrue,
    Reason:             "BaseRefUnresolvable",
    Message:            fmt.Sprintf("unable to resolve '%s' to a commit SHA; fix spec.git.baseRef to re-attempt the clone", baseRef),
    ObservedGeneration: project.Generation, // billing_halt.go does NOT set this — this site MUST (D-07 release-on-edit)
    LastTransitionTime: metav1.Now(),
})
return ctrl.Result{}, c.Status().Patch(ctx, project, patch)
```
All other reasons / unparseable envelope → existing delete-and-re-dispatch unchanged (auth-failed/network-timeout halting is an explicit non-goal).

### Tests

- **pkg/git unit:** extend `seedBareRepo` (`pkg/git/clone_test.go:40`) to seed a second branch + annotated & lightweight tags; new `TestEnsureRunBranch_*` cases follow `branch_test.go:30/54/75` naming and structure. Must cover: non-default branch via `refs/remotes/origin/` (Pitfall 1), annotated-tag peel, 40-hex SHA, `refs/` verbatim, rejection of `HEAD`/short-SHA/`~^`.
- **API static parity:** follow `api/v1alpha1/phase3_schema_test.go` — regex over both source packages AND the generated CRD YAML (both version blocks); add a JSON round-trip test v1alpha1⇄v1alpha2 (strategy-None conversion equivalent).
- **envtest (Layer A):** billing-halt/BYPASS-02 spec style — failed clone Job + envelope → condition set → delete Job (simulate TTL GC) → assert NO re-dispatch → bump spec → assert re-dispatch.
- **kind (Layer B):** helm-render plain go-test in `test/integration/kind/projects_pvc_test.go` style (e.g. `TestHelmDeploymentTemplateRendersManagerPodAnnotations` :140) asserting `baseRef`/`baseSHA` under BOTH version blocks of rendered `tide-crds`; e2e extends the `medium_http_test.go` git-http fixture with a second branch.

## Shared Patterns

### Envelope-on-termination-log transport
**Source:** `cmd/tide-push/main.go:655-703` (write) + `internal/controller/project_controller.go:58-109` (struct + read)
**Apply to:** clone-mode success AND failure paths. This is the ONLY Job→controller result channel (manager cannot mount project PVCs) — do not invent another.

### Status patch idiom
**Source:** `project_controller.go:595-599`
```go
patch := client.MergeFrom(project.DeepCopy())
// ... mutate status ...
r.Status().Patch(ctx, project, patch)
```
**Apply to:** baseSHA stamp, condition stamps, halt release.

### Typed-condition classification (classify-don't-retry)
**Source:** `internal/controller/billing_halt.go:104-135`
**Apply to:** WR-03 clone-failure classification. Delta from analog: set `ObservedGeneration` explicitly (generation-scoped halt release).

### Both-versions + generated-artifact regeneration
**Source:** convention locked by `api/v1alpha1/phase3_schema_test.go`
**Apply to:** every schema field: edit both `api/v1alpha{1,2}`, run `make manifests && make helm-crds`, never hand-edit `config/crd/bases/` or `charts/tide-crds/templates/`.

## No Analog Found

None. Every file has an exact in-repo analog (this phase is contract replication, not invention). Docs files are role-match only — follow the repo's declarative doc voice; content (accepted ref forms table, CRD-chart-first upgrade order) comes from D-01/D-02/D-03 and PITFALLS P8.

## Assumptions

- Headless run: excerpts and line numbers taken from the current worktree state and RESEARCH.md's same-day line-verified findings; RESEARCH.md skeletons (resolution chain, condition stamp) are treated as authoritative implementation shapes since they were verified against go-git v5.19.0 tagged source.
- Chart version bump deferred to Phase 36 per RESEARCH.md A2 (Phase 35 regenerates templates only) — planner should state this so the verifier doesn't flag the unbumped `Chart.yaml`.
- Envelope `Kind` string: keep the single struct / `PushResult` kind (least change) unless the planner prefers `CloneResult`; controller parses by reason/fields, not kind (RESEARCH.md Open Q2).

## Metadata

**Analog search scope:** `pkg/git/`, `cmd/tide-push/`, `internal/controller/`, `api/v1alpha{1,2}/`, `test/integration/kind/`
**Files scanned:** 12 read/grepped this session (all pre-verified by 35-RESEARCH.md the same day)
**Pattern extraction date:** 2026-07-04
