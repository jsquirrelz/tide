# TIDE Small Sample ($0 stub-subagent)

**Audience:** Operators running the $0 small sample as a smoke test against any
TIDE-equipped cluster.

**Status:** v1.0. This sample is the `make dry-run-v1` target (DIST-05) — the
external-operator install-flow proof point that ships as part of the v1
release.

**Scope of this doc:**

- What this sample does (apply / observe / cleanup)
- Why the targetRepo is an RFC 2606 `.example`-TLD https:// sentinel
- Apply / watch / cleanup recipes
- Troubleshooting reminder (CRDs first)

## What this does

Applying this sample drives a TIDE `Project` to `Status.phase=Complete` using
the stub-subagent — a tiny in-cluster binary (`cmd/stub-subagent`) that
returns canned success envelopes regardless of the request shape. The result:

- TIDE's `ProjectReconciler` admits the CRD, runs the init Job (which the
  stub treats as a no-op), and progresses the Phase through to `Complete`.
- Zero LLM API calls. Zero network traffic outside the cluster.
- Zero cost. Repeatable. Safe to run on any TIDE-equipped cluster.

The sample exercises the K8s plumbing — CRD admission, controller dispatch,
Pod lifecycle, status-phase progression — without paying for an LLM round-trip.
It's the first step a new operator takes after `helm install` to confirm
TIDE is wired correctly end-to-end.

Typical wall time: **~1-2 minutes** from `kubectl apply` to `Status=Complete`.

## On the sentinel targetRepo

The Project's `spec.targetRepo` is `https://git.example.internal/stub/no-such-repo.git`
— a deliberately unreachable RFC 2606 reserved-TLD placeholder. The stub-subagent
does NOT resolve `targetRepo`; it returns canned envelopes regardless of what's
there (verified by the stub-subagent's unit test in plan 06 — MEDIUM-7 design lock).

The `https://` scheme is required because the Phase 8 CEL validator now rejects
`file://` URLs at admission: go-git's `file://` transport shells out to a system
`git` binary that is absent from production images, making `file://` an unsupported
production transport. The `.example` TLD is non-routable by design (RFC 2606),
making the sentinel's placeholder intent unmistakable — no real server will ever
resolve this host.

If you ever see the controller log a network error or git-clone failure when
running this sample, that's a regression — the stub-subagent should never
touch `targetRepo`.

## Prerequisites

- A Kubernetes cluster (kind / minikube / any cluster with TIDE installed)
- TIDE CRDs installed: `helm install tide-crds ./charts/tide-crds` (or OCI)
- TIDE controller installed: `helm install tide ./charts/tide` (or OCI)

See [docs/INSTALL.md](../../../docs/INSTALL.md) for the full install recipe.

## Apply

The sample is a single multi-doc YAML file: Namespace + the Phase 8/9
per-namespace dependencies (`tide-projects` PVC, `tide-subagent` and
`tide-reporter` ServiceAccounts + reporter RBAC, a throwaway PVC-warmup pod
for WaitForFirstConsumer storage classes) + the Project itself.

```bash
kubectl apply -f examples/projects/small/project.yaml
```

One dynamic step follows — mirror the cluster-unique signing key the chart
generated in `tide-system` (dispatch Job pods `envFrom` it in their own
namespace, and a static YAML cannot carry a generated secret):

```bash
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata: { name: tide-signing-key, namespace: tide-sample-small }
type: Opaque
data: { TIDE_SIGNING_KEY: ${SIGNING_KEY} }
EOF
```

## Watch

```bash
kubectl get project small-project -n tide-sample-small -w
```

Or wait for completion explicitly:

```bash
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/small-project -n tide-sample-small --timeout=10m
```

Expected outcome: `Status.phase=Complete` within ~1-2 minutes. If it stalls,
see Troubleshooting below.

## Cleanup

```bash
kubectl delete -f examples/projects/small/project.yaml
# Or to remove the namespace and everything in it:
kubectl delete namespace tide-sample-small
```

The Namespace deletion cascades to the Project and all child CRDs (Milestone,
Phase, Plan, Task, Wave) via owner-refs.

## Troubleshooting

**`kubectl apply` returns `no matches for kind "Project" in version
"tideproject.k8s/v1alpha3"`** — the TIDE CRDs are not installed. Install the
CRDs chart FIRST before the controller chart (Pitfall 4):

```bash
helm install tide-crds ./charts/tide-crds -n tide-system --create-namespace
helm install tide ./charts/tide -n tide-system
```

The CRD subchart is intentionally split from the main controller chart (Phase
1 D-E1) so CRDs can be upgraded independently of the controller — but that
split means a fresh cluster needs both, in order.

**Project stays in `Pending` forever** — check the controller logs:

```bash
kubectl logs -n tide-system deploy/tide-controller-manager --tail=100
```

The stub-subagent image must be pullable (`ghcr.io/jsquirrelz/tide-stub-subagent:1.0.9`).
<!-- Canonical form: no-v prefix, matching chart appVersion 1.0.7. The tag must
     track the chart appVersion exactly — the manager and subagent share the
     pkg/dispatch envelope contract and reject version-skewed peers (D-08). -->
If the controller can't pull the image, the Project waits indefinitely. Verify
your cluster has internet access or pre-load the image into the kind node:

```bash
docker pull ghcr.io/jsquirrelz/tide-stub-subagent:1.0.9
kind load docker-image ghcr.io/jsquirrelz/tide-stub-subagent:1.0.9 --name <kind-cluster-name>
```

For the full troubleshooting table, see
[docs/troubleshooting.md](../../../docs/troubleshooting.md) (lands in plan 05-08).

## Related

- [examples/projects/README.md](../README.md) — cost-spectrum overview of all 3 samples
- [examples/projects/large/README.md](../large/README.md) — the $25 acceptance test
- [docs/project-authoring.md](../../../docs/project-authoring.md) — Project.Spec field reference
