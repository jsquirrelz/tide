---
phase: 35-git-base-ref
verified: 2026-07-07T05:01:54Z
status: passed
gate_decision: APPROVED
score: 4/4 success criteria verified
behavior_unverified: 0
overrides_applied: 0
re_verification:
  previous_status: none
  note: initial verification
human_verification:
  - test: "Run the kind Layer B e2e (medium_http_test.go Spec 4) on a live kind cluster + Docker"
    expected: "baseRef Project reaches CloneComplete with status.git.baseSHA == base-ref-target branch tip (and != default tip)"
    why_human: "Requires a live kind cluster + Docker; not run in this verification. RECOMMENDED pre-merge belt-and-suspenders only — the underlying behavior is already proven at the unit (pkg/git), tide-push envelope, and envtest (controller) layers, so this is redundant cluster coverage, not the sole proof of any success criterion."
---

# Phase 35: Git Base Ref Verification Report

**Phase Goal:** Operators can base a run on any branch, tag, or SHA — unresolvable refs fail fast with a typed condition instead of a cryptic worktree-add failure, and the resolved base SHA is stamped in status across both API versions.
**Verified:** 2026-07-07T05:01:54Z
**Status:** passed — **gate_decision: APPROVED**
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths (ROADMAP Success Criteria)

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | baseRef=branch/tag/SHA produces a run branched from that ref; absent = default HEAD, no default marker | ✓ VERIFIED | `pkg/git/branch.go:122-180` resolveBaseRef chain; `TestEnsureRunBranch_NonDefaultBranch/AnnotatedTagPeeled/LightweightTag/FullSHA` resolve to distinct tips in a production-shaped clone (all PASS); `EnsureRunBranch` baseRef=="" → `repo.Head()` (branch.go:85-90); `TestBaseRefHasNoDefaultMarker` PASS (no `+kubebuilder:default`); tide-push `TestRunCloneWritesSuccessEnvelopeFeatureBranch` PASS |
| 2 | Unresolvable baseRef fails fast with a typed condition naming the bad ref (classify-don't-retry, no retry loop) | ✓ VERIFIED | `ErrBaseRefUnresolvable` + `"unable to resolve %q to a commit SHA"` (branch.go:34,156); tide-push exit 2 + reason `baseref-unresolvable` (main.go:381-383, `TestRunCloneWritesFailureEnvelopeUnresolvable` PASS); controller classify+halt (project_controller.go:727-764); **envtest Spec A/B/C PASS** — condition set + Job NOT deleted, halt survives TTL GC (Pitfall 2), releases on generation bump |
| 3 | status.git.baseSHA shows the resolved base SHA on a running Project | ✓ VERIFIED | Read-before-flip stamp in one patch (project_controller.go:682-700); **envtest Spec D PASS** ("stamps status.git.baseSHA and CloneComplete in one patch, CloneFailed=False"); stamped on every run incl default-HEAD (`TestRunCloneWritesSuccessEnvelopeDefaultHead` PASS) |
| 4 | Fields in BOTH API versions, survive v1alpha1⇄v1alpha2 round-trip + tide-crds chart upgrade without silent pruning | ✓ VERIFIED | BaseRef/BaseSHA present in v1alpha1 + v1alpha2 source, config/crd/bases (2 blocks each), and tide-crds chart (2 blocks each); `TestBaseRefBaseSHARoundTrip` PASS (strategy-None equivalent — no conversion webhook, v1alpha1 served:false); render-lock `TestHelmTideCRDsRenderBaseRefBothVersions` PASS (baseRef×2, baseSHA×2, pattern×2 survive `helm template`); INSTALL.md CRD-first upgrade order documented |

**Score:** 4/4 success criteria verified (0 present-behavior-unverified)

### Commands Re-run (this verification)

| Check | Command | Result |
|-------|---------|--------|
| Build touched pkgs | `go build ./api/... ./pkg/git/... ./cmd/tide-push/... ./internal/...` | exit 0 |
| Build all (demo-init concern) | `go build ./...` | exit 0 — demo-init break did NOT reproduce; untouched by Phase 35 |
| Vet | `go vet ./api/... ./pkg/git/... ./cmd/tide-push/...` | exit 0 |
| Full touched-pkg tests | `go test ./api/... ./pkg/git/... ./cmd/tide-push/... -count=1` | exit 0, 4× `ok`, 0 FAIL |
| baseRef envtest (35-03) | `go test ./internal/controller/... -ginkgo.focus="baseRef classification"` | exit 0 — **SUCCESS! 7 Passed \| 0 Failed** (Specs A–F) |
| Schema/round-trip (35-01) | `go test ./api/v1alpha1 -run 'Phase35\|BaseRef\|RoundTrip'` | exit 0, all PASS incl `TestBaseRefBaseSHARoundTrip` |
| CRD render-lock (35-01) | `go test ./test/integration/kind -run TestHelmTideCRDsRenderBaseRefBothVersions` | exit 0, PASS (cluster-free `helm template`) |
| tide-push clone (35-02) | `go test ./cmd/tide-push -run 'Clone\|Envelope'` | exit 0, 11 PASS |

### Required Artifacts

| Artifact | Status | Details |
|----------|--------|---------|
| `api/v1alpha2/project_types.go` + `api/v1alpha1/project_types.go` | ✓ VERIFIED | BaseRef (MinLength=1, MaxLength=250, Pattern `^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`, no default) + BaseSHA; identical twins |
| `pkg/git/branch.go` | ✓ VERIFIED | resolveBaseRef explicit chain (refs/-verbatim → branch → refs/remotes/origin fallback → tag peel → 40-hex SHA), ErrBaseRefUnresolvable sentinel, idempotent early-return FIRST |
| `cmd/tide-push/main.go` | ✓ VERIFIED | `--base-ref`/`--project-uid` flags, writeCloneEnvelope (Kind=CloneResult), exit 2 + reason baseref-unresolvable, baseSHA success envelope |
| `api/v1alpha2/shared_types.go` | ✓ VERIFIED | `ConditionCloneFailed="CloneFailed"`, `ReasonBaseRefUnresolvable="BaseRefUnresolvable"` |
| `internal/controller/project_controller.go` + `push_helpers.go` | ✓ VERIFIED | generation-scoped halt gate, WR-03 classify branch (stale-envelope guard), read-before-flip baseSHA stamp; buildCloneJob wiring + termination-message policy |
| `internal/controller/project_baseref_halt_test.go` | ✓ VERIFIED | envtest Specs A–F, all pass |
| `test/integration/kind/baseref_crd_render_test.go` | ✓ VERIFIED | helm-render both-versions lock, passes |
| `test/integration/kind/medium_http_test.go` Spec 4 + `withBaseRef` fixture | ⚠️ PRESENT (cluster-run pending) | Substantive + wired (asserts baseSHA==targetTip AND !=defaultTip); requires live kind cluster — behavior covered redundantly at lower layers |
| `docs/project-authoring.md` + `docs/INSTALL.md` | ✓ VERIFIED | Accepted/rejected forms table, inert-after-clone + recovery semantics, CRD-first upgrade order with silent-pruning warning |

### Key Link Verification

| From | To | Via | Status |
|------|----|----|--------|
| Go struct markers | tide-crds chart | controller-gen → config/crd/bases → helmify; both version blocks render (render-lock PASS) | ✓ WIRED |
| EnsureRunBranch (single resolution site) | runClone → writeCloneEnvelope → /dev/termination-log | tide-push clone tests | ✓ WIRED |
| envelope.reason==baseref-unresolvable | CloneFailed/BaseRefUnresolvable condition → dispatch halt gate | envtest Spec A/B | ✓ WIRED |
| envelope.baseSHA | status.git.baseSHA in CloneComplete flip patch | envtest Spec D | ✓ WIRED |
| condition.ObservedGeneration==Generation | generation-scoped halt release on spec edit | envtest Spec C | ✓ WIRED |

### Requirements Coverage

| Requirement | Status | Evidence |
|-------------|--------|----------|
| BASE-01 (spec.git.baseRef, absent=HEAD, no default) | ✓ SATISFIED | Criterion 1 evidence |
| BASE-02 (unresolvable → typed condition, classify-don't-retry) | ✓ SATISFIED | Criterion 2 evidence |
| BASE-03 (baseSHA stamped; both versions + round-trip + upgrade-path lock) | ✓ SATISFIED | Criteria 3 & 4 evidence |

### Anti-Patterns Found

None. Zero TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER across all phase-modified source and docs files.

### Intentional Scoping (confirmed deliberate — NOT gaps)

- **Chart version NOT bumped** (stays 1.0.6): confirmed intentional per CONTEXT/RESEARCH — batches with Phase 36. BASE-03's "no silent pruning" is proven by the render-lock test + chart-installed-CRD e2e, not a version bump. ✓
- **Unresolvable-ref locked at envtest layer only** (not duplicated in kind suite): confirmed intentional (35-03 specs). ✓
- **Exit 2 + reason baseref-unresolvable** (controller classifies on `reason`, not exit code); exit 14 remains Phase 34's — no collision. ✓

### Human Verification Recommended (non-blocking)

1. **kind Layer B e2e (Spec 4)** — run `medium_http_test.go` "stamps status.git.baseSHA from a non-default baseRef branch over http://" on a live kind cluster + Docker. Expected: CloneComplete with baseSHA == base-ref-target tip, != default tip. This is redundant cluster coverage; criteria 1/2/3 are independently proven at unit + tide-push + envtest layers, so it does not block merge.

### Gaps Summary

No blocking gaps. All four ROADMAP success criteria are independently verified against re-run tests and read code. The only item not executed here is the kind Layer B e2e, which requires a live cluster and whose behavior is fully covered by faster layers.

---

_Verified: 2026-07-07T05:01:54Z_
_Verifier: Claude (gsd-verifier)_
