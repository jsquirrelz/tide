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

// Package anthropic implements pkg/dispatch.Subagent against Anthropic's
// Claude Code CLI. Per D-C1 (the provider firewall) all Anthropic-specific
// code lives here, NOT in pkg/dispatch, pkg/controller, or pkg/dag â the
// firewall is enforced at build time by tools/analyzers/providerfirewall.
//
// HARN-06 decision (03-RESEARCH Â§"Alternatives Considered"): we shell out to
// the `claude` CLI rather than embedding the Anthropic Go SDK. The CLI bundles
// the agent loop, hooks, MCP, skills, and bash/file tools that would otherwise
// have to be re-implemented in Go. Phase 3 does not depend on the Anthropic
// Go SDK module.
//
// CLAUDE.md anti-pattern guardrails baked in:
//   - NEVER mount the host claude config dir â the --bare flag (RESEARCH
//     Â§"Critical new finding") skips auto-discovery of hooks/skills/plugins/MCP
//     and any CLAUDE-doc auto-memory, so the per-Pod runtime is hermetic.
//   - NEVER use OAuth headless â claude-code#29983, #7100 break it. We pin
//     ANTHROPIC_API_KEY to the signed token minted by the controller and
//     validated by the credproxy sidecar (Phase 2 D-C1..C4).
//   - NEVER embed an LLM API key directly â the API key lives only in the
//     credproxy sidecar; this binary sees a short-lived HMAC token.
package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// vendorSentinel is the Provider.Vendor value this binary accepts. The
// envelope.Provider.Vendor field is the compile-time agreement between the
// orchestrator-resolved provider triple and the running subagent image â if
// they disagree, we refuse to dispatch (Pitfall 14 mitigation).
const vendorSentinel = "anthropic"

// paramsAllowList enforces Q3 RESOLVED (03-RESEARCH Â§"Open Questions" line
// 933): unknown Provider.Params keys are rejected at startup. The fail-fast
// posture catches typos at apply time rather than letting them silently
// disappear into a passthrough. Add new keys here only after wiring the
// provider-side mapping that actually consumes them.
var paramsAllowList = map[string]bool{
	"temperature":     true,
	"thinking_budget": true,
	"top_p":           true,
	"top_k":           true,
}

// nodeExtraCACertsPath is where the credproxy sidecar mounts its self-signed
// CA. NODE_EXTRA_CA_CERTS makes the claude CLI (a Node binary) trust the
// 127.0.0.1:8443 reverse proxy without disabling TLS verification globally
// (Phase 2 D-C2).
const nodeExtraCACertsPath = "/etc/tide/proxy/ca.crt"

// Options configures the Anthropic subagent. Zero values pick sensible
// defaults for production use; tests override individual fields.
type Options struct {
	// ClaudeBinary is the path to the claude CLI. Defaults to "claude" so
	// the image's PATH resolves it.
	ClaudeBinary string

	// WorkspaceRoot is the per-Project PVC mount point. The per-Task
	// events.jsonl audit log lives at
	// <WorkspaceRoot>/envelopes/<TaskUID>/events.jsonl per Phase 3 D-B4.
	// Defaults to "/workspace".
	WorkspaceRoot string
}

// Anthropic implements pkg/dispatch.Subagent against the Claude Code CLI.
// Construct via [New]; instances are safe for concurrent calls because
// Run() makes no mutable struct state changes.
type Anthropic struct {
	opts Options

	// execFunc is the indirection seam that lets tests replace
	// exec.CommandContext with a fixture-serving fake. Production calls
	// New() which defaults this to exec.CommandContext; tests set it
	// directly on the returned *Anthropic.
	execFunc func(ctx context.Context, name string, args ...string) *exec.Cmd
}

// New returns an *Anthropic configured with opts. Defaults: ClaudeBinary
// resolves "claude" via PATH; WorkspaceRoot is "/workspace".
func New(opts Options) *Anthropic {
	if opts.ClaudeBinary == "" {
		opts.ClaudeBinary = "claude"
	}
	if opts.WorkspaceRoot == "" {
		opts.WorkspaceRoot = "/workspace"
	}
	return &Anthropic{
		opts:     opts,
		execFunc: exec.CommandContext,
	}
}

// NewWithExec is the exported test seam: same defaults as [New] but takes an
// execFunc override so external packages (e.g. cmd/claude-subagent_test) can
// inject a fake exec without touching the unexported execFunc field. If
// execFunc is nil, defaults to exec.CommandContext (equivalent to [New]).
//
// Production code MUST use [New]. NewWithExec exists solely so the
// cmd/claude-subagent shim's tests can replicate the fake-`bash -c cat`
// fixture pattern from internal/subagent/anthropic/subagent_test.go without
// the shim having to live inside the anthropic package itself.
func NewWithExec(opts Options, execFunc func(ctx context.Context, name string, args ...string) *exec.Cmd) *Anthropic {
	a := New(opts)
	if execFunc != nil {
		a.execFunc = execFunc
	}
	return a
}

// Run satisfies pkg/dispatch.Subagent.Run. The dispatch flow:
//
//  1. Fail-fast vendor check: refuse if in.Provider.Vendor != "anthropic".
//  2. Fail-fast params allow-list (Q3): refuse unknown Provider.Params keys.
//  3. Load + render the compiled-in prompt template via
//     common.LoadPromptTemplate(in.Role, in.Level) â never read the host
//     filesystem for prompt content (CLAUDE.md anti-pattern).
//  4. Build `claude -p <rendered> --model <Provider.Model> --output-format
//     stream-json --verbose --include-partial-messages --bare` (the --bare
//     flag is REQUIRED per RESEARCH Â§"Critical new finding").
//  5. Wire credproxy env: ANTHROPIC_BASE_URL = in.ProxyEndpoint;
//     ANTHROPIC_API_KEY = in.SignedToken (never the raw key);
//     NODE_EXTRA_CA_CERTS = /etc/tide/proxy/ca.crt.
//  6. Tee stdout through ParseStream into events.jsonl at
//     <WorkspaceRoot>/envelopes/<TaskUID>/events.jsonl.
//  7. Assemble EnvelopeOut with parsed Usage + Result.
//
// Dispatch-level errors (I/O setting up events.jsonl, exec.Cmd start
// failure) return non-nil error. Task-level errors (claude exits non-zero)
// return (EnvelopeOut{ExitCode: N, Reason: ...}, nil) per pkg/dispatch
// godoc: "task-level failures (subagent exited non-zero) are expressed via
// EnvelopeOut.ExitCode and EnvelopeOut.Reason."
func (a *Anthropic) Run(ctx context.Context, in pkgdispatch.EnvelopeIn) (pkgdispatch.EnvelopeOut, error) {
	// 1. Vendor fail-fast (CLAUDE.md anti-pattern: provider firewall).
	if in.Provider.Vendor != vendorSentinel {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: refusing vendor=%q (expected %q)", in.Provider.Vendor, vendorSentinel)
	}

	// 2. Params allow-list fail-fast (Q3 RESOLVED â 03-RESEARCH line 933).
	for key := range in.Provider.Params {
		if !paramsAllowList[key] {
			return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: unknown param %q (allowed: temperature, thinking_budget, top_p, top_k)", key)
		}
	}

	// 2.5. In-pod prompt read (defect #10b fix). If PromptPath is set, read the
	// executor prompt artifact from the local namespace PVC and populate in.Prompt
	// so step 3's template render resolves {{.Prompt}} correctly. This is the
	// same-namespace read: the executor pod mounts the project PVC at WorkspaceRoot
	// via subPath {project.UID}/workspace, so PromptPath is workspace-relative
	// directly under WorkspaceRoot (NOT <root>/<uid>/workspace — the subPath
	// already places the pod inside the project subtree). T-09-05 traversal defense:
	// absolute paths and "../" escapes are rejected before any OS read.
	if in.PromptPath != "" {
		prompt, err := readPromptArtifact(a.opts.WorkspaceRoot, in.PromptPath)
		if err != nil {
			return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: read prompt artifact: %w", err)
		}
		in.Prompt = prompt
	}

	// 3. Render the prompt template. Template content uses the EnvelopeIn
	// shape as its execution context, so {{.Level}}, {{.TaskUID}},
	// {{.Provider.Model}}, etc. are addressable.
	tmpl, err := common.LoadPromptTemplate(in.Role, in.Level)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: load prompt template (role=%q, level=%q): %w", in.Role, in.Level, err)
	}
	var promptBuf bytes.Buffer
	if err := tmpl.Execute(&promptBuf, in); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: render prompt template: %w", err)
	}
	renderedPrompt := promptBuf.String()

	// 4a. Compute + create the per-Task events dir BEFORE the args build: the
	// claude invocation scopes its write capability to this dir via --add-dir
	// (defect #7), and the planner child-CRD handoff (defect #5) writes to
	// <eventsDir>/children/ -- the same dir readChildCRDs scans below.
	eventsDir := filepath.Join(a.opts.WorkspaceRoot, "envelopes", in.TaskUID)
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: mkdir events dir %q: %w", eventsDir, err)
	}

	// 4b. Build the claude CLI invocation.
	//   - --bare is REQUIRED per RESEARCH "Critical new finding": it skips
	//     auto-discovery of the host claude config dir, .mcp.json, hooks,
	//     skills, plugins, and any project CLAUDE-doc auto-memory. Hermetic by
	//     construction.
	//   - --permission-mode acceptEdits (defect #7) is the only headless mode
	//     that auto-approves the Write/Edit tools; without it the planner
	//     child-CRD Write is denied ("sandbox restrictions") and no children/
	//     dir is ever created. bypassPermissions is sandbox-only; text-scraping
	//     of Result was rejected for #5.
	//   - --add-dir eventsDir (defect #7) scopes the granted write capability to
	//     the per-Task dir only (minimal privilege -- NOT the whole workspace);
	//     readChildCRDs traversal/Kind/empty-name guards remain the second line
	//     of defense.
	//   - The rendered prompt is delivered via STDIN, not as the -p positional
	//     arg (defect #8): a large planner prompt as one argv entry risks Linux
	//     MAX_ARG_STRLEN (128 KiB). claude -p reads the prompt from stdin when
	//     the positional is omitted (stdin cap is 10 MB). There is no
	//     --prompt-file flag in the pinned CLI.
	args := []string{
		"-p",
		"--model", in.Provider.Model,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--permission-mode", "acceptEdits",
		"--add-dir", eventsDir,
		"--bare",
	}
	cmd := a.execFunc(ctx, a.opts.ClaudeBinary, args...)
	cmd.Stdin = strings.NewReader(renderedPrompt)

	// 5. Credproxy env wiring (Phase 2 D-C1..C4). ANTHROPIC_API_KEY carries
	// the HMAC-signed token, NEVER the raw API key -- the real key lives
	// only in the credproxy sidecar Secret mount.
	cmd.Env = append(cmd.Environ(),
		"ANTHROPIC_BASE_URL="+in.ProxyEndpoint,
		"ANTHROPIC_API_KEY="+in.SignedToken,
		"NODE_EXTRA_CA_CERTS="+nodeExtraCACertsPath,
	)

	// 6. Create the events.jsonl audit log. Phase 4 OpenInference parsing
	// reads this file untouched; we tee every line through ParseStream as
	// it arrives.
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: create events.jsonl %q: %w", eventsPath, err)
	}
	defer func() { _ = eventsFile.Close() }() // best-effort event sink; close error is non-actionable cleanup

	// 7. Pipe stdout â ParseStream(stdout, events.jsonl). Stderr surfaces
	// as task-level Reason on non-zero exit.
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: stdout pipe: %w", err)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: start %s: %w", a.opts.ClaudeBinary, err)
	}

	usage, resultText, parseErr := ParseStream(stdout, eventsFile)
	waitErr := cmd.Wait()

	// Decide exit code + reason. Order:
	//   - parse error on the stream â dispatch-level (return err).
	//   - wait error: task-level (ExitCode from exec.ExitError, Reason from stderr).
	if parseErr != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: parse stream: %w", parseErr)
	}

	// Compute cost from the per-model price table before assembling EnvelopeOut.
	// EstimatedCostCents flows into budget.RollUpUsage → Project.Status.budget.
	// This call stays in internal/subagent/anthropic/ (provider firewall, D-C1).
	usage.EstimatedCostCents = estimatedCostCents(in.Provider.Model, usage)

	out := pkgdispatch.EnvelopeOut{
		APIVersion:  pkgdispatch.APIVersionV1Alpha1,
		Kind:        pkgdispatch.KindTaskEnvelopeOut,
		TaskUID:     in.TaskUID,
		Result:      resultText,
		Usage:       usage,
		CompletedAt: time.Now().UTC(),
	}

	if waitErr != nil {
		// Surface task-level failure via ExitCode + Reason (pkg/dispatch
		// godoc contract). Do NOT return as a dispatch-level error.
		out.ExitCode = 1
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			out.ExitCode = exitErr.ExitCode()
		}
		out.Reason = fmt.Sprintf("claude exit %d: %s", out.ExitCode, truncate(stderrBuf.String(), 256))
		return out, nil
	}

	// Planner file-handoff (defect #5): a planner dispatch (Role=="planner")
	// authors child-CRD JSON files into <eventsDir>/children/*.json via the
	// claude CLI's Write tool (the template instructs the exact path). The
	// subagent pod has zero K8s verbs (Phase 2 D-A4) — this file handoff is the
	// only channel from the model's output into EnvelopeOut.ChildCRDs, which
	// the controller materializes server-side. No text-scraping of Result.
	// Executors (Role=="executor") emit no children; skip the read entirely.
	if in.Role == "planner" {
		// relPrefix is the workspace-relative children dir
		// ("envelopes/<TaskUID>/children") stamped onto each child's SourcePath so
		// the controller can set Task.Spec.PromptPath without knowing this pod's
		// absolute PVC mount (defect #10b — prompt-as-PVC-artifact).
		relPrefix := filepath.Join("envelopes", in.TaskUID, "children")
		children, readErr := readChildCRDs(filepath.Join(eventsDir, "children"), relPrefix)
		if readErr != nil {
			// A malformed/poisoned children dir is a task-level failure, not a
			// dispatch-level crash: the agent loop completed, but its structural
			// output is unusable. Surface via ExitCode+Reason so the controller
			// marks the parent Failed rather than retrying a clean dispatch.
			out.ExitCode = 1
			out.Reason = fmt.Sprintf("read child CRDs: %s", truncate(readErr.Error(), 256))
			return out, nil
		}
		out.ChildCRDs = children
	}

	return out, nil
}

// truncate clips s to at most n bytes for embedding into EnvelopeOut.Reason.
// Reason is human-readable; full stderr lives in the Pod's container log.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "...(truncated)"
}

// childKindAllowlist is the runner-side T-308 mitigation mirror of the
// controller's childKindAllowlist (internal/controller/dispatch_helpers.go).
// Only these Kinds may flow from a planner's child-handoff files into
// EnvelopeOut.ChildCRDs. A file declaring any other Kind poisons the whole
// batch — the controller enforces the same allowlist again at materialize
// time, but rejecting here gives a clearer task-level Reason and keeps a
// non-TIDE Kind from ever reaching the cluster's typed CRD graph.
var childKindAllowlist = map[string]bool{
	"Milestone": true,
	"Phase":     true,
	"Plan":      true,
	"Task":      true,
	"Wave":      true,
}

// readChildCRDs reads the planner's child-CRD handoff files from childrenDir
// (<workspaceRoot>/envelopes/<TaskUID>/children) and returns them as typed
// []ChildCRDSpec, in deterministic filename order.
//
// Contract (defect #5, file-handoff option a):
//   - Each *.json file in childrenDir is one ChildCRDSpec {kind, name, spec}.
//   - A missing childrenDir is NOT an error — it yields zero children (the
//     controller then surfaces the empty-output condition). This keeps an
//     executor-shaped or no-op planner run from failing on a clean exit.
//   - Path safety: only regular files whose resolved path stays within
//     childrenDir are read. Symlinks and any entry escaping childrenDir are
//     rejected (traversal defense) — the model's Write tool is constrained to
//     the per-task dir, but the runner does not trust that.
//   - Kind allowlist: every spec's Kind must be in childKindAllowlist.
//   - Name required: a child with an empty Name is rejected (the controller
//     uses it as metadata.name).
func readChildCRDs(childrenDir, relPrefix string) ([]pkgdispatch.ChildCRDSpec, error) {
	entries, err := os.ReadDir(childrenDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no handoff dir → zero children, not an error
		}
		return nil, fmt.Errorf("read children dir %q: %w", childrenDir, err)
	}

	// Canonical base for the traversal check. EvalSymlinks resolves the real
	// directory so a symlinked childrenDir is still anchored correctly.
	baseReal, err := filepath.EvalSymlinks(childrenDir)
	if err != nil {
		return nil, fmt.Errorf("resolve children dir %q: %w", childrenDir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if filepath.Ext(e.Name()) != ".json" {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names) // deterministic order for stable child materialization

	children := make([]pkgdispatch.ChildCRDSpec, 0, len(names))
	var parseErrs []error
	for _, name := range names {
		full := filepath.Join(childrenDir, name)

		// Traversal defense: reject any entry that is a symlink or whose
		// resolved path escapes baseReal. Lstat (not Stat) so we inspect the
		// link itself, not its target. These are security boundaries — hard-abort,
		// NOT per-file-skip (T-10-03-A mitigation).
		info, lerr := os.Lstat(full)
		if lerr != nil {
			return nil, fmt.Errorf("lstat child file %q: %w", name, lerr)
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("child file %q is a symlink (rejected: traversal defense)", name)
		}
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("child file %q is not a regular file (rejected)", name)
		}
		realPath, rerr := filepath.EvalSymlinks(full)
		if rerr != nil {
			return nil, fmt.Errorf("resolve child file %q: %w", name, rerr)
		}
		if realPath != baseReal && !strings.HasPrefix(realPath, baseReal+string(os.PathSeparator)) {
			return nil, fmt.Errorf("child file %q resolves outside children dir (rejected: traversal defense)", name)
		}

		data, rderr := os.ReadFile(full)
		if rderr != nil {
			return nil, fmt.Errorf("read child file %q: %w", name, rderr)
		}

		// Escape raw control characters that the model left unescaped inside
		// string literals (Phase 10 medium-DoD cascade: multi-line title/desc
		// values produced literal '\n'/'\t' bytes → "invalid character '\\n' in
		// string literal"). Control chars OUTSIDE strings are valid JSON
		// whitespace and are left untouched, so pretty-printed JSON still parses.
		data = sanitizeJSONStringControls(data)

		// Use json.Decoder instead of json.Unmarshal so trailing prose after the
		// closing } is ignored (Decoder stops at end of first JSON value). This
		// is the observed production failure class: model appended explanatory
		// text after }. json.Unmarshal would return "invalid character 'W'...".
		var spec pkgdispatch.ChildCRDSpec
		dec := json.NewDecoder(bytes.NewReader(data))
		if jerr := dec.Decode(&spec); jerr != nil {
			parseErrs = append(parseErrs, fmt.Errorf("parse child file %q: %w", name, jerr))
			continue
		}
		// Double-object detection (T-10-03-B mitigation): attempt to decode a
		// second value from the stream. If a second full JSON value parses
		// successfully, the file contains two concatenated objects — reject it so
		// the second ChildCRDSpec cannot be silently injected. Trailing prose
		// (e.g. "With these tasks we will...") is NOT valid JSON, so the second
		// Decode returns a syntax error and we ignore it — trailing prose is
		// the observed production failure class and must be tolerated.
		var extra interface{}
		if extraErr := dec.Decode(&extra); extraErr == nil {
			parseErrs = append(parseErrs, fmt.Errorf("child file %q contains extra content after JSON object", name))
			continue
		}
		// Kind/name validation: correctness checks, not security boundaries —
		// per-file-skip (append error + continue) so a bad kind/name in one file
		// does not lose its valid siblings.
		if !childKindAllowlist[spec.Kind] {
			parseErrs = append(parseErrs, fmt.Errorf("child file %q declares disallowed kind %q (allowed: Milestone, Phase, Plan, Task, Wave)", name, spec.Kind))
			continue
		}
		if spec.Name == "" {
			parseErrs = append(parseErrs, fmt.Errorf("child file %q has empty name", name))
			continue
		}
		// Stamp the workspace-relative origin path so the controller can wire
		// Task.Spec.PromptPath → this artifact (defect #10b). Not model-authored.
		spec.SourcePath = filepath.Join(relPrefix, name)
		children = append(children, spec)
	}
	if len(parseErrs) > 0 {
		// Return valid siblings alongside the joined parse errors. The caller
		// (Run) treats a non-nil readErr as a task-level failure — children that
		// parsed successfully are still surfaced so the error message can name
		// exactly which file(s) failed.
		return children, errors.Join(parseErrs...)
	}
	return children, nil
}

// sanitizeJSONStringControls escapes raw ASCII control characters (U+0000–
// U+001F) that appear INSIDE JSON string literals, leaving everything else —
// including control chars used as structural whitespace between tokens —
// byte-for-byte unchanged. LLM-authored child specs routinely contain literal
// newlines/tabs inside multi-line title/description values; the JSON spec
// requires those to be escaped, so a verbatim decode fails with "invalid
// character '\n' in string literal". This makes such files decodable without
// altering valid JSON (the escape of an already-valid document is a no-op).
//
// The scan tracks string-literal state with a backslash-escape toggle so a
// quote that closes a string is distinguished from an escaped quote inside one.
func sanitizeJSONStringControls(data []byte) []byte {
	// Fast path: a document with no control bytes at all (e.g. compact
	// single-line JSON) is already spec-valid — return it untouched.
	hasControl := false
	for _, b := range data {
		if b < 0x20 {
			hasControl = true
			break
		}
	}
	if !hasControl {
		return data
	}

	out := make([]byte, 0, len(data)+16)
	inString := false
	escaped := false
	for _, b := range data {
		if !inString {
			out = append(out, b)
			if b == '"' {
				inString = true
			}
			continue
		}
		// Inside a string literal.
		if escaped {
			out = append(out, b)
			escaped = false
			continue
		}
		switch {
		case b == '\\':
			out = append(out, b)
			escaped = true
		case b == '"':
			out = append(out, b)
			inString = false
		case b < 0x20:
			// Raw control char inside a string → escape it.
			switch b {
			case '\n':
				out = append(out, '\\', 'n')
			case '\r':
				out = append(out, '\\', 'r')
			case '\t':
				out = append(out, '\\', 't')
			case '\b':
				out = append(out, '\\', 'b')
			case '\f':
				out = append(out, '\\', 'f')
			default:
				const hex = "0123456789abcdef"
				out = append(out, '\\', 'u', '0', '0', hex[b>>4], hex[b&0xf])
			}
		default:
			out = append(out, b)
		}
	}
	return out
}

// promptArtifact is the minimal shape readPromptArtifact decodes from the
// children JSON file. It mirrors the ChildCRDSpec {kind,name,spec} envelope
// but only decodes spec.prompt so unrelated spec fields are ignored.
type promptArtifact struct {
	Spec struct {
		Prompt string `json:"prompt"`
	} `json:"spec"`
}

// readPromptArtifact reads the executor prompt from the workspace-relative
// promptPath under base (the in-pod WorkspaceRoot). It mirrors the traversal
// defense from FilesystemEnvelopeReader.ReadPrompt but uses base directly —
// the executor pod mounts the project PVC at WorkspaceRoot via subPath
// {project.UID}/workspace, so PromptPath is relative to WorkspaceRoot itself
// (NOT <WorkspaceRoot>/<uid>/workspace). T-09-05 mitigations:
//
//   - empty promptPath → error
//   - absolute promptPath → rejected
//   - filepath.Clean then ".." prefix check → traversal rejected
//   - resolved full path must stay under base (second-line defense)
//   - empty .spec.prompt → error (defect #4 class: no silent empty-prompt dispatch)
func readPromptArtifact(base, promptPath string) (string, error) {
	if promptPath == "" {
		return "", fmt.Errorf("empty promptPath")
	}
	if filepath.IsAbs(promptPath) {
		return "", fmt.Errorf("promptPath %q must be workspace-relative, not absolute", promptPath)
	}
	clean := filepath.Clean(promptPath)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("promptPath %q escapes the workspace (rejected: traversal defense)", promptPath)
	}
	full := filepath.Join(base, clean)
	// Second-line defense: resolved path must remain under base.
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("promptPath %q resolves outside the workspace (rejected)", promptPath)
	}
	data, err := os.ReadFile(full)
	if err != nil {
		return "", fmt.Errorf("read prompt artifact %q: %w", full, err)
	}
	var pa promptArtifact
	if err := json.Unmarshal(data, &pa); err != nil {
		return "", fmt.Errorf("parse prompt artifact %q: %w", full, err)
	}
	if pa.Spec.Prompt == "" {
		return "", fmt.Errorf("prompt artifact %q has empty .spec.prompt", full)
	}
	return pa.Spec.Prompt, nil
}
