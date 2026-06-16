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

// Env-variable helpers for Helm-driven configuration (Phase 3 plan 03-09).
//
// These helpers exist as testable units (cmd/manager/env_test.go) because
// the main() function itself is not unit-testable (it constructs the
// controller-runtime Manager and blocks on mgr.Start). Helpers cover the
// per-level model defaults (D-C4) and leader-election tuning (D-D1).
package main

import (
	"os"
	"strconv"
	"time"

	"github.com/jsquirrelz/tide/internal/controller"
)

// envOrDefault returns the value of the env var named key, or fallback when
// the env var is unset or empty. Empty-string is treated as unset so a Helm
// value left at its zero default (e.g. `value: ""`) cleanly falls through to
// the binary's compile-time default rather than overriding it with "".
func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// atoiOrDefault returns the integer value of the env var named key, or
// fallback when the env var is unset, empty, or not an integer.
// Non-integer values are tolerated (return fallback) so a stray non-numeric
// value in the Helm chart cannot crash-loop the controller.
func atoiOrDefault(key string, fallback int) int {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

// resolvePerLevelModels returns the four-key map from level name
// ("milestone"|"phase"|"plan"|"task") to the model identifier used as the
// last-fallback in dispatch_helpers.ResolveProvider (D-C2).
//
// Defaults match D-C4 — phase brief lock-in:
//
//	milestone → claude-opus-4-8    (heavy planning, lowest fan-out)
//	phase     → claude-sonnet-4-6
//	plan      → claude-sonnet-4-6
//	task      → claude-haiku-4-5   (highest fan-out, cost-bounded)
//
// Override per-level via env vars (set by the manager Deployment from Helm
// values.yaml subagent.levels.{level}.model):
//
//	TIDE_DEFAULT_MODEL_MILESTONE
//	TIDE_DEFAULT_MODEL_PHASE
//	TIDE_DEFAULT_MODEL_PLAN
//	TIDE_DEFAULT_MODEL_TASK
func resolvePerLevelModels() map[string]string {
	return map[string]string{
		"milestone": envOrDefault("TIDE_DEFAULT_MODEL_MILESTONE", "claude-opus-4-8"),
		"phase":     envOrDefault("TIDE_DEFAULT_MODEL_PHASE", "claude-sonnet-4-6"),
		"plan":      envOrDefault("TIDE_DEFAULT_MODEL_PLAN", "claude-sonnet-4-6"),
		"task":      envOrDefault("TIDE_DEFAULT_MODEL_TASK", "claude-haiku-4-5"),
	}
}

// tideHelmProviderDefaults builds the controller.ProviderDefaults struct
// (D-C2 last-fallback in ResolveProvider) from the Helm-chart subagent image
// + per-level model map. claudeImage is the default subagent image
// (CLAUDE_SUBAGENT_IMAGE env var); per-level models come from
// resolvePerLevelModels (TIDE_DEFAULT_MODEL_* env vars).
//
// Called once at Manager startup; the result is stamped onto each up-stack
// reconciler's HelmProviderDefaults field (MilestoneReconciler /
// PhaseReconciler / PlanReconciler).
func tideHelmProviderDefaults(claudeImage string) controller.ProviderDefaults {
	return controller.ProviderDefaults{
		Image:  claudeImage,
		Models: resolvePerLevelModels(),
	}
}

// resolveLeaderElectionTiming returns the (lease, renew, retry) durations
// for controller-runtime's LeaderElection block (D-D1 / Phase 3 chaos-resume).
//
// Defaults pin to common controller-runtime production values
// (lease=15s, renew=10s, retry=2s) which give a single-pod restart ≤25s
// failover ceiling, with the invariant lease > renew > retry preserved.
// Override via env vars (set by manager Deployment from Helm values.yaml
// leaderElection.*):
//
//	TIDE_LEADER_LEASE_SECONDS  (default 15)
//	TIDE_LEADER_RENEW_SECONDS  (default 10)
//	TIDE_LEADER_RETRY_SECONDS  (default 2)
func resolveLeaderElectionTiming() (lease, renew, retry time.Duration) {
	lease = time.Duration(atoiOrDefault("TIDE_LEADER_LEASE_SECONDS", 15)) * time.Second
	renew = time.Duration(atoiOrDefault("TIDE_LEADER_RENEW_SECONDS", 10)) * time.Second
	retry = time.Duration(atoiOrDefault("TIDE_LEADER_RETRY_SECONDS", 2)) * time.Second
	return lease, renew, retry
}
