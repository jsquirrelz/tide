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

// vendor_capabilities.go is the ADAPT-01 runtime-neutral adapter seam's
// routing datum. SelfInstruments answers "does this vendor's Subagent
// implementation emit OpenInference spans natively, in-process, during
// Run()?" — the manager consults this at reporter-spawn time (never the
// reporter itself, which trusts only the manager-computed boolean carried on
// the Job — D-02) to decide whether internal/reporter/tracesynth.go's
// events.jsonl parser (the anthropic-CLI runtime's own trace adapter) should
// run at all.
//
// Default-safe (D-03, Pitfall 7): every current and unrecognized vendor
// returns false. A false "native" assumption silently produces zero spans;
// a false "synthesize" assumption produces (at worst, once a self-
// instrumenting runtime exists) visible duplicates — always fail toward
// visibility, never toward silence.
package dispatch

// SelfInstruments reports whether vendor's Subagent implementation emits
// OpenInference spans natively, in-process, so the reporter should skip
// internal/reporter/tracesynth.go's events.jsonl-based synthesis entirely
// for dispatches to that vendor. Keep the vendor literals identical to
// ProviderSpec.Vendor's doc comment.
func SelfInstruments(vendor string) bool {
	switch vendor {
	case "langgraph":
		return true // self-instruments via openinference-instrumentation-langchain; reporter skips events.jsonl synthesis
	case "anthropic", "openai", "google", "xai", "opencode":
		return false // CLI/wrapper-shimmed — no in-process OTel SDK
	default:
		return false // fail-closed: unknown vendor never skips synthesis
	}
}
