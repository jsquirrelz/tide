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
	"errors"
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

// maxPromptFileBytes is the D-11 cap on --prompt-file content.
// spec.outcomePrompt has no CRD MaxLength, so this CLI cap is the only
// guard; 256 KiB keeps the Project object comfortably under etcd's
// ~1.5 MiB ceiling with headroom for the rest of the spec.
const maxPromptFileBytes = 256 * 1024

// loadPromptFile reads and validates a --prompt-file per D-11: size cap
// enforced before any apiserver contact, exactly one trailing newline
// (LF or CRLF) trimmed, empty/whitespace-only content rejected. Bytes are
// otherwise returned verbatim — no templating or interpolation of any kind.
func loadPromptFile(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("read prompt file %s: %w", path, err)
	}
	if len(raw) > maxPromptFileBytes {
		return "", fmt.Errorf("prompt file %s is %d bytes; the limit is %d (256 KiB)", path, len(raw), maxPromptFileBytes)
	}
	content := strings.TrimSuffix(string(raw), "\n")
	content = strings.TrimSuffix(content, "\r")
	if strings.TrimSpace(content) == "" {
		return "", fmt.Errorf("prompt file %s is empty or whitespace-only", path)
	}
	return content, nil
}

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
		file       string
		promptFile string
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
			return runApply(cmd.Context(), file, promptFile, cmd.OutOrStdout())
		},
	}
	c.Flags().StringVarP(&file, "file", "f", "", "Path to YAML manifest (required)")
	c.Flags().StringVar(&promptFile, "prompt-file", "",
		"Inline this file into spec.outcomePrompt of the manifest's single Project document")
	return c
}

// prepareApplyObject is the cluster-free seam runApply calls BEFORE
// constructing the Kubernetes client, so every --prompt-file validation
// error fires without any apiserver contact.
//
// Without --prompt-file the first document decodes exactly as before this
// flag existed — behavior byte-identical. With --prompt-file, D-10 requires
// the manifest to contain exactly one Project document; refusing extra
// non-Project documents (rather than silently dropping them) is the
// fail-loud reading consistent with apply's single-object SSA semantics.
// D-09 refuses to override an existing spec.outcomePrompt.
func prepareApplyObject(raw []byte, path, promptFile string) (*unstructured.Unstructured, error) {
	if promptFile == "" {
		obj := &unstructured.Unstructured{}
		dec := yaml.NewYAMLOrJSONDecoder(bytesReader(raw), 4096)
		if err := dec.Decode(obj); err != nil {
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		if obj.GetKind() == "" || obj.GetAPIVersion() == "" {
			return nil, fmt.Errorf("%s: missing kind/apiVersion", path)
		}
		return obj, nil
	}

	// Fail fast on prompt content errors before touching the manifest.
	content, err := loadPromptFile(promptFile)
	if err != nil {
		return nil, err
	}

	var docs []*unstructured.Unstructured
	dec := yaml.NewYAMLOrJSONDecoder(bytesReader(raw), 4096)
	for {
		obj := &unstructured.Unstructured{}
		if err := dec.Decode(obj); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse %s: %w", path, err)
		}
		// Bare `---` separators decode to empty objects — skip them.
		if len(obj.Object) == 0 {
			continue
		}
		docs = append(docs, obj)
	}

	projectCount := 0
	var prj *unstructured.Unstructured
	for _, d := range docs {
		if d.GetKind() == "Project" {
			projectCount++
			prj = d
		}
	}
	if projectCount != 1 {
		return nil, fmt.Errorf("%s contains %d Project documents; --prompt-file requires exactly 1", path, projectCount)
	}
	if len(docs) > 1 {
		return nil, fmt.Errorf("--prompt-file requires a single-document manifest; %s has %d documents", path, len(docs))
	}

	// D-09: no silent override of an authored prompt.
	existing, found, _ := unstructured.NestedString(prj.Object, "spec", "outcomePrompt")
	if found && strings.TrimSpace(existing) != "" {
		return nil, fmt.Errorf(
			"%s already sets spec.outcomePrompt; remove it from the manifest or drop --prompt-file (no silent override)", path)
	}
	if err := unstructured.SetNestedField(prj.Object, content, "spec", "outcomePrompt"); err != nil {
		return nil, fmt.Errorf("inject prompt file %s into %s: %w", promptFile, path, err)
	}
	if prj.GetKind() == "" || prj.GetAPIVersion() == "" {
		return nil, fmt.Errorf("%s: missing kind/apiVersion", path)
	}
	return prj, nil
}

// runApply is the testable seam — reads the YAML, builds an unstructured
// object via prepareApplyObject, and patches it server-side.
func runApply(ctx context.Context, path, promptFile string, out io.Writer) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read %s: %w", path, err)
	}

	obj, err := prepareApplyObject(raw, path, promptFile)
	if err != nil {
		return err
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
