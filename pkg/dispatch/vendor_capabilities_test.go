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

package dispatch

import "testing"

// TestSelfInstruments_KnownVendorsDefaultFalse asserts that every currently
// canonical vendor (per ProviderSpec.Vendor's doc comment) resolves to
// "does not self-instrument" — no self-instrumenting runtime exists yet, so
// the reporter's events.jsonl synthesizer (internal/reporter/tracesynth.go)
// must run for all of them today (ADAPT-01).
func TestSelfInstruments_KnownVendorsDefaultFalse(t *testing.T) {
	for _, v := range []string{"anthropic", "openai", "google", "xai", "opencode"} {
		if SelfInstruments(v) {
			t.Errorf("SelfInstruments(%q) = true, want false (no self-instrumenting runtime exists yet)", v)
		}
	}
}

// TestSelfInstruments_UnknownVendorDefaultsFalse pins D-03's default-safe
// polarity: an unrecognized or absent vendor string must default to
// "synthesize" (false), never to "assume native" (true) — a false "native"
// assumption silently drops spans, while a false "synthesize" assumption is
// at worst visible duplication (Pitfall 7).
func TestSelfInstruments_UnknownVendorDefaultsFalse(t *testing.T) {
	if SelfInstruments("some-future-unregistered-vendor") {
		t.Error("SelfInstruments on an unknown vendor = true, want false (D-03 fail-closed default)")
	}
	if SelfInstruments("") {
		t.Error(`SelfInstruments("") = true, want false`)
	}
}

// TestSelfInstruments_LangGraphTrue pins Phase 51 D-02: the LangGraph
// verifier image self-instruments via openinference-instrumentation-langchain
// in-process, so the reporter must skip events.jsonl synthesis for it — the
// first vendor to flip this predicate to true.
func TestSelfInstruments_LangGraphTrue(t *testing.T) {
	if !SelfInstruments("langgraph") {
		t.Error(`SelfInstruments("langgraph") = false, want true (self-instruments via openinference-instrumentation-langchain)`)
	}
}
