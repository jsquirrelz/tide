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

package reporter

import (
	"testing"
)

// testSecret is the exact Anthropic-key-shaped fixture string already
// proven against internal/harness/redact's TestRedactingWriter/TestString
// tables — reused here so MSG-02's redaction tests exercise the project's
// single source of truth for secret patterns rather than a second fixture.
const testSecret = "sk-ant-api03-aBcDeFgHiJkLmNoPqRsTuV"

// ─── ReconstructConversation (Task 1, MSG-01 core) ─────────────────────────

// TestReconstructConversation is the MSG-01 core happy path against
// testdata/events_sample.jsonl (3 message_start..message_stop cycles) seeded
// from testdata/in_planner.json.
func TestReconstructConversation(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want exactly 3", len(calls))
	}

	// Call 1's input is exactly the seeded prompt turn.
	call1 := calls[0]
	if len(call1.InputMessages) != 1 {
		t.Fatalf("call1 InputMessages len = %d, want 1 (seed turn only)", len(call1.InputMessages))
	}
	if call1.InputMessages[0].Role != "user" {
		t.Errorf("call1 InputMessages[0].Role = %q, want %q", call1.InputMessages[0].Role, "user")
	}
	if call1.InputMessages[0].Content != "Investigate the plan and confirm tests pass." {
		t.Errorf("call1 InputMessages[0].Content = %q, want the in.json prompt", call1.InputMessages[0].Content)
	}

	// Call 1's output aggregates BOTH the thinking block and the tool_use
	// block into ONE assistant message (Pitfall 1).
	if len(call1.OutputMessages) != 1 {
		t.Fatalf("call1 OutputMessages len = %d, want 1 (aggregated)", len(call1.OutputMessages))
	}
	out1 := call1.OutputMessages[0]
	if len(out1.Contents) != 1 || out1.Contents[0].Type != "reasoning" {
		t.Errorf("call1 output Contents = %+v, want one reasoning block", out1.Contents)
	}
	if len(out1.ToolCalls) != 1 || out1.ToolCalls[0].Name != "Read" {
		t.Errorf("call1 output ToolCalls = %+v, want one Read tool call", out1.ToolCalls)
	}

	// Call 2's input grew by at least 2 turns versus call 1's input: the
	// assistant turn call 1 produced, plus at least one tool_result turn
	// (this fixture has two tool_result events after call 1).
	call2 := calls[1]
	if len(call2.InputMessages) < len(call1.InputMessages)+2 {
		t.Errorf("call2 InputMessages len = %d, want >= call1's (%d) + 2", len(call2.InputMessages), len(call1.InputMessages))
	}

	// Call 2's output is text-free (tool_use only); call 3's output is
	// text-only.
	out2 := calls[1].OutputMessages[0]
	if len(out2.ToolCalls) != 1 || out2.ToolCalls[0].Name != "Bash" {
		t.Errorf("call2 output ToolCalls = %+v, want one Bash tool call", out2.ToolCalls)
	}

	out3 := calls[2].OutputMessages[0]
	if out3.Content != "All tests pass. Task complete." {
		t.Errorf("call3 output Content = %q, want the fixture's text block", out3.Content)
	}
	if len(out3.ToolCalls) != 0 || len(out3.Contents) != 0 {
		t.Errorf("call3 output should carry no tool calls/reasoning blocks, got ToolCalls=%+v Contents=%+v", out3.ToolCalls, out3.Contents)
	}

	for i, c := range calls {
		if !c.TimingSynthetic {
			t.Errorf("calls[%d].TimingSynthetic = false, want true (no in-band absolute call timestamp exists)", i)
		}
		if c.Degraded {
			t.Errorf("calls[%d].Degraded = true, want false (clean fixture)", i)
		}
	}
}

// TestReconstructConversation_SeedsPromptFromPromptPath — Pitfall 2: an
// executor-shaped in.json (.prompt empty, .promptPath set) seeds turn 0 from
// the referenced children/task-NN.json's .spec.prompt, one hop away.
func TestReconstructConversation_SeedsPromptFromPromptPath(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_executor.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) == 0 {
		t.Fatal("got 0 calls, want at least 1")
	}
	if len(calls[0].InputMessages) != 1 {
		t.Fatalf("call1 InputMessages len = %d, want 1", len(calls[0].InputMessages))
	}
	want := "Fix the flaky test and open a PR."
	if got := calls[0].InputMessages[0].Content; got != want {
		t.Errorf("call1 InputMessages[0].Content = %q, want %q (children/task-01.json's .spec.prompt)", got, want)
	}
}

// TestReconstructConversation_PerCallUsage — D-04: each CallSpan's Usage
// carries that call's message_start input/cache tokens plus the
// message_delta output_tokens.
func TestReconstructConversation_PerCallUsage(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}

	wantUsage := []Usage{
		{InputTokens: 120, OutputTokens: 45, CacheReadTokens: 5, CacheCreationTokens: 10},
		{InputTokens: 300, OutputTokens: 30, CacheReadTokens: 120, CacheCreationTokens: 0},
		{InputTokens: 350, OutputTokens: 12, CacheReadTokens: 300, CacheCreationTokens: 0},
	}
	for i, want := range wantUsage {
		if got := calls[i].Usage; got != want {
			t.Errorf("calls[%d].Usage = %+v, want %+v", i, got, want)
		}
	}
}

// TestReconstructConversation_TolerantSkip — D-05/D-11: a fixture with a
// non-JSON garbage line AND a dangling message_start (no message_stop) still
// returns the prior complete call normally and the dangling call marked
// Degraded — never an error, never a panic.
func TestReconstructConversation_TolerantSkip(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_truncated.jsonl", "testdata/in_planner.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 2 {
		t.Fatalf("got %d calls, want 2 (1 complete + 1 dangling)", len(calls))
	}
	if calls[0].Degraded {
		t.Errorf("calls[0].Degraded = true, want false (complete call)")
	}
	if !calls[1].Degraded {
		t.Errorf("calls[1].Degraded = false, want true (dangling call, no message_stop + garbage line)")
	}
}

// TestReconstructConversation_MissingInJSON — D-05: an absent in.json
// reconstructs the conversation without a seed turn, marking only the first
// CallSpan Degraded — never an error.
func TestReconstructConversation_MissingInJSON(t *testing.T) {
	calls, err := ReconstructConversation("testdata/events_sample.jsonl", "testdata/does-not-exist.json", "testdata")
	if err != nil {
		t.Fatalf("ReconstructConversation: %v", err)
	}
	if len(calls) != 3 {
		t.Fatalf("got %d calls, want 3", len(calls))
	}
	if len(calls[0].InputMessages) != 0 {
		t.Errorf("call1 InputMessages = %+v, want empty (no seed turn, no prior turns)", calls[0].InputMessages)
	}
	if !calls[0].Degraded {
		t.Errorf("calls[0].Degraded = false, want true (missing in.json)")
	}
	if calls[1].Degraded {
		t.Errorf("calls[1].Degraded = true, want false (degraded marker scoped to first call only)")
	}
}
