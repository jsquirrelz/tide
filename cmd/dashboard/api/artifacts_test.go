/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/cmd/dashboard/gitfetch"
)

// fakeFetcher is a gitfetch.Fetcher test double. It serves a fixed SHA + file
// set, or an injected error, and records the auth it was handed so tests can
// assert credential pass-through without a real remote.
type fakeFetcher struct {
	sha      string
	files    []gitfetch.File
	tipErr   error
	fetchErr error

	// capturedAuth records the *gitfetch.Auth handed to Fetch so tests can
	// assert credential pass-through (nil == anonymous) without a real remote.
	fetchCalled  bool
	capturedAuth *gitfetch.Auth
}

func (f *fakeFetcher) Tip(_ context.Context, _, _ string, _ *gitfetch.Auth) (string, error) {
	if f.tipErr != nil {
		return "", f.tipErr
	}
	return f.sha, nil
}

func (f *fakeFetcher) Fetch(_ context.Context, _, _ string, auth *gitfetch.Auth) (string, []gitfetch.File, error) {
	f.fetchCalled = true
	f.capturedAuth = auth
	if f.fetchErr != nil {
		return "", nil, f.fetchErr
	}
	return f.sha, f.files, nil
}

// newArtifactsHandler wires an ArtifactsHandler over a fake controller-runtime
// client (for CR Gets), an optional typed clientset (for the Secret read), and
// a Store built over the given Fetcher.
func newArtifactsHandler(t *testing.T, cs kubernetes.Interface, f gitfetch.Fetcher, objs ...runtime.Object) http.Handler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	store, err := gitfetch.NewStore(f, 4)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	h := &ArtifactsHandler{
		Client:    builder.Build(),
		Clientset: cs,
		Store:     store,
		Log:       testr.New(t),
	}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/nodes/{kind}/{name}/artifacts", h.Get)
	})
	return r
}

// gitProject builds a Project with a git remote + a run branch so the handler
// proceeds past the no-git / absent-branch short circuits.
func gitProject() *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			Git: &tidev1alpha3.GitConfig{
				RepoURL:        "https://example.com/repo.git",
				CredsSecretRef: "git-creds",
			},
		},
		Status: tidev1alpha3.ProjectStatus{
			Git: tidev1alpha3.GitStatus{BranchName: "tide/run-prj-1-1"},
		},
	}
}

// httpGitProject builds a Project on an anonymous in-cluster http:// remote
// (Gap 37-G1 repro) with a run branch + a credsSecretRef. The scheme is the
// only difference from gitProject() (https://) — it gates the empty-PAT relax.
func httpGitProject() *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "http://git-http-server.tide.svc/repo.git",
			Git: &tidev1alpha3.GitConfig{
				RepoURL:        "http://git-http-server.tide.svc/repo.git",
				CredsSecretRef: "git-creds",
			},
		},
		Status: tidev1alpha3.ProjectStatus{
			Git: tidev1alpha3.GitStatus{BranchName: "tide/run-prj-1-1"},
		},
	}
}

const patSentinel = "ghp_SUPERSECRETPATVALUE_do_not_leak"

func credsSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "default"},
		Data:       map[string][]byte{"GIT_PAT": []byte(patSentinel)},
	}
}

// emptyCredsSecret carries a GIT_PAT key whose value is an empty byte slice —
// the exact shape an anonymous in-cluster creds Secret takes.
func emptyCredsSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "default"},
		Data:       map[string][]byte{"GIT_PAT": []byte("")},
	}
}

// noKeyCredsSecret has a Data map with no GIT_PAT key at all.
func noKeyCredsSecret() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "default"},
		Data:       map[string][]byte{"OTHER": []byte("x")},
	}
}

func doGet(t *testing.T, router http.Handler, path string) (*http.Response, []byte) {
	t.Helper()
	srv := httptest.NewServer(router)
	defer srv.Close()
	resp, err := http.Get(srv.URL + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read body: %v", err)
	}
	return resp, buf.Bytes()
}

// TestArtifactsNoGit: a Project without spec.git returns 200 {state:"no-git", files:[]}.
func TestArtifactsNoGit(t *testing.T) {
	prj := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec:       tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/repo.git"},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(), &fakeFetcher{}, prj)
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "no-git" {
		t.Errorf("state=%q want no-git", na.State)
	}
	if na.Files == nil || len(na.Files) != 0 {
		t.Errorf("files=%v want empty non-nil slice", na.Files)
	}
	// Empty-array-not-null: the raw body must carry `"files":[]`.
	if !bytes.Contains(body, []byte(`"files":[]`)) {
		t.Errorf("body must serialize files as [], got %s", body)
	}
}

// TestArtifactsAvailable: full content fidelity incl. a >1 MiB fixture.
func TestArtifactsAvailable(t *testing.T) {
	big := bytes.Repeat([]byte("A"), (1<<20)+7) // > 1 MiB (D-03: no caps/truncation)
	f := &fakeFetcher{
		sha: "deadbeefcafe",
		files: []gitfetch.File{
			{Name: "MILESTONE.md", Path: ".tide/planning/milestone/m1/MILESTONE.md", Content: []byte("# Milestone 1\n")},
			{Name: "p1.json", Path: ".tide/planning/milestone/m1/children/p1.json", Content: []byte(`{"plan":"p1"}`)},
			{Name: "BIG.md", Path: ".tide/planning/milestone/m1/BIG.md", Content: big},
			// A file OUTSIDE the requested node prefix — must be filtered out.
			{Name: "other.md", Path: ".tide/planning/phase/p9/other.md", Content: []byte("nope")},
		},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "available" {
		t.Fatalf("state=%q want available", na.State)
	}
	if na.Branch != "tide/run-prj-1-1" {
		t.Errorf("branch=%q want tide/run-prj-1-1", na.Branch)
	}
	if na.CommitSHA != "deadbeefcafe" {
		t.Errorf("commitSHA=%q want deadbeefcafe", na.CommitSHA)
	}
	if len(na.Files) != 3 {
		t.Fatalf("files=%d want 3 (node-prefix filtered)", len(na.Files))
	}
	byName := map[string]artifactFile{}
	for _, af := range na.Files {
		byName[af.Name] = af
	}
	bigFile, ok := byName["BIG.md"]
	if !ok {
		t.Fatalf("BIG.md missing from response")
	}
	if int64(len(bigFile.Content)) != int64(len(big)) {
		t.Errorf("BIG.md content len=%d want %d (full-fidelity, no truncation)", len(bigFile.Content), len(big))
	}
	if bigFile.SizeBytes != int64(len(big)) {
		t.Errorf("BIG.md sizeBytes=%d want %d", bigFile.SizeBytes, len(big))
	}
}

// TestArtifactsAbsent: a valid node prefix with no matching files → absent.
func TestArtifactsAbsent(t *testing.T) {
	f := &fakeFetcher{
		sha:   "abc123",
		files: []gitfetch.File{{Name: "MILESTONE.md", Path: ".tide/planning/milestone/m1/MILESTONE.md", Content: []byte("x")}},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/phase/px/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "absent" {
		t.Errorf("state=%q want absent", na.State)
	}
	if na.Files == nil || len(na.Files) != 0 {
		t.Errorf("files=%v want empty non-nil", na.Files)
	}
}

// TestArtifactsError: a Fetcher error surfaces as 200 {state:"error"} and the
// PAT value never appears anywhere in the response body.
func TestArtifactsError(t *testing.T) {
	f := &fakeFetcher{fetchErr: errors.New("gitfetch clone https://example.com/repo.git: auth failed")}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "error" {
		t.Errorf("state=%q want error", na.State)
	}
	if na.Error == "" {
		t.Errorf("error message must be populated")
	}
	if bytes.Contains(body, []byte(patSentinel)) {
		t.Fatalf("PAT sentinel leaked into response body: %s", body)
	}
}

// TestArtifactsValidation: kind allowlist (400), unknown project (404), missing
// ?project= (400).
func TestArtifactsValidation(t *testing.T) {
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), &fakeFetcher{}, gitProject())

	t.Run("bad kind", func(t *testing.T) {
		resp, _ := doGet(t, router, "/api/v1/nodes/bogus/m1/artifacts?project=prj-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status=%d want 400", resp.StatusCode)
		}
	})
	t.Run("missing project", func(t *testing.T) {
		resp, _ := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status=%d want 400", resp.StatusCode)
		}
	})
	t.Run("unknown project", func(t *testing.T) {
		resp, _ := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=nope")
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("status=%d want 404", resp.StatusCode)
		}
	})
	t.Run("kind traversal still 400s (closed allowlist preserved)", func(t *testing.T) {
		resp, _ := doGet(t, router, "/api/v1/nodes/task%2F..%2Fproject/m1/artifacts?project=prj-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("status=%d want 400", resp.StatusCode)
		}
	})
	t.Run("bad kind 400 body names task in the allowed-kinds enumeration", func(t *testing.T) {
		resp, body := doGet(t, router, "/api/v1/nodes/bogus/m1/artifacts?project=prj-1")
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("status=%d want 400", resp.StatusCode)
		}
		if !strings.Contains(string(body), "task") {
			t.Errorf("400 body must name task in the allowed-kinds enumeration; got: %s", body)
		}
	})
}

// TestArtifactsTaskKindAdmitted (Finding 10 / 53-03): kind=task no longer
// 400s — it reaches the normal gitfetch-backed handling and, with no
// findings.json staged for this fixture, yields the absent-state 200 shape
// (empty node prefix, same as any other unstaged kind).
func TestArtifactsTaskKindAdmitted(t *testing.T) {
	f := &fakeFetcher{
		sha:   "abc123",
		files: []gitfetch.File{{Name: "MILESTONE.md", Path: ".tide/planning/milestone/m1/MILESTONE.md", Content: []byte("x")}},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/task/t1/artifacts?project=prj-1")
	if resp.StatusCode == http.StatusBadRequest {
		t.Fatalf("kind=task must not 400 (allowlist admits task); status=%d body=%s", resp.StatusCode, body)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "absent" {
		t.Errorf("state=%q want absent (no findings.json staged under .tide/planning/task/t1/)", na.State)
	}
}

// TestArtifactsTaskFindingsAvailable: a staged findings.json under the task
// node prefix renders through the SAME gitfetch/artifacts path as every
// other kind — no new endpoint (OBS-04).
func TestArtifactsTaskFindingsAvailable(t *testing.T) {
	f := &fakeFetcher{
		sha: "cafebabe",
		files: []gitfetch.File{
			{Name: "findings.json", Path: ".tide/planning/task/t1/findings.json", Content: []byte(`{"verdict":"BLOCKED"}`)},
		},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(credsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/task/t1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "available" {
		t.Fatalf("state=%q want available", na.State)
	}
	if len(na.Files) != 1 || na.Files[0].Name != "findings.json" {
		t.Fatalf("files=%v want exactly [findings.json]", na.Files)
	}
}

// TestArtifactsHTTPEmptyPATAvailable (Gap 37-G1): an anonymous http:// remote
// whose creds Secret carries an EMPTY GIT_PAT renders artifacts (state:available,
// NOT error) and the fetch is handed nil Auth (anonymous pass-through proven).
func TestArtifactsHTTPEmptyPATAvailable(t *testing.T) {
	f := &fakeFetcher{
		sha:   "cafed00d",
		files: []gitfetch.File{{Name: "MILESTONE.md", Path: ".tide/planning/milestone/m1/MILESTONE.md", Content: []byte("# M1\n")}},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(emptyCredsSecret()), f, httpGitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "available" {
		t.Fatalf("state=%q want available (Gap 37-G1: empty PAT on http:// must not error); error=%q", na.State, na.Error)
	}
	if !f.fetchCalled {
		t.Fatalf("fetch was never called — cannot assert anonymous pass-through")
	}
	if f.capturedAuth != nil {
		t.Errorf("Fetch auth=%+v want nil (anonymous pass-through)", f.capturedAuth)
	}
}

// TestArtifactsHTTPNoKeyPATAvailable (Gap 37-G1): an anonymous http:// remote
// whose creds Secret has NO GIT_PAT key at all → still state:available, nil Auth.
func TestArtifactsHTTPNoKeyPATAvailable(t *testing.T) {
	f := &fakeFetcher{
		sha:   "beefcafe",
		files: []gitfetch.File{{Name: "MILESTONE.md", Path: ".tide/planning/milestone/m1/MILESTONE.md", Content: []byte("# M1\n")}},
	}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(noKeyCredsSecret()), f, httpGitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "available" {
		t.Fatalf("state=%q want available (absent GIT_PAT key on http:// must not error); error=%q", na.State, na.Error)
	}
	if !f.fetchCalled || f.capturedAuth != nil {
		t.Errorf("want anonymous fetch (nil Auth); fetchCalled=%v auth=%+v", f.fetchCalled, f.capturedAuth)
	}
}

// TestArtifactsHTTPSEmptyPATError (regression guard, T-37-11-01): an https://
// remote with an empty GIT_PAT still returns state:error and the message names
// the missing data key — the relaxation is scheme-gated, not blanket.
func TestArtifactsHTTPSEmptyPATError(t *testing.T) {
	f := &fakeFetcher{sha: "abc123", files: []gitfetch.File{{Name: "x", Path: ".tide/planning/milestone/m1/x", Content: []byte("x")}}}
	router := newArtifactsHandler(t, fakeclientset.NewSimpleClientset(emptyCredsSecret()), f, gitProject())
	resp, body := doGet(t, router, "/api/v1/nodes/milestone/m1/artifacts?project=prj-1")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", resp.StatusCode, body)
	}
	var na nodeArtifacts
	if err := json.Unmarshal(body, &na); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if na.State != "error" {
		t.Fatalf("state=%q want error (https:// still REQUIRES the PAT)", na.State)
	}
	if !strings.Contains(na.Error, gitPATKey) {
		t.Errorf("error=%q must name the missing data key %q", na.Error, gitPATKey)
	}
	if f.fetchCalled {
		t.Errorf("fetch must NOT run when creds resolution errors")
	}
}
