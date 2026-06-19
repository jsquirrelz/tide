# Phase 29: Operator Tooling + E2E - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-19
**Phase:** 29-operator-tooling-e2e
**Areas discussed:** Export mechanism & bundle contents, Import flow & PVC-load, Dry-run report shape, E2E test scope (TOOL-02)

---

## Export mechanism & bundle contents

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse inspector-pod pattern | Short-lived pod mounts PVC, tars envelopes, streams to stdout (same as artifact-get) | ✓ |
| kubectl cp from a live pod | Copy from an already-running pod; fragile | |

| Option | Description | Selected |
|--------|-------------|----------|
| Match salvage fixture, tgz default | CR YAMLs + seed manifest + envelopes + SEED-OUTLINE; .tgz default with --dir | ✓ |
| Directory only | Unpacked tree, no tarball | |
| tgz only | Always tarball, no directory | |

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, generate sha256 | Per-envelope sha256 in seed manifest; closes Phase 28 deferred gap | ✓ |
| No, defer again | Rely on len==ChildCount completeness only | |

**User's choice:** Inspector-pod read; fixture-shaped bundle, tgz default; sha256 checksums.
**Notes:** Export emits the seed manifest carrying CR specs + name→oldUID map + checksums.

---

## Import flow & PVC-load

| Option | Description | Selected |
|--------|-------------|----------|
| Stage-only, separate apply | import-envelopes stages PVC + seed ConfigMap + surfaces project.yaml; operator runs `tide apply` | ✓ |
| One command, applies Project too | import-envelopes also applies the Project end-to-end | |

| Option | Description | Selected |
|--------|-------------|----------|
| Loader pod (reverse of export) | Pod mounts PVC, CLI streams tgz in, unpacks to pvcSubPath; tide-import rekeys | ✓ |
| Extend tide-import to fetch | Make the Job pull from a source; reopens Phase 28 contract | |

| Option | Description | Selected |
|--------|-------------|----------|
| Export emits it; import applies verbatim | Export captures live UIDs atomically into seed manifest | ✓ |
| Import assembles from bundle YAMLs | Build seed ConfigMap at import time | |

**User's choice:** Stage-only + separate `tide apply`; loader pod; export emits seed.
**Notes:** On the apply question the user first asked what `tide apply` does and whether apply should be independent; after explanation (server-side-apply wrapper; one blessed Project-creation path; natural dry-run→review→apply flow) they chose stage-only.

---

## Dry-run report shape

| Option | Description | Selected |
|--------|-------------|----------|
| Locally on the unpacked bundle | Reuse Phase 28 validation client-side; no cluster writes/pods | ✓ |
| In-pod dry-run | Spawn tide-import --dry-run in-cluster | |

| Option | Description | Selected |
|--------|-------------|----------|
| Per-level table + summary, --output json | level/name/verdict/reason table + summary; JSON option | ✓ |
| Summary counts only | Totals only | |
| JSON only | Machine-readable only | |

| Option | Description | Selected |
|--------|-------------|----------|
| Hard-reject whole import, show edges | Cycle ⇒ entire import rejected (mirrors Phase 28 D-10) | ✓ |
| Mark only involved levels re-plan | Adopt the rest; diverges from atomic-reject | |

**User's choice:** Local validation; per-level table + JSON; cycle = hard-reject.

---

## E2E test scope (TOOL-02)

| Option | Description | Selected |
|--------|-------------|----------|
| Drive via the CLI | Test runs real export/import commands; exercises TOOL-01 + TOOL-02 together | ✓ |
| Apply seed + Project directly | Bypass CLI; controller path only | |

| Option | Description | Selected |
|--------|-------------|----------|
| Small fixture to Succeeded + full adoption | Small fixture drains to all-Succeeded; full salvage asserts adoption + zero-planner + $0 | ✓ |
| Full salvage fixture to Succeeded | Real 42-plan fixture to all-Succeeded behind long-test flag; slowest/flakiest | |
| Adoption + exec-starts only (amend ROADMAP) | Cheapest; would require editing ROADMAP criterion #4 | |

**User's choice:** Drive via CLI; two-tier bar (small fixture to Succeeded + full salvage adoption).
**Notes:** User asked for the reasoning before deciding. Walkthrough: the unique-to-import signal (adopt → skip planners → exec starts → $0 re-paid) is observable without full drain; full drain mostly re-tests the executor (Phases 24–26 already cover it) and is the flakiest shape on a single ~7.65 GiB kind node; but a pure "exec proceeds" bar could mask a cross-milestone-drain defect on an imported graph AND would force amending ROADMAP criterion #4. The two-tier design resolves all three: cheap full-drain proof on small data + real-fixture adoption proof, honoring criterion #4.

## Claude's Discretion

- Exact cobra flag names, seed-manifest on-disk schema, inspector/loader pod RBAC verbs, the small E2E fixture shape.
- Whether export/import share a `pkg/`-level bundle reader/writer vs. per-command code.

## Deferred Ideas

- Automatic export-on-halt (convenience layer atop TOOL-01).
- Hybrid by-name write-side envelope paths (new capability; own backlog item).
- Partial/incremental re-planning (out of scope; full-tree adoption only).
- `cache-f1-direct-sdk-cross-pod-caching` todo — reviewed, not folded (orthogonal, deferred).
