---
title: Next-session prompt — run TIDE-on-TIDE dogfood #2 (Codex/OpenAI backend)
date: 2026-06-16
context: prepared at the close of v1.0.2 Phase 21; manifest 02-codex-runtime budget capped to $10
manifest: examples/projects/dogfood/02-codex-runtime-project.yaml
---

# Next session: run dogfood #2 (Codex/OpenAI heterogeneous dispatch)

Paste the **Kickoff prompt** below to start the next session. Everything under it
is the runbook/context that session will need.

---

## Kickoff prompt (paste this)

> Run TIDE-on-TIDE dogfood #2 against the TIDE repo using
> `examples/projects/dogfood/02-codex-runtime-project.yaml` (the Codex/OpenAI
> heterogeneous-dispatch run). Budget is hard-capped at **$10** — treat this as a
> bounded probe, not a milestone build; expect it to halt on budget mid-phase and
> that's fine. Bring up a fresh kind cluster per the constrained-VM recipe, deploy
> TIDE + Prometheus + the dashboard, create the secrets (ANTHROPIC_API_KEY +
> GIT_PAT + OPENAI_API_KEY), apply the manifest, and watch spend live on the
> Phase-21 **Cache Efficiency** panel + cost panels in TelemetryView. Observe first:
> read controller logs + `kubectl get project -o yaml` before hypothesizing on any
> stall. While it runs, also knock out Phase 21's two pending human-UAT checks
> (`.planning/phases/21-cost-cache-observability/21-HUMAN-UAT.md`) on the live
> dashboard — they need real dispatch data, which this run produces.

---

## Why $10 is tight (set deliberately by the operator)

The manifest's `outcomePrompt` describes a full multi-phase deliverable (a new
`internal/subagent/codex/` Subagent + heterogeneous dispatch + chart values +
tests). $10 will **not** complete that. With the per-level model tiers in the
manifest (opus-4-8 milestone planner, sonnet phase/plan, haiku task) a $10 cap
realistically buys the milestone + maybe the first phase of planning before the
budget gate halts the Project. That is the intended outcome for this run: prove
the run *starts cleanly, dispatches, accounts cost correctly, and halts on cap* —
i.e. exercise the budget + observability path end-to-end on a real heterogeneous
config — without spending milestone money. If the goal shifts to actually
*landing* the Codex subagent, raise `absoluteCapCents` first.

## Pre-flight runbook (condensed — full recipe in examples/projects/dogfood/README.md)

1. **Cluster (constrained-VM recipe, CLAUDE.md):** delete any stale heavy cluster,
   recreate a fresh single-node kind cluster, pre-warm (provisioner Ready +
   `kind load busybox:1.36`). Never run a second heavy/acceptance cluster
   alongside it (OOM on the 7.65 GiB VM). The durable `kind-tide-dogfood` cluster
   already exists for real-API calls; reuse or rebuild it deliberately.
2. **Deploy TIDE** from the chart with `prometheus.serviceMonitor.enabled=true`
   (so the dashboard's `/api/v1/query_range` proxy has data) and the dashboard
   enabled. Point `prometheus.endpoint` at the in-cluster Prometheus.
3. **Per-namespace resources** in `tide-dogfood-codex` (template:
   `examples/projects/medium/per-namespace-resources.yaml`): `tide-projects` PVC,
   `tide-subagent` SA, `tide-signing-key` Secret mirrored from `tide-system`.
4. **Secrets** in `tide-dogfood-codex`:
   - `tide-secrets`: `ANTHROPIC_API_KEY` (real key lives at `~/.tide/anthropic.key`,
     outside the repo) + `GIT_PAT` (push access to a non-main branch).
   - `openai-secrets`: `OPENAI_API_KEY` (or `CODEX_API_KEY`).
   - macOS SSL caveat from Phase 18: if minting/credproxy work is done on the host,
     macOS ignores `SSL_CERT_FILE` — run that piece inside a `golang:1.26.3` Linux
     container (the `make eval` pattern at `/tmp/tide-eval/`).
5. **Apply:** `kubectl apply -f examples/projects/dogfood/02-codex-runtime-project.yaml`.
   Gates are `milestone: approve` / `phase: approve` — a human approves each
   descent. (Flip both to `auto` in the YAML for unattended.)
6. **Watch:** TelemetryView cost panels + the new **Cache Efficiency** panel; flip
   the per-level selector (Project/Phase/Plan/Wave). Controller logs + `kubectl
   describe project dogfood-codex-runtime` for any BLOCKED gate.

## What changed since this manifest was drafted (v1.0.2, Phases 18–21) — already folded into the manifest

- **Observability now exists in-tree.** The README framed run 1 (analytics) as the
  thing that builds the dashboard run 2 is watched on — but Phase 21 already added
  per-level token labels, the `tide_cache_savings_cents_total` counter, and the
  Cache Efficiency panel + per-level selector directly. So run 2 is watchable
  **today** without first doing run 1. (Run 1/analytics is now largely redundant
  with shipped work — reassess before applying `01-analytics-project.yaml`.)
- **Usage normalization is mandatory for the Codex subagent** (now stated in the
  manifest's outcomePrompt): map OpenAI `prompt_tokens` / `completion_tokens` /
  `prompt_tokens_details.cached_tokens` into `pkg/dispatch.Usage`, and add a
  Codex-side pricing table mirroring `internal/subagent/anthropic/pricing.go` —
  otherwise budget enforcement, `tide_cost_cents_total`, and the cache panel all
  read $0 for Codex dispatches. OpenAI caching is automatic/marker-free (1,024
  floor), so `CacheCreationTokens` stays 0 and there is no `cache_control` to set
  (contrast with the Anthropic CLI path, where caching only fires on the CLI
  scaffold — Phase 20 CACHE-01).
- **Per-provider usage normalizer** (deferred in Phase 20) is the load-bearing
  prerequisite for trustworthy multi-provider cost numbers in this run.

## Pairs with Phase 21 closeout

Phase 21 is `Needs Review` pending two live-cluster UAT items (panel renders from
live counters; per-level selector slices `sum by(<dim>)`). This dogfood run is the
natural way to satisfy them — it produces the real dispatch data the panel needs.
After confirming both on the live dashboard, mark Phase 21 complete
(`/gsd:verify-work 21` or reply "approved" to flip the roadmap row).
