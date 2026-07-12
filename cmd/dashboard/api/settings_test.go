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
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/yaml"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

func newSettingsHandler(t *testing.T, objs ...runtime.Object) http.Handler {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme(client-go): %v", err)
	}
	if err := tidev1alpha3.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	h := &SettingsHandler{Client: builder.Build(), Log: testr.New(t)}
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects/{name}/settings", h.Get)
	})
	return r
}

func fullSettingsProject() *tidev1alpha3.Project {
	return &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "prj-1", Namespace: "default"},
		Spec: tidev1alpha3.ProjectSpec{
			SchemaRevision: "v1alpha3",
			TargetRepo:     "https://example.com/repo.git",
			OutcomePrompt:  "Build the thing end to end",
			Git: &tidev1alpha3.GitConfig{
				RepoURL:        "https://example.com/repo.git",
				CredsSecretRef: "git-creds",
			},
			Subagent: tidev1alpha3.SubagentConfig{
				Levels: tidev1alpha3.LevelOverrides{
					Milestone: &tidev1alpha3.LevelConfig{Model: "claude-opus"},
					Phase:     &tidev1alpha3.LevelConfig{Model: "claude-sonnet"},
					Plan:      &tidev1alpha3.LevelConfig{Model: "claude-sonnet"},
					Task:      &tidev1alpha3.LevelConfig{Model: "claude-haiku"},
				},
			},
			Budget: tidev1alpha3.BudgetConfig{AbsoluteCapCents: 10000, RollingWindowCapCents: 5000},
			Gates: tidev1alpha3.Gates{
				Milestone: "approve", Phase: "auto", Plan: "auto", Task: "auto", PauseBetweenWaves: true,
			},
			SecretRefs: tidev1alpha3.SecretRefs{
				AnthropicAPIKey: "anthropic-key", GitCredentials: "git-cred-secret",
			},
			ProviderSecretRef: "provider-secret",
		},
		Status: tidev1alpha3.ProjectStatus{
			Git:    tidev1alpha3.GitStatus{BranchName: "tide/run-prj-1-1"},
			Budget: tidev1alpha3.BudgetStatus{CostSpentCents: 250},
		},
	}
}

// TestSettingsFullyPopulated: every card field surfaces; rawSpecYAML round-trips.
func TestSettingsFullyPopulated(t *testing.T) {
	router := newSettingsHandler(t, fullSettingsProject())
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/prj-1/settings?namespace=default")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var ps projectSettings
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ps.OutcomePrompt != "Build the thing end to end" {
		t.Errorf("outcomePrompt=%q", ps.OutcomePrompt)
	}
	if ps.Repo.RepoURL != "https://example.com/repo.git" || ps.Repo.BranchName != "tide/run-prj-1-1" {
		t.Errorf("repo=%+v", ps.Repo)
	}
	if ps.Models.Milestone != "claude-opus" || ps.Models.Task != "claude-haiku" {
		t.Errorf("models=%+v", ps.Models)
	}
	if ps.Budget.AbsoluteCapCents != 10000 || ps.Budget.RollingWindowCapCents != 5000 || ps.Budget.CostSpentCents != 250 {
		t.Errorf("budget=%+v", ps.Budget)
	}
	if ps.Gates.Milestone != "approve" || ps.Gates.Task != "auto" || !ps.Gates.PauseBetweenWaves {
		t.Errorf("gates=%+v", ps.Gates)
	}
	wantSecrets := map[string]string{
		"anthropic-api-key": "anthropic-key",
		"git-credentials":   "git-cred-secret",
		"git-creds":         "git-creds",
		"provider":          "provider-secret",
	}
	if len(ps.Secrets) != len(wantSecrets) {
		t.Fatalf("secrets=%+v want %d entries", ps.Secrets, len(wantSecrets))
	}
	for _, s := range ps.Secrets {
		if wantSecrets[s.Purpose] != s.Name {
			t.Errorf("secret purpose=%q name=%q want %q", s.Purpose, s.Name, wantSecrets[s.Purpose])
		}
	}
	if ps.RawSpecYAML == "" {
		t.Fatal("rawSpecYAML empty")
	}
	var roundTrip map[string]any
	if err := yaml.Unmarshal([]byte(ps.RawSpecYAML), &roundTrip); err != nil {
		t.Errorf("rawSpecYAML does not round-trip through a YAML parser: %v", err)
	}
}

// TestSettingsRedaction: secret VALUES planted in cluster Secrets never appear
// anywhere in the raw response body (the spec carries NAMES only, by design).
func TestSettingsRedaction(t *testing.T) {
	const valueSentinel = "PLAINTEXT_SECRET_VALUE_MUST_NOT_LEAK"
	prj := fullSettingsProject()
	secrets := []runtime.Object{
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "anthropic-key", Namespace: "default"},
			Data:       map[string][]byte{"ANTHROPIC_API_KEY": []byte(valueSentinel)},
		},
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "git-creds", Namespace: "default"},
			Data:       map[string][]byte{"GIT_PAT": []byte(valueSentinel)},
		},
	}
	objs := append([]runtime.Object{prj}, secrets...)
	router := newSettingsHandler(t, objs...)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/prj-1/settings?namespace=default")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var buf bytes.Buffer
	if _, err := buf.ReadFrom(resp.Body); err != nil {
		t.Fatalf("read: %v", err)
	}
	if bytes.Contains(buf.Bytes(), []byte(valueSentinel)) {
		t.Fatalf("secret value sentinel leaked into settings response (incl. rawSpecYAML): %s", buf.String())
	}
	// Sanity: the secret NAME is expected to be present (names are not secret).
	if !bytes.Contains(buf.Bytes(), []byte("anthropic-key")) {
		t.Errorf("expected secret NAME anthropic-key to be present")
	}
}

// TestSettingsHonestDefaults: absent optionals serialize as "" (never invented);
// unknown project → 404.
func TestSettingsHonestDefaults(t *testing.T) {
	minimal := &tidev1alpha3.Project{
		ObjectMeta: metav1.ObjectMeta{Name: "bare", Namespace: "default"},
		Spec:       tidev1alpha3.ProjectSpec{SchemaRevision: "v1alpha3", TargetRepo: "https://example.com/r.git"},
	}
	router := newSettingsHandler(t, minimal)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/bare/settings?namespace=default")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d want 200", resp.StatusCode)
	}
	var ps projectSettings
	if err := json.NewDecoder(resp.Body).Decode(&ps); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if ps.Repo.BaseRef != "" {
		t.Errorf("baseRef=%q want empty (HEAD-default; field not yet in schema)", ps.Repo.BaseRef)
	}
	if ps.Repo.RepoURL != "" || ps.Repo.BranchName != "" {
		t.Errorf("git-less repo settings should be empty, got %+v", ps.Repo)
	}
	if ps.Models.Milestone != "" || ps.Gates.Task != "" {
		t.Errorf("unset models/gates should be empty strings, got models=%+v gates=%+v", ps.Models, ps.Gates)
	}
	if len(ps.Secrets) != 0 {
		t.Errorf("no secret refs → empty slice, got %+v", ps.Secrets)
	}

	resp2, err := http.Get(srv.URL + "/api/v1/projects/nope/settings?namespace=default")
	if err != nil {
		t.Fatalf("GET nope: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusNotFound {
		t.Errorf("unknown project status=%d want 404", resp2.StatusCode)
	}
}
