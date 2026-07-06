# Phase 40: Deprecate v1alpha1 API - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-06
**Phase:** 40-deprecate-v1alpha1-api
**Areas discussed:** Scope shape (fold + v1alpha3 horizon), Upgrade-path stance, Guard & dual-accept fate, Docs & samples sweep, Envelope contract decoupling, v1alpha3 content batching

---

## Todo Cross-Reference

| Option | Description | Selected |
|--------|-------------|----------|
| Fold none (Recommended) | No pending todo belongs in this phase; list subagent.levels/v1alpha3 under deferred | |
| Fold subagent.levels rename | Pull the v1alpha3-requiring rename into Phase 40 | ✓ |

**User's choice:** Fold subagent.levels rename
**Notes:** Deliberately supersedes the 2026-07-03 "own milestone" routing — see Scope shape below for the reconciliation.

---

## Scope shape (fold + v1alpha3 horizon)

| Option | Description | Selected |
|--------|-------------|----------|
| Full lifecycle turn (Recommended) | Remove v1alpha1 + introduce v1alpha3 with the levels rename + mark v1alpha2 deprecated; one coherent version crank | ✓ (amended) |
| Remove + prep only | Remove v1alpha1, build reusable lifecycle machinery; v1alpha3 stays a fast-follow | |
| Rename without v1alpha3 | Re-interpret levels keys in place behind SchemaRevision, no new API version | |

**User's choice:** Full lifecycle turn, amended: "I'm the only user using this currently. I lean towards full lifecycle turn, but we should remove v1alpha2 as well once v1alpha3 is ready."
**Notes:** Single-user reality removes the need for any deprecation grace period. End state: v1alpha3 sole served+storage version; v1alpha1 and v1alpha2 both removed within the phase.

---

## Upgrade-path stance (storedVersions)

| Option | Description | Selected |
|--------|-------------|----------|
| Reinstall-only (Recommended) | Consistent with Phase 23 D-09; delete CRDs + re-apply under v1alpha3; no storedVersions machinery | ✓ |
| Document prune step | kubectl storedVersions status-patch recipe for in-place upgraders | |
| Preflight automation | Chart hook / manager check detecting stale storedVersions | |

**User's choice:** Reinstall-only
**Notes:** kind-tide-dogfood (pre-Spring-Tide v1alpha1) gets rebuilt, not upgraded.

---

## Guard & dual-accept fate

| Option | Description | Selected |
|--------|-------------|----------|
| Generalize guard, drop dual-accepts (Recommended) | Guard expects schemaRevision: v1alpha3, version-parameterized; owner-ref checks simplify to current GroupVersion | ✓ |
| Keep all arms | Guard + dual-accepts retain v1alpha1/v1alpha2 acceptance — dead code under reinstall-only | |
| Drop the guard too | Rely on CRD validation alone — loses fail-closed protection | |

**User's choice:** Generalize guard, drop dual-accepts

---

## Docs & samples sweep

| Option | Description | Selected |
|--------|-------------|----------|
| Accuracy pass, envelope untouched (Recommended) | Full prose pass; migration guide gains v1alpha2→v1alpha3 chapter; envelope keeps its string | ✓ (sweep depth) |
| Mechanical bump only | Swap apiVersion strings and filenames, skip prose accuracy | |
| Rename envelope too | Decouple envelope naming in the same phase | (superseded by dedicated envelope question) |

**User's choice:** "The sweep should go deep. Should the envelope's contract be decoupled? How do other projects manage this?"
**Notes:** The envelope question was split out and answered with prior art (kubeadm subdomain groups, CRI/CSI independent contract versioning, CloudEvents specversion, Argo's unversioned-contract cautionary tale) — see next section.

---

## Envelope contract decoupling

| Option | Description | Selected |
|--------|-------------|----------|
| Decouple: dispatch subgroup (Recommended) | dispatch.tideproject.k8s/v1alpha1 (kubeadm pattern); version stays v1alpha1, pure decoupling | ✓ |
| Decouple + declare v1 | dispatch.tideproject.k8s/v1 — declares contract stable while Level field is being touched | |
| Keep coupled | Shared string + warning comment; pre-planned envelope v1beta1 bump collides later | |

**User's choice:** Decouple: dispatch subgroup
**Notes:** Supersedes pkg/dispatch/doc.go's documented plan to bump the envelope to tideproject.k8s/v1beta1.

---

## v1alpha3 content batching

| Option | Description | Selected |
|--------|-------------|----------|
| Rename only (Recommended) | v1alpha3 = v1alpha2 + levels rename + schemaRevision bump | |
| Research audits for batchable changes | Researcher inventories v1alpha2 schema warts; user picks at plan time | ✓ |

**User's choice:** Research audits for batchable changes
**Notes:** ASK-FIRST checkpoint at plan approval — present the inventory, don't silently batch.

## Claude's Discretion

- Envelope `Level` string values (within the todo's decided mapping)
- Plan sequencing/decomposition (v1alpha3-first then removals)
- REQUIREMENTS.md requirement IDs for this phase
- Whether api/v1alpha3 starts as a v1alpha2 copy or fresh scaffold

## Deferred Ideas

- Envelope stability declaration (`dispatch.tideproject.k8s/v1`) — revisit after the post-rename contract soaks
- Reviewed-but-not-folded todos: dashboard drawer/artifact view (Phase 37), git baseRef (Phase 35), pricing table + Prometheus step (Phase 38), GPG Verified badge (descoped to Future Requirements)
