# RESUME — TIDE v1.0 minikube validation → Phase 8 opened (handoff 2026-06-03)

**Read this, then VERIFY each claim against live state before acting (Observe First / Verify Before Claiming).**

## Where we are

v1.0 minikube validation ran. Results:

- **`$0` stub path: GREEN on minikube.** A bare `examples/projects/small/project.yaml` drove the full five-level cascade (Milestone→Phase→Plan→Task→Wave all Succeeded) to `Project status.phase=Complete` on minikube (docker driver, **K8s v1.33.7** — matches the kind-node pin CI proves against). Controller + dashboard both healthy; dashboard API served the Complete project. This stands as real-cluster (non-kind) validation evidence.
- **Real-key (medium `$5`) path: BLOCKED by two real bugs → opened Phase 8.**
  1. **git-binary bug (fixed, committed `93595b9`):** go-git's `file://` transport shells out to a `git` binary, but all runtime images shipped git-less (3× distroless/static, 1× node:22-slim). The `demo-remote-init` Job failed `exec: "git": executable file not found`. Debug session `.planning/debug/file-transport-git-missing.md` (status: resolved) added git to the 3 file://-op images. **NOTE: this fix is slated for partial REVERT in Phase 8** (see below) — production doesn't need git.
  2. **Architectural gap (the real blocker):** the controller's clone Job (`buildCloneJob`, internal/controller/push_helpers.go:259) mounts ONLY `tide-projects`, never `demo-remote-pvc` where the bare repo lives — so `file:///demo-remote.git` is unreachable. The medium README:96 claims a `demo-remote-pvc` mount that does not exist in code. The medium `file://` sample never worked end-to-end (it's not in CI; only small — stub ignores targetRepo — and large — https:// pure-Go — are exercised).

## The decision that reframed it (user, 2026-06-03)

**Production git transport = http(s)/SSH ONLY** (go-git pure-Go, no git binary; matches the github/gitlab/gitea portability constraint). **`file://` is NOT a supported production transport** — it was a demo-only shortcut. So:
- Revert `93595b9`'s **core-image** git additions (`tide-push`, `claude-subagent` → git-less again). Keep git only in a demo git-server image.
- Rewrite the medium sample to serve its fixture over **in-cluster `http://`** (git-http-backend pod + Service), so the demo exercises the SAME transport production uses — still local/air-gapped.

## Next step: PLAN PHASE 8

Phase 8 is registered in ROADMAP.md (detail block carries the full goal + locked decisions + 6 success criteria) and STATE.md roadmap-evolution. Committed `c90adfa`.

**Recommended:** `/clear`, then `/gsd-plan-phase 8`. (This conversation is long; a clean context makes the planner sharper. The Phase 8 ROADMAP entry is self-contained.)

Phase 8 success criteria (see ROADMAP.md §"Phase 8" for full text):
1. `tide-push` + `claude-subagent` git-less again (revert 93595b9 core); git only in demo git-server image.
2. Medium sample → `http://` in-cluster remote; drives `Project=Complete` with real Claude (Haiku) **re-tested live on minikube**.
3. `file://` explicitly unsupported — CEL `targetRepo` policy + handle small sample's `file:///tmp/no-such-repo` sentinel.
4. Docs: medium README false-mount claim; `pkg/git/doc.go` reframe.
5. CI coverage for the medium/http path (was never in CI).
6. Align `:v1.0.0` (v-prefix sample/demo manifests) vs `:1.0.0` (no-v chart/controller defaults).

## Release sequencing — DECIDED (option a, user 2026-06-03)

v1.0.0 does NOT ship until Phase 8 lands. After Phase 8 completes: **delete the local `v1.0.0` tag (currently `8a8e843`) and re-create it at the post-Phase-8 HEAD, then push** (still confirm-only — triggers `release.yaml`). NOT v1.0.1. **Do NOT push the tag** without explicit user go-ahead.

## Live environment (still up)

- minikube up, context `minikube`, K8s v1.33.7, 4 CPU / 6 GB. TIDE in `tide-system` (controller Available, dashboard Running), cert-manager v1.20.2.
- `tide-sample-small`: the `$0` Complete project still present.
- `tide-sample-medium`: namespace + `demo-remote-pvc` (Bound) + `demo-remote-init` Job (now **Complete** after the git fix) + the bootstrapped bare repo. Per-ns subagent deps (tide-subagent SA / tide-projects PVC / tide-signing-key) NOT yet mirrored here (would be needed for a medium run).
- minikube has all images loaded at both `:1.0.0` and `:v1.0.0` (controller, dashboard, stub-subagent, claude-subagent, credproxy, push, demo-init, busybox). **Gotcha:** `minikube image load` does NOT overwrite an existing tag's digest — `minikube ssh -- docker rmi -f` first, then reload.

## Hard rules (binding — see CLAUDE.md)

- Chart SOT is `hack/helm/` source + `make helm`, NEVER rendered `charts/`. CI gate: `git diff --quiet charts/`.
- Do NOT push `v1.0.0` (or any tag) without explicit user go-ahead.
- Route production/chart/CI edits through GSD (`/gsd-plan-phase` → `/gsd-execute-phase`, or `/gsd-quick`/`/gsd-debug`). Manual kubectl/helm/minikube exploration is fine.
- Recommend the thorough root-cause fix; checkpoint at genuine forks.

## Pointers

- Debug record: `.planning/debug/file-transport-git-missing.md` (resolved; documents the git fix + the two minikube traps).
- Phase 8 scope: `.planning/ROADMAP.md` §"Phase 8".
- Samples: `examples/projects/{small,medium,large}/`. Clone-Job builder: `internal/controller/push_helpers.go:259`.
- Memory: `~/.claude/.../memory/project_tide_trajectory.md`.
