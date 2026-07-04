# Phase 37: Dashboard Surfaces — Artifact View, Project View, Log-Drawer States - Pattern Map

**Mapped:** 2026-07-04
**Files analyzed:** 18 new/modified files
**Analogs found:** 16 / 18 (2 partial — gitfetch, ResizeHandle)

**Assumptions (headless run):** file inventory taken from RESEARCH.md "Recommended Project Structure" plus CONTEXT.md reusable assets; Phase 34/36 have NOT landed in this worktree, so write-path excerpts (boundary_push.go, tide-push) reflect today's pre-34 shape — plans touching them must re-verify surfaces at execution start per RESEARCH "Dependency correction."

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `cmd/dashboard/api/artifacts.go` (new) | controller (chi handler) | request-response | `cmd/dashboard/api/plans.go` | exact |
| `cmd/dashboard/api/settings.go` (new) | controller (chi handler) | request-response | `cmd/dashboard/api/projects.go` | exact |
| `cmd/dashboard/api/logs_sse.go` (modify) | controller (SSE) | streaming | itself (`resolvePodName` widening) | exact |
| `cmd/dashboard/router.go` (modify) | route/config | request-response | itself (existing route registrations) | exact |
| `cmd/dashboard/gitfetch/` (new pkg) | service | file-I/O (git fetch + LRU) | `pkg/git/clone.go` | role-match (shallow/memfs variant is new) |
| `cmd/tide-push/main.go` (extend) | utility (CLI binary) | file-I/O | itself — `ArtifactPaths` staging loop | exact |
| `internal/controller/dispatch_helpers.go` (extend: `triggerArtifactPush`) | controller (reconciler helper) | event-driven | `spawnReporterIfNeeded` (same file) + `triggerBoundaryPush` (`boundary_push.go`) | exact |
| `internal/controller/boundary_push.go` (extend) | controller (reconciler helper) | event-driven | itself | exact |
| `dashboard/web/src/components/NodeDetailPanel.tsx` (new) | component | request-response | `TaskDetailDrawer.tsx` | exact |
| `dashboard/web/src/components/ArtifactViewer.tsx` (new) | component | request-response | `TaskDetailDrawer.tsx` + `PodLogStreamer.tsx` state placeholders | role-match |
| `dashboard/web/src/components/ProjectSettingsPanel.tsx` (new) | component | request-response | `TaskDetailDrawer.tsx` (MetaRow grid) | role-match |
| `dashboard/web/src/components/ApproveStrip.tsx` (new) | component | request-response | `TaskDetailDrawer.tsx` `actionsForStatus` + `ClipboardCopyAction` | exact |
| `dashboard/web/src/components/PodLogStreamer.tsx` (modify) | component | streaming | itself (placeholder pattern lines 209–235) | exact |
| `dashboard/web/src/components/ResizeHandle.tsx` (new) | component/hook | event-driven | no direct analog — hand-roll per RESEARCH | partial |
| `dashboard/web/src/components/NodeClickContext.tsx` (modify) | provider | event-driven | itself | exact |
| `dashboard/web/src/lib/api.ts` (extend) | utility (fetch layer) | request-response | itself (`fetchPlan`) | exact |
| `dashboard/web/src/lib/sse.ts` (modify) | hook | streaming | itself (named-listener block lines 194–202) | exact |
| `charts/tide/templates/dashboard-rbac.yaml` (modify) | config | — | itself (existing verb blocks + D-D2 comments) | exact |

## Pattern Assignments

### `cmd/dashboard/api/artifacts.go` + `settings.go` (chi handlers, request-response)

**Analog:** `cmd/dashboard/api/plans.go` (simplest single-GET handler) and `cmd/dashboard/api/projects.go` (shared helpers live here).

**Handler struct + imports** (`plans.go:30-49`):
```go
import (
    "fmt"; "net/http"
    "github.com/go-chi/chi/v5"
    "github.com/go-logr/logr"
    apierrors "k8s.io/apimachinery/pkg/api/errors"
    "sigs.k8s.io/controller-runtime/pkg/client"
    tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

type PlansHandler struct {
    Client client.Client
    Log    logr.Logger
}
```
New handlers add `Clientset kubernetes.Interface` for the fetch-time Secret read (R-02 — NEVER the cached `Client` for Secrets; see router.go Dependencies comment at `router.go:56-61` for precedent of a nil-tolerant Clientset field).

**Core GET pattern** (`plans.go:80-97`): `chi.URLParam(r, "name")`, `?namespace=` query default `"default"`, `h.Client.Get` → `apierrors.IsNotFound` → `writeError(w, http.StatusNotFound, ...)`; other errors → log + 500.

**Response shape convention** (`plans.go:54-73`, `projects.go:57-106`): lowercase-doc'd unexported structs with json tags, comments cross-referencing the mirroring frontend type. Pre-allocate slices with `make([]T, 0, n)` so empties serialize `[]` not `null` (`projects.go:145-148` — D-UI-SPEC empty-array contract). The artifacts endpoint must carry a typed state discriminator (`available|absent|no-git|error` per R-04), not an empty list.

**Error/JSON helpers — reuse, don't copy** (`projects.go:392-410`): `writeJSON` (Encoder with default HTML-escape — T-04-D1) and `writeError` are package-level in `projects.go`; new files in the same package call them directly.

**Doc-comment convention** (`plans.go:17-28`): file header names the endpoint, the plan/requirement ID, and restates the DASH-05 GET-only contract.

**Settings redaction:** follow `summarize()` (`projects.go:345-384`) — whitelist fields explicitly into the response struct; never marshal the raw Spec except the deliberate server-side `sigs.k8s.io/yaml` raw-spec string. Secret refs: names only.

**Tests:** mirror `cmd/dashboard/api/plans_test.go` / `projects_test.go` (fake client + httptest; table-driven states).

---

### `cmd/dashboard/router.go` (modify — new routes)

**Analog:** itself.

**Registration pattern** (`router.go:171-187`): construct handler with deps above the `r.Route("/api/v1", ...)` block, register inside it as `r.Get(...)`. Conditional registration on nil deps (`if eh != nil` / `if lh != nil`, lines 177-182) — the gitfetch-backed artifacts handler should follow this nil-tolerant pattern for tests. Update the route-table doc comment (lines 88-101). Every route MUST be `r.Get` — `TestZeroMutationRoutes` (`router_test.go`) walks the tree.

---

### `cmd/dashboard/gitfetch/` (new package, shallow fetch + LRU)

**Analog (partial):** `pkg/git/clone.go` — the repo's go-git auth + error-wrapping idiom.

**Auth pattern** (`clone.go:44-51`):
```go
repo, err := gogit.PlainCloneContext(ctx, destDir, true, &gogit.CloneOptions{
    URL: repoURL,
    Auth: &gitclient.BasicAuth{
        Username: "x-access-token", // GitHub convention; GitLab/Gitea accept any non-empty username
        Password: pat,
    },
})
```
Also copy the `pat == ""` → anonymous/no-auth guard (`clone.go:64-69`) — plain-http in-cluster remotes reject empty BasicAuth. Error wrapping: `fmt.Errorf("git clone %s: %w", repoURL, err)`.

**Divergences from the analog (per RESEARCH Pattern 3):** use `gogit.CloneContext` with `memory.NewStorage()` (not PlainClone), `Depth: 1, SingleBranch: true, Tags: gogit.NoTags, NoCheckout: true, ReferenceName: plumbing.NewBranchReferenceName(branch)`; fresh clone per new tip SHA — never Fetch into a shallow clone. LRU: `hashicorp/golang-lru/v2` (promote to direct in go.mod). Wrap the fetcher in an interface seam (CLI fallback swappability). Creds: typed clientset `Secrets(ns).Get` at fetch time, key `GIT_PAT` (mirrors `cmd/tide-push/main.go:270,409`).

---

### `cmd/tide-push/main.go` (extend — `--stage-envelopes`)

**Analog:** itself — the existing `ArtifactPaths` staging loop.

**Staging + loud-failure pattern** (`main.go:452-474`):
```go
for _, rel := range cfg.ArtifactPaths {
    src := filepath.Join(cfg.Workspace, rel)
    dst := filepath.Join(worktreeDir, rel)
    if err := copyIntoWorktree(src, dst); err != nil {
        writePushEnvelope(cfg, "", exitGenericFail, "artifact-copy-failed")
        fmt.Fprintf(stderr, "tide-push: copy artifact %s -> worktree: %v\n", rel, err)
        return exitGenericFail
    }
    if err := pkggit.AddPath(wt, rel); err != nil {
        writePushEnvelope(cfg, "", exitGenericFail, "stage-failed")
        ...
    }
}
```
New flag stages src→**different** dst (`.tide/planning/<destPrefix>/...`) — the existing flag's identical-rel copy is exactly why it can't be reused verbatim (RESEARCH Pattern 1). Failure convention: `writePushEnvelope(cfg, "", exitGenericFail, "<typed-reason>")` + stderr line + nonzero exit — use reason `artifact-stage-failed` (D-03 loud failure). The clean-tree skip (`main.go:488-528`, `worktreeClean` → push HEAD without empty commit) makes cumulative re-staging idempotent — do not bypass it. Commit identity is `tideBotSignature()` (`main.go:521,128-131`) today; Phase 36 makes it configurable — inherit, don't fork. Flag parsing: mirror `splitCSV(*artifactPaths)` (`main.go:166`).

---

### `internal/controller/` — `triggerArtifactPush` (new helper)

**Analogs:** `spawnReporterIfNeeded` (`dispatch_helpers.go:58-106`) for placement/idempotency; `triggerBoundaryPush` (`boundary_push.go:58-134`) for the push-Job construction.

**Guard chain** (`boundary_push.go:71-91`): nil project → return nil; `project.Spec.Git == nil || RepoURL == ""` → silent skip; empty `tidePushImage` → `logger.Info` skip (CR-02: Info, not V(1)).

**Idempotent deterministic-Job dispatch** (`boundary_push.go:93-129`):
```go
pushJobName := fmt.Sprintf("tide-push-%s", project.UID)
var existing batchv1.Job
getErr := c.Get(ctx, types.NamespacedName{Name: pushJobName, Namespace: project.Namespace}, &existing)
if getErr == nil { return nil } // already in flight — Phase 34 single-flight; cumulative map self-heals
if !apierrors.IsNotFound(getErr) { return fmt.Errorf("get push job %s: %w", pushJobName, getErr) }
...
pushJob := buildPushJob(project, pvcName, pushOpts, scheme)
if cErr := c.Create(ctx, pushJob); cErr != nil && !apierrors.IsAlreadyExists(cErr) { ... }
```
`PushOptions` carries `Branch: project.Status.Git.BranchName, LastPushedSHA: project.Status.Git.LastPushedSHA, LeaksConfigMap: project.Spec.Git.LeaksConfigRef` (`boundary_push.go:115-122`) — add the new StageEnvelopes map field beside these. Per-reconciler thin entry points at the bottom of the file (`maybeTriggerBoundaryPush` receivers, lines 200-223) are the shape for per-level `triggerArtifactPush` wrappers. Metrics: `tidemetrics.PushJobsTotal.WithLabelValues(...)` (`boundary_push.go:132`).

**Placement:** in `handleJobCompletion` immediately after `spawnReporterIfNeeded`, at all four level controllers; PLUS the `AwaitingApproval` early-return arm with `RequeueAfter` (RESEARCH Pitfall 8 — e.g. `milestone_controller.go:256` parked branch).

**⚠ Precondition:** Phase 34 rewrites these internals (single-flight gate, verify-in-push, lastPushedSHA stamping). Excerpts above are the pre-34 shape — re-read `boundary_push.go`/`push_helpers.go` at execution start.

---

### `dashboard/web/src/components/NodeDetailPanel.tsx` / `ArtifactViewer.tsx` / `ProjectSettingsPanel.tsx` / `ApproveStrip.tsx`

**Analog:** `TaskDetailDrawer.tsx` (a11y-complete right panel — generalize, don't rewrite).

**Panel chrome + a11y** (`TaskDetailDrawer.tsx:237-267`): backdrop div (`fixed inset-0 z-40`, click-closes, `color-mix` surface) + panel div `role="dialog" aria-modal aria-labelledby` with:
```tsx
className={clsx(
  "fixed top-0 right-0 z-50 flex h-full flex-col overflow-y-auto",
  "bg-[var(--color-surface-raised)] text-[var(--color-text-primary)]",
  "border-l border-[var(--color-border-subtle)] shadow-xl",
)}
style={{ width: "420px", transition: "transform 180ms cubic-bezier(0.4, 0, 0.2, 1)", transform: "translateX(0)" }}
```
The hardcoded `width: "420px"` is the D-06 resize target — lift to state driven by the ResizeHandle hook.
Copy wholesale: focus capture/restore effect (lines 184-200), Escape-close effect (203-210), Tab focus-trap `onTrapKey` (213-231 with `FOCUSABLE_SELECTOR` at 168-169), `if (!isOpen || !data) return null` guard (233).

**Metadata cards** (`TaskDetailDrawer.tsx:308-334, 395-431`): `<dl>` grid of `MetaRow` (label uppercase 11px muted / value 13px optional mono+truncate) — the ProjectSettingsPanel settings-card idiom.

**ApproveStrip** — command-button pattern (`TaskDetailDrawer.tsx:89-99, 336-349`):
```tsx
// actionsForStatus: single source of truth for status × command copy
case "AwaitingApproval":
  return [{ variant: "primary", label: "Approve", command: `tide approve ${proj}` }, ...];
// render:
{actions.map((a) => (
  <ClipboardCopyAction key={a.label} command={a.command} label={a.label} variant={a.variant} />
))}
```

**ArtifactViewer states:** reuse `EmptyState.tsx`/`ErrorState.tsx`/`LoadingState.tsx` components (CONTEXT reusable assets) for `absent`/`error`/`loading`; markdown via `react-markdown@10.1.0 + remark-gfm@4.0.1`, NO `rehypePlugins` (XSS default), `components` prop mapped to `var(--font-mono)` etc. tokens (RESEARCH code example); JSON via `JSON.stringify(x, null, 2)` in a `pre`. Styling idiom throughout: CSS custom properties (`var(--color-*)`, `var(--font-mono)`) + Tailwind utility classes + `clsx` from `../lib/clsx`, `data-testid` on every stateful element.

---

### `dashboard/web/src/components/PodLogStreamer.tsx` (modify — four-state model)

**Analog:** itself — extend the mutually-exclusive placeholder block (`PodLogStreamer.tsx:209-235`):
```tsx
{lines.length === 0 && state === "connecting" && (
  <div data-testid="pod-log-placeholder" style={{ color: "var(--color-text-muted)" }}>
    {COPY_CONNECTING}
  </div>
)}
```
Locked-copy constants at top of file (`COPY_CONNECTING/WAITING/DISCONNECTED`, lines 43-45) — add `COPY_POD_GONE` ("Logs no longer available — pod garbage-collected.", D-13, message only) and error copy + Reconnect button (D-14). **The missing case:** `state === "reconnecting"` currently renders nothing — add a visible placeholder (Pitfall 1).

### `dashboard/web/src/lib/sse.ts` (modify — named terminal events)

**Analog:** itself — the project-event named-listener registration (`sse.ts:194-202`):
```ts
es.onmessage = handler;
for (const type of SSE_PROJECT_EVENT_TYPES) {
  es.addEventListener(type, handler as EventListener);
}
```
Root cause: log-stream terminal events (`pod-gone`/`error`/`idle-timeout`) are named frames never registered here. Add per RESEARCH code example: `onNamedRef`-style callback option mirroring `onMessageRef` (`sse.ts:127-128` ref-held-callback idiom so identity changes don't reopen the stream). Terminal-state contract: on `pod-gone`, clear `timerRef` and do NOT call `scheduleReconnect` — study `onerror` (`sse.ts:204-224`, owns close-exactly-once + `scheduleReconnect`) and the cleanup contract (`244-260`). `useTaskLog` (`282-317`) grows the terminal states in its returned `TaskLogState`.

### `dashboard/web/src/components/NodeClickContext.tsx` (modify)

**Analog:** itself (23 lines). Change `createContext<(name: string) => void>` → `(kind: TideNodeKind, name: string) => void`; keep the no-op default for provider-less unit tests. `TideNodeShell.tsx` already receives `kind` — pass through; set `clickable` for all Planning-DAG kinds (today Plan-only).

### `dashboard/web/src/lib/api.ts` (extend — `fetchNodeArtifacts`, `fetchProjectSettings`)

**Analog:** itself — `fetchPlan` (`api.ts:171-184`):
```ts
export async function fetchPlan(name: string, namespace?: string): Promise<PlanDetail> {
  const url = withNamespace(`/api/v1/plans/${encodeURIComponent(name)}`, namespace);
  const res = await fetch(url);
  if (!res.ok) throw new Error(`fetchPlan(${name}) failed: ${await readError(res)}`);
  return (await res.json()) as PlanDetail;
}
```
Reuse `withNamespace` + `readError` helpers (`api.ts:58-72`). Type comments cite the mirrored Go struct (`/** Mirrors cmd/dashboard/api/plans.go::planDetail */`). Artifact response type carries the R-04 state discriminator union.

### `cmd/dashboard/api/logs_sse.go` (modify — `resolvePodName` widening)

**Analog:** itself (`logs_sse.go:274-298`): widen the phase switch `case corev1.PodRunning, corev1.PodPending:` to include `PodSucceeded, PodFailed` (terminated-but-present pods still serve logs; EOF → existing `pod-gone` frame at line 248 ends honestly). Terminal-frame emitter: `writeSSEEvent(w, flusher, "pod-gone", `{}`)` (`logs_sse.go:303-305`) — the frontend contract to match.

### `charts/tide/templates/dashboard-rbac.yaml` (modify)

**Analog:** itself. Add `secrets` with verbs `["get"]` ONLY (passes `make helm-rbac-assert` ⊆{get,list,watch}); mirror the existing D-D2 commentary block style documenting the expansion (Argo-Server artifact-creds analogue, per R-02).

### `dashboard/web/src/components/ResizeHandle.tsx` (new — no analog)

Hand-rolled per RESEARCH (react-resizable-panels rejected: SUS flag + layout-refactor cost). No in-repo drag precedent; nearest conventions: hook style of `sse.ts` (refs + effects), a11y discipline of `TaskDetailDrawer` (`role="separator"`, arrow keys), persistence via `localStorage` (browser-only, per Responsibility Map). ~60 LOC shared hook driving panel width + log-area height.

## Shared Patterns

### JSON response + error helpers (Go)
**Source:** `cmd/dashboard/api/projects.go:392-410` (`writeJSON`/`writeError`)
**Apply to:** artifacts.go, settings.go — call directly (same package), never re-implement. Encoder HTML-escape stays on (T-04-D1).

### GET-only + read-only invariants
**Source:** `cmd/dashboard/router.go` doc comments + `router_test.go` `TestZeroMutationRoutes`; `make helm-rbac-assert`
**Apply to:** every new route and RBAC change. Also: Secrets via typed clientset only (Pitfall 4).

### Empty-array-not-null serialization
**Source:** `projects.go:145-148` (`make([]T, 0, n)` pre-allocation)
**Apply to:** all new list-shaped response fields.

### Idempotent deterministic-Job dispatch
**Source:** `boundary_push.go:93-129` and `dispatch_helpers.go:85-103` (Get → NotFound → Create → tolerate AlreadyExists)
**Apply to:** `triggerArtifactPush`.

### Typed envelope reason + nonzero exit (loud failure, D-03)
**Source:** `cmd/tide-push/main.go:464-473` (`writePushEnvelope(cfg, "", exitGenericFail, "<reason>")`)
**Apply to:** all new tide-push failure paths.

### Panel a11y kit (focus trap / Escape / focus restore)
**Source:** `TaskDetailDrawer.tsx:168-231`
**Apply to:** NodeDetailPanel and any panel-shaped surface.

### Design-token styling + data-testid
**Source:** `TaskDetailDrawer.tsx` / `PodLogStreamer.tsx` throughout (`var(--color-*)`, `var(--font-mono)`, `clsx`, `data-testid` per stateful element)
**Apply to:** all new components; UI-SPEC (`04-UI-SPEC.md`) holds the copywriting contract for state copy.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| `ResizeHandle.tsx` | component/hook | event-driven | No drag-interaction precedent in the codebase; hand-roll per RESEARCH (library rejected) |
| `gitfetch` shallow/memfs specifics | service | file-I/O | `pkg/git/clone.go` covers auth/idiom but repo has no in-memory shallow clone; follow RESEARCH Pattern 3 skeleton |

## Metadata

**Analog search scope:** `cmd/dashboard/`, `cmd/tide-push/`, `internal/controller/`, `pkg/git/`, `dashboard/web/src/{components,lib}/`, `charts/tide/templates/`
**Files scanned:** ~50 listed; 12 read in depth
**Pattern extraction date:** 2026-07-04
