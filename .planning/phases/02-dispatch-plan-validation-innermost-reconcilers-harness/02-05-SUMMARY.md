---
phase: 2
plan: 5
subsystem: credproxy
tags: [security, credential-proxy, hmac, tls, sidecar, harn-03]
dependency_graph:
  requires: ["02-01"]
  provides: ["internal/credproxy package", "cmd/credproxy binary", "images/credproxy/Dockerfile"]
  affects: ["02-08 (PodJobBackend wires sidecar)", "02-09 (TaskReconciler signs token)"]
tech_stack:
  added:
    - "github.com/go-logr/logr v1.4.3 (direct)"
    - "github.com/go-logr/zapr v1.3.0 (direct)"
    - "go.uber.org/zap v1.28.0 (direct)"
  patterns:
    - "HMAC-SHA256 signed token: nonce||expiry||mac = 56 bytes raw / 75 chars RawURLBase64"
    - "time-constant hmac.Equal (T-02-05-02)"
    - "ECDSA P-256 self-signed cert minting with 1-min clock skew tolerance"
    - "httputil.NewSingleHostReverseProxy with custom Director for header rewriting"
    - "multi-stage Dockerfile: golang:1.26-alpine builder + distroless/static:nonroot runtime"
key_files:
  created:
    - internal/credproxy/doc.go
    - internal/credproxy/token.go
    - internal/credproxy/token_test.go
    - internal/credproxy/cert.go
    - internal/credproxy/cert_test.go
    - internal/credproxy/server.go
    - internal/credproxy/server_test.go
    - cmd/credproxy/main.go
    - images/credproxy/Dockerfile
    - images/credproxy/.dockerignore
  modified:
    - go.mod (logr/zapr/zap promoted to direct)
    - .gitignore (add /credproxy root binary)
decisions:
  - "Token layout: nonce(16)||expiry(8BE)||mac(32) = 56 bytes raw; taskUID MAC-bound but not stored — wrong-UID verify returns ErrBadMAC (indistinguishable from tampered bytes without original UID)"
  - "ECDSA P-256 over RSA: faster keygen, no security concession for ephemeral 24h Job-lifetime certs"
  - "cmd/credproxy uses zapr+zap directly (not controller-runtime zap wrapper) to keep binary ~10 MB vs ~29 MB"
  - "Image base: distroless/static:nonroot (not scratch) for clean USER 1000 enforcement across OCI runtimes"
  - "Apache 2.0 header on cmd/credproxy/main.go; leaf package internal/credproxy/*.go bare package declarations"
metrics:
  duration: ~25min
  completed: "2026-05-13"
  tasks: 4
  files: 12
---

# Phase 2 Plan 5: Signed-Token Credential Proxy Summary

HMAC-SHA256 credential proxy sidecar with ECDSA P-256 self-signed cert and HTTPS reverse-proxy; subagent never sees raw `ANTHROPIC_API_KEY` (HARN-03 / Pitfall 18).

## What Was Built

### Token layout and sentinel errors

`internal/credproxy/token.go` implements Sign/Verify over a fixed 56-byte raw token:

```
[0..16)  nonce        — 16 random bytes (crypto/rand)
[16..24) expiry       — int64 unix-seconds (big-endian)
[24..56) mac          — HMAC-SHA256 over (nonce || expiry || taskUID)
```

Base64.RawURLEncoding produces a 75-character string with no padding. The `taskUID` is bound into the MAC but **not stored in the token** — cross-Pod replay is defeated because `Verify` recomputes the MAC using `expectedTaskUID` from `TIDE_TASK_UID` env. A leaked token from Pod A will fail verification against Pod B's `expectedTaskUID`.

Sentinel errors (all sentinel, all `errors.Is` compatible):
- `ErrBadTokenLength` — base64 decode failed or wrong byte count
- `ErrExpired` — unix-second expiry passed
- `ErrBadMAC` — `hmac.Equal` returned false (covers both tampered bytes AND wrong-UID cross-Pod replay, since these are indistinguishable at Verify time without the original UID)
- `ErrTaskUIDMismatch` — reserved sentinel; not returned by current Verify implementation (wrong UID produces ErrBadMAC)

The HMAC compare uses `hmac.Equal` (time-constant) per T-02-05-02 to prevent MAC oracle timing attacks.

### Self-signed cert minting

`internal/credproxy/cert.go` implements `MintSelfSignedCert(dir, validity)`:
- Algorithm: ECDSA P-256 (faster keygen than RSA; no security concession for ephemeral 24h Pod certs)
- Subject: `CN=tide-credproxy`
- SANs: `DNS:localhost`, `IP:127.0.0.1`, `IP:::1`
- `IsCA: true` so the same cert serves as both server cert and CA bundle (`ca.crt` = `cert.pem`)
- `NotBefore`: 1 minute in the past (clock-skew tolerance); `NotAfter`: `now + validity`
- Recommended validity: 24h for Job-lifetime Pods (Pitfall 11 note: rotation never needed within Pod lifetime)
- Idempotent: re-minting on every Pod start overwrites existing files cleanly

### HTTPS reverse-proxy server

`internal/credproxy/server.go` implements `Proxy.Handler()` and `Proxy.ListenAndServe(ctx)`:

**Token extraction order** (per D-C1 subagent SDK header behavior):
1. `Authorization: Bearer <token>` (strip prefix)
2. `x-api-key: <token>` (Anthropic SDK alternate form, no prefix to strip)

On `Verify` failure: HTTP 401 with body `unauthorized: <err.Error()>`.

On success, `httputil.NewSingleHostReverseProxy` `Director` rewrites:
- `Authorization: Bearer <RealAPIKey>` (replaces signed-token)
- `x-api-key: <RealAPIKey>` (replaces signed-token)
- `Host: <upstream.Host>` (required for reverse proxy)
- All other headers (including `anthropic-version`) pass through untouched.

`ListenAndServe` runs `srv.ListenAndServeTLS` in a goroutine and calls `srv.Shutdown` on ctx cancellation. TLS minimum version: TLS 1.2.

### cmd/credproxy binary

`cmd/credproxy/main.go` (Apache 2.0 header per PATTERNS.md — cmd alongside cmd/manager):

**Flags**: `--listen-addr` (default `127.0.0.1:8443`), `--cert-dir` (default `/etc/tide/proxy`), `--upstream-url` (default `https://api.anthropic.com`), `--cert-validity` (default `24h`).

**Required env vars**: `TIDE_TASK_UID`, `TIDE_SIGNING_KEY` (base64-encoded), `ANTHROPIC_API_KEY`. All required; binary exits 1 with structured error on missing.

**Signing-secret decode**: `TIDE_SIGNING_KEY` is base64-decoded (standard encoding) at startup. The Secret data key is `TIDE_SIGNING_KEY` (env-friendly naming fixed in Plan 12); `envFrom: [{secretRef: {name: tide-signing-key}}]` makes it available directly.

**Logger**: `zapr.NewLogger(zap.NewProduction())` — zap-behind-logr without pulling in controller-runtime (keeps binary ~10 MB vs ~29 MB with the full ctrl-runtime zap adapter).

**Startup order**: parse flags → read env → decode key → MintSelfSignedCert → construct Proxy → signal.NotifyContext(SIGTERM/SIGINT) → ListenAndServe(ctx).

### Dockerfile

`images/credproxy/Dockerfile` (invoked from repo root):
- Stage 1: `golang:1.26-alpine` builder; copies only `go.mod + go.sum + cmd/credproxy/ + internal/credproxy/`
- Stage 2: `gcr.io/distroless/static:nonroot`; `USER 1000`; `ENTRYPOINT ["/usr/local/bin/credproxy"]`
- Final image size: ~10 MB (well under 25 MB target)

## Plan-to-Plan Data Flow

| This plan provides | Consumer plan | Wiring |
|-------------------|--------------|--------|
| `internal/credproxy.Proxy` struct | Plan 08 (PodJobBackend) | Sidecar container spec in every Task Job PodSpec; `envFrom: secretRef` for both `tide-signing-key` and providerSecretRef |
| `internal/credproxy.Sign` function | Plan 09 (TaskReconciler) | Signs per-Task token at Job-create time with `TIDE_TASK_UID` = Task.UID and signing key from `tide-signing-key` Secret |
| `images/credproxy/Dockerfile` | Plan 12 (Helm chart) | Image referenced as `sidecar.image` Helm value; signing Secret created by chart hook |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] TamperedMAC test used invalid base64 flip strategy**
- **Found during:** Task 1 GREEN phase
- **Issue:** Original test flipped the last character of the base64 string via XOR, producing an invalid base64 character; Verify returned `ErrBadTokenLength` instead of `ErrBadMAC`
- **Fix:** Decode the token, XOR the last MAC byte in the raw bytes, re-encode — keeps valid base64 while corrupting the MAC region
- **Files modified:** `internal/credproxy/token_test.go`
- **Commit:** 0b95b5d

**2. [Rule 2 - Design clarification] ErrTaskUIDMismatch vs ErrBadMAC distinction**
- **Found during:** Task 1 implementation
- **Issue:** The plan spec shows both `ErrTaskUIDMismatch` and `ErrBadMAC` as distinct sentinel returns, but the 56-byte token layout (nonce||expiry||mac) does not embed the taskUID — the verifier cannot distinguish "wrong UID" from "tampered bytes" without knowing the original UID
- **Fix:** Both wrong-UID and tampered-bytes scenarios return `ErrBadMAC` (the MAC does not verify in both cases). `ErrTaskUIDMismatch` is declared as a sentinel for future use or for callers that construct test-only scenarios. Tests updated to assert `ErrBadMAC` for the wrong-UID scenario
- **Files modified:** `internal/credproxy/token.go`, `internal/credproxy/token_test.go`
- **Commit:** 0b95b5d

**3. [Rule 1 - Size optimization] cmd/credproxy uses zapr/zap directly**
- **Found during:** Task 4 Docker build
- **Issue:** Using `sigs.k8s.io/controller-runtime/pkg/log/zap` pulled in all of controller-runtime, producing a 26 MB binary and ~30 MB Docker image (target: < 25 MB)
- **Fix:** Import `github.com/go-logr/zapr` + `go.uber.org/zap` directly; promoted to direct deps in go.mod; binary drops to ~7.5 MB; image to ~10 MB
- **Files modified:** `cmd/credproxy/main.go`, `go.mod`
- **Commit:** 997b640

**4. [Rule 2 - Missing .gitignore entry] Root-level credproxy binary**
- **Found during:** Task 4 git status check
- **Issue:** `go build ./cmd/credproxy/...` placed a `credproxy` binary in the repo root (same pattern as `manager` and `tide-lint`)
- **Fix:** Added `/credproxy` to `.gitignore` alongside the existing `/manager` and `/tide-lint` entries
- **Files modified:** `.gitignore`
- **Commit:** 997b640

## Known Stubs

None - all fields wired; Plan 08 provides the PodSpec wiring; Plan 09 provides the per-Task token signing.

## Threat Flags

No new security-relevant surfaces beyond the plan's threat model. The proxy is the mitigation for T-02-05-01 through T-02-05-08; no additional surfaces introduced.

## Self-Check

Files created:
- internal/credproxy/doc.go — FOUND
- internal/credproxy/token.go — FOUND
- internal/credproxy/token_test.go — FOUND
- internal/credproxy/cert.go — FOUND
- internal/credproxy/cert_test.go — FOUND
- internal/credproxy/server.go — FOUND
- internal/credproxy/server_test.go — FOUND
- cmd/credproxy/main.go — FOUND
- images/credproxy/Dockerfile — FOUND
- images/credproxy/.dockerignore — FOUND

Key commits:
- 3dcdf3b — test(02-05): RED HMAC token tests
- 0b95b5d — feat(02-05): GREEN HMAC token implementation
- 11d3a4d — test(02-05): RED cert tests
- 9a2b01b — feat(02-05): GREEN cert implementation
- 681e2d8 — test(02-05): RED server tests
- 40c809f — feat(02-05): GREEN server implementation
- 997b640 — feat(02-05): cmd/credproxy + Dockerfile

Test run: `go test ./internal/credproxy/... -count=1` PASS (21 subtests)
Build: `go build ./cmd/credproxy/...` PASS
Docker: `docker build -f images/credproxy/Dockerfile .` PASS; image 9.85 MB; USER 1000

## Self-Check: PASSED
