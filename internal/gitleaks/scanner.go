package gitleaks

import (
	_ "embed"
	"errors"
	"fmt"

	"github.com/zricethezav/gitleaks/v8/detect"
	"github.com/zricethezav/gitleaks/v8/report"
)

// Finding is an alias for github.com/zricethezav/gitleaks/v8/report.Finding,
// re-exported under the local package name so consumers of this package can
// avoid importing the gitleaks/v8/report package directly. (Plan 03-04's
// `<interfaces>` block referred to "[]detect.Finding"; the upstream gitleaks
// v8.30.1 detector.DetectString call actually returns []report.Finding —
// this alias preserves the public-facing intent of plan 03-04 while
// honoring the actual upstream API. See SUMMARY.md "Deviations from Plan"
// for the Rule 1 deviation note.)
type Finding = report.Finding

// defaultRulesTOML is the vendored copy of the gitleaks v8.30.1 upstream
// config/gitleaks.toml ruleset, embedded at build time. The byte slice is
// retained as an audit copy — gitleaks v8 internally embeds the same content
// via config.DefaultConfig, so ScanDiff and LoadConfig("") delegate to the
// upstream-embedded copy. Future variants (e.g., a TIDE-specific subset
// with non-upstream rules) would swap the embedded content here without
// changing the package's public API.
//
//go:embed default_rules.toml
var defaultRulesTOML []byte

// DefaultRulesTOML returns a copy of the embedded gitleaks v8 default
// ruleset bytes. Exposed for inspection / audit; not used on the scan
// hot-path (which goes through detect.NewDetectorDefaultConfig() —
// upstream-embedded copy with identical content).
func DefaultRulesTOML() []byte {
	out := make([]byte, len(defaultRulesTOML))
	copy(out, defaultRulesTOML)
	return out
}

// ScanDiff scans diffContent against the embedded gitleaks v8 default
// ruleset and returns whether any secret-shaped patterns were found.
//
// Defense-in-depth role (D-B3, ART-07): TIDE's push Job invokes ScanDiff on
// the staged diff before calling go-git Push; a finding short-circuits the
// push (push Job exits non-zero) so the secret never leaves the cluster.
// internal/harness/redact (Phase 2 D-F4) is the harness-side first line of
// defense on subagent stdout; gitleaks here is the push-side second line.
//
// The detector is constructed via detect.NewDetectorDefaultConfig() which
// embeds the upstream gitleaks v8 ruleset (150+ rules, including
// Anthropic API keys, AWS access keys, GitHub PATs, Stripe keys, JWT, etc.)
// at build time. Per-Project rule-set overrides land via LoadConfig +
// ScanDiffWithConfig (plan 03-06 wires the ConfigMap mount mechanics on
// the push Job spec).
//
// Returns:
//   - found:    true if len(findings) > 0; false otherwise.
//   - findings: slice of report.Finding entries (re-exported as Finding);
//     empty (len == 0) when found is false; never nil-with-len>0.
//   - err:      non-nil only if the detector failed to construct
//     (gitleaks default config parse error — should never happen on a
//     working binary).
func ScanDiff(diffContent string) (found bool, findings []Finding, err error) {
	detector, err := detect.NewDetectorDefaultConfig()
	if err != nil {
		return false, nil, fmt.Errorf("gitleaks: build default detector: %w", err)
	}
	return scanWithDetector(diffContent, detector)
}

// ScanDiffWithConfig scans diffContent against a pre-constructed gitleaks
// detector (typically obtained via LoadConfig). Returns an error if the
// detector argument is nil.
//
// Use this entry point when the caller has already paid the detector-build
// cost (e.g., the push Job reuses a detector across multiple diffs) or
// when a per-Project override config is in play.
func ScanDiffWithConfig(diffContent string, detector *detect.Detector) (found bool, findings []Finding, err error) {
	if detector == nil {
		return false, nil, errors.New("gitleaks: ScanDiffWithConfig: detector is nil")
	}
	return scanWithDetector(diffContent, detector)
}

// scanWithDetector is the shared inner scan helper; both ScanDiff and
// ScanDiffWithConfig delegate here. Kept private so all public entry
// points run the identical DetectString call.
func scanWithDetector(diffContent string, detector *detect.Detector) (bool, []Finding, error) {
	findings := detector.DetectString(diffContent)
	return len(findings) > 0, findings, nil
}
