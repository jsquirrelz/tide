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

package kind_integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// subagentImagePinRE matches versioned subagent image references in the
// shipped sample Projects and their docs, capturing the tag. Both subagent
// images embed pkg/dispatch, whose envelope apiVersion validation is strict
// equality (D-08) — a sample pinning any tag other than the chart appVersion
// dispatches a subagent that rejects every envelope the same-release manager
// writes (exit 2 at loadEnvelope), and the Project never leaves the
// pre-Milestone state. The v1.0.7-rc.1 release gate failed exactly this way:
// examples/projects/small/project.yaml still pinned stub-subagent:1.0.0
// (unbumped since Phase 5) against a 1.0.7 manager.
var subagentImagePinRE = regexp.MustCompile(
	`ghcr\.io/jsquirrelz/tide-(?:stub|claude)-subagent:([A-Za-z0-9][A-Za-z0-9._-]*)`)

// TestExamplesSubagentImagePinsMatchChartAppVersion locks every versioned
// subagent image reference under examples/projects/ (YAML pins and README
// pull/load instructions alike) to charts/tide/Chart.yaml's appVersion. This
// makes the release recipe's mandatory appVersion bump mechanically catch the
// samples too — the same rot class the v1.0.1 release fixed for the chart.
//
// examples/projects/dogfood/ is excluded: those files are historical run
// artifacts that document the image tags actually deployed at the time.
func TestExamplesSubagentImagePinsMatchChartAppVersion(t *testing.T) {
	appVersion := chartAppVersion(t)

	examplesDir := filepath.Join("..", "..", "..", "examples", "projects")
	var checked int
	err := filepath.WalkDir(examplesDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "dogfood" {
				return filepath.SkipDir
			}
			return nil
		}
		ext := filepath.Ext(path)
		if ext != ".yaml" && ext != ".yml" && ext != ".md" {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		for _, m := range subagentImagePinRE.FindAllStringSubmatch(string(data), -1) {
			checked++
			if m[1] != appVersion {
				t.Errorf("%s: subagent image pin %q has tag %q; must equal chart appVersion %q "+
					"(manager and subagent share the pkg/dispatch envelope contract — "+
					"a skewed pin fails every dispatch at apiVersion validation)",
					path, m[0], m[1], appVersion)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", examplesDir, err)
	}

	// The small sample's stub pin plus its README instructions must exist —
	// zero matches means the scan itself broke (moved files, renamed images),
	// not that the samples became pin-free.
	if checked == 0 {
		t.Fatalf("found no versioned subagent image references under %s; "+
			"expected at least the small sample's stub-subagent pin", examplesDir)
	}
}

// chartAppVersion reads appVersion from charts/tide/Chart.yaml — the single
// source of truth the release recipe bumps and load-images-if-needed.sh
// derives image tags from.
func chartAppVersion(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "charts", "tide", "Chart.yaml"))
	if err != nil {
		t.Fatalf("read charts/tide/Chart.yaml: %v", err)
	}
	var chart struct {
		AppVersion string `yaml:"appVersion"`
	}
	if err := yaml.Unmarshal(data, &chart); err != nil {
		t.Fatalf("parse charts/tide/Chart.yaml: %v", err)
	}
	if strings.TrimSpace(chart.AppVersion) == "" {
		t.Fatal("charts/tide/Chart.yaml has empty appVersion")
	}
	return chart.AppVersion
}
