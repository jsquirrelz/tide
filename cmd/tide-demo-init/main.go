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

// Command tide-demo-init is the local-only git remote bootstrap binary
// for the TIDE medium sample (Phase 5 D-B3 — D-B3 local-only git remote
// per CONTEXT.md).
//
// The binary initializes a fresh bare git repo at --bootstrap-dir, then
// populates it with the embedded `examples/tide-demo-fixture/` content
// (carried via //go:embed all:fixture, with the fixture directory
// positioned at build time — see Build notes below). Used as an in-cluster
// Job (`examples/projects/medium/demo-remote-init-job.yaml`) that runs
// BEFORE the medium-sample Project is applied; the resulting bare repo on
// `demo-remote-pvc` is what TIDE then clones via the medium-sample
// `Project.Spec.targetRepo: file:///demo-remote.git`. Result: operators
// run the medium sample with real Claude (~$5) against a fully local-only
// git remote — no external public repo dependency (per D-B3 + RESEARCH
// §"Topic 4 Option b").
//
// Flags:
//
//	--bootstrap-dir <path>   Required. Filesystem path at which to create
//	                         the bare repo (e.g. /workspace/demo-remote.git).
//	                         Refuses to overwrite an existing directory.
//
// Exit codes (mirrors cmd/tide-push convention):
//
//	0 — success (bare repo created and populated)
//	1 — generic failure (git init / fixture extract / commit / push)
//	2 — invariant violation (--bootstrap-dir empty, target dir already
//	    exists, or other argument-shape errors)
//
// Build (local):
//
//	First run `go generate ./cmd/tide-demo-init/...` to materialize
//	cmd/tide-demo-init/fixture/ from examples/tide-demo-fixture/ — this
//	directory is required by the //go:embed all:fixture directive below
//	but is gitignored to keep the SOT at examples/tide-demo-fixture/.
//	Then `go build ./cmd/tide-demo-init/`.
//
// Build (Docker):
//
//	The Dockerfile at images/tide-demo-init/Dockerfile materializes the
//	fixture into cmd/tide-demo-init/fixture/ in the build context
//	automatically; just run
//	`docker build -f images/tide-demo-init/Dockerfile .` from the repo
//	root.
//
// Embed strategy is RESOLVED per MEDIUM-11 (PLAN.md 05-12): the embed
// directive points at the sibling `fixture/` directory, populated either
// by `go generate` (local builds) or by the Dockerfile COPY (image
// builds). No symlinks (incompatible across toolchains).
//
// Submodule shim: the fixture SOT at examples/tide-demo-fixture/ ships
// its own go.mod / go.sum (it's a standalone Go module for the Claude
// task to operate on). Embedding a sibling go.mod inside this command
// would make Go treat fixture/ as a different module and reject the
// //go:embed directive ("cannot embed directory ...: in different
// module"). Workaround: the go:generate / Dockerfile rename go.mod →
// go.mod.txt and go.sum → go.sum.txt when materializing fixture/, and
// unpackFixture reverses the rename at write time so the bare repo's
// working tree carries the canonical go.mod / go.sum filenames.
package main

//go:generate bash -c "set -eu; rm -rf fixture; mkdir -p fixture; cp -r ../../examples/tide-demo-fixture/. ./fixture/; [ -f fixture/go.mod ] && mv fixture/go.mod fixture/go.mod.txt; [ -f fixture/go.sum ] && mv fixture/go.sum fixture/go.sum.txt; true"

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	gogit "github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// fixtureFS embeds the medium-sample seed scaffold from a sibling
// `fixture/` directory positioned at build time (per the package-doc Build
// section). The `all:` prefix preserves dotfiles and otherwise-skipped
// names if the fixture ever grows them.
//
//go:embed all:fixture
var fixtureFS embed.FS

// fixtureRoot is the directory name embedded via fixtureFS — the WalkDir
// closure strips this prefix when writing files into the working tree so
// the resulting bare-repo layout looks like the source-of-truth fixture
// directly (no nested `fixture/` subdir inside the remote).
const fixtureRoot = "fixture"

const (
	exitSuccess     = 0
	exitGenericFail = 1
	exitInvariant   = 2
)

// commitMessage is the boundary message for the single seeding commit in
// the bootstrapped bare repo. Phase 5 D-B3 (per CONTEXT.md): the medium
// sample's local-only remote starts from this initial commit; TIDE
// subsequently clones the remote, plans the function-addition outcome,
// and pushes per-run-branch commits back.
const commitMessage = "Initial fixture content (Phase 5 D-B3)"

// authorName / authorEmail compose the deterministic git author signature
// used for the seeding commit. Distinct from cmd/tide-push's "tide-bot"
// identity (boundary-commit author at runtime) — this is the demo-init
// identity, present only on the very first commit of the local-only
// remote. The `.example` TLD per RFC 2606 keeps the address non-routable.
const (
	authorName  = "TIDE Demo Bootstrap"
	authorEmail = "tide-demo@noreply.example"
)

func main() {
	flagSet := flag.NewFlagSet("tide-demo-init", flag.ExitOnError)
	bootstrapDir := flagSet.String("bootstrap-dir", "",
		"filesystem path at which to create the bare git remote (e.g. /workspace/demo-remote.git)")

	if err := flagSet.Parse(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "tide-demo-init: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	if err := bootstrap(ctx, *bootstrapDir); err != nil {
		// Invariant errors (missing flag / target exists) map to exit 2;
		// all other errors map to exit 1. The errInvariant sentinel lets
		// tests assert the invariant path without depending on stderr.
		var inv *invariantError
		if errors.As(err, &inv) {
			fmt.Fprintf(os.Stderr, "tide-demo-init: %v\n", err)
			os.Exit(exitInvariant)
		}
		fmt.Fprintf(os.Stderr, "tide-demo-init: %v\n", err)
		os.Exit(exitGenericFail)
	}

	fmt.Printf("OK: bootstrapped local-only git remote at %s\n", *bootstrapDir)
	os.Exit(exitSuccess)
}

// invariantError marks errors that map to exit code 2 (invariant
// violation) rather than 1 (generic failure). main() uses errors.As to
// distinguish; tests assert the same via errors.As without relying on
// process exit semantics.
type invariantError struct {
	msg string
}

func (e *invariantError) Error() string { return e.msg }

func newInvariantError(format string, a ...any) error {
	return &invariantError{msg: fmt.Sprintf(format, a...)}
}

// bootstrap is the testable in-process entry point. Steps:
//
//  1. Validate dir is non-empty and does NOT already exist (refuse to
//     overwrite — invariantError → exit 2).
//  2. PlainInit(dir, true) to create the bare repo at dir.
//  3. Materialize the embedded fixture into a temp working dir.
//  4. PlainInit(workdir, false) + Worktree().Add(.) + Worktree().Commit
//     with the deterministic author signature.
//  5. CreateRemote pointing at file://<dir>; PushContext refs/heads/<default-branch>.
//  6. Return nil on success.
//
// On error: any failure after step 1 is a generic-failure error (exit 1);
// only the step-1 validation paths return invariantError (exit 2).
func bootstrap(ctx context.Context, dir string) error {
	// Step 1 — invariant checks.
	if dir == "" {
		return newInvariantError("bootstrap-dir required (use --bootstrap-dir <path>)")
	}
	if _, err := os.Stat(dir); err == nil {
		return newInvariantError("target dir already exists at %s (refuse to overwrite)", dir)
	} else if !errors.Is(err, os.ErrNotExist) {
		// Any non-NotExist stat error is a generic failure (permission
		// denied, broken symlink, etc.) — distinct from the
		// existing-target invariant.
		return fmt.Errorf("stat %s: %w", dir, err)
	}

	// Ensure the parent directory exists. The bare repo dir itself MUST
	// not exist (PlainInit creates it); the parent is conventional
	// MkdirAll.
	if parent := filepath.Dir(dir); parent != "" && parent != "." {
		if err := os.MkdirAll(parent, 0o755); err != nil {
			return fmt.Errorf("mkdir parent %s: %w", parent, err)
		}
	}

	// Step 2 — initialize the bare repo at dir.
	bareRepo, err := gogit.PlainInit(dir, true /* bare */)
	if err != nil {
		return fmt.Errorf("git init --bare %s: %w", dir, err)
	}
	// Smoke-check: the bare repo's config exists. If PlainInit returned
	// nil error AND nil repo we'd panic below; defensive nil check.
	if bareRepo == nil {
		return fmt.Errorf("git init --bare %s returned nil repo without error", dir)
	}

	// Step 3 — materialize the embedded fixture into a temp working dir.
	// We don't use t.TempDir-style cleanup here because the in-cluster
	// Job lifetime IS the cleanup boundary (the Pod terminates after the
	// process exits, taking its writable layer with it). For host-machine
	// testing, the per-process os.MkdirTemp tempdir is cleaned via the
	// caller's t.TempDir or test-suite teardown.
	workdir, err := os.MkdirTemp("", "tide-demo-init-work-*")
	if err != nil {
		return fmt.Errorf("mktemp workdir: %w", err)
	}
	// Best-effort cleanup; do not mask the primary error if RemoveAll fails.
	defer func() { _ = os.RemoveAll(workdir) }()

	if err := unpackFixture(workdir); err != nil {
		return fmt.Errorf("unpack fixture into %s: %w", workdir, err)
	}

	// Step 4 — init working repo + stage + commit.
	workRepo, err := gogit.PlainInit(workdir, false /* not bare */)
	if err != nil {
		return fmt.Errorf("git init workdir %s: %w", workdir, err)
	}
	wt, err := workRepo.Worktree()
	if err != nil {
		return fmt.Errorf("worktree workdir %s: %w", workdir, err)
	}
	if err := wt.AddGlob("."); err != nil {
		return fmt.Errorf("git add . in %s: %w", workdir, err)
	}
	if _, err := wt.Commit(commitMessage, &gogit.CommitOptions{
		Author: &object.Signature{
			Name:  authorName,
			Email: authorEmail,
			When:  time.Now(),
		},
	}); err != nil {
		return fmt.Errorf("git commit %q: %w", commitMessage, err)
	}

	// Step 5 — wire workdir → bare repo as `origin`, then push the
	// default branch the working repo created (go-git's PlainInit picks
	// "master" by default at v5.19.0; we resolve the actual ref via
	// repo.Head so we don't hardcode the name).
	headRef, err := workRepo.Head()
	if err != nil {
		return fmt.Errorf("resolve workdir HEAD: %w", err)
	}
	branchRef := headRef.Name() // e.g. refs/heads/master

	bareURL := "file://" + dir
	if _, err := workRepo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{bareURL},
	}); err != nil {
		return fmt.Errorf("create remote %s: %w", bareURL, err)
	}

	refSpec := config.RefSpec(fmt.Sprintf("%s:%s", branchRef, branchRef))
	if err := workRepo.PushContext(ctx, &gogit.PushOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
	}); err != nil {
		return fmt.Errorf("git push %s -> %s: %w", branchRef, bareURL, err)
	}

	return nil
}

// unpackFixture walks fixtureFS and writes every regular file to its
// corresponding path under destDir. The leading "fixture/" path component
// is stripped so the resulting layout mirrors examples/tide-demo-fixture/
// at the destDir root.
//
// Submodule shim (per package-doc note): filenames ending in `.txt` that
// correspond to renamed go.mod / go.sum sources (so Go's embed doesn't
// see a nested module) are restored to their canonical names at write
// time. This keeps the bare repo's working tree byte-for-byte equivalent
// to the SOT at examples/tide-demo-fixture/.
//
// Directories are created lazily on the parent of each file write
// (MkdirAll(0o755)) — empty directories in the embed are NOT recreated,
// matching the medium-sample's content shape (no empty dirs in
// examples/tide-demo-fixture/).
func unpackFixture(destDir string) error {
	return fs.WalkDir(fixtureFS, fixtureRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return fmt.Errorf("walk %s: %w", path, err)
		}
		if d.IsDir() {
			return nil
		}
		// Strip the fixtureRoot prefix so destPath sits directly under destDir.
		rel, err := filepath.Rel(fixtureRoot, path)
		if err != nil {
			return fmt.Errorf("rel %s vs %s: %w", path, fixtureRoot, err)
		}
		// Reverse the submodule-shim rename so go.mod.txt → go.mod, etc.
		rel = restoreShimmedName(rel)
		destPath := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", filepath.Dir(destPath), err)
		}
		data, err := fixtureFS.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read embedded %s: %w", path, err)
		}
		if err := os.WriteFile(destPath, data, 0o644); err != nil {
			return fmt.Errorf("write %s: %w", destPath, err)
		}
		return nil
	})
}

// restoreShimmedName reverses the go.mod → go.mod.txt rename applied by
// the go:generate / Dockerfile step. The shim's only purpose is to keep
// Go's embed from rejecting fixture/ as a nested module; the bare repo
// must carry the canonical filenames.
func restoreShimmedName(rel string) string {
	switch filepath.Base(rel) {
	case "go.mod.txt":
		return filepath.Join(filepath.Dir(rel), "go.mod")
	case "go.sum.txt":
		return filepath.Join(filepath.Dir(rel), "go.sum")
	default:
		return rel
	}
}
