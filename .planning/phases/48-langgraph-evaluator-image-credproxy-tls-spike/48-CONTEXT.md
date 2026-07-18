# Phase 48: LangGraph Evaluator Image + Credproxy-TLS Spike - Context

**Gathered:** 2026-07-18
**Status:** Ready for planning

<domain>
## Phase Boundary

Deliver a **minimal read-only Python/LangGraph "evaluator" container** that:
1. dispatches through the **unchanged** `pkg/dispatch.Subagent` + file-envelope seam (same seam the Claude-CLI subagent uses),
2. proves credproxy TLS trust **live** through the real `ChatAnthropic` construction path,
3. is **structurally** read-only (proven adversarially — mount/credential layer, not prompt refusal), and
4. patch-exact pins every Python dependency behind a CI gate.

**Scope = EVAL-01 + EVAL-02 only.** This is the de-risking beachhead of v1.0.9 Slack Tide and the first rung of the LangGraph successor-runtime ladder — prove the trust seam *before* any evaluation/verdict logic is built on it.

**Explicitly NOT in this phase (later phases own these):**
- The `gate_decision` verdict schema (`APPROVED | REPAIRABLE | BLOCKED` + `findings[]`), Go+Pydantic pair, fail-closed handling → **Phase 49** (EVAL-03).
- `VerifyContext` pointer field on `EnvelopeIn`; `LoopPolicy`/`LoopStatus` shared types → **Phase 49** (LOOP-01/02/03).
- Orchestrator-side `role="verifier"` Go prompt templates + coverage-not-conservatism split → **Phase 51** (EVAL-04).
- `TaskReconciler` dispatch integration, concurrency-gate accounting, `SelfInstruments` "langgraph" vendor registration, `EVALUATOR`-kind span emission → **Phase 51** (TASK-*, ESC-04, OBS-03).
- Findings size×locality persistence (`TerminationStub` summary + run-branch artifact) → **Phase 49** (EVAL-05).

The Phase-48 image emits a **trivial** `EnvelopeOut` (exitCode 0, one-line result, no findings) — it exercises the output *path*, not the verdict *schema*.
</domain>

<decisions>
## Implementation Decisions

### Image scope & shape
- **D-01 (Scope depth):** Bare **seam-conformance shell**. Happy path = decode `in.json` → git-**read** the worktree + run **one** read-only bash gate command → make **one** `ChatAnthropic` call (the TLS proof) → emit a trivial `EnvelopeOut` (exitCode 0, one-line `result`, no findings, no `Git`/`ChildCRDs`). No `gate_decision` schema — Phase 49 formalizes the output. Rejected: emitting a placeholder `gate_decision:"APPROVED"` (would bake a schema Phase 49 reworks and blur the EVAL-01/02-only boundary).
- **D-02 (LangGraph proof):** The image must genuinely exercise the **LangGraph runtime** (this is the beachhead), even though the graph does trivial work — leaning `create_react_agent` (langgraph-prebuilt, already transitive per STACK.md §5) with two hand-authored `@tool` functions: git-read and a `subprocess` bash gate-command tool. No `deepagents`/file-edit tooling; **no checkpointer**. Exact graph internals = researcher/planner discretion.
- **D-03 (Envelope transport — CORRECTS a common assumption):** The envelope is exchanged as **files on the per-Project PVC**, *not* stdin/stdout — read `in.json` from `/workspace/envelopes/<task-uid>/in.json`, write `out.json` next to it + a ≤4 KB `TerminationStub` to `/dev/termination-log` (`$TIDE_TERMINATION_MESSAGE_PATH`). The Python image **cannot import** the Go `pkg/dispatch` package (import-firewalled to Go), so it **re-implements the JSON envelope shape independently** and MUST validate `apiVersion == "dispatch.tideproject.k8s/v1alpha1"` + `kind == "TaskEnvelopeIn"` with **strict equality** as the first step (mirrors `ValidateAPIVersionKind`; a skewed version makes the image reject every envelope its same-release manager writes).
- **D-04 (Vendor sentinel):** Phase-48 image presents `provider.vendor = "anthropic"` (it wraps `ChatAnthropic` and does not self-instrument yet, so `SelfInstruments("anthropic")` stays `false` and the reporter's `events.jsonl` synthesis path is unaffected). The "does the LangGraph runtime need a NEW `"langgraph"` sentinel vs. reuse `"anthropic"` with a discriminator" question is **deferred to Phase 51** (OBS-03 / the Phase-51 research flag).
- **D-05 (Naming):** `cmd/tide-langgraph-verifier/` (or a sibling image build dir) + image family `ghcr.io/jsquirrelz/tide-langgraph-verifier`. Deliberately distinct from the pre-existing `internal/eval/` + `cmd/tide-eval/` (a *different*, deterministic prompt-quality/cost gate — name-collision hazard).

### Credproxy TLS trust spike (the centerpiece — genuine unknown)
- **D-06 (Spike harness & upstream):** **Standalone container spike** (mirrors the `cmd/tide-spike` precedent), *not* a full kind PodJob — build the image, stand up the real `internal/credproxy` binary with a freshly-minted self-signed CA on `127.0.0.1:8443`, set `ANTHROPIC_BASE_URL=https://127.0.0.1:8443` + `SSL_CERT_FILE=/etc/tide/proxy/ca.crt`, and make **one real `ChatAnthropic(...).invoke()`** with `max_tokens=1` against the **real Anthropic API** (via the durable key at `~/.tide/anthropic.key`; ~fractions of a cent). Binary pass/fail: connection succeeds (whole chain: CA trust + credproxy route-allowlist + real key injection + upstream TLS) or raises `SSLCertVerificationError`/`httpx.ConnectError`. The trust chain under test is identical to production; the kind-sidecar surface adds only dispatch-integration fidelity (proven for Node/Go already) and can be promoted to a kind gate later, optionally.
- **D-07 (Client-construction posture):** **Separate the spike from the shipped image.** The *spike* uses **plain** `ChatAnthropic` trusting `SSL_CERT_FILE` alone — that is the only way to actually *measure* the A1 unknown and produce a durable regression signal. The *shipped image* uses a **defensive client factory**: construct `anthropic.Anthropic(http_client=httpx.Client(verify=ssl.create_default_context(cafile=SSL_CERT_FILE)))` and pass it into `ChatAnthropic(..., client=...)` (or the pinned-version equivalent `anthropic_client=`). Robust whether or not the alone-path works, and against future SDK-transport regressions (`anthropic-sdk-python#923`, `langchain#35843`). Honors the research directive to "plan slack for the fallback" rather than discover it mid-build.

### Read-only enforcement (structural, not prompt)
- **D-08 (Read-only jobspec variant lands in Phase 48):** Add a read-only verifier pod-spec path (a `buildVerifierJobSpec`, or a read-only mode on `internal/dispatch/podjob/jobspec.go`): **`ReadOnly: true`** on the `/workspace` worktree mount + a **separate ephemeral `emptyDir` scratch** volume for incidental gate-command writes + **omit** all git-write/push credentials (they are already push-Job-only via `project.Spec.Git.CredsSecretRef`) + **no manager-side child-CRD consumption path** for verifier output. **Unit-tested and asserted, but NOT yet dispatched by any reconciler** — full `TaskReconciler` dispatch integration stays Phase 51. This keeps the `Subagent` interface + envelope contract literally unchanged while making EVAL-01's "enforced structurally" language real in-repo.
- **D-09 (RO proof — both layers):** (a) **Static** unit assertions on the verifier jobspec — `ReadOnly:true` worktree mount, `ReadOnlyRootFilesystem`, no `GIT_PAT`/git-write secret name in `Env`/`EnvFrom`, no child-CRD path; **and** (b) a **lightweight behavioral** container test — run the image against a fixture worktree on a **read-only bind mount** and have its git/bash tool *attempt* `git commit`/`git push`, asserting failure at the **filesystem/credential layer** (EROFS / missing remote credential), not prompt refusal. Mirrors the Phase-45 stub contract test + PITFALLS Pitfall 4 point 5. The behavioral test uses `docker --read-only`-style isolation, not a full kind dispatch, so it stays CI-friendly.

### Dependency pinning + CI gate
- **D-10:** Patch-exact pins (`langgraph==1.2.9`, `langchain-anthropic==1.4.8`, `anthropic==0.117.0`, `langchain-core==1.4.9`, `httpx==0.28.1`, base `python:3.13-slim-bookworm` — **re-verify at build time**, 1.x moves ~weekly) in a **hash-locked** `requirements.txt` installed with `pip install --require-hashes`, plus a **`make verify-langgraph-pins`** grep gate that rejects any range/unpinned specifier (`>=`/`~=`/`^`/no-version), wired into CI. Mirrors the Go import-firewall analyzers (`make verify-dispatch-imports`) and the `@sha256` node-image discipline. Dockerfile base images digest-pinned inline (`python:3.13-slim-bookworm@sha256:...`).

### Claude's Discretion
- LangGraph graph internals (`create_react_agent` vs. a minimal `StateGraph`), the exact trivial gate command the shell runs, multi-stage Dockerfile layout, and whether the standalone spike is driven by a `pytest` or a plain `python` entrypoint — planner/researcher to decide within the decisions above.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (scope authority)
- `.planning/REQUIREMENTS.md` — **EVAL-01** (read-only image + structural enforcement + adversarial test) and **EVAL-02** (live TLS spike + pinning gate) are Phase 48; the Out-of-Scope table names the anti-features (`deepagents`, `REQUESTS_CA_BUNDLE`, Python prompt port).
- `.planning/ROADMAP.md` §"Phase 48" — goal, 4 success criteria, research flag (TLS outcome unknown until run live).

### Research (committed `f85ee3d` — the load-bearing pre-decisions)
- `.planning/research/PITFALLS.md` — **Pitfall 2** (httpx/`langchain-anthropic` may not honor `SSL_CERT_FILE` through the SDK's custom transport; the concrete test recipe at line 41; the `http_client=`/`anthropic_client=` fallback at line 42), **Pitfall 3** (dependency-pin discipline + CI gate), **Pitfall 4** (read-only structural enforcement — ReadOnly mount `jobspec.go:434`, credential omission, no child-CRD path, adversarial fixture test).
- `.planning/research/STACK.md` — §1 verified pins + no-conflict resolution, §5 base image (`python:3.13-slim-bookworm`) + `create_react_agent`/`langgraph-prebuilt` note, the `anthropic`/`httpx` explicit-pin rationale ("where the A1 CA-trust behavior lives").
- `.planning/research/ARCHITECTURE.md` — Q4/Q5 envelope reuse (context for Phase 49; the image name `tide-langgraph-verifier` at line 192; the `cmd/tide-langgraph-verifier/` structure at line 273). **Caveat:** written under the pre-2026-07-18 "three verify stages" framing — the reframe to ONE `LoopPolicy`-parameterized loop supersedes its config-surface shape; for Phase 48 (image + spike only) this doesn't bite.
- `.planning/research/SUMMARY.md` — cross-cutting synthesis.

### Strategy notes
- `.planning/notes/langgraph-successor-runtime-strategy.md` — the ladder (this image is the beachhead rung); the pluggable-runtime seam is `pkg/dispatch.Subagent` + envelope, not any image; verify BLOCKED = new halt class (Phase 51).
- `.planning/notes/five-loop-model.md` — the wider organizing frame the Task loop plugs into (context, not Phase-48 scope).

### The seam the image must conform to (source of truth — read before coding the image)
- `pkg/dispatch/subagent.go:33` — the `Subagent.Run(ctx, EnvelopeIn) (EnvelopeOut, error)` contract + out-of-tree-author godoc.
- `pkg/dispatch/envelope.go` — `EnvelopeIn` (`:45`), `EnvelopeOut` (`:170`), `TerminationStub` (`:394`), version discriminators (`:30`), `ValidateAPIVersionKind` (`:446`). The Python image re-implements these JSON shapes.
- `pkg/dispatch/provider.go:36` — `ProviderSpec` (vendor/model/params); provider impl rejects mismatched `vendor`.
- `pkg/dispatch/doc.go` — the import-firewall contract (`make verify-dispatch-imports`) — why the Python image can't import the Go package.
- `pkg/dispatch/vendor_capabilities.go:38` — `SelfInstruments` (why `"anthropic"` keeps synthesis correct for now; the future `"langgraph"` flip).
- `internal/dispatch/podjob/jobspec.go` — the pod wiring: credproxy env at `:423` (`ANTHROPIC_BASE_URL`/`SSL_CERT_FILE`/`NODE_EXTRA_CA_CERTS`), ReadOnly cert-mount pattern `:434`, subagent RW `/workspace` mount `:394`, `credproxyEnabled` gate `:322`. This is where the read-only verifier variant (D-08) lands.
- `internal/credproxy/server.go` (`ListenAndServeTLS` `:312`, route allowlist `:123`) + `internal/credproxy/cert.go:49` (`MintSelfSignedCert` → `ca.crt`) — the CA the spike trusts.
- `images/claude-subagent/Dockerfile` + `cmd/claude-subagent/main.go` + `internal/harness/envelope_io.go` — the existing image's Dockerfile/entrypoint/file-I/O pattern to mirror.
- `cmd/tide-spike/main.go` — the existing standalone-spike harness precedent for D-06.
- `test/integration/kind/examples_image_pin_test.go` — the image-pin-vs-appVersion CI precedent (strict envelope-version equality rationale).
- `docs/project-authoring.md` (`:47`, `:566`) — the prose "container image contract" for pluggable subagents.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`cmd/tide-spike/main.go`** — an existing standalone spike harness; the D-06 TLS spike follows its shape (stand up credproxy, make a real call, binary pass/fail) rather than inventing a new harness.
- **`internal/credproxy`** (`server.go`, `cert.go`) — reused *as-is* for the spike: `MintSelfSignedCert` produces `ca.crt`; `ListenAndServeTLS` on `127.0.0.1:8443` with the `POST /v1/messages` allowlist. No credproxy changes needed.
- **`images/claude-subagent/Dockerfile`** — the multi-stage + digest-pinned-base + version-pinned-runtime pattern to mirror for the Python image (swap the Go/Node stages for a `python:3.13-slim-bookworm` runtime; the CLI subagent's Go stage has no analog since the Python image parses JSON directly).
- **Phase-45 stub contract test** — the precedent for the D-09 behavioral adversarial test (dispatch against a fixture, attempt the forbidden op, assert layer-level failure).

### Established Patterns
- **Envelope = files on PVC, strict-version-validated first.** `harness.ReadEnvelopeIn`/`WriteEnvelopeOut` (`internal/harness/envelope_io.go`) + `ValidateAPIVersionKind` define the exact I/O + validation the Python image re-implements. Setgid-shared `envelopes/` dir (uid 1000 ↔ push-Job uid 65532) — the verifier writes `out.json` `0o644`.
- **Vendor-sentinel gating.** A `Subagent` impl rejects an envelope whose `provider.vendor` ≠ its compiled-in sentinel (`provider.go:26`). The Python image's sentinel = `"anthropic"` for Phase 48 (D-04).
- **Read-only via mount-flag + credential-omission**, already used for the credproxy sidecar's cert mount (`jobspec.go:434`, `ReadOnly:true`) and the push-Job-only credential (`push_helpers.go:320`). D-08 extends the same idioms to the worktree mount.
- **Image-pin CI discipline.** New images join the Makefile `docker-buildx-snapshot` + release.yaml `build-images` matrix + `verify-chart-images-published`; envelope-version strict-equality drives the pin-vs-appVersion test.

### Integration Points
- **`internal/dispatch/podjob/jobspec.go`** — the *only* controller-side file Phase 48 changes: a read-only verifier pod-spec variant (D-08), unit-tested, not yet dispatched.
- **Makefile + `.github/workflows/`** — new image build target/matrix entry + the `make verify-langgraph-pins` gate (D-10).
- **The `Subagent` interface + `EnvelopeIn`/`EnvelopeOut` JSON contract** — consumed by the new image but **unchanged** (no Go edits to `pkg/dispatch` beyond none; the image conforms to the existing shape).
</code_context>

<specifics>
## Specific Ideas

- The spike's real call should be the **cheapest possible real invoke** — `max_tokens=1` `ChatAnthropic(...).invoke("hi")` — to exercise `POST /v1/messages` (the allowlisted route) end-to-end at ~0 cost; the durable real key lives at `~/.tide/anthropic.key` (outside the repo, per the `make eval` recipe).
- The defensive factory (D-07) is the *shipping* default; keep the plain-`SSL_CERT_FILE` spike as a **retained** test/artifact so the A1 answer is a durable, re-runnable regression signal, not a one-time throwaway.
- Read-only enforcement is **three independent layers** (mount + credential omission + no child-CRD path) — the phase proves *each* is load-bearing, so no single layer is a single point of failure.
</specifics>

<deferred>
## Deferred Ideas

- **New `"langgraph"` vendor sentinel + `SelfInstruments` registration + `EVALUATOR`-kind span** → Phase 51 (OBS-03); Phase 48 rides `"anthropic"`.
- **`VerifyContext` envelope field, `gate_decision` verdict schema, findings persistence** → Phase 49.
- **`role="verifier"` orchestrator-side Go prompt templates + coverage-not-conservatism** → Phase 51 (EVAL-04).
- **`TaskReconciler` dispatch of the verifier + concurrency-gate accounting (`verifierInFlightCount`) + `LoopPolicy.BudgetCents`** → Phase 51 (ESC-04, TASK-*).
- **Kind-cluster full-fidelity TLS/dispatch gate** (native credproxy sidecar surface) — optional promotion of the D-06 standalone spike once the image dispatches for real (Phase 51).
- **`cache_control` / `AnthropicPromptCachingMiddleware` + `Provider.Params` passthrough** (temperature/thinking/top_p/top_k via the SDK path) — CACHE-F1, a future authoring-migration win; the defensive-factory client seam (D-07) is where it would later attach.

*Discussion stayed within phase scope — these are pre-mapped downstream-phase boundaries, not scope creep.*
</deferred>

---

*Phase: 48-langgraph-evaluator-image-credproxy-tls-spike*
*Context gathered: 2026-07-18*
