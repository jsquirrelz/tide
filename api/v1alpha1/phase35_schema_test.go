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

// Phase 35 Plan 01: schema parity + round-trip locks for
//   - GitConfig.BaseRef  (spec.git.baseRef, D-01)  — both API versions
//   - GitStatus.BaseSHA  (status.git.baseSHA, D-11) — both API versions
//
// There is no conversion webhook in this repo (v1alpha1 is
// +kubebuilder:unservedversion; conversion strategy is None). "Survive
// v1alpha1⇄current conversion round-trip" therefore means: identical field
// shape in both Go packages AND both version blocks of the generated CRD YAML,
// plus a JSON round-trip that mirrors what the strategy-None apiVersion rewrite
// does at the API server. "Current" was v1alpha2 at Phase 35 authorship and is
// v1alpha3 as of Phase 40 Plan 40-03 (D-04 version crank). These tests follow
// the phase3_schema_test.go static-analysis convention (regex over source +
// generated CRD YAML) and reuse its findRepoRoot / readProjectCRD helpers
// (same package).
package v1alpha1_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	tideprojectv1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
	tideprojectv1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// readProjectTypesV2 reads api/v1alpha3/project_types.go (the twin of the
// phase3 readProjectTypes helper, which reads the v1alpha1 file). Named V2
// for the helper's historical Phase 35 role (checking v1alpha1 against
// whichever version is current); it now points at v1alpha3, the sole
// served+storage version as of Phase 40 Plan 40-03.
func readProjectTypesV2(t *testing.T) string {
	t.Helper()
	root := findRepoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, "api", "v1alpha3", "project_types.go"))
	if err != nil {
		t.Fatalf("read v1alpha3 project_types.go: %v", err)
	}
	return string(data)
}

// TestBaseRefFieldDeclaredBothVersions asserts GitConfig.BaseRef exists in BOTH
// api/v1alpha1 and api/v1alpha3 source (P9 both-versions rule). Removing either
// side makes this fail loudly — that is the point.
func TestBaseRefFieldDeclaredBothVersions(t *testing.T) {
	re := regexp.MustCompile(`BaseRef\s+string`)
	if src := readProjectTypes(t); !re.MatchString(src) {
		t.Errorf("api/v1alpha1/project_types.go missing `BaseRef string` in GitConfig (P9 both-versions drop)")
	}
	if src := readProjectTypesV2(t); !re.MatchString(src) {
		t.Errorf("api/v1alpha3/project_types.go missing `BaseRef string` in GitConfig (P9 both-versions drop)")
	}
}

// TestBaseSHAFieldDeclaredBothVersions asserts GitStatus.BaseSHA exists in BOTH
// API versions.
func TestBaseSHAFieldDeclaredBothVersions(t *testing.T) {
	re := regexp.MustCompile(`BaseSHA\s+string`)
	if src := readProjectTypes(t); !re.MatchString(src) {
		t.Errorf("api/v1alpha1/project_types.go missing `BaseSHA string` in GitStatus (P9 both-versions drop)")
	}
	if src := readProjectTypesV2(t); !re.MatchString(src) {
		t.Errorf("api/v1alpha3/project_types.go missing `BaseSHA string` in GitStatus (P9 both-versions drop)")
	}
}

// TestBaseRefHasNoDefaultMarker enforces the P10 / STATE.md binding constraint:
// baseRef carries NO +kubebuilder:default marker — absence is the only HEAD
// encoding. The check is scoped to the BaseRef marker region so unrelated
// defaults elsewhere in the file don't mask a regression.
func TestBaseRefHasNoDefaultMarker(t *testing.T) {
	region := regexp.MustCompile(`(?s)// BaseRef optionally names.*?BaseRef string`)
	for name, src := range map[string]string{
		"v1alpha1": readProjectTypes(t),
		"v1alpha3": readProjectTypesV2(t),
	} {
		m := region.FindString(src)
		if m == "" {
			t.Fatalf("%s: could not locate BaseRef marker region", name)
		}
		if strings.Contains(m, "kubebuilder:default") {
			t.Errorf("%s: baseRef must carry NO +kubebuilder:default marker (P10 — absent is the only HEAD encoding)", name)
		}
		if strings.Contains(m, "XValidation") {
			t.Errorf("%s: baseRef must carry NO XValidation/oldSelf rule (D-08)", name)
		}
		if !strings.Contains(m, "+optional") {
			t.Errorf("%s: baseRef must be +optional", name)
		}
	}
}

// TestProjectCRDSchemaHasBaseRefBothVersions asserts the generated CRD YAML
// carries baseRef (under spec.git) and baseSHA (under status.git) in every
// version block — exactly one occurrence each per block, so the count must
// equal the number of version blocks in the generated CRD to prove
// all-version presence (only the Project CRD declares these fields).
// v1alpha1/v1alpha2/v1alpha3 all carry the field during Phase 40's
// transitional 3-version window (D-01); the count tracks that, not a
// hardcoded 2.
func TestProjectCRDSchemaHasBaseRefBothVersions(t *testing.T) {
	crd := readProjectCRD(t)
	wantVersions := strings.Count(crd, "name: v1alpha")
	if got := strings.Count(crd, "baseRef:"); got != wantVersions {
		t.Errorf("config/crd baseRef: occurrences = %d, want %d (one per version block); `make manifests` stale or a version block dropped it", got, wantVersions)
	}
	if got := strings.Count(crd, "baseSHA:"); got != wantVersions {
		t.Errorf("config/crd baseSHA: occurrences = %d, want %d (one per version block)", got, wantVersions)
	}
}

// TestProjectCRDSchemaHasBaseRefPattern asserts the charset Pattern marker for
// baseRef landed in the regenerated CRD YAML (analog to the repoURL pattern
// test at phase3_schema_test.go:230). The pattern rejects leading '-' (argument
// injection, T-35-01), spaces, and git-forbidden metacharacters. It must appear
// once per version block (see TestProjectCRDSchemaHasBaseRefBothVersions for
// why the count tracks the live version-block count rather than a hardcoded 2).
func TestProjectCRDSchemaHasBaseRefPattern(t *testing.T) {
	crd := readProjectCRD(t)
	wantVersions := strings.Count(crd, "name: v1alpha")
	const want = `pattern: ^[A-Za-z0-9][A-Za-z0-9._+@/-]*$`
	if got := strings.Count(crd, want); got != wantVersions {
		t.Errorf("config/crd baseRef pattern %q occurrences = %d, want %d (one per version block); marker missing or stale", want, got, wantVersions)
	}
	if !strings.Contains(crd, "maxLength: 250") {
		t.Errorf("config/crd missing `maxLength: 250` bound on baseRef (T-35-04 regex-cost bound)")
	}
}

// TestBaseRefBaseSHARoundTrip marshals a Project carrying BaseRef + BaseSHA in
// one API version to JSON and unmarshals into the other — BOTH directions. This
// is literally what the strategy-None apiVersion rewrite does at the API server;
// dropping either field on either side fails here.
func TestBaseRefBaseSHARoundTrip(t *testing.T) {
	const (
		wantRef = "release/1.2"
		wantSHA = "0123456789abcdef0123456789abcdef01234567"
	)

	// v1alpha1 -> v1alpha3
	src1 := tideprojectv1alpha1.Project{
		Spec: tideprojectv1alpha1.ProjectSpec{
			Git: &tideprojectv1alpha1.GitConfig{
				RepoURL:        "https://github.com/owner/repo.git",
				CredsSecretRef: "git-creds",
				BaseRef:        wantRef,
			},
		},
		Status: tideprojectv1alpha1.ProjectStatus{
			Git: tideprojectv1alpha1.GitStatus{BaseSHA: wantSHA},
		},
	}
	raw1, err := json.Marshal(src1)
	if err != nil {
		t.Fatalf("marshal v1alpha1 Project: %v", err)
	}
	var dst2 tideprojectv1alpha3.Project
	if err := json.Unmarshal(raw1, &dst2); err != nil {
		t.Fatalf("unmarshal into v1alpha3 Project: %v", err)
	}
	if dst2.Spec.Git == nil {
		t.Fatal("v1alpha1->v1alpha3: spec.git dropped entirely")
	}
	if dst2.Spec.Git.BaseRef != wantRef {
		t.Errorf("v1alpha1->v1alpha3: spec.git.baseRef = %q, want %q", dst2.Spec.Git.BaseRef, wantRef)
	}
	if dst2.Status.Git.BaseSHA != wantSHA {
		t.Errorf("v1alpha1->v1alpha3: status.git.baseSHA = %q, want %q", dst2.Status.Git.BaseSHA, wantSHA)
	}

	// v1alpha3 -> v1alpha1 (reverse)
	src2 := tideprojectv1alpha3.Project{
		Spec: tideprojectv1alpha3.ProjectSpec{
			Git: &tideprojectv1alpha3.GitConfig{
				RepoURL:        "https://github.com/owner/repo.git",
				CredsSecretRef: "git-creds",
				BaseRef:        wantRef,
			},
		},
		Status: tideprojectv1alpha3.ProjectStatus{
			Git: tideprojectv1alpha3.GitStatus{BaseSHA: wantSHA},
		},
	}
	raw2, err := json.Marshal(src2)
	if err != nil {
		t.Fatalf("marshal v1alpha3 Project: %v", err)
	}
	var dst1 tideprojectv1alpha1.Project
	if err := json.Unmarshal(raw2, &dst1); err != nil {
		t.Fatalf("unmarshal into v1alpha1 Project: %v", err)
	}
	if dst1.Spec.Git == nil {
		t.Fatal("v1alpha3->v1alpha1: spec.git dropped entirely")
	}
	if dst1.Spec.Git.BaseRef != wantRef {
		t.Errorf("v1alpha3->v1alpha1: spec.git.baseRef = %q, want %q", dst1.Spec.Git.BaseRef, wantRef)
	}
	if dst1.Status.Git.BaseSHA != wantSHA {
		t.Errorf("v1alpha3->v1alpha1: status.git.baseSHA = %q, want %q", dst1.Status.Git.BaseSHA, wantSHA)
	}
}
