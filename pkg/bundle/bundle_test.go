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
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeBundleFiles returns a minimal in-memory file map representing a bundle.
func makeBundleFiles() map[string][]byte {
	return map[string][]byte{
		BundleFileProject:      []byte("project: yaml"),
		BundleFileMilestones:   []byte("milestones: yaml"),
		BundleFilePhases:       []byte("phases: yaml"),
		BundleFilePlans:        []byte("plans: yaml"),
		BundleFileSeedManifest: []byte(`{"milestones":[],"phases":[],"plans":[]}`),
		BundleFileSeedOutline:  []byte("# Seed Outline\n"),
		BundleFilePVCEnvelopes: []byte("fake-tgz-bytes"),
	}
}

// TestRoundTrip writes a bundle tgz then reads it back and asserts contents match.
func TestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "bundle.tgz")

	files := makeBundleFiles()

	if err := WriteBundle(tgzPath, files); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	got, err := ReadBundle(tgzPath)
	if err != nil {
		t.Fatalf("ReadBundle: %v", err)
	}

	for name, want := range files {
		data, ok := got[name]
		if !ok {
			t.Errorf("ReadBundle missing file %q", name)
			continue
		}
		if !bytes.Equal(data, want) {
			t.Errorf("ReadBundle file %q: got %q, want %q", name, data, want)
		}
	}
	for name := range got {
		if _, ok := files[name]; !ok {
			t.Errorf("ReadBundle returned unexpected file %q", name)
		}
	}
}

// TestBundleSevenEntries verifies WriteBundle produces exactly seven entries.
func TestBundle(t *testing.T) {
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "bundle.tgz")

	if err := WriteBundle(tgzPath, makeBundleFiles()); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	f, err := os.Open(tgzPath)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var names []string
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		names = append(names, hdr.Name)
	}

	if len(names) != 7 {
		t.Errorf("expected 7 entries, got %d: %v", len(names), names)
	}

	wantNames := map[string]bool{
		BundleFileProject:      true,
		BundleFileMilestones:   true,
		BundleFilePhases:       true,
		BundleFilePlans:        true,
		BundleFileSeedManifest: true,
		BundleFileSeedOutline:  true,
		BundleFilePVCEnvelopes: true,
	}
	for _, n := range names {
		if !wantNames[n] {
			t.Errorf("unexpected entry %q in tgz", n)
		}
	}
}

// TestExtract verifies ExtractBundle unpacks to a temp dir and returns readable files.
func TestExtract(t *testing.T) {
	dir := t.TempDir()
	tgzPath := filepath.Join(dir, "bundle.tgz")
	files := makeBundleFiles()

	if err := WriteBundle(tgzPath, files); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	destDir, cleanup, err := ExtractBundle(tgzPath)
	if err != nil {
		t.Fatalf("ExtractBundle: %v", err)
	}
	defer cleanup()

	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(destDir, name))
		if err != nil {
			t.Errorf("read extracted %q: %v", name, err)
			continue
		}
		if !bytes.Equal(data, want) {
			t.Errorf("extracted %q: got %q, want %q", name, data, want)
		}
	}
}

// TestZipSlipDotDot verifies extraction rejects "../escape" entries with no file written.
func TestZipSlip(t *testing.T) {
	t.Run("dot-dot entry rejected", func(t *testing.T) {
		destDir := t.TempDir()
		tgzBytes := makeMaliciousTgz(t, "../escape.txt", []byte("evil"))

		err := extractTgzTo(bytes.NewReader(tgzBytes), destDir)
		if err == nil {
			t.Fatal("expected error for ../escape.txt entry, got nil")
		}
		if !strings.Contains(err.Error(), "path traversal") && !strings.Contains(err.Error(), "zip-slip") {
			t.Errorf("error should mention path traversal or zip-slip: %v", err)
		}

		// No file must have been written.
		escapeFile := filepath.Join(filepath.Dir(destDir), "escape.txt")
		if _, statErr := os.Stat(escapeFile); statErr == nil {
			t.Error("zip-slip: ../escape.txt was written outside dest dir")
		}
	})

	t.Run("absolute path entry rejected", func(t *testing.T) {
		destDir := t.TempDir()
		tgzBytes := makeMaliciousTgz(t, "/etc/passwd", []byte("evil"))

		err := extractTgzTo(bytes.NewReader(tgzBytes), destDir)
		if err == nil {
			t.Fatal("expected error for /etc/passwd entry, got nil")
		}
	})
}

// TestBundleDirVsTgz verifies OpenBundleDir works for both a .tgz and a directory.
func TestBundleDirVsTgz(t *testing.T) {
	// Prepare bundle tgz.
	tgzDir := t.TempDir()
	tgzPath := filepath.Join(tgzDir, "bundle.tgz")
	files := makeBundleFiles()
	if err := WriteBundle(tgzPath, files); err != nil {
		t.Fatalf("WriteBundle: %v", err)
	}

	t.Run("tgz input", func(t *testing.T) {
		dir, cleanup, err := OpenBundleDir(tgzPath)
		if err != nil {
			t.Fatalf("OpenBundleDir(tgz): %v", err)
		}
		defer cleanup()
		assertFilesPresent(t, dir, files)
	})

	t.Run("directory input", func(t *testing.T) {
		// Write the same files into a plain directory.
		plainDir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(plainDir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}

		dir, cleanup, err := OpenBundleDir(plainDir)
		if err != nil {
			t.Fatalf("OpenBundleDir(dir): %v", err)
		}
		defer cleanup()
		assertFilesPresent(t, dir, files)
	})
}

// makeMaliciousTgz creates an in-memory tgz with a single entry at the given name.
func makeMaliciousTgz(t *testing.T, entryName string, data []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{
		Name: entryName,
		Mode: 0o644,
		Size: int64(len(data)),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// assertFilesPresent checks that all expected files exist under dir with matching content.
func assertFilesPresent(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, want := range files {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("file %q missing in dir %s: %v", name, dir, err)
			continue
		}
		if !bytes.Equal(data, want) {
			t.Errorf("file %q content mismatch", name)
		}
	}
}
