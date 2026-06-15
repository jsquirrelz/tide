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

// Command credproxy is the TIDE credential proxy sidecar (HARN-03 / D-C1).
//
// It runs as a Kubernetes native sidecar (initContainer with restartPolicy:
// Always) inside every Task Job Pod. At startup it:
//
//  1. Parses flags and required environment variables (fail-fast if missing).
//  2. Mints a self-signed TLS cert + key via MintSelfSignedCert (D-C2).
//  3. Starts an HTTPS reverse-proxy on 127.0.0.1:8443 (or --listen-addr).
//  4. Blocks on SIGTERM / SIGINT for graceful shutdown.
//
// Required environment variables:
//
//	TIDE_TASK_UID      — the UID of the Task CRD this Pod runs for.
//	TIDE_SIGNING_KEY   — HMAC signing secret from tide-signing-key Secret (the
//	                     env var is already plaintext at this point: K8s
//	                     base64-decodes `data:` values on its way to envFrom,
//	                     and the Helm template renders the data key as
//	                     `randAlphaNum 64 | b64enc`).
//	ANTHROPIC_API_KEY  — the real LLM API key from the Project's providerSecretRef.
//
// The subagent container receives only a short-lived signed token as its
// ANTHROPIC_API_KEY; this binary holds the real key and injects it on every
// approved request (Pitfall 18 prevention).
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-logr/logr"
	"github.com/go-logr/zapr"
	"go.uber.org/zap"

	"github.com/jsquirrelz/tide/internal/credproxy"
)

func main() {
	var listenAddr string
	var certDir string
	var upstreamURL string
	var certValidity time.Duration
	var teeBodyDir string

	flag.StringVar(&listenAddr, "listen-addr", "127.0.0.1:8443", "Address for the HTTPS proxy listener")
	flag.StringVar(&certDir, "cert-dir", "/etc/tide/proxy", "Directory for minted TLS cert and key")
	flag.StringVar(&upstreamURL, "upstream-url", "https://api.anthropic.com", "Upstream URL to proxy requests to")
	flag.DurationVar(&certValidity, "cert-validity", 24*time.Hour, "Validity duration for the self-signed cert")
	flag.StringVar(&teeBodyDir, "tee-body-dir", "", "Optional: directory to write teed /v1/messages request bodies (CACHE-01 FAIL-path diff; disabled when empty). Caller must create the dir with 0700 perms.")
	flag.Parse()

	// Set up zap-behind-logr (same backend as cmd/manager/main.go; leaner setup
	// since credproxy does not need controller-runtime's flag bindings).
	zapCore, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "credproxy: failed to build zap logger: %v\n", err)
		os.Exit(1)
	}
	defer zapCore.Sync() //nolint:errcheck
	//nolint:logcheck // logr idiom; klogr LoggerWithName helper not adopted in this codebase
	log := zapr.NewLogger(zapCore).WithName("credproxy")

	// 1. Read required environment variables; fail-fast on missing.
	taskUID := requireEnv(log, "TIDE_TASK_UID")
	signingKeyRaw := requireEnv(log, "TIDE_SIGNING_KEY")
	realAPIKey := requireEnv(log, "ANTHROPIC_API_KEY")

	// 2. TIDE_SIGNING_KEY is the plaintext 64-char alphanum produced by the
	// Helm `randAlphaNum 64 | b64enc` template — K8s base64-decodes Secret
	// `data:` once on its way to envFrom, so by the time we see it the value
	// is already the signing key bytes (WR-04). A second DecodeString here
	// would silently truncate the key on the first non-base64 character.
	signingKey := []byte(signingKeyRaw)
	if len(signingKey) < 32 {
		log.Error(nil, "TIDE_SIGNING_KEY too short", "len", len(signingKey), "min", 32)
		os.Exit(1)
	}

	// 2b. TIDE_ALLOWED_ROUTES (optional) carries a JSON-encoded []RouteSpec that
	// extends the credproxy upstream allowlist for this Project (Phase 04.1 P4.2).
	// Empty/unset → empty slice → only the hardcoded baseline applies.
	var extraRoutes []credproxy.RouteSpec
	if rawRoutes := os.Getenv("TIDE_ALLOWED_ROUTES"); rawRoutes != "" {
		if err := json.Unmarshal([]byte(rawRoutes), &extraRoutes); err != nil {
			log.Error(err, "failed to parse TIDE_ALLOWED_ROUTES; falling back to baseline-only allowlist")
			extraRoutes = nil
		}
	}
	log.Info("loaded ExtraAllowedRoutes", "count", len(extraRoutes), "routes", extraRoutes)

	// 3. Mint self-signed TLS cert + key into certDir.
	log.Info("minting self-signed cert", "certDir", certDir, "validity", certValidity.String())
	if err := credproxy.MintSelfSignedCert(certDir, certValidity); err != nil {
		log.Error(err, "failed to mint self-signed cert", "certDir", certDir)
		os.Exit(1)
	}

	// 4. Construct the proxy.
	p := &credproxy.Proxy{
		SigningKey:         signingKey,
		ExpectedTaskUID:    taskUID,
		UpstreamBaseURL:    upstreamURL,
		RealAPIKey:         realAPIKey,
		ListenAddr:         listenAddr,
		CertFile:           certDir + "/cert.pem",
		KeyFile:            certDir + "/key.pem",
		ExtraAllowedRoutes: extraRoutes,
		TeeBodyDir:         teeBodyDir,
	}
	if teeBodyDir != "" {
		log.Info("tee-body-dir enabled — writing /v1/messages request bodies for FAIL-path diff",
			"teeBodyDir", teeBodyDir)
	}

	// 5. Install signal handler for graceful shutdown on SIGTERM/SIGINT.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	log.Info("starting credential proxy",
		"listenAddr", listenAddr,
		"certDir", certDir,
		"upstreamURL", upstreamURL,
		"taskUID", taskUID,
	)

	// Plaintext boot banner (intentional divergence from cmd/manager/main.go's
	// structured-log-only convention). Operator-readable signal that the listener
	// bound; the literal asserted by test/integration/kind/credproxy_test.go.
	fmt.Fprintf(os.Stdout, "credproxy listening on %s\n", listenAddr)

	// 6. Block on ListenAndServe; graceful shutdown when ctx is cancelled.
	if err := p.ListenAndServe(ctx); err != nil {
		log.Error(err, "credential proxy exited with error")
		os.Exit(1)
	}
	log.Info("credential proxy shut down cleanly")
}

// requireEnv reads an environment variable and calls os.Exit(1) if it is empty.
func requireEnv(log logr.Logger, name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Error(nil, "required environment variable not set", "var", name)
		os.Exit(1)
	}
	return v
}
