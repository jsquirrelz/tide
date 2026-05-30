# TIDE Troubleshooting

**Audience:** Operators and ops engineers diagnosing TIDE failures during install or steady-state.

**Status:** v1.0; ~13 canonical failure modes covered in a Symptom/Cause/Recipe table per Phase 5 D-C4 lock. Operator-incident reference. Each row points to either a copy-pasteable `kubectl`/`helm` recipe OR a deeper doc that owns the deeper recipe (cross-links inline).

**Scope of this doc:**

- Single-page lookup table covering install-time, first-apply, and steady-state failure modes.
- Each Recipe column gives one runnable command or one cross-link — not a runbook.
- For depth on storage drivers / git hosts / gates / observability / dashboard / install order, follow the cross-links in the Recipe column to the per-domain doc that owns that surface.

## Symptom / Cause / Recipe

| Symptom | Cause | Recipe |
|---------|-------|--------|
| Project hangs in `Status.Phase=Deleting`; finalizer never clears | Child CRD finalizer cascade stuck (controller restarted mid-delete, or child reconciler can't satisfy its finalizer condition) | `kubectl patch project <name> -n <ns> --type=merge -p '{"metadata":{"finalizers":[]}}'` — manual unstick (per ROADMAP Phase 5 mandate). Inspect controller logs first; force-clear only when no graceful path remains. |
| Controller log: `401 unauthorized` from Anthropic API | Invalid `ANTHROPIC_API_KEY` in the Secret referenced by `Project.Spec.subagent.apiKeySecretRef` (key revoked, expired, or never populated) | Recreate the Secret with a valid key, then bounce the controller: `kubectl create secret generic anthropic-api-key -n tide-system --from-literal=key=sk-ant-... --dry-run=client -o yaml \| kubectl apply -f -` then `kubectl rollout restart deploy/tide-controller-manager -n tide-system` |
| `Project.Status.git.condition=PushLeaseFailed` | External commit landed on the per-run branch concurrently — `--force-with-lease` push rejected (Phase 3 D-B6 concurrent-push race) | `kubectl annotate project <name> tideproject.k8s/bypass-push-lease=true` — see [gates.md](gates.md) for the full annotation handshake |
| `tide_secret_leak_blocked_total` counter increments; push Job failed with `gitleaks-blocked` | Subagent output included a pattern matching the gitleaks ruleset (API key, password, private key) | Inspect the per-task envelope diff and remove the offending content; re-trigger the Task. To tune the rule set for a specific Project, mount a per-Project `gitleaks.toml` via ConfigMap — see [observability.md](observability.md) for the cardinality bound on this metric. |
| Pod stuck in `Pending`; PVC stays `Pending` with `WaitForFirstConsumer` or `no persistent volumes available` | Cluster has no `ReadWriteMany` StorageClass; the default StorageClass only supports RWO | Install an RWX CSI driver appropriate for your cluster (EFS / Filestore / Azure Files / NFS / Longhorn). See [rwx-drivers.md](rwx-drivers.md) — driver matrix + per-driver install steps + the Helm value `workspaces.pvc.storageClassName` override. |
| Dashboard 404 / unreachable at `https://tide-dashboard.<cluster>/` | No Ingress configured (chart defaults to `ClusterIP` Service without an Ingress) | `kubectl port-forward -n tide-system svc/tide-dashboard 8080:80` then browse `http://localhost:8080/` — see [dashboard.md](dashboard.md) for the full local-dev + ingress shapes. |
| `kubectl get project` returns `error: the server doesn't have a resource type "project"` | The `tide-crds` subchart was not installed (common: operators install only `tide` and miss the CRD subchart that ships separately) | Install the CRD subchart FIRST: `helm install tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system --create-namespace` then re-install the main chart — see [INSTALL.md](INSTALL.md#install-order-pitfall-4--crds-first) (Pitfall 4) |
| Admission webhook rejects `kubectl apply -f plan.yaml` with HTTP 422 | Either a cycle was detected in the task DAG (`pkg/dag` rejects cyclic input) OR `depends_on` doesn't match `files_touched` in strict mode (Phase 2 D-E1..E4) | Inspect the rejection message: `kubectl apply -f plan.yaml` echoes the validator's specific complaint. For cycles, re-author the offending Plan's task DAG to break the cycle. For file-touch drift, either fix the `depends_on`/`files_touched` declarations OR set `Plan.Spec.fileTouchPolicy: warn` (default `strict`). |
| `Project.Status.conditions` shows `BudgetExceeded`; dispatch halted | Hard budget cap reached (`Project.Spec.budget.costCeilingCents`) — Phase 2 D-D2 + Phase 04.1 P4.1 rolling-window infrastructure | `kubectl annotate project <name> tideproject.k8s/bypass-budget=true` (only if scoped recovery is appropriate — production runs should refuse bypass per Phase 5 D-A2; bypass is a manual-recovery affordance, not a regression-suppression knob). |
| Project paused at `Status.Phase=AwaitingApproval` | Gate policy at this level = `approve` per `Project.Spec.gates`; controller waiting on human signal (Phase 4 D-G3) | `tide approve <project>` OR `kubectl annotate project <name> tideproject.k8s/approve=true` — see [gates.md](gates.md) for per-level overrides and `--wave plan/N` scoped approval. |
| `Pod tide-controller-manager-…` stuck in `ImagePullBackoff` | Image pull secret missing for a private registry, OR `appVersion` in the chart doesn't match an available tag | `kubectl describe pod -n tide-system <pod>` shows the reason. If credentials: `kubectl create secret docker-registry tide-registry-creds -n tide-system --docker-server=<reg> --docker-username=<u> --docker-password=<p>` and reference via `imagePullSecrets` in the chart values. If tag drift: confirm `helm get values tide -n tide-system \| grep appVersion` matches a published image tag. |
| `deploy/tide-controller-manager` or `tide-dashboard` pod stuck in `ImagePullBackOff` immediately after `helm install` (before the controller reaches `Available`) | Chart references component images not yet published to ghcr.io (pre-release state), OR chart tag pin (`v0.1.0-dev`) does not match any published tag | 1. Check image existence: `docker manifest inspect ghcr.io/jsquirrelz/tide-controller:1.0.0`. 2. If not published: run `make acceptance-v1-smoke` (builds and kind-loads all 6 images locally). 3. Verify chart tags: `grep -E 'v0\.1\.0-dev' charts/tide/values.yaml` should return 0 after Phase 6. See [docs/INSTALL.md](INSTALL.md) §"Maintainer: image-publish" for the publish pipeline. |
| Controller restart loop; `kubectl get pods -n tide-system` shows new controller age every ~15s | Leader election lease conflict — zombie leader from a chaos-resume scenario or stale lease holder (the leader election lock under `coordination.k8s.io/Lease` is stuck on a non-existent Pod). Phase 3 D-D1 territory. | Wait ~15s for the lease window to expire and a new leader to elect cleanly. If the loop persists, inspect leases: `kubectl get leases -n tide-system` and delete the stale holder if it points at a non-existent Pod. Tasks resume from CRDs + PVC; no work is lost. See [live-e2e.md](live-e2e.md) for the chaos-resume infrastructure design. |
| `kubectl get project` works but `kubectl get plan`/`task`/`phase` returns `no matches for kind` after a chart upgrade | `tide-crds` subchart was not upgraded in lockstep with the `tide` controller chart (Research Pitfall 2) | `helm upgrade tide-crds oci://ghcr.io/jsquirrelz/tide-charts/tide-crds --version 1.0.0 -n tide-system` — both charts version-bump together (Phase 5 D-X3 lockstep rule). Verify both: `helm list -n tide-system \| grep -E 'tide(-crds)?'` |

## See also

- [docs/README.md](README.md) — full reader-journey index of the v1 doc set.
- [INSTALL.md](INSTALL.md) — install-order detail (CRD subchart first), prerequisites per OS, ANTHROPIC_API_KEY + git-credentials Secret recipes.
- [rbac.md](rbac.md) — per-Kind ClusterRole reference, namespace-scoped RoleBinding template (AUTH-02).
- [observability.md](observability.md) — full metric catalog (`tide_secret_leak_blocked_total`, ServiceMonitor gate, OTel + OpenInference setup).
- [dashboard.md](dashboard.md) — dashboard install, port-forward, Ingress shapes.
- [gates.md](gates.md) — per-level gate policy, `tide approve` handshake, bypass annotations.
- [rwx-drivers.md](rwx-drivers.md) — RWX CSI driver matrix and StorageClass override.
- [git-hosts.md](git-hosts.md) — PAT-via-Secret patterns for GitHub / GitLab / Gitea.

---

*Phase 5 DIST-04 deliverable. Locked to ~13 canonical failure modes per D-C4. Per-incident runbooks are deferred to post-v1 (see Phase 5 `<deferred>` notes in `.planning/phases/05-distribution-self-hosting-acceptance/05-CONTEXT.md`).*
