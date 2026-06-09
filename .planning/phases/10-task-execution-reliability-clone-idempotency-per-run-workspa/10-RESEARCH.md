# Phase 10: Task-Execution Reliability — Research

**Researched:** 2026-06-08
**Domain:** Go/go-git, Kubernetes pod securityContext, LLM output robustness, dashboard API routing
**Confidence:** HIGH (all four forks resolved from direct codebase reads)

## Summary

All four defects are pre-existing code-level bugs, first exposed by the Phase 9 run that legitimately reached real task execution. No new architecture is required — each fix is a targeted change to an existing package. The research resolves each fork with a specific recommendation grounded in the actual source files.

Fork (a) — clone idempotency: `pkg/git/Clone()` calls `gogit.PlainCloneContext` which returns `gogit.ErrRepositoryAlreadyExists` when `destDir` already holds a bare repo. The fix is a skip-if-exists / fetch-into-existing pattern using `gogit.PlainOpen` + `repo.FetchContext`, both of which are already exercised elsewhere in `pkg/git/`. [VERIFIED: codebase read of `pkg/git/clone.go`, `pkg/git/fetch.go`, `cmd/tide-push/main.go`]

Fork (b) — per-run workspace permissions: `buildCloneJob` and `buildPushJob` in `internal/controller/push_helpers.go` emit no `securityContext` on the pod spec. The planner/executor job builder `internal/dispatch/podjob/BuildJobSpec` already sets `FSGroup: 1000` on its `PodSecurityContext` — the push/clone builders missed it. The fix is adding `PodSecurityContext{FSGroup: ptr.To(int64(1000))}` in the binary (not in values.yaml, which is a fixed chart contract). [VERIFIED: codebase read of `push_helpers.go`, `internal/dispatch/podjob/jobspec.go`, `charts/tide/values.yaml`]

Fork (c) — child-CRD parse robustness: `readChildCRDs` in `internal/subagent/anthropic/subagent.go` returns on the first `json.Unmarshal` error, losing all valid children alongside the bad file. A full format comparison (JSON, YAML, XML, TOML, plain-text, MCP tool-call) was conducted against the observed failure. The verdict is: keep JSON as the file format, add a `json.Decoder`-based balanced-brace extractor in `readChildCRDs` to tolerate the observed trailing-prose failure class, and tighten the prompt. YAML is a worse default for this shape; MCP tool-call is the right long-term fix but is out of Phase-10 scope due to the `--bare` flag constraint. [VERIFIED: codebase reads + external research; see section below]

Fork (d) — dashboard project-detail 404: `ProjectsHandler.Get` (line in `cmd/dashboard/api/projects.go`) defaults `namespace = "default"` when the `?namespace=` query param is absent, while `ProjectsHandler.List` uses all-namespaces by default. A multi-namespace project (`tide-sample-medium`) 404s on the detail call because the namespace mismatch makes the `client.Get` miss. The fix is a cross-namespace lookup fallback in `Get` that mirrors the `List` semantics. [VERIFIED: codebase read of `cmd/dashboard/api/projects.go`, `cmd/dashboard/router.go`, existing test `TestGetProjectWithChildren`]

**Primary recommendation:** Fix all four defects in a single phase (they share no ordering dependency). SC-3 (EnvelopeIn.Branch threading) is already merged and confirmed wired through task_controller → BuildJobSpec → claude-subagent. No re-work needed there.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Clone idempotency | API / Backend (pkg/git) | cmd/tide-push (caller) | git operations live in pkg/git; caller (runClone) picks up the fix transparently |
| Workspace permissions | API / Backend (job builder) | Kubernetes kubelet | FSGroup is a pod-spec field emitted by the controller binary; kubelet applies chown at mount time |
| Child-CRD parse robustness | API / Backend (subagent runner) | Prompt template | Primary: tolerant parse + per-file error isolation in Go code; secondary: prompt guidance to prevent malformed output |
| Dashboard detail 404 | Frontend Server (dashboard API) | — | Pure Go handler bug in chi route handler; no frontend change needed |

## Standard Stack

### Core (all already in go.mod — no new dependencies)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| github.com/go-git/go-git/v5 | v5.19.0 | git operations | Already vendored; `PlainOpen` + `FetchContext` are stable APIs used in `pkg/git/fetch.go` |
| k8s.io/utils/ptr | bundled with controller-runtime | typed pointer helpers | `ptr.To(int64(1000))` already used in `internal/dispatch/podjob/jobspec.go` |
| encoding/json | stdlib | JSON parse | Already used throughout; `json.Decoder` enables tolerant extraction |
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

### Observed Error Analysis

The live failure: `parse child file "task-03.json": invalid character 'W' after object key:value pair`

The character `'W'` is a word character — it can only appear after a key:value pair if:

1. **Trailing prose after the closing `}`** — the model wrote the JSON object correctly, then appended a sentence starting with `W` (e.g., `"With these tasks..."`) after the `}`. `json.Unmarshal` reads the whole buffer; any non-whitespace after the final `}` is `invalid character 'W'`. This is the most probable failure class: the model's Write tool call included explanatory text appended to the file after the object.

2. **Unquoted natural-language value followed by inline prose** — the `"prompt"` field contains free-form text, and the model wrote something like `"prompt": With task-03 we will...` (missing the opening quote). This produces the same error at the first character of the unquoted string.

Both classes arise from the same root cause: the model treated the file as a conversational artifact (with prose reasoning) rather than a bare machine contract. The arXiv "Let Me Speak Freely?" study (2408.02442) documents a 10-15% reasoning quality penalty under strict format constraints, and the two-step pattern — reason freely, then format — is their recommendation. The planner writes a PLAN.md (the reasoning artifact) and ALSO child JSON files (the machine contract); mixing them in one step creates exactly this failure class.

**What recovery survives which class:**

| Recovery mechanism | Trailing prose after `}` | Mid-object unquoted value |
|--------------------|--------------------------|--------------------------|
| `strings.TrimSpace` then `json.Unmarshal` | No — trailing prose is not whitespace | No |
| Extract first balanced `{...}` block then unmarshal | Yes — prose after `}` is stripped | No — unquoted value breaks the scan |
| `json.Decoder` + `DisallowUnknownFields(false)` | Yes — Decoder stops at end of first value | No |
| Per-file isolation (skip + log) | Partial recovery — siblings succeed | Partial recovery — siblings succeed |
| Prompt tightening | Reduces frequency | Reduces frequency |

The balanced-brace extractor survives trailing prose but NOT mid-object corruption. Per-file isolation is the necessary second layer: it rescues the N-1 valid siblings regardless of which failure class hit the bad file.

### Format Comparison

Architecture constraints that bound the evaluation:

- **`ChildCRDSpec.Spec` is `runtime.RawExtension`** — an opaque JSON blob that the controller decodes to a typed CRD spec at materialization time. Any format that is not JSON-native requires a JSON intermediate step for the `Spec` field; it cannot be escaped. [VERIFIED: `pkg/dispatch/childcrd.go:57`]
- **Files are written by the model's Write tool** — not by a constrained API response. Provider-native structured output (Anthropic's `output_config.format` schema mode) is not reachable here; it requires an API call shape, not a file Write.
- **`--bare` disables `.mcp.json` and plugins** — an `emit_child` MCP tool would require the claude CLI to load a custom MCP server, which `--bare` explicitly suppresses. [VERIFIED: `subagent.go:215-216` comments]
- **`gopkg.in/yaml.v3` and `sigs.k8s.io/yaml` are already transitive deps** — YAML parsing has zero new-dependency cost. [VERIFIED: `go.mod:24,174`]
- **Prefill is NOT supported on Claude Sonnet 4.6** — the model used in TIDE's v1 concrete impl. Anthropic's docs state explicitly: "Prefilling is not supported on... Claude Sonnet 4.6." [CITED: platform.claude.com/docs/en/docs/test-and-evaluate/strengthen-guardrails/increase-consistency]

| Format | LLM authoring reliability | Tolerates trailing prose | Nesting fit for ChildCRDSpec | Go parse-robustness + recovery | Schema-validatable | Phase-10 cost |
|--------|--------------------------|--------------------------|-----------------------------|---------------------------------|--------------------|---------------|
| **JSON (current)** | MEDIUM — model frequently appends prose; hard-fails on any character after `}`. `invalid character 'W'` is the observed failure. No tolerant mode in stdlib. | No without extractor; Yes with balanced-brace extractor | GOOD — `runtime.RawExtension` is JSON-native; no intermediate step needed | `json.Decoder` stops at end of first object, ignoring trailer. Per-file isolation handles siblings. | Yes — JSON Schema or struct tags | LOW — one function change + prompt patch |
| **YAML** | LOW-MEDIUM — indentation sensitivity is a second failure dimension absent from JSON. Models produce incorrect indentation especially in multi-line string values (the `prompt` field is typically 500-2000 chars). Norway problem: bare `NO` → `false`; `on`/`off`/`yes`/`no` → bool. Colon-in-string without quoting breaks parse. Research shows JSON-to-YAML output decreases syntax accuracy vs JSON-to-JSON. | YAML is line-oriented; trailing prose after the document end (`---`) would be a new document, not an error — but the model would need to emit the terminator correctly, which it often doesn't. | POOR for `runtime.RawExtension` — the `Spec` field must be JSON bytes (`raw []byte`); YAML unmarshal via `gopkg.in/yaml.v3` into `RawExtension` requires a YAML→JSON re-encoding step, adding complexity and a second failure surface. | `gopkg.in/yaml.v3` is available (no new dep), but YAML parse errors are often more cryptic than JSON. Per-file isolation applies equally. | Partial — YAML Schema exists but tooling is less mature in Go ecosystem | MEDIUM — YAML→JSON bridge for `RawExtension`, rewrite prompt schema example, update all tests |
| **XML** | MEDIUM — Anthropic's official prompt engineering docs recommend XML tags for structuring Claude's *input* and its *responses within a conversation*, and Claude is trained on XML. But XML as a *file format for nested structured data with string fields containing arbitrary prose* has different failure modes: the model may omit CDATA wrapping for the `prompt` field (which contains characters like `<`, `>`, `&`), producing malformed XML. | XML is tolerant of content after the closing tag only if the parser is lenient; stdlib `encoding/xml` is strict. | POOR for `Spec` — same intermediate-step problem as YAML; `runtime.RawExtension` expects JSON bytes. XML→JSON conversion for `Spec` is unavoidable. | `encoding/xml` (stdlib, no new dep). However, XML parse errors on unescaped `<` in `prompt` text are hard to recover from. | XSD-based schema validation is possible but no Go XSD library in the dependency tree | HIGH — prompt schema rewrite, CDATA handling, XML→JSON bridge, test updates |
| **TOML** | LOW — TOML multi-line strings require `"""` delimiters; models frequently emit single-quoted or bare strings for the `prompt` field. TOML has weak ecosystem familiarity for most models. | TOML parsers stop at first error; no trailing-content tolerance | POOR — same JSON-intermediate problem for `RawExtension`. TOML does not natively express raw bytes. | `github.com/pelletier/go-toml/v2` is a transitive dep (go.mod:113) but indirect; would become a direct dep. | TOML schema validation not standard | HIGH — format unfamiliar to model, intermediate encoding step, new direct dep import |
| **Plain-text / delimited** | LOW — flat key=value or pipe-delimited structures cannot express the nested `spec` object without inventing a bespoke micro-format. Models drift from bespoke schemas. | High tolerance if delimiters are line-based | POOR — fundamentally flat; nesting requires inventing JSON-inside-plain-text, which defeats the purpose | Hand-rolled parser; no stdlib support for the bespoke schema | None | HIGH — inventing and maintaining a custom format |
| **MCP tool-call (`emit_child`)** | HIGH — Anthropic's `strict: true` tool use applies constrained decoding (compiled grammar) against the `input_schema`, guaranteeing schema compliance with mathematical certainty. No trailing prose possible: tool call parameters are JSON-schema-enforced at token generation time. | Not applicable — constrained decoding prevents the failure class entirely | EXCELLENT — `input_schema` can express the exact `ChildCRDSpec` shape including nested `spec` object | No Go-side parse ambiguity: the tool call arguments arrive as valid JSON by construction | Yes — `input_schema` IS the schema | HIGH — requires `--bare`-compatible MCP injection (architectural change to CLI invocation; deferred) |

**Verdict:** JSON is the correct default format for Phase 10. The case against switching to YAML or XML rests on three independent constraints: (1) `runtime.RawExtension` demands JSON bytes for the `Spec` field regardless of outer format — any non-JSON format adds a mandatory intermediate re-encoding step that creates a second failure surface; (2) YAML's indentation sensitivity and implicit type coercion (Norway problem, boolean strings) introduce failure modes that are *absent* from JSON and that are particularly likely to fire inside the `prompt` field, which contains multi-line free-form text; (3) XML requires CDATA for the `prompt` field's arbitrary characters, and models routinely omit it. MCP tool-call is the definitively correct long-term fix but cannot be implemented within `--bare` mode without a non-trivial CLI invocation architecture change.

### Options

| Option | Benefit | Downside |
|--------|---------|---------|
| Balanced-brace extractor + per-file isolation (recommended) | Survives the observed trailing-prose failure class; siblings always recovered | Does not survive mid-object unquoted value; prompt patch is the second line of defense for that class |
| Per-file isolation only (no extractor) | Simple; siblings always recovered | The bad file still fails; a 1-of-N failure wastes the whole task slot even if siblings are valid |
| Strict abort-all (current behavior) | Surfaces the error immediately | Wastes the entire dispatch on a single bad file |
| Switch to YAML | Slightly more resilient to trailing prose (line-oriented) | Indentation + Norway + `RawExtension` bridge; more failure modes than it solves |
| Switch to XML | Claude is XML-trained; tolerant of some extra content | CDATA required for `prompt`; `RawExtension` bridge; net worse than JSON |
| MCP `emit_child` tool | Eliminates the failure class entirely | Requires `--bare`-compatible MCP injection; out of Phase-10 scope |
| Prompt constraint only | No code change | LLMs are probabilistic; cannot guarantee; needs a Go-side fallback |

### Recommendation

**Keep JSON. Add a tolerant extractor in `readChildCRDs` + per-file isolation + prompt patch.**

Three changes, all in Phase-10 scope:

**1. Tolerant JSON extraction in `readChildCRDs`** (in `internal/subagent/anthropic/subagent.go`):

Use `json.NewDecoder(bytes.NewReader(data)).Decode(&spec)` instead of `json.Unmarshal(data, &spec)`. `json.Decoder.Decode` reads exactly one JSON value from the stream and stops; any bytes after the closing `}` are not read and do not cause an error. This is stdlib `encoding/json` — no new dependency. This directly survives the observed failure class (trailing prose after `}`).

```go
dec := json.NewDecoder(bytes.NewReader(data))
dec.DisallowUnknownFields() // keep strict on field names
if jerr := dec.Decode(&spec); jerr != nil {
    // per-file: record error, continue to next file
    parseErrs = append(parseErrs, fmt.Errorf("parse child file %q: %w", name, jerr))
    continue
}
```

Note: `DisallowUnknownFields` is optional here — the security boundary is the kind allowlist and name check, not field strictness. The planner may add forward-compatible fields. Omit `DisallowUnknownFields` to avoid spurious rejections on future spec fields.

**2. Per-file isolation**: record errors in a `[]error` slice and continue. After the loop, if `len(parseErrs) > 0`, set `out.ExitCode = 1` with a reason listing the bad files by name. The valid siblings are still returned in `out.ChildCRDs`. The controller sees a failed dispatch AND a non-empty child list — it can surface the exact bad files in the Task status without losing the valid work.

The traversal-defense hard-aborts (symlink rejection, path-escape check) MUST remain hard-abort — they are a security boundary, not a format issue.

**3. Prompt patch in `plan_planner.tmpl`**: add an explicit constraint after the JSON schema example:

```
IMPORTANT: Each file MUST contain ONLY the JSON object — nothing before
the opening { and nothing after the closing }. No prose, no markdown fences,
no explanation. The file is read by a machine that fails on any character
outside the JSON object.
```

**Phase-10 scope call:** The MCP `emit_child` tool approach (which would eliminate the failure class at token-generation time via constrained decoding) is deferred. It requires changing the CLI invocation to pass a custom MCP server while preserving `--bare` hermeticity — that is an architectural change beyond Phase 10's defect-fix scope. Record it as a Phase 11 candidate.

The `TestReadChildCRDs_RejectsMalformedJSON` test in `childcrd_read_test.go` currently expects a hard error return. It must be updated to: (a) expect per-file isolation (valid siblings returned + error for the bad file), and (b) add a case with trailing prose after `}` asserting that the file parses successfully with `json.Decoder`. [VERIFIED: `subagent.go:437-438` hard-abort with `json.Unmarshal`; `plan_planner.tmpl` prompt text; `pkg/dispatch/childcrd.go:57` — `Spec runtime.RawExtension`; `go.mod:24,174` — yaml deps already transitive; Anthropic docs on prefill not supported for Sonnet 4.6; arXiv 2408.02442 on format restriction penalties]

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
**How to avoid:** The `return nil, fmt.Errorf(...)` for traversal violations must remain hard-abort. Only the `json.Decoder.Decode` failure and kind/name validation failures become per-file-skip.

### Pitfall 4: Dashboard Get Must Use p.Namespace for Child Listing
**What goes wrong:** After the all-namespace fallback finds a Project in `tide-sample-medium`, the child List calls (`h.Client.List(ctx, &ms, client.InNamespace(p.Namespace))`) must use `p.Namespace`, not the original (empty) `namespace` variable.
**How to avoid:** Refactor the detail-build logic into a shared helper `buildDetail(ctx, p)` that is called from both the explicit-namespace path and the cross-namespace fallback path. The helper always uses `p.Namespace`.

### Pitfall 5: `json.Decoder` Does Not Validate Extra Fields by Default
**What goes wrong:** Switching from `json.Unmarshal` to `json.Decoder.Decode` removes the implicit "whole-buffer" validation. A file with two valid JSON objects concatenated would decode only the first, silently ignoring the second.
**How to avoid:** After `dec.Decode(&spec)`, call `dec.More()` — if it returns true, a second token exists; treat this as a parse error (unexpected extra content). This catches double-object files while still tolerating trailing prose.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Idempotent clone | Custom PVC-wipe + reclone logic | `gogit.PlainOpen` + `pkggit.Fetch` | Already implemented in `pkg/git/fetch.go`; network-refreshes the repo |
| Per-run dir ownership | initContainer with `chown -R` | `fsGroup` on PodSecurityContext | Kubelet applies at mount time; no extra container or image pull |
| Tolerant JSON parse for LLM trailing prose | Custom balanced-brace scanner | `json.NewDecoder(...).Decode(...)` | stdlib `json.Decoder.Decode` stops at end of first JSON value by design; zero new deps |
| LLM format-compliance guarantee | Prompt engineering alone | MCP `emit_child` tool with `input_schema` (Phase 11) | Only constrained decoding at token generation time provides mathematical certainty; prompt reduces frequency, not eliminates |

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
| SC-4 | Trailing prose after `}` does not abort parse | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_TrailingProse` | ❌ Wave 0 |
| SC-4 | Malformed child JSON does not abort valid siblings | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_PartialParse` | ❌ Wave 0 |
| SC-4 | Existing malformed JSON test updated for per-file behavior | unit | `go test ./internal/subagent/anthropic/ -run TestReadChildCRDs_RejectsMalformedJSON` | ✅ (update) |
| SC-7 | `GET /api/v1/projects/{name}` without namespace finds cross-ns project | unit | `go test ./cmd/dashboard/api/ -run TestGetProjectWithoutNamespace` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./pkg/git/... ./internal/subagent/anthropic/... ./cmd/dashboard/api/...`
- **Per wave merge:** `make test`
- **Phase gate:** `make test` green + manual medium-sample DoD re-run before `/gsd-verify-work`

### Wave 0 Gaps
- [ ] `pkg/git/clone_test.go` — add `TestCloneIdempotent` (calls `Clone` twice on same destDir, asserts nil error second call)
- [ ] `internal/controller/push_helpers_test.go` — add `TestBuildCloneJobFSGroup` / `TestBuildPushJobFSGroup` asserting `PodSecurityContext.FSGroup == 1000`
- [ ] `internal/subagent/anthropic/childcrd_read_test.go` — add `TestReadChildCRDs_TrailingProse` (valid JSON + trailing "With..." → successful parse); add `TestReadChildCRDs_PartialParse` (valid task-01.json + malformed task-02.json → task-01 returned + error for task-02); update `TestReadChildCRDs_RejectsMalformedJSON` to match new per-file behavior; add `TestReadChildCRDs_DoubleObject` (two JSON objects concatenated → error via `dec.More()` check)
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
| V5 Input Validation | yes (child JSON) | `json.Decoder.Decode` + kind allowlist + `dec.More()` double-object check in `readChildCRDs` — all maintained in per-file-skip path |
| V6 Cryptography | no | — |

### Known Threat Patterns for this phase

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Traversal via malformed child JSON filename | Tampering | Traversal defense (symlink reject, path-escape check) remains a hard-abort; only parse errors become per-file-skip |
| FSGroup chown exposes PVC data to other pods | Information Disclosure | Per-Project PVC subPath isolation (`{project.UID}/workspace`) already enforced at mount time; FSGroup does not cross subPath boundaries |
| Double-object injection in child file | Tampering | `dec.More()` check after decode catches concatenated objects; treated as a parse error |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `ptr.To[int64]` is importable in `push_helpers.go` via `k8s.io/utils/ptr` (same dep as jobspec.go) | Fork (b) | Low — `k8s.io/utils` is already a transitive dep; if not directly listed, use `func() *int64 { v:=int64(1000); return &v }()` |
| A2 | The live medium-sample run 404 was observed without `?namespace=` from the frontend | Fork (d) | Medium — if the frontend does pass `?namespace=` and the 404 is from a different cause, the fix may be elsewhere; but the code bug exists regardless |
| A3 | The observed `invalid character 'W'` error is trailing prose after `}`, not a mid-object unquoted value | Fork (c) | Medium — if mid-object, `json.Decoder` also fails and per-file isolation rescues siblings either way; only the extractor's effectiveness differs |

## Open Questions

1. **`Fetch` with empty PAT against anonymous in-cluster remote**
   - What we know: `pkggit.Fetch` always populates `BasicAuth`; `pat == ""` is valid for public/anonymous remotes.
   - What's unclear: whether go-git sends a Basic auth header with empty password against `http://` anonymous servers (the in-cluster demo git-http-server).
   - Recommendation: test `Clone` idempotency against the in-cluster `http://` remote with `pat=""` during the Phase 10 DoD run; add a `TestFetchAnonymous` unit test using `seedBareRepo` + local `file://` URL as a proxy.

2. **MCP `emit_child` tool as Phase 11 candidate**
   - What we know: Anthropic's `strict: true` tool use applies constrained decoding against `input_schema`, providing mathematical format compliance. This eliminates the trailing-prose failure class entirely. The blocker is `--bare` suppressing `.mcp.json` and plugins.
   - What's unclear: whether the claude CLI supports injecting a custom MCP server via a flag (e.g., `--mcp-server`) without `--bare` being lifted, or whether an alternative hermeticity mechanism exists.
   - Recommendation: track as Phase 11 work. Phase 10's `json.Decoder` extractor + prompt patch is a sound interim fix.

## Sources

### Primary (HIGH confidence)
- [VERIFIED: codebase] `pkg/git/clone.go` — `Clone()` implementation, single call to `PlainCloneContext`
- [VERIFIED: codebase] `pkg/git/fetch.go` — `Fetch()` implementation, `PlainOpen` + `FetchContext` pattern
- [VERIFIED: codebase] `internal/controller/push_helpers.go` — `buildCloneJob`, `buildPushJob` — no SecurityContext
- [VERIFIED: codebase] `internal/dispatch/podjob/jobspec.go:403-406` — `FSGroup: new(int64(1000))` working pattern
- [VERIFIED: codebase] `internal/subagent/anthropic/subagent.go:437-438` — hard-abort on parse error with `json.Unmarshal`
- [VERIFIED: codebase] `internal/subagent/common/templates/plan_planner.tmpl` — prompt JSON schema
- [VERIFIED: codebase] `pkg/dispatch/childcrd.go:57` — `Spec runtime.RawExtension` (JSON-native; non-JSON outer formats require bridge)
- [VERIFIED: codebase] `go.mod:24,174` — `gopkg.in/yaml.v3` and `sigs.k8s.io/yaml` are transitive deps (YAML has zero new-dep cost)
- [VERIFIED: codebase] `cmd/dashboard/api/projects.go` — `Get` handler `namespace = "default"` default
- [VERIFIED: codebase] `cmd/dashboard/router.go` — route registration
- [VERIFIED: codebase] `cmd/claude-subagent/main.go:84` — `env.Branch` threaded to `EnsureWorktree`
- [VERIFIED: codebase] `internal/controller/task_controller.go:1068` — `Branch:` field stamped
- [VERIFIED: go-git source] `go-git/v5@v5.19.0/repository.go:58` — `ErrRepositoryAlreadyExists` sentinel

### Secondary (MEDIUM confidence)
- [CITED: platform.claude.com/docs/en/docs/test-and-evaluate/strengthen-guardrails/increase-consistency] — Anthropic official: prefill not supported on Sonnet 4.6; XML tags recommended for prompt structure (not file format); constrained decoding via structured outputs is the guarantee mechanism
- [CITED: platform.claude.com/docs/en/build-with-claude/structured-outputs] — Anthropic official: strict tool use applies constrained decoding; `output_config.format` requires API call shape (not applicable to file-write scenario)
- [CITED: arxiv.org/abs/2408.02442] — "Let Me Speak Freely?": stricter format constraints → greater reasoning degradation; two-step (reason free, then format) recommended; JSON-mode worst for reasoning tasks
- [CITED: .planning/debug/09-07-premature-succession-evidence.md] — live runtime evidence for all three task-execution defects; exact error strings

### Tertiary (LOW confidence — for context, not used as decision drivers)
- [WebSearch: dasroot.net/posts/2026/05/structured-output-llms-json-breaks-analyzed/] — 288-call log of JSON failures; trailing prose and unescaped characters are top failure modes
- [WebSearch: webcrawlerapi.com/blog/json-vs-yaml-choosing-the-right-format-for-llm-prompts] — JSON-to-YAML output decreases syntax accuracy vs JSON-to-JSON

## Metadata

**Confidence breakdown:**
- Fork (a) clone idempotency: HIGH — code read directly; go-git sentinel verified in module cache
- Fork (b) workspace perms: HIGH — missing SecurityContext confirmed by direct read; working pattern in same codebase
- Fork (c) child-JSON robustness: HIGH — hard-abort at exact line confirmed; format comparison grounded in codebase constraints (RawExtension, --bare, Sonnet 4.6 prefill restriction) + cited research; `json.Decoder` behavior is stdlib-documented
- Fork (d) dashboard 404: HIGH — handler bug at exact line confirmed; test behavior cross-checked

**Research date:** 2026-06-08
**Valid until:** 2026-07-08 (stable stack; no external moving parts)
