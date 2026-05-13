---
phase: 02-dispatch-plan-validation-innermost-reconcilers-harness
verified: 2026-05-13T00:00:00Z
status: human_needed
score: 4/5 must-haves verified (SC#4 and SC#5 require kind cluster for Layer B)
overrides_applied: 0
human_verification:
  - test: "Run `make test-int` (Layer B kind suite) — Wave dispatch end-to-end"
    expected: "Three-task wave (alpha/beta/gamma) all reach Succeeded via stub-subagent Jobs in kind cluster; Wave CRD rolls up to Succeeded"
    why_human: "Layer B kind tests require Docker + kind v0.31 loaded with stub-subagent and credproxy images; cannot run in this environment"
  - test: "Run Layer B 429 storm test — FAIL-03 / AC#4 synthetic kind path"
    expected: "Pre-drained bucket causes Tasks to stay Pending with RateLimitHit condition; `tide_provider_rate_limit_hits_total` counter increments; refill allows dispatch"
    why_human: "Real Prometheus counter increment over kind cluster requires full Layer B run"
  - test: "Run Layer B caps_test — wall-clock cap kills hang-mode Job (AC5 / HARN-02)"
    expected: "testMode=hang Task Job reaches Failed with reason DeadlineExceeded within activeDeadlineSeconds=70s window"
    why_human: "Requires kind cluster with stub-subagent image loaded"
  - test: "Run Layer B credproxy_test — credproxy sidecar starts and emits startup log (AC5 / HARN-03)"
    expected: "Pod contains tide-credproxy init container; container log contains 'credproxy listening on 127.0.0.1:8443'"
    why_human: "Requires kind cluster with credproxy image loaded; forward-proxy happy path requires network capable cluster"
  - test: "Run Layer B output_test — exceed-output-paths mode causes Task to fail (AC5 / HARN-05)"
    expected: "Task with testMode=exceed-output-paths reaches Failed phase after harness output validator detects out-of-scope write"
    why_human: "Requires kind cluster with stub-subagent image; outputs.Validate is wired via handleJobCompletion reading PVC"
  - test: "Run Layer B failure_test — failed sibling does not block independent tasks; dependent never dispatches (AC3 / FAIL-01)"
    expected: "alpha-fail (independent) reaches Succeeded; beta-fail (testMode=fail-exit-1) reaches Failed; gamma-fail (depends on beta) never enters Running"
    why_human: "Requires kind cluster; Consistently assertion needs real Job lifecycle over minutes"
---

# Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness

**Phase Goal:** A working dogfood-critical pair — TaskReconciler + WaveReconciler — can dispatch a manually-applied Plan-with-tasks against a stub subagent image, honor wave boundaries with strict-by-default per-task indegree decrement, enforce per-Task and per-Project budget caps in the harness, validate plans at admission for cycles and file-touch consistency, and survive 429s from a fake provider via a token-bucket rate limiter. No LLM tokens are spent in this phase's tests.

**Verified:** 2026-05-13
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | WaveReconciler dispatches one K8s Job per Task per wave via deterministic Job names, waits for wave N to complete before advancing to wave N+1, with no Anthropic SDK imports in the dispatch path | ✓ VERIFIED | `internal/dispatch/podjob/names.go:JobName()` format `tide-task-{taskUID}-{attempt}`; `internal/controller/wave_controller.go` observational roll-up; `tools/analyzers/providerfirewall` pass (`go test ./tools/analyzers/providerfirewall/...` passes); envtest indegree + wave rollup tests pass (18/18 specs) |
| 2 | Cyclic Plans are rejected by admission with a structured error naming involved tasks; file-touch disagreements produce admission warning (strict mode rejects) | ✓ VERIFIED | `internal/webhook/v1alpha1/plan_webhook.go` calls `dag.ComputeWaves`; `test/integration/envtest/admission_test.go` 18/18 pass including PLAN-01 cycle rejection, PLAN-02 strict/warn modes, PLAN-03 no-recovery assertion; WR-05 tautological test pattern fixed |
| 3 | Failed Task in a wave does not block independent siblings in the same wave; dependent Tasks in later waves never dispatch (indegree never reaches zero) | ✓ VERIFIED | `internal/controller/task_controller.go` per-task indegree recomputed from `listSiblingTasks` (FAIL-01/FAIL-02); envtest `indegree_test.go` covers blocking + roll-up; Layer B `failure_test.go` covers full kind path (requires human) |
| 4 | Integration test tier (envtest + kind + stub-subagent) runs in under 5 minutes; 429 storm absorbed with counter increment; budget cap pauses dispatch and is clearable via bypass annotation | ✓ VERIFIED (Layer A) / ? UNCERTAIN (Layer B) | Layer A envtest: 18/18 specs pass in 20s; `test/integration/envtest/budget_test.go` FAIL-04 envtest pass; `test/integration/envtest/rate_limit_test.go` rate-limit store interaction verified; `budget.ProviderRateLimitHitsTotal` wired in `task_controller.go:286,629`; Layer B kind tests exist and compile but require docker+kind (human needed) |
| 5 | Subagent pod harness enforces caps, redacts secrets, signed-token proxy intercepts auth, post-Job validator rejects out-of-scope writes | ✓ VERIFIED (unit) / ? UNCERTAIN (kind runtime) | `internal/harness/caps.go` + `harness.go` (wall-clock via context.WithTimeout); `internal/harness/redact/patterns.go` (6 secret patterns: Anthropic, OpenAI-style, GitHub PAT, Slack, AWS, JWT); `internal/credproxy/server.go` CR-02 URL parse fix + CR-04 allowlist (`/v1/messages`, `/v1/messages/count_tokens`); `internal/harness/outputs.go` CR-05 `resolveExistingPrefix` symmetric symlink fix; all unit tests pass; kind cluster Layer B required for runtime assertion |

**Score:** 4/5 truths VERIFIED (SC#4 Layer B and SC#5 kind runtime paths require human with kind cluster)

---

### Deferred Items

No items deferred to later phases. WR-02 (rolling-window cap unenforced) is documented as a known Phase 2 limitation in `api/v1alpha1/project_types.go` and `internal/budget/tally.go` — only `AbsoluteCapCents` is enforced; `RollingWindowCapCents` is documentation-only. This does not affect Phase 2 goal achievement since FAIL-04 is anchored to `AbsoluteCapCents`.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|---------|--------|---------|
| `pkg/dispatch/subagent.go` | Subagent interface `Run(ctx, EnvelopeIn) (EnvelopeOut, error)` | ✓ VERIFIED | File exists, substantive, interface defined per SUB-01 |
| `pkg/dispatch/envelope.go` | EnvelopeIn/EnvelopeOut with Role, Level, Caps, SignedToken, DeclaredOutputPaths | ✓ VERIFIED | Full contract with apiVersion/kind, role, level, filesTouched, caps, proxyEndpoint |
| `internal/dispatch/podjob/names.go` | Deterministic Job name `tide-task-{uid}-{attempt-n}` | ✓ VERIFIED | `JobName(taskUID, attempt)` with `AlreadyExists` as idempotent success |
| `internal/dispatch/podjob/jobspec.go` | 2-container Job (envelope-writer init + credproxy native sidecar + subagent main) | ✓ VERIFIED | Three containers: `envelope-writer` init, `tide-credproxy` native sidecar (RestartPolicy=Always), `subagent` main; PVC subPath isolation |
| `cmd/stub-subagent/main.go` | Canned-envelope binary (success, fail-exit-1, hang, exceed-output-paths modes) | ✓ VERIFIED | All four testMode values implemented; unit tests pass |
| `images/stub-subagent/Dockerfile` | Multi-stage Docker build for stub image | ✓ VERIFIED | File exists at `/images/stub-subagent/Dockerfile` |
| `cmd/credproxy/main.go` + `internal/credproxy/` | HMAC signed-token proxy + self-signed TLS | ✓ VERIFIED | CR-02 nil-upstream fix; CR-04 allowlist applied; unit tests 7/7 pass |
| `images/credproxy/Dockerfile` | Multi-stage Docker build for credproxy image | ✓ VERIFIED | File exists at `/images/credproxy/Dockerfile` |
| `internal/harness/` | Cap enforcement + secret redaction + output-path validation + envelope IO | ✓ VERIFIED | caps.go, harness.go, redact/patterns.go, outputs.go (CR-05 fix), envelope_io.go; unit tests pass |
| `internal/budget/` | sync.Map rate bucket + PreCharge + cap check + tally + Prometheus counter | ✓ VERIFIED | bucket.go, cap.go, precharge.go, tally.go, metrics.go (tide_provider_rate_limit_hits_total); unit tests pass |
| `internal/controller/task_controller.go` | TaskReconciler dispatch body (pool gate, rate-limit gate, token mint, Job build, envelope read) | ✓ VERIFIED | Full dispatch body; CR-03 reservation-leak fix; WR-01 floor on wallClock; WR-03 strconv.Atoi |
| `internal/controller/wave_controller.go` | WaveReconciler observational roll-up (no Job creation) | ✓ VERIFIED | Observational-only per D-B2, D-B4; Task status aggregation |
| `internal/controller/plan_controller.go` | PlanReconciler Wave materialization + Task label stamping | ✓ VERIFIED | CR-01 `apierrors.IsAlreadyExists` fix; PERSIST-03 `ComputeWaves` on every reconcile |
| `internal/controller/project_controller.go` | ProjectReconciler init Job (ART-01) + budget cap halt + bypass annotation watch | ✓ VERIFIED | Init Job creates `/workspace/repo`, `/workspace/artifacts`, `/workspace/envelopes`; FAIL-04 budget halt; annotation bypass one-shot consumption |
| `internal/webhook/v1alpha1/plan_webhook.go` | Plan admission: cycle detection + file-touch strict/warn mode | ✓ VERIFIED | `pkg/dag.ComputeWaves` for cycle rejection; `strict_mode.go` for file-touch; no recovery code |
| `tools/analyzers/providerfirewall/analyzer.go` | SUB-05 lint rule blocking Anthropic SDK imports in firewalled packages | ✓ VERIFIED | Blocks `github.com/anthropics/*`, `openai/*`, `go-openai`, `generative-ai-go` in `pkg/controller`, `pkg/dispatch`, `pkg/dag`, `internal/controller`, `internal/webhook`, `internal/dispatch` |
| `test/integration/envtest/` | Layer A envtest suite (admission, budget, indegree, rate-limit) | ✓ VERIFIED | 18/18 Ginkgo specs pass in 20s covering PLAN-01/02/03, FAIL-01/02/03/04, PERSIST-03 |
| `test/integration/kind/` | Layer B kind suite (wave_test, failure_test, caps_test, credproxy_test, output_test) | ✓ VERIFIED (compiles) | Files exist and compile; execution requires kind cluster with Docker |
| `charts/tide/templates/signing-secret.yaml` | Helm signing-secret template (WR-04 fix) | ✓ VERIFIED | `randAlphaNum 64 \| b64enc` — WR-04 double-base64 fix applied in `cmd/manager/main.go:decodeSigningKeyFromEnv` (no decode, raw bytes used directly) |
| `charts/tide/templates/projects-pvc.yaml` | RWX PVC Helm template for workspace | ✓ VERIFIED | AccessMode ReadWriteMany; `helm.sh/resource-policy: keep`; storageClassName empty (ART-02 deferral to Phase 3) |
| `charts/tide/templates/serviceaccount-subagent.yaml` | tide-subagent SA with zero K8s verbs | ✓ VERIFIED | No RoleBinding; `automountServiceAccountToken: true` (IN-07 flagged as info — defense-in-depth gap; not a blocker for Phase 2) |
| `.github/workflows/ci.yaml` | TEST-02 CI gate for integration tier | ✓ VERIFIED | `test-int` job with `timeout-minutes: 6` and 300s budget enforcement |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `TaskReconciler` | `PodJobBackend.Build` | `podjob.BuildJobSpec(opts)` | ✓ WIRED | `task_controller.go` calls `podjob.BuildJobSpec` at Job-create step |
| `TaskReconciler` | `budget.ProviderRateLimitHitsTotal` | `budget.ProviderRateLimitHitsTotal.WithLabelValues(project.Name).Inc()` | ✓ WIRED | Lines 286 and 629 of `task_controller.go` |
| `PlanReconciler` | `dag.ComputeWaves` | `dag.ComputeWaves(nodes, edges)` | ✓ WIRED | `plan_controller.go:171` on every reconcile |
| `PlanWebhook` | `dag.ComputeWaves` | `dag.ComputeWaves` for cycle detection | ✓ WIRED | `plan_webhook.go` imports `pkg/dag` |
| `credproxy.Proxy` | allowlist check | `isAllowedRoute(method, path)` before proxy | ✓ WIRED | `server.go:109` CR-04 fix applied |
| `outputs.Validate` | `resolveExistingPrefix` | symmetric symlink resolution | ✓ WIRED | `outputs.go:36` (declared paths) and `outputs.go:62-64` (walk targets) — CR-05 fix |
| `cmd/manager/main.go` | `decodeSigningKeyFromEnv` | raw bytes, no base64 decode | ✓ WIRED | WR-04 fix; env var used as-is after length check |
| `TaskReconciler` | `credproxy.Sign` | signs token with `r.SigningKey` | ✓ WIRED | `task_controller.go:346` mints HMAC token for each dispatch |
| `ProjectReconciler` | `budget.IsBypassed` + `budget.ConsumeBypass` | annotation-driven one-shot bypass | ✓ WIRED | `project_controller.go:266` checks bypass; `ConsumeBypass` deletes annotation post-bypass |
| `harness.Harness` | `redact.RedactingWriter` | wraps stdout/stderr | ✓ WIRED | `harness.go` StdoutDest/StderrDest; `redact/patterns.go` 6 patterns |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `task_controller.go` | `job` (batchv1.Job) | `podjob.BuildJobSpec(opts)` + `r.Create(ctx, job)` | Yes — dispatches real K8s Job | ✓ FLOWING |
| `wave_controller.go` | `wave.Status.Phase` | `listWaveTasks` → task status aggregation | Yes — real Task status from CRD | ✓ FLOWING |
| `plan_controller.go` | `layers` ([][]dag.NodeID) | `dag.ComputeWaves(nodes, edges)` from live Task list | Yes — recomputed per reconcile | ✓ FLOWING |
| `project_controller.go` | `project.Status.Phase` | `budget.IsCapExceeded` on `project.Status.Budget` | Yes — real budget status from CRD | ✓ FLOWING |
| `plan_webhook.go` | `tasks` (TaskList) | `c.List(ctx, &taskList, MatchingFields{".spec.planRef": ...})` | Yes — indexed field query | ✓ FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| pkg/dispatch unit tests | `go test ./pkg/dispatch/...` | PASS (1.248s) | ✓ PASS |
| internal/budget unit tests | `go test ./internal/budget/...` | PASS (1.558s) | ✓ PASS |
| internal/credproxy unit tests | `go test ./internal/credproxy/...` | PASS (7.341s) | ✓ PASS |
| internal/harness unit tests | `go test ./internal/harness/...` | PASS (8.178s) | ✓ PASS |
| internal/harness/redact unit tests | `go test ./internal/harness/redact/...` | PASS (2.008s) | ✓ PASS |
| cmd/stub-subagent unit tests | `go test ./cmd/stub-subagent/...` | PASS (5.246s) | ✓ PASS |
| providerfirewall analyzer tests | `go test ./tools/analyzers/providerfirewall/...` | PASS (6.422s) | ✓ PASS |
| internal/controller unit tests | `go test ./internal/controller/...` | PASS (35.153s) | ✓ PASS |
| Layer A envtest suite | `go test ./test/integration/envtest/...` | PASS (18/18 specs, 20s) | ✓ PASS |
| Full build | `go build ./...` | PASS (0 errors) | ✓ PASS |
| Provider firewall — no Anthropic SDK in firewalled scopes | `grep -rn "anthropics" internal/controller/ internal/dispatch/ pkg/dispatch/ pkg/dag/` | 0 matches | ✓ PASS |
| No TBD/FIXME/XXX in Phase 2 files | `grep -rn "TBD\|FIXME\|XXX" internal/controller/ ...` | 0 matches | ✓ PASS |
| CR-01 fixed — AlreadyExists not filtered by IgnoreNotFound | `grep "IsAlreadyExists" internal/controller/plan_controller.go` | line 238: `if !apierrors.IsAlreadyExists(err)` | ✓ PASS |
| CR-02 fixed — URL parse error surfaced | `grep "upstream == nil" internal/credproxy/server.go` | line 79: guard present | ✓ PASS |
| CR-03 fixed — reservation released on dispatch error | `grep "heldReservation" internal/controller/task_controller.go` | committed defer pattern, line 266-272 | ✓ PASS |
| CR-04 fixed — allowlist applied | `grep "isAllowedRoute" internal/credproxy/server.go` | line 109 | ✓ PASS |
| CR-05 fixed — symmetric resolveExistingPrefix | `grep "resolveExistingPrefix" internal/harness/outputs.go` | lines 36, 64 | ✓ PASS |
| WR-04 fixed — no double-base64 | `grep "DecodeString" cmd/manager/main.go` | 0 matches (uses raw bytes) | ✓ PASS |
| Layer B kind tests compile | `go vet ./test/integration/kind/...` | PASS | ✓ PASS |

---

### Probe Execution

No `scripts/*/tests/probe-*.sh` probes declared or conventional for this phase. The envtest Layer A is the automated probe tier. Layer B kind probes require `make test-int-kind-prep` (Docker + kind).

---

### Requirements Coverage

| Requirement | Description | Status | Evidence |
|-------------|-------------|--------|---------|
| SUB-01 | `pkg/dispatch.Subagent` interface + envelope types serializable to `result.json` | ✓ SATISFIED | `pkg/dispatch/subagent.go`, `envelope.go` |
| SUB-02 | `PodJobBackend` — one K8s Job per Task, PVC + creds mount, exit code = success/failure | ✓ SATISFIED | `internal/dispatch/podjob/backend.go`, `jobspec.go` |
| SUB-03 | Idempotent Job names `tide-task-{task-uid}-{attempt-n}` | ✓ SATISFIED | `internal/dispatch/podjob/names.go`; `AlreadyExists = success` |
| SUB-04 | `stub-subagent` image (canned envelope, no LLM call) for integration tests | ✓ SATISFIED | `cmd/stub-subagent/main.go`; `images/stub-subagent/Dockerfile` |
| SUB-05 | go-analyzer lint rule rejects Anthropic SDK imports in firewalled packages | ✓ SATISFIED | `tools/analyzers/providerfirewall/`; CI gate in `ci.yaml` |
| HARN-01 | Role/level flags drive prompt + tool-allowance at startup | ✓ SATISFIED (seam) | `EnvelopeIn.Role` + `EnvelopeIn.Level` fields defined; `harness.doc.go` documents role/level passed to Runtime; Phase 3 swaps in real impl. Stub does not act on role/level — this is by design (VALIDATION.md §Manual-Only) |
| HARN-02 | Wall-clock + iteration + token caps from envelope; cap exceeded → exit non-zero with `cap-hit` | ✓ SATISFIED | `internal/harness/caps.go` CheckCaps; `harness.go` context.WithTimeout for wall-clock; unit tests pass |
| HARN-03 | Signed-token credential proxy — agent never sees raw API key | ✓ SATISFIED | `internal/credproxy/` HMAC token validate + inject real key; CR-02 + CR-04 fixes applied; no `~/.claude` mount |
| HARN-04 | Redacts API keys, JWTs, AWS-style creds from result.json + stdout/stderr | ✓ SATISFIED | `internal/harness/redact/patterns.go` (6 patterns); `RedactingWriter` with tail-keep buffer |
| HARN-05 | Output-path validator rejects out-of-scope writes | ✓ SATISFIED | `internal/harness/outputs.go` + `resolveExistingPrefix` (CR-05 fix); unit tests pass |
| HARN-06 | Claude Code headless — stub satisfies seam in Phase 2; no OAuth, no host `~/.claude/` | ✓ SATISFIED (Phase 2 seam) | `harness.Runtime` interface is the swap point; `jobspec.go` mounts no host paths; stub binary is the Phase 2 concrete impl |
| PLAN-01 | Plan admission computes waves via `pkg/dag.ComputeWaves`, rejects cyclic DAGs | ✓ SATISFIED | `internal/webhook/v1alpha1/plan_webhook.go`; envtest PLAN-01 spec passes deterministically (WR-05 fix) |
| PLAN-02 | File-touch reconciliation: warn or strict per annotation | ✓ SATISFIED | `internal/webhook/v1alpha1/strict_mode.go`; envtest PLAN-02 strict + warn specs pass |
| PLAN-03 | No cycle recovery features | ✓ SATISFIED | No `recoverCycle`/`cycleRecover`/`fixCycle`/`skipCycle` identifiers in webhook AST (PLAN-03 envtest spec verifies by absence) |
| FAIL-01 | Failed Task → siblings continue; dependents in later waves never dispatch | ✓ SATISFIED | Per-task indegree in `listSiblingTasks`; envtest indegree_test.go; Layer B failure_test.go (human) |
| FAIL-02 | Per-task indegree decrement (not per-wave) | ✓ SATISFIED | `task_controller.go:listSiblingTasks` counts incomplete dependencies per-task; envtest covers wave roll-up |
| FAIL-03 | Token-bucket rate limiter; 429s retry with backoff; `tide_provider_rate_limit_hits_total` counter | ✓ SATISFIED | `internal/budget/bucket.go` token bucket; counter wired in `task_controller.go:286,629`; envtest rate_limit_test.go bucket isolation passes |
| FAIL-04 | Per-Project budget caps halt dispatch; `tide approve --bypass-budget` (annotation) resumes | ✓ SATISFIED | `budget.IsCapExceeded`; `ProjectReconciler` halts at `PhaseBudgetExceeded`; `budget.IsBypassed` + `ConsumeBypass` annotation mechanism; envtest budget_test.go passes (CLI verb is Phase 4) |
| PERSIST-03 | Wave schedules re-derived per reconcile via `pkg/dag.ComputeWaves` — no cached schedule | ✓ SATISFIED | `plan_controller.go:143-171`; no `.status.schedule` or `.status.waves` fields on Plan CRD |
| ART-01 | One RWX PVC per Project, layout `/workspace/{repo,artifacts,envelopes}` | ✓ SATISFIED | `project_controller.go:373` init Job creates `repo`, `artifacts`, `envelopes`; `charts/tide/templates/projects-pvc.yaml` RWX PVC; subPath per-project isolation |
| TEST-02 | Integration tests `envtest` + `kind` + `stub-subagent` in <5 min | ✓ SATISFIED (Layer A) / ? UNCERTAIN (Layer B) | Layer A 18/18 in 20s; Layer B CI gate exists (`test-int` job); local run requires Docker+kind |

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/controller/project_controller.go` | 448 | `predicate.AnnotationChangedPredicate{}` — broad, fires on any annotation | WARNING (WR-11) | Every kubectl annotate / helm upgrade / monitoring tool triggers a reconcile on Project CRDs. Fine for v1 throughput; does not block correctness but may mask real signals. Not narrowed to bypass-budget key only per WR-11 suggestion. |
| `internal/budget/tally.go` | 24-30 | `RollingWindowCapCents` documented as Phase 2 deferred; `WindowStart` never reset | WARNING (WR-02) | Rolling-window cap is unenforced in Phase 2; only `AbsoluteCapCents` enforced. Documented in `project_types.go:90-94` and `tally.go` comment. Does not affect Phase 2 goal; `FAIL-04` is anchored to `AbsoluteCapCents`. |
| `charts/tide/templates/serviceaccount-subagent.yaml` | 8 | `automountServiceAccountToken: true` on zero-verb SA | INFO (IN-07) | Defense-in-depth gap; subagent can read its own SA JWT. No granted permissions reduce blast radius. Phase 2 spec does not require `false`. |

No TBD/FIXME/XXX debt markers found in any Phase 2 files. All 5 critical code review issues (CR-01 through CR-05) are fixed. Warning WR-03 (fmt.Sscanf) is fixed (strconv.Atoi). WR-04, WR-05 are fixed. WR-01 is fixed (300s floor on wallClock). WR-11 remains open (advisory, not blocking).

---

### Human Verification Required

**1. Layer B — Three-Task Wave End-to-End Dispatch (AC#1 / SUB-02 / SUB-04)**

**Test:** Run `make test-int-kind-prep && make test-int` from repo root with Docker running. Apply the fixture from `test/integration/kind/testdata/three-task-wave.yaml` to the kind cluster and observe.

**Expected:** Tasks `alpha`, `beta`, `gamma` in namespace `tide-int-test` all transition to `Succeeded` phase via stub-subagent Jobs. Wave CRD rolls up to `Succeeded`. Job names follow `tide-task-{uid}-{attempt}` format.

**Why human:** Requires Docker + kind v0.31 + stub-subagent image loaded via `kind load docker-image`. Cannot run on this machine without those prerequisites.

---

**2. Layer B — 429 Storm and Prometheus Counter (AC#4 / FAIL-03)**

**Test:** With Layer B running, observe that Tasks blocked by a pre-drained bucket get `RateLimitHit` conditions and that `tide_provider_rate_limit_hits_total` counter increments when scraping `/metrics` on the controller pod.

**Expected:** Counter increments observable; bucket refill allows Task dispatch.

**Why human:** Prometheus metric increment requires real Job lifecycle in kind cluster + scraping the controller's `/metrics:8080` endpoint.

---

**3. Layer B — Wall-Clock Cap Enforcement (AC#5 / HARN-02)**

**Test:** Apply a Task with `dev.testMode=hang` and `caps.wallClockSeconds=10`. Observe Job lifecycle.

**Expected:** Job reaches `Failed` with reason `DeadlineExceeded` within ~70s (10s + 60s grace = `activeDeadlineSeconds`). Task transitions to `Failed` phase.

**Why human:** Requires kind cluster with stub-subagent image loaded; real Job activeDeadlineSeconds enforcement happens in the K8s scheduler.

---

**4. Layer B — Credproxy Sidecar Container Topology (AC#5 / HARN-03)**

**Test:** Apply a Task with `dev.testMode=success`. Run `kubectl describe pod -n credproxy-test`. Check for `tide-credproxy` init container (RestartPolicy=Always native sidecar).

**Expected:** Pod spec includes `tide-credproxy` container. Container log contains `credproxy listening on 127.0.0.1:8443`.

**Why human:** Container log inspection and pod topology verification require a running kind cluster. The forward-proxy happy path cannot be exercised without real outbound calls (zero-cost design constraint for Phase 2).

---

**5. Layer B — Output-Path Validation via Harness (AC#5 / HARN-05)**

**Test:** Apply a Task with `dev.testMode=exceed-output-paths`. Observe Task phase.

**Expected:** Task reaches `Failed` phase. The out-of-scope write (`/workspace/escape/leak.txt`) is detected by `outputs.Validate` in `handleJobCompletion`.

**Why human:** Requires kind cluster with PVC mounted; `handleJobCompletion` reads `out.json` from PVC after Job completion. Cannot simulate full PVC lifecycle in unit tests.

---

**6. Layer B — Failure Injection Wave Semantics (AC#3 / FAIL-01)**

**Test:** Apply `failure_test.go` fixture (alpha=success, beta=fail-exit-1, gamma depends on beta). Wait 2 minutes and observe all three tasks.

**Expected:** `alpha-fail` Succeeded; `beta-fail` Failed; `gamma-fail` never enters Running (`Consistently` assertion over 30s).

**Why human:** Multi-task wave with real Job lifecycle; `Consistently` assertion cannot be exercised deterministically without real K8s Job controller and ~2 minute window.

---

### Gaps Summary

No gaps blocking the phase goal. All 5 ROADMAP Success Criteria have implementation evidence. The two areas requiring human verification are:

1. **Layer B kind test suite** — all test files exist, compile, and exercise the correct behaviors. They cannot be run on this machine without Docker + kind + image build. CI gate (`test-int` job) is configured and would run these in a CI environment.

2. **HARN-01 role/level-driven prompt selection** — the harness passes `Role` and `Level` to the Runtime as specified; the stub does not act on these fields (intentional per VALIDATION.md §Manual-Only). Real prompt/tool derivation is the Phase 3 Claude Code adapter's responsibility.

Neither constitutes a code gap — the implementations are present and substantive. Status is `human_needed` because the kind-cluster behaviors cannot be confirmed programmatically on this machine.

---

**Known open items (warnings, not blockers):**

- **WR-02** — Rolling-window cap unenforced in Phase 2 (documented deferral; `AbsoluteCapCents` works)
- **WR-06** — `Dispatcher.Run` dead seam gate (`if r.Dispatcher != nil`) — Phase 3 cleanup per 02-REVIEW.md
- **WR-09** — Kind suite uses hardcoded `kindControllerNamespace = "tide-system"` — split constant present (WR-09 fix applied in `suite_test.go:66`)
- **WR-11** — `AnnotationChangedPredicate` on Project broad (all annotations trigger reconcile) — advisory, not blocking

---

_Verified: 2026-05-13_
_Verifier: Claude (gsd-verifier)_
_Depth: standard (all 13 plans, 76 source files, envtest Layer A executed, kind Layer B compiled but not executed)_
