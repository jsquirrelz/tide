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

package main

import (
	"testing"
	"time"
)

// TestEnvOrDefault_StringEnvSet verifies the env-set path returns the env value verbatim.
// Phase 3 plan 03-09 D-C4 — TIDE_PUSH_IMAGE / CLAUDE_SUBAGENT_IMAGE / TIDE_DEFAULT_MODEL_*
// flow through this helper.
func TestEnvOrDefault_StringEnvSet(t *testing.T) {
	t.Setenv("TIDE_PUSH_IMAGE", "ghcr.io/test/push:test")
	got := envOrDefault("TIDE_PUSH_IMAGE", "fallback-image")
	if got != "ghcr.io/test/push:test" {
		t.Fatalf("envOrDefault: got %q, want %q", got, "ghcr.io/test/push:test")
	}
}

// TestEnvOrDefault_StringEnvUnset verifies the env-unset path returns the default.
func TestEnvOrDefault_StringEnvUnset(t *testing.T) {
	// Setenv-then-Unsetenv keeps test isolation explicit.
	t.Setenv("TIDE_PUSH_IMAGE", "")
	got := envOrDefault("TIDE_PUSH_IMAGE", "fallback-image")
	if got != "fallback-image" {
		t.Fatalf("envOrDefault: got %q, want %q", got, "fallback-image")
	}
}

// TestAtoiOrDefault_Valid verifies an integer-parseable env value is parsed.
func TestAtoiOrDefault_Valid(t *testing.T) {
	t.Setenv("TIDE_LEADER_LEASE_SECONDS", "30")
	got := atoiOrDefault("TIDE_LEADER_LEASE_SECONDS", 15)
	if got != 30 {
		t.Fatalf("atoiOrDefault: got %d, want 30", got)
	}
}

// TestAtoiOrDefault_Empty verifies an unset env returns the default.
func TestAtoiOrDefault_Empty(t *testing.T) {
	t.Setenv("TIDE_LEADER_LEASE_SECONDS", "")
	got := atoiOrDefault("TIDE_LEADER_LEASE_SECONDS", 15)
	if got != 15 {
		t.Fatalf("atoiOrDefault: got %d, want 15", got)
	}
}

// TestAtoiOrDefault_Garbage verifies a non-integer env value falls back to default.
func TestAtoiOrDefault_Garbage(t *testing.T) {
	t.Setenv("TIDE_LEADER_LEASE_SECONDS", "not-a-number")
	got := atoiOrDefault("TIDE_LEADER_LEASE_SECONDS", 15)
	if got != 15 {
		t.Fatalf("atoiOrDefault: got %d, want 15 (garbage env falls back)", got)
	}
}

// TestResolvePerLevelModels verifies that the per-level-default helper produces
// the D-C4 default map shape when env vars are unset.
// D-C4: milestone→claude-opus-4-8; phase→claude-sonnet-4-6; plan→claude-sonnet-4-6;
// task→claude-haiku-4-5.
func TestResolvePerLevelModels_AllDefaults(t *testing.T) {
	t.Setenv("TIDE_DEFAULT_MODEL_MILESTONE", "")
	t.Setenv("TIDE_DEFAULT_MODEL_PHASE", "")
	t.Setenv("TIDE_DEFAULT_MODEL_PLAN", "")
	t.Setenv("TIDE_DEFAULT_MODEL_TASK", "")
	got := resolvePerLevelModels()
	want := map[string]string{
		"milestone": "claude-opus-4-8",
		"phase":     "claude-sonnet-4-6",
		"plan":      "claude-sonnet-4-6",
		"task":      "claude-haiku-4-5",
	}
	for level, expected := range want {
		if got[level] != expected {
			t.Errorf("resolvePerLevelModels[%q]: got %q, want %q", level, got[level], expected)
		}
	}
}

// TestResolvePerLevelModels_EnvOverride verifies env vars override D-C4 defaults.
func TestResolvePerLevelModels_EnvOverride(t *testing.T) {
	t.Setenv("TIDE_DEFAULT_MODEL_MILESTONE", "override-opus")
	t.Setenv("TIDE_DEFAULT_MODEL_TASK", "override-haiku")
	got := resolvePerLevelModels()
	if got["milestone"] != "override-opus" {
		t.Errorf("resolvePerLevelModels[milestone]: got %q, want override-opus", got["milestone"])
	}
	if got["task"] != "override-haiku" {
		t.Errorf("resolvePerLevelModels[task]: got %q, want override-haiku", got["task"])
	}
	// Phase/Plan unset → defaults still apply.
	if got["phase"] != "claude-sonnet-4-6" {
		t.Errorf("resolvePerLevelModels[phase]: got %q, want claude-sonnet-4-6", got["phase"])
	}
}

// TestResolveLeaderElectionTiming verifies the leader-election durations are
// composed from env vars with controller-runtime-compatible defaults (D-D1).
// Plan 03-09 commit-time defaults: lease=15s renew=10s retry=2s.
func TestResolveLeaderElectionTiming_Defaults(t *testing.T) {
	t.Setenv("TIDE_LEADER_LEASE_SECONDS", "")
	t.Setenv("TIDE_LEADER_RENEW_SECONDS", "")
	t.Setenv("TIDE_LEADER_RETRY_SECONDS", "")
	lease, renew, retry := resolveLeaderElectionTiming()
	if lease != 15*time.Second {
		t.Errorf("lease: got %v, want 15s", lease)
	}
	if renew != 10*time.Second {
		t.Errorf("renew: got %v, want 10s", renew)
	}
	if retry != 2*time.Second {
		t.Errorf("retry: got %v, want 2s", retry)
	}
}

// TestResolveLeaderElectionTiming_EnvOverride verifies overrides land.
func TestResolveLeaderElectionTiming_EnvOverride(t *testing.T) {
	t.Setenv("TIDE_LEADER_LEASE_SECONDS", "60")
	t.Setenv("TIDE_LEADER_RENEW_SECONDS", "45")
	t.Setenv("TIDE_LEADER_RETRY_SECONDS", "5")
	lease, renew, retry := resolveLeaderElectionTiming()
	if lease != 60*time.Second {
		t.Errorf("lease: got %v, want 60s", lease)
	}
	if renew != 45*time.Second {
		t.Errorf("renew: got %v, want 45s", renew)
	}
	if retry != 5*time.Second {
		t.Errorf("retry: got %v, want 5s", retry)
	}
}
