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

package eval

import (
	"bytes"
	"os"
	"strings"
	"testing"

	subagentanthropic "github.com/jsquirrelz/tide/internal/subagent/anthropic"
)

// TestCostReplay_ParseStream replays the synthetic four-field-Usage fixture
// (testdata/fixtures/stream_real.jsonl) through anthropic.ParseStream and
// asserts that all four token dimensions are returned with the expected values.
//
// This test exercises the path from committed JSONL fixture → ParseStream →
// pkgdispatch.Usage, proving the four dimensions (InputTokens, OutputTokens,
// CacheReadTokens, CacheCreationTokens) flow through correctly. It does NOT
// call estimatedCostCents — that cost-arithmetic assertion lives in the
// in-package cost_parity_test.go (package anthropic) to access the unexported
// method.
//
// Fixture: testdata/fixtures/stream_real.jsonl models a cache-warm second
// dispatch with all four usage dimensions non-zero (D-04 revised, Pitfall 4).
func TestCostReplay_ParseStream(t *testing.T) {
	data, err := os.ReadFile("testdata/fixtures/stream_real.jsonl")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	var rawSink bytes.Buffer
	usage, resultText, err := subagentanthropic.ParseStream(strings.NewReader(string(data)), &rawSink)
	if err != nil {
		t.Fatalf("ParseStream: %v", err)
	}

	// The fixture carries: input=500, output=100, cache_read=800, cache_creation=1200.
	if usage.InputTokens != 500 {
		t.Errorf("InputTokens: got %d, want 500", usage.InputTokens)
	}
	if usage.OutputTokens != 100 {
		t.Errorf("OutputTokens: got %d, want 100", usage.OutputTokens)
	}
	if usage.CacheReadTokens != 800 {
		t.Errorf("CacheReadTokens: got %d, want 800", usage.CacheReadTokens)
	}
	if usage.CacheCreationTokens != 1200 {
		t.Errorf("CacheCreationTokens: got %d, want 1200", usage.CacheCreationTokens)
	}

	if resultText != "eval fixture final text" {
		t.Errorf("resultText: got %q, want %q", resultText, "eval fixture final text")
	}
}
