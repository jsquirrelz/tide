# Phase 38: Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 38-Small Independents — Pricing Accuracy, promptFile, Telemetry Nudge, Tech-Debt Carry
**Areas discussed:** Pricing rows + fallback observability, Cache-TTL empirical check (COST-03), promptFile flag semantics, Telemetry nudge + chart coordination

---

## Pricing rows + fallback observability

| Option | Description | Selected |
|--------|-------------|----------|
| Strip -YYYYMMDD suffix only | Exact lookup first; on miss strip trailing date suffix, retry once; else conservative tier | ✓ |
| Family prefix-matching | Longest-prefix match against known family roots; catches more variants, risks mispricing new expensive variants | |
| You decide | | |

**User's choice:** Strip -YYYYMMDD suffix only (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Condition + metric | Envelope flag → reporter → durable Project condition AND manager-side Prometheus counter | ✓ |
| Condition only | Durable status condition, no new metric | |
| Metric only | Prometheus counter — invisible on default prometheus.enabled=false installs | |

**User's choice:** Condition + metric (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Defer to a later milestone | Rows + normalizer + fallback only; new models still need image rebuild | |
| Wire it now | Chart values → subagent env → existing PricingOverrides; batches with milestone chart bump | ✓ |
| You decide | | |

**User's choice:** Wire it now — chart-configurable pricing table pulled into scope
**Notes:** Extends beyond literal COST-01..03 wording; sourced from the folded pricing todo's follow-up suggestion.

| Option | Description | Selected |
|--------|-------------|----------|
| Table tests + run-mix fixture | Unit tests pin rows/normalizer + fixture replaying first-run usage asserting ≈$3.84 | ✓ |
| Table tests only | Skip reconstructing per-dispatch token counts from the perishable PVC | |
| You decide | | |

**User's choice:** Table tests + run-mix fixture (recommended)

---

## Cache-TTL empirical check (COST-03)

| Option | Description | Selected |
|--------|-------------|----------|
| Operator's minikube | Live cluster + real key, CACHE-01 precedent, observes the actual dispatch surface | ✓ |
| Fresh kind cluster | Reproducible but duplicates existing setup | |
| Local CLI + proxy tee | Cheapest; local CLI may differ from subagent image — weaker evidence | |

**User's choice:** Operator's minikube (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Scripted recipe, operator runs | Phase authors probe script + instructions; operator executes, observation recorded | ✓ |
| Executor runs it end-to-end | Fragile assumption about cluster/key reachability | |
| You decide | | |

**User's choice:** Scripted recipe, operator runs (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Single named constant | One cacheWriteMultiplier constant deriving every model's cacheWrite rate, probe evidence cited | ✓ |
| Keep per-model literals | Recompute each row by hand; N places to touch next TTL change | |
| You decide | | |

**User's choice:** Single named constant (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| First plan task + checkpoint | Probe as task 1 with human-verify checkpoint | |
| During research, before planning | Probe runs in /gsd-plan-phase's research stage; planner knows the multiplier | ✓ |
| You decide | | |

**User's choice:** During research, before planning — planning waits on the observed TTL

---

## promptFile flag semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Error on conflict | Manifest outcomePrompt + --prompt-file both set → refuse | ✓ |
| Flag wins, manifest overridden | Convenient but silently ignores manifest content | |
| You decide | | |

**User's choice:** Error on conflict (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Exactly one Project, else error | Zero or multiple Project docs rejected, count named | ✓ |
| Inject into every Project doc | Handles batch applies; almost certainly operator error | |
| You decide | | |

**User's choice:** Exactly one Project, else error (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Verbatim + size cap | Trim single trailing newline, ~256 KiB cap with clear pre-apiserver error | ✓ |
| Fully verbatim, no guards | Let apiserver/etcd reject; cryptic failure mode | |
| You decide | | |

**User's choice:** Verbatim + size cap (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Reject empty | Error before apply on empty/whitespace-only file | ✓ |
| Pass through | Rely on CRD validation surfacing it | |
| You decide | | |

**User's choice:** Reject empty (recommended)

---

## Telemetry nudge + chart coordination

| Option | Description | Selected |
|--------|-------------|----------|
| Short post-install summary + warning | Helm-conventional few lines + conditional telemetry warning | ✓ |
| Warning-only | Smallest diff satisfying TELEM-02 | |
| You decide | | |

**User's choice:** Short post-install summary + warning (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Chart env var on dashboard | prometheus.enabled as env var → dashboard config surface → UI | ✓ |
| Manager API reports it | More indirection for the same install-time bit | |
| You decide | | |

**User's choice:** Chart env var on dashboard (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| kps walkthrough + existing-Prometheus note | Full kube-prometheus-stack path + short variant for existing Prometheus | ✓ |
| kps walkthrough only | Tighter; existing-Prometheus operators adapt alone | |
| You decide | | |

**User's choice:** kps walkthrough + existing-Prometheus note (recommended)

| Option | Description | Selected |
|--------|-------------|----------|
| Fold into the one milestone bump | All v1.0.7 chart changes under a single version bump coordinated with 35/36 | ✓ |
| 38 bumps independently | Self-contained but two bump events in one milestone | |
| You decide | | |

**User's choice:** Fold into the one milestone bump (recommended)

---

## Claude's Discretion

- DEBT-01: retrofit W1 stamp site to the Phase 31/32 RetryOnConflict + optimistic-lock pattern
- DEBT-02: configmap `default 16` → `default 4` (chart edit batches per the bump decision)
- DEBT-03: heavy-spec identification threshold + migration mechanism, spec-count conservation
- Condition/metric naming, exact size-cap value, banner copy/placement

## Deferred Ideas

- ConfigMap-ref promptFile union (`outcomePromptFrom`) — already out of scope per REQUIREMENTS.md
- Per-model TTL/pricing auto-discovery from the provider API — not a requirement; overrides + constant cover the near term
