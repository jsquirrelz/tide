---
phase: 06-v1-image-publish-and-ship-readiness-revalidation
type: acceptance-findings
status: complete
created: 2026-05-30
tags: [boot-04, acceptance, cascade-chain, cascade-7, project-milestone-authoring, ship-blocker]
---

# Phase 6 — BOOT-04 `$0` Acceptance Revalidation: Findings

The Phase 6 closeout gate (`make acceptance-v1-smoke` — the `$0` BOOT-04 revalidation, ACC-01)
was run end-to-end for the first time on 2026-05-30. As designed, it surfaced a **chain of
pre-existing latent gaps** that had never been exercised because BOOT-04 had never run to
completion. Six were in-scope (image-publish / harness / env) and were fixed + validated.
The seventh is a **core product gap outside Phase 6's scope** and is the real v1.0 ship blocker.

## Cascade chain (in order surfaced)

| # | Class | Root cause | Resolution | Commit |
|---|-------|-----------|------------|--------|
| 1 | env (pre-phase) | `acceptance-v1.sh` had no cert-manager bring-up | quick task 260530-h2h | `adb1053` |
| 2 | image-publish (IMG-01) | 6 chart-referenced images never published | `build-images` buildx matrix in `release.yaml` | `af081ed` |
| — | acceleration | `go build -a`, no cache → ~40-min cold image builds | BuildKit cache mounts + drop `-a` (404s→9-48s) | `e09e45e` |
| 3 | build-context | claude-subagent `//go:embed templates/*.tmpl` excluded by `.dockerignore` `**` allowlist | re-include `!**/*.tmpl` | `68f2d32` |
| 4 | helm-install | chart RWX `tide-projects` PVC unbindable on kind's RWO-only `local-path` | `--set workspaces.pvc.accessModes={ReadWriteOnce}` | `9395006` |
| 5 | namespace-provisioning | reconciler/Task Jobs need `tide-projects` PVC + `tide-subagent` SA + `tide-signing-key` Secret in the **Project's** namespace (chart provisions them only in `tide-system`); kind `WaitForFirstConsumer` deadlock | port Layer B per-namespace setup + busybox PVC prewarm into `acceptance-v1.sh` small mode | `819ab29` |
| 6 | env (disk) | Docker VM 94% full (23 GB build cache + stale clusters) → init Job "No space left" | prune 16 GB cache + stale clusters → 65% used | (env) |
| **7** | **product gap (OUT OF SCOPE)** | **Project→Milestone authoring not implemented** — see below | **DEFERRED → new phase** | — |

Each in-scope fix mirrors a proven `test/integration/kind/suite_test.go` pattern.

## What Phase 6 PROVED (image-publish + ship-readiness revalidation goal — MET)

Validated live on `kind-tide-acceptance-1780175685` (run #4, `$0`, no API key):

- ✅ All **6 component images build multi-arch + `kind load`** into the cluster (`tide-controller`, `tide-dashboard`, `tide-stub-subagent`, `tide-credproxy`, `tide-push`, `tide-claude-subagent` @ `1.0.0`)
- ✅ `helm template charts/tide` resolves all 6 tags to appVersion `1.0.0` (no `v0.1.0-dev`); third-party `busybox:1.36` preserved
- ✅ cert-manager rolls out; **both helm installs `deployed`**
- ✅ **`tide-controller-manager` reaches `Available`**, **`tide-dashboard` `Running`** — **no `ImagePullBackOff`** (the exact cascade-2 failure that opened Phase 6 is closed)
- ✅ Per-namespace PVC `Bound` (prewarm), `tide-init` Job **`Complete`**, Project reaches `Initialized` with git branch `tide/run-small-project-*` set

D-06 **infrastructure subset** = PASS. This is the honest image-publish/install proof Phase 6 owns.

## cascade-7 — the real v1.0 ship blocker (OUT OF PHASE 6 SCOPE)

**The Project→Milestone authoring step is not implemented.** A bare `Project` initializes
(init Job + git branch/clone) and then **stalls at `Initialized` forever** — nothing ever
authors its `Milestone`, so the (fully-wired) down-stack reconcilers never trigger.

**Evidence:**
- `internal/controller/project_controller.go:271-380` — after `Initialized`, the ProjectReconciler runs only `reconcilePhase3Lifecycle` (git branch/clone/push). The body comment: *"Plan 03-08 keeps the body skeletal — production wiring … lands in follow-up plans."* Line 207: *"Project scaffolded; awaiting dispatch logic."*
- `grep -rn 'Create(.*Milestone' internal/` → **zero** — no controller creates a Milestone CR from a Project.
- `internal/controller/milestone_controller.go:159,184` — the MilestoneReconciler **does** dispatch a planner to author Phases (down-stack authoring works). The missing link is purely **Project→Milestone**.
- All Layer B integration tests **pre-apply** a `kind: Milestone` fixture (`test/integration/kind/testdata/{up-stack-project,three-task-wave,chaos-resume-three-task}.yaml`) — so the suite tests down-stack dispatch but **never** Project→Milestone authoring. This is why the gap survived to v1.

**Impact:** This is the top of the five-level cascade and the heart of the project's Core Value
(*"TIDE drives its own next milestone end-to-end"* / README *"a human applies a Project; TIDE
authors MILESTONE.md by dispatching a planner"*). Until it is wired, a bare `Project` cannot
self-bootstrap, so the v1.0 self-hosting acceptance cannot complete. **This — not images —
is what gates v1.0 ship.**

**Why deferred, not fixed here:** implementing project-level planner dispatch (the Project-side
analog of `milestone_controller.go:reconcilePlannerDispatch` — dispatch a planner Job, read the
authored Milestone from the result envelope, create the Milestone CR, handle the `milestone: auto`
gate) is Phase-3-sized core product work. Grafting it into an image-publish phase violates scope
and risks further down-stack cascades (8, 9, …) discovered piecemeal.

## Recommended next step

Open a dedicated phase (e.g. **Phase 7 — Project→Milestone authoring / self-bootstrap**):
wire the ProjectReconciler's `Initialized → author-Milestone` dispatch mirroring the proven
`milestone_controller.go` planner pattern; add a Layer B integration test that applies a **bare
Project** (no pre-authored Milestone) and asserts Milestone materialization; then re-run
`make acceptance-v1-smoke` — which already drives correctly through `Initialized` and will now
proceed to `Complete`. The acceptance script needs **no further changes** (cascades 1-6 are fixed).

`/gsd-phase` to formalize Phase 7, or `/gsd-debug` to investigate the authoring wiring first.

## Cross-references
- `06-SPEC.md` (ACC-01 / D-06), `06-CONTEXT.md` (D-05/D-06 decisions), `06-FINDINGS.md` (phase opening)
- `internal/controller/project_controller.go:271-380`, `internal/controller/milestone_controller.go:159-441`
- `test/integration/kind/suite_test.go` (ensureProjectsPVC/SubagentSA/SigningKeySecret/pvcPrewarmPod), `:480` (RWO override)
- Run logs: `/tmp/06-acceptance-smoke-{2,3,4}.log`
