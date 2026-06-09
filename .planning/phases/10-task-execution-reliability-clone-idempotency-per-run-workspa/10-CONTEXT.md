# Phase 10: Task-Execution Reliability — Context

**Gathered:** 2026-06-08
**Status:** Ready for planning
**Source:** Fork-decision checkpoint (research-informed; no discuss-phase session — the ROADMAP Phase 10 draft directed "forks for planning/research")

<domain>
## Phase Boundary

Fix the leaf task-execution / git / clone / LLM-output / dashboard layer that the first
legitimate run-to-task-execution exposed (medium sample, real Claude Haiku, in-cluster
`http://` remote). All four defects are PRE-EXISTING — never caught because no run had
reached real task execution before. Phase 9's cross-namespace orchestration mechanism is
already PROVEN and merged; do NOT re-touch it.

This phase OWNS the medium DoD (Phase 9 SC-4/SC-6), the Phase 8 SC-2 real-Claude leg
(DEFERRED→PASS), and the v1.0.0 retag unblock (retag stays user-gated, confirm-only).

**In scope:** the 4 forks below + SC-3 confirmation (already merged) + the final medium
DoD re-run.

**Out of scope:** editable/re-appliable "envelopes as first-class artifacts" DX and the
clobbering/authoritative-fields question (deferred per Phase 9 boundary). Orphaned-Task
finalizer-release is already correct in code (no change needed — see RESEARCH).
</domain>

<decisions>
## Implementation Decisions (LOCKED via fork checkpoint 2026-06-08)

### Fork (a) — Clone idempotency → **Fetch-into-existing**
`pkg/git/Clone()` must detect `errors.Is(err, gogit.ErrRepositoryAlreadyExists)` →
`gogit.PlainOpen(destDir)` → `pkggit.Fetch(ctx, repo, pat)` → return the opened repo.
Idempotent on any number of retries, keeps the bare repo current, reuses existing
`pkg/git/fetch.go`. Fix lives entirely in `pkg/git/clone.go` — callers (`runClone` in
`cmd/tide-push/main.go`) unchanged.
- REJECTED skip-if-exists (goes stale if remote advanced); REJECTED clean-and-reclone
  (needs PVC-root write perms nonroot lacks; nukes in-flight worktrees).
- Pitfall: `Fetch` with empty PAT against the anonymous in-cluster `http://` remote —
  use nil/empty auth path when `pat == ""`; confirm in `pkg/git/fetch.go`.

### Fork (b) — Per-run workspace perms → **fsGroup=1000 on the pod SecurityContext**
`buildPushJob` and `buildCloneJob` in `internal/controller/push_helpers.go` must set
`PodSecurityContext{FSGroup: <int64 1000>}` — the exact pattern already working in
`internal/dispatch/podjob/BuildJobSpec` (`jobspec.go:403-406`). Kubelet chowns the mount
at startup; no extra container, no chart change.
- REJECTED initContainer chown (image pull + latency; expensive on large PVC); REJECTED
  dedicated init Job (the one-time-assumption pattern that already failed on PVC wipe).
- Match `push_helpers.go`'s existing pointer style (`new(int64(1000))`); chart
  `values.yaml` is a FIXED contract — binary-only change.

### Fork (c) — Child-CRD parse robustness → **Tolerant JSON + per-file isolation + prompt patch**
JSON STAYS — not by default, by architecture: `ChildCRDSpec.Spec` is
`runtime.RawExtension` (`pkg/dispatch/childcrd.go:57`, locked Phase-3 D-A1), the only
channel into the K8s API, which demands JSON bytes. XML/YAML/Markdown/TOML would all
require converting back to JSON before storage — a net regression. The defect is the
intolerant PARSER, not the format. Three changes:
1. In `readChildCRDs` (`internal/subagent/anthropic/subagent.go:~437`): replace
   `json.Unmarshal(data, &spec)` with `json.NewDecoder(bytes.NewReader(data)).Decode(&spec)`
   so the decoder stops at the end of the first JSON value (trailing prose/markdown fences
   ignored — this is the observed `invalid character 'W' after object key:value pair`
   failure, model appended a prose sentence after `}`). Add a `dec.More()` check to catch
   double-object files.
2. Per-file isolation: accumulate parse errors into a slice and CONTINUE to siblings;
   return valid children + a surfaced retriable error naming the bad file(s). One bad child
   no longer wastes the whole dispatch. **Traversal defense (symlink/path-escape) stays
   hard-abort** — only `json.Decode` + kind/name validation become per-file-skip.
3. Prompt patch in `internal/subagent/common/templates/plan_planner.tmpl`: explicit
   "nothing before `{`, nothing after `}`" constraint in the child-emit section.
- Fail-fast this phase (no controller auto-retry); a partial-parse surfaces as a failed
  level with a clear error the author can re-trigger.

### Fork (d) — Dashboard project-detail 404 → **All-namespace fallback in `Get`**
`ProjectsHandler.Get` (`cmd/dashboard/api/projects.go`) defaults `namespace="default"`
when `?namespace=` is absent, while `List` uses all-namespaces — so a cross-namespace
project (`tide-sample-medium`) 404s on detail while list + events 200. Fix: when
`namespace == ""`, `client.List` across all namespaces and return the first project whose
`Name == name`; build the detail using `p.Namespace` for child List calls (refactor into a
shared `buildDetail(ctx, p)` helper used by both the explicit-namespace and fallback paths).
A determined bug fix, not a contested fork.

### SC-3 — Executor worktree branch (CONFIRMATION ONLY)
The 09-09 `EnvelopeIn.Branch` fix is merged and wired end-to-end (task_controller:1068 →
BuildJobSpec → claude-subagent:84 → EnsureWorktree). No re-work — just confirm live in the
DoD run that the executor checks out `tide/run-<project>-<unix>`.

### Claude's Discretion
- Exact error-type/struct names for the per-file parse-error surface (e.g. PartialParseError).
- Test names/placement, beyond the Wave-0 gaps RESEARCH lists.
- Whether to thread `costSpentCents` assertions into the DoD-run task or leave them to verify.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Runtime evidence (defects, exact error strings)
- `.planning/debug/09-07-premature-succession-evidence.md` — all task-execution defects + dashboard symptom, with exact errors and the 09-08/09-09 re-run logs.

### Research (resolved forks, file/line citations, validation architecture)
- `.planning/phases/10-task-execution-reliability-clone-idempotency-per-run-workspa/10-RESEARCH.md` — per-fork recommendations with VERIFIED codebase citations; the Validation Architecture / Wave-0 test-gap section; Fork (c) format-comparison + RawExtension rationale.

### Implicated source (read before editing)
- `pkg/git/clone.go`, `pkg/git/fetch.go`, `cmd/tide-push/main.go` — fork (a)
- `internal/controller/push_helpers.go`, `internal/dispatch/podjob/jobspec.go:403-406` — fork (b)
- `internal/subagent/anthropic/subagent.go` (`readChildCRDs` ~400-450),
  `internal/subagent/common/templates/plan_planner.tmpl`,
  `pkg/dispatch/childcrd.go:57` (RawExtension contract) — fork (c)
- `cmd/dashboard/api/projects.go`, `cmd/dashboard/router.go`,
  `cmd/dashboard/api/projects_test.go` — fork (d)

### Project contract
- `./CLAUDE.md` — anti-patterns: chart `values.yaml` FIXED (binary catches up to chart),
  nonroot pods, CRD `.status` only, file:// git unsupported (http(s)/SSH only), provider
  code stays behind the Subagent interface.
</canonical_refs>

<specifics>
## Specific Ideas

- Each fix is independent (no ordering dependency) EXCEPT SC-3 depends on fork (a)
  landing first so `repo.git` exists when `EnsureWorktree` runs — wave the git/clone fix
  before the DoD re-run.
- Final wave = a "run medium DoD" task: fresh medium-sample re-run, assert all descendants
  Succeeded, `tide/run-*` branch pushed to the in-cluster `http://` remote with real
  authored code, `costSpentCents > 0` under cap. This is the v1.0.0-unblocking gate.
- The parked minikube is primed (fresh Phase-9 images, `TIDE_REPORTER_IMAGE` set, reporter
  RBAC fixed) for fast re-runs.
</specifics>

<deferred>
## Deferred Ideas

- **MCP `emit_child` schema-constrained tool (→ Phase 11).** The mathematically airtight
  fix for malformed child output (constrained decoding). Blocked today by the `--bare` flag
  suppressing `.mcp.json` discovery, and it expands the subagent contract. Recorded as a
  Phase 11 open question; Phase 10 ships the tolerant-parse safety net instead.
- Controller auto-retry of a planner dispatch on partial-parse (ExitCode=2 retriable vs
  ExitCode=1 permanent classification) — deferred; Phase 10 is fail-fast.
- Editable/re-appliable "envelopes as first-class artifacts" DX + clobbering/authoritative
  -fields question — deferred per Phase 9 boundary.
</deferred>

---

*Phase: 10-task-execution-reliability-clone-idempotency-per-run-workspa*
*Context captured: 2026-06-08 via fork-decision checkpoint*
