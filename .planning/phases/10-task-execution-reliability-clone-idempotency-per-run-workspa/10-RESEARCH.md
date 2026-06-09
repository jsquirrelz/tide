# Phase 10: Task-Execution Reliability — Research

**Researched:** 2026-06-08
**Domain:** Go/go-git, Kubernetes pod securityContext, LLM output robustness, dashboard API routing
**Confidence:** HIGH (all four forks resolved from direct codebase reads)

## Summary

All four defects are pre-existing code-level bugs, first exposed by the Phase 9 run that legitimately reached real task execution. No new architecture is required — each fix is a targeted change to an existing package. The research resolves each fork with a specific recommendation grounded in the actual source files.

Fork (a) — clone idempotency: `pkg/git/Clone()` calls `gogit.PlainCloneContext` which returns `gogit.ErrRepositoryAlreadyExists` when `destDir` already holds a bare repo. The fix is a skip-if-exists / fetch-into-existing pattern using `gogit.PlainOpen` + `repo.FetchContext`, both of which are already exercised elsewhere in `pkg/git/`. [VERIFIED: codebase read of `pkg/git/clone.go`, `pkg/git/fetch.go`, `cmd/tide-push/main.go`]

Fork (b) — per-run workspace permissions: `buildCloneJob` and `buildPushJob` in `internal/controller/push_helpers.go` emit no `securityContext` on the pod spec. The planner/executor job builder `internal/dispatch/podjob/BuildJobSpec` already sets `FSGroup: 1000` on its `PodSecurityContext` — the push/clone builders missed it. The fix is adding `PodSecurityContext{FSGroup: ptr.To(int64(1000))}` in the binary (not in values.yaml, which is a fixed chart contract). [VERIFIED: codebase read of `push_helpers.go`, `internal/dispatch/podjob/jobspec.go`, `charts/tide/values.yaml`]

Fork (c) — child-CRD parse robustness: `readChildCRDs` in `internal/subagent/anthropic/subagent.go` returns on the first `json.Unmarshal` error, losing all valid children alongside the bad file. The planner prompt template (`internal/subagent/common/templates/plan_planner.tmpl`) is clear and well-structured but does not prevent an LLM from writing trailing prose after a closing `}`. The fix is per-file isolation (skip-bad-log-continue, not abort-all) combined with a prompt patch that wraps the JSON in a markdown code fence instruction. [VERIFIED: codebase read of `subagent.go`, `plan_planner.tmpl`, `childcrd_read_test.go`]

Fork (d) — dashboard project-detail 404: `ProjectsHandler.Get` (line in `cmd/dashboard/api/projects.go`) defaults `namespace = "default"` when the `?namespace=` query param is absent, while `ProjectsHandler.List` uses all-namespaces by default. A multi-namespace project (`tide-sample-medium`) 404s on the detail call because the namespace mismatch makes the `client.Get` miss. The fix is a cross-namespace lookup fallback in `Get` that mirrors the `List` semantics. [VERIFIED: codebase read of `cmd/dashboard/api/projects.go`, `cmd/dashboard/router.go`, existing test `TestGetProjectWithChildren`]

**Primary recommendation:** Fix all four defects in a single phase (they share no ordering dependency). SC-3 (EnvelopeIn.Branch threading) is already merged and confirmed wired through task_controller → BuildJobSpec → claude-subagent. No re-work needed there.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Clone idempotency | API / Backend (pkg/git) | cmd/tide-push (caller) | git operations live in pkg/git; caller (runClone) picks up the fix transparently |
| Workspace permissions | API / Backend (job builder) | Kubernetes kubelet | FSGroup is a pod-spec field emitted by the controller binary; kubelet applies chown at mount time |
| Child-CRD parse robustness | API / Backend (subagent runner) | Prompt template | Primary: per-file error isolation in Go code; secondary: prompt guidance to prevent malformed output |
| Dashboard detail 404 | Frontend Server (dashboard API) | — | Pure Go handler bug in chi route handler; no frontend change needed |

## Standard Stack

### Core (all already in go.mod — no new dependencies)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| github.com/go-git/go-git/v5 | v5.19.0 | git operations | Already vendored; `PlainOpen` + `FetchContext` are stable APIs used in `pkg/git/fetch.go` |
| k8s.io/utils/ptr | bundled with controller-runtime | typed pointer helpers | `ptr.To(int64(1000))` already used in `internal/dispatch/podjob/jobspec.go` |
| encoding/json | stdlib | JSON parse | Already used throughout |
| sigs.k8s.io/controller-runtime/pkg/client | v0.24.x | K8s client | Already used in dashboard handlers |

**No new dependencies required for any of the four fixes.** [VERIFIED: go.mod inspection via existing imports in affected files]

## Fork (a): Clone Idempotency

### Current Behavior

`pkg/git/Clone()` wraps `gogit.PlainCloneContext(ctx, destDir, true, &CloneOptions{...})`. go-git v5.19.0 returns `ErrRepositoryAlreadyExists` (exported sentinel at `repository.go:58`) when it detects a non-empty storer at `destDir`. This fires on:
- A Job retry (backoffLimit=2 in `buildCloneJob`): the first attempt populates `destDir`, the second attempt hits the same path.
- A warm PVC: a prior run's `repo.git` survives a pod restart.

Evidence from live run: `tide-push: clone failed: ... repository already exists` — even on a freshly-wiped PVC because the clone Job's backoffLimit retries compound.

### Options

| Option | How | Downside |
|--------|-----|---------|
| Skip-if-exists (open-and-return) | Detect `ErrRepositoryAlreadyExists` → call `gogit.PlainOpen` → return the opened repo | Does not refresh the remote; stale if remote has new commits between runs |
| Fetch-into-existing | Detect `ErrRepositoryAlreadyExists` → `PlainOpen` → `repo.FetchContext(ctx, &FetchOptions{Auth: ...})` → return | Adds a network round-trip on every retry but guarantees the bare repo is current |
| Clean-and-reclone | `os.RemoveAll(destDir)` then re-clone | Requires write permission on the PVC root (nonroot pods may lack it); also loses in-flight worktrees |

### Recommendation

**Fetch-into-existing.** The pattern is: detect `errors.Is(err, gogit.ErrRepositoryAlreadyExists)` → call `gogit.PlainOpen(destDir)` → call `pkggit.Fetch(ctx, repo, pat)` (already implemented in `pkg/git/fetch.go`) → return the opened repo. This is idempotent on any number of retries, keeps the bare repo current, and reuses existing `pkg/git` abstractions. The implementation lives entirely in `pkg/git/Clone()` — no change to callers (`runClone` in `cmd/tide-push/main.go` remains a single `pkggit.Clone` call).

**Code sketch** (in `pkg/git/clone.go`):
```go
repo, err := gogit.PlainCloneContext(ctx, destDir, true, &gogit.CloneOptions{...})
if errors.Is(err, gogit.ErrRepositoryAlreadyExists) {
    repo, err = gogit.PlainOpen(destDir)
    if err != nil {
        return nil, fmt.Errorf("git open existing %s: %w", destDir, err)
    }
    if ferr := Fetch(ctx, repo, pat); ferr != nil {
        return nil, fmt.Errorf("git fetch existing %s: %w", destDir, ferr)
    }
    return repo, nil
}
if err != nil {
    return nil, fmt.Errorf("git clone %s: %w", repoURL, err)
}
return repo, nil
```

The test `TestCloneSucceeds` in `pkg/git/clone_test.go` must gain a companion `TestCloneIdempotent` that calls `Clone` twice on the same `destDir` and asserts the second call returns nil error. [VERIFIED: `pkg/git/fetch.go` `Fetch()` signature; `gogit.ErrRepositoryAlreadyExists` sentinel at `go-git/v5@v5.19.0/repository.go:58`]

## Fork (b): Per-Run Workspace Permissions

### Current Behavior

The push/clone Job pods (built by `buildPushJob` / `buildCloneJob` in `internal/controller/push_helpers.go`) have no `PodSecurityContext` at all — no `FSGroup`, no `RunAsUser`. The Dockerfile for `tide-push` runs as a nonroot user. When the push Job pod first writes to `/workspace/envelopes/push/` under a fresh PVC subPath, the directory is owned by root (default for newly-provisioned PV) and the nonroot process gets EACCES.

By contrast, `internal/dispatch/podjob/BuildJobSpec` (planner/executor jobs) already sets:
```go
SecurityContext: &corev1.PodSecurityContext{
    FSGroup: new(int64(1000)),
},
```
at `jobspec.go:403-406`. This kubelet-applied `fsGroup` chowns the mounted volume to gid 1000 at pod startup, making all subdirectories writable by the nonroot container without a manual `chown`.

The chart `values.yaml` manager `podSecurityContext` has `runAsNonRoot: true` but no `fsGroup` — this is for the controller-manager pod, not the Job pods. The chart is a fixed contract (CLAUDE.md); the fix belongs in the Go binary, not in `values.yaml`.

### Options

| Option | How | Trade-off |
|--------|-----|-----------|
| `fsGroup` on PodSecurityContext | Add `FSGroup: ptr.To(int64(1000))` to `buildPushJob` and `buildCloneJob` pod specs | K8s-idiomatic; kubelet applies the chown automatically at mount time; no new container |
| initContainer chown | Add a `busybox:stable` init container that runs `chown -R 1000:1000 /workspace` | Requires a container image, adds startup latency, and chown of the full PVC is expensive if large |
| Dedicated init Job | A separate one-time Job that pre-chowns the PVC subpath | Already the pattern that failed (the one-time assumption; breaks when PVC is wiped/re-provisioned) |
| `runAsUser` on container spec only | Set `RunAsUser: 1000` on the container `SecurityContext` | Does NOT fix directory ownership — the PV root is still owned by root; only `fsGroup` triggers kubelet chown |

### Recommendation

**`fsGroup: 1000` on the PodSecurityContext of both `buildPushJob` and `buildCloneJob`.** This is the exact pattern already working in `BuildJobSpec`. The fix is two additions to `push_helpers.go` — one for `buildPushJob` and one for `buildCloneJob`:

```go
podSpec := corev1.PodSpec{
    SecurityContext: &corev1.PodSecurityContext{
        FSGroup: ptr.To(int64(1000)),
    },
    RestartPolicy:      corev1.RestartPolicyNever,
    ServiceAccountName: pushSAName,
    ...
}
```

Note: `ptr.To` is already imported via `k8s.io/utils/ptr` in `jobspec.go`; `push_helpers.go` currently uses `new(int32(...))` — either helper is acceptable, but match the file's existing style (`new(int32(2))` for BackoffLimit). For `int64` use `ptr.To(int64(1000))` or equivalently `func() *int64 { v := int64(1000); return &v }()`. [VERIFIED: `push_helpers.go` has no SecurityContext; `jobspec.go:403-406` has the working pattern; `values.yaml` confirms chart is FIXED contract]

## Fork (c): Child-CRD Parse Robustness

### Current Behavior

`readChildCRDs` in `internal/subagent/anthropic/subagent.go` iterates over `*.json` files in the children dir. On `json.Unmarshal` failure for any file, it returns the error immediately (line 437-438):

```go
if jerr := json.Unmarshal(data, &spec); jerr != nil {
    return nil, fmt.Errorf("parse child file %q: %w", name, jerr)
}
```

This aborts the entire read: all children are lost even if only `task-03.json` was malformed. The calling path in the same file (lines 324-332) surfaces this as `out.ExitCode = 1` and `out.Reason = "read child CRDs: parse child file..."`, making the planner dispatch fail entirely. The evidence from the live run: `parse child file "task-03.json": invalid character 'W' after object key:value pair` — 22726 output tokens consumed, 27c spent, entire dispatch wasted.

The root cause is likely LLM-generated trailing prose after the closing `}` of the JSON object, or an embedded natural-language field value that itself contains `"key": value` without proper quoting. The `plan_planner.tmpl` instructs the model to write a single JSON object per file and provides a schema — the instruction is clear, but the model violated it.

### Options

| Option | Benefit | Downside |
|--------|---------|---------|
| Per-file isolation (skip-bad, log, continue) | Valid children (task-01, task-02) succeed; only bad files are skipped | Partial children may produce an incomplete task graph; the orchestrator must detect count mismatch |
| Strict abort-all (current behavior) | Surfaces the error immediately | Wastes the entire dispatch; all valid children lost |
| JSON repair / `json.Decoder` lenient mode | Could recover from trailing prose | Go's `encoding/json` has no built-in repair; third-party libs add complexity |
| Prompt constraint + retry | Prevent the malformed output upstream | LLMs are probabilistic; cannot guarantee; needs a fallback |
| Prompt patch + per-file isolation (layered) | Belt-and-suspenders | Combines prevention (prompt) with recovery (per-file error surface) |

### Recommendation

**Layered approach: per-file isolation + prompt constraint + retriable error surface.**

1. **Per-file isolation in `readChildCRDs`**: on `json.Unmarshal` failure for a specific file, record the error in a `[]parseErr` slice and continue to the next file. Return the valid children AND a dedicated `PartialParseError` (wrapping the per-file errors) so the caller can distinguish "all good" from "some bad" from "all bad". Update the calling path (lines 324-332) to surface a retriable error when `PartialParseError` contains parse failures, rather than treating it as a hard stop.

2. **Prompt patch in `plan_planner.tmpl`**: add an explicit constraint in the "HOW TO EMIT THE CHILD CRDS" section:
   - "The file MUST contain ONLY the JSON object — no prose, no markdown, no explanation before or after the `{...}` block."
   - Consider adding a JSON schema reference (the full `spec` field names are already listed; just emphasize no trailing text).

3. **`ExitCode` as retriable vs hard failure**: the current caller marks the whole dispatch `ExitCode=1` on any parse error. With per-file isolation, the controller can retry the planner dispatch if all parse errors are for files that are JSON-syntactically invalid (recoverable via re-dispatch) as opposed to kind-allowlist violations (which would recur on retry). Mark parse failures as `ExitCode=2` (retriable) vs allowlist violations as `ExitCode=1` (permanent), so the reconciler's existing error-classification logic can route accordingly.

The `TestReadChildCRDs_RejectsMalformedJSON` test in `childcrd_read_test.go` currently expects a hard error return. It must be updated to expect per-file isolation behavior (valid siblings returned + error slice). [VERIFIED: `subagent.go:437-438` hard-abort; `plan_planner.tmpl` prompt text; `childcrd_read_test.go:145` existing malformed-JSON test]

## Fork (d): Dashboard Project-Detail 404

### Current Behavior

`ProjectsHandler.Get` (in `cmd/dashboard/api/projects.go`) contains:

```go
namespace := r.URL.Query().Get("namespace")
if namespace == "" {
    namespace = "default"
}
```

`ProjectsHandler.List` contains no such default — it uses all-namespaces when the `namespace` param is absent. The frontend navigates to `/api/v1/projects/medium-project` (no namespace param) after seeing the project in the list (which finds it in `tide-sample-medium` via all-namespace List). The detail call then looks for `{Namespace: "default", Name: "medium-project"}` which does not exist → 404.

The `/api/v1/projects/{name}/events` EventsHandler performs a project existence pre-check via `deps.Client` (wired in `router.go` via `WithClient(deps.Client)`) — that SSE endpoint uses `client.List` with no namespace filter, which is why events 200 while detail 404s.

### Options

| Option | How | Notes |
|--------|-----|-------|
| Remove the `"default"` fallback; require `?namespace=` | Return 400 if namespace is absent | Breaks existing tests that don't pass namespace; requires frontend change |
| Cross-namespace search fallback | If `client.Get` with namespace="default" returns NotFound, fall back to `client.List` across all namespaces and find by name | Handles missing param gracefully; consistent with List semantics |
| Require the frontend to always send `?namespace=` | Frontend change only | Correct solution for production, but dashboard codebase is out of current scope |
| Return the first match across all namespaces when namespace is unspecified | Replaces the "default" fallback with a list-and-find | Correct, deterministic, no frontend change needed |

### Recommendation

**All-namespace fallback in `Get`**: when `namespace == ""`, perform a `client.List` across all namespaces and return the first project whose `Name == name`. This is consistent with `List`'s behavior, requires no frontend change, and handles the multi-namespace topology TIDE is designed for. The implementation:

```go
namespace := r.URL.Query().Get("namespace")
if namespace != "" {
    // Fast path: namespace explicitly specified.
    var p tidev1alpha1.Project
    if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &p); err != nil {
        // ... existing error handling
    }
    // ... build and return detail
} else {
    // Cross-namespace fallback: list all projects and find by name.
    var projects tidev1alpha1.ProjectList
    if err := h.Client.List(ctx, &projects); err != nil {
        writeError(w, http.StatusInternalServerError, ...)
        return
    }
    var p *tidev1alpha1.Project
    for i := range projects.Items {
        if projects.Items[i].Name == name {
            p = &projects.Items[i]
            break
        }
    }
    if p == nil {
        writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", name))
        return
    }
    // ... build and return detail (using p.Namespace for child List calls)
}
```

A new test `TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces` must be added to `cmd/dashboard/api/projects_test.go` exercising a project in a non-default namespace without a `?namespace=` param. [VERIFIED: `projects.go` Get handler; `router.go` route registration `r.Get("/projects/{name}", ph.Get)`; `projects_test.go` existing tests all pass `?namespace=default` explicitly]

## SC-3: Executor Worktree Branch (Confirmation)

The 09-09 `EnvelopeIn.Branch` fix is merged and confirmed wired end-to-end:

- `internal/controller/task_controller.go:1068`: stamps `Branch: project.Status.Git.BranchName` into `EnvelopeIn` at dispatch time.
- `internal/dispatch/podjob/jobspec.go:251`: `BuildJobSpec` encodes `EnvelopeIn` as base64 JSON into the envelope-writer init container's env var.
- `cmd/claude-subagent/main.go:84`: reads `env.Branch` and passes it to `harness.EnsureWorktree(env, workspaceRoot, env.Branch)`.
- `internal/harness/worktree.go:80`: passes `branch` to `pkggit.AddWorktree(bareRepoPath, in.TaskUID, branch)`.

No re-work needed for SC-3. The only dependency is that SC-1 (clone idempotency, fork a) must complete first so `repo.git` exists when `EnsureWorktree` runs. [VERIFIED: all four files read directly]

## SC-5 / SC-6: Legitimate Medium Complete + Push + Retag (Validation Outcomes)

These are validation outcomes, not separate code changes:
- SC-5 (legitimate medium Complete + push): after fixes (a) + (b) + (c) land, a fresh medium-sample re-run is expected to drive all Tasks to Succeeded, trigger the level-boundary push, and record `costSpentCents > 0`.
- SC-6 (v1.0.0 retag unblock): after SC-5 is observed, the user manually deletes and re-creates the `v1.0.0` tag at the post-Phase-10 HEAD (confirm-only per MEMORY.md).

No code change is required for either. The plan should include a "run medium DoD" task as the final wave.

## Orphaned-Task Finalizer-Release (Minor)

`internal/finalizer/finalizer.go` already handles the orphan case correctly: `HandleDeletion` force-removes the finalizer on `context.DeadlineExceeded`, preventing indefinite Terminating-state. Tasks have `BlockOwnerDeletion: true` owner references to their parent Plan — K8s cascade deletion handles the orphan case when a Plan is deleted. No code change is needed; this is already correct. [VERIFIED: `task_controller.go:137-141`, `finalizer.go`]

## Common Pitfalls

### Pitfall 1: `Fetch` Against Anonymous In-Cluster Remote Requires Empty Auth
**What goes wrong:** `pkggit.Fetch` always constructs `&gitclient.BasicAuth{Username: "x-access-token", Password: pat}`. When `pat == ""` (anonymous in-cluster `http://` remote), go-git may send a Basic auth header with an empty password, which some git servers reject.
**How to avoid:** In the idempotent-clone path, call `Fetch` with a nil auth option when `pat == ""` (identical to what `Clone` allows for public repos). Check `pkg/git/fetch.go` to confirm the nil-auth path.
**Warning signs:** `git fetch: authentication required` in the push-mode clone Job logs against an anonymous server.

### Pitfall 2: `ptr.To` vs `new()` Inconsistency in push_helpers.go
**What goes wrong:** `push_helpers.go` uses `new(int32(2))` for `BackoffLimit`; `jobspec.go` uses `ptr.To`. Both compile; mixing styles causes lint noise.
**How to avoid:** Match the file's existing style in the file being edited. `push_helpers.go` uses `new()`; add `new(int64(1000))` for FSGroup there.

### Pitfall 3: Per-File Isolation Must Not Lose Traversal Defense
**What goes wrong:** Loosening the error return in `readChildCRDs` for parse errors might accidentally loosen the traversal defense (symlink/path-escape checks). These are separate branches.
**How to avoid:** The `return nil, fmt.Errorf(...)` for traversal violations must remain hard-abort. Only the `json.Unmarshal` and `json.Unmarshal`-surface kind/name validation failures become per-file-skip.

### Pitfall 4: Dashboard Get Must Use p.Namespace for Child Listing
**What goes wrong:** After the all-namespace fallback finds a Project in `tide-sample-medium`, the child List calls (`h.Client.List(ctx, &ms, client.InNamespace(p.Namespace))`) must use `p.Namespace`, not the original (empty) `namespace` variable.
**How to avoid:** Refactor the detail-build logic into a shared helper `buildDetail(ctx, p)` that is called from both the explicit-namespace path and the cross-namespace fallback path. The helper always uses `p.Namespace`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Idempotent clone | Custom PVC-wipe + reclone logic | `gogit.PlainOpen` + `pkggit.Fetch` | Already implemented in `pkg/git/fetch.go`; network-refreshes the repo |
| Per-run dir ownership | initContainer with `chown -R` | `fsGroup` on PodSecurityContext | Kubelet applies at mount time; no extra container or image pull |
| JSON repair for LLM output | Custom tokenizer/repair loop | Per-file skip + prompt constraint | Repair is fragile; skip-and-continue gives partial results; prompt reduces frequency |

## Validation Architecture

### Test Framework

| Property | Value |
|----------|-------|
| Framework | Go testing + Ginkgo v2.28 |
| Config file | `Makefile` targets (`make test`, `make test-int`) |
| Quick run command | `go test ./pkg/git/... ./internal/subagent/anthropic/... ./cmd/dashboard/api/...` |
| Full suite command | `make test` (unit) or `make test-int` (kind integration) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SC-1 | Clone into existing repo returns nil | unit | `go test ./pkg/git/ -run TestCloneIdempotent` | ❌ Wave 0 |
| SC-1 | Clone Job retry sequence succeeds | unit | `go test ./cmd/tide-push/ -run TestCloneMode` | ✅ (extend) |
| SC-2 | Push/clone pods get FSGroup=1000 | unit | `go test ./internal/controller/ -run TestBuildCloneJobFSGroup` | ❌ Wave 0 |
| SC-4 | Malformed child JSON does not abort valid siblings | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_PartialParse` | ❌ Wave 0 |
| SC-4 | Existing malformed JSON test updated | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_RejectsMalformedJSON` | ✅ (update) |
| SC-7 | `GET /api/v1/projects/{name}` without namespace finds cross-ns project | unit | `go test ./cmd/dashboard/api/ -run TestGetProjectWithoutNamespace` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/git/... ./internal/subagent/anthropic/... ./cmd/dashboard/api/...`
- **Per wave merge:** `make test`
- **Phase gate:** `make test` green + manual medium-sample DoD re-run before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/git/clone_test.go` — add `TestCloneIdempotent` (calls `Clone` twice on same destDir, asserts nil error second call)
- [ ] `internal/controller/push_helpers_test.go` — add `TestBuildCloneJobFSGroup` / `TestBuildPushJobFSGroup` asserting `PodSecurityContext.FSGroup == 1000`
- [ ] `internal/subagent/anthropic/childcrd_read_test.go` — add `TestReadChildCRDs_PartialParse` (valid task-01.json + malformed task-02.json → task-01 returned + error for task-02); update `TestReadChildCRDs_RejectsMalformedJSON` to match new per-file behavior
- [ ] `cmd/dashboard/api/projects_test.go` — add `TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces` (project in non-default namespace, no `?namespace=` param, expect 200)

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| minikube | Phase DoD (medium E2E) | ✓ | v1.34.0 | — |
| kubectl | Phase DoD | ✓ | v1.34.1 | — |
| Go | Build + unit tests | ✓ | go1.26.3 | — |
| go-git v5.19.0 | Clone idempotency fix | ✓ | v5.19.0 (in go.mod) | — |

## Security Domain

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes (child JSON) | `json.Unmarshal` + kind allowlist in `readChildCRDs` — maintained in per-file-skip path |
| V6 Cryptography | no | — |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Traversal via malformed child JSON filename | Tampering | Traversal defense (symlink reject, path-escape check) remains a hard-abort; only parse errors become per-file-skip |
| FSGroup chown exposes PVC data to other pods | Information Disclosure | Per-Project PVC subPath isolation (`{project.UID}/workspace`) already enforced at mount time; FSGroup does not cross subPath boundaries |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `ptr.To[int64]` is importable in `push_helpers.go` via `k8s.io/utils/ptr` (same dep as jobspec.go) | Fork (b) | Low — `k8s.io/utils` is already a transitive dep; if not directly listed, use `func() *int64 { v:=int64(1000); return &v }()` |
| A2 | The live medium-sample run 404 was observed without `?namespace=` from the frontend | Fork (d) | Medium — if the frontend does pass `?namespace=` and the 404 is from a different cause, the fix may be elsewhere; but the code bug exists regardless |

## Open Questions

1. **`Fetch` with empty PAT against anonymous in-cluster remote**
   - What we know: `pkggit.Fetch` always populates `BasicAuth`; `pat == ""` is valid for public/anonymous remotes.
   - What's unclear: whether go-git sends a Basic auth header with empty password against `http://` anonymous servers (the in-cluster demo git-http-server).
   - Recommendation: test `Clone` idempotency against the in-cluster `http://` remote with `pat=""` during the Phase 10 DoD run; add a `TestFetchAnonymous` unit test using `seedBareRepo` + local `file://` URL as a proxy.

2. **Partial-parse vs retry threshold in the controller**
   - What we know: the reconciler marks a planner dispatch failed on `ExitCode=1`; currently no retry logic distinguishes parse-error from allowlist-violation.
   - What's unclear: whether the controller should auto-retry a planner that returned `PartialParseError` (some valid children, some bad).
   - Recommendation: for Phase 10, surface the partial-parse as a failed level (fail-fast); the plan author gets a clear error and can re-trigger manually. Full retry logic can be a Phase 11 improvement.

## Sources

### Primary (HIGH confidence)
- [VERIFIED: codebase] `pkg/git/clone.go` — `Clone()` implementation, single call to `PlainCloneContext`
- [VERIFIED: codebase] `pkg/git/fetch.go` — `Fetch()` implementation, `PlainOpen` + `FetchContext` pattern
- [VERIFIED: codebase] `internal/controller/push_helpers.go` — `buildCloneJob`, `buildPushJob` — no SecurityContext
- [VERIFIED: codebase] `internal/dispatch/podjob/jobspec.go:403-406` — `FSGroup: new(int64(1000))` working pattern
- [VERIFIED: codebase] `internal/subagent/anthropic/subagent.go:437-438` — hard-abort on parse error
- [VERIFIED: codebase] `internal/subagent/common/templates/plan_planner.tmpl` — prompt JSON schema
- [VERIFIED: codebase] `cmd/dashboard/api/projects.go` — `Get` handler `namespace = "default"` default
- [VERIFIED: codebase] `cmd/dashboard/router.go` — route registration
- [VERIFIED: codebase] `cmd/claude-subagent/main.go:84` — `env.Branch` threaded to `EnsureWorktree`
- [VERIFIED: codebase] `internal/controller/task_controller.go:1068` — `Branch:` field stamped
- [VERIFIED: go-git source] `go-git/v5@v5.19.0/repository.go:58` — `ErrRepositoryAlreadyExists` sentinel

### Secondary (MEDIUM confidence)
- [CITED: .planning/debug/09-07-premature-succession-evidence.md] — live runtime evidence for all three task-execution defects; exact error strings

## Metadata

**Confidence breakdown:**
- Fork (a) clone idempotency: HIGH — code read directly; go-git sentinel verified in module cache
- Fork (b) workspace perms: HIGH — missing SecurityContext confirmed by direct read; working pattern in same codebase
- Fork (c) child-JSON robustness: HIGH — hard-abort at exact line confirmed; prompt template read
- Fork (d) dashboard 404: HIGH — handler bug at exact line confirmed; test behavior cross-checked

**Research date:** 2026-06-08
**Valid until:** 2026-07-08 (stable stack; no external moving parts)
