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
	"io"
	"testing"
)

// TestWritePVCEnvelopesTgz_DirEntries (GAP-13/14) verifies the round-trip bundle
// writer honors directory entries (name ending in "/"). processEnvelopesTgz
// preserves the inspector tar's explicit dir entries; writing them as TypeReg
// makes Go's archive/tar fail ("filename may not have trailing slash"), crashing
// the export round-trip. They must be emitted as TypeDir.
func TestWritePVCEnvelopesTgz_DirEntries(t *testing.T) {
	files := map[string][]byte{
		"envelopes/":                      {},
		"envelopes/uid-1/":                {},
		"envelopes/uid-1/out.json":        []byte(`{"taskUID":"uid-1"}`),
		"envelopes/uid-1/children/":       {},
		"envelopes/uid-1/children/a.json": []byte(`{}`),
	}

	tgz, err := WritePVCEnvelopesTgz(files)
	if err != nil {
		t.Fatalf("WritePVCEnvelopesTgz with dir entries: %v", err)
	}

	gr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	tr := tar.NewReader(gr)
	dirTypes := map[string]byte{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar entry: %v", err)
		}
		dirTypes[hdr.Name] = hdr.Typeflag
	}

	for _, name := range []string{"envelopes/", "envelopes/uid-1/", "envelopes/uid-1/children/"} {
		if dirTypes[name] != tar.TypeDir {
			t.Errorf("entry %q typeflag = %q, want TypeDir", name, dirTypes[name])
		}
	}
	if dirTypes["envelopes/uid-1/out.json"] != tar.TypeReg {
		t.Errorf("file entry out.json typeflag = %q, want TypeReg", dirTypes["envelopes/uid-1/out.json"])
	}
}
