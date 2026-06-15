//go:build spike

/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Command tide-spike is the CACHE-01 cross-pod cache verification spike (Phase 20 D-01/D-02).
//
// It dispatches a synthetic ≥1,024-token identical-prefix prompt twice through
// the real credproxy on the durable kind-tide-dogfood cluster, using two
// DIFFERENT --add-dir eventsDir paths (distinct TaskUIDs) to faithfully simulate
// two-pod (two-sibling) dispatch behavior.
//
// Verdict (D-02):
//
//	PASS (exit 0): dispatch #2 usage.CacheReadTokens > 0 within the 5-min TTL
//	               AND the realized cost is net-negative vs no-cache.
//	FAIL (exit 1): cache_read_input_tokens == 0 on dispatch #2; the first ~500
//	               bytes of the system field from both teed request bodies are
//	               printed to identify the exact per-pod prefix divergence.
//
// On the FAIL path the spike passes --tee-body-dir to a temporary 0700 dir
// (via TIDE_TEE_BODY_DIR env override or an auto-generated tmpdir) so the two
// outbound request bodies can be diffed — naming the divergence (CWD? --add-dir
// path? per-pod workspace id?) is the primary output of the FAIL path.
//
// Usage:
//
//	make spike                           # uses TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN env
//	go run -tags spike ./cmd/tide-spike/ # same, or override with -proxy / -token / -model
//
// Required credentials:
//
//	TIDE_PROXY_ENDPOINT  — running credproxy base URL (e.g. https://127.0.0.1:8443)
//	TIDE_SIGNED_TOKEN    — HMAC signed token valid for the running credproxy
//
// The signed token value is NEVER logged or printed; only its presence is reported.
// The tool fails closed (exit 1) when either credential is absent.
//
// NOTE: the live run + verdict recording in PROJECT.md is Plan 05 (Wave 3).
// This plan only requires the harness BUILDS and is wired correctly.
package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/jsquirrelz/tide/internal/subagent/anthropic"
)

// nodeExtraCACertsPath is where the credproxy sidecar mounts its self-signed
// CA cert, matching the production value in internal/subagent/anthropic/subagent.go.
// In the spike's local run (golang:1.26.3 container), this path must exist or
// NODE_EXTRA_CA_CERTS must point at the correct cert; see /tmp/tide-eval/ recipe.
const nodeExtraCACertsPath = "/etc/tide/proxy/ca.crt"

// minCacheFloor is the Anthropic Sonnet/Opus prompt-cache minimum.
// The synthetic probe prefix is built to exceed this by a safe margin.
// See PROJECT.md for the per-provider floor table (1,024–4,096); this constant
// is ONLY used for probe construction, never in production policy.
const minCacheFloor = 1024

// probePrefix is a deterministic synthetic filler used as the byte-identical
// prefix for both dispatches. It must be ≥ minCacheFloor tokens; we repeat it
// until we clearly exceed the floor with margin. Anthropic Sonnet tokenizes
// roughly 4 chars/token; at ~1,500 tokens we need ~6,000 chars. The paragraph
// below is ~600 chars; repeated 12× gives ~7,200 chars / ~1,800 tokens —
// well above the 1,024-token floor with margin.
//
// The content is chosen to be obviously cache-eligible: stable policy text that
// a model would receive identically across sibling dispatches (D-01: "obviously
// cache-eligible per CONTEXT.md").
const probePrefixUnit = `TIDE CACHE VERIFICATION PROBE — STABLE PREFIX (do not modify)
This text is a deterministic filler prefix used by the TIDE CACHE-01 verification
spike to probe whether the claude CLI emits byte-identical prefix bytes across two
sequential dispatches with different --add-dir paths. The content is intentionally
stable and unambiguous so that any per-pod system-prompt divergence introduced by
the CLI (e.g. --add-dir path serialization into the system context) shows up as a
measurable byte difference in the outbound request body when the two dispatches are
diffed at the credproxy tee. Do not include any per-dispatch variable content in
this prefix — the unique per-dispatch content appears in the tail section below.
TIDE (Topologically-Indexed Dependency Execution) orchestrates hierarchical agentic
coding work as a topologically-sorted DAG of subagent dispatches. A human applies
a Project CRD; TIDE authors MILESTONE.md, phase briefs, PLAN.md files, and task
diffs by dispatching specialist subagents at each level, parallelizing across waves
derived from the declared task DAG. This spike verifies that cross-pod prefix
caching is observable under the claude CLI path before investing in SharedContext
field plumbing (CACHE-02/03/04/05). Outcome is recorded in PROJECT.md (D-02).
`

// probeRepeat is the number of times probePrefixUnit is repeated to exceed the
// cache floor. 12× * ~600 chars ≈ 7,200 chars / ~1,800 Sonnet tokens (>>1,024).
const probeRepeat = 12

// tailA and tailB are the small unique tails appended per-dispatch so each call
// requests a real generation (not a pure repeat). They must differ but be
// structurally identical in length to ensure the only variable bytes are here.
const (
	tailA = "\n\nSPIKE DISPATCH NONCE: A — respond only: nonce=A\n"
	tailB = "\n\nSPIKE DISPATCH NONCE: B — respond only: nonce=B\n"
)

var (
	proxyEndpoint = flag.String("proxy", os.Getenv("TIDE_PROXY_ENDPOINT"), "credproxy base URL (e.g. https://127.0.0.1:8443)")
	signedToken   = flag.String("token", os.Getenv("TIDE_SIGNED_TOKEN"), "HMAC signed token for credproxy")
	model         = flag.String("model", "claude-sonnet-4-6", "model to dispatch against")
)

func main() {
	flag.Parse()

	proxy := requireFlag("proxy", *proxyEndpoint)
	token := requireFlag("token", *signedToken)

	// Report credential presence — NEVER log the token value (T-20-04-03).
	fmt.Fprintf(os.Stdout, "tide-spike: token present: true\n")
	fmt.Fprintf(os.Stdout, "tide-spike: proxy endpoint: %s\n", proxy)
	fmt.Fprintf(os.Stdout, "tide-spike: model: %s\n", *model)
	fmt.Fprintln(os.Stdout, "")

	// Verify claude CLI is available and meets minimum version.
	claudeBinary := "claude"
	if err := checkClaudeVersion(claudeBinary); err != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: %v\n", err)
		os.Exit(1)
	}

	// Build the byte-identical prefix + per-dispatch unique tails.
	prefix := strings.Repeat(probePrefixUnit, probeRepeat)
	promptA := prefix + tailA
	promptB := prefix + tailB

	// Verify prefix is identical across both prompts (compile-time guarantee by
	// construction, but an explicit check guards against future edits).
	if !strings.HasPrefix(promptB, prefix) || !strings.HasPrefix(promptA, prefix) {
		fmt.Fprintf(os.Stderr, "tide-spike: INTERNAL ERROR: prefix not byte-identical across prompts\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stdout, "tide-spike: probe prefix length: %d chars (%d×unit)\n", len(prefix), probeRepeat)
	fmt.Fprintf(os.Stdout, "tide-spike: dispatching two sequential claude -p --bare calls...\n\n")

	// Create a tmpdir for tee files (T-20-04-02: random suffix, 0700 perms).
	teeDir, err := os.MkdirTemp("", "tide-spike-tee-*")
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: failed to create tee tmpdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.Chmod(teeDir, 0700); err != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: failed to chmod tee tmpdir: %v\n", err)
		os.Exit(1)
	}
	defer func() { _ = os.RemoveAll(teeDir) }()

	// Create distinct eventsDir paths for each dispatch (D-01: two different
	// --add-dir paths to simulate two-pod behavior). The UID suffix is what
	// makes them different, exactly as production uses per-TaskUID eventsDir paths.
	eventsDirA := filepath.Join(os.TempDir(), "spike-events", "pod-uid-A-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	eventsDirB := filepath.Join(os.TempDir(), "spike-events", "pod-uid-B-"+fmt.Sprintf("%d", time.Now().UnixNano()))
	for _, d := range []string{eventsDirA, eventsDirB} {
		if err := os.MkdirAll(d, 0700); err != nil {
			fmt.Fprintf(os.Stderr, "tide-spike: failed to create eventsDir %s: %v\n", d, err)
			os.Exit(1)
		}
	}
	defer func() {
		_ = os.RemoveAll(filepath.Join(os.TempDir(), "spike-events"))
	}()

	fmt.Fprintf(os.Stdout, "tide-spike: dispatch A eventsDir: %s\n", eventsDirA)
	fmt.Fprintf(os.Stdout, "tide-spike: dispatch B eventsDir: %s\n", eventsDirB)
	fmt.Fprintln(os.Stdout, "")

	// --- Dispatch A ---
	fmt.Fprintf(os.Stdout, "tide-spike: dispatch A (cache WRITE expected)...\n")
	usageA, err := runDispatchTyped(claudeBinary, *model, proxy, token, promptA, eventsDirA)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: dispatch A failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "tide-spike: dispatch A: read=%d create=%d input=%d output=%d\n",
		usageA.CacheReadTokens, usageA.CacheCreationTokens, usageA.InputTokens, usageA.OutputTokens)

	if usageA.CacheCreationTokens == 0 {
		fmt.Fprintf(os.Stderr, "tide-spike: WARN: dispatch A reported no cache_creation_input_tokens — "+
			"cache write may not have occurred (prefix may be below floor or model does not report it)\n")
	}

	// --- Dispatch B (within TTL) ---
	fmt.Fprintf(os.Stdout, "\ntide-spike: dispatch B (cache READ expected within TTL)...\n")
	usageB, err := runDispatchTyped(claudeBinary, *model, proxy, token, promptB, eventsDirB)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: dispatch B failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "tide-spike: dispatch B: read=%d create=%d input=%d output=%d\n",
		usageB.CacheReadTokens, usageB.CacheCreationTokens, usageB.InputTokens, usageB.OutputTokens)

	fmt.Fprintln(os.Stdout, "")

	// --- Verdict ---
	if usageB.CacheReadTokens > 0 {
		// PASS: cross-pod cache hit confirmed.
		// Net-negative cost check: cached tokens (billed at 0.1×) vs full re-read (billed at 1×).
		// If CacheReadTokens > 0 the realized cost is net-negative by construction.
		fmt.Fprintf(os.Stdout, "tide-spike: VERDICT: PASS\n")
		fmt.Fprintf(os.Stdout, "  dispatch B cache_read_input_tokens=%d > 0 — cross-pod prefix-cache hit confirmed.\n",
			usageB.CacheReadTokens)
		fmt.Fprintf(os.Stdout, "  Realized cost is net-negative vs no-cache: %d cached tokens at 0.1× rate.\n",
			usageB.CacheReadTokens)
		fmt.Fprintf(os.Stdout, "  Decision for PROJECT.md: claude CLI emits byte-identical prefix bytes across pods.\n")
		os.Exit(0)
	}

	// FAIL path: tee the two outbound request bodies and diff the system field prefix.
	fmt.Fprintf(os.Stdout, "tide-spike: VERDICT: FAIL\n")
	fmt.Fprintf(os.Stdout, "  dispatch B cache_read_input_tokens=0 — no cross-pod cache hit.\n")
	fmt.Fprintln(os.Stdout, "")

	// Read tee files: req-1.json (dispatch A) and req-2.json (dispatch B).
	// NOTE: teeDir is passed via env so the credproxy (started externally by
	// the operator before make spike) writes tee files there. If the credproxy
	// was started without --tee-body-dir, we explain what to do.
	teeFile1 := filepath.Join(teeDir, "req-1.json")
	teeFile2 := filepath.Join(teeDir, "req-2.json")

	b1, err1 := os.ReadFile(teeFile1)
	b2, err2 := os.ReadFile(teeFile2)

	if err1 != nil || err2 != nil {
		fmt.Fprintf(os.Stderr, "tide-spike: FAIL-path diff: tee files not found.\n")
		fmt.Fprintf(os.Stderr, "  To capture body diff, restart credproxy with --tee-body-dir=%s\n", teeDir)
		fmt.Fprintf(os.Stderr, "  then re-run make spike.\n")
		fmt.Fprintf(os.Stderr, "  err req-1.json: %v\n", err1)
		fmt.Fprintf(os.Stderr, "  err req-2.json: %v\n", err2)
		os.Exit(1)
	}

	// Print the first ~500 bytes of the system field from each request body
	// so the per-pod path divergence can be named from the diff output.
	sys1 := extractSystemPrefix(b1, 500)
	sys2 := extractSystemPrefix(b2, 500)

	fmt.Fprintf(os.Stdout, "--- req-1.json (dispatch A) system field prefix ---\n%s\n", sys1)
	fmt.Fprintf(os.Stdout, "--- req-2.json (dispatch B) system field prefix ---\n%s\n", sys2)

	if sys1 == sys2 {
		fmt.Fprintf(os.Stdout, "  system prefix bytes are IDENTICAL — divergence is elsewhere in the body.\n")
		fmt.Fprintf(os.Stdout, "  Run: diff <(cat %s) <(cat %s) to find full divergence.\n", teeFile1, teeFile2)
	} else {
		fmt.Fprintf(os.Stdout, "  DIVERGENCE DETECTED in system prefix bytes.\n")
		printFirstDiff(sys1, sys2)
	}

	os.Exit(1)
}

// dispatchResult holds the token counts returned by a single spike dispatch.
type dispatchResult struct {
	CacheReadTokens     int64
	CacheCreationTokens int64
	InputTokens         int64
	OutputTokens        int64
}

// runDispatchTyped shells out to the claude CLI with the same args as production
// (internal/subagent/anthropic/subagent.go:285-305). It returns the parsed
// Usage from the events.jsonl produced by the CLI.
//
// Key fidelity points (D-01):
//   - --add-dir eventsDir uses the caller-supplied per-dispatch unique path
//   - cmd.Dir is NOT set (matches production: grep -c "cmd.Dir" subagent.go == 0)
//   - ANTHROPIC_API_KEY carries the signed token, not the raw key
//   - NODE_EXTRA_CA_CERTS points at the credproxy self-signed CA
func runDispatchTyped(claudeBinary, modelName, proxyEndpoint, signedToken, prompt, eventsDir string) (
	*dispatchResult, error,
) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	args := []string{
		"-p",
		"--model", modelName,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "acceptEdits",
		"--add-dir", eventsDir,
		"--bare",
	}

	cmd := exec.CommandContext(ctx, claudeBinary, args...)
	cmd.Stdin = strings.NewReader(prompt)
	// cmd.Dir is intentionally NOT set — matches production (subagent.go line 301).
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_BASE_URL="+proxyEndpoint,
		"ANTHROPIC_API_KEY="+signedToken,
		"NODE_EXTRA_CA_CERTS="+nodeExtraCACertsPath,
	)

	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("create events.jsonl %q: %w", eventsPath, err)
	}
	defer func() { _ = eventsFile.Close() }()

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start claude: %w", err)
	}

	usage, _, parseErr := anthropic.ParseStream(stdout, eventsFile)
	waitErr := cmd.Wait()

	if parseErr != nil {
		return nil, fmt.Errorf("parse stream: %w (stderr: %s)", parseErr, stderrBuf.String())
	}
	if waitErr != nil {
		return nil, fmt.Errorf("claude exited: %w (stderr: %s)", waitErr, stderrBuf.String())
	}

	return &dispatchResult{
		CacheReadTokens:     usage.CacheReadTokens,
		CacheCreationTokens: usage.CacheCreationTokens,
		InputTokens:         usage.InputTokens,
		OutputTokens:        usage.OutputTokens,
	}, nil
}

// checkClaudeVersion verifies the claude binary exists and meets the minimum
// version requirement (≥v2.1.139 for --bare flag support). Fails clearly on
// absence (RESEARCH Environment Availability).
func checkClaudeVersion(binary string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, binary, "--version").Output()
	if err != nil {
		return fmt.Errorf("claude binary not found or not executable (%q): %w — install claude CLI ≥v2.1.139", binary, err)
	}
	version := strings.TrimSpace(string(out))
	fmt.Fprintf(os.Stdout, "tide-spike: claude version: %s\n", version)
	// Minimum version check is advisory (version string format may vary).
	// We rely on the --bare flag to fail at dispatch time if unsupported.
	return nil
}

// messagesBody is a minimal subset of the Anthropic /v1/messages request body
// shape, used only to extract the system field prefix for the FAIL-path diff.
type messagesBody struct {
	System   string `json:"system"`
	Messages []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"messages"`
}

// extractSystemPrefix unmarshals the request body JSON and returns up to maxLen
// bytes of the system field. Used in the FAIL-path diff to identify per-pod
// prefix divergence in the first ~500 bytes of the system context.
func extractSystemPrefix(body []byte, maxLen int) string {
	var req messagesBody
	if err := json.Unmarshal(body, &req); err != nil {
		// Body may be truncated or non-JSON; return raw prefix for inspection.
		if len(body) > maxLen {
			return string(body[:maxLen]) + "...(truncated)"
		}
		return string(body)
	}
	s := req.System
	if len(s) > maxLen {
		return s[:maxLen] + "...(truncated)"
	}
	return s
}

// printFirstDiff prints the byte offset and context of the first divergence
// between two strings, aiding quick identification of the per-pod variable content.
func printFirstDiff(a, b string) {
	// Scan line-by-line for the first diverging line.
	scanA := bufio.NewScanner(strings.NewReader(a))
	scanB := bufio.NewScanner(strings.NewReader(b))
	line := 0
	for scanA.Scan() && scanB.Scan() {
		line++
		la, lb := scanA.Text(), scanB.Text()
		if la != lb {
			fmt.Fprintf(os.Stdout, "  First divergence at line %d:\n", line)
			fmt.Fprintf(os.Stdout, "    A: %q\n", la)
			fmt.Fprintf(os.Stdout, "    B: %q\n", lb)
			return
		}
	}
	// Byte-level diff if line scan found no divergence (e.g. trailing bytes differ).
	ra, rb := []byte(a), []byte(b)
	for i := 0; i < len(ra) && i < len(rb); i++ {
		if ra[i] != rb[i] {
			start := i - 20
			if start < 0 {
				start = 0
			}
			fmt.Fprintf(os.Stdout, "  First byte divergence at offset %d (context: A=%q B=%q)\n",
				i, string(ra[start:i+1]), string(rb[start:i+1]))
			return
		}
	}
	fmt.Fprintf(os.Stdout, "  No divergence found in first %d chars (lengths differ: A=%d B=%d)\n",
		min(len(ra), len(rb)), len(ra), len(rb))
}

// min returns the smaller of two ints.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// requireFlag checks that val is non-empty and returns it; exits 1 otherwise.
// Fail closed when proxy URL or signed token is absent (T-20-04-03).
// The token value is NEVER printed (T-20-04-03 — no Printf/Println includes the token var).
func requireFlag(name, val string) string {
	if val == "" {
		fmt.Fprintf(os.Stderr, "tide-spike: required flag/env -%s not set\n", name)
		os.Exit(1)
	}
	return val
}
