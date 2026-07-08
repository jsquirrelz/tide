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
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	fakeclientset "k8s.io/client-go/kubernetes/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
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
}

func (f *fakeFetcher) Tip(_ context.Context, _, _ string, _ *gitfetch.Auth) (string, error) {
	if f.tipErr != nil {
		return "", f.tipErr
	}
	return f.sha, nil
}

func (f *fakeFetcher) Fetch(_ context.Context, _, _ string, _ *gitfetch.Auth) (string, []gitfetch.File, error) {
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
	if err := tidev1alpha2.AddToScheme(scheme); err != nil {
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
func gitProject() *tidev1alpha2.Project {
	return &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha2.ProjectSpec{
			SchemaRevision: "v1alpha2",
			TargetRepo:     "https://example.com/repo.git",
			Git: &tidev1alpha2.GitConfig{
				RepoURL:        "https://example.com/repo.git",
				CredsSecretRef: "git-creds",
			},
		},
		Status: tidev1alpha2.ProjectStatus{
			Git: tidev1alpha2.GitStatus{BranchName: "tide/run-prj-1-1"},
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
	prj := &tidev1alpha2.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec:       tidev1alpha2.ProjectSpec{SchemaRevision: "v1alpha2", TargetRepo: "https://example.com/repo.git"},
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
}
