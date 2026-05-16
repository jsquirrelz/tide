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
// No /bin/git shell-out. The package depends on go-git/v5 only — pure-Go,
// works on distroless/static, no system git binary needed in the push Job
// image. Compatible with K8s pod images that don't ship a system git.
//
// Import firewall: this package MUST NOT import LLM SDKs of any vendor or
// the TIDE CRD API types — pkg/git is provider-agnostic and CRD-agnostic.
// The push Job binary (cmd/tide-push, plan 03-06) composes pkg/git with
// internal/gitleaks and reads CRDs only via the controller side; pkg/git
// itself stays a leaf. The providerfirewall analyzer (cmd/tide-lint)
// should extend its forbiddenScopes denylist to cover this package as
// defense-in-depth.
package git
