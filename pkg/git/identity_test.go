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

import "testing"

// TestAgentIdentityDefault asserts that with both env vars unset, AgentIdentity()
// returns the compiled-in default pair (D-04).
func TestAgentIdentityDefault(t *testing.T) {
	t.Setenv(EnvAgentName, "")
	t.Setenv(EnvAgentEmail, "")

	name, email := AgentIdentity()
	if name != DefaultAgentName {
		t.Errorf("name: got %q, want %q", name, DefaultAgentName)
	}
	if email != DefaultAgentEmail {
		t.Errorf("email: got %q, want %q", email, DefaultAgentEmail)
	}
	// Pin the concrete default strings so a rename cannot silently pass.
	if name != "TIDE Agent" {
		t.Errorf("DefaultAgentName drift: got %q, want %q", name, "TIDE Agent")
	}
	if email != "tide-agent@tideproject.k8s" {
		t.Errorf("DefaultAgentEmail drift: got %q, want %q", email, "tide-agent@tideproject.k8s")
	}
}

// TestAgentIdentityOverride asserts that both env vars override the defaults.
func TestAgentIdentityOverride(t *testing.T) {
	t.Setenv(EnvAgentName, "Custom Agent")
	t.Setenv(EnvAgentEmail, "custom@example.com")

	name, email := AgentIdentity()
	if name != "Custom Agent" {
		t.Errorf("name: got %q, want %q", name, "Custom Agent")
	}
	if email != "custom@example.com" {
		t.Errorf("email: got %q, want %q", email, "custom@example.com")
	}
}

// TestAgentIdentityPerVarIndependence asserts that empty-string env values are
// treated as unset, and each var falls back to its default independently: a set
// name with an unset email yields the custom name and the default email.
func TestAgentIdentityPerVarIndependence(t *testing.T) {
	t.Setenv(EnvAgentName, "Custom Agent")
	t.Setenv(EnvAgentEmail, "")

	name, email := AgentIdentity()
	if name != "Custom Agent" {
		t.Errorf("name: got %q, want %q", name, "Custom Agent")
	}
	if email != DefaultAgentEmail {
		t.Errorf("email: got %q, want default %q (empty env is unset)", email, DefaultAgentEmail)
	}
}
