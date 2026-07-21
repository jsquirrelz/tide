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

import (
	"encoding/json"
	"fmt"
	"strings"
)

// LevelVerifyDefault is a single dispatch level's chart-supplied verify
// posture default (D-01): whether verification is enabled by default at
// this level, and the LoopPolicy defaults (MaxIterations/OnExhaustion) to
// apply when no authored VerificationSpec overrides them.
//
// This type lives in pkg/dispatch (the provider-agnostic dispatch contract
// package, already imported by both cmd/manager and internal/controller) so
// the manager can validate the JSON at startup WITHOUT importing the
// controller package — the same provider-firewall reason PriceOverride
// lives here (D-C1). It is the transport schema for the chart's
// subagent.verify.levels map (D-01).
type LevelVerifyDefault struct {
	Enabled       bool   `json:"enabled"`
	MaxIterations int32  `json:"maxIterations,omitempty"`
	OnExhaustion  string `json:"onExhaustion,omitempty"`
}

// verifyLevelKeys is the closed set of dispatch levels a verify-levels-json
// key may name. Unlike PriceOverride's open model-ID keyspace, this
// keyspace is closed — an unrecognized key is very likely a typo in a
// hand-authored Helm value and must be rejected, not silently ignored.
var verifyLevelKeys = map[string]bool{
	"task":      true,
	"plan":      true,
	"phase":     true,
	"milestone": true,
	"project":   true,
}

// verifyOnExhaustionValues is the closed set of legal OnExhaustion values,
// matching the kubebuilder enum on VerificationSpec.OnExhaustion
// (api/v1alpha3/task_types.go) plus the empty string (unset — the resolver
// applies its own per-level default).
var verifyOnExhaustionValues = map[string]bool{
	"":                true,
	"escalate":        true,
	"requireApproval": true,
}

// ParseVerifyLevelDefaults unmarshals s (empty or "{}" -> empty map, nil
// error) and validates: every key MUST be one of task|plan|phase|milestone|
// project; MaxIterations MUST be >= 0; OnExhaustion MUST be one of
// ""|escalate|requireApproval.
//
// Validation per ASVS V5 / T-53-03: reject malformed JSON, unknown level
// keys, negative MaxIterations, and unrecognized OnExhaustion values, with
// the level and offending value named in the error string so the
// operator's fix is mechanical (the pricing.go doc-comment convention).
// Fail-closed: the manager validates this at startup (cmd/manager/main.go)
// and os.Exit(1)s on error rather than starting with a silently-broken or
// overly-permissive verify posture.
func ParseVerifyLevelDefaults(s string) (map[string]LevelVerifyDefault, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return map[string]LevelVerifyDefault{}, nil
	}

	var raw map[string]LevelVerifyDefault
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("verify level defaults: invalid JSON: %w", err)
	}

	for level, d := range raw {
		if !verifyLevelKeys[level] {
			return nil, fmt.Errorf(
				"verify level defaults: unknown level %q (must be one of task|plan|phase|milestone|project)",
				level)
		}
		if d.MaxIterations < 0 {
			return nil, fmt.Errorf(
				"verify level defaults: level %q has invalid maxIterations %d (must be >= 0)",
				level, d.MaxIterations)
		}
		if !verifyOnExhaustionValues[d.OnExhaustion] {
			return nil, fmt.Errorf(
				"verify level defaults: level %q has invalid onExhaustion %q (must be one of \"\"|escalate|requireApproval)",
				level, d.OnExhaustion)
		}
	}

	return raw, nil
}
