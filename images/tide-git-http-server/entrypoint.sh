#!/bin/sh
# entrypoint.sh — tide-git-http-server startup script.
#
# Sequence:
#   1. Ensure /run directory exists (socket dir for fcgiwrap, pid dir for nginx).
#   2. Enable anonymous receive-pack (http.receivepack=true) on every repo under
#      /srv/git so git-http-backend advertises the push service (debug #15).
#   3. Start fcgiwrap via spawn-fcgi on a Unix socket (world-writable so the
#      nginx nonroot worker process can connect — Pitfall 5 mitigation).
#   4. Hand off to nginx (daemon off = process group leader, logs to stdout/stderr).
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

# Enable anonymous push (debug #15). git-http-backend advertises and serves the
# receive-pack service ONLY when the served repo's own config has
# http.receivepack truthy — the nginx GIT_HTTP_RECEIVE_PACK fastcgi param is a
# misconception git-http-backend does not honor. Set it on every bare repo under
# /srv/git so anonymous in-cluster push works deterministically on a fresh
# deploy, regardless of how the repo was seeded (self-heals repos created by an
# older tide-demo-init that predates the seed-time config). This is a no-op for a
# repo already configured by tide-demo-init. The PVC is mounted read-write here,
# and the container UID (1000) matches the demo-init writer, so the config write
# succeeds. Failures are non-fatal: log and continue (the seed-time config in
# tide-demo-init is the primary path).
if [ -d /srv/git ]; then
    for repo in /srv/git/*.git; do
        [ -d "$repo" ] || continue
        if git --git-dir="$repo" config http.receivepack true 2>/dev/null; then
            echo "tide-git-http-server: enabled http.receivepack on $repo"
        else
            echo "tide-git-http-server: WARN could not set http.receivepack on $repo (push may be refused)" >&2
        fi
    done
fi

# Start fcgiwrap via spawn-fcgi on a Unix domain socket.
# -s /run/nginx/fcgi.sock : socket path in a nonroot-owned dir. /run itself is
#                    root-owned (mode 755), so a nonroot UID 1000 process cannot
#                    create a socket directly in /run ("spawn-fcgi: bind failed:
#                    Permission denied"). /run/nginx is chowned to nonroot in the
#                    Dockerfile, so the socket lives there.
# -M 777           : socket permissions (world-writable so nginx nonroot worker can connect)
#                    (Pitfall 5 mitigation — without this, nginx gets EACCES on connect)
# /usr/bin/fcgiwrap: the fcgiwrap binary path on Alpine
spawn-fcgi -s /run/nginx/fcgi.sock -M 777 /usr/bin/fcgiwrap

# Hand off to nginx in foreground mode. Logs go to stdout/stderr as configured
# in nginx.conf (access_log /var/log/nginx/access.log; error_log to stderr).
exec nginx -g 'daemon off;'
