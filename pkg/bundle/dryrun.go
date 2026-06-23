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

package bundle

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	dag "github.com/jsquirrelz/tide/pkg/dag"
	dispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ValidationRow is a single row in the dry-run validation result: one entry
// per seed manifest entry. The CLI layer (29-03) renders this as text or JSON.
type ValidationRow struct {
	// Level is "milestone", "phase", or "plan".
	Level string
	// Name is the CR name.
	Name string
	// FQName is the fully-qualified name.
	FQName string
	// Verdict is "adopt" or "re-plan".
	Verdict string
	// Reason is the failure class for "re-plan" rows; empty for "adopt".
	// Failure classes: "schema mismatch", "completeness failure", "checksum mismatch",
	// "no complete envelope".
	Reason string
}

// ValidationResult is the structured output of ValidateBundle (D-07/D-08).
// The CLI layer renders this as text table or JSON.
type ValidationResult struct {
	// Rows holds per-entry validation verdicts.
	Rows []ValidationRow
	// CycleRejected is true when ComputeWaves returned a CycleError (D-09).
	// When true, the entire import would fail — no per-level partial adoption.
	CycleRejected bool
	// CycleError carries the *dag.CycleError when CycleRejected is true.
	CycleError error
}

// AdoptCount returns the number of rows with verdict "adopt".
func (r *ValidationResult) AdoptCount() int {
	n := 0
	for _, row := range r.Rows {
		if row.Verdict == "adopt" {
			n++
		}
	}
	return n
}

// RePlanCount returns the number of rows with verdict "re-plan".
func (r *ValidationResult) RePlanCount() int {
	n := 0
	for _, row := range r.Rows {
		if row.Verdict == "re-plan" {
			n++
		}
	}
	return n
}

// ValidateBundle performs offline dry-run validation of the bundle at bundleDir
// (must be an unpacked directory; call OpenBundleDir first for tgz input).
//
// For each seed entry it:
//  1. Locates the envelope's out.json under pvc-envelopes.tgz by OldUID.
//  2. Applies stampChildCount (D-16a — repairs legacy 0-shape transparently).
//  3. Calls ValidateAPIVersionKind (schema check).
//  4. Checks len(ChildCRDs)==ChildCount (completeness — after stamp).
//  5. Compares sha256 against BundleEntry.SHA256 (D-04; skipped when SHA256=="").
//
// After per-entry checks it calls dag.ComputeWaves on all seed nodes+edges.
// A cyclic graph (D-09) sets CycleRejected=true and CycleError — the entire
// import would fail; no per-level partial adoption is returned.
//
// The sha256 gate is skipped (not failed) when BundleEntry.SHA256 is empty, to
// remain tolerant of hand-written test fixtures that omit the field.
func ValidateBundle(bundleDir string) (*ValidationResult, error) {
	// 1. Read seed-manifest.json.
	manifestData, err := os.ReadFile(filepath.Join(bundleDir, BundleFileSeedManifest))
	if err != nil {
		return nil, fmt.Errorf("read seed-manifest.json: %w", err)
	}
	var manifest BundleManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return nil, fmt.Errorf("parse seed-manifest.json: %w", err)
	}

	// 2. Load all envelopes from pvc-envelopes.tgz keyed by OldUID.
	pvcTgzPath := filepath.Join(bundleDir, BundleFilePVCEnvelopes)
	envelopes, err := loadPVCEnvelopes(pvcTgzPath)
	if err != nil {
		return nil, fmt.Errorf("load pvc-envelopes.tgz: %w", err)
	}

	// 3. Collect all entries for validation + DAG.
	type levelEntry struct {
		level string
		entry BundleEntry
	}
	var allEntries []levelEntry
	for _, e := range manifest.Milestones {
		allEntries = append(allEntries, levelEntry{"milestone", e})
	}
	for _, e := range manifest.Phases {
		allEntries = append(allEntries, levelEntry{"phase", e})
	}
	for _, e := range manifest.Plans {
		allEntries = append(allEntries, levelEntry{"plan", e})
	}

	// 4. Per-entry validation.
	result := &ValidationResult{}
	for _, le := range allEntries {
		row := ValidationRow{
			Level:  le.level,
			Name:   le.entry.Name,
			FQName: le.entry.FQName,
		}

		envBytes, ok := envelopes[le.entry.OldUID]
		if !ok {
			row.Verdict = "re-plan"
			row.Reason = "no complete envelope"
			result.Rows = append(result.Rows, row)
			continue
		}

		// D-16a: stamp legacy childCount before validation.
		stamped, err := stampChildCount(envBytes, io.Discard)
		if err != nil {
			row.Verdict = "re-plan"
			row.Reason = fmt.Sprintf("envelope parse error: %v", err)
			result.Rows = append(result.Rows, row)
			continue
		}

		// sha256 check (D-04): skip if BundleEntry.SHA256 is empty (hand-written fixtures).
		if le.entry.SHA256 != "" {
			actual := computeEnvelopeSHA256(envBytes)
			if actual != le.entry.SHA256 {
				row.Verdict = "re-plan"
				row.Reason = fmt.Sprintf("checksum mismatch: want %s, got %s", le.entry.SHA256, actual)
				result.Rows = append(result.Rows, row)
				continue
			}
		}

		// Schema check.
		var env dispatch.EnvelopeOut
		if err := json.Unmarshal(stamped, &env); err != nil {
			row.Verdict = "re-plan"
			row.Reason = fmt.Sprintf("envelope parse error: %v", err)
			result.Rows = append(result.Rows, row)
			continue
		}
		if err := dispatch.ValidateAPIVersionKind(env.APIVersion, env.Kind, dispatch.KindTaskEnvelopeOut); err != nil {
			row.Verdict = "re-plan"
			row.Reason = fmt.Sprintf("schema mismatch: %v", err)
			result.Rows = append(result.Rows, row)
			continue
		}

		// Completeness check (after stamp, so legacy 0-shape is repaired).
		if env.ChildCount != len(env.ChildCRDs) {
			row.Verdict = "re-plan"
			row.Reason = fmt.Sprintf("completeness failure: childCount=%d len(childCRDs)=%d",
				env.ChildCount, len(env.ChildCRDs))
			result.Rows = append(result.Rows, row)
			continue
		}

		row.Verdict = "adopt"
		result.Rows = append(result.Rows, row)
	}

	// 5. DAG cycle check (D-09).
	nodes := make([]dag.NodeID, 0, len(allEntries))
	var edges []dag.Edge
	for _, le := range allEntries {
		nodes = append(nodes, le.entry.FQName)
		for _, dep := range le.entry.DependsOn {
			edges = append(edges, dag.Edge{From: dep, To: le.entry.FQName})
		}
	}

	if len(nodes) > 0 {
		_, wavesErr := dag.ComputeWaves(nodes, edges)
		if wavesErr != nil {
			var cycleErr *dag.CycleError
			if errors.As(wavesErr, &cycleErr) {
				result.CycleRejected = true
				result.CycleError = wavesErr
			} else {
				return nil, fmt.Errorf("dag.ComputeWaves: %w", wavesErr)
			}
		}
	}

	return result, nil
}

// loadPVCEnvelopes reads pvc-envelopes.tgz and returns a map of
// OldUID → out.json bytes. Entries at path "envelopes/<uid>/out.json"
// are keyed by <uid>.
func loadPVCEnvelopes(tgzPath string) (map[string][]byte, error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		// If the file doesn't exist, return an empty map (all entries → re-plan).
		if os.IsNotExist(err) {
			return map[string][]byte{}, nil
		}
		return nil, fmt.Errorf("open pvc-envelopes.tgz: %w", err)
	}
	defer f.Close()

	result := make(map[string][]byte)
	if err := readPVCEnvelopesTgz(f, func(uid string, data []byte) {
		result[uid] = data
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// readPVCEnvelopesTgz reads a pvc-envelopes.tgz stream and calls fn for each
// "envelopes/<uid>/out.json" entry with the uid and content bytes.
func readPVCEnvelopesTgz(r io.Reader, fn func(uid string, data []byte)) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read pvc-envelopes tar: %w", err)
		}

		// Match "envelopes/<uid>/out.json".
		if hdr.Typeflag != tar.TypeReg && hdr.Typeflag != tar.TypeRegA {
			continue
		}
		uid, ok := parseEnvelopePath(hdr.Name)
		if !ok {
			continue
		}

		data, err := io.ReadAll(tr)
		if err != nil {
			return fmt.Errorf("read pvc-envelopes entry %s: %w", hdr.Name, err)
		}
		fn(uid, data)
	}
	return nil
}

// parseEnvelopePath extracts the uid from "envelopes/<uid>/out.json".
// Returns (uid, true) on match, ("", false) otherwise.
func parseEnvelopePath(name string) (string, bool) {
	// Expected form: "envelopes/<uid>/out.json"
	const prefix = "envelopes/"
	const suffix = "/out.json"
	if len(name) <= len(prefix)+len(suffix) {
		return "", false
	}
	if name[:len(prefix)] != prefix {
		return "", false
	}
	rest := name[len(prefix):]
	if rest[len(rest)-len(suffix):] != suffix {
		return "", false
	}
	uid := rest[:len(rest)-len(suffix)]
	// uid must not contain path separators.
	if uid == "" || filepath.Clean(uid) != uid {
		return "", false
	}
	return uid, true
}

// WritePVCEnvelopesTgz creates an in-memory pvc-envelopes.tgz whose entries
// are the keys of files (arbitrary names allowed, not restricted to the
// canonical seven-entry bundle set). Used by pkg/bundle tests and export.
func WritePVCEnvelopesTgz(files map[string][]byte) ([]byte, error) {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)

	for name, data := range files {
		// GAP-13: directory entries (name ending in "/") must be written with
		// TypeDir — Go's archive/tar rejects a trailing-slash Name under TypeReg
		// ("filename may not have trailing slash"). processEnvelopesTgz preserves
		// the inspector tar's explicit dir entries, so honor them here rather than
		// crash assembling the round-trip bundle.
		if strings.HasSuffix(name, "/") {
			hdr := &tar.Header{
				Name:     name,
				Mode:     0o755,
				Typeflag: tar.TypeDir,
			}
			if err := tw.WriteHeader(hdr); err != nil {
				return nil, fmt.Errorf("write tar dir header %s: %w", name, err)
			}
			continue
		}
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("write tar header %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return nil, fmt.Errorf("write tar entry %s: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("close tar writer: %w", err)
	}
	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("close gzip writer: %w", err)
	}
	return buf.Bytes(), nil
}
