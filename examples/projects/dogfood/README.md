# TIDE-on-TIDE dogfood runs

**Audience:** TIDE maintainers running TIDE against the TIDE repo itself.

These three Project CRs are not cost-tier samples for new operators — they are
the internal dogfood runs where TIDE authors its own next milestone's artifacts.
Each run targets `https://github.com/jsquirrelz/tide.git` and requires a GitHub
PAT with push access to a non-main branch.

## Run order

The Project CRD has no ordering mechanism. Sequencing is operator discipline:
apply the next manifest only after the previous Project reaches `status.phase: Complete`.

| # | Manifest | Project Name | Namespace | Rationale |
|---|----------|--------------|-----------|-----------|
| 1 | `01-analytics-project.yaml` | `dogfood-analytics` | `tide-dogfood-analytics` | Builds the observability surfaces that make run 2 watchable |
| 2 | `02-codex-runtime-project.yaml` | `dogfood-codex-runtime` | `tide-dogfood-codex` | Run 2's token/dispatch behavior is visible on run 1's new dashboard |
| 3 | `03-project-editor-project.yaml` | `dogfood-project-editor` | `tide-dogfood-editor` | Builds the editor surface that authors subsequent Project CRs — run 3's output creates run 4's input |

## Per-namespace prerequisites

Each namespace needs the same set of per-namespace resources as the medium and
large samples. Use `examples/projects/medium/per-namespace-resources.yaml` as
the template:

- `tide-projects` PVC (workspace for TIDE's artifact push)
- `tide-subagent` ServiceAccount
- `tide-signing-key` Secret mirrored from `tide-system`

Apply per namespace before creating the Project CR:

```bash
# Replace NAMESPACE with tide-dogfood-analytics, tide-dogfood-codex, or
# tide-dogfood-editor as appropriate.
kubectl apply -f examples/projects/medium/per-namespace-resources.yaml \
  -n NAMESPACE
```

## Secret creation

Each namespace needs a `tide-secrets` Secret carrying provider credentials and
a GitHub PAT. The Codex namespace additionally needs an `openai-secrets` Secret.

```bash
# Analytics and editor namespaces
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY=<your-anthropic-api-key> \
  --from-literal=GIT_PAT=<your-github-pat> \
  -n tide-dogfood-analytics

kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY=<your-anthropic-api-key> \
  --from-literal=GIT_PAT=<your-github-pat> \
  -n tide-dogfood-editor

# Codex namespace: same tide-secrets plus the OpenAI key in a separate Secret
kubectl create secret generic tide-secrets \
  --from-literal=ANTHROPIC_API_KEY=<your-anthropic-api-key> \
  --from-literal=GIT_PAT=<your-github-pat> \
  -n tide-dogfood-codex

kubectl create secret generic openai-secrets \
  --from-literal=OPENAI_API_KEY=<your-openai-api-key> \
  -n tide-dogfood-codex
```

Never commit real credentials. The values above are placeholders.

## Applying the manifests

Each manifest contains two YAML documents: a Namespace and a Project. Apply
them in order, waiting for completion before advancing:

```bash
# Run 1
kubectl apply -f examples/projects/dogfood/01-analytics-project.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/dogfood-analytics -n tide-dogfood-analytics --timeout=120m

# Run 2 (only after run 1 completes)
kubectl apply -f examples/projects/dogfood/02-codex-runtime-project.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/dogfood-codex-runtime -n tide-dogfood-codex --timeout=120m

# Run 3 (only after run 2 completes)
kubectl apply -f examples/projects/dogfood/03-project-editor-project.yaml
kubectl wait --for=jsonpath='{.status.phase}'=Complete \
  project/dogfood-project-editor -n tide-dogfood-editor --timeout=120m
```

## Cluster sizing

These are multi-phase real work runs — significantly heavier than the cost-tier
samples. The constrained-VM recipe from CLAUDE.md applies:

- Delete and recreate a fresh kind cluster before each heavy run.
- Pre-warm: provisioner Ready + `kind load busybox:1.36`.
- Never run two heavy runs (or an acceptance run alongside a dogfood run) at
  the same time on a 7.65 GiB VM — two single-node clusters OOM the node.
- The `make acceptance-v1` target spins its own `tide-acceptance-<ts>` cluster;
  never leave a `tide-test` cluster up while running it.

See CLAUDE.md "Constrained-VM full-suite recipe" for the full procedure.

## Gate policy

All three manifests are configured with `milestone: approve` and `phase: approve`
so a human reviews each level before TIDE descends. Change to `auto` for a
fully-autonomous run (one edit in the YAML before applying).
