package gitleaks

import (
	"os"
	"path/filepath"
	"testing"
)

// Test fixtures use well-known secret-shaped strings that match the gitleaks
// v8 upstream ruleset (config/gitleaks.toml in github.com/gitleaks/gitleaks
// v8.30.1). The exact rule-id that matches is implementation-defined; tests
// assert on finding count (>=1 for known patterns, ==0 for clean input).

const (
	// sk-ant-* (Anthropic API key) — matches gitleaks rule "anthropic-api-key".
	anthropicKeyDiff = "Some context line\n" +
		"+ANTHROPIC_API_KEY=sk-ant-api03-AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n" +
		"Other line\n"

	// AKIA-style AWS access key — matches gitleaks rule "aws-access-token".
	awsKeyDiff = "+AWS_ACCESS_KEY_ID=AKIAIOSFODNN7EXAMPLE\n"

	// No secrets — must return zero findings.
	cleanDiff = "+func main() {\n" +
		"+  fmt.Println(\"hello\")\n" +
		"+}\n"

	// Custom rule fixture — does NOT match upstream gitleaks rules; only
	// matches when a custom rule is composed via [extend] useDefault = true.
	customSecretDiff = "+CUSTOM_SECRET_ABC123\n"
)

// TestScanDiff exercises the embedded default ruleset against known
// secret-shaped inputs and a clean input. See plan 03-04 task behavior
// cases 1, 2, and 3.
func TestScanDiff(t *testing.T) {
	tests := []struct {
		name           string
		input          string
		wantFound      bool
		wantMinFinding int
	}{
		{
			name:           "AnthropicAPIKey",
			input:          anthropicKeyDiff,
			wantFound:      true,
			wantMinFinding: 1,
		},
		{
			name:           "AWSAccessKey",
			input:          awsKeyDiff,
			wantFound:      true,
			wantMinFinding: 1,
		},
		{
			name:           "CleanDiff",
			input:          cleanDiff,
			wantFound:      false,
			wantMinFinding: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found, findings, err := ScanDiff(tc.input)
			if err != nil {
				t.Fatalf("ScanDiff returned unexpected error: %v", err)
			}
			if found != tc.wantFound {
				t.Errorf("found = %v, want %v (findings=%d)", found, tc.wantFound, len(findings))
			}
			if len(findings) < tc.wantMinFinding {
				t.Errorf("len(findings) = %d, want >= %d", len(findings), tc.wantMinFinding)
			}
			if !tc.wantFound && len(findings) != 0 {
				t.Errorf("clean input produced %d findings, want 0: %+v", len(findings), findings)
			}
		})
	}
}

// TestLoadConfig_EmptyPath verifies that LoadConfig("") returns a detector
// equivalent in behavior to detect.NewDetectorDefaultConfig().
// See plan 03-04 task behavior case 4.
func TestLoadConfig_EmptyPath(t *testing.T) {
	detector, err := LoadConfig("")
	if err != nil {
		t.Fatalf("LoadConfig(\"\") returned unexpected error: %v", err)
	}
	if detector == nil {
		t.Fatal("LoadConfig(\"\") returned nil detector")
	}

	// Behavioral equivalence check: both detectors should flag the same
	// Anthropic key fixture with the same number of findings (rule sets are
	// identical — the embedded gitleaks DefaultConfig).
	foundDefault, findingsDefault, err := ScanDiff(anthropicKeyDiff)
	if err != nil {
		t.Fatalf("ScanDiff returned unexpected error: %v", err)
	}
	if !foundDefault {
		t.Fatal("Sanity: ScanDiff should detect the Anthropic key fixture")
	}

	foundLoaded, findingsLoaded, err := ScanDiffWithConfig(anthropicKeyDiff, detector)
	if err != nil {
		t.Fatalf("ScanDiffWithConfig returned unexpected error: %v", err)
	}
	if !foundLoaded {
		t.Fatal("LoadConfig(\"\") detector failed to detect the Anthropic key fixture")
	}
	if len(findingsDefault) != len(findingsLoaded) {
		t.Errorf("finding-count mismatch between NewDetectorDefaultConfig and LoadConfig(\"\"): default=%d loaded=%d",
			len(findingsDefault), len(findingsLoaded))
	}
}

// TestLoadConfig_ExtendUseDefault verifies that a TOML config with
//
//	[extend] useDefault = true
//	[[rules]]
//	id = "test-custom"
//	...
//
// composes the embedded default rules with the user's custom rule — both
// the upstream Anthropic-key rule AND the custom rule fire.
// See plan 03-04 task behavior case 5.
func TestLoadConfig_ExtendUseDefault(t *testing.T) {
	customRuleTOML := `title = "tide-test-custom"

[extend]
useDefault = true

[[rules]]
id = "test-custom"
description = "test custom rule"
regex = '''CUSTOM_SECRET_[A-Z0-9]+'''
keywords = ["custom_secret_"]
`

	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "custom.toml")
	if err := os.WriteFile(cfgPath, []byte(customRuleTOML), 0o600); err != nil {
		t.Fatalf("failed to write temp TOML: %v", err)
	}

	detector, err := LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("LoadConfig(%q) returned unexpected error: %v", cfgPath, err)
	}
	if detector == nil {
		t.Fatal("LoadConfig returned nil detector")
	}

	// Custom rule fires on its own.
	foundCustom, findingsCustom, err := ScanDiffWithConfig(customSecretDiff, detector)
	if err != nil {
		t.Fatalf("ScanDiffWithConfig (custom input) error: %v", err)
	}
	if !foundCustom {
		t.Errorf("custom rule did not fire on %q (findings=%d): %+v",
			customSecretDiff, len(findingsCustom), findingsCustom)
	}

	// Default rule (Anthropic key) STILL fires through useDefault=true compose.
	foundDefault, findingsDefault, err := ScanDiffWithConfig(anthropicKeyDiff, detector)
	if err != nil {
		t.Fatalf("ScanDiffWithConfig (anthropic input) error: %v", err)
	}
	if !foundDefault {
		t.Errorf("default Anthropic rule did not fire after useDefault=true compose (findings=%d): %+v",
			len(findingsDefault), findingsDefault)
	}
}

// TestScanDiffWithConfig_NilDetector ensures the helper rejects a nil
// detector argument with a clean error (not a panic).
func TestScanDiffWithConfig_NilDetector(t *testing.T) {
	_, _, err := ScanDiffWithConfig("anything", nil)
	if err == nil {
		t.Fatal("ScanDiffWithConfig(_, nil) returned err=nil, want non-nil")
	}
}

// TestDefaultRulesEmbedded asserts the package-level defaultRulesTOML byte
// slice is non-empty (sanity check that go:embed wired up correctly).
func TestDefaultRulesEmbedded(t *testing.T) {
	if len(defaultRulesTOML) == 0 {
		t.Fatal("defaultRulesTOML is empty — go:embed default_rules.toml did not wire up")
	}
	// Header line check — vendored copy carries the upstream-source comment.
	head := string(defaultRulesTOML[:min(len(defaultRulesTOML), 256)])
	if !contains(head, "v8.30.1") {
		t.Errorf("defaultRulesTOML header does not reference v8.30.1: head=%q", head)
	}
}

// contains is a tiny helper so the test file has no third-party imports
// beyond stdlib + the local package.
func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && indexOf(haystack, needle) >= 0
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
