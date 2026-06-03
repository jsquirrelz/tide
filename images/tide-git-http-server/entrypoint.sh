#!/bin/sh
# entrypoint.sh — tide-git-http-server startup script.
#
# Sequence:
#   1. Ensure /run directory exists (socket dir for fcgiwrap, pid dir for nginx).
#   2. Start fcgiwrap via spawn-fcgi on a Unix socket (world-writable so the
#      nginx nonroot worker process can connect — Pitfall 5 mitigation).
#   3. Hand off to nginx (daemon off = process group leader, logs to stdout/stderr).
#
# The demo-remote-pvc is mounted at /srv/git/ by the Deployment. The init Job
# (demo-remote-init-job.yaml) MUST complete before this Deployment pod starts,
# to avoid RWO PVC mount conflicts (Pitfall 3 mitigation — apply sequence step 5:
# `kubectl wait --for=condition=Complete job/demo-remote-init` before applying
# git-http-server-deployment.yaml).

set -e

# Ensure socket and pid directories exist (they may not exist in a fresh
# container if /run is tmpfs-based). The Dockerfile pre-creates /run/nginx
# with correct ownership, but /run itself may need the fcgi.sock parent.
mkdir -p /run /run/nginx

# Start fcgiwrap via spawn-fcgi on a Unix domain socket.
# -s /run/fcgi.sock : socket path (Assumption A2 — explicit -s avoids default path guessing)
# -M 777           : socket permissions (world-writable so nginx nonroot worker can connect)
#                    (Pitfall 5 mitigation — without this, nginx gets EACCES on connect)
# /usr/bin/fcgiwrap: the fcgiwrap binary path on Alpine
spawn-fcgi -s /run/fcgi.sock -M 777 /usr/bin/fcgiwrap

# Hand off to nginx in foreground mode. Logs go to stdout/stderr as configured
# in nginx.conf (access_log /var/log/nginx/access.log; error_log to stderr).
exec nginx -g 'daemon off;'
