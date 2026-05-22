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

package anthropic

import (
	"bytes"
	"strings"
	"testing"
)

// TestParseStream_HappyPath asserts a three-line JSONL stream containing a
// system/init, a stream_event, and a final result event with usage parses
// into the expected Usage + Result. Verifies all 4 token fields flow from
// snake_case stream-json → camelCase pkg/dispatch.Usage per D-C5.
func TestParseStream_HappyPath(t *testing.T) {
	const fixture = `{"type":"system/init","session_id":"sess-1"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"hello"}}}
{"type":"result","result":"final assistant text","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":30,"cache_creation_input_tokens":20},"total_cost_usd":0.0012}
`
	var rawSink bytes.Buffer
	usage, resultText, err := ParseStream(strings.NewReader(fixture), &rawSink)
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	if resultText != "final assistant text" {
		t.Errorf("resultText: got %q, want %q", resultText, "final assistant text")
	}
	if usage.InputTokens != 100 {
		t.Errorf("InputTokens: got %d, want 100", usage.InputTokens)
	}
	if usage.OutputTokens != 50 {
		t.Errorf("OutputTokens: got %d, want 50", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 30 {
		t.Errorf("CacheReadTokens: got %d, want 30 (cache_read_input_tokens → CacheReadTokens, D-C5)", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 20 {
		t.Errorf("CacheCreationTokens: got %d, want 20 (cache_creation_input_tokens → CacheCreationTokens, D-C5)", usage.CacheCreationTokens)
	}

	// rawSink must contain the verbatim fixture (3 lines, each with trailing \n).
	if rawSink.String() != fixture {
		t.Errorf("rawSink mismatch:\ngot:  %q\nwant: %q", rawSink.String(), fixture)
	}
}

// TestParseStream_UsageMapping isolates the snake_case→camelCase mapping in a
// minimal one-event fixture. Useful for diagnosing field-name regressions if
// the CLI stream-json schema drifts (Risk 2 in 03-RESEARCH).
func TestParseStream_UsageMapping(t *testing.T) {
	const fixture = `{"type":"result","result":"x","usage":{"input_tokens":1,"output_tokens":2,"cache_read_input_tokens":3,"cache_creation_input_tokens":4}}
`
	usage, _, err := ParseStream(strings.NewReader(fixture), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	want := struct{ I, O, CR, CC int64 }{1, 2, 3, 4}
	got := struct{ I, O, CR, CC int64 }{usage.InputTokens, usage.OutputTokens, usage.CacheReadTokens, usage.CacheCreationTokens}
	if got != want {
		t.Errorf("usage mapping: got %+v, want %+v", got, want)
	}
}

// TestParseStream_ToleratesNonJSON asserts that a non-JSON line between two
// JSON events is written to rawSink but does not cause an error — mirroring
// RESEARCH Pattern 5 lines 552-555 (defensive: continue on json.Unmarshal
// failure).
func TestParseStream_ToleratesNonJSON(t *testing.T) {
	const fixture = `{"type":"system/init"}
not-valid-json garbage line
{"type":"result","result":"ok","usage":{"input_tokens":7,"output_tokens":3}}
`
	var rawSink bytes.Buffer
	usage, resultText, err := ParseStream(strings.NewReader(fixture), &rawSink)
	if err != nil {
		t.Fatalf("ParseStream tolerated non-JSON: %v", err)
	}
	if resultText != "ok" {
		t.Errorf("resultText: got %q, want %q", resultText, "ok")
	}
	if usage.InputTokens != 7 || usage.OutputTokens != 3 {
		t.Errorf("usage parsed despite non-JSON line: got input=%d output=%d, want 7/3",
			usage.InputTokens, usage.OutputTokens)
	}
	// Non-JSON line must still appear in rawSink (the events.jsonl audit log
	// records EVERYTHING the CLI emitted, including malformed lines, for
	// Phase 4 OpenInference forensic analysis).
	if !strings.Contains(rawSink.String(), "not-valid-json garbage line") {
		t.Error("rawSink missing non-JSON line; want it teed through verbatim")
	}
}

// TestParseStream_NoResultEvent asserts that a stream containing no `result`
// event returns zero-valued Usage + empty Result without error. The harness
// will surface this as an empty EnvelopeOut.Result; this is observable rather
// than fatal.
func TestParseStream_NoResultEvent(t *testing.T) {
	const fixture = `{"type":"system/init"}
{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"text_delta","text":"partial"}}}
`
	usage, resultText, err := ParseStream(strings.NewReader(fixture), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}
	if resultText != "" {
		t.Errorf("resultText: got %q, want empty", resultText)
	}
	if usage.InputTokens != 0 || usage.OutputTokens != 0 {
		t.Errorf("usage should be zero when no result event: got %+v", usage)
	}
}
