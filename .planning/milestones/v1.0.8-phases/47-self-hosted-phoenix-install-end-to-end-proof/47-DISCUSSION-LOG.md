# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-17
**Phase:** 47-Self-Hosted Phoenix Install + End-to-End Proof
**Mode:** `--auto` — all gray areas auto-selected; each question resolved to the research-recommended option (or locked prior posture) without user prompts. Every selection logged below.
**Areas discussed:** Doc placement & recipe shape, Chart pin & version floor, Storage paths & VM sizing, Auth posture, OTLP wiring & NOTES.txt nudge, Live-proof environment/run/evidence

---

## Doc placement & recipe shape

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror the Prometheus telemetry precedent | INSTALL.md "Enable tracing (Phoenix)" step + observability.md reference recipe | ✓ |
| All-in-INSTALL.md | Single long install section; observability.md only cross-links | |
| New docs/phoenix.md | Dedicated doc; both existing files cross-link | |

**[auto] Selected:** Mirror the Prometheus precedent — PHX-01's letter names both files; the TELEM-01 "Enable telemetry (Prometheus)" section is the proven three-beat structure (install external thing → set TIDE values → verify), and observability.md already holds the placeholder ("install instructions land in Phase 47").

---

## Chart pin & version floor

| Option | Description | Selected |
|--------|-------------|----------|
| Live re-fetch + three-point re-verify | Fresh Chart.yaml fetch at research time; pin exact string; re-verify appVersion ≥ 14.2.0, auth default, storage exclusivity | ✓ |
| Trust STACK.md's 10.0.0/18.0.0 pin | Use the recorded research pin as-is | |
| Floating version in docs | Document `--version` omitted, let operators get latest | |

**[auto] Selected:** Live re-fetch + three-point re-verify — the ROADMAP research flag is explicit and binding; STACK.md itself marks the pin MEDIUM-confidence; observability.md already commits Phase 47 to the ≥ 14.2.0 deep-link floor check; REQUIREMENTS' auth-default claim and research Pitfall 8's differ (chart churned between authorings) and must be settled against the fetched chart.

---

## Storage paths & constrained-VM sizing

| Option | Description | Selected |
|--------|-------------|----------|
| Chart's own two strategies, right-sized | SQLite-on-PVC = quickstart/kind-dev; Postgres = production; few-Gi PVC + bounded retention for the 8 GiB VM; proof runs SQLite path | ✓ |
| Postgres-first recipe | Chart default as the only documented path | |
| Copy Phoenix example values verbatim | 20Gi PVC, default retention | |

**[auto] Selected:** Chart's own two strategies right-sized — PHX-01 requires BOTH paths; research STACK says "mirror the chart's own two documented strategies exactly, don't invent a third"; Pitfalls 8/9 make the 20Gi/infinite-retention defaults unacceptable on the ~8 GiB dev VM with full message arrays in spans. Proof exercises the SQLite path (it IS the kind/dev posture); doc stays honest that Postgres wasn't live-proven.

---

## Auth posture

| Option | Description | Selected |
|--------|-------------|----------|
| Auth ON + Secret-sourced credentials | Keep the chart's auth; PHOENIX_SECRET/admin password from a K8s Secret; call out the chart-vs-image default divergence; dev-only opt-out mentioned with warning | ✓ |
| Auth OFF for dev quickstart | Disable auth in the recipe; simpler first run | |
| Silent on auth | Let operators discover the login page | |

**[auto] Selected:** Auth ON + Secret-sourced credentials — Pitfall 8: an unauthenticated LAN-reachable Phoenix holding full prompt/completion history is its own secret-exposure surface; the Secret pattern mirrors TIDE's existing cred posture; PHX-01's letter requires the default called out either way.

---

## OTLP wiring & NOTES.txt nudge

| Option | Description | Selected |
|--------|-------------|----------|
| Bare host:port → 4317 + augment-script NOTES.txt edit + render gate | Explicit scheme-prefix warning; full values→env chain; nudge mirrors prometheus block; edit heredoc in augment-tide-chart.sh; helm-template contract test | ✓ |
| Doc-only wiring, no NOTES.txt test | Write the nudge in the template only | |
| Point at 6006 | Phoenix's UI/HTTP port | |

**[auto] Selected:** The full-chain option — PHX-02's letter demands the bare host:port form and the NOTES.txt nudge; TIDE's exporter is gRPC-only so 4317 is the only correct target; NOTES.txt's own header says template-only edits are reverted by the augment script; the Phase 38/46 render-gate precedent makes the contract test the house norm.

---

## Live-proof environment, run shape & evidence

| Option | Description | Selected |
|--------|-------------|----------|
| Fresh kind cluster from the recipe + small real run + phase-dir evidence | Recipe proven by execution; small project, real key, pre-flight static checks; screenshots + trace IDs + DSL query in phase dir; deep link exercised | ✓ |
| Reuse tide-dogfood cluster | Existing durable cluster | |
| Synthetic/no-spend proof | Replay fixtures into Phoenix without a live run | |

**[auto] Selected:** Fresh-cluster-from-the-recipe — tide-dogfood predates the v1alpha3 crank (currency unverified; reuse only if verified); a fresh cluster following the doc's own steps makes the proof validate the recipe (research: "Looks Done But Isn't" only surfaces on a real inspected trace tree); PROOF-01's letter requires a LIVE run with redacted Task-level message arrays, so synthetic replay can't satisfy it. Pre-flight static checks before real spend per the locked working preference. Evidence in the phase dir per the Phase 22 milestone-close precedent; deep link exercised live as free Phase 46 validation.

---

## Claude's Discretion

- Phoenix namespace name; exact PVC/retention numbers; INSTALL.md/observability.md content-split details; evidence file shapes; connectivity-check mechanics; NOTES.txt nudge wording; optional hero screenshot in docs/.

## Deferred Ideas

- Postgres path live-proof (documented honest, not proven on dev VM).
- Multi-node clock-skew validation (Pitfall 5 known limitation).
- Tail-sampling collector / Grafana / alerts (observability.md "What's coming").
- Data-minimization toggle (carried from Phase 46).

## Todo Review (auto-fold override)

`todo.match-phase` returned 4 matches at score 0.6 (≥ the 0.4 auto-fold threshold). The mechanical fold was deliberately overridden: all four are generic-keyword false-positives carrying locked not-folded dispositions from Phases 42–46 (GPG signing, two W-2 dispatch-gate concerns, CACHE-F1), and folding them into a docs+proof phase would violate the fixed phase boundary. Carried forward as reviewed-not-folded in CONTEXT.md `<deferred>`.
