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
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// ReadEnvelopeIn opens the file at path, JSON-decodes a [pkgdispatch.EnvelopeIn],
// and validates the apiVersion/kind discriminator via
// [pkgdispatch.ValidateAPIVersionKind]. Returns a wrapped error if the file is
// unreadable, malformed, or carries an unrecognized apiVersion/kind.
//
// This is the first call the harness binary makes on startup; an error here is
// a hard failure — the Task cannot proceed without a valid envelope (D-A3 /
// T-02-06-04 mitigate).
func ReadEnvelopeIn(path string) (pkgdispatch.EnvelopeIn, error) {
	f, err := os.Open(path)
	if err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: open envelope %q: %w", path, err)
	}
	defer func() { _ = f.Close() }() // read-only handle; close error is not actionable

	var env pkgdispatch.EnvelopeIn
	if err := json.NewDecoder(f).Decode(&env); err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: decode envelope %q: %w", path, err)
	}

	if err := pkgdispatch.ValidateAPIVersionKind(env.APIVersion, env.Kind, pkgdispatch.KindTaskEnvelopeIn); err != nil {
		return pkgdispatch.EnvelopeIn{}, fmt.Errorf("harness: validate envelope %q: %w", path, err)
	}

	return env, nil
}

// WriteEnvelopeIn marshals env to JSON and writes it to path. Missing ancestor
// directories are created (D-G2 lazy mkdir). Permissions are 0o644.
//
// This is used by the orchestrator (controller) to write the input envelope to
// the PVC before creating the Task's K8s Job. The harness binary reads it back
// via [ReadEnvelopeIn].
func WriteEnvelopeIn(path string, env pkgdispatch.EnvelopeIn) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("harness: mkdirall for envelope-in %q: %w", path, err)
	}
	data, err := json.Marshal(env)
	if err != nil {
		return fmt.Errorf("harness: marshal envelope-in: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("harness: write envelope-in %q: %w", path, err)
	}
	return nil
}

// WriteEnvelopeOut marshals out to JSON and writes it to path. Missing ancestor
// directories are created via os.MkdirAll (D-G2 lazy mkdir). Permissions
// are 0o644.
//
// The harness binary calls this after [Harness.Run] returns to persist the
// result envelope to /workspace/envelopes/{task-uid}/out.json on the PVC.
// The controller reads it back in handleJobCompletion (Plan 09).
func WriteEnvelopeOut(path string, out pkgdispatch.EnvelopeOut) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("harness: mkdirall for envelope-out %q: %w", path, err)
	}
	data, err := json.Marshal(out)
	if err != nil {
		return fmt.Errorf("harness: marshal envelope-out: %w", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("harness: write envelope-out %q: %w", path, err)
	}
	return nil
}
