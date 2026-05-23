# TIDE Samples

**Audience:** Operators trying TIDE end-to-end against three cost tiers.

**Status:** v1.0. Three cost-spectrum samples (D-B1). The **medium** sample is
authored separately in plan 05-12 because it depends on `cmd/tide-demo-init`
(also plan 05-12) — this directory ships **small** and **large** today; the
medium entry below is the forward link.

**Scope of this doc:**

- Cost-spectrum overview for first-time operators
- Per-sample cost / LLM / target / run summary
- Forward link to `docs/project-authoring.md` for the field-by-field walkthrough

## Cost spectrum

The three samples are ordered by the discriminator new operators care about
most — cost. Start with **small** ($0, no API key needed) before paying a cent,
then graduate to **medium** for a real LLM-driven plan against a local-only
remote, then to **large** for the full v1 acceptance ritual on this TIDE repo.

| Sample            | Cost  | LLM                       | Target                                              | Run with                                                |
| ----------------- | ----- | ------------------------- | --------------------------------------------------- | ------------------------------------------------------- |
| [small](small/)   | $0    | stub-subagent (canned)    | placeholder `file://` path (ignored by stub)        | `kubectl apply -f examples/projects/small/`             |
| [medium](medium/) | ~$5   | Claude (cheap model)      | local-only `file://` git remote (in-cluster init)   | apply PVC + init Job + project.yaml (see medium/README) |
| [large](large/)   | ~$25  | Claude (real model)       | THIS TIDE repo (`github.com/jsquirrelz/tide.git`)   | `make acceptance-v1` (maintainer ritual; plan 05-15)    |

**small** is the `make dry-run-v1` target (DIST-05) — $0, stub-subagent canned
envelopes, repeatable without burning API quota. The stub-subagent ignores
`targetRepo` entirely, so the placeholder `file:///tmp/no-such-repo` value is
load-bearing-by-virtue-of-being-ignored: the smoke test exercises the K8s
plumbing (CRD admission, controller dispatch, Pod lifecycle) without any
network or LLM cost.

**medium** is the first taste of real LLM-driven planning. The sample applies a
PVC + init Job (driven by `cmd/tide-demo-init`, plan 05-12) which bootstraps a
fresh local-only git remote from `examples/tide-demo-fixture/`. TIDE clones
from that local remote, plans, commits artifacts back, and pushes — all
contained on the operator's own machine. Zero external dependencies.

**large** IS the v1 acceptance test (BOOT-04). It targets THIS TIDE repo and
drives TIDE to author the scaffold for `internal/subagent/openai/` mirroring
the existing `internal/subagent/anthropic/` layering (Phase 3 D-C1). Single
Phase scope. Hard $25 cap, no bypass. Run via `make acceptance-v1` (lands in
plan 05-15) — NOT directly via `kubectl apply` (the make target sets up the
cluster, secrets, watch, and evidence capture).

## Prerequisites by sample

| Sample | kind cluster | TIDE charts installed | ANTHROPIC_API_KEY Secret | git push creds Secret |
| ------ | ------------ | --------------------- | ------------------------ | --------------------- |
| small  | yes          | yes                   | no                       | no                    |
| medium | yes          | yes                   | yes                      | no (file:// is local) |
| large  | yes          | yes                   | yes                      | yes (GitHub PAT)      |

See [docs/INSTALL.md](../../docs/INSTALL.md) for the cluster + chart install
recipe and [docs/project-authoring.md](../../docs/project-authoring.md) for
the per-field walkthrough of `Project.Spec`.

## A note on namespaces

Each sample lives in its own namespace: `tide-sample-small`,
`tide-sample-medium`, `tide-sample-large`. These are deliberately distinct
from Phase 1's `tide-samples` (plural) kubebuilder fixture namespace — the
two paths serve different audiences and never share resources.
