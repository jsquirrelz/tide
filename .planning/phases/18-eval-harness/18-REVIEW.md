---
phase: 18-eval-harness
reviewed: 2026-06-15T00:00:00Z
depth: standard
files_reviewed: 8
files_reviewed_list:
  - Makefile
  - cmd/tide-eval/main.go
  - internal/eval/cost_replay_test.go
  - internal/eval/doc.go
  - internal/eval/protocol_test.go
  - internal/eval/render_test.go
  - internal/subagent/anthropic/cost_parity_test.go
  - internal/subagent/anthropic/protocol_compliance_test.go
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 18: Code Review Report

**Reviewed:** 2026-06-15
**Depth:** standard
**Files Reviewed:** 8
**Status:** issues_found

## Summary

The Phase 18 eval harness was reviewed against the stated deliverable: goldie golden-render + byte-ratchet tests, deterministic protocol-compliance + cost-parity tests, and a `cmd/tide-eval` count_tokens preflight behind `//go:build eval`.

The security-sensitive items the prompt flagged hold up: `cmd/tide-eval/main.go` never logs the signed token value (only `token present: true`), fails closed via `requireFlag` when either credential is absent, and the Makefile `eval` target re-guards both env vars before invoking. The `//go:build eval` tag is correctly applied (line 1, blank line before the license block) so the online tool stays out of `make test`. Goldie/ratchet determinism is sound: the five templates contain no `range`/map iteration, `Provider.Params` is nil, and the fixtures are byte-stable.

The substantive defects are correctness/validity, not security. The most important: the eval fixture renders the **executor** template (`task_executor.tmpl`, which interpolates `{{.Role}}`/`{{.Level}}`) using a `planner`/`plan` envelope, so the golden file, the byte ratchet, and the online count_tokens floor for `task_executor` all measure a body production never sends. Separately, `doc.go` and the online tool claim to measure "the actual billed token count," but production routes the rendered template through the Claude Code CLI (stdin), which wraps it in its own system prompt + tool schemas before building `/v1/messages` — so the tool measures a template-only floor, not billed input. The cost-parity arithmetic was verified against `pricing.go` and is correct.

## Warnings

### WR-01: Executor template rendered with a planner/plan fixture — golden, ratchet, and token-floor are non-representative

**File:** `internal/eval/render_test.go:37-50, 93-95` and `cmd/tide-eval/main.go:71-83, 125-126`
**Issue:** `task_executor.tmpl` interpolates `{{.Level}}` and `{{.Role}}` (template lines 9-10). Both the golden/ratchet harness and the online tool render *all five* templates — including the executor one — with the single `fixedEnvelope` whose `Role: "planner"` and `Level: "plan"`. The `task_executor` golden therefore contains `Level: plan` / `Role: planner`, a combination that never occurs in production (executor dispatches are `Role: "executor"`, `Level: "task"`). Consequences: (a) the committed `task_executor.golden` and its `1960`-byte ratchet ceiling are pinned to a body that does not reflect real executor renders; (b) `make eval`'s count_tokens floor for `task_executor` measures the wrong body. A future template change that grows the executor prompt *only* on the `Role`/`Level` lines for real values could pass the ratchet while regressing production. The render is deterministic, so this is a representativeness/correctness defect, not a flake.
**Fix:** Use a per-case fixture so each template is rendered with a role/level matching the production dispatch shape. Minimal change — derive the envelope from the case:
```go
func envelopeFor(role, level string) pkgdispatch.EnvelopeIn {
    e := baseEnvelope        // shared deterministic constants
    e.Role = role
    e.Level = level
    return e
}
// goldenAssert / ratchetAssert / countTokens then render with envelopeFor(role, level)
```
Regenerate the `task_executor` golden + ratchet after the fix.

### WR-02: `doc.go` and tool output overclaim "actual billed token count"

**File:** `internal/eval/doc.go:26-29`, `cmd/tide-eval/main.go:19-25`
**Issue:** Both the package doc and the command doc state the tool measures "the actual billed token count for each rendered template." Production (`internal/subagent/anthropic/subagent.go:285-296`) delivers the rendered template to the Claude Code CLI via **stdin**; the CLI constructs the real `/v1/messages` request, prepending its own system prompt, tool definitions, and conversation scaffolding. `cmd/tide-eval/main.go:162-168` instead POSTs the bare template as a single `user` message with `System: ""`. The reported count is therefore a *lower-bound floor on the template's contribution*, not the billed input. This matters because the 1,024-token cache-floor PASS/FAIL (`main.go:132-138`) is evaluated against this floor: a template could clear the real cache floor in production while the tool reports FAIL, or vice-versa, leading an operator to mis-trim templates. (Note: the v1.0.2 milestone finding that "templates ~200 tokens < cache minimum so caching never fires" is exactly the kind of conclusion this tool is meant to inform — so the floor-vs-billed gap is load-bearing.)
**Fix:** Soften the doc to "real token count of the rendered template body (a floor on billed input; the CLI adds its own system prompt and tool schemas on top)." If a closer estimate is wanted, add a representative fixed system-prompt/tools stanza to the count_tokens request so the floor tracks production more tightly.

### WR-03: `ratchetAssert` only fails on growth — silent shrink leaves a stale, loose ceiling

**File:** `internal/eval/render_test.go:167-171`
**Issue:** The ratchet asserts `actual > ceiling` only. If a template is trimmed (the explicit Phase 18→later goal per `doc.go:34-36`), the rendered count drops well below the frozen ceiling and the test still passes, leaving a ceiling far looser than the current size. The ratchet then permits silent re-growth all the way back up to the old ceiling without any test catching it — defeating the "ratchet down after trimming lands" intent. The ceiling is currently exactly equal to the byte count (verified: all five ratchets equal their golden sizes), so the ratchet is tight *today*, but nothing enforces that it stays tight.
**Fix:** Either (a) emit a non-fatal `t.Logf` when `actual` is materially below `ceiling` to prompt a maintainer to re-tighten, or (b) make it a strict equality ratchet (`actual != ceiling`) so any change — grow or shrink — forces an explicit ceiling update with a deliberate commit. Option (b) matches the "frozen byte count" framing in `doc.go` more faithfully.

### WR-04: `http.Client` has no timeout — `make eval` can hang indefinitely

**File:** `cmd/tide-eval/main.go:121`
**Issue:** `client := &http.Client{}` uses no `Timeout`. If the credproxy accepts the connection but stalls (slow upstream, half-open socket, hung TLS handshake against `https://127.0.0.1:8443`), `client.Do` blocks forever and `make eval` never returns. `countTokens` already builds the request with `context.Background()` (`main.go:174`), which also carries no deadline, so there is no cancellation path. This is a maintainer tool, but a hung preflight in a release ritual is a real footgun.
**Fix:**
```go
client := &http.Client{Timeout: 30 * time.Second}
```
and/or replace `context.Background()` with a `context.WithTimeout` derived context passed into `countTokens`.

## Info

### IN-01: Two divergent `fixedEnvelope` definitions risk drift

**File:** `internal/eval/render_test.go:37-50` and `cmd/tide-eval/main.go:71-83`
**Issue:** The golden/ratchet fixture (`render_test.go`) and the online tool fixture (`main.go`) are separate literals. They currently agree field-for-field (one uses `pkgdispatch.KindTaskEnvelopeIn`, the other the `"TaskEnvelopeIn"` literal — same value), but they can silently diverge, at which point the offline ratchet would no longer correspond to the body the online tool measures. The two are meant to be the same fixture.
**Fix:** Hoist a single exported fixture (e.g. in a small `evalfixture` package or an exported var the `eval`-tagged tool imports) so both render paths share one source of truth.

### IN-02: `model` flag and fixture `Provider.Model` can disagree

**File:** `cmd/tide-eval/main.go:106, 126, 79-82`
**Issue:** `-model` (default `claude-sonnet-4-6`) is sent to count_tokens, while `fixedEnvelope.Provider.Model` is independently hardcoded to `claude-sonnet-4-6`. Templates interpolate `{{.Provider.Model}}` (e.g. `task_executor.tmpl:13`). If an operator passes `-model claude-opus-...`, the count_tokens request uses the opus tokenizer but the rendered body still says `Provider.Model: claude-sonnet-4-6`. Minor, but the two model knobs should move together.
**Fix:** Set `fixedEnvelope.Provider.Model = *model` before rendering, or document that `-model` only selects the tokenizer and does not affect the rendered body.

### IN-03: `TestDeclaredOutputPaths_Presence` is a tautology, not a protocol gate

**File:** `internal/eval/protocol_test.go:68-84`
**Issue:** The test constructs an `EnvelopeIn` with `DeclaredOutputPaths: []string{"..."}` and asserts `len(...) != 0`, then constructs one with `[]string{}` and asserts `len(...) == 0`. It re-asserts the literals it just wrote and exercises no production validation logic — it cannot catch a regression in any harness/webhook check (the comment even notes `harness.Validate` is out of scope). It is dead-weight coverage masquerading as a "structural protocol gate."
**Fix:** Either call the real validation path (e.g. the webhook/harness check that enforces non-empty `DeclaredOutputPaths`) so the test guards actual behavior, or drop it and rely on the DAG-acyclicity gates that do exercise `pkg/dag.ComputeWaves`.

### IN-04: `count_tokens` request always sends `System: ""` via `omitempty` — relies on undocumented API tolerance

**File:** `cmd/tide-eval/main.go:88, 164`
**Issue:** `System string \`json:"system,omitempty"\`` with `System: ""` serializes the field away entirely. This is fine for the current API, but combined with WR-02 it cements the "no system prompt" floor. Worth a comment that the empty system is deliberate (template-floor measurement), so a future maintainer doesn't "fix" it by injecting a system prompt and silently shift every reported count.
**Fix:** Add a one-line comment at `main.go:164` noting the empty system is intentional and what it measures.

### IN-05: Misleading comment — `Provider.Params is nil to avoid map-iteration ordering` but no template iterates Params

**File:** `cmd/tide-eval/main.go:69-70`, `internal/eval/render_test.go:48`
**Issue:** Both fixtures justify `Params: nil` as avoiding "map-iteration ordering non-determinism." Verified: none of the five templates range over `Provider.Params` (no `{{range}}` over a map anywhere in `templates/`). The comment describes a hazard that does not exist for these templates, which could mislead a maintainer into thinking map-ordering protection is load-bearing here when it is purely defensive.
**Fix:** Reword to "Params nil keeps the fixture minimal; if a future template iterates Params, sort keys before rendering to keep goldens deterministic."

---

_Reviewed: 2026-06-15_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
