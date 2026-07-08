---
phase: 37-dashboard-surfaces-artifact-view-project-view-log-drawer-sta
reviewed: 2026-07-08T11:32:40Z
depth: standard
files_reviewed: 22
files_reviewed_list:
  - charts/tide/templates/dashboard-rbac.yaml
  - cmd/dashboard/api/artifacts.go
  - cmd/dashboard/api/logs_sse.go
  - cmd/dashboard/api/settings.go
  - cmd/dashboard/gitfetch/gitfetch.go
  - cmd/dashboard/gitfetch/store.go
  - cmd/dashboard/main.go
  - cmd/dashboard/router.go
  - cmd/tide-push/main.go
  - internal/controller/artifact_push.go
  - internal/controller/boundary_push.go
  - internal/controller/push_helpers.go
  - internal/controller/plan_controller.go
  - dashboard/web/src/App.tsx
  - dashboard/web/src/lib/api.ts
  - dashboard/web/src/lib/sse.ts
  - dashboard/web/src/components/ArtifactViewer.tsx
  - dashboard/web/src/components/PodLogStreamer.tsx
  - dashboard/web/src/components/ProjectSettingsPanel.tsx
  - dashboard/web/src/components/ResizeHandle.tsx
  - dashboard/web/src/components/NodeDetailPanel.tsx
  - test/integration/kind/artifact_staging_test.go
findings:
  critical: 1
  warning: 3
  info: 2
  total: 6
status: issues_found
---

# Phase 37: Code Review Report

**Reviewed:** 2026-07-08T11:32:40Z
**Depth:** standard
**Files Reviewed:** 22 (deep) + remaining phase files swept for anti-patterns
**Status:** issues_found

## Summary

Reviewed the DASH-01/02/03/04 dashboard surfaces, the gitfetch read path, the
controller artifact-push trigger, and the tide-push staging binary. The security
posture on the flagged high-risk surfaces is genuinely strong and the mitigations
are load-bearing rather than decorative:

- **Secret handling** — `settings.go` holds no clientset and projects secret refs
  by NAME only; `rawSpecYAML` marshals a Spec that carries no values; the
  `ProjectSettingsPanel` renders the prompt and YAML in `<pre>`, never markdown.
  `artifacts.go` reads the git-creds Secret via a one-shot typed clientset GET
  (no informer) and the PAT never enters a cache key/value or a log line. Verified
  no secret-value escape path.
- **XSS** — `ArtifactViewer` uses react-markdown with only `remarkGfm`, no
  rehype-raw; JSON goes through `JSON.stringify` into a `<pre>`; `PodLogStreamer`
  renders ANSI as styled `<span>`s. No `dangerouslySetInnerHTML` anywhere.
- **SSE terminal logic** (`sse.ts`) — the terminal-frame handler correctly
  distinguishes named `event: error` SSE frames (MessageEvent → terminal, suppress
  reconnect) from transport `error` Events (plain Event → owned by `onerror`), and
  breaks the auto-backoff loop exactly once. The DASH-04 infinite-reconnect
  regression is properly closed.
- **RBAC** — verbs are strictly `{get,list,watch}`; the `helm-rbac-assert` gate
  enforces read-only. (See WR-01 on scope breadth.)
- **tide-push** `parseStageEnvelopes` fail-closes with a regex + post-`filepath.Clean`
  containment gate against traversal; PAT redaction strips raw + URL-encoded forms.

Note during review: `new(int64(100))` / `new(int32(2))` in `logs_sse.go` and
`push_helpers.go` are **not** bugs — Go 1.26 (the pinned toolchain) added
`new(expr)` returning a pointer to a copy of the value. Verified by compiling in
isolation (`*int64` → 100) and `go vet` passes clean on all changed packages.

One blocking correctness defect: the pod-log stream does not forward the selected
project's namespace, so the log drawer is broken for every project outside the
`default` namespace — including the sample projects the product ships.
**→ RESOLVED during the execute-phase code-review gate** (commit `3e90d6c` fix +
`b92820d` embed-dist rebuild; freshness gate re-verified PASS, vitest 266 green).
WR-01, WR-02, and IN-01 remain open as advisory findings for human judgment.

## Critical Issues

### CR-01: Pod-log stream is namespace-blind — falsely reports "pod garbage-collected" for live pods in non-default namespaces  — ✅ RESOLVED (commits 3e90d6c + b92820d)

**File:** `dashboard/web/src/lib/sse.ts:456-457`, `dashboard/web/src/components/PodLogStreamer.tsx:747-750` (App.tsx mount), `cmd/dashboard/api/logs_sse.go:167-170`

**Issue:** `useTaskLog` builds the stream URL with no namespace:
```ts
const url = `/api/v1/tasks/${encodeURIComponent(taskName)}/log`;
```
and the `PodLogStreamer` mount passes only `taskName` (its props have no namespace
field). The backend then defaults the namespace:
```go
namespace := r.URL.Query().Get("namespace")
if namespace == "" {
    namespace = "default"
}
```
`resolvePodName` does `Client.Get(ctx, {Namespace: "default", Name: taskName}, &task)`
→ `IsNotFound` → returns `("", nil)` → the handler emits `event: pod-gone`. For any
Task whose Project lives outside `default`, the drawer therefore renders
`"Logs no longer available — pod garbage-collected."` for a healthy, running pod.

This is not hypothetical: `App.tsx:56-62` documents this exact class of bug for the
plan/task fetches and fixes it by deriving `selectedNamespace` from the projects
list and forwarding it — explicitly citing the shipped `tide-sample-medium` /
`tide-sample-small` namespaces. The DASH-04 log path was not given the same
treatment, so the phase's headline deliverable (eliminate silent-empty / misleading
terminal states) ships regressed for the supported multi-namespace configuration,
and does so with the single most misleading of the terminal messages (claims GC of
a live pod).

**Fix:** Thread the namespace through the hook and append it to the URL, mirroring
the fetch-side fix the app already documents:
```ts
export function useTaskLog(taskName: string, namespace?: string): TaskLogResult {
  const base = `/api/v1/tasks/${encodeURIComponent(taskName)}/log`;
  const url = namespace
    ? `${base}?namespace=${encodeURIComponent(namespace)}`
    : base;
  // ...
}
```
Add `namespace?: string` to `PodLogStreamerProps`, pass
`namespace={selectedNamespace ?? undefined}` at the App.tsx:747 mount, and forward
it into `useTaskLog(taskName, namespace)`. (The backend already reads `?namespace=`
correctly — only the client is missing it.)

## Warnings

### WR-01: Dashboard ClusterRole grants cluster-wide `secrets: get` — large blast radius

**File:** `charts/tide/templates/dashboard-rbac.yaml:61-66`

**Issue:** The rule is read-only (get only — the `⊆{get,list,watch}` invariant holds,
good), but it is a **ClusterRole**, so the dashboard ServiceAccount can read *any*
Secret in *any* namespace, not just per-Project git-creds Secrets. Because the SA can
also `list` Projects cluster-wide, it can enumerate every git-creds Secret name and
read it. The API handlers never return secret values, so this is not directly
exploitable through the HTTP surface, but it makes the dashboard pod a
cluster-wide-secret-exfil primitive if the process is ever compromised. RBAC `get`
cannot be scoped by label selector, and Secret names are dynamic, so tight scoping is
awkward — but the grant could at least be a per-namespace `Role`/`RoleBinding` created
per Project namespace, or the design decision to keep it cluster-wide should be
explicitly recorded as an accepted risk.

**Fix:** Prefer namespaced `Role`s bound in each Project namespace over a cluster-wide
`secrets: get`. If cluster-wide is a deliberate tradeoff, document it as an accepted
risk in the chart comment (the current comment justifies read-only, not
cluster-wide-all-secrets).

### WR-02: gitfetch clone / ls-remote have no timeout — a slow Project remote hangs the artifacts handler

**File:** `cmd/dashboard/api/artifacts.go:170`, `cmd/dashboard/gitfetch/gitfetch.go:108-152`, `cmd/dashboard/main.go:174-180`

**Issue:** `Store.Artifacts` passes `r.Context()` straight into `ls-remote` and the
shallow clone. The dashboard `http.Server` sets only `ReadHeaderTimeout` (correctly no
`WriteTimeout`, since the SSE endpoints must stay open), so there is no per-request
handler deadline. The Project's `repoURL` is operator-supplied and points at an
arbitrary external git remote; a slow or hung remote makes the artifacts handler block
indefinitely, holding the goroutine until the client disconnects. `Depth:1 /
SingleBranch` bounds transfer size but not connect/read time.

**Fix:** Bound the outbound git work explicitly in the handler:
```go
ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
defer cancel()
sha, files, err := h.Store.Artifacts(ctx, proj.Spec.Git.RepoURL, branch, auth)
```
(A hit still short-circuits before the clone, so the timeout only bites the network
path.)

### WR-03: `settings.go` cross-namespace fallback returns a nondeterministic first name-match

**File:** `cmd/dashboard/api/settings.go:127-139`

**Issue:** When `?namespace=` is absent, the handler `List`s all Projects and returns
the first whose `.Name == name`. If two Projects share a name across namespaces, which
one's settings are served depends on list ordering — an operator could be shown (and
act on) the wrong Project's config. This mirrors the existing `projects.go` pattern, so
it is not new behavior, but the settings panel is a higher-consequence surface (repo
URL, gate policy, budget caps) than the summary list.

**Fix:** When the name is ambiguous, prefer requiring `?namespace=` (the frontend
already has `selectedNamespace` and forwards it elsewhere), or 409 on multiple matches
rather than silently picking one.

## Info

### IN-01: artifact-push and boundary-push share the deterministic Job name — residual timing coupling

**File:** `internal/controller/artifact_push.go:212`, `internal/controller/boundary_push.go:94`

**Issue:** Both triggers create/serialize on `tide-push-<project.UID>`. The 37-06
preemption bug is mitigated well — the artifact push fires on gate-*park* arms only
(never the succeed/approve path, per `plan_controller.go:709-712`), both push classes
now carry the full cumulative `StageEnvelopes` map, and every push (artifact or
boundary) pushes the run-branch HEAD, so wave-integration merges are already local via
the separately-named `tide-push-wave-*` jobs. The residual: if an artifact-push Job is
still within its 300s TTL when a plan-boundary push fires, the boundary push no-ops
("already in flight") for that tick and relies on requeue + TTL-GC to eventually land
the boundary commit. No concrete data-loss path was provable (the merges are local and
subsequent boundary pushes carry them), but the shared-name coupling is fragile enough
to warrant a regression test that asserts the plan-boundary integration push is not
permanently swallowed by an in-TTL artifact-push Job.

**Fix:** Add an integration/envtest case: artifact-push Job present + within TTL at plan
boundary → assert the run branch HEAD (with task-branch merges) reaches the remote
before the Plan is marked Succeeded (via requeue).

### IN-02: `settings.go` `repo.baseRef` is permanently `""`

**File:** `cmd/dashboard/api/settings.go:53-57,151-153`

**Issue:** `BaseRef` is hardcoded empty pending the Phase-35 `Spec.Git.BaseRef` field.
Documented, and the UI renders `"HEAD (default)"`, so it degrades gracefully — but it is
silent dead wiring that will misreport a configured base ref once that field lands.

**Fix:** Wire `settings.Repo.BaseRef = p.Spec.Git.BaseRef` (guarded by the existing
`p.Spec.Git != nil` check) as soon as the field exists; until then leave a tracked TODO
so it is not forgotten.

---

_Reviewed: 2026-07-08T11:32:40Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
