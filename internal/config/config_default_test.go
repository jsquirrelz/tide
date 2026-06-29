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

// Package config — PREFLIGHT-01 default resolution tests.
//
// These tests assert the controller-level half of Success Criterion #1: a fresh
// default deploy resolves an effective planner cap of 4 (not 16), so a single-node
// run cannot dispatch a 16-wide concurrent planning burst and OOM the node.
//
// No envtest, no fake K8s client — these are pure-Go tests that call config.Load
// on a minimal/empty YAML file written to a temp dir.
package config

import (
	"os"
	"path/filepath"
	"testing"
)

// TestDefaultPlannerConcurrencyIsFour verifies that resolving a default Config
// (all fields omitted) yields PlannerConcurrency == 4 (PREFLIGHT-01).
//
// This is the controller-level assertion for the configmap `plannerConcurrency`
// default correction: a fresh default deploy must cap concurrent planner
// dispatches at 4 (the single-node-safe value), never 16.
func TestDefaultPlannerConcurrencyIsFour(t *testing.T) {
	// Write an empty config.yaml to a temp dir — no plannerConcurrency key set.
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(""), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load on empty config: %v", err)
	}

	// PREFLIGHT-01 Success Criterion #1: default PlannerConcurrency must be 4.
	const wantPlannerConcurrency = 4
	if cfg.PlannerConcurrency != wantPlannerConcurrency {
		t.Errorf("PlannerConcurrency = %d, want %d (PREFLIGHT-01: stale default 16 would OOM a single-node run)",
			cfg.PlannerConcurrency, wantPlannerConcurrency)
	}
}

// TestDefaultPlannerConcurrencyExplicitOverrideWins verifies that an explicit
// plannerConcurrency value overrides the default — the default applies only when
// the key is absent, not unconditionally.
//
// Boundary assertion for PREFLIGHT-01: this ensures the resolution uses the
// standard nil-pointer-check pattern (omitted → default; explicit → explicit)
// and does not unconditionally cap at 4.
func TestDefaultPlannerConcurrencyExplicitOverrideWins(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte("plannerConcurrency: 2\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	const wantOverride = 2
	if cfg.PlannerConcurrency != wantOverride {
		t.Errorf("PlannerConcurrency = %d, want %d (explicit override must win over default)",
			cfg.PlannerConcurrency, wantOverride)
	}
}
