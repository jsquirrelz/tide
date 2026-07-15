---
created: 2026-07-03T18:55:00.000Z
title: Dashboard "Open log stream" drawer renders empty
area: ui
resolves_phase: 37
files:
  - dashboard/
---

## Problem

Observed on the first external-repo run (2026-07-03, TIDE 1.0.6 on minikube): clicking a task node, then "Open log
stream", slides up the bottom drawer — but it stays empty. No log lines, no
error state, no "logs unavailable" message.

Likely-relevant context: by the time a human clicks a finished task, the
executor pod may already be gone (Job TTL / pod GC), so there may genuinely
be no stream to attach. But the drawer showing nothing at all is a bug
either way — an empty component is indistinguishable from a broken one.

## Solution

TBD — reproduce with (a) a running task and (b) a completed/GC'd task, and
check the SSE endpoint the drawer subscribes to (chi manager.Runnable
server) for both cases. Fix likely splits into:

- backend: verify the log-stream endpoint actually proxies pod logs for
  live pods and returns a distinguishable "pod gone" response otherwise;
- frontend: render loading / streaming / "logs no longer available (pod
  garbage-collected)" states instead of an empty drawer.
