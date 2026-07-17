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

// tracesynth.go is the Phase 44 LLM message-array-span synthesizer. It reads
// a completed dispatch's events.jsonl (plus in.json for the out-of-band
// call-1 prompt) and emits one redacted, size-bounded OpenInference LLM-kind
// span per API call (D-01).
//
// This is specifically the anthropic-CLI runtime's trace adapter behind the
// Phase 45 ADAPT-01 seam: it parses that runtime's events.jsonl stream
// format and nothing else. pkg/dispatch.SelfInstruments is the routing
// datum the manager consults to decide whether a dispatch's reporter Job
// should invoke this parser at all — see cmd/tide-reporter/main.go's
// synthesizeSpans skip guard, which is the sole point that honors it.
//
// Like materialize.go, this file is intentionally import-safe from cmd
// binaries: its only dependencies are the standard library,
// go.opentelemetry.io/otel, internal/harness/redact, internal/subagent/common,
// and pkg/otelai — no back-edge into the controller package.
//
// Spans are created and closed within EmitSpans's per-call loop iteration —
// never held open across a function return (mirrors the controller
// package's span_emission.go constraint).
package reporter

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/jsquirrelz/tide/internal/harness/redact"
	"github.com/jsquirrelz/tide/internal/subagent/common"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// maxMessageContentBytes is the D-08 per-message truncation floor (MSG-03).
// Positioned ~1 KiB above the real p99 single-turn size (31,020 B) observed
// across 3,217 real conversation turns (RESEARCH.md Size-Boundary Model) —
// this threshold almost never fires on real conversational content and
// exists specifically as a backstop against one pathological turn (e.g. a
// tool result that dumps an entire generated file).
const maxMessageContentBytes = 32 * 1024

// maxSpanPayloadBytes is the whole-span secondary backstop (RESEARCH.md
// Size-Boundary Model) bounding the SUM of one LLM span's
// LLMInputMessages+LLMOutputMessages attribute bytes. The real max observed
// per-call payload is 334 KiB, so this will not trigger on real content
// today but bounds future growth (larger repos, more turns per call)
// without requiring per-message truncation to shrink an already-small
// aggregate.
const maxSpanPayloadBytes = 512 * 1024

// truncationHalf is the D-08 head/tail split: keep the first and last half
// of maxMessageContentBytes, eliding the middle with an explicit marker.
const truncationHalf = maxMessageContentBytes / 2

// Usage carries one CallSpan's per-call (message_start + message_delta)
// token accounting (D-04).
type Usage struct {
	InputTokens         int
	OutputTokens        int
	CacheReadTokens     int
	CacheCreationTokens int
}

// CallSpan is one message_start..message_stop API-call cycle reconstructed
// from events.jsonl (D-01: one LLM span per API call). InputMessages is the
// conversation context at the moment the call began; OutputMessages is that
// call's own assistant turn, aggregated from every content block the CLI
// tee'd as separate assistant-type events (Pitfall 1).
type CallSpan struct {
	Model           string
	InputMessages   []otelai.Message
	OutputMessages  []otelai.Message
	Usage           Usage
	StartTime       time.Time
	EndTime         time.Time
	Degraded        bool
	TimingSynthetic bool
}

// ─── events.jsonl raw line shapes (verified against 58 real fixtures — ────
// ─── RESEARCH.md "events.jsonl Schema") ────────────────────────────────────

// rawEvent is the top-level JSONL line shape. Only stream_event, assistant,
// and user lines are load-bearing for reconstruction; system/result lines
// are read (Type matches neither case in the walk below) and skipped.
type rawEvent struct {
	Type      string          `json:"type"`
	Event     *rawStreamEvent `json:"event,omitempty"`
	Message   *rawMessage     `json:"message,omitempty"`
	Timestamp string          `json:"timestamp,omitempty"`
}

// rawStreamEvent is the nested "event" object on a stream_event line.
// message_start carries Message (model + usage); message_delta carries
// Usage directly (cumulative output_tokens, no repeated message);
// message_stop carries neither.
type rawStreamEvent struct {
	Type    string      `json:"type"`
	Message *rawMessage `json:"message,omitempty"`
	Usage   *rawUsage   `json:"usage,omitempty"`
}

// rawMessage is the "message" object shared by message_start (nested under
// event.message) and assistant/user top-level lines.
type rawMessage struct {
	ID      string            `json:"id,omitempty"`
	Model   string            `json:"model,omitempty"`
	Content []rawContentBlock `json:"content,omitempty"`
	Usage   *rawUsage         `json:"usage,omitempty"`
}

// rawUsage mirrors the stream's usage block (snake_case wire field names).
type rawUsage struct {
	InputTokens              int `json:"input_tokens,omitempty"`
	OutputTokens             int `json:"output_tokens,omitempty"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens,omitempty"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens,omitempty"`
}

// rawContentBlock is one assistant content block (thinking | tool_use |
// text) or one user tool_result block. A single assistant-type JSONL line
// carries exactly one content block (Pitfall 1) — multiple lines share the
// same Message.ID within one message_start..message_stop window.
type rawContentBlock struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	Thinking  string          `json:"thinking,omitempty"`
	Signature string          `json:"signature,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	// Content is the tool_result payload, which the wire format sends as
	// either a plain JSON string or an array of {type,text} blocks —
	// stringifyToolResultContent handles both shapes.
	Content json.RawMessage `json:"content,omitempty"`
}

// promptArtifact is the shape read at .promptPath — mirrors
// internal/subagent/anthropic/subagent.go's promptArtifact exactly (the same
// children/task-NN.json convention).
type promptArtifact struct {
	Spec struct {
		Prompt string `json:"prompt"`
	} `json:"spec"`
}

// envelopeInSeed is the minimal EnvelopeIn subset needed to seed
// conversation turn 0 (Pitfall 2).
type envelopeInSeed struct {
	Prompt     string `json:"prompt"`
	PromptPath string `json:"promptPath,omitempty"`
}

// pendingCall accumulates one in-progress message_start..message_stop cycle.
type pendingCall struct {
	model         string
	inputSnapshot []otelai.Message
	blocks        []rawContentBlock
	usage         Usage
	startTime     time.Time
	degraded      bool
}

// seedPrompt resolves the call-1 seed turn per Pitfall 2: read inJSONPath's
// .prompt directly, or follow .promptPath one hop (workspace-relative,
// resolved INSIDE workspaceRoot) to a children/task-NN.json artifact's
// .spec.prompt. Returns ("", false) for any missing/unreadable/empty case —
// never an error; a missing seed degrades the first call (D-05) rather than
// failing reconstruction.
//
// .promptPath comes from in.json on the subagent-writable PVC, so it is
// attacker-populatable: resolution goes through os.Root, which confines the
// read to workspaceRoot against both ".."/absolute-path traversal and
// symlinks pointing outside the root. The reporter reads files the subagent
// cannot — an unconfined read here would be a privilege step-up straight
// into the trace stream.
func seedPrompt(inJSONPath, workspaceRoot string) (string, bool) {
	if inJSONPath == "" {
		return "", false
	}
	data, err := os.ReadFile(inJSONPath)
	if err != nil {
		return "", false
	}
	var in envelopeInSeed
	if err := json.Unmarshal(data, &in); err != nil {
		return "", false
	}
	if in.Prompt != "" {
		return in.Prompt, true
	}
	if in.PromptPath == "" {
		return "", false
	}
	root, err := os.OpenRoot(workspaceRoot)
	if err != nil {
		return "", false
	}
	defer func() { _ = root.Close() }() // read-only handle; close error is non-actionable cleanup
	pdata, err := root.ReadFile(in.PromptPath)
	if err != nil {
		return "", false // includes traversal/symlink escape attempts — degrade, don't read
	}
	var pa promptArtifact
	if err := json.Unmarshal(pdata, &pa); err != nil {
		return "", false
	}
	if pa.Spec.Prompt == "" {
		return "", false
	}
	return pa.Spec.Prompt, true
}

// stringifyToolResultContent flattens a tool_result block's "content" field,
// which the wire format sends as either a plain JSON string or an array of
// {type,text} blocks.
func stringifyToolResultContent(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(raw, &blocks); err == nil {
		parts := make([]string, 0, len(blocks))
		for _, b := range blocks {
			parts = append(parts, b.Text)
		}
		return strings.Join(parts, "")
	}
	return string(raw)
}

// buildAssistantMessage aggregates one call's accumulated content blocks
// (Pitfall 1: multiple assistant-type lines sharing one message.id) into a
// SINGLE otelai.Message per the D-03 content-block mapping: text blocks
// concatenate into Content; tool_use blocks become ToolCalls; thinking
// blocks become Contents (type "reasoning").
func buildAssistantMessage(blocks []rawContentBlock) otelai.Message {
	msg := otelai.Message{Role: "assistant"}
	var textParts []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			textParts = append(textParts, b.Text)
		case "tool_use":
			msg.ToolCalls = append(msg.ToolCalls, otelai.ToolCall{
				ID:            b.ID,
				Name:          b.Name,
				ArgumentsJSON: string(b.Input),
			})
		case "thinking":
			msg.Contents = append(msg.Contents, otelai.MessageContent{
				Type:      "reasoning",
				Text:      b.Thinking,
				Signature: b.Signature,
			})
		}
	}
	msg.Content = strings.Join(textParts, "")
	return msg
}

// buildUserMessage flattens a user-type event's tool_result block(s) into a
// single user-role otelai.Message.
func buildUserMessage(ev *rawEvent) otelai.Message {
	msg := otelai.Message{Role: "user"}
	if ev.Message == nil {
		return msg
	}
	parts := make([]string, 0, len(ev.Message.Content))
	for _, b := range ev.Message.Content {
		parts = append(parts, stringifyToolResultContent(b.Content))
	}
	msg.Content = strings.Join(parts, "\n")
	return msg
}

// ReconstructConversation walks eventsPath (tolerant-skip per D-11, via
// common.ReadLines which enforces the 16 MB per-line budget) and returns one
// CallSpan per message_start..message_stop cycle, seeded with the
// in.json/PromptPath-derived prompt as conversation turn 0 (Pitfall 2).
// workspaceRoot resolves a workspace-relative PromptPath (e.g.
// "envelopes/<plannerUID>/children/task-NN.json") independently of
// eventsPath's own directory, since the two need not share a parent.
//
// Unmarshal errors on a line are tolerated (D-11): the line is skipped and,
// if a call is currently open (between message_start and message_stop), that
// call is marked Degraded. A call still open at EOF (mid-stream kill, D-05)
// is emitted as a Degraded CallSpan rather than dropped or erred.
//
// Every returned CallSpan has TimingSynthetic=true unconditionally: no
// in-band absolute timestamp exists for message_start/message_delta/
// message_stop/assistant events (RESEARCH.md "Timestamps — confirmed
// asymmetric, not absent") — only user (tool_result) events carry one.
// StartTime/EndTime are interpolated from the nearest preceding/following
// user-event timestamp and left zero when none exists in either direction.
func ReconstructConversation(eventsPath, inJSONPath, workspaceRoot string) ([]CallSpan, error) {
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil, fmt.Errorf("ReconstructConversation: open %q: %w", eventsPath, err)
	}
	defer func() { _ = f.Close() }() // read-only fixture/PVC file; close error is non-actionable cleanup

	var conversation []otelai.Message
	firstCallDegraded := false
	if prompt, ok := seedPrompt(inJSONPath, workspaceRoot); ok {
		conversation = append(conversation, otelai.Message{Role: "user", Content: prompt})
	} else {
		firstCallDegraded = true
	}

	var calls []CallSpan
	var pending *pendingCall
	var lastUserTime time.Time
	haveLastUserTime := false

	closeCall := func() {
		if pending == nil {
			return
		}
		outMsg := buildAssistantMessage(pending.blocks)
		conversation = append(conversation, outMsg)
		cs := CallSpan{
			Model:           pending.model,
			InputMessages:   pending.inputSnapshot,
			OutputMessages:  []otelai.Message{outMsg},
			Usage:           pending.usage,
			StartTime:       pending.startTime,
			Degraded:        pending.degraded,
			TimingSynthetic: true,
		}
		if len(calls) == 0 {
			cs.Degraded = cs.Degraded || firstCallDegraded
		}
		calls = append(calls, cs)
		pending = nil
	}

	readErr := common.ReadLines(f, func(line []byte) error {
		var ev rawEvent
		if jerr := json.Unmarshal(line, &ev); jerr != nil {
			// D-11: tolerate non-JSON lines. Mark the in-progress call (if
			// any) degraded rather than propagating the parse error.
			if pending != nil {
				pending.degraded = true
			}
			return nil
		}

		switch ev.Type {
		case "stream_event":
			if ev.Event == nil {
				return nil
			}
			switch ev.Event.Type {
			case "message_start":
				if pending != nil {
					// Defensive: a dangling call before this one starts —
					// close it as degraded rather than dropping its content.
					pending.degraded = true
					closeCall()
				}
				startTime := time.Time{}
				if haveLastUserTime {
					startTime = lastUserTime
				}
				pending = &pendingCall{
					inputSnapshot: append([]otelai.Message(nil), conversation...),
					startTime:     startTime,
				}
				if ev.Event.Message != nil {
					pending.model = ev.Event.Message.Model
					if ev.Event.Message.Usage != nil {
						pending.usage.InputTokens = ev.Event.Message.Usage.InputTokens
						pending.usage.CacheReadTokens = ev.Event.Message.Usage.CacheReadInputTokens
						pending.usage.CacheCreationTokens = ev.Event.Message.Usage.CacheCreationInputTokens
					}
				}
			case "message_delta":
				if pending != nil && ev.Event.Usage != nil {
					pending.usage.OutputTokens = ev.Event.Usage.OutputTokens
				}
			case "message_stop":
				closeCall()
			}
		case "assistant":
			if pending != nil && ev.Message != nil {
				pending.blocks = append(pending.blocks, ev.Message.Content...)
			}
		case "user":
			userMsg := buildUserMessage(&ev)
			conversation = append(conversation, userMsg)
			if ev.Timestamp != "" {
				if ts, terr := time.Parse(time.RFC3339, ev.Timestamp); terr == nil {
					if len(calls) > 0 && calls[len(calls)-1].EndTime.IsZero() {
						calls[len(calls)-1].EndTime = ts
					}
					lastUserTime = ts
					haveLastUserTime = true
				}
			}
		}
		return nil
	})
	if readErr != nil {
		// D-11: a read error (e.g. bufio.ErrTooLong on one oversized line)
		// still returns every call reconstructed so far — including the
		// still-open one, flushed as Degraded — so the caller can emit the
		// partial conversation alongside the error.
		if pending != nil {
			pending.degraded = true
			closeCall()
		}
		return calls, fmt.Errorf("ReconstructConversation: read %q: %w", eventsPath, readErr)
	}

	// D-05/Pitfall: a call still open at EOF is a mid-stream kill — emit
	// whatever was reconstructed, marked Degraded, never an error.
	if pending != nil {
		pending.degraded = true
		closeCall()
	}

	return calls, nil
}

// truncateHeadTail returns s unchanged when len(s) <= limit; otherwise
// returns the first truncationHalf bytes + an explicit marker citing the
// elided byte count + the last truncationHalf bytes (D-08 head+tail shape —
// conversations carry signal at both ends). Byte-safe splitting may cut
// mid-UTF-8-rune, acceptable for a pathological-input backstop that almost
// never fires on real content (RESEARCH.md Size-Boundary Model).
//
// MUST only ever be called AFTER redact.String on the same string (D-09,
// locked, non-negotiable) — see redactTruncate, the sole call-site wrapper.
func truncateHeadTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	head := s[:truncationHalf]
	tail := s[len(s)-truncationHalf:]
	elided := len(s) - 2*truncationHalf
	return fmt.Sprintf("%s[... %d bytes truncated by TIDE ...]%s", head, elided, tail)
}

// redactTruncate is the sole call-site for the D-09 locked ordering:
// redact.String FIRST, then truncateHeadTail SECOND. Truncating first can
// split a secret across the cut so the pattern no longer matches, leaving a
// partial credential visible (RESEARCH.md Pitfall 4) — never reverse this
// order.
func redactTruncate(s string) string {
	redacted := redact.String(s)
	return truncateHeadTail(redacted, maxMessageContentBytes)
}

// boundMessages applies the D-09 redact-then-truncate pipeline to every
// message's Content, ToolCall.ArgumentsJSON, and reasoning Contents[].Text
// and .Signature, returning the bounded copies plus the attribute-value
// bytes they contribute to the span (message content, tool-call
// ID/name/arguments, reasoning text, and signature all counted).
func boundMessages(msgs []otelai.Message) ([]otelai.Message, int) {
	bounded := make([]otelai.Message, len(msgs))
	total := 0
	for i, m := range msgs {
		bm := otelai.Message{Role: m.Role}
		bm.Content = redactTruncate(m.Content)
		total += len(bm.Content)
		for _, tc := range m.ToolCalls {
			btc := otelai.ToolCall{
				ID:            tc.ID,
				Name:          tc.Name,
				ArgumentsJSON: redactTruncate(tc.ArgumentsJSON),
			}
			total += len(btc.ID) + len(btc.Name) + len(btc.ArgumentsJSON)
			bm.ToolCalls = append(bm.ToolCalls, btc)
		}
		for _, c := range m.Contents {
			// Signature rides the same subagent-writable stream as every
			// other content string — it passes the identical MSG-02
			// redact-then-truncate pipeline and counts toward the span budget.
			bc := otelai.MessageContent{
				Type:      c.Type,
				Text:      redactTruncate(c.Text),
				Signature: redactTruncate(c.Signature),
			}
			total += len(bc.Text) + len(bc.Signature)
			bm.Contents = append(bm.Contents, bc)
		}
		bounded[i] = bm
	}
	return bounded, total
}

// roleOnlyMessages returns role-only copies of msgs (content, tool calls,
// and reasoning blocks all dropped) — the degrade floor when the whole-span
// budget is exceeded, rather than attempting to truncate dozens of
// already-small messages individually.
func roleOnlyMessages(msgs []otelai.Message) []otelai.Message {
	roleOnly := make([]otelai.Message, len(msgs))
	for i, m := range msgs {
		roleOnly[i] = otelai.Message{Role: m.Role}
	}
	return roleOnly
}

// boundedSpanAttrs bounds BOTH sides of one span under a SINGLE
// maxSpanPayloadBytes budget: the cap applies to the SUM of the span's
// LLMInputMessages+LLMOutputMessages attribute bytes — the whole-span
// invariant the reporter Job's OTEL_BSP_MAX_EXPORT_BATCH_SIZE=6 batch math
// depends on (6 spans × 512 KiB = 3 MiB, ~25% headroom under the 4 MiB OTLP
// gRPC ceiling; reporter_jobspec.go). When the joint budget is exceeded, the
// larger side degrades to role-only messages first; if the remaining side
// alone still exceeds the budget, it degrades too. Returns the flattened
// OpenInference attributes for both sides plus per-side degrade flags.
func boundedSpanAttrs(
	inputMsgs, outputMsgs []otelai.Message,
) (inputAttrs, outputAttrs []attribute.KeyValue, inputDegraded, outputDegraded bool) {
	inBounded, inTotal := boundMessages(inputMsgs)
	outBounded, outTotal := boundMessages(outputMsgs)

	if inTotal+outTotal > maxSpanPayloadBytes {
		if inTotal >= outTotal {
			inBounded, inTotal = roleOnlyMessages(inputMsgs), 0
			inputDegraded = true
		} else {
			outBounded, outTotal = roleOnlyMessages(outputMsgs), 0
			outputDegraded = true
		}
	}
	if inTotal+outTotal > maxSpanPayloadBytes {
		if inputDegraded {
			outBounded = roleOnlyMessages(outputMsgs)
			outputDegraded = true
		} else {
			inBounded = roleOnlyMessages(inputMsgs)
			inputDegraded = true
		}
	}
	return otelai.LLMInputMessages(inBounded), otelai.LLMOutputMessages(outBounded), inputDegraded, outputDegraded
}

// EmitSpans creates one LLM-kind child span per CallSpan under ctx's
// SpanContext (the caller extracts the parented context from --traceparent
// before calling), applying the redact-then-truncate pipeline (D-09) plus
// the joint whole-span budget (boundedSpanAttrs) to every message before
// calling otelai.LLMInputMessages/LLMOutputMessages. Spans are flat siblings
// under ctx — mirrors what openinference-instrumentation-langchain emits
// natively (runtime-neutrality lock) — and are created and closed within a
// single loop iteration, never held open across a return.
//
// artifactPath (the workspace-relative events.jsonl path, e.g.
// "envelopes/<taskUID>/events.jsonl") is stamped on EVERY span via
// otelai.ArtifactPath — a superset of MSG-03's truncation-time requirement;
// the full-fidelity pointer is always useful.
//
// Errors are reserved for programming-level failures; content-level
// problems (oversized messages, degraded calls) degrade to marker
// attributes rather than failing the run (D-10/D-11).
func EmitSpans(ctx context.Context, tracer trace.Tracer, calls []CallSpan, artifactPath string) error {
	for _, call := range calls {
		spanName := call.Model
		if spanName == "" {
			spanName = "llm"
		}

		// Zero-time fallbacks preserve temporal ordering: the first call of a
		// conversation has no preceding user timestamp (StartTime zero) while
		// its EndTime is back-filled from a historical user event, and the
		// last call has the inverse. Collapsing the missing side onto the
		// known side yields a correctly-ordered zero-duration span instead of
		// a negative-duration one (or one inflated by the Job-completion→
		// reporter-spawn latency). All timing here is already stamped
		// synthetic via otelai.TimingSynthetic below.
		startTime, endTime := call.StartTime, call.EndTime
		switch {
		case startTime.IsZero() && !endTime.IsZero():
			startTime = endTime
		case !startTime.IsZero() && endTime.IsZero():
			endTime = startTime
		case startTime.IsZero() && endTime.IsZero():
			now := time.Now()
			startTime, endTime = now, now
		}
		if endTime.Before(startTime) {
			endTime = startTime
		}
		_, span := tracer.Start(ctx, spanName, trace.WithTimestamp(startTime))

		inputAttrs, outputAttrs, inputDegraded, outputDegraded := boundedSpanAttrs(call.InputMessages, call.OutputMessages)

		span.SetAttributes(otelai.LLMSpanKind())
		// D-07: provider is deliberately hardcoded "anthropic" — this
		// synthesizer parses the Anthropic CLI stream format specifically.
		// Runtime-neutral routing lives one level up, in
		// pkg/dispatch.SelfInstruments plus the reporter's
		// --skip-message-spans skip guard (Phase 45) — this hardcoded
		// literal is correct because this function only ever runs for the
		// anthropic-CLI adapter path.
		span.SetAttributes(otelai.LLMIdentity("anthropic", call.Model)...)
		span.SetAttributes(inputAttrs...)
		span.SetAttributes(outputAttrs...)
		span.SetAttributes(otelai.TokenCount(
			// D-08: prompt = uncached + cache-read + cache-write subsets.
			call.Usage.InputTokens+call.Usage.CacheReadTokens+call.Usage.CacheCreationTokens,
			call.Usage.OutputTokens,
			call.Usage.CacheReadTokens,
			call.Usage.CacheCreationTokens,
		)...)
		span.SetAttributes(otelai.ArtifactPath(artifactPath))
		span.SetAttributes(otelai.TimingSynthetic())
		if call.Degraded || inputDegraded || outputDegraded {
			span.SetAttributes(otelai.ParseDegraded())
		}
		span.SetStatus(codes.Ok, "")
		span.End(trace.WithTimestamp(endTime))
	}
	return nil
}
