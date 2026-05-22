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

// Package config unit tests verifying YAML parsing, defaults, and
// validation. CTRL-04 specifies a Config struct with PlannerConcurrency,
// ExecutorConcurrency, and per-Kind MaxConcurrentReconciles.
package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeConfig writes content to a temp file and returns its path.
func writeConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return p
}

func TestConfigLoad_DefaultsApplied(t *testing.T) {
	// Empty YAML — every field should fall back to the documented default.
	p := writeConfig(t, "")
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load empty config: %v", err)
	}
	if cfg.PlannerConcurrency != 16 {
		t.Errorf("PlannerConcurrency = %d, want 16", cfg.PlannerConcurrency)
	}
	if cfg.ExecutorConcurrency != 4 {
		t.Errorf("ExecutorConcurrency = %d, want 4", cfg.ExecutorConcurrency)
	}
	want := MaxConcurrentReconciles{
		Project:   1,
		Milestone: 1,
		Phase:     2,
		Plan:      4,
		Wave:      8,
		Task:      16,
	}
	if cfg.MaxConcurrentReconciles != want {
		t.Errorf("MaxConcurrentReconciles = %+v, want %+v",
			cfg.MaxConcurrentReconciles, want)
	}
}

func TestConfigLoad_AllFieldsExplicit(t *testing.T) {
	yaml := `plannerConcurrency: 32
executorConcurrency: 8
maxConcurrentReconciles:
  project: 3
  milestone: 5
  phase: 7
  plan: 11
  wave: 13
  task: 17
`
	p := writeConfig(t, yaml)
	cfg, err := Load(p)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.PlannerConcurrency != 32 {
		t.Errorf("PlannerConcurrency = %d, want 32", cfg.PlannerConcurrency)
	}
	if cfg.ExecutorConcurrency != 8 {
		t.Errorf("ExecutorConcurrency = %d, want 8", cfg.ExecutorConcurrency)
	}
	want := MaxConcurrentReconciles{
		Project: 3, Milestone: 5, Phase: 7, Plan: 11, Wave: 13, Task: 17,
	}
	if cfg.MaxConcurrentReconciles != want {
		t.Errorf("MaxConcurrentReconciles = %+v, want %+v",
			cfg.MaxConcurrentReconciles, want)
	}
}

func TestConfigLoad_FileNotFound(t *testing.T) {
	_, err := Load("/nonexistent/path/to/config.yaml")
	if err == nil {
		t.Fatalf("expected error for missing file, got nil")
	}
}

func TestConfigLoad_InvalidYAML(t *testing.T) {
	p := writeConfig(t, "this: : is :: not :: yaml ::: at all\n  - mixed\n -indent")
	_, err := Load(p)
	if err == nil {
		t.Fatalf("expected error for malformed YAML, got nil")
	}
}

func TestConfigLoad_RejectsZeroValues(t *testing.T) {
	// Explicitly zero PlannerConcurrency — defaults should NOT silently
	// overwrite a user's explicit (invalid) choice. Validation rejects.
	p := writeConfig(t, "plannerConcurrency: 0\n")
	_, err := Load(p)
	if err == nil {
		t.Fatalf("expected error for plannerConcurrency=0, got nil")
	}
	if !strings.Contains(err.Error(), "plannerConcurrency") {
		t.Errorf("error %q should mention plannerConcurrency", err.Error())
	}
}

func TestConfigLoad_RejectsNegativeValues(t *testing.T) {
	p := writeConfig(t, "executorConcurrency: -1\n")
	_, err := Load(p)
	if err == nil {
		t.Fatalf("expected error for executorConcurrency=-1, got nil")
	}
	if !strings.Contains(err.Error(), "executorConcurrency") {
		t.Errorf("error %q should mention executorConcurrency", err.Error())
	}
}
