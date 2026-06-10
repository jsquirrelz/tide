# TIDE Documentation

**Audience:** Operators installing, authoring, and operating TIDE in their own clusters.

**Status:** v1.0 docs surface. No mkdocs site for v1; this index is the front door.

**Scope of this doc:**

- Reader-journey index for the v1 documentation surface under `docs/`.
- 12 numbered entries, 13 doc files total (entry #4 is co-located: dashboard + CLI as a single operator-UI pair).
- Recommended on-ramp at the bottom for first-time readers.

## Index

1. [install](INSTALL.md) — prerequisites + Helm install + first sample
2. [concepts](concepts.md) — operator mental model of the five-level paradigm (read this before authoring your first Project)
3. [project authoring](project-authoring.md) — `Project.Spec` reference + walkthrough of the small / medium / large samples
4. [dashboard](dashboard.md) + [tide CLI](cli.md) — operator UIs; the dashboard is the visual surface, `tide` is the headless equivalent
5. [gates](gates.md) — per-level gate policy + approval handshake
6. [observability](observability.md) — OpenTelemetry + Prometheus wiring
7. [git hosts](git-hosts.md) — GitHub / GitLab / Gitea PAT setup
8. [storage drivers](rwx-drivers.md) — RWX PVC driver matrix
9. [live E2E](live-e2e.md) — nightly Claude-backed cron + cost-bounded fixture
10. [troubleshooting](troubleshooting.md) — symptom / cause / recipe table for install + steady-state failures
11. [RBAC reference](rbac.md) — per-Kind verbs + per-namespace RoleBinding template
12. [production checklist](production.md) — **read before a real-Claude run against a repo you care about**: repo-safety contract, budget safety, sizing, gates, v1.0 limitations

## Where to start

New here? Walk these three docs in order — install, mental model, first Project — and you have the full v1 on-ramp:

- [INSTALL.md](INSTALL.md) — prerequisites + Helm install + first sample
- [concepts.md](concepts.md) — the five-level paradigm in operator language
- [project-authoring.md](project-authoring.md) — `Project.Spec` field reference + sample walkthrough

Going to run TIDE against a real repo with real money? Read [production.md](production.md) first — it's the safety + budget + sizing checklist.
