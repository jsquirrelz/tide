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

// Command tide-import is the in-namespace envelope-rekey Job binary (Phase 28).
//
// Reads a rekey table (fqName→oldUID, fqName→newUID pairs) from stdin as JSON.
// For each pair: validates the envelope is complete (exitCode==0 and
// len(ChildCRDs)==ChildCount); copies envelope files from
// /old-workspace/envelopes/<oldUID>/ to /new-workspace/envelopes/<newUID>/
// using cp -n semantics (no-clobber + atomic rename for partial-write safety,
// D-12). Rewrites out.json.taskUID atomically to newUID. Runs schema conversion
// (json.Unmarshal→json.Marshal through typed v1alpha2 structs) on child
// Spec.Raw bytes, validating each child Kind against the local allowlist
// {Milestone,Phase,Plan,Task} before conversion (D-05/D-08). Wave CRs are
// never imported (D-09). Path-traversal defended (D-08 layer 2, T-28-03-01).
//
// Exit-code map:
//
//	0 — success: all complete envelopes copied and converted
//	1 — generic failure (I/O error, unmarshal error)
//	2 — invariant violation (bad stdin JSON, path traversal, Kind not in allowlist)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

const (
	exitSuccess  = 0
	exitGeneric  = 1
	exitInvariant = 2
)

// childKindAllowlist is the local re-declaration of the T-308 Kind allowlist.
// Wave is intentionally excluded (D-09: Wave CRs are always re-derived, never
// imported). Mirrors internal/reporter.ChildKindAllowlist values without
// importing internal/reporter (provider firewall constraint).
var childKindAllowlist = map[string]bool{
	"Milestone": true,
	"Phase":     true,
	"Plan":      true,
	"Task":      true,
}

// importConfig is the parsed CLI configuration passed by value into run().
type importConfig struct {
	OldWorkspace string // default "/old-workspace"
	NewWorkspace string // default "/new-workspace"
}

// rekeyEntry is one row of the stdin rekey table.
// FQName is the fully-qualified object name (object name + full parent chain,
// e.g. "milestone-02/phase-03/plan-01-foo") that guarantees 1:1 mapping even
// where sibling subtrees reuse short names (D-07, T-28-03-04).
type rekeyEntry struct {
	FQName string `json:"fqName"`
	OldUID string `json:"oldUID"`
	NewUID string `json:"newUID"`
}

// importReport is the structured JSON emitted to stdout on success.
type importReport struct {
	Copied     int `json:"copied"`
	Skipped    int `json:"skipped"`
	Converted  int `json:"converted"`
	Incomplete int `json:"incomplete"`
}

func main() {
	fs := flag.NewFlagSet("tide-import", flag.ExitOnError)
	oldWorkspace := fs.String("old-workspace", "/old-workspace",
		"mount point for salvaged PVC subPath (read-only)")
	newWorkspace := fs.String("new-workspace", "/new-workspace",
		"mount point for new project PVC subPath (read-write)")

	if err := fs.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tide-import: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	cfg := importConfig{
		OldWorkspace: *oldWorkspace,
		NewWorkspace: *newWorkspace,
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	os.Exit(run(ctx, cfg, os.Stdin, os.Stdout, os.Stderr))
}

// run is the testable entry point. stdin carries the rekey table JSON;
// stdout receives the completion report JSON; stderr carries log lines.
func run(ctx context.Context, cfg importConfig, stdin io.Reader, stdout, stderr io.Writer) int {
	// Decode the rekey table from stdin.
	var table []rekeyEntry
	if err := json.NewDecoder(stdin).Decode(&table); err != nil {
		fmt.Fprintf(stderr, "tide-import: decode rekey table: %v\n", err)
		return exitInvariant
	}

	report := importReport{}

	for _, entry := range table {
		if ctx.Err() != nil {
			fmt.Fprintf(stderr, "tide-import: context cancelled\n")
			return exitGeneric
		}

		// Path-traversal defense (T-28-03-01): validate both old and new UID
		// paths resolve inside their respective mount roots.
		srcBase := filepath.Join(cfg.OldWorkspace, "envelopes")
		dstBase := filepath.Join(cfg.NewWorkspace, "envelopes")

		srcDir, err := containedJoin(srcBase, entry.OldUID)
		if err != nil {
			fmt.Fprintf(stderr, "tide-import: path traversal in oldUID %q: %v\n", entry.OldUID, err)
			return exitInvariant
		}
		dstDir, err := containedJoin(dstBase, entry.NewUID)
		if err != nil {
			fmt.Fprintf(stderr, "tide-import: path traversal in newUID %q: %v\n", entry.NewUID, err)
			return exitInvariant
		}

		// Read and validate the source out.json before doing any copying.
		outSrc := filepath.Join(srcDir, "out.json")
		outData, err := os.ReadFile(outSrc)
		if err != nil {
			// Missing out.json means the envelope was never written (e.g. budget
			// halt at plan level): treat as incomplete, skip.
			fmt.Fprintf(stderr, "tide-import: read out.json %q: %v (skipping — treated as incomplete)\n", outSrc, err)
			report.Incomplete++
			continue
		}

		var env pkgdispatch.EnvelopeOut
		if err := json.Unmarshal(outData, &env); err != nil {
			fmt.Fprintf(stderr, "tide-import: unmarshal out.json %q: %v\n", outSrc, err)
			return exitGeneric
		}

		// Completeness guard (IMPORT-02 / T-28-03-03): reject incomplete envelopes.
		// exitCode != 0 OR len(ChildCRDs) != ChildCount → incomplete.
		if !isEnvelopeComplete(env) {
			fmt.Fprintf(stderr, "tide-import: envelope %q is incomplete (exitCode=%d, childCount=%d, len(childCRDs)=%d) — skipping\n",
				entry.FQName, env.ExitCode, env.ChildCount, len(env.ChildCRDs))
			report.Incomplete++
			continue
		}

		// Kind allowlist + schema conversion (T-28-03-02, D-05, D-06).
		// Convert every child Spec.Raw through the typed v1alpha2 structs.
		for i, child := range env.ChildCRDs {
			if !childKindAllowlist[child.Kind] {
				fmt.Fprintf(stderr, "tide-import: child Kind %q not in allowlist (fqName=%q)\n", child.Kind, entry.FQName)
				return exitInvariant
			}
			converted, err := convertSpecRaw(child.Kind, child.Spec.Raw)
			if err != nil {
				fmt.Fprintf(stderr, "tide-import: convert child %q Kind=%q: %v\n", child.Name, child.Kind, err)
				return exitInvariant
			}
			env.ChildCRDs[i].Spec.Raw = converted
			report.Converted++
		}

		// Rewrite TaskUID to newUID (D-12: atomic rewrite).
		env.TaskUID = entry.NewUID

		// Re-marshal the (converted + TaskUID-updated) envelope for writing.
		convertedOutData, err := json.Marshal(env)
		if err != nil {
			fmt.Fprintf(stderr, "tide-import: marshal converted envelope: %v\n", err)
			return exitGeneric
		}

		// Copy all non-out.json files from srcDir to dstDir using cp -n semantics.
		// out.json is handled separately below with its own no-clobber+atomic write.
		copied, skipped, copyErr := copyDirNoClobberExcluding(srcDir, dstDir, "out.json")
		if copyErr != nil {
			fmt.Fprintf(stderr, "tide-import: copy %q → %q: %v\n", srcDir, dstDir, copyErr)
			return exitGeneric
		}
		report.Copied += copied
		report.Skipped += skipped

		// Write the converted out.json atomically with no-clobber semantics.
		// If the file already exists (idempotent re-run, D-12), skip it.
		outDst := filepath.Join(dstDir, "out.json")
		// Create the directory first if copyDirNoClobberExcluding hasn't yet
		// (e.g. srcDir had no other files).
		if err := os.MkdirAll(dstDir, 0o755); err != nil {
			fmt.Fprintf(stderr, "tide-import: MkdirAll %q: %v\n", dstDir, err)
			return exitGeneric
		}
		if _, err := os.Stat(outDst); err == nil {
			// out.json already exists; skip (cp -n, idempotent re-run).
			report.Skipped++
		} else {
			if err := atomicWriteJSON(outDst, convertedOutData); err != nil {
				fmt.Fprintf(stderr, "tide-import: atomic write out.json %q: %v\n", outDst, err)
				return exitGeneric
			}
			report.Copied++
		}
	}

	// Emit the completion report as JSON on stdout.
	reportData, err := json.Marshal(report)
	if err != nil {
		fmt.Fprintf(stderr, "tide-import: marshal report: %v\n", err)
		return exitGeneric
	}
	fmt.Fprintf(stdout, "%s\n", reportData)
	return exitSuccess
}

// containedJoin joins base with elem using filepath.Clean and verifies the
// result is strictly inside base. Returns an error if elem is absolute or
// resolves outside base (path-traversal defense, mirrors backend.go:116-127).
func containedJoin(base, elem string) (string, error) {
	if filepath.IsAbs(elem) {
		return "", fmt.Errorf("elem %q must be relative, not absolute", elem)
	}
	clean := filepath.Clean(elem)
	if clean == ".." || strings.HasPrefix(clean, ".."+string(os.PathSeparator)) {
		return "", fmt.Errorf("elem %q escapes the base directory (traversal)", elem)
	}
	full := filepath.Join(base, clean)
	// Second-line defense: the resolved path must remain under base.
	if full != base && !strings.HasPrefix(full, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("elem %q resolves outside base %q (rejected)", elem, base)
	}
	return full, nil
}

// isEnvelopeComplete returns true iff the envelope has exitCode==0 and
// len(ChildCRDs) == ChildCount (completeness guard per IMPORT-02 / D-12).
func isEnvelopeComplete(env pkgdispatch.EnvelopeOut) bool {
	if env.ExitCode != 0 {
		return false
	}
	// ChildCount == 0 is valid for leaf planners (Task-level or leaf Plan-level).
	// Mismatch is only flagged when ChildCount > 0 and the slice is shorter.
	if env.ChildCount > 0 && len(env.ChildCRDs) != env.ChildCount {
		return false
	}
	return true
}

// convertSpecRaw round-trips rawBytes through the appropriate v1alpha2 typed
// spec struct (json.Unmarshal → json.Marshal). This strips unknown fields
// (objective/wave/filesTouched per RESEARCH) and preserves required fields.
// A non-allowlisted Kind (already checked by caller) falls to the default case
// and returns an error (fail-closed, D-05). Wave is excluded (D-09).
func convertSpecRaw(kind string, rawBytes []byte) ([]byte, error) {
	switch kind {
	case "Milestone":
		var spec tidev1alpha2.MilestoneSpec
		if err := json.Unmarshal(rawBytes, &spec); err != nil {
			return nil, fmt.Errorf("unmarshal MilestoneSpec: %w", err)
		}
		return json.Marshal(spec)
	case "Phase":
		var spec tidev1alpha2.PhaseSpec
		if err := json.Unmarshal(rawBytes, &spec); err != nil {
			return nil, fmt.Errorf("unmarshal PhaseSpec: %w", err)
		}
		return json.Marshal(spec)
	case "Plan":
		var spec tidev1alpha2.PlanSpec
		if err := json.Unmarshal(rawBytes, &spec); err != nil {
			return nil, fmt.Errorf("unmarshal PlanSpec: %w", err)
		}
		return json.Marshal(spec)
	case "Task":
		var spec tidev1alpha2.TaskSpec
		if err := json.Unmarshal(rawBytes, &spec); err != nil {
			return nil, fmt.Errorf("unmarshal TaskSpec: %w", err)
		}
		return json.Marshal(spec)
	default:
		// Wave and any other unlisted Kind: fail closed (D-05 / D-09).
		return nil, fmt.Errorf("unsupported Kind %q in import conversion (not in allowlist)", kind)
	}
}

// copyDirNoClobberExcluding recursively copies all files from srcDir to dstDir
// using cp -n semantics, skipping any file whose name matches excludeName.
// Creates destination subdirectories as needed. Returns (copied, skipped, error).
func copyDirNoClobberExcluding(srcDir, dstDir, excludeName string) (int, int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return 0, 0, fmt.Errorf("ReadDir %q: %w", srcDir, err)
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, 0, fmt.Errorf("MkdirAll %q: %w", dstDir, err)
	}

	copied, skipped := 0, 0
	for _, e := range entries {
		if !e.IsDir() && e.Name() == excludeName {
			continue // handled separately by atomicWriteJSON
		}
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())

		if e.IsDir() {
			c, s, err := copyDirNoClobberExcluding(src, dst, excludeName)
			if err != nil {
				return copied, skipped, err
			}
			copied += c
			skipped += s
			continue
		}

		n, err := copyFileNoClobber(dst, src)
		if err != nil {
			return copied, skipped, err
		}
		copied += n
		if n == 0 {
			skipped++
		}
	}
	return copied, skipped, nil
}

// copyDirNoClobber recursively copies all files from srcDir to dstDir using
// cp -n semantics: if the destination file already exists, skip it. Creates
// destination subdirectories as needed. Returns (copied, skipped, error).
func copyDirNoClobber(srcDir, dstDir string) (int, int, error) {
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		// Source directory may not exist (envelope was never written).
		return 0, 0, fmt.Errorf("ReadDir %q: %w", srcDir, err)
	}

	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return 0, 0, fmt.Errorf("MkdirAll %q: %w", dstDir, err)
	}

	copied, skipped := 0, 0
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())

		if e.IsDir() {
			c, s, err := copyDirNoClobber(src, dst)
			if err != nil {
				return copied, skipped, err
			}
			copied += c
			skipped += s
			continue
		}

		n, err := copyFileNoClobber(dst, src)
		if err != nil {
			return copied, skipped, err
		}
		copied += n
		if n == 0 {
			skipped++
		}
	}
	return copied, skipped, nil
}

// copyFileNoClobber copies src to dst using cp -n semantics. If dst already
// exists, the function is a no-op and returns (0, nil). On success it returns
// (1, nil). The write is atomic: data is written to dst.tmp then os.Rename'd
// into place to prevent partial-write corruption (D-12 / Anti-Pattern 3).
func copyFileNoClobber(dst, src string) (int, error) {
	if _, err := os.Stat(dst); err == nil {
		return 0, nil // destination exists; skip (cp -n behavior)
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return 0, fmt.Errorf("read %q: %w", src, err)
	}
	tmp := dst + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return 0, fmt.Errorf("write tmp %q: %w", tmp, err)
	}
	if err := os.Rename(tmp, dst); err != nil {
		return 0, fmt.Errorf("rename %q → %q: %w", tmp, dst, err)
	}
	return 1, nil
}

// atomicWriteJSON writes data to path atomically (via .tmp + os.Rename).
// If the destination already exists (idempotent re-run, D-12), it is
// overwritten with the converted bytes (unlike copyFileNoClobber which skips —
// out.json always needs the updated TaskUID + converted Spec.Raw bytes).
func atomicWriteJSON(path string, data []byte) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp %q: %w", tmp, err)
	}
	return os.Rename(tmp, path)
}

// rewriteTaskUID reads the out.json at outPath, updates TaskUID to newUID if
// it differs, and atomically writes the result back. No-op if already correct.
// Kept as a standalone function for unit-test clarity; the main run() path now
// uses atomicWriteJSON on the fully-converted envelope instead.
func rewriteTaskUID(outPath, newUID string) error {
	data, err := os.ReadFile(outPath)
	if err != nil {
		return err
	}
	var out pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if out.TaskUID == newUID {
		return nil // already correct; no-op
	}
	out.TaskUID = newUID
	newData, err := json.Marshal(out)
	if err != nil {
		return err
	}
	tmp := outPath + ".tmp"
	if err := os.WriteFile(tmp, newData, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, outPath) // atomic on Linux ext4/tmpfs
}
