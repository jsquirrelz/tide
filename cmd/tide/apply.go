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
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// newApplyCmd implements `tide apply -f <file>` — a server-side-apply wrapper
// around the kubectl apply semantics. Reads a YAML manifest, unmarshals into
// unstructured.Unstructured, calls client.Patch(client.Apply) with field
// manager "tide-cli".
//
// On CRD validation errors (apierrors.IsInvalid) the StatusError details
// are surfaced field-by-field so the operator sees what to fix without
// scraping a raw apiserver response.
func newApplyCmd() *cobra.Command {
	var (
		file string
	)
	c := &cobra.Command{
		Use:   "apply",
		Short: "Apply a TIDE manifest (server-side apply)",
		Long: "tide apply wraps kubectl apply semantics: reads YAML, server-side-applies with " +
			"FieldManager=tide-cli, and surfaces CRD validation errors field-by-field.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if file == "" {
				return fmt.Errorf("--file/-f is required")
			}
			return runApply(cmd.Context(), file, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "Path to YAML manifest (required)")
	return c
}

// runApply is the testable seam — reads the YAML, builds an unstructured
// object, and patches it server-side.
func runApply(ctx context.Context, path string, out io.Writer) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	obj := &unstructured.Unstructured{}
	dec := yaml.NewYAMLOrJSONDecoder(bytesReader(raw), 4096)
	if err := dec.Decode(obj); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if obj.GetKind() == "" || obj.GetAPIVersion() == "" {
		return fmt.Errorf("%s: missing kind/apiVersion", path)
	}

	c, err := K8sClient()
	if err != nil {
		return err
	}

	// Default namespace from the resolved kubectl context when the manifest
	// omits one (kubectl-aligned UX).
	if obj.GetNamespace() == "" {
		ns, nerr := resolveNamespace()
		if nerr == nil && ns != "" {
			obj.SetNamespace(ns)
		}
	}

	//nolint:staticcheck // SA1019: client.Apply SSA path is load-bearing for unstructured objects;
	// migrating to client.Client.Apply() (typed ApplyConfiguration) is out of scope for lint hygiene.
	if err := c.Patch(ctx, obj, client.Apply, &client.PatchOptions{
		FieldManager: "tide-cli",
		Force:        new(true),
	}); err != nil {
		return formatApplyError(obj, err)
	}

	fmt.Fprintf(out, "%s/%s applied\n", obj.GetKind(), obj.GetName())
	return nil
}

// formatApplyError renders apierrors.IsInvalid causes inline so the operator
// sees "spec.targetRepo: required value" instead of a wall of JSON.
func formatApplyError(obj *unstructured.Unstructured, err error) error {
	if !apierrors.IsInvalid(err) {
		return fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	se, ok := err.(*apierrors.StatusError)
	if !ok || se.ErrStatus.Details == nil {
		return fmt.Errorf("apply %s/%s: %w", obj.GetKind(), obj.GetName(), err)
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "apply %s/%s: validation failed", obj.GetKind(), obj.GetName())
	for _, cause := range se.ErrStatus.Details.Causes {
		fmt.Fprintf(&msg, "\n  %s: %s", cause.Field, cause.Message)
	}
	return fmt.Errorf("%s", msg.String())
}

// bytesReader wraps a byte slice in an io.Reader without pulling in bytes
// package symbols at the call site (keeps the import list short).
func bytesReader(b []byte) io.Reader {
	return &byteReader{b: b}
}

type byteReader struct {
	b []byte
	i int
}

func (r *byteReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
