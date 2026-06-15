//go:build eval

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

// Command tide-eval is the TIDE count_tokens pre-flight maintainer tool (EVAL-05).
//
// It renders each of the five compiled-in prompt templates with a fixed fixture
// envelope, POSTs each rendered body to the credproxy's already-allowlisted
// POST /v1/messages/count_tokens route using stdlib net/http (no Anthropic SDK),
// and prints per-template real token counts plus a 1,024-token Sonnet/Opus
// cache-floor pass/fail.
//
// The reported count is a FLOOR on billed input, not the full billed count: it
// measures the rendered template body alone (sent as a single user message with
// no system prompt). In production the template is delivered to the Claude Code
// CLI over stdin, and the CLI prepends its own system prompt and tool schemas
// before building the real request — so the true billed input is higher. Treat
// these counts as a lower bound when reasoning about cache-floor thresholds.
//
// Usage:
//
//	make eval                            # uses TIDE_PROXY_ENDPOINT + TIDE_SIGNED_TOKEN env
//	go run -tags eval ./cmd/tide-eval/  # same, or override with -proxy / -token / -model
//
// Required credentials:
//
//	TIDE_PROXY_ENDPOINT  — running credproxy base URL (e.g. https://127.0.0.1:8443)
//	TIDE_SIGNED_TOKEN    — HMAC signed token valid for the running credproxy
//
// The signed token value is NEVER logged or printed; only its presence is reported.
// The tool fails closed (exit 1) when either credential is absent.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jsquirrelz/tide/internal/subagent/common"
	pkgdispatch "github.com/jsquirrelz/tide/pkg/dispatch"
)

// cacheFloor is the minimum input_tokens required for Sonnet/Opus prompt caching.
const cacheFloor = 1024

// templates lists the five (role, level, name) tuples tide-eval iterates over.
var templates = []struct{ role, level, name string }{
	{"planner", "project", "project_planner"},
	{"planner", "milestone", "milestone_planner"},
	{"planner", "phase", "phase_planner"},
	{"planner", "plan", "plan_planner"},
	{"executor", "task", "task_executor"},
}

// baseEnvelope holds the deterministic fixture fields shared across all five
// template renders. All fields are compile-time constants; Provider.Params is
// nil to avoid map-iteration ordering non-determinism. Role and Level are set
// per-template by envelopeFor so each template's token count measures the body
// production actually dispatches it under (e.g. the executor as
// "executor"/"task"), matching the offline render_test.go fixture exactly.
var baseEnvelope = pkgdispatch.EnvelopeIn{
	APIVersion:          "tideproject.k8s/v1alpha1",
	Kind:                pkgdispatch.KindTaskEnvelopeIn,
	TaskUID:             "eval-fixture-uid-000",
	Prompt:              "EVAL FIXTURE: do not submit",
	DeclaredOutputPaths: []string{"internal/eval/testdata/placeholder.go"},
	Provider: pkgdispatch.ProviderSpec{
		Vendor: "anthropic",
		Model:  "claude-sonnet-4-6",
	},
}

// envelopeFor returns a copy of baseEnvelope with Role and Level set to the
// production dispatch shape for the template under test.
func envelopeFor(role, level string) pkgdispatch.EnvelopeIn {
	e := baseEnvelope
	e.Role = role
	e.Level = level
	return e
}

// countTokensReq is the request body for POST /v1/messages/count_tokens.
type countTokensReq struct {
	Model    string    `json:"model"`
	System   string    `json:"system,omitempty"`
	Messages []message `json:"messages"`
}

// message is a single conversation turn in the count_tokens request.
type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// countTokensResp is the response body from POST /v1/messages/count_tokens.
type countTokensResp struct {
	InputTokens int64 `json:"input_tokens"`
}

var (
	proxyEndpoint = flag.String("proxy", os.Getenv("TIDE_PROXY_ENDPOINT"), "credproxy base URL (e.g. https://127.0.0.1:8443)")
	signedToken   = flag.String("token", os.Getenv("TIDE_SIGNED_TOKEN"), "HMAC signed token for credproxy")
	model         = flag.String("model", "claude-sonnet-4-6", "model for token counting")
)

func main() {
	flag.Parse()

	proxy := requireFlag("proxy", *proxyEndpoint)
	token := requireFlag("token", *signedToken)

	// Report credential presence — NEVER log the token value (T-18-03-01).
	fmt.Fprintf(os.Stdout, "tide-eval: token present: true\n")
	fmt.Fprintf(os.Stdout, "tide-eval: proxy endpoint: %s\n", proxy)
	fmt.Fprintf(os.Stdout, "tide-eval: model: %s\n", *model)
	fmt.Fprintln(os.Stdout, "")

	// Timeout guards against a credproxy that accepts the connection but stalls
	// (hung upstream / half-open socket / stuck TLS) — without it, make eval
	// could block forever (WR-04).
	client := &http.Client{Timeout: 30 * time.Second}
	endpoint := strings.TrimRight(proxy, "/") + "/v1/messages/count_tokens"

	allPassed := true
	for _, tmplSpec := range templates {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		n, err := countTokens(ctx, client, endpoint, token, *model, tmplSpec.role, tmplSpec.level)
		cancel()
		if err != nil {
			fmt.Fprintf(os.Stderr, "tide-eval: %s: error: %v\n", tmplSpec.name, err)
			os.Exit(1)
		}

		cacheResult := "FAIL"
		if n >= cacheFloor {
			cacheResult = "PASS"
		} else {
			allPassed = false
		}
		fmt.Fprintf(os.Stdout, "%s: %d tokens — cache-floor(%d): %s\n", tmplSpec.name, n, cacheFloor, cacheResult)
	}

	fmt.Fprintln(os.Stdout, "")
	if !allPassed {
		fmt.Fprintf(os.Stderr, "tide-eval: one or more templates are below the %d-token cache floor\n", cacheFloor)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stdout, "tide-eval: all templates clear the %d-token cache floor\n", cacheFloor)
}

// countTokens renders the template for (role, level) and POSTs to the
// count_tokens endpoint, returning the reported input_tokens count. The ctx
// carries a deadline so a stalled credproxy cannot hang the call indefinitely.
func countTokens(ctx context.Context, client *http.Client, endpoint, token, modelName, role, level string) (int64, error) {
	tmpl, err := common.LoadPromptTemplate(role, level)
	if err != nil {
		return 0, fmt.Errorf("load template (%s, %s): %w", role, level, err)
	}

	var rendered bytes.Buffer
	if err := tmpl.Execute(&rendered, envelopeFor(role, level)); err != nil {
		return 0, fmt.Errorf("render template (%s, %s): %w", role, level, err)
	}

	reqBody := countTokensReq{
		Model:  modelName,
		System: "",
		Messages: []message{
			{Role: "user", Content: rendered.String()},
		},
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return 0, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	// MUST include anthropic-version header — API returns 400 without it (Pitfall 5).
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", token)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("POST %s: %w", endpoint, err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("read response body: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("POST %s: HTTP %d: %s", endpoint, resp.StatusCode, string(respBytes))
	}

	var parsed countTokensResp
	if err := json.Unmarshal(respBytes, &parsed); err != nil {
		return 0, fmt.Errorf("unmarshal response: %w", err)
	}

	return parsed.InputTokens, nil
}

// requireFlag checks that val is non-empty and returns it; exits 1 otherwise.
// Fail closed when proxy URL or signed token is absent (T-18-03-02).
func requireFlag(name, val string) string {
	if val == "" {
		fmt.Fprintf(os.Stderr, "tide-eval: required flag/env -%s not set\n", name)
		os.Exit(1)
	}
	return val
}
