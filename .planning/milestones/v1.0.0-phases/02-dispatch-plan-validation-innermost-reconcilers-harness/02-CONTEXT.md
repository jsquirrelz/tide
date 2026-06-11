# Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness - Context

**Gathered:** 2026-05-12
**Status:** Ready for planning

<domain>
## Phase Boundary

Light up the dogfood-critical innermost pair — `TaskReconciler` + `WaveReconciler` — so a manually-applied `Plan` with a fixed task DAG dispatches per-Task K8s `Job`s against a `stub-subagent` image, honors strict-by-default wave-boundary failure semantics with per-task indegree decrement, validates Plans at admission for cycles and file-touch consistency, and survives synthetic 429 storms via a token-bucket rate limiter. The harness inside every subagent pod enforces per-Task wall-clock + iteration + token caps, mediates outbound LLM calls through a signed-token credential proxy (subagent process never sees the raw `ANTHROPIC_API_KEY`), redacts known secret patterns from `result.json` and stdout, and rejects artifact writes outside declared output paths.

Phase 2 ships:
- `pkg/dispatch` public Go contract (Subagent interface + envelope types).
- `internal/dispatch/podjob` concrete `PodJobBackend` driving K8s Jobs.
- `cmd/stub-subagent/` Go binary + image (canned envelopes, no LLM call) for `TEST-02` integration tests.
- `internal/harness` subagent-side harness: cap enforcement, signed-token client, secret redaction, output-path validation. The Claude-Code-headless runtime selection happens inside the harness package but the v0 image only spawns the stub mode; Phase 3 swaps in the real Claude-backed image behind the same `Subagent` interface (REQ-HARN-06 satisfied by stub in Phase 2, by real image in Phase 3).
- `internal/credproxy` sidecar container image (HTTPS proxy + HMAC token validator) shipping alongside the controller.
- Plan admission webhook body wiring `pkg/dag.ComputeWaves` for cycle detection + file-touch ↔ `dependsOn` reconciliation; layered strict/warn mode flag (Plan annotation > Project.Spec > Helm default).
- Token-bucket rate limiter at the controller layer keyed by credential `Secret` UID; per-Project budget caps rolled up at Task completion into `Project.Status.budget`.
- One-shot `tide-init-{project-uid}` Job spawned by `ProjectReconciler` to bootstrap `/workspace/{repo,artifacts,envelopes}` layout on the per-Project RWX PVC (ART-01).
- Provider-firewall lint rule (SUB-05) extending `cmd/tide-lint` to a `multichecker` (Phase 1 prepared the flip): no `github.com/anthropics/*` or other LLM SDK imports under `pkg/controller/...`, `pkg/dispatch/...`, `pkg/dag/...`.

Phase 2 does NOT: clone the repo into `/workspace/repo` (Phase 3 `pkg/git`), push to git remotes (Phase 3), swap the stub for a real Claude-backed image (Phase 3 ART → REQ-HARN-06 second half), wire human gates (Phase 4 GATE-01..03), ship the dashboard or `tide` CLI (Phase 4 CLI-01..04, DASH-01..05), or run the chaos-resume test (Phase 3 TEST-04).

</domain>

<decisions>
## Implementation Decisions

### Envelope contract (the cross-process API)

- **D-A1:** `EnvelopeIn` and `EnvelopeOut` Go types live in `pkg/dispatch/envelope.go` — the **public** Go contract. Out-of-tree subagent image authors import `github.com/jsquirrelz/tide/pkg/dispatch` to decode envelopes; no vendoring, no copy-paste. This is what makes the spec's "pluggable Subagent runtime via a documented container image contract" actually pluggable. The Phase 1 placeholder at `internal/dispatch/doc.go` is moved (the `Dispatcher dispatch.Dispatcher` field on all six reconcilers stays at `internal/dispatch.Dispatcher` for the runtime interface; envelope types are at `pkg/dispatch`).
- **D-A2:** Envelopes are JSON files on the per-Project PVC at `/workspace/envelopes/{task-uid}/in.json` (orchestrator-written, mounted read-only on subagent) and `/workspace/envelopes/{task-uid}/out.json` (harness-written, controller reads after Job completion). No env vars, no ConfigMaps for envelope content — keeps envelope size unbounded and traceable via `kubectl exec`.
- **D-A3:** Every envelope JSON carries explicit `apiVersion: tideproject.k8s/v1alpha1` + `kind: TaskEnvelopeIn | TaskEnvelopeOut`. Harness rejects unknown `apiVersion` with a structured `cap-hit`-equivalent error. Future `v1beta1` envelopes ride the same hub/spoke conversion story the CRDs use (CRD-05 scaffold extends here).
- **D-A4:** Envelopes are **self-contained** — they carry everything the subagent needs to run (task-uid, role `planner|executor`, level `milestone|phase|plan|task`, prompt body, `filesTouched`, `dependsOn`, `declaredOutputPaths`, caps `{wallClockSeconds, iterations, inputTokens, outputTokens}`, signed-token endpoint URL + token). Subagent pod's `ServiceAccount` has **zero K8s verbs** (no `get tasks`, no `get configmaps`). This locks down Pitfall 7 (subagent context bleed) at the RBAC layer.

### Wave ↔ Task dispatch split

- **D-B1:** `TaskReconciler` is the **sole** Job creator. When it observes a `Task` whose `Spec.DependsOn` predecessors all carry `Status.Phase=Succeeded` (i.e., indegree==0), it creates `Job tide-task-{task-uid}-{attempt-n}` with `OwnerReferences` back to the Task and `BlockOwnerDeletion: true`.
- **D-B2:** `WaveReconciler` is **observational only**: on Plan ready it materializes `Wave` objects from `pkg/dag.ComputeWaves` (Phase 1 D-B1 commitment), watches owned `Task`s, and rolls up `Wave.Status.phase`. It does NOT create Jobs. This naturally encodes FAIL-02 (per-task indegree decrement, not per-wave) — a sibling Task completing triggers `TaskReconciler` for each of its dependents independently.
- **D-B3:** Indegree is **recomputed per reconcile** via sibling-Task query (same purity rule as PERSIST-03 wave schedules). `TaskReconciler` lists sibling Tasks in the same Plan via the owner-ref label index, counts `DependsOn` predecessors whose `Status.Phase != Succeeded`, dispatches if 0. No persisted indegree map anywhere. Restart-resumption is structural: no special-case code (Phase 3 PERSIST-04 chaos-resume inherits this property for free).
- **D-B4:** `Wave.Status.phase=Succeeded` iff every member Task is `Succeeded`. A failed Task transitions the Wave to `Failed` but does NOT block sibling Tasks in the same wave (strict-by-default per FAIL-01). The Wave object is purely descriptive of layer completion for dashboard/observability.
- **D-B5:** `Task.Status.Attempt` is owned by `TaskReconciler` and incremented at **Job-creation time** (not at Job-completion). On retry, the reconciler lists Jobs labeled with this Task's UID, finds max attempt, creates `attempt-(max+1)`. SUB-03 idempotency: a watch-lag duplicate create on the same `attempt-n` returns `AlreadyExists` — the deterministic Job name `tide-task-{task-uid}-{attempt-n}` IS the dedup key. Retry cap: `Project.Spec.maxAttemptsPerTask` (Helm default 3).

### Signed-token credential proxy (HARN-03)

- **D-C1:** Two-container Pod topology per Task: (a) sidecar `tide-credproxy` container running the HTTPS proxy with the real `ANTHROPIC_API_KEY` from `envFrom: [secretRef]`; (b) `subagent` container running Claude Code headless (stub in Phase 2, real in Phase 3) with `ANTHROPIC_BASE_URL=https://127.0.0.1:8443` and `ANTHROPIC_API_KEY=<signed-token>`. The subagent container's env, filesystem, and process tree never touch the real key.
- **D-C2:** Transport is **localhost HTTPS** with a self-signed cert minted at pod startup. The cert is generated by the sidecar's init logic, written to a shared `emptyDir` volume mounted as `/etc/tide/proxy/` on both containers; the subagent's CA bundle includes that cert. TLS protects the signed token in transit against any future malicious-sidecar-injection scenario. Compatible with Anthropic SDK / Claude Code's `ANTHROPIC_BASE_URL` override with zero subagent-side code changes.
- **D-C3:** Signed-token shape: **HMAC-SHA256** over `(nonce || taskUID || expiry)` with an installation-wide signing secret. Concretely:
  - At Job-create time, controller generates a random 32-byte nonce, computes `hmac.New(sha256, signingSecret).Sum(nonce || taskUID || expiry)`, base64-encodes `(nonce || expiry || mac)`, and places the token in `EnvelopeIn.signedToken`.
  - Proxy validates: `taskUID` matches its env-injected `TIDE_TASK_UID`, `expiry` not passed, MAC verifies. Token leak outside the pod → useless (token is bound to the specific Pod's `TIDE_TASK_UID`).
  - Signing secret comes from a single `Secret tide-signing-key` deployed by the Helm chart (auto-generated on first install if not provided); mounted on the controller AND on every Job pod's sidecar via `envFrom: [secretRef: {name: tide-signing-key}]`.
- **D-C4:** Raw `ANTHROPIC_API_KEY` delivery: `envFrom: [secretRef: {name: <Project.Spec.providerSecretRef>}]` on the **sidecar container only**. Subagent container has zero `secretRef`s. AUTH-02 namespace-per-project alignment: the provider Secret lives in the Project's namespace.

### Budget cap & rate-limit state durability

- **D-D1:** Token-bucket rate limiter lives **in-memory** in the controller process: `sync.Map[secretUID]*RateBucket`. On Manager restart, the bucket is rederived by listing active Jobs in the last bucket-window (configurable, default 60s) and pre-charging the bucket accordingly. Same PERSIST-01 purity rule applied: "CRD `.status` is cache, rederivable" — and the in-process map is the cache. No etcd writes for bucket state. Hot path stays controller-local.
- **D-D2:** Per-Project budget tally is **rolled up at Task completion** into `Project.Status.budget.{tokensSpent, costSpentCents, windowStart, absoluteCap, rollingWindowCap}`. On each Task completion, `TaskReconciler` reads `EnvelopeOut.usage.{inputTokens, outputTokens, estimatedCostCents}` and Patches the Project's Status. One write per Task completion (NOT per dispatch) — churn proportional to throughput. Halt is structural: `TaskReconciler` checks `Project.Status.budget` against `Project.Spec.budget.absoluteCap` before dispatching the next attempt; if exceeded, halts and writes `Project.Status.phase=BudgetExceeded`.
- **D-D3:** Bucket scope is **per credential Secret UID** — keyed by `Secret.metadata.uid`, not Project UID. Anthropic's rate limits are per-org-key; two Projects sharing the same Secret share a bucket. Per-Secret config sources, in precedence order: (1) Secret annotations `tideproject.k8s/requests-per-minute`, `tideproject.k8s/tokens-per-minute`; (2) `Project.Spec.providers[].requestsPerMinute` / `tokensPerMinute`; (3) Helm-chart defaults from Anthropic's documented tier values.
- **D-D4:** Budget-pause recovery in Phase 2 (before Phase 4 `tide` CLI exists): operator runs `kubectl annotate project foo tideproject.k8s/bypass-budget=true`; `ProjectReconciler` watches annotation changes and clears `Status.phase=BudgetExceeded`, allowing dispatch to resume. The annotation is consumed (annotator removes the bypass after one allowance OR sets a TTL via paired `tideproject.k8s/bypass-budget-until=<RFC3339>` annotation). Phase 4's `tide approve --bypass-budget` becomes a thin wrapper that flips the annotation.

### Plan admission webhook (PLAN-01, PLAN-02, PLAN-03)

- **D-E1:** Cycle detection — Phase 1's webhook scaffold (D-B3) gets its body in Phase 2. Webhook computes wave structure via `pkg/dag.ComputeWaves` over the Plan's owned Tasks (queried by owner-ref label selector at admission time), rejects with `AdmissionResponse.allowed=false` and a structured `Status.Details` listing the cycle nodes when a `CycleError` is returned. Cycle "recovery" is out of scope (PLAN-03 confirmed).
- **D-E2:** File-touch ↔ `dependsOn` reconciliation — derived edges from `filesTouched` (each Task's writes become inferred edges into Tasks that read those paths) are computed and compared against declared `dependsOn`. Mismatch types: (a) declared edge missing in derived (orchestrator-detected: LLM under-declared, suspicious — likely false-negative on file-touch heuristic); (b) derived edge missing in declared (LLM over-declared OR file-touch hallucination — Pitfall 19 territory). Both surfaced in the webhook response.
- **D-E3:** Strict/warn mode flag is **layered** (precedence Plan annotation > Project.Spec > Helm default):
  1. Plan annotation `tideproject.k8s/file-touch-mode=strict|warn` (per-Plan override).
  2. `Project.Spec.planAdmission.fileTouchMode=strict|warn` (per-Project override).
  3. Helm value `planAdmission.fileTouchMode` (cluster default; ships **`warn`** in v1 chart).
- **D-E4:** Mismatch response:
  - **Strict:** `AdmissionResponse.allowed=false` + structured `Status.Details` listing the mismatched `(taskName, declaredDependsOn, derivedFromFileTouches)` tuples.
  - **Warn:** `AdmissionResponse.allowed=true` + `AdmissionResponse.warnings: [...]` (kubectl surfaces these to the user).
  - **Both modes:** fire a K8s `Event` on the Plan for audit traceability (whether admit or reject).

### Stub-subagent (SUB-04 + TEST-02 ergonomics)

- **D-F1:** Stub-subagent learns its test behavior from an **envelope field** `EnvelopeIn.testMode` (enum `success | fail-exit-1 | hang | exceed-output-paths`). The field lives in a `// +optional dev-only` sub-struct of `EnvelopeIn` (e.g., `EnvelopeIn.Dev.TestMode`). Real Claude-backed image in Phase 3 ignores the field entirely. Tests author Task fixtures with `Task.Spec.dev.testMode=fail-exit-1` and `TaskReconciler` copies it into `EnvelopeIn.Dev.TestMode` at dispatch.
- **D-F2:** Stub-subagent location: `cmd/stub-subagent/main.go` builds to a small static Go binary; Dockerfile at `images/stub-subagent/Dockerfile` produces `ghcr.io/jsquirrelz/tide-stub-subagent:v0.1.0-dev` (and `:test` for local kind loads). CLI: `stub-subagent --envelope /workspace/envelopes/$TIDE_TASK_UID/in.json`. The stub **reuses `pkg/dispatch` envelope types** — proving the public-contract claim of D-A1.
- **D-F3:** Phase 2 canonical stub modes:
  - **success** — writes canned `result.json` + any declared `filesTouched` artifacts; exit 0.
  - **fail-exit-1** — writes structured failure result `{reason: "forced-failure"}`; exit 1. Drives FAIL-01 sibling-continues / dependents-never-dispatch assertions.
  - **hang** — sleeps past `EnvelopeIn.caps.wallClockSeconds`. Drives HARN-02 wall-clock-cap test (harness SIGTERMs subagent, exits with `cap-hit`).
  - **exceed-output-paths** — writes a file outside `EnvelopeIn.declaredOutputPaths`. Drives HARN-05 post-Job output-path-validation rejection test.
- **D-F4:** **Non-stub test layers** for capabilities not in the stub:
  - **HARN-04 secret-pattern redaction:** unit test under `internal/harness/redact/redact_test.go` against known-pattern fixtures (`sk-ant-*`, `sk-*`, `gh[ps]_*`, `xox[abp]-*`, AWS `AKIA*`, JWT `eyJ*`). No subagent invocation needed.
  - **FAIL-03 token-bucket rate-limit absorption:** unit test against a fake `http.RoundTripper` injected into `internal/credproxy` that returns synthetic 429s; assertions on `tide_provider_rate_limit_hits_total` counter increment and exponential-backoff timing.
  - **HARN-03 signed-token rejection:** unit test against the proxy's HMAC validator with tampered tokens (wrong taskUID, expired, bad MAC).

### PVC bootstrap (ART-01)

- **D-G1:** `ProjectReconciler` runs a one-shot `Job tide-init-{project-uid}` on first reconcile after the per-Project PVC binds. The init Job mounts the PVC and runs `mkdir -p /workspace/{repo,artifacts,envelopes} && chmod 0775 ...` (busybox `sh` is fine; no Go binary needed for v0). `Project.Status.phase=Initialized` set after the init Job's pod terminates with exit 0. Idempotent: re-applying the same Project does nothing (init Job's deterministic name `tide-init-{project-uid}` survives).
- **D-G2:** Per-Phase/Plan/Task artifact subdirs (e.g., `/workspace/artifacts/M-001/P-001/L-001/`) are created **lazily** by `TaskReconciler` at dispatch time — the reconciler stamps the Task's full level-path into `EnvelopeIn.declaredOutputPaths` and the harness `mkdir -p`s any missing ancestors before writing. Avoids pre-computing the whole tree at Project-init (the tree isn't known until plans are authored, which is Phase 3 territory).
- **D-G3:** UID model:
  - Init Job, all Task Jobs, and the controller Manager Pod set `securityContext.fsGroup: 1000` (the `tide` GID).
  - Orchestrator container runs as **`runAsUser: 65532`** (kubebuilder default nonroot).
  - Subagent harness container runs as **`runAsUser: 1000`**.
  - Sidecar `tide-credproxy` container runs as **`runAsUser: 1000`** (matches subagent for emptyDir cert sharing).
  - PVC files created by either UID are group-readable+writable via `fsGroup` rewriting.

### Integration test tier (TEST-02 <5 min budget)

- **D-H1:** **Two-layer split:**
  - **Layer A — envtest (~90s budget):** Reconciler internals + admission webhook bodies. Coverage: cycle detection rejection, file-touch reconciliation modes (strict/warn), `TaskReconciler` indegree recompute, deterministic Job naming + attempt-counter increment, owner-ref cascade across Project→Milestone→Phase→Plan→Task→Wave, Wave roll-up to `Status.phase=Succeeded|Failed`. No real kubelet; envtest apiserver only. Ginkgo `--label-filter=envtest`.
  - **Layer B — kind (~3min budget):** Real Job lifecycle with stub-subagent. Coverage: 3-task wave success path with all three completing (success-criterion #1), fail-injection wave with one Task `testMode=fail-exit-1` and sibling/dependent assertion (success-criterion #3 via FAIL-01), `testMode=hang` exercising HARN-02 wall-clock cap, `testMode=exceed-output-paths` exercising HARN-05 post-Job validation. One shared `kind` cluster spun up once per Make target via `setup-envtest` + pinned-by-SHA `kindest/node` image (per STACK.md). Ginkgo `--label-filter=kind`.
- **D-H2:** Make target layout:
  - `make test-int` — runs both layers serially under <5 min.
  - `make test-int-fast` — Layer A only (~90s).
  - CI matrix runs `make test-int`.
- **D-H3:** Stub-subagent image load: `kind load docker-image ghcr.io/jsquirrelz/tide-stub-subagent:test`. Make recipe builds the image first (BuildKit cache for <2s rebuild on no-change) then loads. No registry push, no creds required. Offline-capable.
- **D-H4:** **Local dev fallback (planner-noted, non-blocking):** developer has `minikube` configured as a kubectl context locally. Useful for manual smoke testing — e.g., `make manifests && kubectl --context minikube apply -k config/samples/` to eyeball CEL validation against a real cluster without spinning kind. Canonical CI test runtime stays **kind** per STACK.md (pinned-by-SHA node image for reproducibility); minikube is not added to the supported matrix in v1.

### Claude's Discretion

- **Wall-clock cap defense-in-depth.** Phase 2 enforces wall-clock caps at both layers: harness SIGTERMs the subagent process on internal timer expiry (HARN-02 explicit), AND the K8s `Job.spec.activeDeadlineSeconds` is set to `EnvelopeIn.caps.wallClockSeconds + 30s` (grace window for SIGTERM→SIGKILL escalation). Planner chooses exact grace constant.
- **429 backoff timing constants.** Initial delay 250ms, exponential factor 2.0, max delay 30s, max retries 5, jitter ±25% (full-jitter strategy from AWS Architecture blog). Planner may adjust within reason; document chosen values.
- **Secret-redaction regex set (HARN-04).** Start from the Pitfall 18 list (`sk-ant-*`, `sk-*`, `gh[ps]_*`, `xox[abp]-*`, AWS `AKIA[A-Z0-9]{16}`, JWT `eyJ[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+\.[A-Za-z0-9_-]+`). Compile once at harness startup; apply to stdout, stderr, `result.json`, and every artifact write before flush. Planner chooses library (stdlib `regexp` likely sufficient; `trufflehog` regex set is comprehensive but overkill for v1).
- **Output-path validation algorithm (HARN-05).** Use `filepath.Rel(declaredPath, actualWrite)` + reject any result starting with `../`. Symlink resolution: `filepath.EvalSymlinks` on `actualWrite` before comparison (defends against symlink-out-of-scope writes).
- **Plan admission webhook ordering vs Wave creation.** Webhook fires before etcd persists the Plan; `WaveReconciler` only creates Wave objects after the Plan is `Status.phase=Validated`. No race.
- **`internal/credproxy` Go module structure.** Likely a small `cmd/credproxy/main.go` + `internal/credproxy/*.go` library; image at `images/credproxy/Dockerfile`. Planner picks exact package layout.
- **Helm chart additions for Phase 2.** New values: `planAdmission.fileTouchMode`, `rateLimits.defaults.requestsPerMinute`, `rateLimits.defaults.tokensPerMinute`, `images.stubSubagent.repository`, `images.credProxy.repository`. CRD subchart unchanged (`Project.Spec` gains `providerSecretRef`, `providers[]`, `budget`, `planAdmission`, `maxAttemptsPerTask` — these are schema bumps to v1alpha1 inside the bridge BOOT-03 commits to).
- **`internal/dispatch.Dispatcher` interface body.** Phase 1's empty interface gets the real shape: `Run(ctx context.Context, in pkg.EnvelopeIn) (pkg.EnvelopeOut, error)`. The injected concrete impl in `internal/dispatch/podjob.PodJobBackend` creates the Job and blocks (via watch) until the Job terminates, reading the out-envelope from PVC.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing Phase 2.**

### Spec & paradigm
- `README.md` — TIDE paradigm spec: five-level hierarchy, two-DAG split, failure semantics at wave boundaries (§"Failure handling at wave boundaries"), resumption state contract
- `CLAUDE.md` — Project instructions: spec is load-bearing, vocabulary conventions, two parallelism budgets, full Technology Stack table (Go 1.26, controller-runtime v0.24.x, Anthropic Go SDK v1.42.x pinning rule, OpenTelemetry train rules, kind v0.31 SHA-pinning)

### Project frame
- `.planning/PROJECT.md` — Vision, 13 locked Key Decisions; Phase 1 entries marked ✓ Validated
- `.planning/REQUIREMENTS.md` — 21 Phase 2 REQ-IDs: SUB-01..05, HARN-01..06, PLAN-01..03, FAIL-01..04, PERSIST-03, ART-01, TEST-02. Traceability table maps every REQ to its phase
- `.planning/ROADMAP.md` §"Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness" — Five success criteria; research-flag noted
- `.planning/STATE.md` — Current cursor at Phase 2; 11/11 Phase 1 plans complete

### Phase 1 carry-forward (decisions that constrain Phase 2)
- `.planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-CONTEXT.md` — D-A3 (API group `tideproject.k8s`), D-B1..B3 (Wave-as-Kind ownership, Spec carries only `planRef + waveIndex`, cycle detection lives in Plan admission webhook), D-C1..C2 (Standard reconciler stub depth, no `time.Sleep` in `Reconcile()`), D-F1..F2 (`Task.Spec.DependsOn` sibling-name strings; `Task.Spec.FilesTouched` required non-empty), D-D1 (POOL-03 analyzer + CI gate; `cmd/tide-lint` ready to flip to `multichecker` for SUB-05)
- `.planning/phases/01-foundation-crds-pkg-dag-controller-scaffold/01-RESEARCH.md` — Phase 1's research synthesis; reusable for Phase 2's controller-side patterns
- `internal/dispatch/doc.go` — Phase 1 reserved-path placeholder; doc-comment outlines the Phase 2 interface shape that landed in this CONTEXT
- `api/v1alpha1/task_types.go` — `TaskSpec` locked: `PlanRef`, `DependsOn []string`, `FilesTouched []string` (MinItems=1), `PromptRef`; `TaskStatus` locked: `Phase`, `Conditions`, `Attempt`, `ExitCode`, `CompletedAt`

### Research synthesis (Phase 2 is the security/correctness fanout)
- `.planning/research/PITFALLS.md` §"Pitfall 7" (subagent context bleed → harness output-path validation = HARN-05), §"Pitfall 8" (runaway agent loops → harness budget caps = HARN-02), §"Pitfall 9" (429 rate-limit cascade → token-bucket + Job-create gating = FAIL-03), §"Pitfall 10" (indegree on partial failure → per-task decrement = FAIL-02), §"Pitfall 11" (watch-lag duplicate dispatch → deterministic Job names = SUB-03), §"Pitfall 14" (vendor lock-in creep → provider-firewall lint = SUB-05), §"Pitfall 18" (secret leakage → signed-token proxy + redaction = HARN-03/HARN-04), §"Pitfall 19" (hallucinated `dependsOn` → file-touch reconciliation = PLAN-02), §"Pitfall 20" (LLM cost runaway in tests → stub-subagent = SUB-04)
- `.planning/research/ARCHITECTURE.md` — Component responsibilities; relevant patterns: Pattern 1 (one reconciler per Kind), Pattern 2 (owner-ref cascade — applies to Job→Task→Plan→...→Project), Pattern 3 (two-DAG one-algorithm — Phase 2 wires Execution DAG side)
- `.planning/research/STACK.md` — Anthropic SDK v1.42.x pin (weekly rev-bumps mean don't auto-bump), `kind` SHA-pinning rule, `go-chi` v5 as `manager.Runnable` (for any HTTP endpoints in Phase 2, e.g., admission webhook + future Phase 4 metrics scraping)
- `.planning/research/FEATURES.md` — Phase 2-relevant feature classifications

### External references (read on demand; don't pre-load)
- Kubebuilder Book — https://book.kubebuilder.io — Webhooks (admission body wiring), Finalizers (already-applied from Phase 1)
- controller-runtime v0.24 docs — `client.Patch` for Status sub-resource updates (used by Project budget tally)
- Anthropic API reference — https://docs.anthropic.com/api/rate-limits — tier rate-limit defaults (informs Helm chart default values for `rateLimits.defaults.{requestsPerMinute, tokensPerMinute}`)
- Claude Code CLI reference (≥ v2.1.139 per STACK.md) — headless mode flags (Phase 3 swap-in territory but the harness shape must accommodate the flag set today)
- `golang.org/x/tools/go/analysis` — `multichecker` entry point (cmd/tide-lint flip target)

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets

- **`pkg/dag.ComputeWaves`** — `nodes, edges → [][]string` + `CycleError` already shipping (DAG-04 fixture pinned). Plan admission webhook consumes it directly for cycle detection (D-E1). No new graph code needed.
- **`internal/dispatch/doc.go`** — Phase 1 placeholder package + `Dispatcher interface{}` stub. Phase 2 replaces with real interface; the `Dispatcher dispatch.Dispatcher` field already on all six reconciler structs (`internal/controller/{project,milestone,phase,plan,wave,task}_controller.go:54`) so wiring is a one-line struct-field-init in `cmd/manager/main.go`.
- **`internal/finalizer/`, `internal/owner/`, `internal/pool/`, `internal/config/`** — Phase 1 helper packages. Finalizer cleanup hooks need extending in `TaskReconciler` to also `DELETE` orphan Jobs on Task deletion. Owner-ref helpers reused for `Job → Task` ownership wiring.
- **`cmd/tide-lint/`** — Phase 1's `singlechecker.Main(crosspool.Analyzer)` setup, designed to flip to `multichecker.Main(crosspool.Analyzer, providerfirewall.Analyzer)` for SUB-05 with zero changes outside `main.go` (D-D1 note in 01-CONTEXT). New analyzer package: `tools/analyzers/providerfirewall/`.
- **`config/samples/`** — α…θ Task fixtures from Phase 1 (D-G1). Phase 2's Layer B kind tests reuse the same Task names and graph shape — a 3-task subset (α, β, ε with edge α→β, β→ε) makes a tight wave-of-three FAIL-01 test fixture.
- **Helm chart pair (`charts/tide/`, `charts/tide-crds/`)** — helmify-generated and idempotently regenerable from `config/`. Phase 2 schema additions to `Project.Spec` (D-E3, D-D3) flow through controller-gen → kustomize → helmify on `make helm`. The augment layer at `hack/helm/` is the seam for new template values (`planAdmission.fileTouchMode`, `rateLimits.defaults.*`, image refs).

### Established Patterns

- **Standard reconciler stub depth** (Phase 1 D-C1). Phase 2 inherits this baseline: every new reconciler responsibility wires through `Owns()`/`Watches()` event-driven; no `time.Sleep`, no blocking I/O inside `Reconcile()`. The custom analyzer (POOL-03) catches one class of regression at CI time.
- **Vocabulary discipline** (CLAUDE.md). New names in Phase 2 follow the tide metaphor where possible: the credential proxy is `tide-credproxy` (sidecar) not `auth-broker`; the init Job is `tide-init` not `bootstrap-job`. Stub subagent is `tide-stub-subagent` (slightly long but accepts the OSS-style "tide-" image prefix). Where the metaphor doesn't fit, prefer plain prose over coined terms.
- **Per-Kind RBAC marker discipline** (Phase 1 D-D1 + 01-09-PLAN gate). Every new kubebuilder RBAC marker introduced in Phase 2 (e.g., `// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete`) must be per-Kind, no wildcards. The Plan 09 `verify-rbac-marker-discipline` Make target catches violations.
- **Conditions vocabulary** (Phase 1 D-C1 shared types): `Pending`, `Ready`, `Reconciling`, `Failed`. Phase 2 adds `BudgetExceeded` to the Project condition set; `Validated` to the Plan condition set; reuses `Running`, `Succeeded`, `Failed` for Task/Wave.
- **α…θ thread-through** (Phase 1 specifics). Integration tests in Layer B reuse α…θ Task names where useful — keeps the spec, unit test, sample CRDs, and integration tests all telling the same worked-example story.

### Integration Points

- **Plan admission webhook** — Phase 1 scaffolded the validating-admission-webhook endpoint as a no-op `Allow` always (CRD-04). Phase 2 fills the body: cycle detection (PLAN-01), file-touch reconciliation (PLAN-02), structured rejection (D-E1, D-E2, D-E4). Endpoint URL path unchanged.
- **`internal/controller/*_controller.go` Reconcile bodies** — Phase 2 adds dispatch logic to `TaskReconciler.Reconcile` (D-B1) and Wave roll-up logic to `WaveReconciler.Reconcile` (D-B2, D-B4). `ProjectReconciler.Reconcile` gains the init Job creation (D-G1) and the budget cap check + halt logic (D-D2). `PhaseReconciler` and `MilestoneReconciler` remain Standard-depth stubs until Phase 3 wires planner-subagent dispatch.
- **`cmd/manager/main.go`** — Phase 1's main wires the Manager with leader election + six reconcilers + two pools. Phase 2 adds: signing-secret bootstrap (D-C3), credential-proxy image config injection, stub-subagent image config injection, rate-limit defaults wiring, `Dispatcher dispatch.Dispatcher` field assignment on the six Reconciler structs.
- **Helm values surface** — new keys land in `values.yaml`: `planAdmission.fileTouchMode`, `rateLimits.defaults.*`, `images.stubSubagent.{repository,tag}`, `images.credProxy.{repository,tag}`, `signingKey.secretName`. These flow through helmify regen.

</code_context>

<specifics>
## Specific Ideas

- **Envelope as the public contract is load-bearing.** The user's explicit pick of `pkg/dispatch` (public) over `internal/dispatch` (private) for the envelope types is the structural commitment to "pluggable Subagent runtime via a documented container image contract." Once the planner authors PLAN.md, the very first plan should be the envelope-types-public-API plan, because every other plan in Phase 2 (and every subagent image Phase 3+) depends on its shape. Naming, field ordering, and JSON tag stability matter immediately.
- **Stub-subagent imports `pkg/dispatch` from day one.** The user picked the Go-binary stub over a shell script specifically to prove the public-contract claim. The stub's `go.mod` says `require github.com/jsquirrelz/tide v0.1.0-dev` and decodes envelopes with the same types the controller serializes. If a future planner is tempted to "just regenerate the envelope types in the stub's tree" — that's the failure mode this decision exists to prevent.
- **In-memory rate-limit bucket + Project.Status budget tally is the two-cache split.** Both pieces of state live in different homes for a reason: rate-limit state needs sub-second update granularity and would crush etcd via Status churn; budget tally needs durability across restarts and survives one Status write per Task completion (low). Don't unify them into one home — they have different access patterns.
- **The `tideproject.k8s/bypass-budget` annotation is a Phase 2 / Phase 4 contract.** Phase 2 implements the watch-and-clear semantic; Phase 4's `tide approve --bypass-budget` becomes a thin wrapper. Document the annotation key in Phase 2's CRD docs so external automation can use it before the CLI ships.
- **Layer A / Layer B split tracks the dev velocity story.** `make test-int-fast` (Layer A only) is what a developer runs while iterating on webhook code — sub-90-second feedback. CI runs `make test-int` (both layers). This mirrors the Phase 1 `make test` / `make test-only` split that landed in Plan 11 — same ergonomics pattern.
- **Minikube is the developer's local exploration tool, not a supported test runtime.** User volunteered that minikube is configured locally; kind stays the canonical runtime per STACK.md. Honor the user's tool ecosystem without bending the project's commitments — that's why minikube is noted in `code_context` (developer ergonomics) but kind is locked in `decisions` (D-H1, D-H3) and `canonical_refs` (STACK.md).

</specifics>

<deferred>
## Deferred Ideas

- **Real Claude-Code-backed subagent image** — Phase 3 (REQ-HARN-06 second half). Phase 2 ships the harness with the stub mode; Phase 3 swaps the executable inside the harness for `claude -p ... --output-format stream-json` against the same Subagent interface.
- **`pkg/git` integration** — Phase 3 (REQ-ART-02..07). Phase 2 leaves `/workspace/repo` empty; only `/workspace/{artifacts,envelopes}` is exercised.
- **Chaos-resume integration test (TEST-04)** — Phase 3 (PERSIST-04). Phase 2's two-layer test tier is positioned to extend naturally — chaos-resume is a Layer B kind test that adds `kubectl delete pod tide-controller-...` mid-wave.
- **Per-level human gate policy** — Phase 4 (GATE-01..03). Phase 2's budget-bypass annotation is the only "halt-and-resume" mechanism shipping in Phase 2; per-level gates layer on top in Phase 4.
- **`tide approve --bypass-budget` CLI verb** — Phase 4 (CLI-02). Phase 2 implements the underlying annotation-driven contract.
- **OpenTelemetry tracing across the dispatch chain** — Phase 4 (OBS-03..05). Phase 2 stays on structured logs only; Phase 4 wires `pkg/otelai` with OpenInference attributes onto the Task → Wave → Plan span chain Phase 2 establishes.
- **Conservative wave-failure profile as a per-Project setting** — explicit v1.x deferral (REQUIREMENTS.md "Deferred"). Phase 2 ships strict-by-default; conservative profile is a future Project.Spec field with no Phase 2 code path.
- **PR creation / auto-CI-fix automation per git host** — v2+ (REQUIREMENTS.md "Deferred").
- **gRPC streaming subagent contract** — v2+ (REQUIREMENTS.md "Deferred"). Phase 2's PodJobBackend is the v1 concrete impl; streaming is additive behind the same Subagent interface.

</deferred>

---

*Phase: 02-dispatch-plan-validation-innermost-reconcilers-harness*
*Context gathered: 2026-05-12*
