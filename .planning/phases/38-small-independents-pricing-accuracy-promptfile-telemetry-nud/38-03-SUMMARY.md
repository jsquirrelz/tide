---
phase: 38-small-independents-pricing-accuracy-promptfile-telemetry-nud
plan: 03
subsystem: cli
tags: [cli, apply, prompt-file, cobra, unstructured]
requires: []
provides:
  - "tide apply --prompt-file <path> inlines a file into spec.outcomePrompt (PROMPT-01)"
  - "loadPromptFile D-11 validation helper (256 KiB cap, newline trim, empty rejection)"
  - "prepareApplyObject cluster-free decode/inject seam"
affects: [cmd/tide]
tech-stack:
  added: []
  patterns:
    - "cluster-free validation seam called before K8sClient() so CLI errors fire pre-apiserver"
key-files:
  created: []
  modified:
    - cmd/tide/apply.go
    - cmd/tide/cmd_test.go
decisions:
  - "D-11 cap set as maxPromptFileBytes = 256 KiB — spec.outcomePrompt has no CRD MaxLength, so the CLI cap is the only guard; keeps the Project object under etcd's ~1.5 MiB ceiling"
  - "Extra non-Project docs in a --prompt-file manifest are refused fail-loud (single-document requirement) rather than silently dropped — consistent with apply's single-object SSA semantics"
  - "CRLF handled: TrimSuffix \\n then TrimSuffix \\r, so exactly one trailing newline of either flavor is trimmed"
metrics:
  duration: "7 minutes"
  completed: "2026-07-11"
  tasks: 2
  files: 2
status: complete
---

# Phase 38 Plan 03: --prompt-file for tide apply Summary

`tide apply --prompt-file` inlines a validated local file verbatim into the single Project document's spec.outcomePrompt — CLI-side only, no CRD change, every failure mode errors loudly before any apiserver contact.

## Tasks Completed

| Task | Name | Commits | Files |
|------|------|---------|-------|
| 1 | loadPromptFile — content validation helper (D-11) | d6195e9 (RED), 40a8d14 (GREEN) | cmd/tide/apply.go, cmd/tide/cmd_test.go |
| 2 | --prompt-file flag, Project targeting, conflict check, injection (D-09/D-10) | 9ba0030 (RED), 5ef205a (GREEN) | cmd/tide/apply.go, cmd/tide/cmd_test.go |

## What Was Built

- **`maxPromptFileBytes = 256 * 1024`** — D-11 cap with etcd-headroom rationale comment; enforced in `loadPromptFile` before any cluster call.
- **`loadPromptFile(path)`** — os.ReadFile → size cap → exactly one trailing newline trimmed (LF or CRLF) → empty/whitespace-only rejection. Bytes otherwise verbatim; no templating or interpolation (mitigates T-38-08).
- **`prepareApplyObject(raw, path, promptFile)`** — the cluster-free seam `runApply` calls before constructing the Kubernetes client:
  - `promptFile == ""`: first-document decode path moved unchanged — behavior byte-identical to before the flag existed (locked by `TestApplyWithoutPromptFileKeepsFirstDocBehavior`).
  - `promptFile != ""`: fail-fast on content errors, decode all docs (skipping bare `---` separators), require exactly one Project (D-10, error names the count), refuse extra non-Project docs fail-loud, refuse an existing non-empty `spec.outcomePrompt` (D-09), inject via `unstructured.SetNestedField`.
- **`--prompt-file` StringVar on `tide apply`** — threaded through the extended `runApply(ctx, file, promptFile, out)` testable seam.
- **Tests** — `TestLoadPromptFile*` (4 functions, 9 cases) and `TestApplyPromptFile*` + `TestApplyWithoutPromptFileKeepsFirstDocBehavior` (6 functions, 10 cases), all table-driven plain-testing per cmd_test.go conventions; cluster-free except one root.SetArgs CLI-level case.

## Verification Observed

- `go test ./cmd/tide/` — full package `ok` (all pre-existing TestApply*/verb tests still pass)
- `go build ./cmd/tide/` — clean
- `bin/golangci-lint run ./cmd/tide/...` — `0 issues` (v2.11.4; two lll offenses found and wrapped before final commit)
- `grep -c 'maxPromptFileBytes = 256 \* 1024' cmd/tide/apply.go` == 1
- `grep -c 'K8sClient()' cmd/tide/apply.go` == 1, at line 178, after `prepareApplyObject` call at line 173 (validation precedes apiserver contact)
- `git diff --name-only` across all four commits touches only `cmd/tide/apply.go` and `cmd/tide/cmd_test.go` — no CRD/API type change
- `--prompt-file` flag registration asserted by `TestApplyPromptFileFlagRegistered`

## TDD Gate Compliance

RED → GREEN per task: d6195e9 (test) → 40a8d14 (feat) for Task 1; 9ba0030 (test) → 5ef205a (feat) for Task 2. Both RED commits verified failing (undefined-symbol build failures) before implementation. No refactor commits needed.

## Deviations from Plan

None - plan executed exactly as written. (Two in-task lint wraps and one doc-comment rewording to keep the `K8sClient()` grep acceptance at exactly one occurrence — cosmetic, within task scope.)

## Threat Mitigations Applied

- T-38-07 (DoS): 256 KiB cap enforced in `loadPromptFile` before any apiserver call.
- T-38-08 (Tampering): content inlined verbatim via `SetNestedField`; no interpolation.
- T-38-09 (Spoofing): exactly-one-Project + single-document targeting, fail-loud errors naming counts.

## Known Stubs

None.

## Self-Check: PASSED
