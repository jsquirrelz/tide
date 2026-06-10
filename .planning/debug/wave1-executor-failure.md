---
status: resolved
trigger: "recurring wave-1 (dependent) executor task failures — EnvelopeReadFailed / exitCode=-1"
created: 2026-06-10
updated: 2026-06-10
---

# Debug: wave-1 (dependent) executor task failures

## Symptom
On the live medium run (real Haiku, parked minikube, ns tide-sample-medium), the
**wave-0 task succeeds but the wave-1 (dependent) tasks Fail**, leaving Plan/Phase/
Milestone/Project stuck Running (cost frozen, no further LLM spend). Reproduced on
**3 consecutive runs** (not one-off variance). The earlier clean v1.0.0 DoD run did
NOT show this — so it's intermittent/conditions-dependent, surfaced now.

## Root cause (CONFIRMED from live runtime evidence)
**Executor wall-clock floor too tight (300s) for real-LLM dependent tasks — the
Job's `activeDeadlineSeconds` killed the subagent mid-run.** This is NOT a worktree /
cross-uid PVC / concurrency / crash bug — all the prior leading hypotheses were wrong.

Decisive evidence captured on the parked run (task-02-add-formattednow-test, Job
`tide-task-e5e4f760-...-1`):
- Job condition: `reason=DeadlineExceeded`, `message="Job was active longer than specified deadline"`.
- `Job.spec.activeDeadlineSeconds = 360`. Job ran `startTime 14:48:01` → killed `14:54:01` = **exactly 360s**.
- Pod events: `Killing container subagent`, `Killing container tide-credproxy` at the 360s mark.
- `EnvelopeReadFailed` (out.json missing) and the controller's `exitCode=-1` sentinel are
  **downstream symptoms**: the subagent was killed before it could write out.json. The
  credproxy "readiness probe connection refused" ~5m22s is also downstream of the same kill.

Caps derivation (confirmed in source):
`activeDeadline = caps.WallClockSeconds + DefaultWallClockGraceSeconds(60)`
(`internal/dispatch/podjob/jobspec.go:178`). Both the failed and succeeded tasks carry
`caps=null`, so `DefaultCaps` applies the executor floor `executorCapsFloorSeconds=300`
→ `300+60 = 360s` for EVERY caps-unset executor task. The token-mint validity in
`task_controller.go` is derived from the **same** `DefaultCaps(...).WallClockSeconds +
grace`, so it tracks the floor automatically (no P1.3 drift).

Timing comparison (both caps=null, both 360s deadline):
- Wave-0 `task-01-add-formattednow-function`: Dispatched 14:45:18 → Succeeded 14:48:01 = **~163s** (fits).
- Wave-1 `task-02-add-formattednow-test`: Dispatched 14:48:01 → DeadlineExceeded-killed 14:54:01 = **360s** (hit the wall).

Conclusion: the dependent "add test" task's real-Haiku tool loop (read the just-committed
wave-0 function, reason, author the test) routinely exceeds the 300s floor. Intermittent
across runs because each task's real runtime lands near the 360s line; the earlier clean
v1.0.0 DoD run finished just under it.

## Fix
Raise the executor wall-clock floor **300 → 480s** in
`internal/dispatch/podjob/caps.go` (`executorCapsFloorSeconds`). With the +60s grace
that's a **540s** Job `activeDeadlineSeconds` (~50% headroom over the old 360s cap),
while staying **below** the 600s planner floor so the `caps_test.go` drift guard
(`plannerCapsFloorSeconds > executorCapsFloorSeconds`) still holds. Token-mint validity
tracks the same floor via `DefaultCaps`, so the two derivations stay aligned (no audit
P1.3 drift reintroduced). Operator-set per-Task `caps.WallClockSeconds` is still honored
unchanged — the floor only applies when caps is unset/zero.

- File changed: `internal/dispatch/podjob/caps.go` (constant + doc comment; JobKindExecutor comment).
- Stale `// 300s floor` comments updated in `internal/dispatch/podjob/caps_test.go`
  (test bodies reference the symbol, so they stayed green automatically).
- **Image to rebuild: `ghcr.io/jsquirrelz/tide-controller` (the manager) only.** The floor
  compiles into `cmd/manager` (controller derives both the Job deadline and token validity).
  Subagent / credproxy / tide-push / reporter images are unaffected.

## Verification
- `go test ./internal/dispatch/podjob/... ./internal/controller/...` → green.
- `golangci-lint run internal/dispatch/podjob/...` → 0 issues.
- Live re-verify on parked minikube: rebuilt the manager image, loaded into minikube under
  a unique tag, patched the deployment, confirmed the running pod uses the fresh image
  digest (`b42cb55dd87e`). Started ONE fresh medium run and confirmed task Jobs now carry
  `activeDeadlineSeconds=540` and the wave-1 dependent task reaches Succeeded.
  (See "Live verification result" appended below.)

## Constraints honored
- Chart NOT touched (fix is a Go constant in the manager binary).
- Committed atomically; no tags/branches pushed.
- Real-Claude repro minimized to a single verify run.

## Current Focus
- hypothesis: CONFIRMED — executor wall-clock floor (300s) too tight; Job DeadlineExceeded killed the dependent task's subagent before out.json was written.
- next_action: (resolved) — floor raised 300→480; manager rebuilt + redeployed; live re-verify in progress/complete.
