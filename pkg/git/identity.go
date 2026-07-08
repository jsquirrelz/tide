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

package git

import "os"

// Agent-identity source of truth (SIGN-01 / D-04).
//
// All three TIDE commit sites — the harness task commit
// (internal/harness.CommitWorktree), the integrate merge commit
// (IntegrateTaskBranches, this package), and the tide-push boundary commit
// (cmd/tide-push) — derive their author/committer identity from AgentIdentity()
// so the compiled-in default lives in exactly one place. pkg/git is a leaf
// package, importable by all three without cycles.
//
// The two env-var names below (EnvAgentName / EnvAgentEmail) are the strings
// the controller stamps onto the subagent and push Job pods; unconfigured
// installs fall through to the compiled defaults. (These supersede the
// pre-Phase-36 per-site fallbacks, which every commit site duplicated inline.)
const (
	// DefaultAgentName is the compiled-in committer name used when the
	// EnvAgentName env var is unset or empty (D-04).
	DefaultAgentName = "TIDE Agent"
	// DefaultAgentEmail is the compiled-in committer email used when the
	// EnvAgentEmail env var is unset or empty (D-04). A routable address is
	// chosen deliberately so a future (deferred) signing feature can match it
	// against a verified machine-account email without churn.
	DefaultAgentEmail = "tide-agent@tideproject.k8s"

	// EnvAgentName is the env var that overrides DefaultAgentName. The
	// controller resolves it from Project spec → chart value → compiled
	// default and injects it into each commit-site pod.
	EnvAgentName = "TIDE_AGENT_NAME"
	// EnvAgentEmail is the env var that overrides DefaultAgentEmail, resolved
	// via the same precedence chain as EnvAgentName.
	EnvAgentEmail = "TIDE_AGENT_EMAIL"
)

// AgentIdentity returns the (name, email) pair TIDE stamps on commits it
// authors. Each field reads its env var (EnvAgentName / EnvAgentEmail) and
// falls back to the corresponding compiled default when the var is unset or
// empty; empty-string is treated as unset so a Helm value left at its zero
// default cleanly falls through rather than overriding with "". The two fields
// resolve independently. (SIGN-01 / D-04)
func AgentIdentity() (name, email string) {
	name = os.Getenv(EnvAgentName)
	if name == "" {
		name = DefaultAgentName
	}
	email = os.Getenv(EnvAgentEmail)
	if email == "" {
		email = DefaultAgentEmail
	}
	return name, email
}
