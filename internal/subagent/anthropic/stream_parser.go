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
	"encoding/json"
	"fmt"
	"io"

	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// streamEvent is the verified Claude Code stream-json line shape (RESEARCH
// Pattern 5 lines 502-523). Only the fields Phase 3 actually consumes are
// declared; everything else (subtype, session_id, durations, model_usage)
// rides through to events.jsonl untouched for Phase 4 OpenInference parsing.
//
// JSON tags use snake_case to match the CLI's top-level shape. The per-model
// nested model_usage uses camelCase per the CLI's TypeScript pass-through —
// Phase 3 ignores that nested map because Phase 2's Usage struct does not
// expose per-model breakdown yet.
type streamEvent struct {
	Type   string       `json:"type"`
	Result string       `json:"result,omitempty"`
	Usage  *streamUsage `json:"usage,omitempty"`
}

// streamUsage mirrors the top-level `usage` block in a stream-json result
// event. Field names use snake_case to match what the CLI emits; the parser
// maps them to camelCase pkg/dispatch.Usage fields per D-C5 (CacheReadTokens,
// CacheCreationTokens drop the "Input" infix because Phase 2's
// pkg/dispatch.Usage already separates input/output via dedicated fields).
type streamUsage struct {
	InputTokens              int64 `json:"input_tokens"`
	OutputTokens             int64 `json:"output_tokens"`
	CacheReadInputTokens     int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
}

// ParseStream reads JSONL stream-json events from r, tees the raw bytes to
// rawSink (the per-Task events.jsonl audit log for Phase 4 OpenInference),
// and returns the final assistant text + token tally extracted from the
// terminal result event.
//
// Per D-C5: maps snake_case stream-json keys → camelCase pkg/dispatch.Usage
// fields:
//
//	input_tokens                → Usage.InputTokens
//	output_tokens               → Usage.OutputTokens
//	cache_read_input_tokens     → Usage.CacheReadTokens
//	cache_creation_input_tokens → Usage.CacheCreationTokens
//
// Defensive parsing: non-JSON lines (rare per RESEARCH Pattern 5) are written
// to rawSink and silently skipped — they do not cause ParseStream to return
// an error. The rationale (RESEARCH Pattern 5 line 555): "tolerate non-JSON
// lines (rare, but defensive)" — the audit log records everything; the
// orchestrator gets the structured tally.
//
// The 16MB per-line budget is enforced by [common.ReadLines]; lines longer
// than that return an error.
func ParseStream(r io.Reader, rawSink io.Writer) (pkgdispatch.Usage, string, error) {
	var (
		usage      pkgdispatch.Usage
		resultText string
	)

	err := common.ReadLines(r, func(line []byte) error {
		// Tee raw bytes (with newline) to events.jsonl. Phase 4 OpenInference
		// parsing reads this file untouched — Phase 3 only consumes the
		// terminal result event.
		if _, werr := rawSink.Write(append(line, '\n')); werr != nil {
			return fmt.Errorf("anthropic: write events.jsonl: %w", werr)
		}

		var ev streamEvent
		if jerr := json.Unmarshal(line, &ev); jerr != nil {
			// Tolerate non-JSON lines — they have already been teed to
			// rawSink for Phase 4 forensic analysis.
			return nil
		}

		if ev.Type == "result" {
			resultText = ev.Result
			if ev.Usage != nil {
				usage.InputTokens = ev.Usage.InputTokens
				usage.OutputTokens = ev.Usage.OutputTokens
				// D-C5 snake_case → camelCase mapping. Drop the "Input"
				// infix on cache fields: pkg/dispatch.Usage separates input
				// vs output via dedicated fields, so qualifying the cache
				// counters with "Input" would be redundant.
				usage.CacheReadTokens = ev.Usage.CacheReadInputTokens
				usage.CacheCreationTokens = ev.Usage.CacheCreationInputTokens
			}
		}
		return nil
	})
	if err != nil {
		return usage, resultText, err
	}
	return usage, resultText, nil
}
