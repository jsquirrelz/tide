// Package anthropic implements pkg/dispatch.Subagent against Anthropic's
// Claude Code CLI. Per D-C1 (the provider firewall) all Anthropic-specific
// code lives here, NOT in pkg/dispatch, pkg/controller, or pkg/dag — the
// firewall is enforced at build time by tools/analyzers/providerfirewall.
//
// HARN-06 decision (03-RESEARCH §"Alternatives Considered"): we shell out to
// the `claude` CLI rather than embedding the Anthropic Go SDK. The CLI bundles
// the agent loop, hooks, MCP, skills, and bash/file tools that would otherwise
// have to be re-implemented in Go. Phase 3 does not depend on the Anthropic
// Go SDK module.
//
// CLAUDE.md anti-pattern guardrails baked in:
//   - NEVER mount the host claude config dir — the --bare flag (RESEARCH
//     §"Critical new finding") skips auto-discovery of hooks/skills/plugins/MCP
//     and any CLAUDE-doc auto-memory, so the per-Pod runtime is hermetic.
//   - NEVER use OAuth headless — claude-code#29983, #7100 break it. We pin
//     ANTHROPIC_API_KEY to the signed token minted by the controller and
//     validated by the credproxy sidecar (Phase 2 D-C1..C4).
//   - NEVER embed an LLM API key directly — the API key lives only in the
//     credproxy sidecar; this binary sees a short-lived HMAC token.
package anthropic

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// vendorSentinel is the Provider.Vendor value this binary accepts. The
// envelope.Provider.Vendor field is the compile-time agreement between the
// orchestrator-resolved provider triple and the running subagent image — if
// they disagree, we refuse to dispatch (Pitfall 14 mitigation).
const vendorSentinel = "anthropic"

// paramsAllowList enforces Q3 RESOLVED (03-RESEARCH §"Open Questions" line
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
//     common.LoadPromptTemplate(in.Role, in.Level) — never read the host
//     filesystem for prompt content (CLAUDE.md anti-pattern).
//  4. Build `claude -p <rendered> --model <Provider.Model> --output-format
//     stream-json --verbose --include-partial-messages --bare` (the --bare
//     flag is REQUIRED per RESEARCH §"Critical new finding").
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

	// 2. Params allow-list fail-fast (Q3 RESOLVED — 03-RESEARCH line 933).
	for key := range in.Provider.Params {
		if !paramsAllowList[key] {
			return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: unknown param %q (allowed: temperature, thinking_budget, top_p, top_k)", key)
		}
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

	// 4. Build the claude CLI invocation. The --bare flag is REQUIRED per
	// RESEARCH §"Critical new finding" — it skips auto-discovery of the
	// host claude config dir, .mcp.json, hooks, skills, plugins, and any
	// project CLAUDE-doc auto-memory. Hermetic by construction.
	args := []string{
		"-p", renderedPrompt,
		"--model", in.Provider.Model,
		"--output-format", "stream-json",
		"--verbose",
		"--include-partial-messages",
		"--bare",
	}
	cmd := a.execFunc(ctx, a.opts.ClaudeBinary, args...)

	// 5. Credproxy env wiring (Phase 2 D-C1..C4). ANTHROPIC_API_KEY carries
	// the HMAC-signed token, NEVER the raw API key — the real key lives
	// only in the credproxy sidecar's Secret mount.
	cmd.Env = append(cmd.Environ(),
		"ANTHROPIC_BASE_URL="+in.ProxyEndpoint,
		"ANTHROPIC_API_KEY="+in.SignedToken,
		"NODE_EXTRA_CA_CERTS="+nodeExtraCACertsPath,
	)

	// 6. Create the events.jsonl audit log. Phase 4 OpenInference parsing
	// reads this file untouched; we tee every line through ParseStream as
	// it arrives.
	eventsDir := filepath.Join(a.opts.WorkspaceRoot, "envelopes", in.TaskUID)
	if err := os.MkdirAll(eventsDir, 0o755); err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: mkdir events dir %q: %w", eventsDir, err)
	}
	eventsPath := filepath.Join(eventsDir, "events.jsonl")
	eventsFile, err := os.Create(eventsPath)
	if err != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: create events.jsonl %q: %w", eventsPath, err)
	}
	defer eventsFile.Close()

	// 7. Pipe stdout → ParseStream(stdout, events.jsonl). Stderr surfaces
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
	//   - parse error on the stream → dispatch-level (return err).
	//   - wait error: task-level (ExitCode from exec.ExitError, Reason from stderr).
	if parseErr != nil {
		return pkgdispatch.EnvelopeOut{}, fmt.Errorf("anthropic subagent: parse stream: %w", parseErr)
	}

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
