# Dogfood Run #2 — Bring-up Runbook

Operational runbook for dogfood run #2 (TIDE-on-TIDE: build the OpenAI/Codex subagent +
per-level provider switch). Scope + decisions: [`.planning/dogfood/run-2-SCOPE.md`](../../../../.planning/dogfood/run-2-SCOPE.md).

**Posture:** one cluster at a time (single-node kind OOMs if you stack clusters). $50 metered cap.
The run pushes to an in-cluster TIDE mirror — never to public origin.

Set once:
```bash
NS=tide-dogfood-codex
VER=1.0.4
RUN2=examples/projects/dogfood/run-2
```

---

## 1. Fresh kind cluster

```bash
kind get clusters                       # expect empty; if a stale cluster exists, delete it first
kind create cluster --name tide-dogfood
kubectl config use-context kind-tide-dogfood
```

## 2. Install the published v1.0.4 chart (CRDs FIRST — Pitfall 4)

```bash
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds \
  --version "$VER" -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
  --version "$VER" -n tide-system --wait
kubectl -n tide-system get deploy            # tide-controller-manager + tide-dashboard Available
```
All eight v1.0.4 images (controller, dashboard, stub/claude/codex-subagent…, push, reporter,
import) were built from the same `main` as this chart, so no ImagePullBackOff is expected.

## 3. Real Anthropic key → `tide-secrets` (the run's engine is Claude)

```bash
kubectl create namespace "$NS"
kubectl -n "$NS" create secret generic tide-secrets \
  --from-file=ANTHROPIC_API_KEY="$HOME/.tide/anthropic.key"
```
No OpenAI key is needed — this run *builds* OpenAI support, it does not *use* it.

## 4. Per-namespace wiring (SA / PVC / signing-key)

Adapt [`examples/projects/medium/per-namespace-resources.yaml`](../medium/per-namespace-resources.yaml)
for `$NS` (it carries the `tide-subagent` SA, the `tide-projects` PVC, and the signing-key Secret the
dispatch Jobs mount). Edit the namespace, then:
```bash
kubectl apply -f "$RUN2"/per-namespace-resources.yaml   # namespace-substituted copy
```

## 5. In-cluster TIDE mirror, seeded from current `main`

Adapt the medium git server ([`git-http-server-deployment.yaml`](../medium/git-http-server-deployment.yaml)
+ its PVC) into `$NS`, repo name **`tide.git`** (image `ghcr.io/jsquirrelz/tide-git-http-server:$VER`,
Service `git-http-server` port 80→8080). Then seed it from the local checkout and enable receive-pack
(the http push path needs it — see the #15 lesson in the trajectory):

```bash
# init an empty bare repo + enable anonymous receive-pack in the server PVC
kubectl -n "$NS" exec deploy/git-http-server -- sh -c \
  'git init --bare /srv/git/tide.git && \
   git -C /srv/git/tide.git config http.receivepack true && \
   git -C /srv/git/tide.git config core.sharedRepository group'

# push current main into the mirror via a port-forward
kubectl -n "$NS" port-forward svc/git-http-server 8080:80 &
PF=$!; sleep 2
git push "http://localhost:8080/tide.git" "$(git rev-parse --abbrev-ref HEAD):refs/heads/main"
kill $PF
```
Service DNS the operator (and the run) use: `http://git-http-server.$NS.svc.cluster.local/tide.git`
— matches `targetRepo`/`git.repoURL` in `project.yaml`.

## 6. Apply the skeleton, THEN the Project

Order matters: the milestone/phase CRs must exist before the Project so the adoption guards see
"already authored" and skip project + milestone planning.
```bash
kubectl apply -f "$RUN2"/skeleton.yaml      # 3 Milestones + 15 Phases (structure-only)
kubectl apply -f "$RUN2"/project.yaml       # fresh v1alpha2 Project ($50 cap, mirror targetRepo)
```

**Pre-run verification (do this before walking away):** confirm the project-level adoption guard
actually suppresses the project-planner (open item from the scope doc). Watch that NO
milestone-planner or project-planner Jobs are created, and that phase-planner Jobs DO appear:
```bash
kubectl -n "$NS" get jobs -w
kubectl -n "$NS" get project,milestone,phase,plan
```
If milestone/project-planner Jobs spawn (re-authoring), STOP — the adoption guard is not firing for
this path; reassess before spending.

## 7. Drive, monitor, kill criteria

```bash
# live status
watch kubectl -n "$NS" get project,milestone,phase,plan,task,wave
# cost
kubectl -n "$NS" get project dogfood-codex-runtime -o jsonpath='{.status.costSpentCents}{"\n"}'
# dashboard
kubectl -n tide-system port-forward svc/tide-dashboard 8081:80   # http://localhost:8081
```
Halt + report on any of:
- **Budget gate** trips at 5000¢ ($50) — Project condition flips to a budget halt.
- **Stall** — no status advance / reconcile loop / repeated requeue.
- **Executor DeadlineExceeded** — diagnose from pod `state.terminated.{exitCode,reason,message}`
  (the executor is silent on stdout), captured before the 600s Job TTL GC.
- **Completion** — `Project=Complete`.

Metered: on a budget halt *with real progress*, report cost + state, then top up (raise
`absoluteCapCents`) rather than abandon.

## 8. Extract the authored code

```bash
kubectl -n "$NS" port-forward svc/git-http-server 8080:80 &
PF=$!; sleep 2
git fetch "http://localhost:8080/tide.git" 'refs/heads/tide/run-*:refs/remotes/run2/*'
kill $PF
git branch -r | grep run2/        # the run branch(es)
```
Report what landed (`internal/subagent/codex/{client,run,doc}.go` + `Dockerfile`; the vendor switch
in `dispatch_helpers.go` + schema; chart values / manager env), total cost, and how far the cascade
reached. Hand off to the **hardening phase** (review / test / live heterogeneous validation with a
real OpenAI key / publish `tide-codex-subagent`).

---

### Deferred to the hardening phase (NOT this run)
Code review, tests-green, mergeability, live planner=Claude/executor=Codex validation, and image publish.
