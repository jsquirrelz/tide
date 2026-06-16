/*
Copyright 2026 TIDE Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
*/

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr/testr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha2"
)

// newHandler returns a ProjectsHandler with a fake client populated with
// the given objects, plus a chi router mounted at /api/v1/projects... so
// tests exercise the full URL-param + query-param path.
func newHandler(t *testing.T, objs ...runtime.Object) (*ProjectsHandler, http.Handler) {
	t.Helper()
	scheme := runtime.NewScheme()
	if err := tidev1alpha1.AddToScheme(scheme); err != nil {
		t.Fatalf("AddToScheme: %v", err)
	}
	builder := fake.NewClientBuilder().WithScheme(scheme)
	for _, o := range objs {
		builder = builder.WithRuntimeObjects(o)
	}
	c := builder.Build()
	h := &ProjectsHandler{Client: c, Log: testr.New(t)}

	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) {
		r.Get("/projects", h.List)
		r.Get("/projects/{name}", h.Get)
	})
	return h, r
}

// newProject is a minimal project factory.
func newProject(name, namespace, phase string) *tidev1alpha1.Project {
	return &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec: tidev1alpha1.ProjectSpec{SchemaRevision: "v1alpha2",
			TargetRepo: "https://example.com/repo.git",
			Budget:     tidev1alpha1.BudgetConfig{AbsoluteCapCents: 10000},
		},
		Status: tidev1alpha1.ProjectStatus{
			Phase: phase,
			Budget: tidev1alpha1.BudgetStatus{
				CostSpentCents: 100,
			},
		},
	}
}

// newMilestone is a minimal milestone factory.
func newMilestone(name, namespace, projectRef, phase string) *tidev1alpha1.Milestone {
	return &tidev1alpha1.Milestone{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       tidev1alpha1.MilestoneSpec{ProjectRef: projectRef},
		Status:     tidev1alpha1.MilestoneStatus{Phase: phase},
	}
}

// newPhase + newPlan are similar minimal factories.
func newPhase(name, namespace, milestoneRef, phase string) *tidev1alpha1.Phase {
	return &tidev1alpha1.Phase{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       tidev1alpha1.PhaseSpec{MilestoneRef: milestoneRef},
		Status:     tidev1alpha1.PhaseStatus{Phase: phase},
	}
}

func newPlan(name, namespace, phaseRef, phase string) *tidev1alpha1.Plan {
	return &tidev1alpha1.Plan{
		ObjectMeta: metav1.ObjectMeta{Name: name, Namespace: namespace},
		Spec:       tidev1alpha1.PlanSpec{PhaseRef: phaseRef},
		Status:     tidev1alpha1.PlanStatus{Phase: phase},
	}
}

// TestListProjectsReturnsAll covers behavior #1: GET /api/v1/projects
// against a fake client with 3 Projects returns a JSON array of length 3.
// Each entry carries name, namespace, phase, activeMilestoneCount, budget.
func TestListProjectsReturnsAll(t *testing.T) {
	objs := []runtime.Object{
		newProject("a", "default", "Running"),
		newProject("b", "default", "Pending"),
		newProject("c", "default", "Complete"),
		// Add a Milestone for project "a" so activeMilestoneCount > 0.
		newMilestone("a-m1", "default", "a", "Running"),
	}
	_, router := newHandler(t, objs...)

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET /api/v1/projects: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json; got %q", ct)
	}

	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 projects, got %d", len(got))
	}

	// Each entry must carry the documented fields.
	for i, p := range got {
		for _, field := range []string{"name", "namespace", "phase", "activeMilestoneCount", "budget"} {
			if _, ok := p[field]; !ok {
				t.Errorf("project[%d] missing field %q (have %v)", i, field, p)
			}
		}
	}

	// Project "a" should report activeMilestoneCount=1 (the one Running
	// milestone). The other two should report 0.
	for _, p := range got {
		name := p["name"].(string)
		count := int(p["activeMilestoneCount"].(float64))
		switch name {
		case "a":
			if count != 1 {
				t.Errorf("project a: activeMilestoneCount=%d, want 1", count)
			}
		default:
			if count != 0 {
				t.Errorf("project %s: activeMilestoneCount=%d, want 0", name, count)
			}
		}
	}
}

// TestListProjectsNamespaceFilter covers behavior #2: ?namespace=foo
// filters; empty result is `[]` not null.
func TestListProjectsNamespaceFilter(t *testing.T) {
	_, router := newHandler(t,
		newProject("a", "ns-A", "Running"),
		newProject("b", "ns-B", "Running"),
	)

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects?namespace=ns-X")
	if err != nil {
		t.Fatalf("GET filtered: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	// Empty-array sentinel: the body must contain a JSON array literal.
	trim := strings.TrimSpace(string(body))
	if trim != "[]" {
		t.Errorf("empty filter result: got %q, want '[]'", trim)
	}

	// Filter to ns-A: exactly 1 project.
	resp2, err := http.Get(srv.URL + "/api/v1/projects?namespace=ns-A")
	if err != nil {
		t.Fatalf("GET ns-A: %v", err)
	}
	defer resp2.Body.Close()
	var got []map[string]any
	if err := json.NewDecoder(resp2.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "a" {
		t.Errorf("ns-A filter: got %v", got)
	}
}

// TestGetProjectWithChildren covers behavior #3: GET .../projects/{name}
// returns a single Project with embedded Milestones + Phases + Plans
// arrays for the Planning DAG render.
func TestGetProjectWithChildren(t *testing.T) {
	_, router := newHandler(t,
		newProject("alpha", "default", "Running"),
		newMilestone("m1", "default", "alpha", "Running"),
		newMilestone("m2", "default", "alpha", "Succeeded"),
		newMilestone("orphan", "default", "other-project", "Running"),
		newPhase("p1", "default", "m1", "Running"),
		newPhase("p2", "default", "m1", "Pending"),
		newPhase("orphan-phase", "default", "wrong-milestone", "Running"),
		newPlan("pl1", "default", "p1", "Running"),
		newPlan("orphan-plan", "default", "wrong-phase", "Running"),
	)

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/alpha?namespace=default")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got["name"] != "alpha" {
		t.Errorf("name=%v, want alpha", got["name"])
	}

	milestones, _ := got["milestones"].([]any)
	if len(milestones) != 2 {
		t.Errorf("milestones len=%d, want 2 (m1 + m2; orphan excluded)", len(milestones))
	}

	phases, _ := got["phases"].([]any)
	if len(phases) != 2 {
		t.Errorf("phases len=%d, want 2 (p1 + p2; orphan-phase excluded)", len(phases))
	}

	plans, _ := got["plans"].([]any)
	if len(plans) != 1 {
		t.Errorf("plans len=%d, want 1 (pl1; orphan-plan excluded)", len(plans))
	}

	// activeMilestoneCount = 1 (m1 Running; m2 Succeeded is NOT active).
	if c, _ := got["activeMilestoneCount"].(float64); int(c) != 1 {
		t.Errorf("activeMilestoneCount=%v, want 1", got["activeMilestoneCount"])
	}
}

// TestGetProjectMissingReturns404 covers behavior #4: 404 JSON body for
// a missing project.
func TestGetProjectMissingReturns404(t *testing.T) {
	_, router := newHandler(t)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects/nonexistent")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected 404, got %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.HasPrefix(ct, "application/json") {
		t.Errorf("expected application/json error body; got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body["error"] == "" {
		t.Errorf("expected non-empty error field, got %v", body)
	}
	if !strings.Contains(body["error"], "nonexistent") {
		t.Errorf("expected error to reference project name; got %q", body["error"])
	}
}

// TestResponseContentType covers behavior #6: 200 + 404 + 500 all use
// application/json; charset=utf-8.
func TestResponseContentType(t *testing.T) {
	_, router := newHandler(t, newProject("a", "default", "Running"))
	srv := httptest.NewServer(router)
	defer srv.Close()

	cases := []struct {
		name, url string
	}{
		{"list 200", "/api/v1/projects"},
		{"get 200", "/api/v1/projects/a?namespace=default"},
		{"get 404", "/api/v1/projects/missing"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp, err := http.Get(srv.URL + tc.url)
			if err != nil {
				t.Fatalf("%s: %v", tc.url, err)
			}
			defer resp.Body.Close()
			ct := resp.Header.Get("Content-Type")
			if ct != "application/json; charset=utf-8" {
				t.Errorf("%s: Content-Type=%q, want application/json; charset=utf-8", tc.name, ct)
			}
		})
	}
}

// TestXSSViaProjectName covers behavior #7 + T-04-D1 mitigation: when a
// Project's metadata.name contains `<script>...`, the JSON encoder
// escapes the angle brackets so the literal tag never appears in the
// response body (json.Encoder defaults to SetEscapeHTML=true).
func TestXSSViaProjectName(t *testing.T) {
	// kubectl wouldn't accept this name at admission, but the fake
	// client lets us assert the encoder's defense-in-depth behavior.
	xssName := "evil-name-with-script-tag"
	// Use a literal name field separately to inject the <script> via
	// JSON; the dashboard surface re-encodes via json.Encoder which
	// does the right thing.
	_, router := newHandler(t, &tidev1alpha1.Project{
		ObjectMeta: metav1.ObjectMeta{Name: xssName, Namespace: "default"},
		Spec:       tidev1alpha1.ProjectSpec{SchemaRevision: "v1alpha2", TargetRepo: "https://example.com/r.git"},
		Status:     tidev1alpha1.ProjectStatus{Phase: "<script>alert(1)</script>"},
	})

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	// json.Encoder.SetEscapeHTML defaults to true, so `<`, `>`, `&` are
	// emitted as the Unicode escape sequences `<`, `>`, `&`.
	// The literal `<script>` MUST NOT appear (browser would interpret it
	// if a downstream consumer treated the body as HTML).
	if strings.Contains(string(body), "<script>") {
		t.Errorf("XSS guard failed: literal <script> tag present in response body:\n%s",
			string(body))
	}
	// The escaped form is `<script>` — json.Encoder.SetEscapeHTML
	// emits Unicode escape sequences for `<` `>` `&`, not HTML entities.
	// The JSON body literally contains the 12 ASCII characters
	// `<script>` (backslash + u + four hex digits).
	// `<script>` in a regular Go string is the 12 ASCII characters
	// the JSON encoder writes: backslash, u, 0, 0, 3, c, s, c, r, i, p, t,
	// backslash, u, 0, 0, 3, e.
	if !strings.Contains(string(body), "\\u003cscript\\u003e") {
		t.Errorf("expected escaped \\u003cscript\\u003e sequence in body; got:\n%s",
			string(body))
	}
}

// TestBudgetSummary covers the budget field shape: capCents + currentSpend
// + withinBudget derived predicate.
func TestBudgetSummary(t *testing.T) {
	p := newProject("p1", "default", "Running")
	p.Spec.Budget.AbsoluteCapCents = 10000
	p.Status.Budget.CostSpentCents = 5000

	overP := newProject("p2", "default", "Running")
	overP.Spec.Budget.AbsoluteCapCents = 10000
	overP.Status.Budget.CostSpentCents = 12000

	uncappedP := newProject("p3", "default", "Running")
	uncappedP.Spec.Budget.AbsoluteCapCents = 0 // no cap
	uncappedP.Status.Budget.CostSpentCents = 99999

	_, router := newHandler(t, p, overP, uncappedP)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byName := make(map[string]map[string]any)
	for _, p := range got {
		byName[p["name"].(string)] = p["budget"].(map[string]any)
	}

	if w := byName["p1"]["withinBudget"]; w != true {
		t.Errorf("p1 (5000 < 10000): withinBudget=%v, want true", w)
	}
	if w := byName["p2"]["withinBudget"]; w != false {
		t.Errorf("p2 (12000 > 10000): withinBudget=%v, want false", w)
	}
	if w := byName["p3"]["withinBudget"]; w != true {
		t.Errorf("p3 (cap=0 = uncapped): withinBudget=%v, want true", w)
	}
}

// TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces covers SC-7:
// GET /api/v1/projects/{name} without a ?namespace= query param must find a
// project that lives in a non-default namespace (e.g. tide-sample-medium).
// Before the fix, the handler defaulted namespace="default" and 404'd.
// After the fix, a cross-namespace List finds the first matching project by
// name and returns its full detail with HTTP 200.
func TestGetProjectWithoutNamespaceParamFindsAcrossNamespaces(t *testing.T) {
	_, router := newHandler(t,
		newProject("medium-project", "tide-sample-medium", "Running"),
	)

	srv := httptest.NewServer(router)
	defer srv.Close()

	// No ?namespace= query param — exercises the cross-namespace fallback.
	resp, err := http.Get(srv.URL + "/api/v1/projects/medium-project")
	if err != nil {
		t.Fatalf("GET medium-project (no namespace param): %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("expected 200, got %d; body: %s", resp.StatusCode, body)
	}

	var got map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got["name"] != "medium-project" {
		t.Errorf("name=%v, want medium-project", got["name"])
	}
	if got["namespace"] != "tide-sample-medium" {
		t.Errorf("namespace=%v, want tide-sample-medium", got["namespace"])
	}
}

// TestBlockingConditionsTrueBudgetBlocked — behavior #1: a Project with a
// True BudgetBlocked condition exposes it under blockingConditions in both
// GET /api/v1/projects (list) and GET /api/v1/projects/{name} (detail).
// The entry carries type/reason/message verbatim and a non-empty age string.
func TestBlockingConditionsTrueBudgetBlocked(t *testing.T) {
	msg := "Cost spent 10100 cents (+ 220 reserved) exceeds cap 10000 cents; dispatch halted project-wide"
	p := newProject("alpha", "default", "Running")
	p.Status.Conditions = []metav1.Condition{
		{
			Type:               "BudgetBlocked",
			Status:             metav1.ConditionTrue,
			Reason:             "BudgetCapReached",
			Message:            msg,
			LastTransitionTime: metav1.Now(),
		},
	}
	_, router := newHandler(t, p)
	srv := httptest.NewServer(router)
	defer srv.Close()

	// List endpoint
	respList, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET /api/v1/projects: %v", err)
	}
	defer respList.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(respList.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 {
		t.Fatalf("expected 1 project, got %d", len(list))
	}
	bc := assertBlockingConditions(t, list[0], 1)
	assertConditionEntry(t, bc[0], "BudgetBlocked", "BudgetCapReached", msg)

	// Detail endpoint
	respGet, err := http.Get(srv.URL + "/api/v1/projects/alpha?namespace=default")
	if err != nil {
		t.Fatalf("GET /api/v1/projects/alpha: %v", err)
	}
	defer respGet.Body.Close()
	var detail map[string]any
	if err := json.NewDecoder(respGet.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	bc2 := assertBlockingConditions(t, detail, 1)
	assertConditionEntry(t, bc2[0], "BudgetBlocked", "BudgetCapReached", msg)
}

// TestBlockingConditionsFalseStatusExcluded — behavior #2: a Status=False
// BudgetBlocked condition is EXCLUDED (recovered caps disappear from payload).
func TestBlockingConditionsFalseStatusExcluded(t *testing.T) {
	p := newProject("alpha", "default", "Running")
	p.Status.Conditions = []metav1.Condition{
		{
			Type:               "BudgetBlocked",
			Status:             metav1.ConditionFalse,
			Reason:             "BudgetOK",
			Message:            "budget recovered",
			LastTransitionTime: metav1.Now(),
		},
	}
	_, router := newHandler(t, p)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertBlockingConditions(t, list[0], 0)
}

// TestBlockingConditionsNonWhitelistedTypeExcluded — behavior #3: a
// Status=True condition of a non-whitelisted type (e.g. "Ready") is EXCLUDED.
func TestBlockingConditionsNonWhitelistedTypeExcluded(t *testing.T) {
	p := newProject("alpha", "default", "Running")
	p.Status.Conditions = []metav1.Condition{
		{
			Type:               "Ready",
			Status:             metav1.ConditionTrue,
			Reason:             "AsExpected",
			Message:            "all good",
			LastTransitionTime: metav1.Now(),
		},
	}
	_, router := newHandler(t, p)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertBlockingConditions(t, list[0], 0)
}

// TestBlockingConditionsEmptyIsNotNull — behavior #4: a Project with no
// matching conditions serializes the literal JSON fragment
// "blockingConditions":[] (assert on raw body — never null).
func TestBlockingConditionsEmptyIsNotNull(t *testing.T) {
	p := newProject("alpha", "default", "Running")
	// No conditions at all.
	_, router := newHandler(t, p)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), `"blockingConditions":[]`) {
		t.Errorf("expected literal \"blockingConditions\":[] in body; got:\n%s", string(body))
	}
}

// TestBlockingConditionsBothWhitelistedTypes — behavior #5: simultaneous True
// BudgetBlocked AND True BillingHalt yields exactly 2 entries.
func TestBlockingConditionsBothWhitelistedTypes(t *testing.T) {
	p := newProject("alpha", "default", "Running")
	p.Status.Conditions = []metav1.Condition{
		{
			Type:               "BudgetBlocked",
			Status:             metav1.ConditionTrue,
			Reason:             "BudgetCapReached",
			Message:            "cap exceeded",
			LastTransitionTime: metav1.Now(),
		},
		{
			Type:               "BillingHalt",
			Status:             metav1.ConditionTrue,
			Reason:             "CreditExhausted",
			Message:            "provider credits exhausted",
			LastTransitionTime: metav1.Now(),
		},
	}
	_, router := newHandler(t, p)
	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	assertBlockingConditions(t, list[0], 2)
}

// assertBlockingConditions verifies the blockingConditions field is present,
// is an array (never null), and has the expected length. Returns the entries.
func assertBlockingConditions(t *testing.T, proj map[string]any, wantLen int) []map[string]any {
	t.Helper()
	raw, ok := proj["blockingConditions"]
	if !ok {
		t.Fatalf("missing blockingConditions field; have %v", proj)
	}
	if raw == nil {
		t.Fatal("blockingConditions is null, want []")
	}
	arr, ok := raw.([]any)
	if !ok {
		t.Fatalf("blockingConditions is not an array: %T %v", raw, raw)
	}
	if len(arr) != wantLen {
		t.Errorf("blockingConditions len=%d, want %d; entries: %v", len(arr), wantLen, arr)
	}
	out := make([]map[string]any, 0, len(arr))
	for _, e := range arr {
		m, ok := e.(map[string]any)
		if !ok {
			t.Errorf("blockingConditions entry is not a map: %T", e)
			continue
		}
		out = append(out, m)
	}
	return out
}

// assertConditionEntry checks type/reason/message are verbatim and age is non-empty.
func assertConditionEntry(t *testing.T, entry map[string]any, wantType, wantReason, wantMessage string) {
	t.Helper()
	if entry["type"] != wantType {
		t.Errorf("condition type=%v, want %q", entry["type"], wantType)
	}
	if entry["reason"] != wantReason {
		t.Errorf("condition reason=%v, want %q", entry["reason"], wantReason)
	}
	if entry["message"] != wantMessage {
		t.Errorf("condition message=%v, want %q", entry["message"], wantMessage)
	}
	if age, _ := entry["age"].(string); age == "" {
		t.Errorf("condition age is empty, want non-empty relative time string")
	}
}

// TestListProjectsActiveMilestoneCountCrossNamespace is the WR-10 regression
// test: projects in different namespaces that happen to share a name must
// NOT cross-contaminate each other's activeMilestoneCount via the hoisted
// MilestoneList. Before the fix the per-project countActiveMilestones
// listed by namespace, so name-collisions did NOT bleed; after the fix the
// activeByProject map keys on (namespace, name) — this test guards that
// keying.
func TestListProjectsActiveMilestoneCountCrossNamespace(t *testing.T) {
	_, router := newHandler(t,
		newProject("alpha", "ns-A", "Running"),
		newProject("alpha", "ns-B", "Running"),
		newMilestone("a-m-running", "ns-A", "alpha", "Running"),
		newMilestone("a-m-succeeded", "ns-A", "alpha", "Succeeded"), // excluded
		newMilestone("b-m-pending", "ns-B", "alpha", "Pending"),
		newMilestone("b-m-running", "ns-B", "alpha", "Running"),
	)

	srv := httptest.NewServer(router)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/projects")
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var got []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(got))
	}

	// Index by composite key so same-name distinct-namespace projects
	// are unambiguously addressable in assertions.
	byKey := make(map[string]int)
	for _, p := range got {
		key := p["namespace"].(string) + "/" + p["name"].(string)
		byKey[key] = int(p["activeMilestoneCount"].(float64))
	}
	if byKey["ns-A/alpha"] != 1 {
		t.Errorf("ns-A/alpha activeMilestoneCount=%d, want 1 (Running only; Succeeded excluded)", byKey["ns-A/alpha"])
	}
	if byKey["ns-B/alpha"] != 2 {
		t.Errorf("ns-B/alpha activeMilestoneCount=%d, want 2 (Pending + Running)", byKey["ns-B/alpha"])
	}
}
