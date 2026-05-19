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

// tail.go — `tide tail <task>` cobra command + testable seam. Streams pod
// logs via the canonical pods/log subresource (Follow:true) with
// signal-aware ctx cancellation (Pitfall 25 mitigation).
//
// Architecture:
//   - tailRun: top-level seam. Resolves Task → active Pod → container, then
//     hands off to tailStreamer.
//   - tailPodPicker (func var): looks up the active Pod for the Task via the
//     canonical tideproject.k8s/task-uid label (per task_controller.go:673).
//     Function var so tests can inject without a live apiserver.
//   - tailStreamer (func var): opens the GetLogs stream and copies until
//     ctx.Done() or EOF. Function var so the ctx-cancel test can swap in a
//     deterministic blocker.
//   - pickContainer: heuristic — first non-credproxy/non-init-* container,
//     unless --container is explicit.
//
// Pitfall 25 mitigation: the streamer wires ctx.Done() to stream.Close() so
// Ctrl-C cancels within ~1s; defer stream.Close() handles the EOF path.

package main

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	tidev1alpha1 "github.com/jsquirrelz/tide/api/v1alpha1"
)

// tailOptions carries the parsed flag set for `tide tail`.
type tailOptions struct {
	container  string
	tailLines  int64
	timestamps bool
}

// tailPodPicker resolves the active Pod + container for a Task. Function var
// so tests can inject without a live apiserver. Production implementation:
// list Pods with label tideproject.k8s/task-uid=<task.UID>; pick the first
// Pod in Running/Pending phase; resolve container via pickContainer.
var tailPodPicker = defaultTailPodPicker

// tailStreamer opens the pod-log stream and copies until ctx is cancelled or
// EOF. Function var so the ctx-cancel test can swap in a deterministic
// blocker without a live apiserver.
var tailStreamer = defaultTailStreamer

// defaultTailPodPicker is the production implementation of pod resolution.
// Filters by the canonical tideproject.k8s/task-uid label (per
// task_controller.go:673) and picks the first Pod whose Status.Phase is in
// {Running, Pending}. Pending is allowed because Follow=true streams begin
// once the container is ready — operator UX matches `kubectl logs -f`.
func defaultTailPodPicker(ctx context.Context, k client.Client, ns, taskName string, opt tailOptions) (string, string, error) {
	var task tidev1alpha1.Task
	if err := k.Get(ctx, types.NamespacedName{Namespace: ns, Name: taskName}, &task); err != nil {
		if apierrors.IsNotFound(err) {
			return "", "", fmt.Errorf("tide: task %q not found in namespace %q", taskName, ns)
		}
		return "", "", fmt.Errorf("get task: %w", err)
	}

	var pods corev1.PodList
	if err := k.List(ctx, &pods,
		client.InNamespace(ns),
		client.MatchingLabels{"tideproject.k8s/task-uid": string(task.UID)},
	); err != nil {
		return "", "", fmt.Errorf("list pods for task %s: %w", taskName, err)
	}

	for i := range pods.Items {
		p := &pods.Items[i]
		switch p.Status.Phase {
		case corev1.PodRunning, corev1.PodPending:
			c := pickContainer(p.Spec.Containers, opt.container)
			if c == "" {
				return "", "", fmt.Errorf(
					"tide: task %q has pod %q with no resolvable container; pass --container",
					taskName, p.Name,
				)
			}
			return p.Name, c, nil
		}
	}
	return "", "", fmt.Errorf("tide: task %q has no running pod (status: %s)", taskName, task.Status.Phase)
}

// defaultTailStreamer opens the pods/log subresource stream with Follow=true
// and copies bytes to out until ctx is cancelled or the stream returns EOF.
//
// Pitfall 25 mitigation: a watcher goroutine waits on ctx.Done() and closes
// the stream so io.Copy returns. EOF from a terminated pod prints a stderr
// banner and exits 0 (operator UX expectation — kubectl logs -f exits 0 on
// pod-terminate too).
func defaultTailStreamer(ctx context.Context, cs kubernetes.Interface, ns, pod, container string, opt tailOptions, out, errOut io.Writer) error {
	req := cs.CoreV1().Pods(ns).GetLogs(pod, &corev1.PodLogOptions{
		Follow:     true,
		Container:  container,
		TailLines:  ptr.To(opt.tailLines),
		Timestamps: opt.timestamps,
	})
	stream, err := req.Stream(ctx)
	if err != nil {
		return fmt.Errorf("open log stream for pod/%s: %w", pod, err)
	}
	defer stream.Close()

	// Ctx-cancel watcher — closes the stream so io.Copy returns within ~1s
	// of Ctrl-C (Pitfall 25). Without this, io.Copy can block on a read
	// indefinitely even after ctx cancels.
	go func() {
		<-ctx.Done()
		_ = stream.Close()
	}()

	if _, err := io.Copy(out, stream); err != nil && ctx.Err() == nil {
		return fmt.Errorf("read log stream: %w", err)
	}
	if ctx.Err() == nil {
		// EOF without ctx-cancel = pod terminated mid-stream. UX-friendly
		// surface on stderr; exit 0.
		fmt.Fprintln(errOut, "(stream closed by pod termination)")
	}
	return nil
}

// tailRun is the testable seam. The cobra adapter resolves clientsets, picks
// the pod/container via tailPodPicker, then hands off to tailStreamer.
func tailRun(ctx context.Context, k client.Client, cs kubernetes.Interface, ns, taskName string, opt tailOptions, out, errOut io.Writer) error {
	pod, container, err := tailPodPicker(ctx, k, ns, taskName, opt)
	if err != nil {
		return err
	}
	return tailStreamer(ctx, cs, ns, pod, container, opt, out, errOut)
}

// pickContainer resolves the container the operator wants to tail. Explicit
// --container wins (operator authority). Otherwise the first container whose
// Name is NOT "credproxy" and does NOT start with "init-" — that's the
// subagent main container by Phase-1/2 convention. Returns empty string if
// no candidate is found (caller surfaces a friendly error).
func pickContainer(containers []corev1.Container, explicit string) string {
	if explicit != "" {
		return explicit
	}
	for _, c := range containers {
		if c.Name == "credproxy" {
			continue
		}
		if strings.HasPrefix(c.Name, "init-") {
			continue
		}
		return c.Name
	}
	return ""
}

// newTailCmd constructs the cobra command for `tide tail`.
func newTailCmd() *cobra.Command {
	var opt tailOptions
	c := &cobra.Command{
		Use:   "tail <task>",
		Short: "Stream pod logs for a Task",
		Long: "tide tail opens a Follow:true log stream against the Task's " +
			"currently-active Pod via the pods/log subresource. Ctrl-C cancels " +
			"cleanly. Use --container to pick a specific container; default skips " +
			"credproxy and init-* containers.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			k, err := K8sClient()
			if err != nil {
				return err
			}
			cfg, err := RESTConfig()
			if err != nil {
				return err
			}
			cs, err := kubernetes.NewForConfig(cfg)
			if err != nil {
				return fmt.Errorf("build kubernetes clientset: %w", err)
			}
			ns, err := resolveNamespace()
			if err != nil {
				return err
			}
			if ns == "" {
				ns = "default"
			}
			return tailRun(cmd.Context(), k, cs, ns, args[0], opt, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	c.Flags().StringVarP(&opt.container, "container", "c", "", "Container name to stream from (default: first non-credproxy/non-init container)")
	c.Flags().Int64Var(&opt.tailLines, "tail", 100, "Number of recent lines to print before streaming")
	c.Flags().BoolVarP(&opt.timestamps, "timestamps", "t", true, "Include timestamps on log lines")
	// metav1 is used by the production picker via the corev1.PodList paginator;
	// keep the symbol referenced to keep go vet happy across refactors.
	_ = metav1.ObjectMeta{}
	return c
}
