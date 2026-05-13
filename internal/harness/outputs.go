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
//   - directories are never flagged; only regular files are considered.
func Validate(workspaceRoot string, runStart time.Time, declared []string) ([]string, error) {
	// Pre-resolve declared paths to their absolute, symlink-evaluated forms.
	// Paths that don't exist yet are kept as the abs form (they'll be created
	// by the runtime before Validate is called again, per D-G2 lazy mkdir).
	declaredAbs := make([]string, 0, len(declared))
	for _, d := range declared {
		abs := filepath.Join(workspaceRoot, d)
		if real, err := filepath.EvalSymlinks(abs); err == nil {
			declaredAbs = append(declaredAbs, real)
		} else {
			// Path doesn't exist yet — keep the non-resolved absolute form.
			declaredAbs = append(declaredAbs, abs)
		}
	}

	var violations []string
	err := filepath.WalkDir(workspaceRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Directories are never flagged.
		if d.IsDir() {
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
		real, resolveErr := filepath.EvalSymlinks(p)
		if resolveErr != nil {
			// File may have been removed between walk and resolution; use the
			// un-resolved path (fall back to the walk path).
			real = p
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
