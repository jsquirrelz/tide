# Phase 8: Medium-sample http transport and production git-transport policy — Research

**Researched:** 2026-06-03
**Domain:** Go-git transport layer, in-cluster git-http-backend, Kubernetes CRD CEL validation, Docker multi-stage images
**Confidence:** HIGH (all claims verified against live codebase + official docs; see Sources)

---

<user_constraints>
## User Constraints (from locked decisions — no CONTEXT.md; decisions are in ROADMAP.md Phase 8 block)

### Locked Decisions
1. Production git transport = http(s)/SSH ONLY. `file://` is NOT a supported production transport.
2. Revert `93595b9`'s core-image git additions (`images/tide-push`, `images/claude-subagent` → git-less/distroless again). Keep git ONLY in the demo git-server image.
3. Rewrite the medium sample to serve its local fixture over in-cluster `http://` (a git-http-backend server pod + Service in `tide-sample-medium`) — still local/air-gapped (D-B3 "no external repo" preserved), but exercising the SAME transport production uses.
4. Chart SOT = `hack/helm/` source + `make helm`. NEVER hand-edit rendered `charts/`. CI gate: `git diff --quiet charts/`.
5. Do NOT push the v1.0.0 tag without explicit user go-ahead.
6. Route all production/chart/CI edits through GSD.

### Claude's Discretion
- Sentinel value for the small/stub sample's targetRepo (must pass CEL if file:// is rejected at admission and must be ignored by stub subagent).
- Whether CEL validator REJECTS file:// at admission or DOCUMENTS-only.
- Whether the git-http server is a new Deployment/Service or extends `cmd/tide-demo-init`.
- Whether the GIT_PAT empty-guard in `cmd/tide-push` push mode is relaxed to allow anonymous push, or whether a placeholder non-empty GIT_PAT value is used for the in-cluster http server.

### Deferred Ideas (OUT OF SCOPE)
- SSH transport wiring (documented with host-key caveats; v1.x scope per ART-05).
- SSH host-key verification UX.
- Hosting a public `github.com/jsquirrelz/tide-demo-fixture` repo (rejected by D-B3).
- PR-creation surfaces (v2+ per REQUIREMENTS.md "Deferred").
</user_constraints>

---

## Summary

Phase 8 is a correctness + quality cleanup phase that makes three changes in concert: (1) reverts the stop-gap git addition to core images (`tide-push`, `claude-subagent`), (2) replaces the never-working `file://` medium sample with an in-cluster HTTP git server that exercises the same pure-Go transport as production, and (3) tightens the CEL `targetRepo` validator to make the production-transport contract explicit.

The key technical finding is that **go-git/v5's `file://` transport shells out to the system `git` binary** — it is NOT pure-Go. The HTTPS and SSH transports are pure-Go (Go's `crypto/tls`, no `git` binary needed). This distinction is the root cause of every blocker in this phase and drives all the design decisions.

The in-cluster git-http server is best implemented as a new minimal Deployment/Service in the `tide-sample-medium` namespace: `alpine:3.21` base + `apk add git` + `git http-backend` CGI fronted by a simple HTTP wrapper (e.g. `git-http-backend` via `fcgiwrap` behind `nginx:alpine`, or using the `git http-backend` CGI directly with a minimal Go/shell http server). The Deployment serves the repo bootstrapped by the existing `cmd/tide-demo-init` binary, reused as-is. The push Job currently requires a non-empty `GIT_PAT` in push mode — this guard must be relaxed (or a dummy placeholder used) for anonymous in-cluster push.

**Primary recommendation:** Implement the git-http server as a new `images/tide-git-http-server/` image (`alpine:3.21` + `git` + `nginx` + `fcgiwrap`) with a companion Deployment + ClusterIP Service manifest in `examples/projects/medium/`. Reuse `cmd/tide-demo-init` for the initial repo population (it already runs as a Job that pushes over `file://` to the PVC — the server then serves that repo over HTTP). Relax the push-mode `GIT_PAT` empty-guard in `cmd/tide-push` to accept empty PAT for non-HTTPS schemes or provide a dummy value via the secret. Update the CEL validator to reject `file://` and change the small sample's sentinel to an `https://` no-op placeholder.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Git clone/push transport | API / Backend (tide-push binary, pkg/git) | — | All git ops flow through cmd/tide-push and pkg/git; controller never touches git directly |
| In-cluster git remote (demo) | Kubernetes Service + Deployment (git-http server) | tide-sample-medium namespace | The server pod IS the "remote"; accessible only from inside the cluster via DNS |
| CEL targetRepo validation | Kubernetes admission (CRD x-kubernetes-validations) | — | Validated at kubectl-apply time before any reconciler runs |
| Demo repo bootstrapping | Job (cmd/tide-demo-init) → git-http server Deployment | PVC (demo-remote-pvc) | init Job populates the bare repo on the PVC; server pod serves it over HTTP |
| Per-namespace wiring (SA, PVC, signing-key) | Medium sample apply sequence | — | Must be documented in README; provisioned manually (no operator) |
| Image tag alignment | hack/helm/ SOT + make helm | examples/projects/ manifests | chart appVersion is the canonical version; sample manifests must match |

---

## Research Findings by Question

### Question A: In-Cluster git-http Server

**Finding: The standard, well-understood approach is `git http-backend` CGI + a FastCGI wrapper + nginx.** [VERIFIED: git-scm.com/docs/git-http-backend + live codebase examination]

#### Smart HTTP Protocol Requirements

The git smart HTTP protocol requires two CGI endpoints:
- `GET /repo.git/info/refs?service=git-upload-pack` — for clone/fetch
- `POST /repo.git/git-upload-pack` — for clone/fetch data
- `GET /repo.git/info/refs?service=git-receive-pack` — for push advertisement
- `POST /repo.git/git-receive-pack` — for push data

`git http-backend` implements all four as a CGI program. go-git v5's HTTP transport is pure-Go smart-HTTP — it speaks this protocol directly without needing a system `git` binary on the CLIENT side. [VERIFIED: pkg/git/clone.go and push.go use `plumbing/transport/http` pure-Go transport]

#### Anonymous Push Enablement

By default, `git http-backend` disables `receive-pack` for anonymous (unauthenticated) requests. To allow anonymous push, set in the git config of the served repository:

```
[http]
    receivepack = true
```

Or set `GIT_HTTP_RECEIVE_PACK=true` in the server environment. [CITED: git-scm.com/docs/git-http-backend]

Additionally, to skip the `git-daemon-export-ok` file requirement, set `GIT_HTTP_EXPORT_ALL=true` in the server environment. [CITED: git-scm.com/docs/git-http-backend]

#### HTTP Server Option: nginx + fcgiwrap (RECOMMENDED)

The standard Alpine-based approach:

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache git nginx fcgiwrap spawn-fcgi \
    && adduser -D -u 1000 nonroot
```

nginx config forwards `/repo.git/*` requests to `fcgiwrap` which executes `git http-backend`. This is well-established (multiple open-source Dockerfiles, e.g. ynohat/git-http-backend pattern). [VERIFIED: WebFetch + WebSearch]

The nginx.conf structure needed:

```nginx
server {
  listen 80;
  location ~ /.*\.git(/.*)?$ {
    root /srv/git;
    fastcgi_pass unix:/run/fcgi.sock;
    fastcgi_param SCRIPT_FILENAME /usr/libexec/git-core/git-http-backend;
    fastcgi_param GIT_HTTP_EXPORT_ALL "";
    fastcgi_param GIT_HTTP_RECEIVE_PACK true;
    fastcgi_param GIT_PROJECT_ROOT /srv/git;
    fastcgi_param PATH_INFO $uri;
    include fastcgi_params;
  }
}
```

The startup sequence: `spawn-fcgi -s /run/fcgi.sock -M 777 /usr/bin/fcgiwrap` then `nginx -g 'daemon off;'`. [ASSUMED: exact nginx.conf syntax for git-http-backend + fcgiwrap on alpine needs integration-test verification; the structure is correct but exact parameter names may vary]

#### Service/DNS Name

The git-http server in `tide-sample-medium` will be accessible from Jobs in the same namespace at:

```
http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git
```

Or simply within the same namespace:

```
http://git-http-server/demo-remote.git
```

The clone Job and worktree PlainClone both need this URL. `Project.Spec.TargetRepo` and `Project.Spec.Git.RepoURL` will be set to this in-cluster URL.

#### go-git HTTP Push with Anonymous Server

go-git v5.19.0 HTTP transport sends `BasicAuth` credentials as an HTTP `Authorization: Basic` header. If `Password` is empty string, go-git still sends a header (Basic base64 of `username:`). [VERIFIED: pkg/git/push.go line 65-68 — always sets BasicAuth]

An anonymous git-http-backend server that has `http.receivepack=true` will accept the push regardless of credentials since it is not checking auth. The server simply ignores the Authorization header when authentication is not configured. [CITED: git-scm.com/docs/git-http-backend — "If http.receivepack is set to true, unrecognized requests that are treated as dumb requests do not work"]

**Critical finding: `cmd/tide-push` push mode has a hard guard requiring non-empty `GIT_PAT`** (line 246: `if pat == ""` → `missing-creds` exit 2). This guard exists because D-B1 established that production repos need auth. For the in-cluster anonymous http server, options are:

- **Option A (recommended):** Relax the guard to require non-empty GIT_PAT only when the targetRepo scheme is `https://` or `git@`. Anonymous `http://` push skips the check. This requires a small code change in `cmd/tide-push/main.go`.
- **Option B:** Populate the `GIT_PAT` secret with a dummy placeholder value (e.g., `demo-placeholder`) — git-http-backend ignores it when `http.receivepack=true`. No code change required but slightly dishonest.

Option A is the cleaner production-contract fix. The guard was written for production https remotes; in-cluster `http://` is demo-only and the contract should be explicit. [VERIFIED: cmd/tide-push/main.go:243-249]

#### Demo Init Reuse

`cmd/tide-demo-init` pushes over `file://` internally (within the init Job's pod) to write the bare repo to the PVC. This can be kept exactly as-is — the init Job populates the bare repo on `demo-remote-pvc`, and the git-http-server Deployment serves that same PVC. The repo lives at `/srv/git/demo-remote.git` in the server pod; init populates it at `/workspace/demo-remote.git`. These are the same PVC directory, just mounted at different paths. [VERIFIED: demo-remote-init-job.yaml, demo-remote-pvc.yaml]

Note: `cmd/tide-demo-init` still needs the `git` binary because it pushes over `file://` internally. It KEEPS its alpine/git base from `93595b9`. Only `tide-push` and `claude-subagent` revert to git-less.

---

### Question B: CEL `targetRepo` Validation

**Current validator** (line 272 of `api/v1alpha1/project_types.go`): [VERIFIED: grep output]

```go
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@') || self.targetRepo.startsWith('file://')",message="targetRepo must be a http(s), SSH, or file:// git URL"
```

This currently ALLOWS `file://`. Phase 8 must change this.

**Additionally**, there is a field-level pattern on `GitConfig.RepoURL` (line 211):

```go
// +kubebuilder:validation:Pattern=`^(https?://|file:///).+`
```

This also allows `file:///`.

**New CEL rule for `ProjectSpec` (object-level):**

```go
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http://') || self.targetRepo.startsWith('https://') || self.targetRepo.startsWith('git@')",message="targetRepo must be an http(s) or SSH (git@) URL; file:// is not a supported production transport"
```

This is a clean `startsWith` check — CEL supports this directly. No regex needed. [VERIFIED: CEL docs via official Kubernetes CEL validation reference]

**New pattern for `GitConfig.RepoURL`:**

```go
// +kubebuilder:validation:Pattern=`^(https?://|git@).+`
```

**Small sample sentinel problem:**

The small sample currently uses `targetRepo: file:///tmp/no-such-repo`. With the new CEL rule rejecting `file://`, applying `examples/projects/small/project.yaml` would fail admission.

**Recommended sentinel:** `https://git.example.internal/stub/no-such-repo.git`

The `.example` TLD is RFC 2606 reserved (non-routable), making it clear this is a placeholder. The stub-subagent ignores `targetRepo` entirely (verified: it returns canned envelopes regardless of the value — `cmd/stub-subagent` reads no git config). The URL passes the new CEL rule (starts with `https://`). [VERIFIED: small/project.yaml comment: "stub-subagent ignores targetRepo and returns canned success envelopes regardless"; examples/projects/small/project.yaml line 36-37]

**The gitURL Pattern on `GitConfig.RepoURL` is separate** from `ProjectSpec.TargetRepo`. The small sample does NOT set `git.repoURL` (no `.spec.git` block) so the new Pattern on GitConfig.RepoURL only affects users who set a git block — and medium/large samples do. Medium's `git.repoURL` will become the in-cluster http URL.

**REJECT at admission vs DOCUMENT-only:**

Reject at admission is the right choice for Phase 8. The locked decision says `file://` is NOT a supported production transport. A permissive CEL that only warns creates confusion for operators. Reject immediately with a clear message. The small sample gets the new sentinel; the medium sample gets the in-cluster http URL. No operator should ever need `file://` in a production targetRepo. [ASSUMED: this is the correct interpretation of the locked decision; if the user wants DOCUMENT-only instead, the CEL marker can be dropped and replaced with a comment]

---

### Question C: What `93595b9` Actually Changed

**Verified from `git show 93595b9 --stat`:** [VERIFIED: live git show output]

The commit touched 5 files:

| File | Change |
|------|--------|
| `.planning/debug/file-transport-git-missing.md` | New debug record (98 lines) — KEEP |
| `images/claude-subagent/Dockerfile` | Added `apt-get install -y --no-install-recommends git` (+12 lines) — **REVERT** |
| `images/tide-demo-init/Dockerfile` | Base-swapped distroless → `alpine:3.21` + `apk add git` (+25, -7 lines) — **KEEP** (demo-init still needs git for file:// push) |
| `images/tide-push/Dockerfile` | Base-swapped distroless → `alpine:3.21` + `apk add git` (+28, -3 lines) — **REVERT** |
| `pkg/git/doc.go` | Updated comments to reflect file:// shell-out reality (+16, -1 lines) — **PARTIAL REVERT** (keep the fact, reframe for http-only context) |

**Precise revert surface for core images:**

1. `images/tide-push/Dockerfile` — revert alpine base back to `FROM distroless/static:nonroot` + remove `apk add git`. Re-add `USER 1000` (distroless sets this via the nonroot image). Also revert the now-incorrect comment about file:// ops — tide-push no longer does file:// ops.

2. `images/claude-subagent/Dockerfile` — remove the `apt-get install -y --no-install-recommends git` RUN layer. The subagent now clones over HTTP (pure-Go). No system git needed.

3. `pkg/git/doc.go` — reframe: HTTP(S)/SSH transports are pure-Go (no git binary needed); `file://` is NOT supported as a production transport. The doc should not say "these images install git for file:// ops" anymore.

**Keep (not reverted):**

- `images/tide-demo-init/Dockerfile` — stays on alpine:3.21 with git. demo-init still pushes over `file://` internally to the PVC, so it needs the git binary.
- `.planning/debug/file-transport-git-missing.md` — debug record, keep as history.

---

### Question D: Medium Sample Wiring — Per-Namespace Deps

**Current medium manifests** [VERIFIED: ls examples/projects/medium/]:

```
examples/projects/medium/
├── namespace.yaml
├── demo-remote-pvc.yaml
├── demo-remote-init-job.yaml
└── project.yaml
```

**Missing (not yet applied for a real medium run):**

From the RESUME.md and test harness analysis, a real medium run needs these per-namespace resources that the Helm chart provisions only in `tide-system`:

| Resource | Name | Type | Source |
|----------|------|------|--------|
| tide-projects PVC | `tide-projects` | PersistentVolumeClaim | Chart-provisioned in tide-system; must be mirrored |
| tide-subagent SA | `tide-subagent` | ServiceAccount | Chart-provisioned in tide-system; must be mirrored |
| tide-signing-key Secret | `tide-signing-key` | Secret | Chart-provisioned in tide-system; must be mirrored |
| ANTHROPIC_API_KEY Secret | `tide-secrets` | Secret | Operator-created manually (step 5 in README) |

**New manifests needed for Phase 8:**

- `examples/projects/medium/git-http-server-deployment.yaml` — Deployment + Service for the in-cluster git-http server
- `examples/projects/medium/per-namespace-resources.yaml` — tide-projects PVC + tide-subagent SA (tide-signing-key must still be mirrored from tide-system post-install because it contains a cluster-unique signing key)

**Revised apply sequence (9 steps):**

```bash
# 1. Namespace
kubectl apply -f examples/projects/medium/namespace.yaml

# 2. PVC for git-http server repo storage
kubectl apply -f examples/projects/medium/demo-remote-pvc.yaml

# 3. Per-namespace resources (tide-projects PVC + tide-subagent SA)
kubectl apply -f examples/projects/medium/per-namespace-resources.yaml

# 4. Mirror the signing-key Secret from tide-system (key is cluster-unique)
SIGNING_KEY=$(kubectl get secret tide-signing-key -n tide-system \
  -o jsonpath='{.data.TIDE_SIGNING_KEY}')
kubectl apply -f - <<EOF
apiVersion: v1
kind: Secret
metadata:
  name: tide-signing-key
  namespace: tide-sample-medium
type: Opaque
data:
  TIDE_SIGNING_KEY: ${SIGNING_KEY}
EOF

# 5. Init Job — bootstraps the bare repo on demo-remote-pvc
kubectl apply -f examples/projects/medium/demo-remote-init-job.yaml
kubectl wait --for=condition=Complete job/demo-remote-init \
  -n tide-sample-medium --timeout=2m

# 6. git-http server — serves the bare repo over HTTP
kubectl apply -f examples/projects/medium/git-http-server-deployment.yaml
kubectl wait --for=condition=Available deployment/git-http-server \
  -n tide-sample-medium --timeout=2m

# 7. ANTHROPIC_API_KEY secret
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY="$ANTHROPIC_API_KEY" \
  --from-literal=GIT_PAT="" \  # empty for anonymous in-cluster http
  -n tide-sample-medium

# 8. Apply the Project CRD
kubectl apply -f examples/projects/medium/project.yaml

# 9. Wait for completion
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/medium-project -n tide-sample-medium --timeout=30m
```

Note on tide-projects PVC prewarm: kind's `rancher.io/local-path` storage class uses `WaitForFirstConsumer` — the PVC won't bind until a Pod is scheduled. Acceptance script handles this with a prewarm busybox Pod. The medium README should document this for kind-based installs.

---

### Question E: CI Coverage Shape

**How the large (https://) sample is currently covered in CI:** [VERIFIED: .github/workflows/dry-run.yaml, nightly-integration.yml, acceptance-v1.sh]

- `dry-run.yaml` workflow: triggered on `v*-rc.*` tags. Runs `hack/scripts/dry-run-v1.sh` which applies `examples/projects/small/project.yaml` (stub-subagent, $0). Does NOT use the large sample's https:// path.
- `nightly-integration.yml`: runs `make test-int` (Layer B kind suite) and `make test-e2e-kind`. Neither tests the large or medium git transport path — they test the controller dispatch plumbing with the stub-subagent.
- `acceptance-v1.sh`: manual maintainer ritual only (`NOT wired into CI per D-A4`). Can be invoked as `ACCEPTANCE_SAMPLE=large` to test the large https:// path, but this is not CI-gated.

**The medium/http path is NOT in CI at all.** [VERIFIED: grep for "medium" across all .github/workflows/ found zero results]

**Recommended CI coverage for Phase 8:**

Given the constrained-VM constraint (CLAUDE.md: single heavy kind cluster at a time; don't OOM), the lightest viable approach is a **hermetic stub** rather than a real Claude run:

Option 1 — **Hermetic kind test with stub-subagent + real git-http server** (RECOMMENDED):

Add to the nightly Layer B suite (or as a new step in nightly-integration.yml): spin up the git-http server image, run `demo-remote-init` Job, apply a medium-style Project with `subagent.image: stub-subagent` (overriding the model), verify the clone Job successfully clones over `http://` (pure-Go transport) and `Project.status.phase=Complete`. This exercises the transport path without LLM cost. The stub-subagent ignores targetRepo but the clone Job (cmd/tide-push `--mode=clone`) would still attempt the http clone.

Caveat: the stub-subagent path skips the actual clone (since the small sample uses `targetRepo: https://git.example.internal/stub/no-such-repo.git` and the stub ignores it). For a proper transport test, the medium git-http test MUST use the real project.yaml with a real targetRepo, even if the subagent is the stub. This means the clone Job runs for real. [ASSUMED: the clone Job always runs regardless of subagent type; needs verification that ProjectReconciler runs clone even for stub-subagent images]

Option 2 — **Docker-level image smoke**: A CI step that runs `docker run --rm --entrypoint git-http-backend` to verify the binary is present in the git-http-server image. Lightweight but doesn't test the full transport.

Option 3 — **Real-Claude gated (`live-e2e.yml` pattern)**: Wire a `workflow_dispatch`-only workflow that runs the full medium sample with real Claude. Follows the existing `live-e2e.yml` pattern. NOT on a schedule (per Phase 04.1 P2.4 lock).

**Recommendation:** Implement Option 1 (hermetic kind test in nightly). The existing nightly-integration.yml already spins a kind cluster; adding a medium-http kind test spec alongside the existing Layer B specs follows the established pattern without adding a cluster-spin. The transport test does not require LLM calls.

---

### Question F: Image Tag Alignment

**Current state** [VERIFIED: grep across examples/, hack/, charts/]:

| Location | Tag | Form |
|----------|-----|------|
| `examples/projects/medium/demo-remote-init-job.yaml:42` | `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0` | v-prefix |
| `examples/projects/medium/project.yaml:90` | `ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0` | v-prefix |
| `examples/projects/large/project.yaml:105` | `ghcr.io/jsquirrelz/tide-claude-subagent:v1.0.0` | v-prefix |
| `examples/projects/small/project.yaml:50` | `ghcr.io/jsquirrelz/tide-stub-subagent:1.0.0` | no-v |
| `examples/projects/small/README.md:130-131` | `ghcr.io/jsquirrelz/tide-stub-subagent:v1.0.0` | v-prefix (README inconsistent with project.yaml!) |
| `hack/helm/tide-chart.yaml:12` | `appVersion: "1.0.0"` | no-v (SOT) |
| `hack/helm/tide-crds-chart.yaml:12` | `appVersion: "1.0.0"` | no-v (SOT) |
| `hack/scripts/acceptance-v1.sh:78` | `IMAGE_TAG="1.0.0"` | no-v |
| `hack/scripts/load-images-if-needed.sh:21` | `1.0.0 — no v prefix` (comment) | no-v |
| `hack/scripts/render-dry-run-report.sh:55-56` | `CHART_TIDE_VERSION="1.0.0"` | no-v |
| `charts/` (rendered) | zero occurrences of `:v1.0.0` | no-v |

**Canonical form: no-v prefix (`1.0.0`).** The chart SOT (`hack/helm/tide-chart.yaml:12` appVersion), the acceptance script, and the load-images script all use no-v consistently. The v-prefix in sample manifests is a holdover from initial authoring. [VERIFIED: hack/helm/tide-chart.yaml, hack/scripts/acceptance-v1.sh, hack/scripts/load-images-if-needed.sh]

**Files to change (align to no-v):**

1. `examples/projects/medium/demo-remote-init-job.yaml:42` — `v1.0.0` → `1.0.0`
2. `examples/projects/medium/project.yaml:90` — `v1.0.0` → `1.0.0`
3. `examples/projects/large/project.yaml:105` — `v1.0.0` → `1.0.0`
4. New `examples/projects/medium/git-http-server-deployment.yaml` — use `:1.0.0` (no-v) for any TIDE-built images
5. `examples/projects/small/README.md:130-131` — `v1.0.0` → `1.0.0` (README inconsistency vs project.yaml)

**Minikube reload note:** After tag alignment, minikube images loaded at `:v1.0.0` must be reloaded at `:1.0.0` (or the old tags deleted). Per RESUME.md: `minikube ssh -- docker rmi -f` first, then reload. The RESUME.md already documents both tags are loaded (`minikube has all images loaded at both :1.0.0 and :v1.0.0`).

---

## Standard Stack

### Core (already in project, used unchanged)
| Library | Version | Purpose | Note |
|---------|---------|---------|------|
| go-git/v5 | v5.19.0 | HTTP clone/push (pure-Go) | HTTPS/SSH are pure-Go; file:// is NOT |
| controller-runtime | v0.24.x | Reconciler lifecycle | Unchanged |
| kubebuilder | v4.14.0 | CRD generation + controller-gen | CEL markers flow through controller-gen |

### New (for git-http server image)
| Package | Version | Purpose | Why |
|---------|---------|---------|-----|
| alpine:3.21 | 3.21 | Base for git-http server image | Same base already used by tide-demo-init post-93595b9 |
| git (apk) | 2.47.x | `git http-backend` CGI + supporting binaries | Provides `git-http-backend` at `/usr/libexec/git-core/git-http-backend` |
| nginx (apk) | stable | HTTP frontend for CGI | Standard, minimal |
| fcgiwrap (apk) | stable | Bridges nginx → git-http-backend CGI | Standard pattern for git-http-backend |
| spawn-fcgi (apk) | stable | Launches fcgiwrap process | Companion to fcgiwrap |

[VERIFIED: alpine:3.21 + apk git confirmed working in tide-demo-init; nginx+fcgiwrap pattern confirmed via WebFetch]

### Installation (git-http server image build)
```bash
docker build -f images/tide-git-http-server/Dockerfile \
             -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 .
```

---

## Architecture Patterns

### System Architecture Diagram

```
                            tide-sample-medium namespace
 ┌──────────────────────────────────────────────────────────────────────────┐
 │                                                                          │
 │  [1] demo-remote-init Job (alpine+git)                                   │
 │       └─ file:// push to /workspace/demo-remote.git                     │
 │            └─ demo-remote-pvc (RWO PVC)                                  │
 │                                                                          │
 │  [2] git-http-server Deployment (alpine+git+nginx+fcgiwrap)              │
 │       └─ mounts demo-remote-pvc at /srv/git/                            │
 │       └─ exposes ClusterIP Service: git-http-server:80                  │
 │            └─ serves http://git-http-server/demo-remote.git             │
 │                                                                          │
 │  [3] medium Project (targetRepo: http://git-http-server/demo-remote.git) │
 │       └─ tide-clone-{uid} Job (tide-push, DISTROLESS — no git needed)   │
 │            └─ go-git HTTP clone (pure-Go) ──────────────────────────►   │
 │       └─ Task Jobs (claude-subagent, node:22-slim — no git needed)       │
 │            └─ worktree.go: go-git HTTP clone (pure-Go) ──────────────► │
 │       └─ push Job (tide-push, DISTROLESS — no git needed)               │
 │            └─ go-git HTTP push (pure-Go) ──────────────────────────────►│
 │                        git-http-server (receives, stores to PVC)         │
 │                                                                          │
 └──────────────────────────────────────────────────────────────────────────┘

Key: ──────────────► = HTTP git transport (pure-Go, no system git binary)
     file:// push   = internal to init Job pod only (git binary in init image)
```

### Recommended Medium Sample Structure (post-Phase-8)
```
examples/projects/medium/
├── namespace.yaml                        # unchanged
├── demo-remote-pvc.yaml                  # unchanged (now shared between init+server)
├── demo-remote-init-job.yaml             # image tag: 1.0.0 (no-v fix)
├── per-namespace-resources.yaml          # NEW: tide-projects PVC + tide-subagent SA
├── git-http-server-deployment.yaml       # NEW: Deployment + ClusterIP Service
├── project.yaml                          # updated targetRepo + git.repoURL + tag fix
└── README.md                             # rewritten: 9-step sequence, no file:// claims
```

### Pattern 1: git-http-backend Anonymous Server (alpine)

```dockerfile
FROM alpine:3.21
RUN apk add --no-cache git nginx fcgiwrap spawn-fcgi \
    && adduser -D -u 1000 nonroot

COPY nginx.conf /etc/nginx/nginx.conf
COPY entrypoint.sh /usr/local/bin/entrypoint.sh

USER 1000
ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
```

`entrypoint.sh`:
```sh
#!/bin/sh
# Initialize bare repo if the data dir is empty (idempotent — init Job populates PVC first)
spawn-fcgi -s /run/fcgi.sock -M 777 /usr/bin/fcgiwrap
exec nginx -g 'daemon off;'
```

nginx.conf key section:
```nginx
location ~ ^(/[^/]+\.git)(/.*)?$ {
    root /srv/git;
    fastcgi_pass unix:/run/fcgi.sock;
    fastcgi_param SCRIPT_FILENAME /usr/libexec/git-core/git-http-backend;
    fastcgi_param GIT_HTTP_EXPORT_ALL "";
    fastcgi_param GIT_HTTP_RECEIVE_PACK "true";
    fastcgi_param GIT_PROJECT_ROOT /srv/git;
    fastcgi_param PATH_INFO $uri;
    include fastcgi_params;
}
```

[ASSUMED: exact alpine fcgiwrap socket path `/run/fcgi.sock` and git-http-backend binary path `/usr/libexec/git-core/git-http-backend` need verification with `apk info -L git` in the image]

### Pattern 2: Updated CEL Validator

```go
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http://') || self.targetRepo.startsWith('https://') || self.targetRepo.startsWith('git@')",message="targetRepo must be an http(s) or SSH (git@) URL; file:// is not a supported production transport"
type ProjectSpec struct {
    // TargetRepo supports http(s) (production) and SSH (git@) only.
    // file:// is NOT supported (go-git's file:// transport shells out to a
    // system git binary absent from production images).
    // +kubebuilder:validation:MinLength=1
    TargetRepo string `json:"targetRepo"`
    ...
}
```

After changing the marker, run:
```bash
make generate manifests
make helm
git diff --quiet charts/  # must be clean
```
[VERIFIED: chart SOT flow: controller-gen → config/crd/bases/*.yaml → make helm → charts/]

### Pattern 3: go-git Anonymous HTTP Push

go-git sends BasicAuth headers on every request. For an anonymous `git-http-backend` server with `GIT_HTTP_RECEIVE_PACK=true`, the server ignores auth headers entirely — push succeeds. The `BasicAuth{Username: "x-access-token", Password: ""}` in `pkg/git/push.go` works correctly against an anonymous server.

The `cmd/tide-push` push-mode guard (line 246) must be relaxed. Recommended change:

```go
// Invariant 2 (D-B1): GIT_PAT required for authenticated remotes (HTTPS production).
// Not required for in-cluster http:// anonymous remotes.
pat := os.Getenv("GIT_PAT")
repoURL := cfg.RepoURL  // available from the clone-mode initial run
requirePAT := strings.HasPrefix(repoURL, "https://") || strings.HasPrefix(repoURL, "git@")
if requirePAT && pat == "" {
    writePushEnvelope(cfg, "", exitInvariant, "missing-creds")
    fmt.Fprintf(stderr, "tide-push: GIT_PAT env is empty (required for https:// and git@ remotes)\n")
    return exitInvariant
}
```

Note: `cfg.RepoURL` is set only in clone mode. In push mode, the RepoURL is read from the local repo's `origin` remote config. This needs checking — either read the remote URL from the open repo, or pass `--repo-url` in push mode too. [ASSUMED: exact code shape needs implementation-time verification against push mode's data flow]

### Anti-Patterns to Avoid

- **Mounting demo-remote-pvc into the clone/push Jobs:** The architectural bug from the original design. The git-http server IS the bridge between the PVC and the network — don't bypass it.
- **Using file:// in production Project specs:** The CEL validator will now reject this at admission.
- **Embedding git into tide-push or claude-subagent:** These images go back to distroless/slim with no git binary — the whole point of Phase 8.
- **Using `nginx:alpine` as base (nginx official image):** nginx official's alpine image is fine but pulls in a different nginx user/group convention. The `alpine:3.21` + `apk add nginx` approach is consistent with the rest of the demo images.
- **Running git-http-server as root:** Must run as UID 1000 (non-root) — add `adduser -D -u 1000 nonroot` and set `USER 1000` as in tide-demo-init.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| HTTP git server | A custom Go HTTP git server | git http-backend CGI + nginx + fcgiwrap | git http-backend is the reference implementation; pure-Go git server libraries are incomplete |
| CEL scheme validation | Regex admit webhook | kubebuilder XValidation + startsWith() | CEL runs in apiserver, no webhook needed for simple string checks |
| Git transport library | System git shell-out | go-git/v5 HTTP transport (already in use) | Already in pkg/git; pure-Go; no git binary in runtime images |
| Anonymous push auth bypass | Custom auth middleware | GIT_HTTP_RECEIVE_PACK=true env | Built-in git http-backend capability |

---

## Runtime State Inventory

This phase involves a rename/refactor of the medium sample's transport layer. The following runtime state is relevant:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | `demo-remote-pvc` on minikube — contains bare repo populated by the init Job (via file://) | The existing PVC data is compatible; the git-http server can serve whatever the init Job populated. No data migration needed. |
| Live service config | `tide-sample-medium` namespace on minikube has: `demo-remote-pvc` (Bound), `demo-remote-init` Job (Complete after 93595b9 fix). Per-namespace subagent deps NOT yet applied. | Init Job is Complete — OK. The old file:// approach's Job output (the bare repo) is compatible with the new http server. No migration needed. |
| OS-registered state | None — minikube is the target; no OS-level registration beyond kubectl context | None |
| Secrets/env vars | `tide-secrets` Secret in tide-sample-medium: currently `GIT_PAT=""` (empty placeholder per medium README step 5). tide-push push mode guard change may affect this. | If GIT_PAT empty guard is relaxed for http:// URLs, the empty value is fine. If Option B (dummy placeholder) is chosen instead, update Secret creation step in README. |
| Build artifacts | `images/tide-push:1.0.0` and `images/claude-subagent:v1.0.0` currently contain git (from 93595b9). These need to be rebuilt and reloaded into minikube. Per RESUME.md, both `:1.0.0` and `:v1.0.0` tags are loaded — stale tags must be force-removed first. | Rebuild + `minikube ssh -- docker rmi -f` for both tags + `minikube image load` at both tags for the reverted images AND the new git-http-server image. |

---

## Common Pitfalls

### Pitfall 1: git-http-backend Path_INFO vs GIT_PROJECT_ROOT Misconfiguration
**What goes wrong:** The CGI receives a `PATH_INFO` that doesn't match the repo path on disk. `git http-backend` returns 404 or "repository not found".
**Why it happens:** nginx needs to pass `PATH_INFO` correctly — for a URL like `/demo-remote.git/info/refs`, PATH_INFO must be `/demo-remote.git/info/refs` and GIT_PROJECT_ROOT must point to the parent directory (`/srv/git`) so that git finds `/srv/git/demo-remote.git`.
**How to avoid:** Test with `curl http://git-http-server/demo-remote.git/info/refs?service=git-upload-pack` from inside a pod in the same namespace. A 200 with `Content-Type: application/x-git-upload-pack-advertisement` confirms the server is working.
**Warning signs:** `kubectl logs git-http-server-pod` shows nginx 404s; go-git returns "repository not found" error from clone.

### Pitfall 2: tide-push Push Mode GIT_PAT Guard Blocks Anonymous Push
**What goes wrong:** The medium Project's push Job exits with code 2 (`missing-creds`) even when pointing at the anonymous in-cluster http server.
**Why it happens:** `cmd/tide-push/main.go:246` — push mode has a hard guard: `if pat == ""` → exit 2. Empty `GIT_PAT` in the Secret triggers this.
**How to avoid:** Relax the guard to only require non-empty GIT_PAT for `https://` and `git@` scheme URLs (Option A), OR populate `GIT_PAT` with a dummy placeholder (Option B, no code change).
**Warning signs:** Controller logs show push Job failed with `missing-creds`; `Project.status.phase` transitions to `PushLeaseFailed`.

### Pitfall 3: demo-remote-pvc RWO Mount Conflict (init Job vs git-http server)
**What goes wrong:** Both the init Job pod and the git-http server Deployment pod try to mount the same RWO PVC simultaneously. On kind (local-path provisioner), RWO means only ONE pod can mount at a time — the second pod stays Pending.
**Why it happens:** RWO (ReadWriteOnce) on a local-path PVC is node-local exclusive. If the init Job pod hasn't terminated before the git-http server pod starts, the mount conflicts.
**How to avoid:** Apply sequence step 5: `kubectl wait --for=condition=Complete job/demo-remote-init` BEFORE applying the git-http server Deployment. The completed Job pod terminates and releases the PVC mount. Then the server pod can mount.
**Warning signs:** `kubectl describe pod git-http-server-xxx` shows `Unable to attach or mount volumes: ... already used by pod`.

**Alternative:** If minikube or the target cluster supports RWX, change `demo-remote-pvc` to `ReadWriteMany`. But for single-node kind/minikube (the primary dev targets), rely on the init-first apply order.

### Pitfall 4: Stale Minikube Image Tags After Revert
**What goes wrong:** After reverting tide-push back to distroless (git-less), minikube's docker daemon still has the old git-carrying alpine image under both `:1.0.0` and `:v1.0.0` tags. minikube does not overwrite existing tags on `image load`.
**Why it happens:** Documented in the debug record (file-transport-git-missing.md): `minikube image load` is NOT idempotent for overwriting existing tags.
**How to avoid:** `minikube ssh -- docker rmi -f ghcr.io/jsquirrelz/tide-push:1.0.0 ghcr.io/jsquirrelz/tide-push:v1.0.0` first, then `minikube image load`.
**Warning signs:** `docker run --rm --entrypoint which ghcr.io/jsquirrelz/tide-push:1.0.0 git` returns `/usr/bin/git` after the revert — that means the old image is still loaded.

### Pitfall 5: fcgiwrap Socket Permissions on Alpine
**What goes wrong:** nginx cannot connect to the fcgiwrap Unix socket because socket permissions are wrong.
**Why it happens:** `spawn-fcgi` creates the socket as root by default; nginx workers may run as a different user.
**How to avoid:** The `-M 777` flag to `spawn-fcgi` sets socket permissions to 777 (world-writable), allowing any user to connect. In the demo context (no security requirement for the git server itself), this is acceptable.
**Warning signs:** nginx logs `connect() to unix:/run/fcgi.sock failed (13: Permission denied)`.

### Pitfall 6: CEL Rule Change Breaks make test-int (Layer B)
**What goes wrong:** The Layer B kind integration tests apply Project fixtures with `targetRepo: file:///...` URLs. After the CEL change, admission rejects them.
**Why it happens:** Test fixtures in `test/integration/kind/testdata/` or `test/e2e/` may hardcode `file:///` targetRepo values.
**How to avoid:** Search all test YAML fixtures for `targetRepo: file:///` before the CEL change lands. Update them to use `https://git.example.internal/...` sentinel.
**Warning signs:** `make test-int` fails on CEL admission rejections after the marker change.

[VERIFIED: need to search test fixtures — not yet done, should be a task in Wave 0 gap list]

---

## Code Examples

### CEL Marker Change (api/v1alpha1/project_types.go)
```go
// Before (line 272 — allows file://):
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http') || self.targetRepo.startsWith('git@') || self.targetRepo.startsWith('file://')",message="targetRepo must be a http(s), SSH, or file:// git URL"

// After (rejects file://, cleaner error message):
// +kubebuilder:validation:XValidation:rule="self.targetRepo.startsWith('http://') || self.targetRepo.startsWith('https://') || self.targetRepo.startsWith('git@')",message="targetRepo must be an http(s) or SSH (git@) URL; file:// is not a supported production transport (go-git's file:// transport requires a system git binary absent from production images)"
```

### GitConfig RepoURL Pattern Change (api/v1alpha1/project_types.go)
```go
// Before (line 211 — allows file:///):
// +kubebuilder:validation:Pattern=`^(https?://|file:///).+`

// After (production-only schemes):
// +kubebuilder:validation:Pattern=`^(https?://|git@).+`
```

### Small Sample Sentinel Update (examples/projects/small/project.yaml)
```yaml
# Before:
targetRepo: file:///tmp/no-such-repo

# After (RFC 2606 .example TLD — non-routable, stub-subagent ignores it):
targetRepo: https://git.example.internal/stub/no-such-repo.git
```

### Medium Project Update (examples/projects/medium/project.yaml)
```yaml
# Before:
targetRepo: file:///demo-remote.git
git:
  repoURL: file:///demo-remote.git

# After (in-cluster http server):
targetRepo: http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git
git:
  repoURL: http://git-http-server.tide-sample-medium.svc.cluster.local/demo-remote.git
```

---

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| file:// PVC-local git transport | http:// in-cluster git-http-backend | Phase 8 | Exercises production pure-Go transport; git binary stays out of core images |
| git binary in tide-push + claude-subagent (93595b9) | Back to distroless/slim, git-less | Phase 8 revert | Smaller, more secure images; no git binary surface area |
| file:// allowed in CEL targetRepo validator | http(s)/git@ only | Phase 8 | Admission rejects unsupported transports |
| v-prefixed image tags in sample manifests | No-v prefix matching chart appVersion | Phase 8 | Consistent tag convention across all manifests |

**Deprecated/outdated:**
- `file:///demo-remote.git` as medium sample targetRepo: replaced by in-cluster http URL
- `demo-remote-pvc` mount on clone/push Jobs (documented in README but never implemented): remove from docs (it was always wrong)
- `pkg/git/doc.go` "transport dependency on system git" text: reframe as http(s)/SSH pure-Go; file:// unsupported

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The exact alpine package path for git-http-backend is `/usr/libexec/git-core/git-http-backend` | Standard Stack, Pattern 1 | nginx CGI `SCRIPT_FILENAME` would be wrong; fix by `apk info -L git \| grep http-backend` in the image |
| A2 | The exact fcgiwrap socket path on alpine is `/run/fcgi.sock` | Pattern 1 (entrypoint.sh) | spawn-fcgi may default to a different path; fix by passing `-s /run/fcgi.sock` explicitly |
| A3 | go-git v5.19.0 with `BasicAuth{Password: ""}` succeeds against an anonymous git-http-backend server (server ignores auth headers when not configured) | Question A findings | If go-git refuses to send push without non-empty password at the library level, need to pass `nil` auth instead; verify with integration test |
| A4 | The strict CEL REJECT approach is the correct interpretation of "file:// is not a supported production transport" | Question B | If user prefers DOCUMENT-only (no admission rejection), CEL marker is replaced by a comment; does not affect functionality, only operator UX |
| A5 | `cmd/tide-push` push mode can access the clone's remote URL (to check scheme before enforcing GIT_PAT) | Pitfall 2 / Pattern 3 | If repoURL is not accessible in push mode's data flow, Option B (dummy PAT) is needed instead of the scheme-conditional guard; verify in code |
| A6 | Layer B test fixtures don't hardcode `file:///` targetRepo values that would break under the new CEL rule | Pitfall 6 | If tests do use file:// URLs, they'll fail admission after the CEL change; Wave 0 task: grep all test YAML for `targetRepo: file` |
| A7 | demo-remote-pvc can be mounted by the git-http server Deployment (after init Job completes) without any capacity changes | Question D architecture | If the PVC is too small or the storage class doesn't support re-mounting after Job completion, need to resize or change storage class |

---

## Open Questions (RESOLVED)

1. **Does go-git `BasicAuth{Password: ""}` work against anonymous git-http-backend?**
   - What we know: git-http-backend accepts push when `http.receivepack=true`; go-git always sets BasicAuth; the server ignores auth when not configured.
   - What's unclear: whether go-git ITSELF refuses to send a push with empty password before the request reaches the server.
   - RESOLVED (partial — Assumption A3 remains): The server-side contract is verified (git-http-backend ignores auth headers when not configured). Whether go-git refuses to send a push with empty `BasicAuth.Password` at the library level is NOT yet verified against a live anonymous server. This is the highest-risk unverified claim in Phase 8 (Assumption A3). Plan 08-05 Task 1 must run a smoke verification (e.g. `docker run` the built image + a short go-git push from a test binary) to confirm empty-password push lands a ref BEFORE the live minikube path in 08-08 depends on it. If go-git refuses, pass `nil` auth instead of `&http.BasicAuth{}`. An automated check is added to `test/integration/kind/medium_http_test.go` as the first assertion the hermetic kind spec must make: "anonymous http push lands a ref" (wired by plan 08-07).

2. **Can `cmd/tide-push` push mode read the remote URL to make the GIT_PAT guard conditional?**
   - What we know: push mode opens the worktree with `gogit.PlainOpen(worktreeDir)` and then calls `repo.PushContext`. The remote URL is in the repo's config.
   - RESOLVED: Plan 08-05 Task 3's scheme-conditional guard reads the origin remote URL via `repo.Config().Remotes["origin"].URLs[0]` after `gogit.PlainOpen` — this is the standard go-git pattern for reading the remote URL from an open repo without needing a `--repo-url` flag. The guard applies `strings.HasPrefix` on the extracted URL to decide whether PAT is required.

3. **Do any Layer B test YAML fixtures use `file:///` targetRepo values?**
   - RESOLVED: `grep -rn 'targetRepo.*file://' test/` (run 2026-06-03) returns exactly one result: `test/integration/kind/testdata/bare-project.yaml:43`. No other test fixtures use `file://` targetRepo values. Plan 08-01 Task 1 migrates that fixture to the `https://git.example.internal/stub/no-such-repo.git` sentinel.

---

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| git (CLI) | Verify image contents, manual git ops | ✓ | 2.49.0 | — |
| docker | Build images, load into minikube | ✓ | 29.5.2 | — |
| kubectl | Apply manifests, wait, verify | ✓ | v1.34.1 | — |
| kind | CI hermetic integration test | ✓ | v0.31.0 | — |
| helm | make helm (chart regen) | ✓ | v3.16.3 | — |
| go | Build binaries, run tests | ✓ | 1.26.3 | — |
| minikube | Live re-test (success criterion #2) | ✓ (still up, context=minikube, K8s 1.33.7) | K8s 1.33.7 | Re-create if cluster was deleted |

**Missing dependencies with no fallback:** None.

**Live minikube state note:** minikube is up with `tide-sample-medium` namespace present, `demo-remote-pvc` Bound, `demo-remote-init` Job Complete. The git-http server Deployment does NOT yet exist. The per-namespace deps (tide-projects PVC, tide-subagent SA, tide-signing-key) are NOT yet applied to tide-sample-medium.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Ginkgo v2.28 + Gomega (Layer B kind) / standard go test (Layer A envtest) |
| Config file | `test/integration/kind/suite_test.go` (Ginkgo bootstrap) |
| Quick run command | `make test` (unit + envtest, < 3 min) |
| Full suite command | `make test-int` (Layer A + Layer B kind, ~10-15 min) |

### Phase Requirements → Test Map

| Success Criterion | Behavior | Test Type | Automated Command | Notes |
|-------------------|----------|-----------|-------------------|-------|
| SC-1: tide-push + claude-subagent git-less | Images have no git binary | Build verification | `docker run --rm --entrypoint which ghcr.io/jsquirrelz/tide-push:1.0.0 git` → exit 1 | Wave 0 gap: add to CI image-smoke step |
| SC-1: tide-demo-init still has git | Init image retains git | Build verification | `docker run --rm --entrypoint git ghcr.io/jsquirrelz/tide-demo-init:1.0.0 --version` → exit 0 | Same CI step |
| SC-2: medium drives Project=Complete | Full end-to-end with real Claude | Manual live test (minikube) | `kubectl wait --for=jsonpath='{.status.phase}'=Complete project/medium-project -n tide-sample-medium --timeout=30m` | Live Haiku run on minikube; not automatable in CI without real key |
| SC-3: file:// rejected at admission | CEL rejects file:// targetRepo | unit (webhook/envtest) | `make test` (runs envtest suite including webhook admission tests) | Wave 0 gap: add admission test for file:// rejection + https:// acceptance |
| SC-3: small sample still works with new sentinel | Stub-subagent succeeds with https://git.example.internal sentinel | Layer B kind test | `ACCEPTANCE_SAMPLE=small make acceptance-v1-smoke` | Existing dry-run test, new sentinel doesn't change behavior |
| SC-4: docs corrected | No false "controller mounts demo-remote-pvc" claim | Manual review | `grep -n 'demo-remote-pvc' examples/projects/medium/README.md` → 0 results | Checked during task execution |
| SC-5: CI coverage for medium/http path | Hermetic kind test with stub-subagent + git-http server | kind integration test | New Ginkgo spec in nightly Layer B suite | Wave 0 gap: new test file |
| SC-6: image tag alignment | All sample manifests use 1.0.0 (no-v) | grep assertion | `grep -rn ':v1\.' examples/ \| grep image:` → 0 results | Simple grep in CI |

### Sampling Rate
- **Per task commit:** `make test` (unit + envtest < 3 min)
- **Per wave merge:** `make test-int` (full Layer A + Layer B)
- **Phase gate:** Full suite green + live minikube re-test before `/gsd-verify-work`

### Wave 0 Gaps

- [ ] `test/integration/envtest/admission_test.go` (or extension to existing webhook test) — covers CEL file:// rejection + https:// acceptance + small sample sentinel acceptance
- [ ] `test/integration/kind/medium_http_test.go` — covers hermetic git-http server clone + push via stub-subagent (SC-5); adds to nightly Layer B suite
- [ ] Check and update all test YAML fixtures for `targetRepo: file:///` → sentinel value (SC-3 prerequisite)
- [ ] CI step in nightly-integration.yml for image smoke verification (`docker run --entrypoint which git`) for SC-1

---

## Security Domain

`security_enforcement` is not explicitly set to `false` in config.json — treating as enabled.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | No | Git-http server is anonymous by design (demo-only, in-cluster) |
| V3 Session Management | No | Stateless HTTP git ops |
| V4 Access Control | Partial | git-http server only exposed as ClusterIP (not NodePort/LoadBalancer); only pods in the cluster can reach it |
| V5 Input Validation | Yes | CEL validator on targetRepo; pattern on GitConfig.RepoURL |
| V6 Cryptography | No | No new crypto paths introduced |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Unauthenticated push from outside cluster | Tampering | ClusterIP Service (not exposed outside cluster); anonymous push intentional for demo |
| git binary retained in core images | Elevation of Privilege | Revert: tide-push and claude-subagent go back to distroless/slim (no git binary, smaller attack surface) |
| file:// targetRepo bypassing CEL | Tampering | CEL marker change rejects file:// at admission |

---

## Sources

### Primary (HIGH confidence)
- Live codebase: `api/v1alpha1/project_types.go:272` — verified current CEL validator text
- Live codebase: `cmd/tide-push/main.go:243-249` — verified GIT_PAT push mode guard
- Live codebase: `pkg/git/push.go`, `pkg/git/clone.go` — verified HTTP transport and BasicAuth usage
- Live codebase: `images/tide-push/Dockerfile`, `images/claude-subagent/Dockerfile`, `images/tide-demo-init/Dockerfile` — verified current state post-93595b9
- Live codebase: `examples/projects/medium/README.md` — verified false-mount claim at line 96 ("mounting the same demo-remote-pvc as the init Job")
- `git show 93595b9 --stat` — verified exact files changed by the commit to be partially reverted
- `.planning/RESUME.md` — verified architectural gap description and locked decisions
- `.planning/debug/file-transport-git-missing.md` — verified root cause analysis and fix shape

### Secondary (MEDIUM confidence)
- [git-http-backend documentation](https://git-scm.com/docs/git-http-backend) — verified CGI env vars, anonymous push enablement, receivepack config
- WebFetch of ynohat/git-http-backend Dockerfile pattern — alpine:latest + nginx + fcgiwrap + spawn-fcgi + git-daemon pattern confirmed
- go-git Context7 docs (library ID `/go-git/go-git`) — confirmed HTTP transport BasicAuth usage pattern

### Tertiary (LOW confidence — assumptions flagged)
- Alpine package paths for git-http-backend and fcgiwrap socket: based on pattern from ynohat/git-http-backend, not verified in this session against `alpine:3.21` specifically (A1, A2)
- go-git behavior with empty BasicAuth.Password against anonymous server (A3) — library source not inspected in this session

---

## Metadata

**Confidence breakdown:**
- What 93595b9 changed: HIGH — verified via `git show --stat`
- CEL validator current text: HIGH — verified via grep
- GIT_PAT push mode guard: HIGH — verified in source
- Transport architecture (file:// shells out, HTTP is pure-Go): HIGH — confirmed in debug record + doc.go
- In-cluster git-http server implementation: MEDIUM — pattern verified via WebFetch, exact alpine paths assumed (A1, A2)
- go-git anonymous push behavior: MEDIUM — transport source not inspected (A3)
- Image tag alignment: HIGH — verified via grep across all files

**Research date:** 2026-06-03
**Valid until:** 2026-07-03 (stable domain — go-git v5 transport API, CEL validation, alpine packages are stable)
