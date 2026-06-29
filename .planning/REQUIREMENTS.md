# Requirements: TIDE — v1.0.7 Flood Tide (TIDE-on-TIDE Self-Hosting Proof)

**Defined:** 2026-06-29
**Core Value:** The five-level paradigm (Milestone → Phase → Plan → Task → Wave) runs as a real K8s orchestrator that can drive its own next milestone end-to-end.

**Milestone goal:** Drive a *completing* dogfood run #2 end-to-end — TIDE orchestrating Claude subagents to build the OpenAI backend — on a slightly-bigger single-node cluster under a hard $100 cap, proving the paradigm self-hosts; then review TIDE's authored output to feed a follow-up "extend TIDE" pass.

**Division of labor:** The human operates TIDE (infra, deploy, launch, babysit, root-fix orchestrator defects). **TIDE builds the entire OpenAI backend** — no hand-written backend code this milestone. The backend is TIDE's *output*, reviewed not merged.

## v1.0.7 Requirements

### Pre-flight Tech-Debt Hardening

Load-bearing v1.0.7 audit carry-ins that protect run #2; fixed before launch.

- [ ] **PREFLIGHT-01**: The chart configmap default for `plannerConcurrency` is corrected from `16` to `4`, so a fresh default deploy can dispatch at most 4 concurrent planners (no latent 16-wide over-dispatch on the single node). Verified by rendering the chart and asserting the configmap value plus a controller-level assertion of the effective cap.
- [ ] **PREFLIGHT-02**: The project-level rollup marker (`PlannerRolledUpUID` / equivalent) is hardened to the milestone/phase exactly-once pattern, so planning-cost rollup at the project level is exactly-once under reporter-Job TTL-GC and the $100 cap accounting stays accurate. Verified by an envtest proving no double-count across TTL-GC at the project level.

### Infra & Fresh Deploy

- [ ] **INFRA-01**: A fresh single-node kind cluster sized *slightly* above the current ~8GiB host (well under 16GB) is stood up for run #2, with the node memory ceiling documented.
- [ ] **INFRA-02**: Current-version v1.0.7 TIDE — images + charts carrying the PREFLIGHT fixes — is built and deployed to the cluster (not stale or local pre-Spring-Tide artifacts), verified by `helm`/`kubectl` showing v1.0.7 components running.
- [ ] **INFRA-03**: The credproxy + real Anthropic key (from `~/.tide/anthropic.key`) are wired so subagents dispatch against the real API, verified by one successful real subagent dispatch (smoke).

### Salvaged-Tree Import & Dry-Run

- [ ] **IMPORT-01**: The salvaged tree (`salvage-20260618`, 3 Milestones / 15 Phases) is imported/adopted onto the fresh cluster via `tide import-envelopes`, validated (cycle-detection + completeness) before any dispatch.
- [ ] **IMPORT-02**: A cost-projection / dry-run is produced before launch so expected spend against the cap is known, and `absoluteCapCents=10000` ($100) is set on the Project.
- [ ] **IMPORT-03**: Effective dispatch concurrency (planner/executor pools, separately sized) is tuned to fit the node's memory ceiling so the cascade cannot OOM the single node; the chosen values are documented.

### Launch & Operate to Completion

- [ ] **RUN-01**: Run #2 is launched and driven to `Project=Complete` on the single-node cluster without OOM.
- [ ] **RUN-02**: Total spend stays within the $100 cap; if the cap halts the run, the human is asked before any cap raise / resume (no blind spend).
- [ ] **RUN-03**: A mid-execution dashboard screenshot is captured and delivered to the user.
- [ ] **RUN-04**: Orchestrator defects that surface during the run are root-fixed in-repo (the v1.0.6 D1–D4 pattern), each with a symptom-reproducing test, and the run relaunched/resumed — not worked around.

### Output Review & Extraction

- [ ] **REVIEW-01**: TIDE's authored OpenAI backend code is collected from the run output and code-reviewed for quality/correctness (expected *not* mergeable as-is).
- [ ] **REVIEW-02**: Learnings from the run (what the paradigm got right/wrong, surfaced defects, cost/perf) are extracted into a retrospective artifact.
- [ ] **REVIEW-03**: Cherry-pick candidates (what to keep vs rework) from TIDE's backend output are identified and recorded to seed a follow-up "extend TIDE" milestone.

### Release

- [ ] **RELEASE-01**: v1.0.7 is published (images + charts + binaries) carrying the PREFLIGHT tech-debt fixes, verified anonymously on ghcr.

## Future Requirements

Deferred beyond v1.0.7. Tracked but not in this roadmap.

### Extend TIDE (follow-up milestone)

- **EXTEND-01**: Rework TIDE's authored OpenAI backend into a mergeable, provider-agnostic `Subagent` implementation behind the interface.
- **EXTEND-02**: Live functional parity — drive a real subagent dispatch through the OpenAI backend end-to-end (the CACHE-F1 direct-SDK path that realizes cross-pod prompt caching).

### Deferred tech-debt

- **DEBT-01**: Controller-envtest-suite tier split (the cosmetic v1.0.7 audit carry-in not folded into this milestone).

## Out of Scope

| Feature | Reason |
|---------|--------|
| Hand-writing the OpenAI backend | The entire point is TIDE-on-TIDE — TIDE builds it; the human only operates TIDE. |
| Merging TIDE's backend output as-is | Expected not mergeable; merge/rework is the follow-up "extend TIDE" milestone. |
| Multi-node / cloud / ≥16GB infra | Explicitly rejected — fit a *slightly* bigger single node via the D3 concurrency cap, not RAM. |
| Live OpenAI functional verification | The OpenAI backend is reviewed not run this milestone; live parity is EXTEND-02. |
| Controller-envtest tier split | Cosmetic v1.0.7 audit carry-in, deferred (DEBT-01) — not load-bearing for run #2. |
| New CRD schema / persistence changes | Persistence stays CRD-`.status`-only; resumption stays minimal/re-derivable. |

## Traceability

Populated during roadmap creation. Each requirement maps to exactly one phase.

| Requirement | Phase | Status |
|-------------|-------|--------|
| PREFLIGHT-01 | TBD | Pending |
| PREFLIGHT-02 | TBD | Pending |
| INFRA-01 | TBD | Pending |
| INFRA-02 | TBD | Pending |
| INFRA-03 | TBD | Pending |
| IMPORT-01 | TBD | Pending |
| IMPORT-02 | TBD | Pending |
| IMPORT-03 | TBD | Pending |
| RUN-01 | TBD | Pending |
| RUN-02 | TBD | Pending |
| RUN-03 | TBD | Pending |
| RUN-04 | TBD | Pending |
| REVIEW-01 | TBD | Pending |
| REVIEW-02 | TBD | Pending |
| REVIEW-03 | TBD | Pending |
| RELEASE-01 | TBD | Pending |

**Coverage:**
- v1.0.7 requirements: 16 total
- Mapped to phases: 0 (roadmap pending)
- Unmapped: 16 ⚠️ (filled by roadmapper)

---
*Requirements defined: 2026-06-29*
*Last updated: 2026-06-29 after initial definition (milestone v1.0.7 Flood Tide)*
