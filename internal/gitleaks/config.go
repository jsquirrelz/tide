package gitleaks

import "errors"

// LoadConfig is a placeholder; the GREEN phase wires in gitleaks/v8/detect
// + gitleaks/v8/config so a TOML file at configPath is parsed (with default
// rules composed via [extend] useDefault = true) and a *detect.Detector is
// returned.
func LoadConfig(configPath string) (any, error) {
	return nil, errors.New("gitleaks: LoadConfig not yet implemented")
}
