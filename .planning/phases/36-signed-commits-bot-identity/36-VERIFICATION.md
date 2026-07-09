---
phase: 36-signed-commits-bot-identity
verified: 2026-07-08T18:30:00Z
status: passed
gate_decision: APPROVED
score: 6/6 must-haves verified
behavior_unverified: 0
overrides_applied: 0
requirements_verified: [SIGN-01]
warnings:
  - summary: "bot→agent vocabulary rename is functionally complete but cosmetically incomplete — 7 residual 'bot' references in 4 .go files (comments + one test fixture); the phase's own legacy-name gate was case-sensitive and missed them"
    detail: "Strict legacy identity MECHANISM (TIDE_BOT_NAME/TIDE_BOT_EMAIL env vars, tideBotSignature helper, hardcoded tide-bot@tideproject.k8s) is fully removed and verified. Residual references are prose vocabulary: cmd/tide-push/main.go:335,526 · internal/controller/push_helpers.go:38 · pkg/git/commit.go:57 · pkg/git/commit_test.go:53,78,79. Two of these (main.go:526 'Author is the fixed TIDE-bot signature', push_helpers.go:38 'the fixed TIDE-bot author signature') are now factually inaccurate — the phase changed that identity from fixed to env-sourced but left the comment claiming it is fixed. SUMMARY 36-01's 'GATE-CLEAN / rename applied at all three commit sites' is narrowly true for the case-sensitive pattern but overstates 'the rename applies everywhere' (ROADMAP goal clause). Zero functional/runtime impact."
    severity: low
    suggested_fix: "Sweep the 7 references to agent vocabulary (or reword the two 'fixed' comments to 'env-sourced'). One-line edits; no behavior change. Alternatively accept via override — the strict legacy names are gone and the residual is comment/test-fixture only."
---

# Phase 36: Signed Commits + Bot Identity — Verification Report

**Phase Goal:** The TIDE agent identity (name/email) is uniformly configurable across all three commit sites — harness, integrate, tide-push — via the precedence chain `spec.git.agentName`/`agentEmail` → chart value → compiled-in default, with the tide-push hardcoded identity removed. The bot→agent rename applies everywhere (env vars `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL`, compiled default `TIDE Agent <tide-agent@tideproject.k8s>`). Scope is identity-only (SIGN-01); GPG signing (SIGN-02/03/04) descoped from v1.0.7.

**Verified:** 2026-07-08
**Status:** passed (1 low-severity WARNING)
**Gate Decision:** APPROVED
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | `pkg/git/identity.go` is the SINGLE compiled-in default `TIDE Agent <tide-agent@tideproject.k8s>` + `TIDE_AGENT_NAME`/`TIDE_AGENT_EMAIL` constants; all three commit sites resolve via `AgentIdentity()` | ✓ VERIFIED | identity.go:34-51 declares the 4 constants + empty-is-unset `AgentIdentity()`; harness/commit.go:59, integrate.go:85, tide-push/main.go:137 all call it; `go test ./pkg/git` green |
| 2 | Full precedence chain end-to-end: `spec.git.agentName/agentEmail` (both API versions, Pattern-validated) → `resolveAgentIdentity` → `ProviderDefaults` → Job env injection (subagent + push Job) → chart `agent.name/email` → manager Deployment env | ✓ VERIFIED | CRD fields in api/v1alpha2 + v1alpha1 (MaxLength 100/254, Pattern `^[^<>\r\n]+$` / `^[^<>@\s]+@[^<>@\s]+$`), rendered into both schemas of crd-bases yaml; resolveAgentIdentity (dispatch_helpers.go:320) walks spec→chart→default nil-safe; manager env.go:107-108 reads via pkggit constants; jobspec.go:376-377 + push_helpers.go:256-257 inject unconditionally; boundary_push has 2 helmDefaults params + 2 resolves; all 6 BuildOptions sites resolve (task=2, project/milestone/phase/plan=1); deployment.yaml:104-107 renders both env from `.Values.agent.name/email`; values.yaml:286-289 `agent:{name:"",email:""}`. Behavioral: `TestBuildJobSpec_AgentIdentityEnv` (executor+planner), `TestPodJobBackend_Run_AgentIdentityPrecedence` (3 tiers), `TestResolveAgentIdentity*`, `TestBuildPushJob_AgentIdentityEnv`, `TestHelmDeploymentTemplateRendersAgentIdentityEnv` all PASS |
| 3 | tide-push carries NO hardcoded commit identity; author == committer preserved on go-git path | ✓ VERIFIED | main.go:136 `agentSignature()` builds from `pkggit.AgentIdentity()`+`time.Now()`, sets Author only; callsite main.go:527; main_test.go:875-883 asserts author == compiled default AND `commit.Committer == commit.Author`; test green |
| 4 | bot→agent rename: legacy identity NAMES/mechanism gone from Go source | ✓ VERIFIED (see WARNING) | Strict names (`TIDE_BOT_NAME`, `TIDE_BOT_EMAIL`, `tideBotSignature`, `tide-bot@tideproject.k8s`) return 0 hits outside demo-init. WARNING: 7 residual `bot` vocabulary refs remain in comments/test fixture across 4 files (case-insensitive scan); 2 are now factually inaccurate. Functional rename complete; vocabulary sweep incomplete |
| 5 | Chart FIXED-contract respected: charts/ reproducible from hack/helm/, version consistent at 1.0.7 | ✓ VERIFIED | `make verify-chart-reproducible` → "OK: charts/ tree is reproducible"; `make verify-version-consistency` → "OK: all 4 version files agree on 1.0.7"; agent.* placed top-level, signingKey HMAC block untouched |
| 6 | NO GPG/signing-key code landed (identity-only scope, D-01) | ✓ VERIFIED | No `gpg`/`openpgp`/go-git `SignKey`/`--gpg-sign`/`armored`/`ProtonMail/go-crypto` git-signing hits. All `signingKey`/`SigningKey` hits are the pre-existing credproxy HMAC token key (`tide-signing-key` Secret), explicitly flagged unrelated in 36-CONTEXT.md |

**Score:** 6/6 truths verified (0 present-behavior-unverified). 1 low-severity WARNING on truth 4.

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `pkg/git/identity.go` | 4 constants + AgentIdentity() single source | ✓ VERIFIED | Present, wired, tested; imported by harness + tide-push |
| `internal/harness/commit.go` | CommitWorktree via AgentIdentity() | ✓ VERIFIED | Line 59; git CLI `-c user.name/email` shape intact |
| `pkg/git/integrate.go` | merge identity via AgentIdentity() | ✓ VERIFIED | Line 85; `TestIntegrateTaskBranchesMergeIdentity` PASS (first-ever merge-identity pin) |
| `cmd/tide-push/main.go` | agentSignature() from AgentIdentity()+time.Now() | ✓ VERIFIED | Line 136; W11 contract comment preserved; author-only |
| `api/v1alpha2` + `v1alpha1/project_types.go` | GitConfig.AgentName/AgentEmail, Pattern+MaxLength, both versions, no webhook | ✓ VERIFIED | Lines 234/246 both files; markers present; no conversion machinery |
| `internal/controller/dispatch_helpers.go` | ProviderDefaults.Agent* + resolveAgentIdentity | ✓ VERIFIED | Pure (no os.Getenv), nil-safe, per-field precedence |
| `cmd/manager/env.go` | TIDE_AGENT_* → ProviderDefaults, empty-is-unset | ✓ VERIFIED | Lines 107-108 via pkggit constants |
| `internal/dispatch/podjob/jobspec.go` + `backend.go` | BuildOptions.Agent* + unconditional env; inline mirror, no controller import | ✓ VERIFIED | jobspec.go:376-377 unconditional; backend import-cycle count 0 |
| `internal/controller/push_helpers.go` + `boundary_push.go` | PushOptions.Agent* + Env block; helmDefaults threaded | ✓ VERIFIED | push_helpers.go:256-257; boundary_push 2×helmDefaults + 2×resolve; creds EnvFrom intact |
| chart sources + `charts/tide`, `charts/tide-crds` | agent block, env render, 1.0.7, reproducible | ✓ VERIFIED | deployment.yaml:104-107; version consistent; reproducible |
| `test/integration/kind/agent_identity_chart_test.go` | helm-template contract test | ✓ VERIFIED | 4 byte-strings asserted; PASS without cluster |
| `docs/project-authoring.md` | agentName/agentEmail rows + routable-email note | ✓ VERIFIED | grep 2/3 hits; no signing-key config documented (D-01 honored) |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Merge-commit identity pinned | `go test ./pkg/git -run TestIntegrate` | TestIntegrateTaskBranchesMergeIdentity PASS | ✓ PASS |
| tide-push author==committer, compiled default | `go test ./cmd/tide-push` | ok (main_test.go:881 committer==author) | ✓ PASS |
| Both-builder + backend precedence env injection | `go test ./internal/dispatch/podjob -run AgentIdentity*` | executor+planner+3-tier precedence PASS | ✓ PASS |
| Resolver + push-job env | `go test ./internal/controller -run 'TestResolveAgentIdentity\|TestBuildPushJob'` | ok | ✓ PASS |
| Chart contract render | `go test ./test/integration/kind -run ...AgentIdentityEnv` | PASS (no cluster) | ✓ PASS |
| Chart reproducible | `make verify-chart-reproducible` | OK: reproducible | ✓ PASS |
| Version consistency | `make verify-version-consistency` | OK: all 4 files = 1.0.7 | ✓ PASS |
| Full build | `go build ./...` | exit 0 | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|----------------|-------------|--------|----------|
| SIGN-01 | 36-01, 36-02, 36-03, 36-04 | Uniform configurable agent identity across all three commit sites via spec→chart→compiled precedence; tide-push hardcoded removed; bot→agent rename | ✓ SATISFIED | Truths 1-5 verified with behavioral evidence; REQUIREMENTS.md marks SIGN-01 [x] Complete |

SIGN-02/03/04 correctly descoped from v1.0.7 (moved to Future Requirements 2026-07-03); truth 6 confirms no signing/GPG code landed. No orphaned requirements for this phase.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| cmd/tide-push/main.go | 526 | Comment "Author is the fixed TIDE-bot signature" — now factually wrong (env-sourced, variable) | ⚠️ Warning | Misleading doc in phase-modified file; no runtime impact |
| internal/controller/push_helpers.go | 38 | Comment "the fixed TIDE-bot author signature" — now factually wrong (env-injected) | ⚠️ Warning | Misleading doc in phase-modified file; no runtime impact |
| cmd/tide-push/main.go | 335 | Stale "TIDE-bot signature" vocabulary in comment | ℹ️ Info | Cosmetic |
| pkg/git/commit.go / commit_test.go | 57 / 53,78,79 | "TIDE-bot"/"TIDE bot"/"tide@local" in doc + test fixture (generic Commit() helper, not phase-modified) | ℹ️ Info | Cosmetic; generic helper takes signature as param |

No debt markers (TBD/FIXME/XXX) in phase-modified files. No stubs.

### Gaps Summary

No functional gaps. SIGN-01 is fully delivered and behaviorally verified end-to-end: one compiled-in default source, the complete spec→chart→compiled precedence chain wired through CRD fields, resolver, manager env, both Job builders, all six dispatch sites, and the chart tier; tide-push hardcoded identity removed with author==committer preserved; chart reproducible at a single batched 1.0.7 bump; no GPG/signing code landed.

**One low-severity WARNING (not blocking):** the bot→agent rename is functionally complete but the vocabulary sweep is incomplete — 7 residual "bot" references survive in comments and one test fixture across 4 files (`cmd/tide-push/main.go`, `internal/controller/push_helpers.go`, `pkg/git/commit.go`, `pkg/git/commit_test.go`). Two (main.go:526, push_helpers.go:38) now factually misdescribe behavior this phase changed from fixed to env-sourced. The phase's legacy-name gate was case-sensitive (`TIDE Bot`/`tide-bot`) and missed the `TIDE-bot`/`TIDE bot` variants, so SUMMARY 36-01's "GATE-CLEAN" is narrowly true but overstates the ROADMAP "rename applies everywhere" clause. The strict legacy MECHANISM (old env vars, `tideBotSignature`, `tide-bot@tideproject.k8s`) is fully removed. Recommend a one-line comment/fixture sweep before milestone close; does not block v1.0.7 ship.

---

_Verified: 2026-07-08T18:30:00Z_
_Verifier: Claude (gsd-verifier)_
