---
slug: file-transport-git-missing
status: resolved
trigger: "file:// git transport is broken across TIDE's runtime images — go-git's file:// transport shells out to a git binary the git-less distroless/node-slim images don't carry; the medium ($5 real-Claude) sample cannot bootstrap."
created: 2026-06-03
updated: 2026-06-03
---

# Debug: file:// git transport missing git binary

## Symptoms

- **Expected:** `kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml` runs the `tide-demo-init` Job to completion, creating a bare git repo at `file:///workspace/demo-remote.git` on `demo-remote-pvc`, so the medium-sample Project can clone → plan → execute (real Claude Haiku) → push back over `file://`.
- **Actual:** The `demo-remote-init` Job fails both pod attempts (backoffLimit 1). Job never reaches `Complete`; the medium run is blocked at its first bootstrap step.
- **Error (observed live on minikube, K8s 1.33.7):**
  ```
  tide-demo-init: git push refs/heads/master -> file:///workspace/demo-remote.git: exec: "git": executable file not found in $PATH
  ```
  The `git push ... ->` prefix is the Go error-wrap from `cmd/tide-demo-init/main.go:288`; the wrapped error is `exec: "git": executable file not found in $PATH`.
- **Timeline:** Never worked end-to-end. Medium sample is not exercised by CI (only `small` — stub ignores targetRepo, never clones — and `large` — `https://`, pure-Go transport — run in CI/acceptance). So the entire `file://` real-dispatch path shipped untested in v1.0.
- **Reproduction:** `minikube` cluster with TIDE installed; apply `examples/projects/medium/{namespace,demo-remote-pvc,demo-remote-init-job}.yaml`; the init Job errors with the above. Reproducible on ANY cluster — not minikube-specific.

## Root cause (pre-investigated — Observe First done during v1.0 validation)

- `cmd/tide-demo-init/main.go` pushes the embedded `examples/tide-demo-fixture/` to a bare repo via go-git (`gogit.PlainInit` + remote `PushContext`) over a `file://` URL.
- **go-git's `file://` transport is NOT pure-Go**: `PushContext`/`Fetch` over `file://` shell out to `git-upload-pack`/`git-receive-pack` (the `git` binary) at runtime. (go-git http(s)/SSH transports ARE pure-Go — this is the source of the false premise below.)
- `images/tide-demo-init/Dockerfile` uses `FROM gcr.io/distroless/static:nonroot` on an explicit but WRONG premise (Dockerfile lines 39–41): *"go-git/v5 is pure-Go … no /bin/git shell-out … file:// transports are local-only."* True for http(s)/SSH, false for `file://`.
- **Blast radius — every runtime image is git-less** and would hit the same wall on any `file://` clone/push (each verified to fail `exec: git`):
  - `images/tide-demo-init/Dockerfile` → `distroless/static:nonroot`
  - `images/tide-push/Dockerfile` → `distroless/static:nonroot`
  - `images/claude-subagent/Dockerfile` → `node:22-slim` (Debian slim, no git)
  - `images/credproxy/Dockerfile` → `distroless/static:nonroot`
- `pkg/git/doc.go:37` repeats the (file://-incorrect) *"No /bin/git shell-out … pure-Go"* claim. `pkg/git/worktree.go:36` PlainClones the local bare repo with a `file://` URL — same dependency. So even past the demo-init step, the controller's own clone (subagent image) and push (tide-push image) over `file://` would fail identically.

## Decision to surface

Is `file://` a supported v1 transport?
- **(A)** YES → add a minimal `git` (or `git-core`) binary to the runtime images that perform `file://` clone/push (`tide-demo-init`, `tide-push`, `claude-subagent`; reassess `credproxy` — it does no git ops, likely leave distroless). Correct the false "pure-Go / no shell-out" comments in those Dockerfiles + `pkg/git/doc.go`. Add a CI guard so the medium/file:// path can't silently rot again. **User leaned toward this (root-cause fix, 2026-06-03).**
- **(B)** NO → re-document medium as https-only and adjust the sample/CEL accordingly. (Rejected by user lean, but note as the alternative.)

**Resolved as (A).** credproxy confirmed git-less by design (only imports `internal/credproxy/`, performs zero git ops) — left distroless/static:nonroot.

## Fix-shape fork (distroless git strategy) — decided

The 2 distroless static-binary git-op images (`tide-demo-init`, `tide-push`) needed a git binary, but distroless has no package manager. Options were: (a) base-swap to alpine + `apk add git`, (b) base-swap to debian-slim + `apt-get install git`, (c) copy a statically-linked git into distroless. **Chosen: (a) alpine 3.21 base swap.** Rationale: smallest base that gives a real apk-managed git with its helper deps (git-remote-*, templates) intact — avoids the fragile hand-assembly of a static git; alpine is already the builder-stage base (familiar); cleanly recreates the non-root UID-1000 contract via `adduser -D -u 1000 nonroot`. (AskUserQuestion was unavailable inside the debug session-manager subagent, so the well-justified default was applied and the fork + alternatives surfaced to the orchestrator for the user.) `claude-subagent` stays on its required `node:22-slim` (Claude CLI is a Node binary) and gets git via `apt-get install -y --no-install-recommends git`.

## Constraints (binding — CLAUDE.md)

- Chart SOT is `hack/helm/` augment scripts + `tide-values.yaml`, NEVER rendered `charts/`. After chart-affecting changes run `make helm`; CI gate is `git diff --quiet charts/`. **(No chart-referenced values changed — image bases only; `git diff --quiet charts/` verified clean post-fix.)**
- Images rebuilt locally must be `minikube image load`ed at BOTH `:1.0.0` and `:v1.0.0` (sample manifests use `v`-prefix; chart/controller defaults use no-`v` — a separate latent inconsistency worth flagging).
- Do NOT push the `v1.0.0` tag (held local-only at `8a8e843`). **(Not pushed.)**
- After image source changes: rebuild → reload into minikube → verify the Pod actually rotated (new image digest / new pod age), don't just re-tag. **(See "minikube tag-overwrite gotcha" below — a real digest-staleness trap was hit and fixed.)**
- Distroless images are non-root (USER 1000/nonroot) by design (D-G3 subagent UID) — any base change must preserve non-root execution. **(Verified `id -u == 1000` in all three fixed images.)**

## Live environment (for re-test)

- minikube up, context `minikube`, K8s v1.33.7, 4 CPU / 6 GB.
- TIDE installed in `tide-system` (controller Available, dashboard Running); cert-manager v1.20.2 installed.
- `tide-sample-medium` namespace exists with `demo-remote-pvc` Bound and the FAILED `demo-remote-init` Job present (delete + re-apply to retry).
- `$0` small sample already validated green on this cluster earlier today.

## Current Focus

- hypothesis: CONFIRMED — go-git `file://` push/clone requires a `git` binary absent from all distroless/slim runtime images; adding `git` to the images that do `file://` ops fixes the medium-sample bootstrap.
- next_action: DONE — fix applied, images rebuilt+reloaded at both tags, bootstrap Job verified `Complete`, file:// clone-smoke verified green. Medium-sample Project run (paid real-Claude) intentionally NOT executed — user wants to watch that live in the main thread.

## Resolution

- **root_cause:** go-git/v5's `file://` transport shells out to the system `git` binary (git-upload-pack/git-receive-pack) at runtime, but all four v1.0 runtime images shipped git-less (3× distroless/static:nonroot, 1× node:22-slim), so every `file://` clone/push failed with `exec: "git": executable file not found in $PATH`. The medium ($5 real-Claude) sample's first bootstrap step (`demo-remote-init` push) never worked end-to-end; the file:// path shipped untested because CI only exercises the small (stub, no clone) and large (https://, pure-Go) samples.
- **fix:** Added a system `git` to exactly the three images that perform file:// git ops. `tide-demo-init` and `tide-push` base-swapped distroless/static:nonroot → `alpine:3.21` + `apk add --no-cache git` + `adduser -D -u 1000 nonroot` (non-root UID-1000 contract preserved). `claude-subagent` kept its required `node:22-slim` base and gained `apt-get install -y --no-install-recommends git`. `credproxy` left distroless (does no git ops). Corrected the false "pure-Go / no /bin/git shell-out / file:// local-only" comments in all three Dockerfiles and in `pkg/git/doc.go`. No chart-referenced values changed (`git diff --quiet charts/` clean).
- **verification:**
  - `docker run --rm --entrypoint git <img> --version` succeeds in all three fixed images (git 2.47.3 alpine, 2.39.5 debian); `id -u == 1000` in all three.
  - Rebuilt fresh from HEAD; `minikube image load`ed at both `:1.0.0` and `:v1.0.0`.
  - Bootstrap Job `demo-remote-init` reaches `Complete` (condition `type=Complete,status=True`), single pod, log `OK: bootstrapped local-only git remote at /workspace/demo-remote.git`.
  - file:// FETCH side proven: `git clone file:///workspace/demo-remote.git` inside `tide-claude-subagent:v1.0.0` (mounting demo-remote-pvc) succeeds, working tree carries commit `df1f1b4 Initial fixture content` + `main.go`.
  - Medium-sample Project (paid real-Claude) intentionally NOT run — deferred to the user's live main-thread session.
- **minikube tag-overwrite gotcha (worth recording):** `minikube image load <name>:v1.0.0` did NOT overwrite a pre-existing `:v1.0.0` tag already in minikube's docker daemon — it silently kept the OLD (git-less) digest while `:1.0.0` updated correctly. The re-run consequently still hit `exec: git` until the stale tag was forcibly removed inside minikube (`minikube ssh -- docker rmi -f ...:v1.0.0`) and reloaded. Always confirm `minikube ssh -- docker images` digests match local after a reload, not just `minikube image ls`.
- **stale-PVC-state gotcha:** the original git-less failures left a PARTIAL bare repo (`HEAD/config/objects/refs` from `PlainInit`, no pushed commit) on demo-remote-pvc. The binary's idempotent-by-refusal guard (`exit 2 / "target dir already exists … refuse to overwrite"`) then masked the real fix until the partial dir was cleared. Clearing `/workspace/demo-remote.git` via a throwaway busybox pod before re-running is required after any failed init.

## Follow-up (proposed, not built — needs user nod)

- **CI guard for the file:// path (debug-file option-A item, deferred).** Medium isn't in CI today, so a future base change could silently drop git and re-introduce this. Lightweight proposal: a CI step that runs `docker run --rm --entrypoint git <git-op-img> --version` per git-op image (tide-demo-init, tide-push, claude-subagent), failing the build if git is absent — or, stronger, a file:// clone/push smoke. Not implemented in this session (touches the CI surface, a separate concern; scoping said "propose, don't over-build").
- **`:v1.0.0` vs `:1.0.0` tag inconsistency (latent, flagged).** Sample manifests pin `v`-prefixed tags; chart/controller defaults use no-`v`. Not in scope for this fix but worth reconciling so a single load/tag convention covers both.

## Evidence

- timestamp: 2026-06-03 — `demo-remote-init` Job both pods `Error`; `kubectl logs job/demo-remote-init` → `exec: "git": executable file not found in $PATH`. `demo-remote-pvc` Bound (100Mi RWO, standard). PVC is not the issue.
- timestamp: 2026-06-03 — `docker run --rm --entrypoint git <img> --version` fails `exec: "git"` for tide-push:1.0.0, tide-claude-subagent:v1.0.0, tide-credproxy:1.0.0 (and tide-demo-init by inference). Base images confirmed via `grep ^FROM`: 3× distroless/static:nonroot, 1× node:22-slim.
- timestamp: 2026-06-03 — `cmd/tide-demo-init/main.go` imports go-git (`gogit "github.com/go-git/go-git/v5"`), pushes via remote `PushContext` (line ~288 error-wrap). `pkg/git/{clone,fetch}.go` import `transport/http` (pure-Go); `pkg/git/worktree.go:36` uses `file://` PlainClone; `pkg/git/doc.go:37` asserts "No /bin/git shell-out".
- timestamp: 2026-06-03 (fix) — alpine base-swap (tide-demo-init, tide-push) + apt git (claude-subagent) applied; false comments corrected in 3 Dockerfiles + pkg/git/doc.go. Rebuilt fresh from HEAD; `git --version` succeeds + `id -u==1000` in all three.
- timestamp: 2026-06-03 (verify) — bootstrap Job `Complete` (`OK: bootstrapped local-only git remote at /workspace/demo-remote.git`); file:// clone-smoke in claude-subagent image succeeds (commit df1f1b4 + main.go). `git diff --quiet charts/` clean; `go build ./pkg/git/` OK.

## Eliminated

- hypothesis: PVC/storage misconfiguration → ELIMINATED. `demo-remote-pvc` Bound; error is `exec: git`, not a mount/IO failure.
- hypothesis: minikube-specific / my-local-build artifact → ELIMINATED. Images built from committed Dockerfiles; distroless has no git by design; reproducible on any cluster.
- hypothesis: stale image (pre-lint-commit) → ELIMINATED as cause. Images rebuilt fresh from HEAD this session still lack git (it's the base image, not the binary).
- hypothesis: fix didn't take / still exec:git after rebuild → ELIMINATED. Root-caused to minikube NOT overwriting a pre-existing `:v1.0.0` tag on `image load`; forced rmi+reload fixed it (digests then matched local, git present in minikube-stored images).

---
**Closed at v1.0.0 milestone completion (2026-06-11).** The defect class this
session tracked was fixed and validated before ship: full `make test-int`
green (Layer A 36/36 + Layer B), nightly-integration green, live medium DoD
on minikube (Project=Complete, BoundaryPushed=True), and the v1.0.0-rc dry-run
gate green end-to-end.
