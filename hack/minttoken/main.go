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

// Command minttoken mints a throwaway HMAC signed token for the Phase 48
// D-06 live credproxy-TLS spike driver (make spike-langgraph-tls). It wraps
// internal/credproxy.Sign directly — no signing logic is reimplemented here.
//
// Committed (unlike the Phase-18 /tmp-only helper) because the TLS spike is
// a RETAINED, re-runnable artifact (48-CONTEXT.md "specifics": the
// SSL_CERT_FILE answer must stay a durable regression signal on any pin
// bump, not a one-time throwaway).
//
// Usage:
//
//	go run ./hack/minttoken -signing-key=<key> -task-uid=<uid> -valid-for=5m
//	TIDE_SIGNING_KEY=<key> go run ./hack/minttoken -task-uid=<uid>
//
// The signing key is read from -signing-key or TIDE_SIGNING_KEY; it is NEVER
// logged. The minted token is the ONLY thing printed, to stdout.
package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/jsquirrelz/tide/internal/credproxy"
)

func main() {
	signingKey := flag.String("signing-key", os.Getenv("TIDE_SIGNING_KEY"),
		"HMAC signing key (or TIDE_SIGNING_KEY env) — NEVER logged")
	taskUID := flag.String("task-uid", "tls-spike",
		"task UID to bind the token to (must match the credproxy's TIDE_TASK_UID)")
	validFor := flag.Duration("valid-for", 5*time.Minute, "token validity duration")
	flag.Parse()

	if *signingKey == "" {
		fmt.Fprintln(os.Stderr, "minttoken: required flag/env -signing-key not set")
		os.Exit(1)
	}

	token, err := credproxy.Sign([]byte(*signingKey), *taskUID, *validFor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "minttoken: sign failed: %v\n", err)
		os.Exit(1)
	}

	// The token is the ONLY thing printed — never the signing key.
	fmt.Println(token)
}
