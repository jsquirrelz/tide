# TIDE Git Host Configuration

**Audience:** TIDE operators configuring a `Project` CRD to push planning artifacts and code changes to a remote git host.

**Status:** v1.0 ships HTTPS+PAT authentication by default (`ART-02` — host-agnostic auth). SSH is technically supported by the underlying `go-git/v5` library but is deferred to v1.x for the reasons documented in [§ SSH Caveats](#ssh-caveats) below (`ART-05`).

**Scope of this doc:**

- How TIDE authenticates to a git host (PAT-over-HTTPS, the v1.0 default).
- Per-host recipes for creating a minimally-scoped Personal Access Token (PAT) on **GitHub**, **GitLab**, **Gitea**, and any other generic HTTPS git remote.
- How to wire that PAT into a Kubernetes `Secret` that a `Project` CRD references.
- The manual smoke recipe for each host that confirms TIDE successfully pushed a `tide/run-*` branch.
- The SSH host-key caveats explaining why SSH is deferred to v1.x.

## Default Authentication: HTTPS+PAT

TIDE's `cmd/tide-push` binary clones, commits, and pushes via HTTPS using `go-git/v5`. At authentication time, the binary reads the environment variable **`GIT_PAT`** and uses it as the password component of a basic-auth credential (`x-access-token:$GIT_PAT@…` — the same shape GitHub, GitLab, and Gitea all accept on HTTPS endpoints).

The PAT is supplied via Kubernetes `envFrom: secretRef:` on the push Job pod — the `Project.Spec.git.credsSecretRef` field names a `Secret` in the Project's namespace; that Secret's `GIT_PAT` data key is mounted directly into the push Job's container environment. **The controller pod itself never sees the PAT.** The dedicated `tide-push` ServiceAccount has `secrets get` on `secrets` only — no other K8s verb, no other resource — so a compromised push Job pod cannot enumerate Secrets or read Secrets in other namespaces (per the `T-304` mitigation; see [`charts/tide/templates/push-rbac.yaml`](../charts/tide/templates/push-rbac.yaml)).

This design is host-agnostic: GitHub, GitLab (self-hosted or SaaS), Gitea, AWS CodeCommit (with codecommit-grc helper), Azure DevOps, and any other HTTPS-speaking git remote all accept the same PAT-as-password shape.

## GitHub

1. Sign in to GitHub and navigate to **Settings → Developer settings → Personal access tokens**.
2. Choose **Fine-grained tokens** (recommended over Classic tokens — narrower blast radius if exfiltrated; `T-301` mitigation):
   - **Resource owner:** the user or organization that owns the target repository.
   - **Repository access:** "Only select repositories" → pick the single TIDE target repo.
   - **Permissions:** under **Repository permissions**, grant **Contents: Read and write** (this maps to the `repo` scope on Classic tokens but is scoped to the chosen repo).
3. Generate the token and copy the value. GitHub displays it once — store it in a secrets manager immediately.
4. [Skip ahead to § K8s Secret Setup](#k8s-secret-setup) to wire the token into your cluster.

**Classic tokens (alternative):** if your org doesn't allow fine-grained tokens yet, generate a Classic token with the `repo` scope. Note that Classic tokens are owner-scoped, not repo-scoped — exfiltrate-blast-radius is the entire user/org.

## GitLab

1. Sign in to GitLab (gitlab.com or self-hosted) and navigate to **User Settings → Access Tokens**.
2. Name the token (e.g. `tide-push`), set an expiration date.
3. Under **Select scopes**, grant **`write_repository`** only. This scope permits commit/push to repositories the user has access to and **not** API calls or admin actions.
4. Click **Create personal access token**; copy the value. GitLab displays it once.
5. [Skip ahead to § K8s Secret Setup](#k8s-secret-setup).

**Project access tokens (alternative):** GitLab also supports project-scoped access tokens (Settings → Access Tokens at the project level). These are functionally identical for push/clone and have a tighter blast radius. Use them when your organization permits.

## Gitea

1. Sign in to Gitea and navigate to **User Settings → Applications**.
2. Under **Manage Access Tokens**, click **Generate New Token**.
3. Name the token (e.g. `tide-push`) and select permissions. The minimum required permission is `repository: Read and Write`.
4. Click **Generate Token**; copy the value.
5. [Skip ahead to § K8s Secret Setup](#k8s-secret-setup).

**Permissions note:** Gitea's permission model is per-repo. If the TIDE Project spans multiple Gitea repos (rare in v1.0; the `Project.Spec.git.repoURL` field is single-repo), generate one token per repo and pick one for the Secret — or grant the token broader read/write on the user's repos.

## K8s Secret Setup

Create a `Secret` in the Project's namespace with a single data key `GIT_PAT`:

```bash
kubectl create secret generic tide-git-creds \
    --namespace <project-namespace> \
    --from-literal=GIT_PAT='<paste-token-here>'
```

Reference the Secret from the `Project` CRD:

```yaml
apiVersion: tideproject.k8s/v1alpha1
kind: Project
metadata:
  name: my-tide-project
  namespace: my-namespace
spec:
  git:
    repoURL: https://github.com/my-org/my-repo.git
    credsSecretRef: tide-git-creds
  # … remainder of Project.Spec …
```

The TIDE controller will mount the Secret via `envFrom: secretRef: name: tide-git-creds` on each push Job pod's container, populating `GIT_PAT` in the container environment.

### Verifying Secret wiring

```bash
# Inspect the Project's resolved credsSecretRef.
kubectl get project my-tide-project -n my-namespace -o jsonpath='{.spec.git.credsSecretRef}{"\n"}'

# Confirm the referenced Secret exists in the namespace and has the GIT_PAT key.
kubectl get secret tide-git-creds -n my-namespace -o jsonpath='{.data.GIT_PAT}' | base64 -d > /dev/null && echo "GIT_PAT present"
```

## Smoke Recipe (Manual)

After applying a `Project` CRD with valid `repoURL` + `credsSecretRef`, the following sequence should occur:

1. TIDE controller creates `tide-init-<project-uid>` Job to scaffold the workspace PVC.
2. TIDE controller creates `tide-clone-<project-uid>` Job to bare-clone the repo into `/workspace/repo.git` on the shared PVC.
3. Planning waves run; up-stack reconcilers author `MILESTONE.md`, phase briefs, and `PLAN.md` files into `/workspace/artifacts/`.
4. At each level boundary, TIDE controller creates a `tide-push-<project-uid>` Job that:
   - Commits the new artifacts on a per-run branch named `tide/run-<project-name>-<unix-timestamp>` (D-B6).
   - Runs `gitleaks` over the diff (D-B3).
   - Pushes the branch using `--force-with-lease` (D-B6) with the PAT supplied via `GIT_PAT`.

**Per-host expected outcome:** the upstream repo's branch list should show `tide/run-<project-name>-<unix-timestamp>`, with commits whose authors are the TIDE bot identity (configured separately) and whose messages are one of the four fixed D-B2 shapes:

- `tide: plan <name> authored + executed`
- `tide: phase <name> authored`
- `tide: milestone <name> authored`
- `tide: project complete`

**Per-host verification commands** (run from a workstation with the same PAT in `$GH_TOKEN` / `$GITLAB_TOKEN` / `$GITEA_TOKEN`):

```bash
# GitHub
curl -s -H "Authorization: token ${GH_TOKEN}" \
  "https://api.github.com/repos/<owner>/<repo>/branches?per_page=100" | \
  jq -r '.[].name' | grep '^tide/run-'

# GitLab
curl -s --header "PRIVATE-TOKEN: ${GITLAB_TOKEN}" \
  "https://gitlab.com/api/v4/projects/<encoded-path>/repository/branches?per_page=100" | \
  jq -r '.[].name' | grep '^tide/run-'

# Gitea
curl -s -H "Authorization: token ${GITEA_TOKEN}" \
  "https://<gitea-host>/api/v1/repos/<owner>/<repo>/branches" | \
  jq -r '.[].name' | grep '^tide/run-'
```

For multi-host smoke recipes (running the same TIDE Project against GitHub, GitLab, and Gitea in succession to confirm `pkg/git` is host-agnostic per `ART-02`), see the `pkg/git/integration_test.go` integration test scaffolding. The integration test is opt-in (gated on `TIDE_GITLAB_PAT` / `TIDE_GITEA_PAT` env vars) and skipped in CI to keep the test surface portable.

## SSH

SSH authentication is **technically supported** by `go-git/v5` via the `ssh` transport — `pkg/git.Fetch` shipped in Phase 3 plan 03-04 accepts an SSH `repoURL` (`git@github.com:owner/repo.git`) and an SSH-key-bearing Secret as `credsSecretRef`. However, **SSH is NOT the v1.0 default** and ships **un-wired** from the push Job's auth-resolution path. The reasons are documented for `ART-05` traceability:

### Host-key handling is fussy

`go-git/v5`'s SSH transport requires either:

1. A populated `~/.ssh/known_hosts` file in the container — but the `tide-push` image is a scratch / distroless minimal image with no `~/.ssh/`, no shell, and no opportunity for an operator to pre-populate known-hosts before the pod starts.
2. Programmatic `ssh.HostKeyCallback` configuration via the Go API — but this exposes a TOFU-vs-strict trade-off to the operator that has no clean Helm-chart surface in v1.0.

GitHub, GitLab, and Gitea all rotate SSH host keys periodically (rare but non-zero — GitHub did this once in 2023 after an accidental key exposure). A v1.0 SSH default would either:

- Embed host-key fingerprints in the chart (brittle — they go stale silently and break pushes a year later).
- Disable host-key verification (defeats the security purpose of SSH).
- Require operators to mount a custom `known_hosts` ConfigMap (more configuration surface than HTTPS+PAT, no security gain).

**Decision (`ART-05`):** v1.0 ships HTTPS+PAT as the default and only-wired auth path. SSH is deferred to v1.x along with a proper host-key-management story (likely TOFU on first push with operator-acknowledgment annotation, or chart-supplied per-host known_hosts ConfigMap).

### SSH agent forwarding is not container-native

Workstation git workflows commonly rely on `ssh-agent` forwarding for short-lived in-memory keys. The push Job pod cannot reach a workstation's `ssh-agent`, so any v1.x SSH wiring must use long-lived static private keys stored as Secrets — which carry roughly the same blast-radius profile as a fine-grained PAT, with the additional fussiness of host-key handling. The cost/benefit on v1.0 came out clearly in favor of HTTPS+PAT.

### Workaround for v1.0 SSH-only hosts

If your git host is reachable **only** over SSH (no HTTPS endpoint), you must either:

1. Run a local HTTPS-to-SSH proxy (e.g. `cgit` or Gitea sitting in front of the upstream).
2. Wait for v1.x SSH wiring.
3. Contribute a host-key-management proposal — see `.planning/phases/03-up-stack-reconcilers-git-integration-real-subagent-resumptio/03-RESEARCH.md` open questions.

## Per-Project gitleaks Override

TIDE's push Job runs `gitleaks` over each commit diff before pushing (`D-B3`). The default rule set is the upstream `gitleaks v8` config compiled into the binary. Per-Project rule overrides are supported via a `ConfigMap`:

```bash
# Create the override ConfigMap in the Project's namespace.
kubectl create configmap tide-leaks-override \
    --namespace my-namespace \
    --from-file=gitleaks-config.toml=./my-custom-gitleaks.toml
```

Reference it from the Project CRD:

```yaml
spec:
  git:
    repoURL: https://github.com/my-org/my-repo.git
    credsSecretRef: tide-git-creds
    leaksConfigRef: tide-leaks-override
```

The push Job mounts the ConfigMap at `/etc/tide/gitleaks-config.toml` and passes `--leaks-config=/etc/tide/gitleaks-config.toml` to the binary. On a leak match, the push Job exits non-zero with a structured leak summary in stdout; the TIDE reconciler increments the `tide_secret_leak_blocked_total` Prometheus counter and sets the `PushLeaksBlocked` Condition on the Project. Recovery requires either fixing the offending commit content (rare — TIDE-authored commits don't typically embed secrets) or relaxing the override.

A cluster-wide default override is reserved on the Helm chart at `gitleaks.configMapName` (v1.0 deferred — leave empty; per-Project override is the only supported v1.0 path).
