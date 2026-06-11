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

// PriceOverride is a provider-agnostic per-model price correction in US cents
// per million tokens. Zero cache fields derive from input (read=0.10x, write=1.25x).
//
// This type lives in pkg/dispatch (the provider-agnostic dispatch contract package,
// already imported by both cmd/manager and internal/subagent/anthropic) so the
// manager can validate the JSON at startup (Plan 14-05) WITHOUT importing the
// anthropic package — the provider firewall stays intact (D-C1).
type PriceOverride struct {
	InputCentsPerMTok      int64 `json:"inputCentsPerMTok"`
	OutputCentsPerMTok     int64 `json:"outputCentsPerMTok"`
	CacheReadCentsPerMTok  int64 `json:"cacheReadCentsPerMTok,omitempty"`
	CacheWriteCentsPerMTok int64 `json:"cacheWriteCentsPerMTok,omitempty"`
}

// ParsePricingOverrides unmarshals s (empty or "{}" → empty map, nil error) and
// validates: Input and Output MUST be > 0; cache fields MUST be >= 0.
//
// Validation per ASVS V5 / threat T-14-01: reject zero or negative
// InputCentsPerMTok/OutputCentsPerMTok and negative cache fields, with the model
// ID in the error string so the operator's fix is mechanical. A bad override must
// not fail the session (the manager validates at startup in Plan 14-05; the subagent
// performs defense-in-depth validation here).
func ParsePricingOverrides(s string) (map[string]PriceOverride, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "{}" {
		return map[string]PriceOverride{}, nil
	}

	var raw map[string]PriceOverride
	if err := json.Unmarshal([]byte(s), &raw); err != nil {
		return nil, fmt.Errorf("pricing overrides: invalid JSON: %w", err)
	}

	for modelID, p := range raw {
		if p.InputCentsPerMTok <= 0 {
			return nil, fmt.Errorf(
				"pricing overrides: model %q has invalid inputCentsPerMTok %d (must be > 0)",
				modelID, p.InputCentsPerMTok)
		}
		if p.OutputCentsPerMTok <= 0 {
			return nil, fmt.Errorf(
				"pricing overrides: model %q has invalid outputCentsPerMTok %d (must be > 0)",
				modelID, p.OutputCentsPerMTok)
		}
		if p.CacheReadCentsPerMTok < 0 {
			return nil, fmt.Errorf(
				"pricing overrides: model %q has negative cacheReadCentsPerMTok %d (must be >= 0)",
				modelID, p.CacheReadCentsPerMTok)
		}
		if p.CacheWriteCentsPerMTok < 0 {
			return nil, fmt.Errorf(
				"pricing overrides: model %q has negative cacheWriteCentsPerMTok %d (must be >= 0)",
				modelID, p.CacheWriteCentsPerMTok)
		}
	}

	return raw, nil
}
