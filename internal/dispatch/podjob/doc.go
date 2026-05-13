// Package podjob implements the concrete PodJobBackend dispatch backend for TIDE.
//
// # Responsibilities
//
// SUB-02: PodJobBackend is the v1 concrete implementation of [internal/dispatch.Dispatcher].
// It creates one K8s Job per Task dispatch attempt, monitors the Job to terminal state,
// and reads the result EnvelopeOut from the per-Project PVC.
//
// SUB-03: Job names are deterministic — "tide-task-{task-uid}-{attempt-n}" — so that a
// duplicate Create (caused by reconciler watch lag) returns AlreadyExists, which the
// backend treats as a success (the Job already exists from a prior create — same attempt
// number = same idempotency key). See JobName.
//
// # Phase 2 architectural choice for envelope I/O
//
// EnvelopeIn JSON is delivered to the Task pod via an init container named
// "envelope-writer" (busybox). The orchestrator passes the JSON as a base64-encoded
// environment variable; the init container decodes it and writes it to
// /workspace/envelopes/{task-uid}/in.json on the per-Project PVC slice.
//
// This approach keeps Manager write-set narrow: the Manager only reads out.json from
// /workspaces/{project-uid}/workspace/envelopes/{task-uid}/out.json; it never directly
// writes to the PVC (the init container writes in.json on the subagent side).
//
// # Shared PVC + subPath architecture (Blocker #2/#3 resolution)
//
// All Task pods share a single chart-provisioned RWX PVC named "tide-projects".
// Per-Project isolation is achieved via kubelet-enforced subPath: {project-uid}/workspace.
// The Manager pod mounts the same PVC at /workspaces (no subPath), so
// FilesystemEnvelopeReader{WorkspaceRoot: "/workspaces"} reads
// /workspaces/{project-uid}/workspace/envelopes/{task-uid}/out.json directly.
// Task pods mount with subPath so the subagent container sees /workspace as its
// per-Project root (i.e., /workspace/envelopes/{task-uid}/in.json).
//
// # Executor path vs Run method
//
// Phase 2's TaskReconciler-driven executor path does NOT call Run. Instead:
//   - TaskReconciler.ensureJob calls BuildJobSpec + client.Create (idempotent)
//   - Job terminal state is observed via Owns-watch events on batchv1.Job
//   - handleJobCompletion reads EnvelopeOut from the PVC via FilesystemEnvelopeReader
//
// Run is exposed for test fixtures and Phase 3 planner-dispatch callers, where the
// dispatch caller runs in a goroutine outside the Reconcile path. Calling Run from
// inside Reconcile would block the reconciler goroutine (Pitfall 1 — forbidden).
package podjob
