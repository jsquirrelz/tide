---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
verified: 2026-06-08T12:10:00Z
verifier: orchestrator (execute-phase) + user live verification
status: passed-with-deferral
gate_decision: APPROVED
score: 5/6 success criteria fully met; SC-2 transport-half met, SC-2 real-Claude leg DEFERRED (blocked by pre-existing claude-subagent defects, out of Phase 8 scope)
release_gate: BLOCKED — v1.0.0 retag/push gated on SC-2 real-Claude leg (see .planning/debug/real-claude-authoring-path.md)
deferred:
  - criterion: SC-2 (medium drives Project=Complete with REAL Claude Haiku)
    leg: real-Claude authoring (transport leg PASSES)
    status: blocked
    reason: >
      Pre-existing chain of real-Claude-authoring-path defects, never exercised
      end-to-end (CI dispatches only the stub). Two confirmed: (1) claude-subagent
      envelope-path default mismatch [FIXED, commit 6dcffe6]; (2) controller
      overloads Provider.Params with dispatch metadata "parentName" that the
      anthropic runner rejects [OPEN]. Likely more downstream. Orthogonal to
      Phase 8's transport deliverable.
    tracked_in: .planning/debug/real-claude-authoring-path.md
    decided_by: user 2026-06-08 (close Phase 8 on transport + open /gsd-debug)
---

# Phase 8 — Verification

## Summary

Phase 8 made TIDE's production git-transport policy explicit (**http(s)/SSH
pure-Go only; `file://` rejected at admission**) and rebuilt the medium ($5)
sample to serve its fixture over an **in-cluster http:// git remote** that
exercises the same pure-Go transport as production. The transport deliverable
is validated on a real cluster (minikube, K8s v1.33.7). The live re-test also
exposed — and this session fixed — a git-http-server nonroot crashloop and a
missing `tide-push` ServiceAccount, plus a pre-existing claude-subagent
envelope-path bug.

SC-2's literal "real Claude (Haiku)" leg is **not** met: the real-Claude
authoring path has a chain of pre-existing defects (beyond Phase 8) that have
never run end-to-end. Per user decision, that work is split into a dedicated
debug session and the v1.0.0 retag stays gated until it is green.

## Success-criteria evidence

### SC-1 — core images git-less; demo-init retains git ✅
- `docker run --rm --entrypoint which ghcr.io/jsquirrelz/tide-push:1.0.0 git` → exit 127 (distroless; no `which`/git) — git absent.
- `… tide-claude-subagent:1.0.0 git` → exit 1 (node:22-slim has `which`; git absent).
- `docker run --rm --entrypoint git ghcr.io/jsquirrelz/tide-demo-init:1.0.0 --version` → exit 0 (`git version 2.47.3`).
- Confirmed both locally and inside minikube after reload.
- Commit `1f26822`/`ed4fd1d`/`c6770ac` (08-02). CI image-smoke step added (08-07, `! docker run --entrypoint which … git`, robust to 127).

### SC-2 — medium drives Project=Complete with real Claude via http:// ⚠ PARTIAL
- **Transport leg ✅ VALIDATED:** controller's clone Job `Complete 1/1` cloning over
  `http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git`
  (pure-Go go-git, `tide-push:1.0.0`, no git binary). `git ls-remote` through the
  ClusterIP Service returns refs. The git-http server (git-http-backend + nginx +
  fcgiwrap, nonroot) serves smart-HTTP fetch + receive-pack.
- **Real-Claude leg ❌ DEFERRED:** see frontmatter `deferred` + debug record. No
  per-run branch pushed yet (the run never reached the executor/push stage); cost
  $0 (failed pre-LLM).

### SC-3 — file:// rejected at admission ✅
- CEL XValidation rejects `file://`; `make test` envtest admission suite GREEN
  (Test A flipped RED→GREEN by the CEL change). Small-sample sentinel migrated to
  `https://git.example.internal/stub/no-such-repo.git`. Live CRD carries the new rule.
- Commits `f284fd8` (sentinel + RED test, 08-01), `6af5b2f`/`6b9f478` (CEL + regen, 08-03).

### SC-4 — docs corrected ✅
- Medium README's false "controller mounts demo-remote-pvc" claim removed; documents
  the http:// 9-step sequence. `pkg/git/doc.go` reframed (http(s)/SSH pure-Go
  supported; `file://` unsupported). Samples index updated.
- Commits `734b175`/`5f8763d`/`9da64a5` (08-06), `1f26822` (08-02 partial doc.go).

### SC-5 — CI coverage for medium/http path ✅
- `nightly-integration.yml` SC-1 image-smoke step added; `test/integration/kind/medium_http_test.go`
  has real assertions (hermetic git-http server clone+push via stub; ports fixed to 8080).
  Compiles + `go vet` clean. Commits `9de0678`/`46e8fdf` (08-07), `1fc2164` (port fix).

### SC-6 — image-tag alignment ✅
- `grep -rn ':v1\.' examples/ | grep image:` → 0 results. Commit `28c5f38` (08-04).

## Plans

8/8 plans executed (08-01…08-07 fully complete; 08-08 transport-validated, real-Claude
leg deferred). Per-wave `make test` MAKE_EXIT=0 throughout; chart SOT clean
(`git diff --quiet charts/`).

## Follow-up

- `.planning/debug/real-claude-authoring-path.md` — fix the real-Claude authoring
  chain, then re-run the medium SC-2 live test to `Complete` and flip SC-2 → full PASS.
- v1.0.0 retag (delete local tag at `8a8e843`, recreate at post-fix HEAD, push —
  confirm-only) remains gated on the above.
