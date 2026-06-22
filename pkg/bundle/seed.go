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

// Package bundle provides the TIDE bundle format: on-disk types, tgz codec,
// sha256 integrity, childCount-stamp, and offline dry-run validation. It is
// importable from cmd/tide/ without dragging in internal/controller/ (D-07).
package bundle

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"

	dispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// BundleEntry mirrors internal/controller.seedEntry but adds SHA256 for
// dry-run integrity validation (D-04). The json tags on the first eight fields
// are byte-identical to seedEntry so ImportController's json.Unmarshal accepts
// the same bytes; the extra sha256 field is silently ignored (unknown fields
// dropped by default — verified import_controller.go:311 uses standard
// json.Unmarshal without DisallowUnknownFields).
type BundleEntry struct {
	// Name is the short K8s object name (used as the CR name).
	Name string `json:"name"`

	// FQName is the fully-qualified name (milestone-name/phase-name/plan-name)
	// used as the rekey table key (Phase 28 D-07).
	FQName string `json:"fqName"`

	// OldUID is the UID from the salvaged run; used to locate the old PVC subPath.
	OldUID string `json:"oldUID"`

	// DependsOn is the list of FQ-name dependsOn references (mirrors Spec.DependsOn).
	DependsOn []string `json:"dependsOn,omitempty"`

	// Status is the initial Status.Phase to patch onto the created CR.
	Status string `json:"status,omitempty"`

	// PhaseRef is the owning Phase name (for Plan entries only).
	PhaseRef string `json:"phaseRef,omitempty"`

	// MilestoneRef is the owning Milestone name (for Phase entries only).
	MilestoneRef string `json:"milestoneRef,omitempty"`

	// ProjectRef is the owning Project name (for Milestone entries only).
	ProjectRef string `json:"projectRef,omitempty"`

	// SHA256 is the lowercase-hex sha256 of the envelope's out.json bytes.
	// Not consumed by ImportController (unknown field, silently ignored).
	// Read by offline dry-run validation (D-04/D-07).
	SHA256 string `json:"sha256,omitempty"`
}

// BundleManifest is the top-level structure of seed-manifest.json in the
// bundle. Mirrors internal/controller.seedManifest exactly for the three
// array keys; ImportController parses only those three keys and drops sha256.
type BundleManifest struct {
	Milestones []BundleEntry `json:"milestones"`
	Phases     []BundleEntry `json:"phases"`
	Plans      []BundleEntry `json:"plans"`
}

// MilestoneFQName returns the fully-qualified name for a Milestone:
// "<milestoneName>".
func MilestoneFQName(milestoneName string) string {
	return milestoneName
}

// PhaseFQName returns the fully-qualified name for a Phase:
// "<milestoneName>/<phaseName>".
func PhaseFQName(milestoneName, phaseName string) string {
	return milestoneName + "/" + phaseName
}

// PlanFQName returns the fully-qualified name for a Plan:
// "<milestoneName>/<phaseName>/<planName>".
func PlanFQName(milestoneName, phaseName, planName string) string {
	return milestoneName + "/" + phaseName + "/" + planName
}

// ComputeEnvelopeSHA256 returns the lowercase hex sha256 digest of outJSONBytes.
// Stable across calls; uses crypto/sha256 stdlib (same approach as
// internal/credproxy/token.go). Exported so cmd/tide export layer can compute
// per-envelope sha256 for the seed manifest (D-04).
func ComputeEnvelopeSHA256(outJSONBytes []byte) string {
	sum := sha256.Sum256(outJSONBytes)
	return fmt.Sprintf("%x", sum)
}

// computeEnvelopeSHA256 is the package-internal alias kept for dryrun.go.
func computeEnvelopeSHA256(outJSONBytes []byte) string {
	return ComputeEnvelopeSHA256(outJSONBytes)
}

// StampChildCount is the exported alias for stampChildCount, usable by
// packages outside pkg/bundle (e.g. cmd/tide export layer, D-16a).
func StampChildCount(outJSONBytes []byte, w io.Writer) ([]byte, error) {
	return stampChildCount(outJSONBytes, w)
}

// stampChildCount repairs legacy out.json bytes that predate the childCount
// field (plan 09-08, D-16a). When ChildCount==0 and len(ChildCRDs)>0 it sets
// ChildCount=len(ChildCRDs), re-marshals, and writes a warning to w. When the
// field is already correct (or ChildCRDs is empty) the original bytes are
// returned unchanged.
//
// w is typically cmd.ErrOrStderr(); passing io.Discard suppresses warnings.
func stampChildCount(outJSONBytes []byte, w io.Writer) ([]byte, error) {
	var env dispatch.EnvelopeOut
	if err := json.Unmarshal(outJSONBytes, &env); err != nil {
		return nil, fmt.Errorf("stampChildCount: unmarshal envelope: %w", err)
	}

	// D-16a: only repair when absent/0 AND children exist.
	if env.ChildCount == 0 && len(env.ChildCRDs) > 0 {
		fmt.Fprintf(w, "repaired legacy childCount for envelope %s (stamped %d)\n",
			env.TaskUID, len(env.ChildCRDs))
		env.ChildCount = len(env.ChildCRDs)
		repaired, err := json.Marshal(env)
		if err != nil {
			return nil, fmt.Errorf("stampChildCount: re-marshal envelope: %w", err)
		}
		return repaired, nil
	}

	return outJSONBytes, nil
}
