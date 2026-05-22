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

package harness

import (
	"io/fs"
	"path/filepath"
	"strings"
	"time"
)

// Validate walks workspaceRoot for files modified at or after runStart,
// resolves symlinks via [filepath.EvalSymlinks], and asserts each resolved
// path is under at least one of the declared output paths.
//
// Returns the list of out-of-scope absolute paths (empty slice if no
// violations). A non-nil error indicates a filesystem walk failure.
//
// Security invariants (HARN-05 / Pitfall 7 — subagent context bleed):
//
//   - symlink resolution: [filepath.EvalSymlinks] is applied to each candidate
//     before the scope check, so a symlink inside a declared path that resolves
//     to a target outside the declared path is correctly flagged.
//   - declared paths that do not yet exist are carried as their absolute forms
//     (creation is permitted; existence is not required at validation time).
//   - files modified before runStart are skipped (they are pre-existing
//     artifacts from prior Tasks).
//   - the workspace-root envelopes directory is reserved for TIDE transport
//     files and skipped; it is not user-declared output.
//   - directories are never flagged; only regular files are considered.
func Validate(workspaceRoot string, runStart time.Time, declared []string) ([]string, error) {
	// CR-05: Resolve declared paths and walk targets through the SAME prefix
	// resolution so workspaces under symlinked roots (e.g. macOS /tmp →
	// /private/tmp, K8s tmpfs mounts) don't false-positive. If the declared
	// path itself doesn't exist yet, resolve as much of its existing prefix
	// as possible and keep the un-resolved suffix.
	declaredAbs := make([]string, 0, len(declared))
	for _, d := range declared {
		abs := filepath.Join(workspaceRoot, d)
		declaredAbs = append(declaredAbs, resolveExistingPrefix(abs))
	}

	var violations []string
	err := filepath.WalkDir(workspaceRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			rel, relErr := filepath.Rel(workspaceRoot, p)
			if relErr == nil && rel == "envelopes" {
				return filepath.SkipDir
			}
			return nil
		}
		// Skip files modified before runStart (pre-existing artifacts).
		info, infoErr := d.Info()
		if infoErr != nil {
			return infoErr
		}
		if info.ModTime().Before(runStart) {
			return nil
		}

		// Resolve symlinks on the actual candidate path so a symlink that
		// points outside the declared scope is treated as its real target.
		// On failure (e.g. file removed mid-walk) fall back to the same
		// prefix-resolution we used for declared paths so the comparison
		// stays symmetric (CR-05).
		real, resolveErr := filepath.EvalSymlinks(p)
		if resolveErr != nil {
			real = resolveExistingPrefix(p)
		}

		// Candidate is in-scope if it resolves under at least one declared path.
		ok := false
		for _, decl := range declaredAbs {
			rel, relErr := filepath.Rel(decl, real)
			if relErr == nil && !strings.HasPrefix(rel, "..") && rel != "." {
				ok = true
				break
			}
		}
		if !ok {
			violations = append(violations, real)
		}
		return nil
	})
	return violations, err
}

// resolveExistingPrefix walks up the path until it finds an ancestor that
// exists, calls [filepath.EvalSymlinks] on that ancestor, and re-joins the
// non-existing suffix. This makes prefix-resolution symmetric between
// declared paths (which may not exist yet at Validate-time) and walk
// targets (which always exist), eliminating the false-positive that
// triggers when the workspace root is under a symlink (e.g. macOS /tmp →
// /private/tmp). If no ancestor resolves, the original path is returned.
func resolveExistingPrefix(p string) string {
	cur := p
	suffix := ""
	for {
		if real, err := filepath.EvalSymlinks(cur); err == nil {
			if suffix == "" {
				return real
			}
			return filepath.Join(real, suffix)
		}
		next := filepath.Dir(cur)
		if next == cur {
			// Reached root without finding anything that resolves.
			return p
		}
		suffix = filepath.Join(filepath.Base(cur), suffix)
		cur = next
	}
}
