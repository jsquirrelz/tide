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
//	TIDE_SIGNING_KEY   — base64-encoded HMAC signing secret from tide-signing-key Secret.
//	ANTHROPIC_API_KEY  — the real LLM API key from the Project's providerSecretRef.
//
// The subagent container receives only a short-lived signed token as its
// ANTHROPIC_API_KEY; this binary holds the real key and injects it on every
// approved request (Pitfall 18 prevention).
package main

import (
	"context"
	"encoding/base64"
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

	flag.StringVar(&listenAddr, "listen-addr", "127.0.0.1:8443", "Address for the HTTPS proxy listener")
	flag.StringVar(&certDir, "cert-dir", "/etc/tide/proxy", "Directory for minted TLS cert and key")
	flag.StringVar(&upstreamURL, "upstream-url", "https://api.anthropic.com", "Upstream URL to proxy requests to")
	flag.DurationVar(&certValidity, "cert-validity", 24*time.Hour, "Validity duration for the self-signed cert")
	flag.Parse()

	// Set up zap-behind-logr (same backend as cmd/manager/main.go; leaner setup
	// since credproxy does not need controller-runtime's flag bindings).
	zapCore, err := zap.NewProduction()
	if err != nil {
		fmt.Fprintf(os.Stderr, "credproxy: failed to build zap logger: %v\n", err)
		os.Exit(1)
	}
	defer zapCore.Sync() //nolint:errcheck
	log := zapr.NewLogger(zapCore).WithName("credproxy")

	// 1. Read required environment variables; fail-fast on missing.
	taskUID := requireEnv(log, "TIDE_TASK_UID")
	signingKeyB64 := requireEnv(log, "TIDE_SIGNING_KEY")
	realAPIKey := requireEnv(log, "ANTHROPIC_API_KEY")

	// 2. Decode TIDE_SIGNING_KEY from base64 (set via envFrom: secretRef).
	signingKey, err := base64.StdEncoding.DecodeString(signingKeyB64)
	if err != nil {
		log.Error(err, "failed to base64-decode TIDE_SIGNING_KEY")
		os.Exit(1)
	}

	// 3. Mint self-signed TLS cert + key into certDir.
	log.Info("minting self-signed cert", "certDir", certDir, "validity", certValidity.String())
	if err := credproxy.MintSelfSignedCert(certDir, certValidity); err != nil {
		log.Error(err, "failed to mint self-signed cert", "certDir", certDir)
		os.Exit(1)
	}

	// 4. Construct the proxy.
	p := &credproxy.Proxy{
		SigningKey:      signingKey,
		ExpectedTaskUID: taskUID,
		UpstreamBaseURL: upstreamURL,
		RealAPIKey:      realAPIKey,
		ListenAddr:      listenAddr,
		CertFile:        certDir + "/cert.pem",
		KeyFile:         certDir + "/key.pem",
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
