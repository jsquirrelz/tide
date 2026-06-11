# 08-08 SUMMARY — Live minikube re-test (SC-2)

**Plan:** 08-08 · **Wave:** 5 · **Status:** complete (transport validated; real-Claude leg deferred)
**Executed:** 2026-06-08 (live minikube, real-key path; orchestrator-driven with user verification)

## What was validated

The live re-test confirmed Phase 8's actual deliverable — the **in-cluster
http:// git transport** — works end-to-end on minikube (real cluster, K8s
v1.33.7):

- The controller's **clone Job succeeded over http://** (`tide-clone-… Complete 1/1`,
  image `tide-push:1.0.0`), cloning from `git-http-server` via pure-Go go-git.
- The `git-http-server` Deployment serves the bare repo over http:// (verified
  `git ls-remote http://git-http-server/demo-remote.git` through the Service).
- SC-1 confirmed live: `tide-push` + `claude-subagent` git-less; `tide-demo-init`
  retains git (docker-run checks, local + in-minikube).
- SC-6 confirmed: no `:v1.` image tags in `examples/`.

## Bugs found + fixed during the live run

The live validation surfaced three real defects (none caught by CI, which uses
the stub exclusively):

1. **git-http-server CrashLoop** (Phase 8 image defect) — nonroot UID 1000 could
   not bind the fcgi socket in root-owned `/run`, nor nginx port 80. Fixed:
   socket → `/run/nginx/`, nginx → `:8080`, Service `port 80 → targetPort 8080`.
   Commit `1fc2164`.
2. **Missing `tide-push` SA** (Phase 8 sample defect) — `per-namespace-resources.yaml`
   omitted the SA the clone/push Jobs run as; Jobs failed at pod creation
   (`FailedCreate: serviceaccount "tide-push" not found`). Added SA+Role+RoleBinding.
   Commit `c4c34d2`.
3. **claude-subagent envelope-path default** (pre-existing, not Phase 8) — defaulted
   to `/workspace/envelopes/in.json` instead of the controller's per-task-uid path.
   Fixed to match the stub. Commit `6dcffe6`.

## What is NOT validated — SC-2 real-Claude leg (deferred)

The medium sample does **not** reach `Complete` with real Claude on this build.
After fix #3, the real planner reached the anthropic runner and hit a **second
pre-existing defect**: the controller injects `parentName` into
`Provider.Params` (for the stub), but the anthropic runner rejects any non-model
param. This is a chain of pre-existing real-Claude-authoring-path bugs, orthogonal
to Phase 8, that have never run end-to-end (CI = stub only; large sample not
CI-gated). Per user decision (2026-06-08), this is split into a dedicated debug
session rather than hot-patched inside Phase 8.

→ Tracked in `.planning/debug/real-claude-authoring-path.md` (status: open).

No LLM spend occurred — both real-Claude attempts failed pre-LLM (cost $0).

## Deviations

- Controller was runtime-patched to `--subagent-image=claude-subagent:1.0.0`
  (was `…stub-subagent:1.0.0` from the $0 install). Runtime-only; not persisted
  to the chart. Left in place for the follow-up debug session.
- `08-VERIFICATION.md` records SC-1/3/4/5/6 PASS + SC-2 transport PASS; SC-2
  real-Claude leg BLOCKED (deferred to debug). The v1.0.0 retag stays gated.
