# Phase 2: Dispatch & Plan Validation — Innermost Reconcilers + Harness - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-05-12
**Phase:** 02-dispatch-plan-validation-innermost-reconcilers-harness
**Areas discussed:** Envelope contract & location, Wave↔Task dispatch split, Signed-token proxy design, Budget/rate-limit state durability, Plan admission strict-mode flag, Stub-subagent test-mode injection, PVC bootstrap & ownership, Integration test tier topology

---

## Envelope contract & location

### Q1: Where do EnvelopeIn / EnvelopeOut Go types live?

| Option | Description | Selected |
|--------|-------------|----------|
| pkg/dispatch (public) | Public Go contract; out-of-tree subagent images import github.com/jsquirrelz/tide/pkg/dispatch. Aligns with "pluggable Subagent runtime via documented container image contract." Cost: any field rename is a public-API break. | ✓ |
| internal/dispatch (private) | Subagent images outside this repo would need to vendor a copy. Works against "never hard-coded to one provider." | |
| api/v1alpha1/envelope_types.go | Treat envelope as CRD-adjacent type with conversion-webhook support. Heaviest contract; couples envelope evolution to CRD-conversion machinery v1 doesn't otherwise use. | |

### Q2: Wire shape

| Option | Description | Selected |
|--------|-------------|----------|
| JSON files on PVC | /workspace/envelopes/{task-uid}/{in,out}.json. No env-var size limits, traceable in kubectl exec. Aligns with ART-01 layout. | ✓ |
| Env vars + JSON file out | Small envelope fields as env vars; bulk via JSON. Env-var hostile to bigger payloads. | |
| ConfigMap mount + JSON out | Per-Task ConfigMap. ConfigMap 1MiB limit, churn on retry, etcd write per dispatch. | |

### Q3: Versioning

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit apiVersion field | apiVersion: tideproject.k8s/v1alpha1 + kind: TaskEnvelopeIn|TaskEnvelopeOut in every envelope. Future v1beta1 hardens without breaking v1alpha1 images. | ✓ |
| Go-struct-with-tags only | No version metadata in wire format. Lockstep release of orchestrator + subagent. | |
| Semver string in JSON | envelopeSchema: "1.0.0". Doesn't map to CRD-versioning vocabulary used elsewhere. | |

### Q4: Envelope contents — self-contained vs in-cluster lookup

| Option | Description | Selected |
|--------|-------------|----------|
| Self-contained envelope | Everything subagent needs in EnvelopeIn; no K8s API access. Subagent SA gets zero verbs. Locks down Pitfall 7 at RBAC layer. | ✓ |
| Thin envelope + harness K8s reads | EnvelopeIn = {task-uid, namespace}. Harness uses in-pod SA to read Task CRD. Every subagent pod needs `get tasks` RBAC. | |
| Envelope + sealed PromptRef | PromptRef ConfigMap mounted directly. Separates secret-ish prompt content. | |

**User's choice:** All four recommended options.
**Notes:** Public Go contract is the structural commitment to plugability. Confirmed declined option of moving to api/v1alpha1 — conversion-webhook coupling not worth the cost.

---

## Wave↔Task dispatch split

### Q1: Job creator

| Option | Description | Selected |
|--------|-------------|----------|
| TaskReconciler dispatches | TaskReconciler creates Job tide-task-{task-uid}-{attempt-n} when sibling dependsOn predecessors all Succeeded. WaveReconciler observational. Naturally encodes FAIL-02 per-task indegree. | ✓ |
| WaveReconciler dispatches | Wave is the unit of dispatch; TaskReconciler reflects Job status. Risks per-wave thinking and Pitfall 10 regression. | |

### Q2: Indegree location

| Option | Description | Selected |
|--------|-------------|----------|
| Recomputed per reconcile | Sibling-Task query per reconcile. No persisted indegree. Matches PERSIST-03 purity rule. Zero special-case code for chaos-resume. | ✓ |
| In-memory map in Manager | sync.Map cache. Lost on restart, must rebuild. Adds reconciler state outside CRDs. | |
| Wave.Status.indegreeMap | Forbidden by PERSIST-02 + D-B2. Would re-introduce Pitfall 2 (cached schedule). | |

### Q3: Wave done rule

| Option | Description | Selected |
|--------|-------------|----------|
| All member Tasks Succeeded | Failed Task → Wave.Status.phase=Failed; siblings continue per FAIL-01. Wave is descriptive only. | ✓ |
| All Succeeded OR dependents Skipped | Stricter; requires Skipped phase concept TaskStatus doesn't have. | |

### Q4: Attempt counter

| Option | Description | Selected |
|--------|-------------|----------|
| TaskReconciler at Job-creation | Reconciler lists existing Jobs, finds max attempt, creates attempt-(max+1). Deterministic Job name = dedup key for SUB-03. | ✓ |
| Wave or top-level coordinator | Cross-reconciler state mutation. Fights one-reconciler-per-Kind pattern. | |
| Job's controller-uid via labels | No Task.Status.Attempt field. Loses human-readable retry count. | |

**User's choice:** All four recommended options.
**Notes:** TaskReconciler-as-dispatcher with per-reconcile indegree recompute is the cleanest carry-through of the spec's "no cached schedule" / "indegree map + completed-task set is the resumption state" principles.

---

## Signed-token credential proxy design

### Q1: Process topology

| Option | Description | Selected |
|--------|-------------|----------|
| Sidecar container | tide-credproxy sidecar holds real key (envFrom). Subagent container has signed token + ANTHROPIC_BASE_URL. Cleanest separation. | ✓ |
| Single container, harness forks subagent | One container, harness is PID 1 with real key. Brief env exposure before unsetenv + fork. Weaker defense-in-depth. | |

### Q2: Transport

| Option | Description | Selected |
|--------|-------------|----------|
| Localhost HTTPS with self-signed cert | Proxy listens 127.0.0.1:8443; subagent's CA bundle includes minted cert. Zero subagent code change for Anthropic SDK ANTHROPIC_BASE_URL pattern. | ✓ |
| Localhost plain HTTP | Acceptable on loopback but lint surface for "no plain-HTTP envelope" is fragile. | |
| Unix domain socket | Strongest isolation but Claude Code / Anthropic SDK don't speak UDS natively. | |

### Q3: Token mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| HMAC-signed envelope-bound nonce | HMAC-SHA256 over (nonce || taskUID || expiry) with installation-wide signing secret. Leak useless outside taskUID + expiry window. | ✓ |
| Random opaque nonce shared via PVC | No HMAC. Leak valid for Job lifetime. | |
| PASETO v4.local | Stronger crypto story but adds non-stdlib dep at localhost-trust-boundary scope. | |

### Q4: Key delivery

| Option | Description | Selected |
|--------|-------------|----------|
| EnvFrom Secret on sidecar only | Subagent container has zero secret refs. Zero env-var leak. Supports SUB-05 lint. | ✓ |
| Projected SA token + workload identity | Assumes Vault/ESO/STS infra PROJECT.md defers. | |
| CSI Secret Store driver mount | Adds CSI dep; not aligned with "plain K8s Secrets only for v1." | |

**User's choice:** All four recommended options.
**Notes:** Two-container Pod topology + HMAC-bound token is the strongest layering. Sidecar runs the proxy with the real key; subagent never sees raw key in env, fs, or process tree.

---

## Budget/rate-limit state durability

### Q1: Rate-bucket location

| Option | Description | Selected |
|--------|-------------|----------|
| In-memory; rederive on restart | sync.Map keyed by credential Secret UID; recharged from active Jobs in bucket window. No etcd writes. Matches PERSIST-01 cache rule. | ✓ |
| Per-Project CRD Status field | Status update per dispatch. Etcd churn; watch storm risk. | |
| ConfigMap per Project | Same churn problem; weaker watch story. | |

### Q2: Budget tally location

| Option | Description | Selected |
|--------|-------------|----------|
| Project.Status.budget at Task completion | One Status update per Task completion (NOT per dispatch). Churn proportional to throughput. | ✓ |
| In-memory + periodic snapshot | Sub-window loss on restart leaks budget. Risky for absolute caps. | |
| Audit log to separate ConfigMap | Strong audit story but ConfigMap 1MiB limit kicks in at scale. | |

### Q3: Bucket scope

| Option | Description | Selected |
|--------|-------------|----------|
| Credential Secret UID | Bucket key = Secret.metadata.uid. Two Projects sharing same Secret share a bucket. Matches Anthropic's per-org-key tier model. | ✓ |
| Per-Project keyed by Project UID | Two Projects with same real key would over-dispatch and trip upstream limit. Fights Pitfall 9. | |
| Per-installation single global bucket | Anti-tenancy; busy Project blocks quiet one. | |

### Q4: Bypass UX

| Option | Description | Selected |
|--------|-------------|----------|
| Annotation toggle | kubectl annotate project foo tideproject.k8s/bypass-budget=true. Phase 4 CLI wraps this. | ✓ |
| Spec field toggle | Project.Spec.budget.bypassUntil=<RFC3339>. Spec churn risk. | |
| Defer entirely to Phase 4 | Tests can verify halt but not recovery. | |

**User's choice:** All four recommended options.
**Notes:** Two-cache split — in-memory bucket for sub-second granularity, Status field for durability — captures the different access patterns of rate-limit and budget state. Annotation-based bypass means the Phase 4 CLI is a thin wrapper, not a re-implementation.

---

## Plan admission strict-mode flag

### Q1: Flag location

| Option | Description | Selected |
|--------|-------------|----------|
| Layered: Helm → Project → Plan | Plan annotation > Project.Spec > Helm default. Resolver helper. | ✓ |
| Two layers: Helm → Project | Drops per-Plan escape hatch. | |
| Project.Spec field only | Every Project author re-decides; no install-wide posture. | |

**User's choice:** Layered after seeing concrete examples per layer.
**Notes:** User asked for examples of when warn vs strict would be used at each layer — provided OSS-dev/CI vs production for Helm; lab vs customer-facing for Project; LLM-was-right vs high-stakes-Plan for annotation. With examples in hand, user confirmed layered approach.

### Q2: Mismatch response

| Option | Description | Selected |
|--------|-------------|----------|
| Strict=reject + structured error; Warn=admit + warnings header | Both modes fire K8s Event on Plan. Caller-visible without scraping logs. | ✓ |
| Both modes admit; emit Plan condition only | Loses "reject at admission time" property REQ-PLAN-02 explicitly calls for. | |

---

## Stub-subagent test-mode injection

### Q1: Injection mechanism

| Option | Description | Selected |
|--------|-------------|----------|
| Envelope field | EnvelopeIn.Dev.TestMode enum. Real Claude image ignores. Stays inside the envelope contract. | ✓ |
| Task annotation | Orchestrator translates annotation → envelope field. Adds glue. | |
| ConfigMap mounted | Heaviest; extra ConfigMap per test Task. | |

### Q2: Image location + CLI

| Option | Description | Selected |
|--------|-------------|----------|
| cmd/stub-subagent/ Go binary | Reuses pkg/dispatch types — proves public-contract claim. ~200 lines of Go. | ✓ |
| Shell script in busybox | Loses the pkg/dispatch reuse demonstration. | |
| Reuse real harness with --stub flag | Couples stub timing to real harness build. Blurs SUB-04 "shipped alongside" wording. | |

### Q3: Canonical modes (multiSelect)

| Option | Description | Selected |
|--------|-------------|----------|
| success | Canned result.json + declared artifacts; exit 0. | ✓ |
| fail-exit-1 | Forced failure result; exit 1. Drives FAIL-01 assertions. | ✓ |
| hang | Sleeps past wallClockSeconds. Drives HARN-02. | ✓ |
| exceed-output-paths | Writes outside declaredOutputPaths. Drives HARN-05. | ✓ |

**User's choice:** Envelope field, Go binary, four modes (success, fail-exit-1, hang, exceed-output-paths).
**Notes:** Stub stays narrow — HARN-04 secret-redaction and FAIL-03 rate-limit are testable in their own layers without stub modes (unit test on redactor; fake HTTP transport on rate-limited client). Captured in CONTEXT D-F4.

---

## PVC bootstrap & ownership

### Q1: Layout creator

| Option | Description | Selected |
|--------|-------------|----------|
| ProjectReconciler one-shot init Job | tide-init-{project-uid} Job. Idempotent on Project recreation. Per-Task subdirs lazy via TaskReconciler. | ✓ |
| Every Task Job has initContainer | ~1s startup latency per Task + race risk on shared artifact parents. | |
| Subagent harness creates on startup | Moves PVC schema concerns into harness; vendor must replicate discipline. | |

### Q2: UID model

| Option | Description | Selected |
|--------|-------------|----------|
| Shared fsGroup, distinct runAsUser | fsGroup=1000 (tide GID); orchestrator UID 65532; subagent UID 1000. Standard K8s pattern. | ✓ |
| Same UID for orchestrator + subagent | Weaker process-isolation story. | |
| World-writable PVC (0777) | Security-hostile. Violates least-privilege. | |

**User's choice:** Both recommended.
**Notes:** Per-Phase/Plan/Task artifact subdirs (e.g., /workspace/artifacts/M-N/P-N/L-N) are created lazily by TaskReconciler at dispatch — the tree isn't known until plans are authored, which is Phase 3 territory.

---

## Integration test tier topology

### Q1: Structure

| Option | Description | Selected |
|--------|-------------|----------|
| Two-layer split | Layer A envtest (~90s) for reconciler internals + webhook bodies; Layer B kind (~3min) for real Job lifecycle. Ginkgo --label-filter separates. | ✓ |
| Single tier, all on kind | Strong coverage but 5-min budget eaten by kind startup + Job-startup latency. | |
| envtest-only with faked Job watch | Loses real K8s Job interactions. Hostile to TEST-04 chaos-resume in Phase 3. | |

### Q2: Image load

| Option | Description | Selected |
|--------|-------------|----------|
| Build locally + kind load docker-image | No registry push needed. Offline-capable. BuildKit cache for <2s rebuild. | ✓ |
| Push to GHCR before each test run | Network dep + creds per test invocation. | |
| Inline registry alongside kind | Overbuilt for Phase 2's two images. | |

**User's choice:** Two-layer split + kind load docker-image.
**Notes:** User volunteered that minikube is configured locally as a kubectl context "in case that's helpful." Captured as developer-ergonomics fallback for manual smoke testing (e.g., `kubectl --context minikube apply -k config/samples/`); kind remains the canonical CI test runtime per STACK.md (pinned-by-SHA node image for reproducibility). Recorded in D-H4 / canonical_refs.

---

## Claude's Discretion

- Wall-clock cap defense-in-depth (Job.activeDeadlineSeconds = caps.wallClockSeconds + 30s grace).
- 429 backoff timing constants (250ms initial, ×2 factor, 30s max, 5 retries, ±25% jitter — full-jitter strategy).
- Secret-redaction regex set (HARN-04) — start from Pitfall 18 list; library choice (stdlib regexp likely sufficient).
- Output-path validation algorithm (HARN-05) — filepath.Rel + reject `../` prefix; EvalSymlinks before compare.
- Plan admission webhook ordering vs Wave creation — webhook fires before etcd persist; WaveReconciler only creates Wave objects after Plan.Status.phase=Validated.
- internal/credproxy Go module structure — cmd/credproxy + internal/credproxy + images/credproxy/Dockerfile.
- Helm chart additions — planAdmission.fileTouchMode, rateLimits.defaults.*, images.stubSubagent.*, images.credProxy.*, signingKey.secretName.
- internal/dispatch.Dispatcher interface body — Run(ctx, pkg.EnvelopeIn) (pkg.EnvelopeOut, error); concrete PodJobBackend creates Job and blocks via watch until termination.

## Deferred Ideas

- Real Claude-Code-backed subagent image — Phase 3 (REQ-HARN-06 second half).
- pkg/git integration — Phase 3 (REQ-ART-02..07).
- Chaos-resume integration test — Phase 3 (PERSIST-04).
- Per-level human gates — Phase 4 (GATE-01..03).
- `tide approve --bypass-budget` CLI — Phase 4 (CLI-02); Phase 2 implements underlying annotation contract.
- OpenTelemetry / OpenInference tracing — Phase 4 (OBS-03..05).
- Conservative wave-failure profile as per-Project setting — v1.x.
- gRPC streaming subagent contract — v2+.
- PR creation / auto-CI-fix automation — v2+.
