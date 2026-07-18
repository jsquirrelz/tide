# Phase 47 — PROOF-01 Milestone-Close Evidence

**Captured:** 2026-07-17 (browser-driven, Phase 22/26 self-capture precedent — orchestrator session drove Chrome against the live proof cluster)
**Status:** Complete five-level trace tree visible and queryable in self-hosted Phoenix. Two named defects recorded below (D-14) — neither blocks the trace-tree/queryability bar; both routed to gap closure.

## 1. Environment (observed, not assumed)

- **Cluster:** `tide-phoenix-proof` (kind, single node) — stood up 2026-07-17 by following `docs/INSTALL.md` §"Enable tracing (Phoenix)" + `docs/observability.md` §"Self-hosted Phoenix" literally (Plan 47-04; full command log in `47-PROOF-RUNLOG.md`). Stale `tide-dogfood` deleted first (one-heavy-workload VM discipline).
- **Phoenix:** chart `phoenix-helm-10.0.1`, appVersion `18.1.0` (footer of every captured frame shows `arize-phoenix v18.1.0`), namespace `phoenix`, Service `tide-phoenix-svc` (ClusterIP; 4317 gRPC / 6006 UI).
- **Storage path exercised:** SQLite-on-PVC (`persistence.enabled=true`, `tide-phoenix-data-pvc` Bound, **2Gi**, RWO), retention 7 days. Postgres path NOT exercised (see Known limitations).
- **Auth:** ON throughout. Admin password rotated from the chart's weak default via the documented flow; OTLP ingest authenticated via a minted System API key riding `tide-otlp-headers` Secret → `OTEL_EXPORTER_OTLP_HEADERS` (`valueFrom: secretKeyRef` on manager + dashboard; forwarded onto reporter Jobs). Secret names only — no credential values in any committed artifact.
- **TIDE:** locally-built HEAD images + local `./charts/tide` (Wave-1/2 OTLP-headers wiring included). See Known limitations for provenance honesty.
- **Capture access:** a dedicated `evidence-capture` ADMIN user was minted via Phoenix's API (the documented UI flow's non-interactive equivalent) for the authenticated browser session; its password lived only in session-scoped temp storage outside the repo.

## 2. The run

- **Project:** `medium-project`, namespace `tide-sample-medium` (`examples/projects/medium`), model `claude-haiku-4-5`.
- **Outcome:** `status.phase=Complete` (confirmed twice by Plan 47-04; see Defect #1 for the status-flap caveat).
- **Cost:** **$0.88** observed (cap $5). Phoenix's independent session rollup shows **$0.43** / 608,363 cumulative tokens for the trace (different accounting scope than TIDE's 150,498 `Project.status` tally — Phoenix sums per-call LLM spans incl. cache reads).
- **Trace latency (Phoenix):** 13m 29s. Reporter Jobs observed across all five levels plus per-Task trace-only Jobs (project-level trace-only dispatched ×2 — see Defect #2).

## 3. Trace evidence

- **Trace ID:** `e9124906f6ee4aeba650a6fdd93b86fd` (deterministic from Project UID; also the `session.id` value `e9124906-f6ee-4aeb-a650-a6fdd93b86fd` on enriched spans).
- **Span inventory (authenticated REST, all 4 pages):** 392 unique spans = 6 AGENT + 386 LLM. The 6 AGENT spans form the exact five-level chain, correctly parented:
  `tide.dispatch.project 2d868bf76ebc5c33` → `tide.dispatch.milestone 6a81317ebba1562f` → `tide.dispatch.phase 8c3317c068c4747e` → `tide.dispatch.plan 4d7f2bda9aee8d57` → `tide.dispatch.task 2d006e81d97ad124` + `tide.dispatch.task dba4acb16f774bf7`.
- **Queryability (exact query used):** `metadata['level'] == 'phase'` in the project Spans filter (mode: All) — returned the phase-level span population live (screenshot c). Sampler sanity: no missing-by-sampling levels (1.0 default; tree complete).
- **Deep link exercised:** dashboard plan-node detail panel rendered `View trace in Phoenix` → `http://localhost:6006/redirects/spans/4d7f2bda9aee8d57`; following it landed on trace `e9124906…` with exactly that plan AGENT span selected (screenshot d). Phoenix ≥ 14.2.0 redirect-route floor confirmed working on 18.1.0.

### Screenshot index

| File | Shows |
|------|-------|
| `47-evidence-trace-tree.png` | Composite of two live frames of the same trace: (1) tree filtered to the dispatch chain — all five AGENT levels with hierarchy indentation, trace ID/cost/latency in header; (2) unfiltered tree at the first Task span — LLM message spans nested beneath `tide.dispatch.task` with per-call token counts |
| `47-evidence-llm-span-redacted.png` | Task-level LLM span `5d0abe61323621b0` (child of task AGENT) detail: multi-role message array (tool-call → assistant → tool-call → user) rendered with real content — see §4 for why no redaction markers appear |
| `47-evidence-query.png` | Spans tab with the DSL filter `metadata['level'] == 'phase'` applied (All mode), matching span rows + project stats in-frame |
| `47-evidence-deeplink.png` | Composite of one click-through: dashboard node panel with the Phoenix link, and the Phoenix landing page resolved to the exact linked span |

Composite disclosure: the tree and deeplink PNGs are labeled composites of two live frames each (captured seconds apart in the same authenticated session); the LLM-span and query PNGs are single frames. No pixel content was altered.

## 4. Redaction verification

- **Key-material search over ALL span content (392 spans, 4 REST pages):** 0 hits for the real key's 8-char prefix, 0 for the 12-char prefix, 0 for the generic Anthropic key-prefix pattern (the one `internal/harness/redact/patterns.go` lists first). The real API key never reached Phoenix.
- **Why no `[REDACTED]`/truncation markers appear in the LLM span screenshot:** the redaction pass (`redact.String` before truncation — the Phase 44 D-09 chokepoint) runs unconditionally on every message, but this run's content contained **no secret-pattern matches to mask**, and the largest single message attribute was **21,573 bytes** — under the 32 KiB per-message elision cap — so head+tail elision never fired either. Content passed through intact, which is the boundary's correct output for secret-free, under-cap content. The pass-through is proven safe by the 0-hit search above, not by marker presence.
- The Anthropic key-prefix grep gate on this file returns 0 — prefix strings are described above, never written.

## 5. Known limitations (D-14 honesty)

- **Cross-pod clock skew (research Pitfall 5) is NOT verified** — single-node kind has one clock; child-span-outside-parent-window rendering defects cannot surface here. Documented limitation, revisit on a multi-node cluster.
- **The bundled-Postgres storage path is documented but NOT live-proven** — the recipe documents it as the chart's tested durable default (D-07); this proof exercised SQLite-on-PVC only.
- **Local-HEAD provenance:** the proof ran on locally-built images and the local chart source. The published OCI chart/images predate this phase's OTLP-headers wiring and gain it at the next release — an operator on published `1.0.7` artifacts cannot yet reproduce the auth-ON path.

## 6. Defects found (named, with disposition)

1. **Boundary-push `--force-with-lease` retry never refreshes `Status.Git.LastPushedSHA` from the remote tip** (`internal/controller/project_controller.go`) — found by Plan 47-04. When a wave-level push lands between boundary-push attempts, the retry re-fails deterministically; `medium-project.status.phase` observably flaps (the dashboard capture frame shows the project node "Running" with all children Succeeded, and its artifacts panel empty because the run branch never received the boundary push). No data loss — authored content verified intact. **Disposition:** documented in `47-04-SUMMARY.md` deviations + `47-PROOF-RUNLOG.md`; routed to a follow-up debug session (run-integrity class, pre-existing code, not introduced by this phase).
2. **Partial enrichment coverage on live LLM message spans** — found during this capture. Only **115/386** LLM spans carry `session.id` + `metadata.*` + `tag.tags` (and token counts); **271/386** carry only `llm.provider` + `llm.model_name` beside full message arrays (6 of 271 have token counts). Coverage splits ~⅓ enriched per level across all five levels; zero conversation-fingerprint overlap between the populations (not simple duplicates); the runlog independently records the project-level trace-only reporter dispatching **×2**. OBS-02's letter ("every span carries session.id") does not hold on this live tree — a class of defect only a live proof could catch (envtest fixtures pass). Suspected shape: multiple reporter emission passes per level with divergent `ReporterOptions` (possibly amplified by Defect #1's retry storm keeping reconciles hot). **Disposition:** named here per D-14, full diagnostic data preserved; root-cause + fix via this phase's gap-closure loop (reporter Job args/logs on the still-running cluster are the next evidence source).
3. **`examples/projects/medium/per-namespace-resources.yaml` PVC ships RWX, deadlocking WaitForFirstConsumer on kind** (the file's own comment says RWO is needed on kind) — worked around by Plan 47-04 with the documented delete/recreate-as-RWO recipe (mirrors `hack/scripts/acceptance-v1.sh`). **Disposition:** known example-fixture gap, recorded in the runlog; fixture fix is a small follow-up.

---
*Cluster left running (`tide-phoenix-proof` + Phoenix) for human review and Defect #2's root-cause investigation; delete when done per the one-heavy-workload discipline.*
