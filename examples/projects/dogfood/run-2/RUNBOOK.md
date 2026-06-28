# Dogfood Run #2 — Bring-up Runbook

Operational runbook for dogfood run #2 (TIDE-on-TIDE: build the OpenAI/Codex subagent +
per-level provider switch). Scope + decisions: [`.planning/dogfood/run-2-SCOPE.md`](../../../../.planning/dogfood/run-2-SCOPE.md).

**Posture:** one cluster at a time (single-node kind OOMs if you stack clusters). $50 metered cap.
The run pushes to an in-cluster TIDE mirror — never to public origin.

Set once:
```bash
NS=tide-dogfood-codex
VER=1.0.5   # was 1.0.4 when authored 2026-06-25; v1.0.5 shipped 2026-06-27 — dogfood the latest (it carries the Phase 30 ImportController fix this run exercises)
RUN2=examples/projects/dogfood/run-2
```

---

> ## ⚠ Corrections from run 2b (2026-06-28) — read before running
> The first live attempt hit these gaps; they are now fixed in the artifacts/steps but
> call them out so the bring-up is reproducible:
> 1. **cert-manager is a hard prereq** — the tide chart references `cert-manager.io/v1`
>    Certificate/Issuer. Install `v1.20.2` BEFORE the chart (see `hack/scripts/acceptance-v1.sh`).
> 2. **Override the chart PVC to RWO on kind** — `helm install … --set 'workspaces.pvc.accessModes={ReadWriteOnce}'`
>    (local-path is RWO-only; default RWX leaves the manager Pending). PVC accessModes are
>    immutable, so on a retry delete the stale `tide-projects` PVC first.
> 3. **Build + `kind load` the `tide-git-http-server` fixture image** — it is unpublished
>    (`make` builds it; 403 on ghcr). Also: its `nginx.conf` needed `client_max_body_size 0`
>    to accept a real repo push (fixed in `images/tide-git-http-server/nginx.conf`).
> 4. **`TIDE_IMPORT_IMAGE=""` does NOT dev-skip** — `cmd/manager/main.go` `envOrDefault` treats
>    empty as unset and falls back to a missing default. Point it at the real published image:
>    `kubectl -n tide-system set env deploy/tide-controller-manager TIDE_IMPORT_IMAGE=ghcr.io/jsquirrelz/tide-import:$VER`.
> 5. **Strip the API key newline** — `kubectl create secret --from-file` keeps a trailing `\n`,
>    which makes the credproxy `X-Api-Key` header invalid (every `claude` call exits 1). Use:
>    `--from-literal=ANTHROPIC_API_KEY="$(tr -d '\n\r' < ~/.tide/anthropic.key)"`.
> 6. **Per-namespace `tide-import` SA** — now in `per-namespace-resources.yaml` (was missing →
>    import Job pod 403'd on create).
> 7. **Seed statuses must be empty + gates milestone/phase: auto** — fixed in
>    `seed-manifest.trimmed.json` (was `Running` → reporter thrash) and `project.yaml`.
>
> **Known product defects this run surfaced (NOT yet fixed — see `.planning/dogfood/run-2b-FINDINGS.md`):**
> cost tracking never wires under import-adoption (budget cap can't enforce); project lifecycle
> stalls at `Initialized`; no planner-concurrency bound (single-node OOM at ~60 parallel planners);
> phase false-`Succeeded` on a failed planner. **A real run needs these fixed + a bigger/multi-node cluster.**

---

## 1. Fresh kind cluster

```bash
kind get clusters                       # expect empty; if a stale cluster exists, delete it first
kind create cluster --name tide-dogfood
kubectl config use-context kind-tide-dogfood
```

## 2. Install the published v1.0.5 chart (CRDs FIRST — Pitfall 4)

```bash
helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds \
  --version "$VER" -n tide-system --create-namespace
helm install tide oci://ghcr.io/jsquirrelz/tide-charts/tide \
  --version "$VER" -n tide-system --wait
kubectl -n tide-system get deploy            # tide-controller-manager + tide-dashboard Available
```
All eight v1.0.5 images (controller, dashboard, stub/claude/codex-subagent…, push, reporter,
import) were built from the same `main` as this chart, so no ImagePullBackOff is expected.

**Blank `TIDE_IMPORT_IMAGE` (dev-skip the envelope Job).** We adopt the M+P skeleton via the
import path, but we do NOT resume task envelopes (plans regenerate). With the import image empty,
`ImportController.CopyingEnvelopes` skips the copy Job and sets `ImportComplete=True` right after
materializing M+P — no envelope copy, no v1alpha1→v1alpha2 envelope-conversion risk
(`import_controller.go:595-600`). Required, because project-level adoption only happens through
`ImportComplete` (the project planner guard is Job-presence-based; hand-apply would re-author).
```bash
kubectl -n tide-system set env deploy/tide-controller-manager TIDE_IMPORT_IMAGE=""
kubectl -n tide-system rollout status deploy/tide-controller-manager
```

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

## 6. Stage the trimmed seed ConfigMap, THEN apply the Project

The ImportController materializes the 3 Milestones + 15 Phases from the seed ConfigMap (key
`manifest`); `ImportComplete=True` then routes the project/milestone controllers down the adoption
branch (no re-authoring). No skeleton.yaml is applied — import owns materialization.
```bash
kubectl -n "$NS" create configmap tide-import-seed-dogfood-codex-runtime \
  --from-file=manifest="$RUN2"/seed-manifest.trimmed.json
kubectl apply -f "$RUN2"/project.yaml        # fresh v1alpha2 Project (importSource, $50 cap, mirror)
```

**Pre-run verification (do this before walking away):**
```bash
# import must reach Complete (dev-skip → no tide-import Job needed)
kubectl -n "$NS" wait --for=condition=ImportComplete project/dogfood-codex-runtime --timeout=120s
kubectl -n "$NS" get project,milestone,phase,plan
kubectl -n "$NS" get jobs -w
```
EXPECT: 3 Milestones + 15 Phases materialized; NO `tide-project-*` or `tide-milestone-*` planner
Jobs (adoption working); `tide-phase-*` planner Jobs DO appear (regenerating plans). If a
project/milestone-planner Job spawns, STOP — adoption isn't firing; reassess before spending.

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
