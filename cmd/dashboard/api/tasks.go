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

// tasks.go — GET /api/v1/tasks/{name} (plan 04-17).
//
// Surfaces the rich TaskDetailData shape the SSE projection cannot carry:
// attempt + attemptMax + podName + envelopePath + conditions[]. The
// dashboard's TaskDetailDrawer.tsx calls fetchTask(name) on task-name
// change and on every SSE refresh-trigger (kind=="Task" && name===taskName).
//
// Pod resolution mirrors the heuristic in logs_sse.go (lines 271-299):
// list Pods with the canonical `tideproject.k8s/task-uid=<UID>` label.
// The TasksHandler tolerates a nil Clientset (returns PodName="") so test
// fixtures that don't need pod lookup can pass nil without rewiring.
package api

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-logr/logr"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// TasksHandler serves GET /api/v1/tasks/{name}. Clientset MAY be nil — if
// so, podName resolution short-circuits to "" (graceful degradation; the
// drawer renders "—" in the pod column rather than 500ing the request).
type TasksHandler struct {
	Client    client.Client
	Clientset kubernetes.Interface
	Log       logr.Logger
}

// taskCondition is the JSON shape carried inside taskDetail.Conditions.
// Mirrors the frontend TaskCondition type (TaskDetailDrawer.tsx) — the
// React layer expects `age` as a pre-formatted relative time string.
type taskCondition struct {
	Type   string `json:"type"`
	Reason string `json:"reason"`
	Age    string `json:"age"`
}

// taskDetail is the JSON shape returned by Get. Mirrors the frontend
// TaskDetailData type (TaskDetailDrawer.tsx) one-to-one with two notes:
//
//   - exitCode is a pointer; nil → JSON `null`, set → JSON number.
//   - elapsedText is server-formatted ("Xm Ys" / "running for Xm Ys" /
//     "finished in Xm Ys") so the React drawer never re-derives wall-clock
//     state and the test harness can pin assertions deterministically.
type taskDetail struct {
	Name         string          `json:"name"`
	ProjectName  string          `json:"projectName"`
	PlanName     string          `json:"planName"`
	Status       string          `json:"status"`
	Namespace    string          `json:"namespace"`
	Attempt      int             `json:"attempt"`
	AttemptMax   int             `json:"attemptMax"`
	PodName      string          `json:"podName"`
	ExitCode     *int            `json:"exitCode"`
	WaveIndex    int             `json:"waveIndex"`
	ScheduledAt  string          `json:"scheduledAt"`
	EnvelopePath string          `json:"envelopePath"`
	ElapsedText  string          `json:"elapsedText"`
	Conditions   []taskCondition `json:"conditions"`
	// TraceID/TraceSpanID (Phase 46 OBS-04 / D-11) carry the deep-link
	// trace identity. TraceSpanID mirrors Status.TaskTraceSpanID directly;
	// TraceID is derived from the resolved parent Project's UID. A broken
	// Task→…→Project resolution chain — or any derivation failure —
	// degrades BOTH fields to "" (never a 500; a partial trace identity
	// with no traceId to anchor it is not a usable Phoenix link).
	TraceID     string `json:"traceId,omitempty"`
	TraceSpanID string `json:"traceSpanId,omitempty"`

	// HasVerification/LoopIteration/VerifyMaxIterations/LoopExitReason/
	// LastEvaluation/LoopRunID/AttemptID (Phase 53 D-07 / OBS-04) project the
	// Task loop's current-iteration summary the CRD already holds — never an
	// iteration-history array (LOOP-03; api/v1alpha3.LoopStatus is
	// structurally guaranteed to hold none).
	//
	// HasVerification mirrors hasVerificationContract's semantics
	// (GateCommand set AND Phase=="Locked") — the drawer's Verification
	// section only renders when true. VerifyMaxIterations reads
	// Spec.Verification.MaxIterations — a DIFFERENT field from AttemptMax
	// above (which stays sourced from Caps.Iterations; the Phase-51
	// infra-vs-quality firewall holds on the wire). LoopRunID/AttemptID are
	// re-derived by the controller's exact task_controller.go:2088-2089
	// formula, never persisted, and only emitted when HasVerification is
	// true (they anchor the Verification section, not the Attempt row).
	HasVerification     bool                `json:"hasVerification,omitempty"`
	LoopIteration       int32               `json:"loopIteration,omitempty"`
	VerifyMaxIterations int32               `json:"verifyMaxIterations,omitempty"`
	LoopExitReason      string              `json:"loopExitReason,omitempty"`
	LastEvaluation      *taskLoopEvaluation `json:"lastEvaluation,omitempty"`
	LoopRunID           string              `json:"loopRunId,omitempty"`
	AttemptID           string              `json:"attemptId,omitempty"`
}

// taskLoopEvaluation is the JSON shape of taskDetail.LastEvaluation — the
// bounded current-iteration verdict summary, mirroring
// api/v1alpha3.EvaluationSummary's Decision/FindingsCount/HighSeverityCount
// trio (CompletedAt is omitted; the drawer has no use for it per UI-SPEC).
type taskLoopEvaluation struct {
	Decision          string `json:"decision"`
	FindingsCount     int32  `json:"findingsCount"`
	HighSeverityCount int32  `json:"highSeverityCount"`
}

// Get implements GET /api/v1/tasks/{name}[?namespace=foo]. Returns the
// rich TaskDetail payload the drawer renders. 404 with JSON body when
// the Task doesn't exist; 500 with JSON body on apiserver errors.
// Resolution-chain breaks (missing Plan / Phase / Milestone / Project)
// degrade gracefully — the response carries empty strings for the
// unreachable fields rather than 500ing the request.
func (h *TasksHandler) Get(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	name := chi.URLParam(r, "name")
	namespace := r.URL.Query().Get("namespace")
	if namespace == "" {
		namespace = "default"
	}

	var tk tidev1alpha3.Task
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: namespace, Name: name}, &tk); err != nil {
		if apierrors.IsNotFound(err) {
			writeError(w, http.StatusNotFound, fmt.Sprintf("task %s not found", name))
			return
		}
		h.Log.Error(err, "get task failed", "name", name, "namespace", namespace)
		writeError(w, http.StatusInternalServerError, fmt.Sprintf("failed to get task: %s", err.Error()))
		return
	}

	// Resolve the parent chain: PlanRef → Plan → PhaseRef → Phase →
	// MilestoneRef → Milestone → ProjectRef. Empty strings on any miss.
	planName, projectName := h.resolveTaskParents(ctx, &tk)

	// waveIndex: find the Wave whose Status.TaskRefs contains tk.Name and
	// whose Spec.PlanRef matches the Task's PlanRef. 0 on no match
	// (pre-materialization default — same convention as plans.go).
	waveIndex := h.resolveWaveIndex(ctx, &tk)

	// podName: tolerate nil Clientset + List errors (return "").
	podName := h.resolvePodName(ctx, &tk)

	// attemptMax: Caps.Iterations if set and >0, else 1 (the floor).
	attemptMax := 1
	if tk.Spec.Caps != nil && tk.Spec.Caps.Iterations > 0 {
		attemptMax = int(tk.Spec.Caps.Iterations)
	}

	// scheduledAt: RFC3339 of Status.StartedAt if non-nil, else "".
	scheduledAt := ""
	if tk.Status.StartedAt != nil {
		scheduledAt = tk.Status.StartedAt.UTC().Format(time.RFC3339)
	}

	// elapsedText: server-formatted Xm Ys / "running for …" / "finished in …".
	elapsedText := formatElapsed(tk.Status.StartedAt, tk.Status.CompletedAt)

	// conditions: each metav1.Condition → taskCondition with a relative-
	// time `age` string derived from cond.LastTransitionTime.
	now := time.Now()
	conds := make([]taskCondition, 0, len(tk.Status.Conditions))
	for _, c := range tk.Status.Conditions {
		conds = append(conds, taskCondition{
			Type:   c.Type,
			Reason: c.Reason,
			Age:    formatAge(now.Sub(c.LastTransitionTime.Time)),
		})
	}

	status := tk.Status.Phase
	if status == "" {
		status = tidev1alpha3.LevelPhasePending
	}

	// traceID/traceSpanID (D-11): needs the resolved Project's UID; any
	// break in the chain (empty projectName, Get error, derive error)
	// degrades both fields to "" — extends the existing graceful-
	// degradation contract, never a 500.
	traceID, traceSpanID := h.resolveTaskTraceIdentity(ctx, &tk, projectName)

	// hasVerification mirrors hasVerificationContract's semantics
	// (internal/controller/task_controller.go:2138) inline — this package
	// does not import internal/controller. LoopRunID/AttemptID use the
	// controller's exact derivation formula (task_controller.go:2088-2089)
	// and are only emitted alongside a real verification contract.
	hasVerification := tk.Spec.Verification.GateCommand != "" && tk.Spec.Verification.Phase == "Locked"
	var loopRunID, attemptID string
	if hasVerification {
		loopRunID = string(tk.UID)
		attemptID = fmt.Sprintf("%s-%d", tk.UID, tk.Status.Attempt)
	}
	var lastEvaluation *taskLoopEvaluation
	if le := tk.Status.LoopStatus.LastEvaluation; le != nil {
		lastEvaluation = &taskLoopEvaluation{
			Decision:          le.Decision,
			FindingsCount:     le.FindingsCount,
			HighSeverityCount: le.HighSeverityCount,
		}
	}

	writeJSON(w, http.StatusOK, taskDetail{
		Name:                tk.Name,
		ProjectName:         projectName,
		PlanName:            planName,
		Status:              status,
		Namespace:           tk.Namespace,
		Attempt:             tk.Status.Attempt,
		AttemptMax:          attemptMax,
		PodName:             podName,
		ExitCode:            tk.Status.ExitCode,
		WaveIndex:           waveIndex,
		ScheduledAt:         scheduledAt,
		EnvelopePath:        fmt.Sprintf("/workspace/envelopes/%s/result.json", string(tk.UID)),
		ElapsedText:         elapsedText,
		Conditions:          conds,
		TraceID:             traceID,
		TraceSpanID:         traceSpanID,
		HasVerification:     hasVerification,
		LoopIteration:       tk.Status.LoopStatus.Iteration,
		VerifyMaxIterations: tk.Spec.Verification.MaxIterations,
		LoopExitReason:      string(tk.Status.LoopStatus.ExitReason),
		LastEvaluation:      lastEvaluation,
		LoopRunID:           loopRunID,
		AttemptID:           attemptID,
	})
}

// resolveTaskTraceIdentity derives the Task's deep-link trace identity
// (Phase 46 OBS-04 / D-11). traceSpanId is the Task's own
// Status.TaskTraceSpanID, but is only meaningful paired with a traceId to
// anchor it — so any failure resolving traceId (empty projectName, Project
// Get error, or a malformed UID that TraceIDFromUID rejects) degrades BOTH
// fields to "". Mirrors resolveTaskParents' "resolution-chain breaks
// degrade gracefully" contract: never a 500.
func (h *TasksHandler) resolveTaskTraceIdentity(ctx context.Context, tk *tidev1alpha3.Task, projectName string) (string, string) {
	if projectName == "" {
		return "", ""
	}
	var proj tidev1alpha3.Project
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: tk.Namespace, Name: projectName}, &proj); err != nil {
		if !apierrors.IsNotFound(err) {
			h.Log.V(1).Info("get project for task trace identity failed", "task", tk.Name, "project", projectName, "err", err)
		}
		return "", ""
	}
	traceID, err := otelai.TraceIDFromUID(string(proj.UID))
	if err != nil {
		h.Log.V(1).Info("derive trace id for task failed", "task", tk.Name, "project", projectName, "err", err)
		return "", ""
	}
	return traceID.String(), tk.Status.TaskTraceSpanID
}

// resolveTaskParents walks Task → Plan → Phase → Milestone → Project via
// the Spec.*Ref chain. Returns (planName, projectName); either may be ""
// if the chain breaks mid-traversal — the dashboard's drawer renders "—"
// for empty fields rather than the handler 500ing the request.
func (h *TasksHandler) resolveTaskParents(ctx context.Context, tk *tidev1alpha3.Task) (string, string) {
	if tk.Spec.PlanRef == "" {
		return "", ""
	}
	var pl tidev1alpha3.Plan
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: tk.Namespace, Name: tk.Spec.PlanRef}, &pl); err != nil {
		if !apierrors.IsNotFound(err) {
			h.Log.V(1).Info("get plan for task failed", "task", tk.Name, "planRef", tk.Spec.PlanRef, "err", err)
		}
		return "", ""
	}
	planName := pl.Name

	if pl.Spec.PhaseRef == "" {
		return planName, ""
	}
	var ph tidev1alpha3.Phase
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: tk.Namespace, Name: pl.Spec.PhaseRef}, &ph); err != nil {
		if !apierrors.IsNotFound(err) {
			h.Log.V(1).Info("get phase for task failed", "task", tk.Name, "phaseRef", pl.Spec.PhaseRef, "err", err)
		}
		return planName, ""
	}

	if ph.Spec.MilestoneRef == "" {
		return planName, ""
	}
	var ms tidev1alpha3.Milestone
	if err := h.Client.Get(ctx, client.ObjectKey{Namespace: tk.Namespace, Name: ph.Spec.MilestoneRef}, &ms); err != nil {
		if !apierrors.IsNotFound(err) {
			h.Log.V(1).Info("get milestone for task failed", "task", tk.Name, "milestoneRef", ph.Spec.MilestoneRef, "err", err)
		}
		return planName, ""
	}

	return planName, ms.Spec.ProjectRef
}

// resolveWaveIndex lists Waves in the Task's namespace, filters to those
// whose Spec.PlanRef matches the Task's PlanRef, and returns the
// Spec.WaveIndex of the one whose Status.TaskRefs contains the Task name.
// Returns 0 (the pre-materialization fallback) on any miss.
func (h *TasksHandler) resolveWaveIndex(ctx context.Context, tk *tidev1alpha3.Task) int {
	if tk.Spec.PlanRef == "" {
		return 0
	}
	var waves tidev1alpha3.WaveList
	if err := h.Client.List(ctx, &waves, client.InNamespace(tk.Namespace)); err != nil {
		h.Log.V(1).Info("list waves for task failed", "task", tk.Name, "err", err)
		return 0
	}
	// v1alpha3 Waves are global-scope (no Spec.PlanRef); a Wave is associated with
	// this Task purely by TaskRefs membership, and WaveIndex is the global wave
	// position (T-23-14 / Phase 24 owns global derivation).
	for i := range waves.Items {
		wv := &waves.Items[i]
		if slices.Contains(wv.Status.TaskRefs, tk.Name) {
			return wv.Spec.WaveIndex
		}
	}
	return 0
}

// resolvePodName lists Pods with the canonical label
// `tideproject.k8s/task-uid=<UID>` via the typed kubernetes.Interface
// and returns the first item's metadata.name. Tolerates nil Clientset
// and List errors — both return "" (the drawer renders "—" for empty
// pod names, never 500s the request).
//
// Label key matches logs_sse.go's existing convention (line 75:
// `const taskUIDLabel = "tideproject.k8s/task-uid"`). Do NOT diverge —
// the controller stamps Pods with this exact key.
func (h *TasksHandler) resolvePodName(ctx context.Context, tk *tidev1alpha3.Task) string {
	if h.Clientset == nil {
		return ""
	}
	if tk.UID == "" {
		return ""
	}
	pods, err := h.Clientset.CoreV1().Pods(tk.Namespace).List(ctx, metav1.ListOptions{
		// Matches logs_sse.go:taskUIDLabel + tail.go convention.
		LabelSelector: "tideproject.k8s/task-uid=" + string(tk.UID),
	})
	if err != nil {
		h.Log.V(1).Info("list pods for task failed", "task", tk.Name, "err", err)
		return ""
	}
	if len(pods.Items) == 0 {
		return ""
	}
	return pods.Items[0].Name
}

// formatElapsed returns the human-readable duration string the dashboard
// drawer's chronograph row displays. Three shapes:
//
//   - StartedAt nil          → ""
//   - StartedAt+CompletedAt  → "finished in Xm Ys"  (rounded to whole seconds)
//   - StartedAt only         → "running for Xm Ys"  (rounded to whole seconds)
func formatElapsed(started, completed *metav1.Time) string {
	if started == nil {
		return ""
	}
	if completed != nil {
		return "finished in " + humanizeDuration(completed.Sub(started.Time))
	}
	return "running for " + humanizeDuration(time.Since(started.Time))
}

// humanizeDuration formats a duration as "Xh Ym" / "Xm Ys" / "Xs"
// depending on magnitude. Always rounds to whole seconds (no
// sub-second precision — operators don't need millisecond noise in the
// drawer chronograph).
func humanizeDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	totalSeconds := int(d.Round(time.Second).Seconds())
	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh %dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm %ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

// formatAge is the relative-time helper for Condition.LastTransitionTime
// → drawer rows. Same humanizeDuration shape, just clamped to >=0.
func formatAge(d time.Duration) string {
	return humanizeDuration(d)
}
