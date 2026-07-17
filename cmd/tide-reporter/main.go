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
// runs in one of two modes:
//
//   - Combined (materialization) mode: mounts the project-namespace PVC at
//     /workspace (subPath {project-uid}/workspace), reads out.json from
//     /workspace/envelopes/{task-uid}/out.json — a SAME-namespace read (the
//     #11/#12 fix) — resolves the parent CR by name to obtain its live UID,
//     and creates the child CRs via an in-cluster controller-runtime client
//     using the least-privilege tide-reporter SA. Before any of that, it
//     best-effort synthesizes LLM message-array spans from events.jsonl
//     (Phase 44 MSG-01) — placed before the client build so a failed
//     planner Job with no/partial out.json still emits its conversation
//     spans (D-05).
//   - Trace-only mode (--trace-only, Phase 44 MSG-01): reads events.jsonl
//     only, emits message-array spans parented under --traceparent, and
//     always exits 0 — no child-CR materialization, no K8s client built.
//
// Idempotent by the spec-ref guard in internal/reporter.ChildrenAlreadyMaterialized.
//
// Exit-code map:
//
//	0 — success: children created (or already existed — idempotent); ALWAYS
//	    for a --trace-only run regardless of synth/export outcome
//	1 — generic failure (K8s API error, unmarshal error)
//	2 — invariant violation (missing args, parent not found, allowlist rejection)
//
// A distinct, non-exit-code-affecting failure class (Phase 44 D-10): LLM
// message-array synth/export errors are logged to stderr but NEVER change
// the exit code above — in trace-only mode the run still exits 0, and in
// combined mode the materialization outcome above is authoritative.
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
	"strings"
	"syscall"
	"time"

	"go.opentelemetry.io/otel"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	tidev1alpha3 "github.com/jsquirrelz/tide/api/v1alpha3"
	"github.com/jsquirrelz/tide/internal/otelinit"
	"github.com/jsquirrelz/tide/internal/reporter"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
	"github.com/jsquirrelz/tide/pkg/otelai"
)

// newTracerProvider is a package-level seam over otelinit.NewTracerProvider
// so tests can inject a stub TracerProvider/ShutdownFunc pair — otelinit
// itself reads OTEL_EXPORTER_OTLP_ENDPOINT and sets the global provider, so
// tests cannot inject an exporter through it directly. Mirrors
// buildFakeClient's "construct only what the test needs" minimalism.
var newTracerProvider = otelinit.NewTracerProvider

// reporterConfig is the parsed CLI configuration passed by value into run()
// so tests can drive it without setting os.Args.
type reporterConfig struct {
	Workspace        string // root, default "/workspace"; out.json at <workspace>/envelopes/<taskUID>/out.json
	ProjectUID       string // informational — the PVC mount already resolves the subPath
	TaskUID          string // keys out.json under <workspace>/envelopes/<taskUID>/out.json
	ParentName       string // K8s metadata.name of the parent CR
	ParentNamespace  string // K8s namespace of the parent CR
	ParentKind       string // Kind of the parent CR: Project | Milestone | Phase | Plan
	TraceParent      string // W3C traceparent for this level's own span (consumed starting Phase 44)
	TraceOnly        bool   // Phase 44 MSG-01: synthesize LLM message-array spans only, no materialization
	SkipMessageSpans bool   // Phase 45 ADAPT-01/D-05: self-instrumenting vendor — skip synthesizeSpans entirely
	SessionID        string // 46 D-05/OBS-02: TIDE's run identity (Project UID), stamped on every emitted LLM span
	MetadataJSON     string // 46 D-05/OBS-03: manager-pre-JSON-encoded metadata, stamped opaquely (never re-marshaled)
	TagsCSV          string // 46 D-05/OBS-03: comma-separated tags, split before threading into EmitSpans
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
	traceOnly := fs.Bool("trace-only", false,
		"synthesize LLM message-array spans from events.jsonl only; no child-CR materialization (Phase 44 MSG-01)")
	skipMessageSpans := fs.Bool("skip-message-spans", false,
		"skip LLM message-array-span synthesis (self-instrumenting vendor; D-03 default-safe: absent = synthesize)")
	sessionID := fs.String("session-id", "", "TIDE's own run identity (Project UID) stamped on every emitted LLM span (46 D-05/OBS-02)")
	metadataJSON := fs.String("metadata", "", "manager-pre-JSON-encoded metadata map, stamped opaquely without re-marshaling (46 D-05/OBS-03)")
	tagsCSV := fs.String("tags", "", "comma-separated tags stamped as a native string list (46 D-05/OBS-03)")

	if err := fs.Parse(args); err != nil {
		return reporterConfig{}, err
	}

	return reporterConfig{
		Workspace:        *workspace,
		ProjectUID:       *projectUID,
		TaskUID:          *taskUID,
		ParentName:       *parentName,
		ParentNamespace:  *parentNamespace,
		ParentKind:       *parentKind,
		TraceParent:      *traceParent,
		TraceOnly:        *traceOnly,
		SkipMessageSpans: *skipMessageSpans,
		SessionID:        *sessionID,
		MetadataJSON:     *metadataJSON,
		TagsCSV:          *tagsCSV,
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
	// 0. TracerProvider lifecycle (TRACE-03/D-12) — FIRST action, before any
	// flag validation, so every subsequent exit path (including the
	// invariant-violation returns below) flushes the batch span processor.
	// D-10: otel init failure must never fail the reporter run — log and
	// continue with a no-op shutdown, mirroring the tracing-dark posture.
	tp, otelShutdown, err := newTracerProvider(ctx)
	if err != nil {
		fmt.Fprintf(stderr, "tide-reporter: otel init failed: %v\n", err)
		otelShutdown = func(context.Context) error { return nil }
	}
	_ = tp // captured by the global otel handle set inside newTracerProvider
	defer func() {
		// D-12: bounded flush timeout — a hung collector delays exit by at
		// most this bound; the drop is logged, never fails the run.
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := otelShutdown(shutdownCtx); err != nil {
			fmt.Fprintf(stderr, "tide-reporter: otel shutdown failed: %v\n", err)
		}
	}()

	// 1. Trace-only mode (Phase 44 MSG-01, D-10): only --task-uid is
	// required — no parent-CR flags, no K8s client. Always exits 0
	// regardless of synth/export outcome.
	if cfg.TraceOnly {
		if cfg.TaskUID == "" {
			fmt.Fprintf(stderr, "tide-reporter: --task-uid is required\n")
			return exitInvariant
		}
		synthesizeSpans(ctx, cfg, stderr)
		return exitSuccess
	}

	// 2. Validate required flags (combined/materialization mode).
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

	// 3. Best-effort LLM message-array synth (D-02/D-05/D-10). Runs BEFORE
	// the client build below — placement is load-bearing: a failed planner
	// Job may have no readable out.json (invariant exit further down), and
	// D-05 requires its conversation spans to still emit. synth needs no
	// K8s client and never influences the exit code returned from here on.
	synthesizeSpans(ctx, cfg, stderr)

	// 4. Build the K8s client (or use the injected test override).
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

	// 5. Read out.json from the local (same-namespace) PVC.
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

	// 6. Resolve the parent CR by name to obtain its live UID (needed for ownerRef).
	parent, exitCode := resolveParent(ctx, c, cfg, stderr)
	if exitCode != exitSuccess {
		return exitCode
	}

	// 7. Check idempotency: if children are already materialized, exit 0.
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

	// 8. Materialize children via K8s API.
	if err := reporter.MaterializeChildCRDs(ctx, c, scheme, parent, envOut.ChildCRDs); err != nil {
		fmt.Fprintf(stderr, "tide-reporter: materialize: %v\n", err)
		// allowlist rejections and spec violations are invariant (2), not generic (1).
		return exitInvariant
	}

	fmt.Fprintf(stderr, "tide-reporter: materialized %d child CRDs for %s/%s\n",
		len(envOut.ChildCRDs), cfg.ParentKind, cfg.ParentName)
	return exitSuccess
}

// splitTags splits the --tags CSV (46 D-05/OBS-03 transport) into its
// non-empty segments: "" yields nil, and empty segments from doubled or
// trailing commas ("a,,b", "a,b,") are transport artifacts, never tags.
// Tag values are manager-authored level/gate/profile enums, so no real tag
// can contain a comma (CRD enum validation enforces it upstream).
func splitTags(csv string) []string {
	var tags []string
	for _, tag := range strings.Split(csv, ",") {
		if tag != "" {
			tags = append(tags, tag)
		}
	}
	return tags
}

// synthesizeSpans is the best-effort LLM message-array-span step (Phase 44
// MSG-01). It reads events.jsonl (plus in.json for the call-1 seed prompt)
// under cfg.Workspace/envelopes/cfg.TaskUID, extracts the remote parent from
// cfg.TraceParent so emitted spans nest under the level's own AGENT span,
// and calls reporter.EmitSpans on the global tracer newTracerProvider set.
//
// NEVER influences runWithClient's exit code (D-10) — errors are logged to
// stderr and otherwise swallowed. Called from both the trace-only branch and
// the combined-mode path (before the K8s client is built), so a failed
// planner Job with no/partial out.json still gets its conversation spans
// (D-05).
//
// Reporter-synthesized span IDs mint fresh per run, so a retried pod
// (BackoffLimit=2 on the combined shape — e.g. a failed planner's missing
// out.json exits 2 AFTER synth) would re-emit the whole conversation as
// duplicate spans and multi-count llm.token_count.* costs in Phoenix. A
// sentinel file next to the artifact (envelopes/<taskUID>/.spans-emitted,
// written after a successful EmitSpans on the PVC the reporter already
// mounts read-write) makes the step idempotent across attempts. Best-effort
// per D-10: a failed sentinel read/write only logs — it never blocks
// emission or the run.
func synthesizeSpans(ctx context.Context, cfg reporterConfig, stderr io.Writer) {
	if cfg.SkipMessageSpans {
		fmt.Fprintf(stderr, "tide-reporter: self-instrumenting vendor — skipping message-span synthesis (D-05)\n")
		return
	}
	eventsPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "events.jsonl")
	inJSONPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, "in.json")
	sentinelPath := filepath.Join(cfg.Workspace, "envelopes", cfg.TaskUID, ".spans-emitted")

	if _, err := os.Stat(sentinelPath); err == nil {
		fmt.Fprintf(stderr,
			"tide-reporter: spans already emitted for task %s (sentinel present) — idempotent skip\n", cfg.TaskUID)
		return
	}

	parentCtx := otelai.ExtractRemoteParent(ctx, cfg.TraceParent)
	if cfg.TraceParent == "" {
		fmt.Fprintf(stderr, "tide-reporter: no --traceparent supplied — spans emit unparented\n")
	}

	calls, err := reporter.ReconstructConversation(eventsPath, inJSONPath, cfg.Workspace)
	if err != nil {
		// D-11 tolerant posture: a read error (e.g. one oversized line
		// tripping bufio.ErrTooLong) still yields the calls reconstructed
		// before it — emit them with the tail call marked Degraded rather
		// than discarding the whole conversation's telemetry.
		fmt.Fprintf(stderr, "tide-reporter: reconstruct conversation (partial, %d calls recovered): %v\n", len(calls), err)
		if n := len(calls); n > 0 {
			calls[n-1].Degraded = true
		}
	}
	if len(calls) == 0 {
		return
	}

	tracer := otel.Tracer("tide.reporter")
	artifactPath := "envelopes/" + cfg.TaskUID + "/events.jsonl"
	if err := reporter.EmitSpans(parentCtx, tracer, calls, artifactPath, cfg.SessionID, cfg.MetadataJSON, splitTags(cfg.TagsCSV)); err != nil {
		fmt.Fprintf(stderr, "tide-reporter: emit spans: %v\n", err)
		return
	}
	if werr := os.WriteFile(sentinelPath, []byte(time.Now().UTC().Format(time.RFC3339)+"\n"), 0o644); werr != nil {
		fmt.Fprintf(stderr, "tide-reporter: write spans-emitted sentinel: %v\n", werr)
	}
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
