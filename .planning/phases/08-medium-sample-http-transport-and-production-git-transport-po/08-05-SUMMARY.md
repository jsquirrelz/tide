---
phase: 08-medium-sample-http-transport-and-production-git-transport-po
plan: "05"
subsystem: medium-sample-transport
tags: [git-http-server, anonymous-push, tide-push, scheme-conditional, kubernetes, alpine, nginx, fcgiwrap]
dependency_graph:
  requires: [08-02, 08-03, 08-04]
  provides: [git-http-server-image, medium-http-manifests, scheme-conditional-pat-guard]
  affects: [cmd/tide-push, examples/projects/medium, images/tide-git-http-server]
tech_stack:
  added:
    - alpine:3.21 + git + git-daemon + nginx + fcgiwrap + spawn-fcgi (tide-git-http-server image)
  patterns:
    - git-http-backend CGI over FastCGI (fcgiwrap + nginx, alpine-based)
    - scheme-conditional auth guard (strings.HasPrefix on origin URL from repo.Config())
    - TDD RED/GREEN: failing test → scheme-conditional implementation
key_files:
  created:
    - images/tide-git-http-server/Dockerfile
    - images/tide-git-http-server/nginx.conf
    - images/tide-git-http-server/entrypoint.sh
    - examples/projects/medium/git-http-server-deployment.yaml
    - examples/projects/medium/per-namespace-resources.yaml
  modified:
    - examples/projects/medium/project.yaml
    - cmd/tide-push/main.go
    - cmd/tide-push/main_test.go
    - .dockerignore
decisions:
  - "git-http-backend is in git-daemon package on Alpine (not git package) — verified by docker run; Dockerfile installs both git + git-daemon"
  - "nginx.conf overrides default Alpine nginx config with user nonroot (UID 1000) and pid /run/nginx/nginx.pid to support non-root execution"
  - "Invariant 2 restructured: CommitMessage check moved before PlainOpen; PAT guard placed after PlainOpen+repo.Config() to read origin URL scheme"
  - "tide-projects PVC uses ReadWriteMany (production default) with kind/minikube caveat documented"
metrics:
  duration_minutes: 60
  completed_date: "2026-06-03"
  tasks_completed: 3
  files_modified: 8
---

# Phase 08 Plan 05: Medium-Sample HTTP Transport and Scheme-Conditional PAT Guard Summary

In-cluster git-http-server image (alpine+nginx+fcgiwrap+git-http-backend) built and verified; medium sample wired to http:// in-cluster URL; tide-push GIT_PAT guard relaxed scheme-conditionally for anonymous in-cluster http:// push.

## Tasks Completed

| Task | Description | Commit | Files |
|------|-------------|--------|-------|
| 1 | tide-git-http-server image (Dockerfile, nginx.conf, entrypoint.sh) | e462925 | images/tide-git-http-server/ + .dockerignore |
| 2 | Medium manifests: git-http-server-deployment.yaml + per-namespace-resources.yaml + project.yaml update | 7fba0a1 | examples/projects/medium/ |
| 3 RED | Failing tests for scheme-conditional PAT guard | 423a953 | cmd/tide-push/main_test.go |
| 3 GREEN | Implement scheme-conditional GIT_PAT guard | 668e468 | cmd/tide-push/main.go |

## Decisions Made

1. **git-daemon package**: `git-http-backend` on Alpine:3.21 is installed by `git-daemon` package, not `git`. RESEARCH Assumption A1 was wrong about the package name; the binary path `/usr/libexec/git-core/git-http-backend` is correct. Dockerfile installs both `git` and `git-daemon`.

2. **nginx user directive**: Alpine's default nginx config sets `user nginx;` which conflicts with running as UID 1000 (nonroot). The custom nginx.conf sets `user nonroot;` and `pid /run/nginx/nginx.pid`. Dockerfile pre-creates `/run/nginx`, `/var/log/nginx`, `/var/lib/nginx` with nonroot ownership.

3. **GIT_PAT guard restructure**: CommitMessage (Invariant 3) moved before PlainOpen as a cheap pre-condition. PAT guard (Invariant 2) placed after PlainOpen so repo.Config() can read the origin URL scheme. Safe fallback: if Config() fails, requirePAT=true.

4. **TDD gate compliance**: RED commit (423a953) has a failing test; GREEN commit (668e468) makes all 9 tests pass. Gate sequence validated in git log.

5. **tide-projects PVC accessModes**: ReadWriteMany (production default). Documented kind/minikube caveat (local-path provisioner is RWO only) in the YAML comment.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] git-http-backend package is git-daemon, not git**
- **Found during:** Task 1 verification (`apk search git` + docker run verification)
- **Issue:** RESEARCH Assumption A1 stated git-http-backend is installed by the `git` package. Verified by docker run that `git-http-backend` is absent after `apk add git`; present only after `apk add git-daemon`.
- **Fix:** Dockerfile installs `git git-daemon nginx fcgiwrap spawn-fcgi`; binary path `/usr/libexec/git-core/git-http-backend` confirmed correct.
- **Files modified:** `images/tide-git-http-server/Dockerfile`
- **Commit:** e462925

**2. [Rule 2 - Missing] nginx user directive for non-root execution**
- **Found during:** Task 1 (analyzing Alpine nginx default config)
- **Issue:** Alpine's default nginx.conf sets `user nginx;` — running as UID 1000 (nonroot) requires overriding this. Without the override, nginx would try to setuid to the `nginx` user, which either fails or runs workers as wrong UID.
- **Fix:** Custom nginx.conf sets `user nonroot;`, `pid /run/nginx/nginx.pid`. Dockerfile pre-creates pid/log/tmp dirs with nonroot ownership (`chown -R nonroot:nonroot`).
- **Files modified:** `images/tide-git-http-server/nginx.conf`, `images/tide-git-http-server/Dockerfile`
- **Commit:** e462925

**3. [Rule 2 - Missing] .dockerignore exceptions for nginx.conf + entrypoint.sh**
- **Found during:** Task 1 (docker build failed with COPY not found)
- **Issue:** Repo `.dockerignore` uses `**` (exclude everything) + selective re-includes. Shell and nginx config files (`.conf`, `.sh`) are not in any re-include pattern, so COPY fails.
- **Fix:** Added `.dockerignore` exceptions for `images/tide-git-http-server/nginx.conf` and `images/tide-git-http-server/entrypoint.sh`.
- **Files modified:** `.dockerignore`
- **Commit:** e462925

## TDD Gate Compliance

| Gate | Commit | Status |
|------|--------|--------|
| RED (test commit) | 423a953 | PASS — `TestRunPushModeHTTPRemoteAcceptsEmptyPAT` fails before implementation |
| GREEN (feat commit) | 668e468 | PASS — all 9 tests pass after implementation |
| REFACTOR | N/A | No refactor needed — code was clean after GREEN |

## Verification Results

All plan verification checks passed:

1. `docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0` → exit 0 (sha256:049a4b2...)
2. `docker run --rm --entrypoint sh ... -c "which git"` → `/usr/bin/git` exit 0
3. `grep git-http-server.tide-sample-medium.svc.cluster.local project.yaml` → 3 matches (comment + targetRepo + git.repoURL)
4. `grep ClusterIP git-http-server-deployment.yaml` → 2 matches (comment + spec.type)
5. `grep tide-projects per-namespace-resources.yaml` → 3 matches
6. `go build ./cmd/tide-push/... && go vet ./cmd/tide-push/...` → exit 0
7. `grep -rn ':v1\.' examples/projects/medium/ | grep image:` → 0 results

## Threat Surface Scan

No new threat surface beyond what is documented in the plan's threat model:

| Flag | File | Description |
|------|------|-------------|
| threat_flag: anonymous-push | images/tide-git-http-server/nginx.conf | GIT_HTTP_RECEIVE_PACK=true enables anonymous push; mitigated by ClusterIP (T-08-05-01) |
| threat_flag: privilege | images/tide-git-http-server/Dockerfile | USER 1000 (nonroot); T-08-05-04 mitigated |
| threat_flag: scheme-bypass | cmd/tide-push/main.go | http:// guard relaxation; T-08-05-03 mitigated by scheme-conditional check |

## Known Stubs

None. All fields are wired. The per-namespace-resources.yaml documents that `tide-signing-key` must be manually mirrored (this is an operator step, not a stub — the signing key is cluster-unique and cannot be automated into the sample manifest).

## Self-Check: PASSED

- images/tide-git-http-server/Dockerfile: FOUND
- images/tide-git-http-server/nginx.conf: FOUND
- images/tide-git-http-server/entrypoint.sh: FOUND
- examples/projects/medium/git-http-server-deployment.yaml: FOUND
- examples/projects/medium/per-namespace-resources.yaml: FOUND
- examples/projects/medium/project.yaml (updated): FOUND
- cmd/tide-push/main.go (updated): FOUND
- Commits e462925, 7fba0a1, 423a953, 668e468: FOUND in git log
