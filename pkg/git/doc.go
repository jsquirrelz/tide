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

// Package git is a provider-agnostic, host-agnostic Go API around go-git/v5
// for the operations TIDE's orchestrator and push Job need: clone, per-Task
// worktree-add, commit, push (with optional --force-with-lease), and fetch.
//
// HTTPS+PAT is the default and only v1.0 auth path (ART-05). The package uses
// the Username "x-access-token" convention: GitHub recognizes it explicitly;
// GitLab and Gitea accept any non-empty Username when the Password is a
// personal access token. SSH is supported by go-git/v5 at the library level
// but documented with host-key caveats in Phase 3 docs (plan 03-09); SSH
// wiring lives behind a future seam in this package, not in v1.0.
//
// Per ART-03, the package is intentionally provider-agnostic: no per-host
// adapters (GitHub API, GitLab webhook clients) live here. Per-host PR
// creation surfaces are deferred (v2+ per REQUIREMENTS.md "Deferred").
//
// Per ART-06, this package is the single seam through which TIDE pushes
// commits to a remote — combined with D-B6's per-run branch naming and
// --force-with-lease against Project.Status.git.lastPushedSHA, this is the
// structural mitigation for Pitfall 13 (TIDE-overwrites-human-commits).
//
// Transport dependency on a system git binary — IMPORTANT. go-git/v5's
// HTTP(S) and SSH transports are pure-Go (Go's crypto/tls, no CA bundle, no
// /bin/git). Its file:// transport is NOT pure-Go: a file:// clone/fetch/push
// shells out to the system git binary (git-upload-pack / git-receive-pack) at
// runtime. The worktree-add path (worktree.go) PlainClones the local bare repo
// over a file:// URL, and the demo/medium-sample bootstrap pushes over file://,
// so ANY runtime image that exercises those paths MUST ship a system git on
// $PATH. The git-op images (tide-demo-init, tide-push, claude-subagent) install
// git for exactly this reason; credproxy performs no git ops and stays
// git-less. Images that only ever use HTTPS/SSH remotes need no system git.
// (See debug session file-transport-git-missing for the v1.0 regression where
// the distroless/static images shipped without git and failed file:// ops with
// `exec: "git": executable file not found in $PATH`.)
//
// Import firewall: this package MUST NOT import LLM SDKs of any vendor or
// the TIDE CRD API types — pkg/git is provider-agnostic and CRD-agnostic.
// The push Job binary (cmd/tide-push, plan 03-06) composes pkg/git with
// internal/gitleaks and reads CRDs only via the controller side; pkg/git
// itself stays a leaf. The providerfirewall analyzer (cmd/tide-lint)
// should extend its forbiddenScopes denylist to cover this package as
// defense-in-depth.
package git
