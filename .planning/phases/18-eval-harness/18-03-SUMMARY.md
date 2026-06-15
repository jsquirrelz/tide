---
phase: 18-eval-harness
plan: "03"
subsystem: eval
tags: [eval, count_tokens, credproxy, build-tag, makefile]
dependency_graph:
  requires: []
  provides: [cmd/tide-eval, make-eval-target]
  affects: [Makefile]
tech_stack:
  added: []
  patterns: [stdlib-net-http, go-build-tag, makefile-credential-guard]
key_files:
  created:
    - cmd/tide-eval/main.go
  modified:
    - Makefile
decisions:
  - "//go:build eval tag on line 1 (no blank line before package) keeps cmd/tide-eval out of default go build ./... and make test"
  - "requireFlag helper (modeled on credproxy) fails closed with os.Exit(1) when TIDE_PROXY_ENDPOINT or TIDE_SIGNED_TOKEN absent"
  - "Token passed only to req.Header.Set; printed only as 'token present: true' — never the value"
  - "Fixed EnvelopeIn fixture with compile-time constants; Provider.Params nil to avoid map-iteration non-determinism"
  - "Makefile eval target placed in Development section after test-e2e-live, mirroring its credential-guard pattern"
metrics:
  duration: "~20 minutes"
  completed: "2026-06-15"
  tasks_completed: 2
  files_created: 1
  files_modified: 1
---

# Phase 18 Plan 03: count_tokens Pre-flight Command Summary

count_tokens pre-flight maintainer tool (`cmd/tide-eval`) behind `//go:build eval`, invoked by a single new `make eval` Makefile target, POSTs all five rendered templates to the credproxy's allowlisted `/v1/messages/count_tokens` via stdlib `net/http` (no SDK) and prints per-template real `input_tokens` plus 1,024-token cache-floor pass/fail.

## Tasks Completed

| Task | Name | Commit | Files |
|------|------|--------|-------|
| 1 | Build cmd/tide-eval count_tokens pre-flight command | 9c70339 | cmd/tide-eval/main.go (created) |
| 2 | Add the make eval target | 608b8b7 | Makefile (modified) |

## What Was Built

### Task 1: cmd/tide-eval/main.go

- `//go:build eval` on line 1 excludes the tool from `go build ./...` and `make test` (zero-network gate stays clean)
- Flag/env pattern mirrors `cmd/credproxy/main.go`: `flag.String` vars for `-proxy` (default `TIDE_PROXY_ENDPOINT`), `-token` (default `TIDE_SIGNED_TOKEN`), `-model` (default `claude-sonnet-4-6`)
- `requireFlag` helper fails closed with `os.Exit(1)` when either credential is absent (T-18-03-02)
- Iterates all five (role, level, name) template pairs using `common.LoadPromptTemplate` + `tmpl.Execute` with a fixed `pkgdispatch.EnvelopeIn` fixture
- POSTs rendered body to `{proxy}/v1/messages/count_tokens` with mandatory headers: `content-type: application/json`, `x-api-key: <token>`, `anthropic-version: 2023-06-01`
- Unmarshals `{"input_tokens": N}` response; prints `<name>: <N> tokens — cache-floor(1024): PASS/FAIL`
- Token value is NEVER logged or printed — only `"token present: true"` is reported at startup (T-18-03-01)
- Non-200 responses print status + body and exit 1
- Imports: stdlib only + `internal/subagent/common` + `pkg/dispatch` (no Anthropic SDK)

### Task 2: Makefile eval target

- `.PHONY: eval` with `##` help comment in the Development section (after `test-e2e-live`)
- Guards `TIDE_PROXY_ENDPOINT` and `TIDE_SIGNED_TOKEN`; exits 1 with descriptive error when either is absent
- Invokes `go run -tags eval ./cmd/tide-eval/` with `-proxy`, `-token`, and `-model` flags
- Supports optional `EVAL_MODEL` override (defaults to `claude-sonnet-4-6` via `$(or $(EVAL_MODEL),...)`)
- No `make test-unit` target added (D-02a constraint honored)

## Verification Results

- `go build ./...`: PASS (zero network gate unaffected)
- `go list ./cmd/tide-eval/`: empty (excluded from default build by `//go:build eval`)
- `go vet -tags eval ./cmd/tide-eval/`: PASS
- `make -n eval`: shows guarded `go run -tags eval` invocation
- `grep "anthropic-version" cmd/tide-eval/main.go`: PASS (mandatory header present)
- `grep "/v1/messages/count_tokens" cmd/tide-eval/main.go`: PASS
- No Anthropic SDK imports: confirmed via `go list -tags eval -f '{{range .Imports}}...'`
- Token only in `req.Header.Set`: confirmed — no print/log calls expose the value

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed countTokens function signature type mismatch**
- **Found during:** Task 1 (go vet -tags eval)
- **Issue:** Initial draft had `endpoint, token *string` in `countTokens` signature; the call site passed plain `string` values, causing a type mismatch
- **Fix:** Changed function signature to `endpoint, token, modelName, role, level string` (all plain strings); token is already dereferenced at the call site before being passed
- **Files modified:** cmd/tide-eval/main.go
- **Commit:** 9c70339 (fixed before commit)

**2. [Rule 3 - Blocking] Pre-existing demo-fixture build issue in worktree**
- **Found during:** Task 1 (`go build ./...` verification)
- **Issue:** `cmd/tide-demo-init/main.go:112: pattern all:fixture: no matching files found` — fixture directory not materialized in this worktree context
- **Fix:** Ran `make demo-fixture` to generate the fixture directory via `go generate ./cmd/tide-demo-init/...` (this is a known worktree setup step, not caused by this plan)
- **Files modified:** cmd/tide-demo-init/fixture/ (generated, gitignored)
- **Commit:** N/A (generated files are gitignored per Makefile comment)

## Known Stubs

None. The `DeclaredOutputPaths: []string{"internal/eval/testdata/placeholder.go"}` in the fixed fixture is an intentional protocol-compliant placeholder path (required by EnvelopeIn struct; the eval tool renders templates, not real task dispatches). It does not prevent the plan's goal from being achieved.

## Threat Flags

No new threat surface beyond what is documented in the plan's threat model:

| Flag | File | Description |
|------|------|-------------|
| T-18-03-01 (mitigated) | cmd/tide-eval/main.go | Signed token crosses cmd→credproxy boundary in x-api-key header; never logged/printed |
| T-18-03-02 (mitigated) | cmd/tide-eval/main.go + Makefile | Both tool and target fail closed when credentials absent |

## Self-Check: PASSED

| Check | Result |
|-------|--------|
| cmd/tide-eval/main.go exists | FOUND |
| 18-03-SUMMARY.md exists | FOUND |
| Commit 9c70339 (Task 1) exists | FOUND |
| Commit 608b8b7 (Task 2) exists | FOUND |
