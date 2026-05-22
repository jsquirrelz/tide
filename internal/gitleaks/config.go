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

package gitleaks

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
	"github.com/zricethezav/gitleaks/v8/config"
	"github.com/zricethezav/gitleaks/v8/detect"
)

// LoadConfig returns a *detect.Detector loaded from the gitleaks TOML
// configuration at configPath. If configPath is empty, the detector is
// built from gitleaks' embedded default ruleset (behaviorally identical
// to detect.NewDetectorDefaultConfig).
//
// Compose semantics (D-B3): a per-Project override TOML SHOULD declare
//
//	[extend]
//	useDefault = true
//
// so the embedded default rules are merged with the user's custom rules.
// Without that directive, the detector loads ONLY the rules in the override
// TOML — embedded defaults are silently dropped (gitleaks v8 design;
// confirmed against gitleaks v8.30.1 config/config.go:extendDefault). The
// ConfigMap-mount mechanics that surface configPath to this loader live in
// the push Job spec (plan 03-06); this package is K8s-API-free.
//
// Errors wrap the underlying gitleaks parse error with the configPath
// context (e.g. "gitleaks: read config /etc/tide/gitleaks-config.toml: ...").
func LoadConfig(configPath string) (*detect.Detector, error) {
	if configPath == "" {
		detector, err := detect.NewDetectorDefaultConfig()
		if err != nil {
			return nil, fmt.Errorf("gitleaks: build default detector: %w", err)
		}
		return detector, nil
	}

	// Confirm the file is readable before handing the path to viper —
	// viper's error messages on missing-file are noisy and lose the
	// caller's configPath context.
	if _, err := os.Stat(configPath); err != nil {
		return nil, fmt.Errorf("gitleaks: stat config %q: %w", configPath, err)
	}

	// Use a private viper.Viper instance so we do not pollute any global
	// viper state (gitleaks/v8 itself uses the global default viper inside
	// NewDetectorDefaultConfig; keeping our overrides isolated avoids
	// cross-test contamination and is hygienic for production callers).
	v := viper.New()
	v.SetConfigType("toml")
	v.SetConfigFile(configPath)
	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("gitleaks: read config %q: %w", configPath, err)
	}

	var vc config.ViperConfig
	if err := v.Unmarshal(&vc); err != nil {
		return nil, fmt.Errorf("gitleaks: unmarshal config %q: %w", configPath, err)
	}

	cfg, err := vc.Translate()
	if err != nil {
		return nil, fmt.Errorf("gitleaks: translate config %q: %w", configPath, err)
	}

	return detect.NewDetector(cfg), nil
}
