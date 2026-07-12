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

// Package api implements the dashboard backend's REST handlers (Phase 4
// D-D2 — read-only). All handlers are HTTP GET; DASH-05's
// TestZeroMutationRoutes (cmd/dashboard/router_test.go) walks the chi
// route tree and fails the build on any non-GET registration.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
)

// ProjectsHandler serves GET /api/v1/projects + GET /api/v1/projects/{name}.
// Per D-D2 the Client is the dashboard's read-only controller-runtime
// client (informer-cache-backed); no Create/Update/Patch/Delete calls
// anywhere in this package — the threat model (T-04-D2) is mitigated by
// not having the verbs in the code path.
type ProjectsHandler struct {
	Client client.Client
	Log    logr.Logger
}

// projectCondition is the JSON shape of a single entry in the
// blockingConditions array. Mirrors taskCondition in tasks.go (same package)
// with the addition of Message — the controller-stamped string surfaced
// verbatim as the badge's native tooltip (UI-SPEC C1). The whitelist (see
// summarize) limits entries to BudgetBlocked and BillingHalt; at most 2
// entries per project keep the response size bounded per the "stripped down"
// doctrine in the projectSummary comment below.
type projectCondition struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Age     string `json:"age"`
}

// projectSummary is the JSON shape returned by the List handler — one
// entry per Project. Stripped down from the full CRD to bound response
// size and avoid surfacing transient fields (managedFields, etc.) the
// dashboard never needs.
type projectSummary struct {
	Name                 string             `json:"name"`
	Namespace            string             `json:"namespace"`
	Phase                string             `json:"phase"`
	ActiveMilestoneCount int                `json:"activeMilestoneCount"`
	Budget               budgetSummary      `json:"budget"`
	BlockingConditions   []projectCondition `json:"blockingConditions"`
}

// budgetSummary captures the three Budget fields the dashboard's status
// pills render (D-UI-SPEC budget chip). The full BudgetConfig +
// BudgetStatus pair is exposed but bounded; `withinBudget` is the
// derived predicate the UI uses.
type budgetSummary struct {
	CapCents     int64 `json:"capCents"`
	CurrentSpend int64 `json:"currentSpend"`
	WithinBudget bool  `json:"withinBudget"`
}

// projectDetail extends projectSummary with the planning-DAG children.
// The dashboard's PlanningDAGView (D-UI-SPEC §3) renders ProjectNode →
// MilestoneNode → PhaseNode → PlanNode from this single payload — one
// API round-trip per Project navigation event.
type projectDetail struct {
	projectSummary
	Milestones []childRef `json:"milestones"`
	Phases     []childRef `json:"phases"`
	Plans      []childRef `json:"plans"`
}

// childRef is the minimal subset of a child CRD the Planning DAG needs:
// the name, status.phase for the status badge, and the parent ref so
// the dagre layout can wire up the edges client-side.
type childRef struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Phase     string `json:"phase"`
	Parent    string `json:"parent"`
}

// errorBody is the JSON shape returned on every non-2xx response.
type errorBody struct {
	Error string `json:"error"`
}

// List implements GET /api/v1/projects[?namespace=foo]. Returns a JSON
// array of projectSummary; empty array (NOT 404) when no projects exist.
// Optional `namespace` query param filters; absent = all namespaces.
//
// WR-10 fix: hoists a single MilestoneList outside the projects loop and
// groups by (namespace, ProjectRef) once, replacing the previous
// O(Projects × Milestones) per-request behavior with O(Projects + Milestones).
// Same namespace filter as the ProjectList so the milestone query stays
// scoped identically. If the milestone List fails, every project still
// returns with activeMilestoneCount=0 (partial-result contract preserved).
func (h *ProjectsHandler) List(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	namespace := r.URL.Query().Get("namespace")

	var opts []client.ListOption
	if namespace != "" {
		opts = append(opts, client.InNamespace(namespace))
	}

	var projects tidev1alpha3.ProjectList
	if err := h.Client.List(ctx, &projects, opts...); err != nil {
		h.Log.Error(err, "list projects failed")
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to list projects: %s", err.Error()))
		return
	}

	// Single milestone List (scoped to the same namespace filter as the
	// projects List). On error the map stays empty and every project
	// reports activeMilestoneCount=0 — same partial-result semantics as
	// the per-project path used to have.
	activeByProject := h.activeMilestoneCountsByProject(ctx, opts)

	// Pre-allocate so an empty result still serializes as `[]`, not
	// `null` — D-UI-SPEC empty-state contract relies on the empty-array
	// distinction client-side.
	summaries := make([]projectSummary, 0, len(projects.Items))
	for i := range projects.Items {
		p := &projects.Items[i]
		count := activeByProject[projectKey(p.Namespace, p.Name)]
		summaries = append(summaries, summarize(p, count))
	}

	writeJSON(w, http.StatusOK, summaries)
}

// projectKey is the composite (namespace, name) key used by the
// activeMilestoneCountsByProject lookup. Distinct namespaces can host
// projects with colliding names without their milestone counts merging.
func projectKey(namespace, name string) string {
	return namespace + "/" + name
}

// activeMilestoneCountsByProject performs a single MilestoneList scoped
// to the given ListOptions and returns a (namespace+"/"+ProjectRef) →
// activeCount map. Active = Status.Phase is neither "Succeeded" nor
// "Failed". On List error: logs and returns an empty map (consistent
// with the previous per-project countActiveMilestones partial-result
// behavior — never 500s the parent List request).
func (h *ProjectsHandler) activeMilestoneCountsByProject(ctx context.Context, opts []client.ListOption) map[string]int {
	var ms tidev1alpha3.MilestoneList
	if err := h.Client.List(ctx, &ms, opts...); err != nil {
		h.Log.Error(err, "list milestones for active-count failed (returning empty map)")
		return map[string]int{}
	}
	out := make(map[string]int, len(ms.Items))
	for i := range ms.Items {
		m := &ms.Items[i]
		if m.Status.Phase == "Succeeded" || m.Status.Phase == "Failed" {
			continue
		}
		out[projectKey(m.Namespace, m.Spec.ProjectRef)]++
	}
	return out
}

// Get implements GET /api/v1/projects/{name}[?namespace=foo]. Returns a
// projectDetail with embedded Milestone/Phase/Plan children for the
// PlanningDAGView render. 404 with JSON body when the project doesn't
// exist; 500 with JSON body on apiserver errors.
//
// SC-7 fix: when ?namespace= is absent, performs a cross-namespace List and
// returns the first project whose Name matches. This mirrors the all-namespace
// behavior of List — so the dashboard's "click through from list to detail"
// path works for projects in any namespace (e.g. tide-sample-medium).
func (h *ProjectsHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")

	if namespace != "" {
		// Fast path: namespace explicitly provided — direct lookup.
		var p tidev1alpha3.Project
		if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &p); err != nil {
			if apierrors.IsNotFound(err) {
				writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", name))
				return
			}
			h.Log.Error(err, "get project failed", "name", name, "namespace", namespace)
			writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get project: %s", err.Error()))
			return
		}
		writeJSON(w, http.StatusOK, h.buildDetail(ctx, &p))
		return
	}

	// Cross-namespace fallback: list all projects and find the first by name.
	// This is the SC-7 fix — the dashboard's List endpoint searches all
	// namespaces; Get must behave consistently when ?namespace= is absent.
	var projects tidev1alpha3.ProjectList
	if err := h.Client.List(ctx, &projects); err != nil {
		h.Log.Error(err, "list projects for cross-namespace Get failed", "name", name)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get project: %s", err.Error()))
		return
	}
	for i := range projects.Items {
		if projects.Items[i].Name == name {
			writeJSON(w, http.StatusOK, h.buildDetail(ctx, &projects.Items[i]))
			return
		}
	}
	writeError(w, http.StatusNotFound, fmt.Sprintf("project %s not found", name))
}

// buildDetail builds the projectDetail response for a known Project p.
// All child List calls use client.InNamespace(p.Namespace) — never any outer
// namespace variable — so the result is always scoped to the project's
// actual namespace. (RESEARCH Pitfall 4: child lists must use p.Namespace.)
func (h *ProjectsHandler) buildDetail(ctx context.Context, p *tidev1alpha3.Project) projectDetail {
	count, _ := h.countActiveMilestones(ctx, p)

	detail := projectDetail{
		projectSummary: summarize(p, count),
	}

	// Milestones — filtered by Spec.ProjectRef == p.Name.
	var ms tidev1alpha3.MilestoneList
	if err := h.Client.List(ctx, &ms, client.InNamespace(p.Namespace)); err != nil {
		h.Log.Error(err, "list milestones failed", "project", p.Name)
	}
	detail.Milestones = make([]childRef, 0, len(ms.Items))
	milestoneNames := make(map[string]bool, len(ms.Items))
	for i := range ms.Items {
		m := &ms.Items[i]
		if m.Spec.ProjectRef != p.Name {
			continue
		}
		milestoneNames[m.Name] = true
		detail.Milestones = append(detail.Milestones, childRef{
			Name:      m.Name,
			Namespace: m.Namespace,
			Phase:     m.Status.Phase,
			Parent:    p.Name,
		})
	}

	// Phases — owned by Milestones via Spec.MilestoneRef.
	var phs tidev1alpha3.PhaseList
	if err := h.Client.List(ctx, &phs, client.InNamespace(p.Namespace)); err != nil {
		h.Log.Error(err, "list phases failed", "project", p.Name)
	}
	detail.Phases = make([]childRef, 0, len(phs.Items))
	phaseNames := make(map[string]bool, len(phs.Items))
	for i := range phs.Items {
		ph := &phs.Items[i]
		if !milestoneNames[ph.Spec.MilestoneRef] {
			continue
		}
		phaseNames[ph.Name] = true
		detail.Phases = append(detail.Phases, childRef{
			Name:      ph.Name,
			Namespace: ph.Namespace,
			Phase:     ph.Status.Phase,
			Parent:    ph.Spec.MilestoneRef,
		})
	}

	// Plans — owned by Phases via Spec.PhaseRef.
	var pls tidev1alpha3.PlanList
	if err := h.Client.List(ctx, &pls, client.InNamespace(p.Namespace)); err != nil {
		h.Log.Error(err, "list plans failed", "project", p.Name)
	}
	detail.Plans = make([]childRef, 0, len(pls.Items))
	for i := range pls.Items {
		pl := &pls.Items[i]
		if !phaseNames[pl.Spec.PhaseRef] {
			continue
		}
		detail.Plans = append(detail.Plans, childRef{
			Name:      pl.Name,
			Namespace: pl.Namespace,
			Phase:     pl.Status.Phase,
			Parent:    pl.Spec.PhaseRef,
		})
	}

	return detail
}

// countActiveMilestones counts milestones owned by `p` whose Status.Phase
// is neither "Succeeded" nor "Failed" — the dashboard's "active count"
// metric. Surfaces apiserver List errors so the caller can decide
// whether to 500 the request or partial-result with count=0.
func (h *ProjectsHandler) countActiveMilestones(ctx context.Context, p *tidev1alpha3.Project) (int, error) {
	var ms tidev1alpha3.MilestoneList
	if err := h.Client.List(ctx, &ms, client.InNamespace(p.Namespace)); err != nil {
		return 0, err
	}
	count := 0
	for i := range ms.Items {
		m := &ms.Items[i]
		if m.Spec.ProjectRef != p.Name {
			continue
		}
		if m.Status.Phase != "Succeeded" && m.Status.Phase != "Failed" {
			count++
		}
	}
	return count, nil
}

// summarize collapses a Project CRD into the projectSummary JSON shape.
// withinBudget is the derived predicate the UI's budget pill renders:
// false iff a positive AbsoluteCapCents is configured AND CostSpentCents
// has met or exceeded it. Zero cap = no enforcement = always within.
//
// BlockingConditions is populated by iterating p.Status.Conditions and
// keeping only entries where Type is ConditionBudgetBlocked or
// ConditionBillingHalt AND Status == ConditionTrue. Pre-allocated with
// make([]projectCondition, 0, 2) so zero matches serialize as [] not null
// (D-UI-SPEC empty-array contract). Age uses the same formatAge helper from
// tasks.go (same package). Whitelist limits exposure to exactly 2 types
// per T-14-06-01/02/03.
func summarize(p *tidev1alpha3.Project, activeMilestoneCount int) projectSummary {
	cap := p.Spec.Budget.AbsoluteCapCents
	spent := p.Status.Budget.CostSpentCents
	within := true
	if cap > 0 {
		within = spent < cap
	}

	// Whitelist: only BudgetBlocked and BillingHalt, Status==True only.
	// Pre-allocate to 2 to bound payload and ensure []-not-null serialization.
	now := time.Now()
	blocking := make([]projectCondition, 0, 2)
	for _, c := range p.Status.Conditions {
		if c.Status != metav1.ConditionTrue {
			continue
		}
		if c.Type != tidev1alpha3.ConditionBudgetBlocked && c.Type != tidev1alpha3.ConditionBillingHalt {
			continue
		}
		blocking = append(blocking, projectCondition{
			Type:    c.Type,
			Reason:  c.Reason,
			Message: c.Message,
			Age:     formatAge(now.Sub(c.LastTransitionTime.Time)),
		})
	}

	return projectSummary{
		Name:                 p.Name,
		Namespace:            p.Namespace,
		Phase:                p.Status.Phase,
		ActiveMilestoneCount: activeMilestoneCount,
		Budget: budgetSummary{
			CapCents:     cap,
			CurrentSpend: spent,
			WithinBudget: within,
		},
		BlockingConditions: blocking,
	}
}

// writeJSON serializes `v` as JSON with the application/json content type
// and the given status code. Encoder.Encode is used (not Marshal) so the
// Go stdlib's HTML escape applies — `<` `>` `&` are escaped as `<`
// etc. on the way out. T-04-D1 XSS mitigation: any Project name with
// a literal `<script>` segment is rendered as escaped Unicode, never as
// a tag.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	// Encoder.SetEscapeHTML defaults to true; do NOT disable.
	if err := enc.Encode(v); err != nil {
		// At this point status code is already flushed; best we can do
		// is log via the package-level error path — but the handler's
		// logger isn't reachable here. Fall through; client sees a
		// truncated response, which the SSE/EventSource layer will
		// surface to operators via the connection-status pill.
		_ = err
	}
}

// writeError writes a JSON error body with the given status code.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, errorBody{Error: msg})
}

// ErrInvalidNamespace is returned by handlers that detect a malformed
// namespace query param. Reserved for future input-validation needs;
// today's handlers tolerate any string and let the apiserver reject.
var ErrInvalidNamespace = errors.New("invalid namespace query parameter")
