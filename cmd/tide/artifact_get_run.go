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

package main

import (
	"fmt"
	"io"
	"strings"
)

// parseArtifactRef splits the operator-provided "<ns>/<project>/<path>" form
// into its three components. The path component may itself contain slashes;
// the function splits only on the first two.
//
// The PLAN spec (CONTEXT.md + 04-07-PLAN.md) documents this exact form so
// the CLI can resolve the per-project PVC + relative file path without an
// extra round-trip to the apiserver.
//
// Rejected inputs:
//
//   - empty string
//   - strings without at least two '/' separators
//   - strings whose first or second component is empty (e.g. "/foo/bar")
//   - strings whose path component is empty (e.g. "default/proj/")
func parseArtifactRef(ref string) (namespace, project, path string, err error) {
	if ref == "" {
		return "", "", "", fmt.Errorf("artifact ref is empty (expected <namespace>/<project>/<path>)")
	}
	// SplitN with n=3 splits only on the first 2 separators so the path
	// component may itself contain slashes.
	parts := strings.SplitN(ref, "/", 3)
	if len(parts) < 3 {
		return "", "", "", fmt.Errorf("artifact ref %q malformed (expected <namespace>/<project>/<path>)", ref)
	}
	namespace, project, path = parts[0], parts[1], parts[2]
	if namespace == "" || project == "" || path == "" {
		return "", "", "", fmt.Errorf("artifact ref %q has empty component (ns=%q project=%q path=%q)",
			ref, namespace, project, path)
	}
	return namespace, project, path, nil
}

// artifactGetDryRun prints a human-readable pod spec describing what the
// real (post-04-14) implementation would create. The dry-run form is the
// v1.0 Task-2 deliverable per the plan — the real apiserver pod-exec path
// lands in the kind harness work.
//
// The pod spec docs the inspector-pod design the v1.x implementation will
// instantiate: short-lived busybox container, PVC mount, terminationMessagePath
// shape, ttlSecondsAfterFinished cleanup.
func artifactGetDryRun(ref string, out io.Writer) error {
	ns, proj, path, err := parseArtifactRef(ref)
	if err != nil {
		return err
	}
	fmt.Fprintf(out, "tide artifact-get (dry-run; real apiserver pod-exec proxy lands in plan 04-14)\n")
	fmt.Fprintf(out, "  namespace: %s\n", ns)
	fmt.Fprintf(out, "  project: %s\n", proj)
	fmt.Fprintf(out, "  path: %s\n", path)
	fmt.Fprintf(out, "  inspector pod:\n")
	fmt.Fprintf(out, "    image: busybox:1.36\n")
	fmt.Fprintf(out, "    command: [\"sh\", \"-c\", \"cat /workspace/artifacts/%s\"]\n", path)
	fmt.Fprintf(out, "    volumeMounts:\n")
	fmt.Fprintf(out, "      - name: workspace\n")
	fmt.Fprintf(out, "        mountPath: /workspace\n")
	fmt.Fprintf(out, "    volumes:\n")
	fmt.Fprintf(out, "      - name: workspace\n")
	fmt.Fprintf(out, "        persistentVolumeClaim:\n")
	fmt.Fprintf(out, "          claimName: tide-projects\n")
	fmt.Fprintf(out, "    ttlSecondsAfterFinished: 30\n")
	return nil
}
