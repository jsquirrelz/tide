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

// run_mix_regression_test.go locks the 2026-07-03 first-run 2.8× budget
// overcount as a regression (D-04 / COST-01, CACHE-01 evidence style).
//
// Evidence: run date 2026-07-03 (CB-1605 log-levels run against
// cashboardfinance/cashboard-api). TIDE's final tally was $10.86 (1086¢) —
// every claude-sonnet-5 dispatch was billed at the conservative
// most-expensive tier because the price table had no sonnet-5 row — while the
// Anthropic developer console reported $3.84 (384¢) actual spend: a 2.8×
// overcount. Model mix per the fixture provenance (prose, unverified against
// envelopes): 1 Sonnet-5 + 1 Fable-5 + 2 Sonnet-5 planner dispatches,
// 3 Haiku-4.5 task executions.
//
// Intro-pricing caveat (RESEARCH Pitfall 1): the console's $3.84 reflects
// sonnet-5 INTRO pricing ($2/M input, $10/M output, through 2026-08-31). The
// compiled table deliberately carries the STICKER rates ($3/$15) — they never
// under-count and stay correct past the intro window — so this test must NOT
// assert equality with 384¢; the sticker-rate replay legitimately lands above
// it (bounded below 1.6× = 615¢).
//
// Fixture provenance (internal/subagent/anthropic/testdata/
// first_run_2026-07-03_usage.json, excerpted verbatim): "Envelopes found: 0
// of the ~6 expected Usage blocks (D-04). Per-dispatch token counts are
// unrecoverable and are NOT reconstructed here (D-04 forbids fabrication).
// Known run-level aggregates [...]: TIDE final tally $10.86 via the
// conservative unknown-model fallback vs $3.84 Anthropic console actual (2.8x
// overcount)". The tide-cashboard namespace and its PVC (reclaimPolicy
// Delete) were deleted before the export, destroying the envelope tree.
//
// RECONSTRUCTION DISCLOSURE (pre-authorized by plan 38-06 for the
// dispatches-empty case): because the fixture ships zero dispatches, the
// replay below uses SYNTHETIC token counts — they are NOT the real run's
// counts. They follow the provenance's prose model mix and were chosen so the
// documented run-level aggregates reproduce EXACTLY under both historical
// tables: the old conservative fallback (sonnet-5 → fable-5 rates) yields
// 1086¢ = $10.86, and intro pricing (2/3 × sticker on the sonnet-5 lines)
// yields 384¢ = $3.84. If real per-dispatch data is ever recovered, add it to
// the fixture's dispatches array — it takes precedence over the
// reconstruction automatically, and the pinned literal below must be
// re-derived.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// runMixDispatch mirrors the fixture's dispatches[] entry schema
// (38-01-PLAN artifacts table): {envelope, model, usage:{inputTokens,
// outputTokens, cacheReadTokens, cacheCreationTokens, estimatedCostCents}}.
// usage.estimatedCostCents (when present) is the OLD table's tally — kept for
// provenance, never replayed; estimatedCostCents(model, u) ignores it.
type runMixDispatch struct {
	Envelope string            `json:"envelope"`
	Model    string            `json:"model"`
	Usage    pkgdispatch.Usage `json:"usage"`
}

// runMixFixture is the top-level testdata JSON shape.
type runMixFixture struct {
	Provenance string           `json:"provenance"`
	Dispatches []runMixDispatch `json:"dispatches"`
}

// reconstructedRunMix is the provenance-disclosed synthetic dispatch set (see
// the file-header RECONSTRUCTION DISCLOSURE). Per-dispatch derivation, cents
// as ceil(numerator/1e6):
//
//	                                 old (fable rates)  new (sticker)  intro (2/3)
//	sonnet-5 planner  100k/30k/400k/108k       425¢          128¢          85¢
//	sonnet-5 planner   60k/20k/250k/80k        285¢           86¢          57¢
//	sonnet-5 planner   40k/10k/150k/50k        168¢           51¢          34¢
//	fable-5  planner   30k/10k/150k/30k        133¢          133¢         133¢
//	haiku-4-5 task ×3  60k/12k/300k/80k       25¢×3         25¢×3        25¢×3
//	                                 ────────────────  ─────────────  ───────────
//	                                     1086¢ ($10.86)   473¢          384¢ ($3.84)
//
// (fable-5 and haiku-4-5 rows were priced correctly in the old table, so
// their old/new/intro costs are identical.)
var reconstructedRunMix = []runMixDispatch{
	{Envelope: "reconstructed-planner-1", Model: "claude-sonnet-5", Usage: pkgdispatch.Usage{
		InputTokens: 100_000, OutputTokens: 30_000, CacheReadTokens: 400_000, CacheCreationTokens: 108_000,
	}},
	{Envelope: "reconstructed-planner-2", Model: "claude-sonnet-5", Usage: pkgdispatch.Usage{
		InputTokens: 60_000, OutputTokens: 20_000, CacheReadTokens: 250_000, CacheCreationTokens: 80_000,
	}},
	{Envelope: "reconstructed-planner-3", Model: "claude-sonnet-5", Usage: pkgdispatch.Usage{
		InputTokens: 40_000, OutputTokens: 10_000, CacheReadTokens: 150_000, CacheCreationTokens: 50_000,
	}},
	{Envelope: "reconstructed-planner-4", Model: "claude-fable-5", Usage: pkgdispatch.Usage{
		InputTokens: 30_000, OutputTokens: 10_000, CacheReadTokens: 150_000, CacheCreationTokens: 30_000,
	}},
	{Envelope: "reconstructed-task-1", Model: "claude-haiku-4-5", Usage: pkgdispatch.Usage{
		InputTokens: 60_000, OutputTokens: 12_000, CacheReadTokens: 300_000, CacheCreationTokens: 80_000,
	}},
	{Envelope: "reconstructed-task-2", Model: "claude-haiku-4-5", Usage: pkgdispatch.Usage{
		InputTokens: 60_000, OutputTokens: 12_000, CacheReadTokens: 300_000, CacheCreationTokens: 80_000,
	}},
	{Envelope: "reconstructed-task-3", Model: "claude-haiku-4-5", Usage: pkgdispatch.Usage{
		InputTokens: 60_000, OutputTokens: 12_000, CacheReadTokens: 300_000, CacheCreationTokens: 80_000,
	}},
}

// TestRunMixRegression_FirstRun20260703 replays the 2026-07-03 run mix
// through the current price table (D-04). Three separate locks:
//
//	(a) the exact pinned tally — any table/normalizer drift moves it;
//	(b) < 615¢ (1.6 × the console's 384¢) — sticker rates may legitimately
//	    exceed the intro-priced bill, but only within the Pitfall-1 tolerance;
//	(c) < 1086¢ — the 2.8× conservative-fallback overcount can never recur
//	    without a red test.
func TestRunMixRegression_FirstRun20260703(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "first_run_2026-07-03_usage.json"))
	if err != nil {
		t.Fatalf("read run-mix fixture: %v", err)
	}
	var fx runMixFixture
	if err := json.Unmarshal(data, &fx); err != nil {
		t.Fatalf("parse run-mix fixture: %v", err)
	}
	if fx.Provenance == "" {
		t.Fatal("fixture provenance is empty — the evidence trail is load-bearing (D-04)")
	}

	dispatches := fx.Dispatches
	if len(dispatches) == 0 {
		// Zero-recovery outcome (38-01): the PVC holding the envelopes was
		// deleted before export. Fall back to the provenance-disclosed
		// reconstruction — never skip this test silently (plan 38-06).
		dispatches = reconstructedRunMix
	}

	a := New(Options{})
	var totalCents int64
	for _, d := range dispatches {
		totalCents += a.estimatedCostCents(d.Model, d.Usage)
	}

	// (a) Pinned exact tally. Derivation: 128 + 86 + 51 (sonnet-5 planners at
	// sticker 300/1500/30/375) + 133 (fable-5 planner) + 3×25 (haiku-4-5
	// tasks) = 473¢. Re-derive this literal if the fixture ever gains real
	// dispatches or a table rate legitimately changes.
	const pinnedCents = int64(473)
	if totalCents != pinnedCents {
		t.Errorf("run-mix tally drifted: got %d cents, pinned %d — price table or normalizer changed; re-derive the pin only for an intentional rate change", totalCents, pinnedCents)
	}

	// (b) Pitfall-1 tolerance: sticker-rate replay must stay under 1.6× the
	// console's intro-priced 384¢ actual.
	if totalCents >= 615 {
		t.Errorf("run-mix tally %d cents >= 615 (1.6 × the $3.84 console actual) — sticker rates should exceed the intro-priced bill only within tolerance", totalCents)
	}

	// (c) The headline regression: the old conservative-fallback tally was
	// 1086¢ ($10.86, a 2.8× overcount of the $3.84 console actual).
	if totalCents >= 1086 {
		t.Errorf("run-mix tally %d cents >= 1086 — the 2.8× conservative-fallback overcount (COST-01) has regressed", totalCents)
	}
}
