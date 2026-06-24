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
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Bundle file name constants define the on-disk layout of a TIDE bundle (D-02).
// The seven-entry tgz root must match the salvage-20260618 fixture shape.
const (
	BundleFileProject      = "project.yaml"
	BundleFileMilestones   = "milestones.yaml"
	BundleFilePhases       = "phases.yaml"
	BundleFilePlans        = "plans.yaml"
	BundleFileSeedManifest = "seed-manifest.json"
	BundleFileSeedOutline  = "SEED-OUTLINE.md"
	// BundleFilePVCEnvelopes is the nested tgz whose root is workspace content
	// (envelopes/, artifacts/) — NO workspace/ prefix (Pitfall 3). The loader
	// pod unpacks it with `tar xzf - -C /workspace`.
	BundleFilePVCEnvelopes = "pvc-envelopes.tgz"
)

// bundleEntryOrder is the deterministic write order for tgz entries.
var bundleEntryOrder = []string{
	BundleFileProject,
	BundleFileMilestones,
	BundleFilePhases,
	BundleFilePlans,
	BundleFileSeedManifest,
	BundleFileSeedOutline,
	BundleFilePVCEnvelopes,
}

// WriteBundle writes a bundle tgz to tgzPath. files maps the canonical bundle
// entry names (BundleFile* constants) to their byte contents. The tgz entries
// are written in the canonical order defined by bundleEntryOrder.
func WriteBundle(tgzPath string, files map[string][]byte) error {
	f, err := os.Create(tgzPath)
	if err != nil {
		return fmt.Errorf("create bundle tgz %s: %w", tgzPath, err)
	}
	defer func() { _ = f.Close() }()

	gz := gzip.NewWriter(f)
	defer func() { _ = gz.Close() }()
	tw := tar.NewWriter(gz)
	defer func() { _ = tw.Close() }()

	for _, name := range bundleEntryOrder {
		data, ok := files[name]
		if !ok {
			continue
		}
		hdr := &tar.Header{
			Name:     name,
			Mode:     0o644,
			Size:     int64(len(data)),
			Typeflag: tar.TypeReg,
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return fmt.Errorf("write tar header %s: %w", name, err)
		}
		if _, err := tw.Write(data); err != nil {
			return fmt.Errorf("write tar entry %s: %w", name, err)
		}
	}

	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar writer: %w", err)
	}
	return gz.Close()
}

// ReadBundle reads a bundle tgz from tgzPath and returns a map of file name
// to contents.
func ReadBundle(tgzPath string) (map[string][]byte, error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		return nil, fmt.Errorf("open bundle tgz %s: %w", tgzPath, err)
	}
	defer func() { _ = f.Close() }()

	result := make(map[string][]byte)
	if err := readTgzEntries(f, func(name string, r io.Reader) error {
		data, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("read tgz entry %s: %w", name, err)
		}
		result[name] = data
		return nil
	}); err != nil {
		return nil, err
	}
	return result, nil
}

// ExtractBundle extracts the tgz at tgzPath into a temporary directory and
// returns the directory path plus a cleanup function. The caller must call
// cleanup() when done to remove the temp dir.
//
// Zip-slip defense is applied: entries with "../" or absolute paths are
// rejected and no file is written.
func ExtractBundle(tgzPath string) (dir string, cleanup func(), err error) {
	f, err := os.Open(tgzPath)
	if err != nil {
		return "", nil, fmt.Errorf("open bundle tgz %s: %w", tgzPath, err)
	}
	defer func() { _ = f.Close() }()

	destDir, err := os.MkdirTemp("", "tide-bundle-*")
	if err != nil {
		return "", nil, fmt.Errorf("create temp dir: %w", err)
	}

	if err := extractTgzTo(f, destDir); err != nil {
		_ = os.RemoveAll(destDir)
		return "", nil, err
	}

	return destDir, func() { _ = os.RemoveAll(destDir) }, nil
}

// OpenBundleDir returns a readable directory root for a bundle that may be
// either a .tgz file or an already-unpacked directory. For a .tgz it extracts
// to a temp dir; for a directory it returns the path as-is with a no-op
// cleanup. (D-02 / Pitfall 6)
func OpenBundleDir(bundlePath string) (dir string, cleanup func(), err error) {
	info, err := os.Stat(bundlePath)
	if err != nil {
		return "", nil, fmt.Errorf("stat bundle path %s: %w", bundlePath, err)
	}

	if info.IsDir() {
		return bundlePath, func() {}, nil
	}

	// Treat as tgz.
	return ExtractBundle(bundlePath)
}

// extractTgzTo reads from r (which must be a gzip-compressed tar stream) and
// extracts all entries under destDir. Zip-slip defense: entries whose cleaned
// path starts with ".." or is absolute, or that resolve outside destDir, are
// rejected and no file is written.
func extractTgzTo(r io.Reader, destDir string) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}

		// Zip-slip defense (T-29-01-01).
		cleanName := filepath.Clean(hdr.Name)
		if strings.HasPrefix(cleanName, "..") || filepath.IsAbs(cleanName) {
			return fmt.Errorf("tgz entry %q rejected: path traversal", hdr.Name)
		}
		destPath := filepath.Join(destDir, cleanName)
		// Confirm destPath is still under destDir (catches symlink/join tricks).
		if !strings.HasPrefix(destPath, destDir+string(os.PathSeparator)) {
			return fmt.Errorf("tgz entry %q resolves outside dest dir (zip-slip)", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(destPath, 0o755); err != nil {
				return fmt.Errorf("mkdir %s: %w", destPath, err)
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
				return fmt.Errorf("mkdir parent of %s: %w", destPath, err)
			}
			outFile, err := os.Create(destPath)
			if err != nil {
				return fmt.Errorf("create %s: %w", destPath, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("write %s: %w", destPath, err)
			}
			if err := outFile.Close(); err != nil {
				return fmt.Errorf("close %s: %w", destPath, err)
			}
		}
	}
	return nil
}

// readTgzEntries reads all entries from a gzip-compressed tar stream and calls
// fn for each regular file entry with its name and a reader for its content.
func readTgzEntries(r io.Reader, fn func(name string, r io.Reader) error) error {
	gr, err := gzip.NewReader(r)
	if err != nil {
		return fmt.Errorf("open gzip reader: %w", err)
	}
	defer func() { _ = gr.Close() }()

	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read tar entry: %w", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			if err := fn(hdr.Name, tr); err != nil {
				return err
			}
		}
	}
	return nil
}
