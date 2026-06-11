# Roadmap: TIDE — Topologically-Indexed Dependency Execution

## Milestones

- ✅ **v1.0.0 — Self-Hosting MVP** (SHIPPED 2026-06-11) — Phases 1–11, 137 plans, 965 commits. Six CRDs + layered-Kahn waves + pluggable subagent dispatch + gates/observability/dashboard/CLI + Helm distribution; release published (binaries, 7 images, 2 OCI charts). Full archive: [milestones/v1.0.0-ROADMAP.md](milestones/v1.0.0-ROADMAP.md) · [milestones/v1.0.0-REQUIREMENTS.md](milestones/v1.0.0-REQUIREMENTS.md)

## Next milestone

Not yet defined — run `/gsd:new-milestone` to start the questioning → research →
requirements → roadmap cycle. The v1.x headline goal carried out of v1.0.0:
**full TIDE-on-TIDE** — a TIDE install drives this repo's own next milestone
end-to-end (the v1.0.0 live DoD proved the mechanism on a fixture repo; the
self-hosted run on this repo is the next bar). Candidate backlog: the 27-item
hardening backlog in docs/audit/ (metrics endpoint auth, digest-pinned base
images, observedGeneration on CRD statuses), SLSA provenance + cosign, krew
index submission.
