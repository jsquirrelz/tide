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

// settings.go — GET /api/v1/projects/{name}/settings (plan 37-07, DASH-03).
//
// Serves the project settings panel: outcome prompt, curated repo/model/budget/
// gate fields, secret reference NAMES (never values — D-10 server-side
// redaction), and a server-rendered raw-spec YAML view. Secrets are surfaced by
// NAME only; this handler never reads a Secret and holds no clientset, so no
// secret value can enter any response field.
//
// DASH-05 zero-mutation contract: this handler is HTTP GET only.
package api

import (
	"fmt"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/yaml"

	tidev1alpha2 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// SettingsHandler serves GET /api/v1/projects/{name}/settings. It needs ONLY the
// read-only controller-runtime Client: every field is projected from the Project
// CR by an explicit whitelist, and secret refs are surfaced by name — there is
// deliberately no typed clientset here, so a Secret read is impossible.
type SettingsHandler struct {
	Client client.Client
	Log    logr.Logger
}

// repoSettings mirrors the TS ProjectSettings.repo shape (plan 37-05). BaseRef
// is a forward-declared field: the Phase-35 Spec.Git.BaseRef had not landed at
// 37-07 execution time, so it serializes as "" (the UI renders the HEAD-default
// label). Wire it to Spec.Git.BaseRef once that field exists.
type repoSettings struct {
	RepoURL    string `json:"repoURL"`
	BaseRef    string `json:"baseRef"`
	BranchName string `json:"branchName"`
}

// modelSettings mirrors ProjectSettings.models (per-level model identifiers).
type modelSettings struct {
	Milestone string `json:"milestone"`
	Phase     string `json:"phase"`
	Plan      string `json:"plan"`
	Task      string `json:"task"`
}

// budgetSettings mirrors ProjectSettings.budget.
type budgetSettings struct {
	AbsoluteCapCents      int64 `json:"absoluteCapCents"`
	RollingWindowCapCents int64 `json:"rollingWindowCapCents"`
	CostSpentCents        int64 `json:"costSpentCents"`
}

// gateSettings mirrors ProjectSettings.gates (per-level policy + wave pause).
type gateSettings struct {
	Milestone         string `json:"milestone"`
	Phase             string `json:"phase"`
	Plan              string `json:"plan"`
	Task              string `json:"task"`
	PauseBetweenWaves bool   `json:"pauseBetweenWaves"`
}

// secretRef is one purpose/name pair. Only the Secret NAME crosses the wire —
// values are never read (D-10).
type secretRef struct {
	Purpose string `json:"purpose"`
	Name    string `json:"name"`
}

// projectSettings mirrors the TS ProjectSettings type field-for-field (plan 37-05).
type projectSettings struct {
	OutcomePrompt string         `json:"outcomePrompt"`
	Repo          repoSettings   `json:"repo"`
	Models        modelSettings  `json:"models"`
	Budget        budgetSettings `json:"budget"`
	Gates         gateSettings   `json:"gates"`
	Secrets       []secretRef    `json:"secrets"`
	RawSpecYAML   string         `json:"rawSpecYAML"`
}

// Get implements GET /api/v1/projects/{name}/settings[?namespace=foo]. Mirrors
// projects.go's Get lookup (fast path when namespace is provided; cross-namespace
// fallback otherwise). 404 with JSON body when the project doesn't exist.
func (h *SettingsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")

	if namespace != "" {
		var p tidev1alpha2.Project
		if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &p); err != nil {
			if apierrors.IsNotFound(err) {
				writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", name))
				return
			}
			h.Log.Error(err, "get project failed", "name", name, "namespace", namespace)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get project: %s", err.Error()))
			return
		}
		h.writeSettings(w, &p)
		return
	}

	// Cross-namespace fallback — mirrors ProjectsHandler.Get (SC-7): the list
	// endpoint searches all namespaces, so settings must too when ?namespace= is absent.
	var projects tidev1alpha2.ProjectList
	if err := h.Client.List(ctx, &projects); err != nil {
		h.Log.Error(err, "list projects for cross-namespace settings failed", "name", name)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get project: %s", err.Error()))
		return
	}
	for i := range projects.Items {
		if projects.Items[i].Name == name {
			h.writeSettings(w, &projects.Items[i])
			return
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", name))
}

// writeSettings projects a Project into the curated projectSettings payload by
// EXPLICIT field whitelist (never marshaling Spec wholesale into typed fields),
// then renders the raw spec as YAML. The Spec object holds secret refs by NAME
// only, so the raw-spec view is the deliberate D-10 exception — the redaction
// test proves no secret value bytes appear (SettingsHandler holds no clientset).
func (h *SettingsHandler) writeSettings(w http.ResponseWriter, p *tidev1alpha2.Project) {
	settings := projectSettings{
		OutcomePrompt: p.Spec.OutcomePrompt,
		Repo: repoSettings{
			// BaseRef intentionally "" — Spec.Git.BaseRef not present in schema
			// at 37-07 execution time (Phase 35 field not yet landed).
			BranchName: p.Status.Git.BranchName,
		},
		Models: modelSettings{
			Milestone: p.Spec.ModelSelection.Milestone,
			Phase:     p.Spec.ModelSelection.Phase,
			Plan:      p.Spec.ModelSelection.Plan,
			Task:      p.Spec.ModelSelection.Task,
		},
		Budget: budgetSettings{
			AbsoluteCapCents:      p.Spec.Budget.AbsoluteCapCents,
			RollingWindowCapCents: p.Spec.Budget.RollingWindowCapCents,
			CostSpentCents:        p.Status.Budget.CostSpentCents,
		},
		Gates: gateSettings{
			Milestone:         string(p.Spec.Gates.Milestone),
			Phase:             string(p.Spec.Gates.Phase),
			Plan:              string(p.Spec.Gates.Plan),
			Task:              string(p.Spec.Gates.Task),
			PauseBetweenWaves: p.Spec.Gates.PauseBetweenWaves,
		},
		Secrets: buildSecretRefs(p),
	}
	// Nil-safe repo URL: Git is a pointer, elided when the Project is git-less.
	if p.Spec.Git != nil {
		settings.Repo.RepoURL = p.Spec.Git.RepoURL
	}

	// Raw spec YAML — server-rendered from the typed Spec. The k8s YAML
	// marshaler emits via JSON tags, so the output matches the CRD's on-disk shape.
	// The Spec carries only secret NAMES; no value can appear here.
	rawYAML, err := yaml.Marshal(p.Spec)
	if err != nil {
		h.Log.Error(err, "marshal spec yaml failed", "project", p.Name)
		// Non-fatal: serve the curated fields with an empty raw view rather
		// than 500ing the whole panel.
		settings.RawSpecYAML = ""
	} else {
		settings.RawSpecYAML = string(rawYAML)
	}

	writeJSON(w, http.StatusOK, settings)
}

// buildSecretRefs collects the Project's Secret references as purpose/name pairs,
// skipping empties. Pre-allocated so zero refs serialize as [] not null. NAMES
// ONLY — never a Secret read (D-10).
func buildSecretRefs(p *tidev1alpha2.Project) []secretRef {
	refs := make([]secretRef, 0, 4)
	add := func(purpose, name string) {
		if name != "" {
			refs = append(refs, secretRef{Purpose: purpose, Name: name})
		}
	}
	add("anthropic-api-key", p.Spec.SecretRefs.AnthropicAPIKey)
	add("git-credentials", p.Spec.SecretRefs.GitCredentials)
	if p.Spec.Git != nil {
		add("git-creds", p.Spec.Git.CredsSecretRef)
	}
	add("provider", p.Spec.ProviderSecretRef)
	return refs
}
