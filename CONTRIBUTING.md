# Contributing to TIDE

**Audience:** Outside contributors to TIDE — engineers picking up issues, sending PRs, or scaffolding a local dev environment for the first time.

**Status:** v1.0 — covers the prerequisites, dev workflow, and PR conventions for the v1.0 release. Updated as the project's tooling evolves.

**Scope of this doc:**

- Prerequisites (Go, kubebuilder, kind, helm, Docker)
- Development workflow (`make test`, `make test-int`, `make test-e2e-kind`, `make lint`)
- Branch naming conventions
- Commit message conventions (Conventional Commits)
- DCO signoff (`git commit -s`)
- Pull request template
- Issue triage

## Prerequisites

The toolchain pins come from `CLAUDE.md` §"Technology Stack". Install them yourself — TIDE does not vendor toolchain binaries.

- **Go 1.26.x** — verify with `go version`. Install via `brew install go` (macOS) or your distro's package manager (Linux). Windows contributors use WSL2.
- **kubebuilder v4.14.0** — `brew install kubebuilder`. Required by `make manifests` and `make generate`.
- **kind v0.31.0** — `brew install kind`. Required by `make test-int` and `make test-e2e-kind` (Layer B kind tier).
- **Helm 3.16.x** — `brew install helm`. Required by `make helm` and any chart-render or chart-install step.
- **Docker** (or a compatible runtime: podman, nerdctl) — required to build container images and feed `kind load docker-image`.

A working `kubectl` on the path is also assumed for envtest setup and cluster interactions.

## Development workflow

Four canonical entry-surface targets cover the test pyramid. Use them rather than ad-hoc `go test` invocations so CI and local runs converge.

- `make test` — unit + envtest suite. Honors the TEST-01 30s budget; `-short` skips the slow leader-election spec.
- `make test-int` — full integration tier (Layer A envtest specs + Layer B kind specs). Requires Docker + kind; runs under the TEST-02 budget (`INTEGRATION_TIMEOUT=1800s`, `KIND_GO_TEST_TIMEOUT=20m`).
- `make test-e2e-kind` — Phase 4 dashboard + gate-flow + tail kind E2E. Builds + helm-installs the chart against a dedicated kind cluster.
- `make lint` — golangci-lint plus the DAG / dispatch / provider import firewalls and TIDE's custom analyzers (crosspool, providerfirewall, metric-cardinality).

Run all four before sending a PR. CI runs them on every push to a PR branch.

## Branch naming

Use a typed prefix that matches the change shape:

- `feat/<short-name>` — new feature, new CRD field, new reconciler path.
- `fix/<short-name>` — bug fix or regression repair.
- `docs/<short-name>` — documentation-only change (no code touched).
- `refactor/<short-name>` — non-functional code reshape (no behavior change, no public-API change).

The short-name is kebab-case and limited to one line — for example, `feat/per-namespace-rolebinding` or `fix/wave-deadline-zero`.

## Commit messages

TIDE uses [Conventional Commits](https://www.conventionalcommits.org/). Use a typed prefix that mirrors the branch-naming verb:

- `feat(scope): description` — adds capability.
- `fix(scope): description` — repairs a defect.
- `docs(scope): description` — documentation-only.
- `chore(scope): description` — tooling, build, or housekeeping (no production behavior change).

The `scope` field is the rough subsystem (`controller`, `dashboard`, `chart`, `cli`, `api`, `dispatch`, `git`, …). Keep the subject line under 72 characters; put detail in the body.

## DCO signoff

All commits must carry the `Signed-off-by:` trailer that the [Developer Certificate of Origin](https://developercertificate.org/) requires. The simplest way is to let git add it for you:

```
git commit -s -m "feat(controller): wire per-namespace rolebinding"
```

The `-s` flag appends `Signed-off-by: Your Name <your@email>` to the commit message using `git config user.name` and `git config user.email`. CI rejects commits without DCO signoff.

## Pull request template

Every PR description should contain three things:

- **Linked issue** — the GitHub issue (or `.planning/`-tracked work item) this PR closes. Use `Closes #N` / `Fixes #N` so the merge auto-closes the issue.
- **Test plan** — which test command proves this works (`make test`, `make test-int`, a targeted `go test ./...`), plus any manual verification the reviewer should repeat.
- **Breaking change?** — explicitly call out CRD schema changes, public API surface changes, Helm value renames/removals, and RBAC scope changes. If the answer is "no," say so — silence reads as "unclear."

## Issue triage

When opening an issue, route it to the right surface:

- **Bugs** — include a reproduction recipe plus the relevant `kubectl get/describe/logs` output (controller logs at `kubectl logs -n tide-system deploy/tide-controller-manager`, CRD status at `kubectl get <crd> <name> -o yaml`). Without observed output, bug reports are not actionable.
- **Feature requests** — reference the v1.x section of `.planning/REQUIREMENTS.md` if your request is already on the roadmap; otherwise describe the operator-facing outcome you want.
- **Questions** — open a [GitHub Discussion](https://github.com/jsquirrelz/tide/discussions), not an Issue. Issues are for actionable, trackable work; questions are conversation.
