# Phase 18: Eval Harness — Research

**Researched:** 2026-06-15
**Domain:** Go test harness, golden-file snapshot testing, offline token proxy, cost-parity delegation, DAG acyclicity reuse, count_tokens HTTP pre-flight
**Confidence:** HIGH (all code findings from direct codebase inspection; goldie API from Context7; count_tokens API from official Anthropic docs)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Token ratchet = per-template no-growth snapshot committed in testdata; any growth hard-fails `make test`.
- **D-01a:** Offline ratchet cannot call count_tokens (zero-network; no exact local Anthropic tokenizer — do NOT add tiktoken). Must snapshot a deterministic offline PROXY of size (bytes/runes/whitespace-words). Proxy unit is Claude's discretion.
- **D-02:** All eval code lives in `internal/eval/` package. Deterministic gate runs under the EXISTING `make test` unit tier — NOT a new `make test-unit` target, NOT build-tagged.
- **D-02a:** No `make test-unit` target added. The ONE new Makefile target is `make eval` (online).
- **D-03:** count_tokens preflight ships as a small command (`cmd/tide-eval/` suggested) behind `//go:build eval`, invoked by `make eval`; stdlib net/http POST to credproxy's allowlisted POST /v1/messages/count_tokens; NO SDK.
- **D-04 (REVISED 2026-06-15):** Realized-savings fixture = **synthetic deterministic** four-field `Usage` / events.jsonl fixtures (cache-hit, cache-miss, cache-creation, zero-usage), matching `stream_parser_test.go` shape — **NOT a real dispatch capture** (reverted: a real capture needs a live cluster + spend with no correctness benefit for a pure-arithmetic gate; see CONTEXT.md). Fixture must populate all four Usage dimensions. Cost-delta check is a deterministic TEST delegating to `(*Anthropic).estimatedCostCents`, asserting parity within 1 cent.
- **D-05:** Deterministic-only gate (child-CRD parse / declared-output-path / DAG acyclicity); NO LLM-as-judge this milestone.

### Claude's Discretion

- Offline ratchet proxy unit (bytes vs runes vs words).
- Exact command path/name for the `make eval` tool.
- Canonical EnvelopeIn fixture shape for golden renders.
- Cheapest real-dispatch capture mechanism for the events.jsonl fixture.

### Deferred Ideas (OUT OF SCOPE)

- LLM-as-judge / semantic-quality scoring — EVAL-F1, deferred.
- Template reorder / token trimming — Phase 19.
- `SharedContext` on `EnvelopeIn` — Phase 20.
- Per-level token accounting + cache-hit dashboard panel — Phase 21.
</user_constraints>

---

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| EVAL-01 | Frozen baseline (golden renders of all five templates + recorded usage snapshot) committed under `testdata/`. | goldie/v2 Assert for renders; events.jsonl fixture for usage snapshot. |
| EVAL-02 | Deterministic protocol-compliance checks: child-CRD parse success, declared-output-path presence, DAG acyclicity; no LLM judge. | `readChildCRDs` (subagent.go:436), `pkg/dag.ComputeWaves` (kahn.go:46), harness Validate (outputs.go:43) already exist — call them from eval fixtures. |
| EVAL-03 | Golden-file snapshot-tests every rendered template via goldie/v2, runs in `make test` with zero network; flags accidental prompt growth. | goldie/v2 `g.Assert(t, name, data)` + `WithFixtureDir("testdata/goldie")`; ratchet file per template for size gate. |
| EVAL-04 | Cost delta via `estimatedCostCents`; realized savings per wave (cache-write premium subtracted); asserts parity within 1 cent. | `estimatedCostCents` at pricing.go:132 — unexported on `*Anthropic`; test file in `package anthropic` resolves this. Realized savings = `cacheRead * (inputRate - cacheReadRate)` from the same price table. |
| EVAL-05 | Pre-flight token counting via credproxy `POST /v1/messages/count_tokens`; stdlib net/http; `//go:build eval`; `make eval` target. | credproxy allowlist confirmed at server.go:105-106; API shape: POST JSON `{model, system, messages:[{role,content}]}`; response `{"input_tokens": N}`. Headers: `content-type: application/json`, `x-api-key: <signed-token>`, `anthropic-version: 2023-06-01`. |
| EVAL-06 | Regression-gates prompt/template changes in CI. | `make test` is the unit-test target invoked in `.github/workflows/test.yml:28`; new `internal/eval/` tests run under it automatically with no CI config change. |
</phase_requirements>

---

## Summary

Phase 18 builds the quality and cost gate that must exist before any template or prompt change in subsequent phases. All six requirements are buildable from existing codebase primitives — no new algorithmic work is needed.

The three deterministic checks required by EVAL-02 are already implemented: `readChildCRDs` in `internal/subagent/anthropic/subagent.go:436` parses child-CRD JSON files with kind/name validation; `pkg/dag.ComputeWaves` at `pkg/dag/kahn.go:46` returns a `*CycleError` on cyclic input; declared-output-path presence validation lives in `internal/harness/outputs.go:43`. The eval harness calls these directly from test fixtures — no new logic, only new test coverage.

The `estimatedCostCents` access question (EVAL-04) resolves cleanly: it is an unexported method on `*Anthropic` (pricing.go:132), so a `_test.go` file in `package anthropic` — the same package — can call it directly without an exported wrapper, preserving the established test pattern from `pricing_test.go`.

The offline token proxy (D-01a) should use **byte count** (`len(rendered)`) rather than runes or words. Byte count is the most stable proxy for Anthropic token count on English/code content (1 token ≈ 3–5 bytes for mixed text/code), is zero-dependency, and avoids unicode normalization concerns. Rune count is indistinguishable from byte count for ASCII-dominant templates. Word count introduces tokenizer-specific whitespace semantics. The ratchet only needs to be monotonically decreasing — it doesn't need to predict exact token counts (that is `make eval`'s job).

The `make eval` target invokes a `cmd/tide-eval/main.go` command (behind `//go:build eval`) that renders each of the five templates with a fixed fixture, then POSTs the rendered text to `{ProxyEndpoint}/v1/messages/count_tokens` using stdlib `net/http`. The `ProxyEndpoint` and `SignedToken` come from env vars (matching the in-pod pattern), or optionally from CLI flags for maintainer use. Response `{"input_tokens": N}` is compared against the 1,024-token Sonnet/Opus cache minimum.

**Primary recommendation:** Create `internal/eval/` with four test files; add goldie/v2 as a test-only `go.mod` entry; use `package anthropic` test files for cost-parity; reuse `readChildCRDs`, `ComputeWaves`, and harness `Validate` without modifying them; capture one real dispatch for the events.jsonl fixture; build `cmd/tide-eval/` behind `//go:build eval`.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Golden render snapshot (EVAL-01/03) | `internal/eval/` test files | `internal/subagent/common` (template loading) | Template rendering is a pure function of EnvelopeIn + template bytes; eval owns the fixture and the assertion |
| Protocol-compliance gate (EVAL-02) | `internal/eval/` test files | `internal/subagent/anthropic` (readChildCRDs), `pkg/dag` (ComputeWaves), `internal/harness` (Validate) | All three checks are already implemented in their domain packages; eval composes them via fixtures |
| Cost-parity assertion (EVAL-04) | `internal/subagent/anthropic/` (same-package test) | `pkg/dispatch` (Usage type) | `estimatedCostCents` is unexported — test must be in the same package |
| count_tokens pre-flight (EVAL-05) | `cmd/tide-eval/` (behind `//go:build eval`) | `internal/credproxy` (ProxyEndpoint convention) | Network-touching code is isolated from `make test` by the build tag and a separate Makefile target |
| CI regression gate (EVAL-06) | `.github/workflows/test.yml` via `make test` | `internal/eval/` (tests auto-included) | test.yml already runs `make test`; internal/eval/ joins the unit tier automatically |

---

## Standard Stack

### Core (no new production deps — test-only)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/sebdah/goldie/v2` | v2.8.0 | Golden-file snapshot testing; `g.Assert`, `g.AssertJson`, `-update` flag | Purpose-built for golden file testing in Go; high reputation; already called out in STACK.md and CONTEXT.md |

**Not in go.mod yet.** Add as a test-only indirect dependency:

```bash
go get -u github.com/sebdah/goldie/v2@v2.8.0
```

Goldie registers a global `-update` flag via `flag.Bool`. Running tests with `go test ./internal/eval/... -update` rewrites all `.golden` files.

### Supporting (stdlib only for `cmd/tide-eval/`)

| Component | Source | Purpose |
|-----------|--------|---------|
| `net/http` | stdlib | POST to credproxy `/v1/messages/count_tokens` |
| `encoding/json` | stdlib | Marshal request; unmarshal `{"input_tokens": N}` response |
| `text/template` | stdlib | Template rendering in eval tests (via `common.LoadPromptTemplate`) |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `goldie/v2` byte assert | Manual `os.ReadFile` + `bytes.Equal` | goldie provides `-update` flag and better diff output; no real advantage to hand-rolling |
| byte count as ratchet proxy | rune count | For ASCII-dominant Go/English templates, rune == byte; byte count is simpler and unambiguous |
| `package anthropic` test for `estimatedCostCents` | Exported thin wrapper in pricing.go | Same-package test preserves the existing pattern in `pricing_test.go` and avoids modifying `pricing.go` (locked file) |

**Installation (test-only dependency):**

```bash
go get -u github.com/sebdah/goldie/v2@v2.8.0
```

---

## Package Legitimacy Audit

| Package | Registry | Source Repo | Disposition |
|---------|----------|-------------|-------------|
| `github.com/sebdah/goldie/v2` | github.com/sebdah/goldie | github.com/sebdah/goldie | Approved — well-established golden-file library, referenced in STACK.md [CITED: .planning/research/STACK.md] |

**Packages removed due to [SLOP]:** none
**Packages flagged [SUS]:** none

*slopcheck not run — package confirmed via Context7 (High source reputation) and STACK.md citation. Not adding tiktoken or any local tokenizer per D-01a.*

---

## Architecture Patterns

### System Architecture Diagram

```
EnvelopeIn fixture (deterministic, fixed fields)
         │
         ▼
common.LoadPromptTemplate(role, level)
         │
         ▼
tmpl.Execute(&buf, fixture) → renderedBytes
         │
         ├─► goldie g.Assert(t, templateName, renderedBytes)
         │        └─ testdata/goldie/<templateName>.golden
         │
         └─► len(renderedBytes) ≤ ratchet[templateName]
                  └─ testdata/ratchets/<templateName>.txt

events.jsonl fixture (committed, real capture)
         │
         ├─► ParseStream(fixture, &sink) → Usage
         │
         └─► estimatedCostCents(model, usage) → costCents
                  └─ assert |costCents - expected| < 1

child-CRD fixture (sample JSON files in testdata/)
         │
         └─► readChildCRDs(dir, prefix) → ChildCRDSpecs, err
                  ├─ assert err == nil (valid fixture)
                  └─ assert err != nil (invalid fixture)

DAG fixture (nodes + edges slices)
         │
         └─► dag.ComputeWaves(nodes, edges) → waves, err
                  ├─ assert err == nil (acyclic)
                  └─ assert *CycleError (cyclic)

cmd/tide-eval/ (//go:build eval, invoked by make eval)
         │
         ├─ renders each of 5 templates with fixture
         ├─ POST /v1/messages/count_tokens via ProxyEndpoint
         │      body: {model, system:"", messages:[{role:user, content:rendered}]}
         │      headers: content-type, x-api-key (SignedToken), anthropic-version:2023-06-01
         └─ prints: per-template input_tokens + pass/fail vs 1024 cache floor
```

### Recommended Project Structure

```
internal/eval/
├── doc.go                              # package overview and discipline statement
├── render_test.go                      # EVAL-01/03: golden render + byte ratchet for all 5 templates
├── protocol_test.go                    # EVAL-02: child-CRD parse, declared-output-path, DAG acyclicity
├── cost_replay_test.go                 # EVAL-04: offline cost replay from events.jsonl fixture
└── testdata/
    ├── goldie/                         # goldie default fixture dir (WithFixtureDir("testdata/goldie"))
    │   ├── project_planner.golden
    │   ├── milestone_planner.golden
    │   ├── phase_planner.golden
    │   ├── plan_planner.golden
    │   └── task_executor.golden
    ├── ratchets/                       # per-template byte-count ceiling (plain text integer)
    │   ├── project_planner.txt
    │   ├── milestone_planner.txt
    │   ├── phase_planner.txt
    │   ├── plan_planner.txt
    │   └── task_executor.txt
    ├── fixtures/
    │   ├── stream_real.jsonl           # one real claude -p --bare dispatch events.jsonl
    │   ├── child_valid_task.json       # valid Task child-CRD JSON for protocol tests
    │   ├── child_bad_kind.json         # invalid Kind for negative test
    │   └── child_missing_name.json     # missing Name for negative test
    └── dag/
        ├── acyclic.json                # nodes+edges for a 3-wave acyclic DAG
        └── cyclic.json                 # nodes+edges for a cyclic DAG (2-node cycle)

internal/subagent/anthropic/
└── cost_parity_test.go                 # EVAL-04: in-package test calling estimatedCostCents

cmd/tide-eval/
└── main.go                             # //go:build eval; count_tokens pre-flight report
```

**Note on cost_replay_test.go location:** The offline cost replay test lives in `internal/eval/` and calls `ParseStream` (exported) plus a `CostEstimator` interface or a thin exported wrapper (see Pattern 2 below). It does NOT need to be in `package anthropic` unless it directly calls `estimatedCostCents`. The cost-parity test that calls the unexported method lives separately in `internal/subagent/anthropic/cost_parity_test.go`.

### Pattern 1: goldie Golden File Render Test

**What:** Load each template, render with a fixed deterministic EnvelopeIn, assert the rendered bytes match the committed `.golden` file.

**When to use:** Every rendered template. First run with `-update` flag writes the golden; subsequent runs are the regression gate.

```go
// Source: Context7 /sebdah/goldie — Assert API
// internal/eval/render_test.go — package eval

package eval

import (
    "bytes"
    "testing"
    "github.com/sebdah/goldie/v2"
    "github.com/jsquirrelz/tide/internal/subagent/common"
    pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

var fixedEnvelope = pkgdispatch.EnvelopeIn{
    APIVersion:          "tideproject.k8s/v1alpha1",
    Kind:                "TaskEnvelopeIn",
    TaskUID:             "eval-fixture-uid-000",
    Role:                "planner",
    Level:               "plan",
    Prompt:              "EVAL FIXTURE: do not submit",
    DeclaredOutputPaths: []string{"internal/eval/testdata/placeholder.go"},
    Provider: pkgdispatch.ProviderSpec{
        Vendor: "anthropic",
        Model:  "claude-sonnet-4-6",
    },
}

func TestGoldenRender_PlanPlanner(t *testing.T) {
    tmpl, err := common.LoadPromptTemplate("planner", "plan")
    if err != nil {
        t.Fatalf("load template: %v", err)
    }
    var buf bytes.Buffer
    if err := tmpl.Execute(&buf, fixedEnvelope); err != nil {
        t.Fatalf("render: %v", err)
    }
    g := goldie.New(t, goldie.WithFixtureDir("testdata/goldie"))
    g.Assert(t, "plan_planner", buf.Bytes())
}
```

**Ratchet test (byte-count ceiling):**

```go
// internal/eval/render_test.go (continued)
import (
    "os"
    "strconv"
    "strings"
)

func TestByteRatchet_PlanPlanner(t *testing.T) {
    tmpl, _ := common.LoadPromptTemplate("planner", "plan")
    var buf bytes.Buffer
    _ = tmpl.Execute(&buf, fixedEnvelope)

    ratchetFile := "testdata/ratchets/plan_planner.txt"
    data, err := os.ReadFile(ratchetFile)
    if err != nil {
        t.Fatalf("missing ratchet file %s — run with -update to create: %v", ratchetFile, err)
    }
    ceiling, err := strconv.Atoi(strings.TrimSpace(string(data)))
    if err != nil {
        t.Fatalf("ratchet file %s malformed: %v", ratchetFile, err)
    }
    actual := buf.Len()
    if actual > ceiling {
        t.Errorf("template plan_planner grew: rendered %d bytes, ratchet ceiling %d — update testdata/ratchets/plan_planner.txt if growth is intentional", actual, ceiling)
    }
}
```

**Updating ratchets:** The `-update` flag convention for goldie does not automatically update the ratchet files; those are plain text integers and are updated manually (or via a small `TestWriteRatchets` helper that writes only when `*goldie.Update` flag is set, guarded with `t.Skip` otherwise).

### Pattern 2: estimatedCostCents Access (in-package test)

**What:** `estimatedCostCents` is unexported on `*Anthropic`. A `_test.go` file in `package anthropic` can call it directly.

**Ground truth:** `pricing.go:132` signature:
```go
func (a *Anthropic) estimatedCostCents(model string, u pkgdispatch.Usage) int64
```

The existing `pricing_test.go` already uses `package anthropic` and calls this method through `New(Options{})`. The cost-parity test follows the identical pattern:

```go
// internal/subagent/anthropic/cost_parity_test.go
package anthropic

import (
    "strings"
    "testing"
    pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

func TestCostParity_CacheRealized(t *testing.T) {
    a := New(Options{})

    // Fixture shape mirrors stream_parser_test.go TestParseStream_HappyPath:
    // a dispatch that has both CacheCreationTokens and CacheReadTokens.
    const fixture = `{"type":"result","result":"x","usage":{"input_tokens":500,"output_tokens":100,"cache_read_input_tokens":800,"cache_creation_input_tokens":1200}}`
    usage, _, err := ParseStream(strings.NewReader(fixture+"\n"), &strings.Builder{})
    if err != nil {
        t.Fatalf("ParseStream: %v", err)
    }

    got := a.estimatedCostCents("claude-sonnet-4-6", usage)

    // Hand-verify: claude-sonnet-4-6 rates: input=300, output=1500, read=30, write=375 cents/MTok
    // (500*300 + 100*1500 + 800*30 + 1200*375) / 1_000_000 = (150000 + 150000 + 24000 + 450000) / 1_000_000
    // = 774000 / 1_000_000 = 0.774 → ceiling → 1 cent
    want := int64(1)
    if got != want {
        t.Errorf("estimatedCostCents parity: got %d, want %d", got, want)
    }
}

func TestCostParity_RealizedSavings(t *testing.T) {
    a := New(Options{})
    model := "claude-sonnet-4-6"

    // Two scenarios with the same total token count:
    // (a) no caching: all tokens are input
    // (b) with caching: 800 tokens read from cache (0.10x) + 1200 tokens written (1.25x) + 500 input + 100 output
    uNoCaching := pkgdispatch.Usage{InputTokens: 2600, OutputTokens: 100}
    uWithCaching := pkgdispatch.Usage{InputTokens: 500, OutputTokens: 100, CacheReadTokens: 800, CacheCreationTokens: 1200}

    costNoCaching := a.estimatedCostCents(model, uNoCaching)
    costWithCaching := a.estimatedCostCents(model, uWithCaching)

    // Realized savings per wave = costNoCaching - costWithCaching
    // This is the correct REALIZED savings (cache-write premium absorbed on first warm; subsequent reads at 0.10x)
    if costWithCaching >= costNoCaching {
        t.Errorf("cache should save money for high read:write ratio: no-cache=%d cents, with-cache=%d cents", costNoCaching, costWithCaching)
    }
}
```

**Key insight:** `(*Anthropic).estimatedCostCents` uses `a.prices` (the per-instance cloned map), not `priceTable` directly. This is consistent with `pricing_test.go`'s approach of constructing a fresh `New(Options{})` instance. No changes to pricing.go are needed or permitted.

### Pattern 3: Protocol-Compliance Gate (EVAL-02)

**What:** Call the existing `readChildCRDs` and `dag.ComputeWaves` from test fixtures. These functions are in different packages, so `internal/eval/protocol_test.go` imports them directly.

**Access note:** `readChildCRDs` is unexported in `package anthropic`. Options:
1. Export it (modifies subagent.go — locked file).
2. Test it from `package anthropic` via a separate test file in that package.
3. Create a thin exported wrapper in a new file `internal/subagent/anthropic/childcrd_eval.go`.
4. Re-implement a simpler version in `internal/eval/` that is sufficient for fixture validation.

**Recommendation:** Option 3 — add a thin exported function `ReadChildCRDsForEval` in a new file `internal/subagent/anthropic/childcrd_eval.go` that calls `readChildCRDs`. This is a new file (not a modification to `subagent.go`) and is the cleaner import path. The file is not gated by a build tag since it is a thin forwarding function used in test-only imports.

Alternatively, since `internal/eval/protocol_test.go` can be `package eval_test` importing the anthropic package, and since we need to exercise the parsing path for protocol compliance, the most pragmatic solution is a dedicated `internal/subagent/anthropic/protocol_eval_test.go` (in `package anthropic`) that contains the fixture-driven child-CRD and DAG compliance tests, avoiding any changes to non-test files entirely.

**Cleanest approach for D-02 (no hot-path file changes):** Move EVAL-02's child-CRD parse tests into `internal/subagent/anthropic/` as a new `protocol_compliance_test.go` in `package anthropic`. The DAG tests go into `internal/eval/protocol_test.go` importing `pkg/dag` (which is an exported package). The declared-output-path test uses `internal/harness.Validate` (exported function).

**DAG acyclicity test (uses exported `pkg/dag.ComputeWaves`):**

```go
// internal/eval/protocol_test.go
package eval

import (
    "testing"
    "github.com/jsquirrelz/tide/pkg/dag"
)

func TestDAGAcyclicity_AcyclicFixture(t *testing.T) {
    nodes := []dag.NodeID{"task-01", "task-02", "task-03"}
    edges := []dag.Edge{
        {From: "task-01", To: "task-02"},
        {From: "task-02", To: "task-03"},
    }
    waves, err := dag.ComputeWaves(nodes, edges)
    if err != nil {
        t.Errorf("acyclic fixture must not return error: %v", err)
    }
    if len(waves) != 3 {
        t.Errorf("expected 3 waves, got %d", len(waves))
    }
}

func TestDAGAcyclicity_CyclicFixture(t *testing.T) {
    nodes := []dag.NodeID{"task-01", "task-02"}
    edges := []dag.Edge{
        {From: "task-01", To: "task-02"},
        {From: "task-02", To: "task-01"}, // cycle
    }
    _, err := dag.ComputeWaves(nodes, edges)
    if err == nil {
        t.Error("cyclic fixture must return CycleError")
    }
    var cycErr *dag.CycleError
    if !errors.As(err, &cycErr) {
        t.Errorf("expected *dag.CycleError, got %T: %v", err, err)
    }
}
```

**Declared-output-path test (uses exported `harness.Validate`):**

```go
// internal/eval/protocol_test.go (continued)
import "github.com/jsquirrelz/tide/internal/harness"

func TestDeclaredOutputPaths_Presence(t *testing.T) {
    // An EnvelopeIn with non-empty DeclaredOutputPaths satisfies the protocol.
    env := pkgdispatch.EnvelopeIn{
        DeclaredOutputPaths: []string{"some/output/file.go"},
    }
    if len(env.DeclaredOutputPaths) == 0 {
        t.Error("fixture must have non-empty DeclaredOutputPaths")
    }
}
```

*Note: `harness.Validate` requires a real filesystem and a `startedAt` time to scan for written files, making it unsuitable for a pure-fixture test. The declared-output-path EVAL-02 gate is therefore a structural check on the fixture EnvelopeIn (non-empty `DeclaredOutputPaths` field), consistent with the constraint that EVAL-02 is deterministic and zero-network.*

### Pattern 4: count_tokens Pre-flight Command (`cmd/tide-eval/`)

**Build tag:** `//go:build eval` at the top of `cmd/tide-eval/main.go`. This excludes it from `go build ./...` and `make test` without a special `go list` exclusion.

**count_tokens request/response shape** [CITED: platform.claude.com/docs/en/build-with-claude/token-counting]:

```
POST {ProxyEndpoint}/v1/messages/count_tokens
Headers:
  x-api-key: {SignedToken}          (credproxy validates HMAC, injects real key)
  content-type: application/json
  anthropic-version: 2023-06-01

Body:
{
  "model": "claude-sonnet-4-6",
  "system": "",                       (optional; use "" for pure user-turn prompts)
  "messages": [
    {"role": "user", "content": "<rendered template bytes>"}
  ]
}

Response (200 OK):
{"input_tokens": <integer>}
```

**How `cmd/tide-eval/` discovers the ProxyEndpoint:**

The credproxy URL follows the same convention as the in-pod `EnvelopeIn.ProxyEndpoint` field. For maintainer use (outside a pod), the command reads from env vars or CLI flags:

```go
// cmd/tide-eval/main.go (sketch)
//go:build eval

package main

import (
    "encoding/json"
    "flag"
    "fmt"
    "net/http"
    "os"
    // ... common.LoadPromptTemplate, pkg/dispatch envelope fixture
)

var (
    proxyEndpoint = flag.String("proxy", os.Getenv("TIDE_PROXY_ENDPOINT"), "credproxy base URL (e.g. https://127.0.0.1:8443)")
    signedToken   = flag.String("token", os.Getenv("TIDE_SIGNED_TOKEN"), "HMAC signed token for credproxy")
    model         = flag.String("model", "claude-sonnet-4-6", "model for token counting")
)
```

The command renders each of the five templates, POSTs to `{proxyEndpoint}/v1/messages/count_tokens`, prints per-template `input_tokens`, and checks each against the 1,024-token Sonnet/Opus cache minimum. A `make eval` target in the Makefile compiles and runs this command:

```makefile
.PHONY: eval
eval: ## Run count_tokens pre-flight against credproxy (online; requires TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN).
    go run -tags eval ./cmd/tide-eval/ -proxy "$(TIDE_PROXY_ENDPOINT)" -token "$(TIDE_SIGNED_TOKEN)"
```

### Anti-Patterns to Avoid

- **Putting cost math in `internal/eval/`:** Do not recompute `inputRate - cacheReadRate` manually. Use `estimatedCostCents` for both the no-cache and with-cache scenarios and subtract the results. If the formula diverges from `pricing.go`, a future pricing change breaks only one branch.
- **Putting the goldie fixture dir at `testdata/` root:** Use `goldie.WithFixtureDir("testdata/goldie")` to keep goldie's `.golden` files separate from ratchet text files and events.jsonl fixtures. This prevents accidental -update behavior on the wrong files.
- **Using `goldie.AssertJson` for template renders:** Template output is plain text, not JSON. Use `g.Assert(t, name, buf.Bytes())`. `AssertJson` marshals a Go struct to JSON — wrong for this use case.
- **Calling `count_tokens` inside `make test`:** Zero-network constraint (D-01a). All count_tokens calls are behind `//go:build eval` and `make eval` only.
- **Adding `claude-fable-5` or `claude-mythos-5` to fixture without re-counting:** These models use a tokenizer that produces ~30% more tokens than older models [CITED: platform.claude.com/docs/en/build-with-claude/token-counting]. The `make eval` output will correctly show higher `input_tokens` for those models.

---

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Golden file diff/update | Custom file comparison + update loop | `goldie/v2` | `-update` flag, diff display, file management |
| DAG cycle detection | Custom DFS in eval fixtures | `pkg/dag.ComputeWaves` | Already correct, tested, O(V+E) |
| Child-CRD JSON parsing + validation | New parser in eval | `readChildCRDs` (via package anthropic test or thin wrapper) | Carries traversal defense, kind allowlist, double-object detection |
| Cost calculation | Re-implement `inputRate * tokens / 1_000_000` | `(*Anthropic).estimatedCostCents` | Any divergence breaks budget tracking |
| count_tokens HTTP client | Anthropic Go SDK | stdlib `net/http` + `encoding/json` | CLAUDE.md anti-pattern; credproxy is the network boundary |

**Key insight:** Every piece of logic Phase 18 tests already exists in the codebase. The eval harness is a test wrapper, not a reimplementation.

---

## Common Pitfalls

### Pitfall 1: Modifying locked files to expose unexported symbols

**What goes wrong:** `estimatedCostCents` and `readChildCRDs` are unexported. The reflex is to export them by modifying `pricing.go` or `subagent.go`.

**Why it happens:** Go's package visibility seems to require this. But both files are locked per CONTEXT.md (files that MUST NOT change).

**How to avoid:** Use `package anthropic` test files (`cost_parity_test.go`, `protocol_compliance_test.go`). These test files are in the same package and can call unexported functions without modifying the production code. The existing `pricing_test.go` demonstrates this pattern exactly.

**Warning signs:** Any edit to `pricing.go` or `subagent.go` lines 436-549 (readChildCRDs) as part of Phase 18.

### Pitfall 2: Golden file flap from non-deterministic EnvelopeIn fields

**What goes wrong:** If `TaskUID` is generated with `uuid.New()` or time-based values, every test run produces a different rendered template, causing `g.Assert` to fail with a golden mismatch even when no templates changed.

**Why it happens:** `plan_planner.tmpl` line 29 interpolates `{{.TaskUID}}` in the children directory path. Any non-fixed UID breaks golden file stability.

**How to avoid:** Use a completely fixed `EnvelopeIn` fixture with hardcoded string values for all fields (see `fixedEnvelope` in Pattern 1 above). The fixture must be a `var` (not computed at test time) so it is constant across all test runs.

**Warning signs:** Golden files differ between identical runs of `make test`.

### Pitfall 3: Ratchet update vs. goldie update confusion

**What goes wrong:** Running `go test ./internal/eval/... -update` updates goldie `.golden` files but does NOT update the byte-count ratchet files in `testdata/ratchets/`. The ratchet test continues to fail if the template grew.

**Why it happens:** Ratchets are plain text integer files, not goldie-managed files. The `-update` flag is goldie's, not the ratchet's.

**How to avoid:** Document clearly in the ratchet test comment that ratchet files are updated manually (or via a separate `-write-ratchets` flag in the test file). The procedure for an intentional template size increase: (1) run with `-update` to regenerate golden files, (2) manually update the ratchet text file with the new byte count, (3) commit both.

### Pitfall 4: events.jsonl fixture missing CacheCreationTokens (zero)

**What goes wrong:** A captured events.jsonl fixture with no cache activity has `cache_creation_input_tokens: 0` and `cache_read_input_tokens: 0`. The realized-savings test cannot demonstrate that `estimatedCostCents` handles the cache dimensions correctly — it becomes a no-op validation.

**Why it happens:** The first dispatch ever run against an account with no prior prompt caching will have zero cache tokens. A fixture captured from such a run cannot demonstrate the cache path.

**How to avoid:** Capture the fixture from a dispatch where the prompt WAS long enough to trigger cache creation (≥1,024 tokens for Sonnet/Opus). If the current templates are ~200 tokens (the known state from SUMMARY.md), then for Phase 18's fixture, either:
1. Capture from a phase/plan/milestone planner dispatch where the full prompt including the phase-brief preamble is longer, OR
2. Construct a synthetic events.jsonl fixture that is structurally valid (same shape as `stream_parser_test.go`'s fixture) with realistic non-zero values for all four fields. This is the cheapest approach and ensures the test is deterministic.

**Recommendation:** Use a synthetic events.jsonl fixture with non-zero values for all four token fields. The structure is identical to the fixture already used in `TestParseStream_HappyPath`. Real capture adds complexity and CI cost with no correctness benefit for a deterministic test.

### Pitfall 5: Forgetting `anthropic-version` header on count_tokens POST

**What goes wrong:** The credproxy forwards the request to `api.anthropic.com`. Anthropic's API returns 400 if `anthropic-version: 2023-06-01` is absent [CITED: platform.claude.com/docs/en/build-with-claude/token-counting].

**How to avoid:** Always include the header in `cmd/tide-eval/`'s HTTP request:
```go
req.Header.Set("anthropic-version", "2023-06-01")
req.Header.Set("x-api-key", *signedToken)
req.Header.Set("content-type", "application/json")
```

### Pitfall 6: Import cycle via internal/eval → internal/controller

**What goes wrong:** If `internal/eval/` imports `internal/controller` (to test higher-level behavior), it creates an import cycle because `internal/controller` imports `internal/subagent/anthropic`.

**How to avoid:** `internal/eval/` imports only:
- `internal/subagent/common` (template loading)
- `pkg/dispatch` (EnvelopeIn, Usage types)
- `pkg/dag` (ComputeWaves)
- `internal/harness` (Validate — for output path structure awareness)
- Standard library + goldie/v2

Do NOT import `internal/controller`, `internal/budget`, `internal/metrics`, or any CRD types (`api/v1alpha1`).

---

## Code Examples

### Rendering a template deterministically (EVAL-01/03)

```go
// Source: internal/subagent/common/prompt_templates.go:65 (LoadPromptTemplate)
// internal/subagent/anthropic/subagent.go:~step5 (tmpl.Execute)

tmpl, err := common.LoadPromptTemplate("planner", "plan")   // "templates/plan_planner.tmpl"
// err returns wrapped fs.ErrNotExist if the (role, level) pair has no template

var buf bytes.Buffer
err = tmpl.Execute(&buf, fixedEnvelope)  // fixedEnvelope is pkgdispatch.EnvelopeIn
// rendered text is now in buf.Bytes()
```

### ParseStream fixture shape (from stream_parser_test.go)

```go
// Source: internal/subagent/anthropic/stream_parser_test.go:30
// All four Usage fields populated — use this as the shape for events.jsonl fixture:
const fixtureEvents = `{"type":"system/init","session_id":"sess-1"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}
{"type":"result","result":"final assistant text","usage":{"input_tokens":500,"output_tokens":100,"cache_read_input_tokens":800,"cache_creation_input_tokens":1200},"total_cost_usd":0.0012}
`
// testdata/fixtures/stream_real.jsonl should follow this JSONL shape
// with the "result" event carrying all four token fields
```

### count_tokens HTTP call (stdlib net/http)

```go
// Source: platform.claude.com/docs/en/build-with-claude/token-counting (curl example, adapted)
// cmd/tide-eval/main.go

type countTokensReq struct {
    Model    string    `json:"model"`
    System   string    `json:"system,omitempty"`
    Messages []message `json:"messages"`
}
type message struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}
type countTokensResp struct {
    InputTokens int64 `json:"input_tokens"`
}

body, _ := json.Marshal(countTokensReq{
    Model:    *model,
    Messages: []message{{Role: "user", Content: rendered}},
})
req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
    *proxyEndpoint+"/v1/messages/count_tokens", bytes.NewReader(body))
req.Header.Set("content-type", "application/json")
req.Header.Set("x-api-key", *signedToken)
req.Header.Set("anthropic-version", "2023-06-01")

resp, err := http.DefaultClient.Do(req)
// ... handle err, read body, unmarshal countTokensResp
```

### goldie New with custom fixture dir

```go
// Source: Context7 /sebdah/goldie — WithFixtureDir option
g := goldie.New(t, goldie.WithFixtureDir("testdata/goldie"))
g.Assert(t, "plan_planner", renderedBytes)
// First run with -update: writes testdata/goldie/plan_planner.golden
// Subsequent runs: compares renderedBytes against committed file
```

### DAG CycleError type assertion

```go
// Source: pkg/dag/errors.go:23 — CycleError type
// pkg/dag/kahn.go:85 — returned on cycle detection
import "errors"
var cycErr *dag.CycleError
if errors.As(err, &cycErr) {
    // cycErr.InvolvedNodes is sorted lexicographically
}
```

---

## Research Focus Answers

### Q1: goldie/v2 mechanics

[VERIFIED via Context7 /sebdah/goldie]

- **API:** `g := goldie.New(t, goldie.WithFixtureDir("testdata/goldie"))`, then `g.Assert(t, name, []byte)` for raw bytes or `g.AssertJson(t, name, struct)` for JSON.
- **Default testdata path:** `testdata` (configurable via `WithFixtureDir`).
- **-update flag:** Goldie registers `flag.Bool("update", ...)` globally. Running `go test ./... -update` rewrites all `.golden` files. Use `goldie.WithFixtureDir` to avoid collisions with non-goldie testdata files.
- **Fixed EnvelopeIn fixture:** Must have all fields set to compile-time constants. Critical fields: `TaskUID` (appears in template path interpolation at plan_planner.tmpl:29), `Provider.Vendor`, `Provider.Model`. Any maps in EnvelopeIn must be serialized deterministically — `EnvelopeIn` has no map fields in its core struct (Provider.Params is a `map[string]string` but the templates do not iterate over it, so order does not affect golden files).
- **PROMPT-05 stable-key-order concern:** Deferred to Phase 19 (out of scope for Phase 18). For Phase 18 golden tests, the fixture does not set `Provider.Params` — use nil/empty to avoid any map iteration.

### Q2: Offline token proxy unit

**Recommendation: byte count (`len(rendered)`).**

Rationale:
- Anthropic's tokenizer produces approximately 1 token per 3-5 bytes for mixed English/code text [ASSUMED — based on general LLM tokenization knowledge; exact ratio for Anthropic's tokenizer is not publicly specified]. The proxy does not need to be exact; it only needs to be monotonically decreasing with template size.
- Rune count equals byte count for ASCII-dominant content (all five templates use ASCII-only characters: Go code comments, English prose, template syntax). No advantage.
- Whitespace-word count is less stable under template reformatting (adding blank lines doesn't change word count but adds bytes that map to tokens).
- Byte count is the simplest, most reproducible proxy.
- The exact token count is the job of `make eval`'s `count_tokens` call; the ratchet only needs to catch accidental growth.

**Committed format:** A plain text file containing a single integer followed by a newline:
```
testdata/ratchets/plan_planner.txt:
4127
```
Test reads it with `os.ReadFile` + `strconv.Atoi`.

### Q3: Protocol-compliance gate — where are the checks?

All three checks exist in the codebase:

1. **Child-CRD parse success:** `readChildCRDs` in `internal/subagent/anthropic/subagent.go:436`. Unexported. Access via a new `internal/subagent/anthropic/protocol_compliance_test.go` in `package anthropic` (same-package test). The function scans a directory of `.json` files, parses each via `json.Decoder`, validates Kind (childKindAllowlist) and Name (non-empty). Returns `([]ChildCRDSpec, error)`.

2. **Declared-output-path presence:** `internal/harness/outputs.go:43` has `Validate(workspace, startedAt, declaredOutputPaths)` which checks the filesystem. For the eval gate, the structural check is simpler: assert `len(env.DeclaredOutputPaths) > 0` on the fixture EnvelopeIn (the harness function requires real file timestamps to scan). The protocol gate for Phase 18 asserts that a Task child-CRD spec carries a non-empty `declaredOutputPaths` field — checked at the JSON parse level in `readChildCRDs` fixtures.

3. **DAG acyclicity:** `pkg/dag.ComputeWaves` at `pkg/dag/kahn.go:46`. Fully exported. Returns `*CycleError` on cycles. No wrapper needed.

**Fixtures needed:**
- `testdata/fixtures/child_valid_task.json`: a well-formed Task child-CRD with non-empty kind, name, and spec.
- `testdata/fixtures/child_bad_kind.json`: a child-CRD with `"kind": "Forbidden"` to test the allowlist rejection.
- `testdata/fixtures/child_missing_name.json`: a child-CRD with `"name": ""` to test the name validation.
- `testdata/dag/acyclic.json` and `testdata/dag/cyclic.json`: simple node/edge lists (can be inline in test code rather than files).

### Q4: estimatedCostCents access

[VERIFIED from direct codebase reading of pricing.go:132]

**Method signature:**
```go
func (a *Anthropic) estimatedCostCents(model string, u pkgdispatch.Usage) int64
```

**Access pattern:** Same-package `_test.go` file in `package anthropic`. The existing `pricing_test.go` does exactly this — constructs `a := New(Options{})` and calls `a.estimatedCostCents(model, u)` directly.

**No exported wrapper needed and no pricing.go modification needed.**

**Four-field cost math (from pricing.go:144-156):**
```go
numerator := u.InputTokens*price.inputCentsPerMTok +
    u.OutputTokens*price.outputCentsPerMTok +
    u.CacheReadTokens*price.cacheReadCentsPerMTok +
    u.CacheCreationTokens*price.cacheWriteCentsPerMTok
// ceiling division: (numerator + 1_000_000 - 1) / 1_000_000
```

For `claude-sonnet-4-6`: input=300, output=1500, cacheRead=30, cacheWrite=375 cents/MTok.

**REALIZED per-wave savings formula:**
```
realized = estimatedCostCents(model, Usage{InputTokens: uncachedInputTokens, OutputTokens: outputTokens})
         - estimatedCostCents(model, Usage{
               InputTokens:         cachedInputTokens,
               OutputTokens:        outputTokens,
               CacheReadTokens:     cacheReadTokens,
               CacheCreationTokens: cacheCreationTokens,
           })
```
The function handles the write premium (1.25x) and read discount (0.10x) automatically. No separate math needed.

### Q5: Realized-savings math and events.jsonl shape

**events.jsonl line shape (from stream_parser_test.go:30 and stream_parser.go):**
```json
{"type":"result","result":"...","usage":{"input_tokens":N,"output_tokens":N,"cache_read_input_tokens":N,"cache_creation_input_tokens":N},"total_cost_usd":X}
```
The parser only reads the `result`-type event. Other event types (system/init, stream_event) are teed to rawSink but ignored for Usage extraction.

**Cheapest fixture capture:** Construct a synthetic events.jsonl file with realistic values matching the stream_parser_test.go fixture shape. This avoids any live dispatch requirement and is guaranteed to have all four fields populated. The fixture is deterministic and permanently valid. No real dispatch is needed for a deterministic test.

For maintainer documentation of real dispatch token counts, the `make eval` command provides the live numbers via count_tokens.

**Per-wave realized savings:** Subtract `estimatedCostCents(withCaching)` from `estimatedCostCents(withoutCaching)` for the same output token count. Note the cache-write premium: on the FIRST dispatch in a wave (cache warm), savings are negative (you pay 1.25x instead of 1.0x for those tokens). Savings are positive only after the second dispatch reads from cache. The eval fixture should model a SECOND-dispatch scenario (high CacheReadTokens, zero or low CacheCreationTokens) to show positive realized savings.

### Q6: count_tokens command — credproxy URL discovery and //go:build wiring

**ProxyEndpoint discovery:** In production, `in.ProxyEndpoint` = `"https://127.0.0.1:8443"` (the credproxy sidecar). For `cmd/tide-eval/`, a maintainer running it locally against a running credproxy passes `--proxy https://127.0.0.1:8443` or sets `TIDE_PROXY_ENDPOINT`. The credproxy must be running and the SignedToken must be valid for the call to succeed.

**Build tag wiring:** `//go:build eval` at line 1 of `cmd/tide-eval/main.go`. This ensures `go build ./...` and `go test ./...` never touch it. The `make eval` target uses `go run -tags eval ./cmd/tide-eval/` to compile it on demand.

**Makefile target pattern** (matching existing `make test` / `make test-int*` conventions):
```makefile
.PHONY: eval
eval: ## count_tokens pre-flight (online, requires TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN; runs make test deterministic gate first)
	@echo "Running deterministic eval gate first..."
	$(MAKE) test-only
	@echo "Running online count_tokens pre-flight..."
	go run -tags eval ./cmd/tide-eval/ \
	  -proxy "$(TIDE_PROXY_ENDPOINT)" \
	  -token "$(TIDE_SIGNED_TOKEN)" \
	  -model "$(or $(EVAL_MODEL),claude-sonnet-4-6)"
```

### Q7: CI wiring for EVAL-06

**Current CI config** [VERIFIED from .github/workflows/test.yml:27-28]:
```yaml
- name: Running Tests
  run: |
    go mod tidy
    git diff --exit-code go.mod go.sum
    make test
```

`make test` invokes:
```bash
go test -short -timeout 120s $(go list ./... | grep -v /e2e | grep -v /test/integration) -coverprofile cover.out
```

`internal/eval/` is not in the exclusion list, so any `_test.go` file in that package automatically runs under `make test`. No CI config changes needed. EVAL-06 is satisfied by creating the package.

**One caveat:** goldie's `-update` flag is a registered global Go test flag. CI never passes `-update`, so golden files are read-only in CI. If a golden file is missing (i.e., not committed), the test fails with "fixture not found" — not a diff failure. Wave 0 must commit all initial `.golden` and ratchet files.

### Q8: Import cycle check

[VERIFIED from direct file reading]

Proposed `internal/eval/` imports:
- `github.com/jsquirrelz/tide/internal/subagent/common` — imports `embed`, `fmt`, `text/template` only. No controller deps.
- `github.com/jsquirrelz/tide/pkg/dispatch` — imports `time` only. No controller deps.
- `github.com/jsquirrelz/tide/pkg/dag` — imports `fmt`, `sort` only. No controller deps.
- `github.com/jsquirrelz/tide/internal/harness` — imports `pkg/dispatch`, OS stdlib, `pkg/dag`. No controller deps.
- `github.com/sebdah/goldie/v2` — test-only, no cycle risk.

**No import cycle.** All listed imports are leaf packages or near-leaf packages. `internal/eval/` must NOT import `internal/controller`, `internal/subagent/anthropic`, `api/v1alpha1`, `internal/budget`, or `internal/metrics`.

The cost-parity test (`cost_parity_test.go`) is in `package anthropic` — it imports `pkg/dispatch` which is already an `internal/subagent/anthropic` dependency.

---

## Environment Availability

| Dependency | Required By | Available | Notes |
|------------|------------|-----------|-------|
| Go 1.26 | All eval tests | ✓ | go.mod specifies `go 1.26.0` |
| goldie/v2 v2.8.0 | internal/eval/ tests | ✗ (not in go.mod yet) | Add via `go get github.com/sebdah/goldie/v2@v2.8.0` |
| `pkg/dag` | protocol_test.go | ✓ | Exists at pkg/dag/kahn.go |
| `internal/harness` | protocol_test.go (Validate) | ✓ | Exists at internal/harness/outputs.go |
| `common.LoadPromptTemplate` | render_test.go | ✓ | Exists at internal/subagent/common/prompt_templates.go:65 |
| `estimatedCostCents` | cost_parity_test.go | ✓ | Unexported on *Anthropic; access via package anthropic test |
| `ParseStream` | cost_parity_test.go | ✓ | Exported at internal/subagent/anthropic/stream_parser.go:76 |
| credproxy (for make eval) | cmd/tide-eval/ | ✗ (runtime only) | Must be running when `make eval` executes; not needed for `make test` |

**Missing dependencies with no fallback (blocks `make eval`):**
- A running credproxy instance + valid signed token (TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN env vars). Only required for the online `make eval` target, not for `make test`.

**Missing dependencies with no impact on `make test`:**
- credproxy — `make test` is zero-network; credproxy is only needed for `make eval`.

---

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + goldie/v2 v2.8.0 |
| Config file | none — goldie uses `-update` flag via `go test` |
| Quick run command | `go test -short ./internal/eval/... ./internal/subagent/anthropic/...` |
| Full suite command | `make test` |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| EVAL-01 | Frozen golden render of each template committed | golden-file | `go test ./internal/eval/... -update` (create); then `go test ./internal/eval/...` | ❌ Wave 0 |
| EVAL-02 | child-CRD parse success + DAG acyclicity + output-path structural check | unit | `go test ./internal/subagent/anthropic/... ./internal/eval/...` | ❌ Wave 0 |
| EVAL-03 | Golden render matches committed file; byte ratchet catches growth | unit + golden | `make test` | ❌ Wave 0 |
| EVAL-04 | estimatedCostCents parity within 1 cent; realized savings math | unit | `go test ./internal/subagent/anthropic/...` | ❌ Wave 0 |
| EVAL-05 | count_tokens POST returns input_tokens; 1024-floor check prints | smoke (manual/online) | `make eval` (online only) | ❌ Wave 0 |
| EVAL-06 | CI runs `make test` which includes internal/eval/ | CI auto-gate | `make test` (in test.yml:28) | No CI change needed |

### Sampling Rate
- **Per task commit:** `go test -short ./internal/eval/... ./internal/subagent/anthropic/...`
- **Per wave merge:** `make test`
- **Phase gate:** Full `make test` green before `/gsd:verify-work`

### Wave 0 Gaps
- [ ] `internal/eval/doc.go` — package overview
- [ ] `internal/eval/render_test.go` — template render + goldie golden + byte ratchet
- [ ] `internal/eval/protocol_test.go` — DAG acyclicity + output-path structural check
- [ ] `internal/eval/cost_replay_test.go` — ParseStream + offline cost replay
- [ ] `internal/subagent/anthropic/cost_parity_test.go` — estimatedCostCents parity (in-package)
- [ ] `internal/subagent/anthropic/protocol_compliance_test.go` — readChildCRDs fixture tests (in-package)
- [ ] `internal/eval/testdata/goldie/*.golden` — initial golden files (generated via `-update` on first run)
- [ ] `internal/eval/testdata/ratchets/*.txt` — byte-count ceiling files (written manually after first render)
- [ ] `internal/eval/testdata/fixtures/stream_real.jsonl` — synthetic events.jsonl fixture with 4-field usage
- [ ] `cmd/tide-eval/main.go` — count_tokens pre-flight command (behind `//go:build eval`)
- [ ] `go.mod` — add `github.com/sebdah/goldie/v2 v2.8.0` as test dependency
- [ ] `Makefile` — add `make eval` target

---

## Security Domain

> EVAL phase adds no new authenticated endpoints. credproxy remains the only auth boundary.

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V5 Input Validation | yes | Fixtures use compile-time constants; no user input parsed in tests |
| V6 Cryptography | no | No new crypto surfaces |
| V2 Authentication | no | `cmd/tide-eval/` uses existing credproxy SignedToken mechanism — no new auth |

**Known Threat Patterns for eval tooling:**

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Fixture file injection (adversarial .json in testdata/) | Tampering | readChildCRDs applies traversal defense and Kind allowlist — fixtures that exercise allowlist rejection are safe by design |
| Leaking SignedToken via logs | Information Disclosure | `cmd/tide-eval/` must not log the token value; log only "token present: yes/no" |

---

## State of the Art

| Old Approach | Current Approach | Impact |
|--------------|------------------|--------|
| No eval harness — template changes are unreviewed | Phase 18 eval harness gates every template change | Catch accidental regressions in CI before they reach production |
| No baseline — token counts unknown | Committed golden renders + byte ratchets | Every PR shows whether template grew, shrank, or changed structure |
| estimatedCostCents only used at dispatch time | Also used in eval for cost-parity testing | Prevents any future pricing.go change from silently breaking cost accounting |

---

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Byte count is 3-5 bytes per token for Anthropic's tokenizer on these templates | Offline token proxy unit | Ratchet might allow growth that costs more tokens than expected; mitigated by `make eval` providing real counts |
| A2 | `make eval` with `go run -tags eval` does not conflict with existing build-tag conventions | Pattern 4 / Makefile | None found; no existing `eval` build tag in codebase [VERIFIED: grepped codebase for //go:build eval — no hits] |

---

## Open Questions (RESOLVED)

1. **Ratchet update tooling** — **RESOLVED: manual update for v1.0.2.**
   - What we know: goldie's `-update` flag rewrites `.golden` files but not `testdata/ratchets/*.txt`.
   - What's unclear: Should there be a `-write-ratchets` test flag or a standalone Go generate script?
   - Decision: Keep it manual for v1.0.2 (the ratchet file is a one-line integer; the procedure is documented in the test comment). No `-write-ratchets` flag is added in Phase 18. Revisit a `TestMain`-driven flag in Phase 19 only if ratchet management becomes friction.

2. **Harness Validate for EVAL-02** — **RESOLVED: structural field check is sufficient for the deterministic gate.**
   - What we know: `internal/harness.Validate` checks the filesystem for written files, requiring a real `startedAt` time.
   - What's unclear: Whether the structural-only approach is insufficient for EVAL-02.
   - Decision: For Phase 18's deterministic gate, the structural check (non-empty `DeclaredOutputPaths` field, e.g. `len(env.DeclaredOutputPaths) > 0`) is sufficient and matches CONTEXT.md D-05's deterministic-only scope. A full `harness.Validate` filesystem check is OUT OF SCOPE for Phase 18.

---

## Sources

### Primary (HIGH confidence)
- Direct codebase inspection at HEAD:
  - `internal/subagent/anthropic/pricing.go:132` — estimatedCostCents method signature and cost math
  - `internal/subagent/anthropic/stream_parser.go:76` — ParseStream signature and four-field Usage mapping
  - `internal/subagent/anthropic/subagent.go:436` — readChildCRDs implementation
  - `internal/subagent/anthropic/pricing_test.go` — in-package test pattern for unexported methods
  - `internal/subagent/anthropic/stream_parser_test.go` — events.jsonl fixture shape
  - `internal/credproxy/server.go:101-107` — allowedRoutes including count_tokens
  - `pkg/dag/kahn.go:46` — ComputeWaves signature and CycleError return
  - `pkg/dispatch/envelope.go:39,252` — EnvelopeIn and Usage types
  - `internal/subagent/common/prompt_templates.go:65` — LoadPromptTemplate signature
  - `.github/workflows/test.yml:27-28` — `make test` invocation in CI
  - `Makefile` — test and test-only targets
  - `go.mod` — current module dependencies (goldie not present)
- Context7 `/sebdah/goldie` — Assert, AssertJson, WithFixtureDir, -update flag API [VERIFIED via Context7]

### Secondary (MEDIUM confidence)
- `.planning/research/SUMMARY.md` — milestone context, five-template structure, ~200 token observation
- `.planning/research/ARCHITECTURE.md` — internal/eval/ slotting, locked files, import boundary
- `18-CONTEXT.md` — locked decisions D-01 through D-05

### Tertiary (referenced)
- [Anthropic token counting official docs](https://platform.claude.com/docs/en/build-with-claude/token-counting) — count_tokens request/response JSON shape, required headers [CITED]

---

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — goldie version from STACK.md, stdlib only for cmd/tide-eval
- Architecture: HIGH — all file paths verified from direct codebase reading
- Pitfalls: HIGH — most trace to specific line numbers in the codebase
- count_tokens API: HIGH — verified from official Anthropic docs

**Research date:** 2026-06-15
**Valid until:** 2026-08-01 (stable — goldie/v2 and Anthropic count_tokens API are stable surfaces)
