package gitleaks

import (
	_ "embed"
	"errors"
)

// defaultRulesTOML is the vendored copy of the gitleaks v8.30.1 upstream
// config/gitleaks.toml ruleset embedded at build time. See default_rules.toml
// for the source comment. The byte slice is exported via the package's
// public helpers; it is also the input to LoadConfig when no custom
// override path is provided.
//
//go:embed default_rules.toml
var defaultRulesTOML []byte

// Finding is a placeholder shape for a single gitleaks finding. The real
// implementation in scanner.go (GREEN phase) replaces this with the gitleaks
// upstream report.Finding type.
type Finding struct{}

// ScanDiff is a placeholder; the GREEN phase wires in gitleaks/v8/detect.
func ScanDiff(diffContent string) (bool, []Finding, error) {
	return false, nil, errors.New("gitleaks: ScanDiff not yet implemented")
}

// ScanDiffWithConfig is a placeholder; the GREEN phase wires in gitleaks/v8/detect.
func ScanDiffWithConfig(diffContent string, detector any) (bool, []Finding, error) {
	return false, nil, errors.New("gitleaks: ScanDiffWithConfig not yet implemented")
}
