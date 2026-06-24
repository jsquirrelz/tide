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

// Command gen-salvage-seed generates the canonical bundle artifacts
// (seed-manifest.json + project.yaml) for the salvage-20260618 fixture from its
// exported CR List YAMLs (Phase 29 GAP-2).
//
// The salvage fixture was hand-assembled in Phase 28 with kubectl-List-style
// {projects,milestones,phases,plans}.yaml + pvc-envelopes.tgz, but `tide
// import-envelopes` expects the export-produced canonical layout: a singular
// project.yaml carrying spec.importSource plus a seed-manifest.json
// (pkg/bundle.BundleManifest). This generator bridges the two so the Phase 29
// kind E2E (29-05 Tier b) can import the real salvage data.
//
// Per-envelope sha256 is intentionally left empty (dry-run skips the check for
// hand-written fixtures, pkg/bundle/dryrun.go); the childCount integrity of the
// envelopes is already enforced by scripts/check-salvage-childcount.sh.
//
// Usage:
//
//	go run ./scripts/gen-salvage-seed examples/projects/dogfood/salvage-20260618
//
// It writes <dir>/seed-manifest.json and <dir>/project.yaml, validating the
// seed planning DAG (dag.ComputeWaves) before writing — an unresolved dependsOn
// ref or a cycle fails generation rather than the E2E.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"sigs.k8s.io/yaml"

	"github.com/jsquirrelz/tide/pkg/bundle"
	"github.com/jsquirrelz/tide/pkg/dag"
)

// crList is the minimal projection of a kubectl `get -o yaml` List we need.
type crList struct {
	Items []crItem `json:"items"`
}

type crItem struct {
	Metadata struct {
		Name string `json:"name"`
		UID  string `json:"uid"`
	} `json:"metadata"`
	Spec struct {
		ProjectRef   string   `json:"projectRef,omitempty"`
		MilestoneRef string   `json:"milestoneRef,omitempty"`
		PhaseRef     string   `json:"phaseRef,omitempty"`
		DependsOn    []string `json:"dependsOn,omitempty"`
	} `json:"spec"`
	Status struct {
		Phase string `json:"phase,omitempty"`
	} `json:"status"`
}

func readList(path string) crList {
	b, err := os.ReadFile(path)
	if err != nil {
		fatalf("read %s: %v", path, err)
	}
	var l crList
	if err := yaml.Unmarshal(b, &l); err != nil {
		fatalf("parse %s: %v", path, err)
	}
	return l
}

func main() {
	if len(os.Args) != 2 {
		fatalf("usage: gen-salvage-seed <salvage-dir>")
	}
	dir := os.Args[1]

	milestones := readList(filepath.Join(dir, "milestones.yaml"))
	phases := readList(filepath.Join(dir, "phases.yaml"))
	plans := readList(filepath.Join(dir, "plans.yaml"))
	projects := readList(filepath.Join(dir, "projects.yaml"))

	if len(projects.Items) != 1 {
		fatalf("expected exactly 1 Project in projects.yaml, got %d", len(projects.Items))
	}
	project := projects.Items[0]

	// phaseName -> milestoneName, for building plan FQNames.
	phaseToMilestone := map[string]string{}
	for _, ph := range phases.Items {
		phaseToMilestone[ph.Metadata.Name] = ph.Spec.MilestoneRef
	}

	var manifest bundle.BundleManifest
	for _, ms := range milestones.Items {
		manifest.Milestones = append(manifest.Milestones, bundle.BundleEntry{
			Name:       ms.Metadata.Name,
			FQName:     bundle.MilestoneFQName(ms.Metadata.Name),
			OldUID:     ms.Metadata.UID,
			DependsOn:  ms.Spec.DependsOn,
			Status:     ms.Status.Phase,
			ProjectRef: ms.Spec.ProjectRef,
		})
	}
	for _, ph := range phases.Items {
		manifest.Phases = append(manifest.Phases, bundle.BundleEntry{
			Name:         ph.Metadata.Name,
			FQName:       bundle.PhaseFQName(ph.Spec.MilestoneRef, ph.Metadata.Name),
			OldUID:       ph.Metadata.UID,
			DependsOn:    ph.Spec.DependsOn,
			Status:       ph.Status.Phase,
			MilestoneRef: ph.Spec.MilestoneRef,
		})
	}
	for _, pl := range plans.Items {
		manifest.Plans = append(manifest.Plans, bundle.BundleEntry{
			Name:      pl.Metadata.Name,
			FQName:    bundle.PlanFQName(phaseToMilestone[pl.Spec.PhaseRef], pl.Spec.PhaseRef, pl.Metadata.Name),
			OldUID:    pl.Metadata.UID,
			DependsOn: pl.Spec.DependsOn,
			Status:    pl.Status.Phase,
			PhaseRef:  pl.Spec.PhaseRef,
		})
	}

	// Validate the seed planning DAG exactly as the ImportController will
	// (nodes = CR names, edges = dep->name). Catch unresolved refs / cycles now.
	validateSeedDAG(manifest)

	manifestBytes, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		fatalf("marshal seed manifest: %v", err)
	}
	manifestBytes = append(manifestBytes, '\n')
	seedPath := filepath.Join(dir, "seed-manifest.json")
	if err := os.WriteFile(seedPath, manifestBytes, 0o644); err != nil {
		fatalf("write %s: %v", seedPath, err)
	}

	projectYAML := buildProjectYAML(project.Metadata.Name, project.Metadata.UID)
	projectPath := filepath.Join(dir, "project.yaml")
	if err := os.WriteFile(projectPath, []byte(projectYAML), 0o644); err != nil {
		fatalf("write %s: %v", projectPath, err)
	}

	fmt.Printf("gen-salvage-seed: wrote %s (%d milestones, %d phases, %d plans) and %s\n",
		seedPath, len(manifest.Milestones), len(manifest.Phases), len(manifest.Plans), projectPath)
}

// validateSeedDAG mirrors import_controller.handleCreatingCRs cycle detection.
func validateSeedDAG(m bundle.BundleManifest) {
	var nodes []dag.NodeID
	var edges []dag.Edge
	add := func(entries []bundle.BundleEntry) {
		for _, e := range entries {
			nodes = append(nodes, e.Name)
		}
	}
	add(m.Milestones)
	add(m.Phases)
	add(m.Plans)
	edge := func(entries []bundle.BundleEntry) {
		for _, e := range entries {
			for _, dep := range e.DependsOn {
				edges = append(edges, dag.Edge{From: dep, To: e.Name})
			}
		}
	}
	edge(m.Milestones)
	edge(m.Phases)
	edge(m.Plans)
	if _, err := dag.ComputeWaves(nodes, edges); err != nil {
		fatalf("seed planning DAG invalid (would fail import cycle detection): %v", err)
	}
}

// buildProjectYAML produces the singular project.yaml the E2E applies. It uses a
// stub subagent + zero budget (the kind E2E drives stub subagents at $0, like
// the small fixture) and wires spec.importSource to the salvage envelopes.
func buildProjectYAML(name, oldProjectUID string) string {
	return fmt.Sprintf(`---
# project.yaml — generated by scripts/gen-salvage-seed (Phase 29 GAP-2).
# The 29-05 Tier-b E2E applies this after `+"`tide import-envelopes`"+` stages the
# seed ConfigMap + salvage envelopes. spec.importSource drives the Phase 28
# ImportController. Stub subagent + $0 budget keep the run cost-free; adoption of
# the milestone/phase envelopes (zero planner Jobs) is what Tier b asserts.
apiVersion: tideproject.k8s/v1alpha2
kind: Project
metadata:
  name: %s
spec:
  schemaRevision: v1alpha2
  targetRepo: "https://git.example.internal/stub/no-such-repo.git"
  providerSecretRef: "tide-provider-secret"
  budget:
    absoluteCapCents: 0
  subagent:
    model: stub
  gates:
    milestone: auto
    phase: auto
    plan: auto
    task: auto
    pauseBetweenWaves: false
  importSource:
    seedManifestConfigMap: "tide-import-seed-%s"
    salvagedPVCSubPath: "%s/workspace"
`, name, name, oldProjectUID)
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "gen-salvage-seed: "+format+"\n", args...)
	os.Exit(1)
}
