# Phase 47: Self-Hosted Phoenix Install + End-to-End Proof - Context

**Gathered:** 2026-07-17 (--auto mode: all gray areas auto-resolved to the research-recommended option; every selection logged in 47-DISCUSSION-LOG.md)
**Status:** Ready for planning

<domain>
## Phase Boundary

Close the milestone: an operator can stand up a self-hosted Phoenix from documented, non-default-safe overrides, point TIDE's existing `otel.exporter.endpoint` at it, and see a real run's complete five-level trace tree — including redacted message arrays at the Task level — rendered and queryable.

1. **PHX-01 (install recipe):** INSTALL.md/observability.md document a self-hosted Phoenix recipe using Phoenix's official OCI Helm chart (`oci://registry-1.docker.io/arizephoenix/phoenix-helm`), covering BOTH storage paths (PVC-backed SQLite for kind/dev matching TIDE's posture; bundled Postgres for durability) and explicitly calling out the chart's `auth.enableAuth=true` default (opposite of the raw image default).
2. **PHX-02 (wiring docs + nudge):** The `otel.exporter.endpoint` wiring is documented end-to-end — including the bare `host:port` requirement (`otlptracegrpc.WithEndpoint` silently breaks on scheme-prefixed values) — and NOTES.txt nudges toward the Phoenix step when tracing is dark.
3. **PROOF-01 (the milestone acceptance bar):** A live run's complete five-level trace tree — including redacted message arrays at the Task level — is visible and queryable in a self-hosted Phoenix, with evidence (screenshots + trace IDs) captured at milestone close.

**Requirements:** PHX-01, PHX-02, PROOF-01 (ROADMAP.md Phase 47 section, 3 success criteria). Depends on Phase 46 (complete — the full enrichment + deep-link surface exists, so the live proof is meaningful).

**ROADMAP research flag (binding):** re-fetch the Phoenix chart/appVersion pin fresh immediately before authoring INSTALL.md — the chart ships near-daily (9 versions in ~9 days at research time); the `10.0.0`/`18.0.0` recorded in `.planning/research/STACK.md` must not be trusted without a live check.

**Explicitly NOT this phase:** Phoenix as a TIDE subchart or bundled manifest (locked Out of Scope — documented-install posture, TELEM-01 precedent); OTel Collector middle tier / tail sampling (Out of Scope); any change to span emission, redaction, size bounds, or enrichment (Phases 42–46 stand as shipped — this phase emits nothing new, it installs, documents, and proves); a bespoke trace-viewer UI (link out, don't rebuild); metrics migration (Prometheus stays the metrics train).

</domain>

<decisions>
## Implementation Decisions

### Doc placement & recipe shape (PHX-01/PHX-02 surfaces)
- **D-01:** Mirror the Prometheus telemetry precedent exactly: `docs/INSTALL.md` gains an **"Enable tracing (Phoenix)"** step-shaped section beside the existing "Enable telemetry (Prometheus)" section (line ~175) carrying the quickstart path; `docs/observability.md` carries the full reference recipe (both storage paths, auth, retention, sizing) under its Tracing section — replacing the "install instructions land in Phase 47" placeholder in the `phoenix.baseURL` deep-link section with a cross-reference. PHX-01's letter names both files; INSTALL.md is the step-by-step entry, observability.md the reference depth. Exact content split within that frame is planner's discretion.
- **D-02:** The recipe is a **separate `helm install` of Phoenix's official OCI chart into its own namespace** — never a TIDE subchart (locked posture, TELEM-01 precedent: the chart ships a ServiceMonitor but no Prometheus; same play here — TIDE ships an endpoint value but no Phoenix). Every example pins the exact chart version string — never `latest`/floating.

### Chart pin & version floor (the ROADMAP research flag)
- **D-03:** Research re-fetches the chart's `Chart.yaml` **fresh at phase-research time** and the fetched pin (chart version + appVersion + image tag) is what lands in every doc example. Record the re-verified pin and fetch date in the phase RESEARCH.md so the freshness chain is auditable.
- **D-04:** The pin check must also re-verify three posture facts against the freshly-fetched chart, because the chart churns near-daily and research/STACK/REQUIREMENTS were written weeks apart: (a) **appVersion ≥ 14.2.0** — the deep-link floor for `/redirects/traces|spans/*` routes that `docs/observability.md` already commits this phase to re-checking; (b) the **current `auth.enableAuth` default** (REQUIREMENTS says chart-default true, opposite of the raw image default; research Pitfall 8 described the image-level default — verify what the fetched chart actually ships); (c) the **storage-mode exclusivity** (`persistence.enabled` vs `postgresql.enabled` cannot be enabled simultaneously per Phoenix's own docs). Divergences update the doc text, not just the pin number.

### Storage paths & constrained-VM sizing (PHX-01)
- **D-05:** Document exactly the chart's own two strategies, don't invent a third: **SQLite-on-PVC** (`persistence.enabled=true`, `postgresql.enabled=false`) as the "quickstart / kind-dev" path matching TIDE's own posture, **bundled Postgres** (chart default) as the "durable / production" path.
- **D-06:** Right-size explicitly for the documented ~8 GiB dev-VM constraint (Pitfall 9): a small `persistence.size` (a few Gi, not the chart's 20Gi default), a **bounded, non-zero `database.defaultRetentionPolicyDays`** (chart default 0 = never expire — unacceptable given full message arrays in spans; mirror the `prometheus.retentionTime` "size it for your run cadence" doc pattern), and fold Phoenix into the existing one-heavy-workload-at-a-time VM discipline. Exact numbers are planner/research discretion grounded in real fixture-scale trace volumes.
- **D-07:** The live proof exercises the **SQLite-on-PVC path** (the kind/dev posture the proof environment actually is). The Postgres path ships documented as the chart's tested happy path but is not live-proven on the dev VM — the doc is honest about which path the proof exercised; no implied both-paths-verified claim.

### Auth posture (PHX-01)
- **D-08:** The documented recipe **keeps auth ON with Secret-sourced credentials** (`PHOENIX_SECRET` / admin initial password from a K8s Secret — mirroring TIDE's existing git-creds/API-key Secret pattern; Pitfall 8's exposure logic: a LAN-reachable Phoenix holding full prompt/completion history is its own secret surface nothing else in TIDE's threat model covers). The chart-default-vs-raw-image-default divergence is explicitly called out per PHX-01's letter. A dev-only auth-off override may be mentioned honestly alongside the exposure warning (port-forward-only access framing). The exact current chart default is verified fresh per D-04(b).

### OTLP wiring & NOTES.txt nudge (PHX-02)
- **D-09:** `otel.exporter.endpoint` is documented end-to-end in the **bare `host:port` form** (e.g. `phoenix-svc.<ns>.svc:4317` shape) targeting Phoenix's **4317 gRPC** port — TIDE's exporter is `otlptracegrpc`-only; 6006 is HTTP/UI and must never be the documented target. The scheme-prefix failure mode (`otlptracegrpc.WithEndpoint` silently rejecting `http(s)://`-prefixed values — traces just go dark, no error) gets an explicit warning callout. The doc shows the full chain: `values.yaml` → manager + dashboard Deployment env → reporter Job env forwarding (all three consumers already wired in Phases 44–46).
- **D-10:** NOTES.txt gains a **tracing-dark nudge mirroring the existing `prometheus.enabled` warning block**: when `otel.exporter.endpoint` is empty, print a pointer to the Phoenix step in the docs. The edit MUST land in the `hack/helm/augment-tide-chart.sh` heredoc (step 4b) as well as the template — NOTES.txt's own header comment warns that template-only edits are reverted by the next augment run. A helm-template contract test gates the rendered output both ways (nudge present when endpoint empty, absent when set — Phase 38 TELEM-02 / Phase 46-02 render-gate precedent).

### Live-proof environment, run shape & evidence (PROOF-01)
- **D-11:** The proof runs on a **fresh kind cluster stood up by following the documented recipe itself** — doc validation and live proof in one pass (the recipe is proven by execution, not review). The existing `tide-dogfood` cluster's currency is unverified (it predates the v1alpha3 CRD crank; reuse only if verified current) — default is delete/recreate per the constrained-VM clean-run recipe, one heavy run at a time, with the Phoenix pod counted in the memory budget.
- **D-12:** The driving run is a **small real project with real API spend** (durable key at `~/.tide/anthropic.key`, outside the repo — verified present), sized to produce the complete five-level tree with Task-level message arrays at bounded cost — small-project shape (examples/projects), not dogfood scale. **Pre-flight cheap static checks before spending** (locked working preference): helm-template render with the recipe's values verifying the env wiring end-to-end, and a no-spend OTLP connectivity check against the installed Phoenix before the first real dispatch.
- **D-13:** Evidence lands in the **phase dir** (`.planning/phases/47-self-hosted-phoenix-install-end-to-end-proof/`) as milestone-close evidence: the five-level trace-tree screenshot, a Task-level LLM span detail showing redacted message arrays, a queryability demonstration (a Phoenix DSL filter over the OBS-03 `tag.tags`/metadata enrichment — e.g. "every span from Phase N"), and recorded trace IDs in a small evidence markdown. Browser-driven capture (the Phase 22/26 screenshot precedent). Set `phoenix.baseURL` during the proof so the dashboard's OBS-04 deep link is exercised live — free bonus evidence that the whole Phase 46 surface works against a real Phoenix.
- **D-14:** Known-limitations honesty at close: single-node kind cannot surface cross-pod clock skew (Pitfall 5 — child spans rendering outside the parent window on skewed multi-node clusters) — documented as a known limitation, not claimed verified (STATE.md already carries this blocker note). Any real defect the live proof surfaces (broken waterfall, missing spans, orphaned fragments, unredacted content) is named and root-fixed in-phase — never worked around to get the screenshot.

### Claude's Discretion
- Phoenix namespace name in examples (`phoenix` vs `observability` — pick one, use it consistently everywhere).
- Exact PVC size and retention-days numbers (D-06), grounded in fixture-scale volumes.
- INSTALL.md-vs-observability.md content split details within D-01's frame; section ordering and cross-link wording.
- Evidence markdown filename/shape; whether one hero screenshot also lands in `docs/` as operator-facing polish (optional, not scope).
- Mechanics of the no-spend OTLP connectivity check (D-12) — anything cheap that proves the pipe before dollars flow.
- NOTES.txt nudge wording and whether it keys on `otel.exporter.endpoint` alone or also mentions `phoenix.baseURL`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Milestone research (v1.0.8 Phoenix Rising — this phase is research "Phase 5"; ROADMAP flags `research: true` for the chart-pin re-check)
- `.planning/research/SUMMARY.md` — §"Phase 5: Self-Hosted Phoenix Install Surface (docs) + End-to-End Proof" (deliverables: non-default overrides list); key-risk cluster (Phoenix defaults, sampler coin-flip, "Looks Done But Isn't" — the proof is the gate that catches what unit tests can't); §confidence: chart pin flagged MEDIUM, re-verify fresh
- `.planning/research/STACK.md` — §"Self-hosted Arize Phoenix (Kubernetes)" table: chart coordinates (`oci://registry-1.docker.io/arizephoenix/phoenix-helm`), storage-mode exclusivity, 20Gi PVC default, OTLP ports 4317 gRPC / 6006 HTTP+UI (TIDE is gRPC-only), the near-daily-churn caveat
- `.planning/research/PITFALLS.md` — **Pitfall 8 (ephemeral SQLite + infinite retention + auth defaults — the D-06/D-08 basis)**; **Pitfall 9 (20Gi PVC + Postgres pod vs the 8 GiB dev VM — the D-06/D-11 basis)**; Pitfall 5 (cross-pod clock skew renders broken waterfalls — the D-14 known-limitation); Pitfall 6 (sampler coin flip — now inert at the Phase 46 1.0 default, but the proof sanity-checks it); Pitfalls 1/2 (what "redacted, size-bounded" means — the proof must show it held)

### Requirements and constraints
- `.planning/REQUIREMENTS.md` §"Self-Hosted Phoenix Surface (PHX)" + §"End-to-End Proof (PROOF)" — PHX-01/PHX-02/PROOF-01 exact text; §Out of Scope (no subchart, no collector, no bespoke trace viewer)
- `.planning/ROADMAP.md` §"Phase 47: Self-Hosted Phoenix Install + End-to-End Proof" — goal, research flag, 3 success criteria
- `.planning/STATE.md` §"Blockers/Concerns" — chart-pin freshness + the single-node clock-skew limitation to document at close; §"v1.0.8 binding constraints" — Phoenix never a subchart
- `.planning/PROJECT.md` §"Runtime-neutrality constraints (2026-07-15)" — why the trace contract matters beyond this milestone (the proof validates the seam a future LangGraph runtime parents into)

### Prior phase context (decisions this phase composes with)
- `.planning/phases/46-observability-enrichment-dashboard-deep-link/46-CONTEXT.md` — D-10/D-12 (`phoenix.baseURL` chain + `/redirects/*` URL shape ≥ 14.2.0 — the D-04(a) floor and the D-13 deep-link exercise); D-01 (sampler 1.0 default — quickstart needs no override anymore)
- `.planning/phases/44-llm-message-array-spans-d-o5-redaction-size-boundary/44-CONTEXT.md` — D-08/D-09 (head+tail elision, redact-before-truncate — what the Task-level LLM span evidence must visibly show); D-10 (exit-0 posture — a dark pipe is visible only in pod logs, which is exactly why NOTES.txt nudges)
- `.planning/phases/45-runtime-neutral-adapter-seam/45-CONTEXT.md` — the seam the proof implicitly validates (all current vendors synthesize; zero duplicate spans expected in the live tree)

### Existing code and docs (surfaces this phase touches)
- `docs/observability.md` — §Tracing (env-var table: bare `host:port` example already shipped for the collector case; the opt-down honesty text); §"Dashboard deep link to Phoenix (`phoenix.baseURL`)" (the "install instructions land in Phase 47" placeholder + the ≥ 14.2.0 floor commitment this phase discharges)
- `docs/INSTALL.md` — §"Enable telemetry (Prometheus)" (line ~175: the exact structural precedent for "Enable tracing (Phoenix)"); §Verifying the install (the post-step verification idiom); §Uninstall
- `charts/tide/templates/NOTES.txt` — the existing `prometheus.enabled` warning block (D-10's mirror) + the header comment: **owned by `hack/helm/augment-tide-chart.sh` step 4b — edit the heredoc there, not only this file**
- `hack/helm/augment-tide-chart.sh` — step 4b NOTES.txt heredoc (the authoritative edit site)
- `charts/tide/values.yaml` — `otel.*` block (endpoint/sampler/serviceName) + `phoenix.baseURL`; comment blocks may gain a Phoenix pointer
- `internal/otelinit/provider.go` — the `otlptracegrpc` construction whose bare-`host:port` requirement PHX-02 documents
- `test/integration/kind/` — helm-template contract-test conventions (the D-10 render gate lands beside the Phase 46 sampler/phoenix.baseURL gates)
- `examples/projects/` — small-project fixture shapes for the D-12 driving run
- `docs/live-e2e.md` — existing live-run recipe conventions the proof may reuse (verify currency before leaning on it)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- The INSTALL.md "Enable telemetry (Prometheus)" section is the exact structural template for the Phoenix step: same "install the external thing → set TIDE values → verify" three-beat, same NOTES.txt-warning tie-in.
- The NOTES.txt prometheus warning block + its helm-template contract tests — the D-10 nudge is the same mechanism with a different condition (`otel.exporter.endpoint` empty).
- Everything the proof needs on the TIDE side already ships: sampler 1.0 default (46), session.id/metadata/tags on every span (46), deep link behind `phoenix.baseURL` (46), redacted size-bounded message arrays (44), five-level AGENT tree + traceparent propagation (42/43), zero-duplicate seam (45). This phase writes docs and drives a run; it should need little-to-no Go.
- `~/.tide/anthropic.key` (durable, outside repo, survives cluster teardowns) + the constrained-VM clean-run recipe (delete → recreate → prewarm, one heavy run at a time) — the proof-run operational playbook.
- Browser-driven screenshot capture precedent (Phase 22 telemetry-render proof, Phase 26 dashboard DAG screenshots captured by the orchestrator itself).

### Established Patterns
- **Documented-install posture (TELEM-01):** TIDE ships the wiring value and the docs, never the external system — Prometheus precedent applied verbatim to Phoenix.
- **NOTES.txt is generated:** the augment script's heredoc is the source of truth; template-only edits revert (Pitfall 3 in the file's own header).
- **Docs voice:** tight, declarative, em-dash-heavy; "alternatives considered and rejected" framing when a design choice is argued; honest-limitation callouts (the opt-down sampler text is the house example).
- **No-flake doctrine:** the proof is a live run, not a test, but the same rule applies — a failed proof run is root-caused, not retried until the screenshot looks right.
- **Verify before claiming:** the recipe is proven by following it on a fresh cluster; the proof evidence states what was observed (trace IDs, query used), not what should work.

### Integration Points
- `docs/INSTALL.md` §Enable-telemetry neighborhood ← new "Enable tracing (Phoenix)" section.
- `docs/observability.md` §Tracing ← full recipe; §deep-link section's placeholder ← cross-reference.
- `charts/tide/templates/NOTES.txt` + `hack/helm/augment-tide-chart.sh` step 4b ← the D-10 nudge (both sites, same content).
- `test/integration/kind/` helm-template contract tests ← NOTES.txt render gate.
- The proof cluster: kind + TIDE (own chart) + Phoenix (official chart, own namespace) + one small real project run → Phoenix UI → evidence into the phase dir.

</code_context>

<specifics>
## Specific Ideas

No user-specific vision requests — this phase was auto-discussed (`--auto`); all six gray areas resolved to the research-recommended option or the locked prior posture. Three framing notes for the planner:

- **This phase emits nothing new.** Phases 42–46 shipped the entire emission surface. If the live proof reveals a gap, that's a defect to name and root-fix (D-14), not scope to quietly add — the phase's own deliverables are docs, chart NOTES.txt, the install recipe, and the evidence.
- **The proof doubles as doc validation.** D-11's fresh-cluster-from-the-recipe rule means a wrong doc step fails the proof — that coupling is deliberate and is the strongest verification this milestone has (research: the entire "Looks Done But Isn't" checklist only surfaces when a real trace tree is inspected).
- **The chart-pin re-fetch is not bureaucracy.** Three separate authored artifacts (STACK.md, REQUIREMENTS.md, observability.md) each recorded chart facts weeks apart against a near-daily-shipping chart; D-04's three re-verification points (version floor, auth default, storage exclusivity) are where stale facts would corrupt the doc.

</specifics>

<deferred>
## Deferred Ideas

- **Postgres path live-proof** — documented as the chart's tested happy path but not live-proven on the 8 GiB dev VM (D-07's honesty rule); a real-cluster validation belongs to whoever first runs TIDE+Phoenix on a non-dev cluster.
- **Multi-node clock-skew validation (Pitfall 5)** — unexercisable on single-node kind; documented known limitation at close, revisit when a multi-node environment exists.
- **Tail-sampling collector recipe, Grafana dashboards, alert rules** — already queued in observability.md's "What's coming"; out of milestone scope.
- **Data-minimization toggle** (`otel.redactMessageContent` / ArtifactPath-only mode) — carried from Phase 46's deferred list; fast-follow candidate, not v1.0.8.

### Reviewed Todos (not folded)
Same four keyword matches as Phases 42–46, carried forward with the dispositions locked there (reviewed 2026-07-15/16/17; reasoning applies identically — no Phoenix-install/proof overlap). The `--auto` fold-at-score≥0.4 rule was deliberately overridden: all four carry locked not-folded dispositions from five consecutive phases, and folding them into a docs+proof phase would violate the fixed phase boundary.
- `2026-07-03-signed-commits-verified-badge.md` — git-identity/GPG scope (Future Requirements), keyword false-positive.
- `2026-07-12-project-dispatch-missing-failurehalt-gate.md` — W-2 dispatch-gate ordering concern, next-milestone candidate.
- `2026-07-12-task-dispatch-gate-order-divergence.md` — W-2 sibling, same disposition.
- `cache-f1-direct-sdk-cross-pod-caching.md` — deferred vNext+, no overlap.

</deferred>

---

*Phase: 47-Self-Hosted Phoenix Install + End-to-End Proof*
*Context gathered: 2026-07-17*
