#!/usr/bin/env bash
# version-files.sh — the single list of files that carry the TIDE chart version.
#
# Both bump-version.sh and verify-version-consistency.sh source this list so the
# set of version-bearing files can never drift between the writer and the gate.
#
# Source-of-truth files live under hack/helm/ (augment-tide-chart.sh cp's them
# into charts/*/Chart.yaml). The generated charts/*/Chart.yaml are listed too so
# the tree is internally consistent without requiring a helmify run at bump time;
# `make verify-chart-reproducible` independently proves source → generated.
#
# Each file carries exactly two keys (both must agree across all files):
#   version: X.Y.Z        (unquoted)
#   appVersion: "X.Y.Z"   (quoted)

VERSION_FILES=(
  "hack/helm/tide-chart.yaml"
  "hack/helm/tide-crds-chart.yaml"
  "charts/tide/Chart.yaml"
  "charts/tide-crds/Chart.yaml"
)
