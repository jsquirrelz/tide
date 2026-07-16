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

// Command tide-reporter is the in-namespace reader Job binary (Phase 9 Option C).
//
// The reporter Job is spawned by the Manager after a dispatch Job completes. It
// mounts the project-namespace PVC at /workspace (subPath {project-uid}/workspace),
// reads out.json from /workspace/envelopes/{task-uid}/out.json — a SAME-namespace
// read (the #11/#12 fix) — resolves the parent CR by name to obtain its live UID,
// and creates the child CRs via an in-cluster controller-runtime client using the
// least-privilege tide-reporter SA.
//
// Idempotent by the spec-ref guard in internal/reporter.ChildrenAlreadyMaterialized.
//
// Exit-code map:
//
//	0 — success: children created (or already existed — idempotent)
//	1 — generic failure (K8s API error, unmarshal error)
//	2 — invariant violation (missing args, parent not found, allowlist rejection)
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// reporterConfig is the parsed CLI configuration passed by value into run()
// so tests can drive it without setting os.Args.
type reporterConfig struct {
	Workspace       string // root, default "/workspace"; out.json at <workspace>/envelopes/<taskUID>/out.json
	ProjectUID      string // informational — the PVC mount already resolves the subPath
	TaskUID         string // keys out.json under <workspace>/envelopes/<taskUID>/out.json
	ParentName      string // K8s metadata.name of the parent CR
	ParentNamespace string // K8s namespace of the parent CR
	ParentKind      string // Kind of the parent CR: Project | Milestone | Phase | Plan
	TraceParent     string // W3C traceparent for this level's own span (consumed starting Phase 44)
}

const (
	exitSuccess = 0
	// exitGenericFail is returned for unexpected K8s API errors or unmarshal failures.
	exitGenericFail = 1
	// exitInvariant is returned for bad arguments or precondition violations
	// (missing required flag, parent not found, Kind not in allowlist).
	exitInvariant = 2
)

// parseFlags parses the reporter's CLI flags into a reporterConfig. Extracted
// from main() so flag parsing is unit-testable (Phase 43 Pitfall 4): an Args
// entry without a registered flag would crash-loop every reporter Job in the
// cluster, so the flag set and BuildReporterJob's Args must stay in sync.
// flag.ContinueOnError (not ExitOnError) lets callers observe the parse error
// instead of the process exiting mid-test.
func parseFlags(args []string) (reporterConfig, error) {
	fs := flag.NewFlagSet("tide-reporter", flag.ContinueOnError)
	workspace := fs.String("workspace", "/workspace",
		"workspace root — out.json at <workspace>/envelopes/<task-uid>/out.json")
	projectUID := fs.String("project-uid", "",
		"UID of the parent Project (informational; PVC mount already resolves the subPath)")
	taskUID := fs.String("task-uid", "", "UID of the dispatch Task — keys the out.json path")
	parentName := fs.String("parent-name", "", "metadata.name of the parent CR (Project/Milestone/Phase/Plan)")
	parentNamespace := fs.String("parent-namespace", "", "namespace of the parent CR")
	parentKind := fs.String("parent-kind", "", "Kind of the parent CR: Project | Milestone | Phase | Plan")
	traceParent := fs.String("traceparent", "", "W3C traceparent for this level's own span (consumed starting Phase 44)")

	if err := fs.Parse(args); err != nil {
		return reporterConfig{}, err
	}

	return reporterConfig{
		Workspace:       *workspace,
		ProjectUID:      *projectUID,
		TaskUID:         *taskUID,
		ParentName:      *parentName,
		ParentNamespace: *parentNamespace,
		ParentKind:      *parentKind,
		TraceParent:     *traceParent,
	}, nil
}

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintf(os.Stderr, "tide-reporter: flag parse: %v\n", err)
		os.Exit(exitInvariant)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer cancel()

	os.Exit(run(ctx, cfg, os.Stdout, os.Stderr))
}

// run is the testable in-process entry point. Tests pass a fake client via
// runWithClient; production uses nil to trigger in-cluster config resolution.
// Stdout is reserved for future structured output; stderr carries log lines.
func run(ctx context.Context, cfg reporterConfig, stdout io.Writer, stderr io.Writer) int {
	return runWithClient(ctx, cfg, stdout, stderr, nil)
}

// runWithClient is the implementation seam that accepts an optional pre-built
// client.Client. Tests pass a fake.Client; production passes nil to trigger
// in-cluster config resolution.
func runWithClient(
	ctx context.Context, cfg reporterConfig, _ io.Writer, stderr io.Writer, clientOverride client.Client,
) int {
	// 1. Validate required flags.
	if cfg.TaskUID == "" {
		fmt.Fprintf(stderr, "tide-reporter: --task-uid is required\n")
		return exitInvariant
	}
	if cfg.ParentName == "" {
		fmt.Fprintf(stderr, "tide-reporter: --parent-name is required\n")
		return exitInvariant
	}
	if cfg.ParentNamespace == "" {
		fmt.Fprintf(stderr, "tide-reporter: --parent-namespace is required\n")
		return exitInvariant
	}
	if cfg.ParentKind == "" {
		fmt.Fprintf(stderr, "tide-reporter: --parent-kind is required\n")
		return exitInvariant
	}

	// 2. Build the K8s client (or use the injected test override).
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(tidev1alpha3.AddToScheme(scheme))

	var c client.Client
	if clientOverride != nil {
		c = clientOverride
	} else {
		restCfg, err := config.GetConfig()
		if err != nil {
			fmt.Fprintf(stderr, "tide-reporter: get in-cluster config: %v\n", err)
			return exitGenericFail
		}
		var buildErr error
		c, buildErr = client.New(restCfg, client.Options{Scheme: scheme})
		if buildErr != nil {
			fmt.Fprintf(stderr, "tide-reporter: build client: %v\n", buildErr)
			return exitGenericFail
		}
	}

	// 3. Read out.json from the local (same-namespace) PVC.
	// The reader Job mounts subPath {project-uid}/workspace at /workspace so the
	// path is /workspace/envelopes/<taskUID>/out.json — same-namespace read (#11/#12 fix).
	outPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "out.json")
	data, err := os.ReadFile(outPath)
	if err != nil {
		fmt.Fprintf(stderr, "tide-reporter: read out.json %q: %v\n", outPath, err)
		return exitInvariant
	}

	var envOut pkgdispatch.EnvelopeOut
	if err := json.Unmarshal(data, &envOut); err != nil {
		fmt.Fprintf(stderr, "tide-reporter: unmarshal out.json %q: %v\n", outPath, err)
		return exitInvariant
	}

	// 4. Resolve the parent CR by name to obtain its live UID (needed for ownerRef).
	parent, exitCode := resolveParent(ctx, c, cfg, stderr)
	if exitCode != exitSuccess {
		return exitCode
	}

	// 5. Check idempotency: if children are already materialized, exit 0.
	already, err := reporter.ChildrenAlreadyMaterialized(ctx, c, parent)
	if err != nil {
		fmt.Fprintf(stderr, "tide-reporter: idempotency check: %v\n", err)
		return exitGenericFail
	}
	if already {
		fmt.Fprintf(stderr, "tide-reporter: children already materialized for %s/%s — idempotent skip\n",
			cfg.ParentKind, cfg.ParentName)
		return exitSuccess
	}

	// 6. Materialize children via K8s API.
	if err := reporter.MaterializeChildCRDs(ctx, c, scheme, parent, envOut.ChildCRDs); err != nil {
		fmt.Fprintf(stderr, "tide-reporter: materialize: %v\n", err)
		// allowlist rejections and spec violations are invariant (2), not generic (1).
		return exitInvariant
	}

	fmt.Fprintf(stderr, "tide-reporter: materialized %d child CRDs for %s/%s\n",
		len(envOut.ChildCRDs), cfg.ParentKind, cfg.ParentName)
	return exitSuccess
}

// resolveParent fetches the parent CR by {namespace, name, kind} from the K8s API
// and returns a metav1.Object. Returns (nil, exitInvariant) if the parent does
// not exist or the Kind is unsupported.
func resolveParent(ctx context.Context, c client.Client, cfg reporterConfig, stderr io.Writer) (metav1.Object, int) {
	key := client.ObjectKey{Namespace: cfg.ParentNamespace, Name: cfg.ParentName}

	switch cfg.ParentKind {
	case "Project":
		var obj tidev1alpha3.Project
		if err := c.Get(ctx, key, &obj); err != nil {
			fmt.Fprintf(stderr, "tide-reporter: get Project %q: %v\n", cfg.ParentName, err)
			return nil, exitInvariant
		}
		return &obj, exitSuccess
	case "Milestone":
		var obj tidev1alpha3.Milestone
		if err := c.Get(ctx, key, &obj); err != nil {
			fmt.Fprintf(stderr, "tide-reporter: get Milestone %q: %v\n", cfg.ParentName, err)
			return nil, exitInvariant
		}
		return &obj, exitSuccess
	case "Phase":
		var obj tidev1alpha3.Phase
		if err := c.Get(ctx, key, &obj); err != nil {
			fmt.Fprintf(stderr, "tide-reporter: get Phase %q: %v\n", cfg.ParentName, err)
			return nil, exitInvariant
		}
		return &obj, exitSuccess
	case "Plan":
		var obj tidev1alpha3.Plan
		if err := c.Get(ctx, key, &obj); err != nil {
			fmt.Fprintf(stderr, "tide-reporter: get Plan %q: %v\n", cfg.ParentName, err)
			return nil, exitInvariant
		}
		return &obj, exitSuccess
	default:
		fmt.Fprintf(stderr, "tide-reporter: unsupported parent Kind %q (want Project|Milestone|Phase|Plan)\n", cfg.ParentKind)
		return nil, exitInvariant
	}
}
