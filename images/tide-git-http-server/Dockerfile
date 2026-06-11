# images/tide-git-http-server/Dockerfile — in-cluster anonymous git-http-backend server.
#
# Serves a bare git repository over HTTP (smart HTTP protocol) using
# git http-backend CGI fronted by fcgiwrap + nginx. Enables anonymous
# push/pull from within the cluster (no auth required — ClusterIP Service
# ensures only in-cluster pods can reach it).
#
# Build from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-git-http-server:1.0.0 \
#                -f images/tide-git-http-server/Dockerfile .
#
# NOTE: git-http-backend lives in the `git-daemon` package on Alpine
# (not the `git` package). Verified: `apk info -L git-daemon | grep http-backend`
# returns /usr/libexec/git-core/git-http-backend. The `git` package is also
# required for supporting git plumbing binaries.
#
# Single-stage build — no Go binary to compile; just package installation.
FROM alpine:3.21@sha256:48b0309ca019d89d40f670aa1bc06e426dc0931948452e8491e3d65087abc07d

RUN apk add --no-cache git git-daemon nginx fcgiwrap spawn-fcgi \
    && adduser -D -u 1000 nonroot \
    # Allow nonroot user to write nginx pid and log files
    && mkdir -p /run/nginx /var/log/nginx /var/lib/nginx/tmp \
    && chown -R nonroot:nonroot /run/nginx /var/log/nginx /var/lib/nginx \
    # Ensure the git repository root exists
    && mkdir -p /srv/git \
    && chown -R nonroot:nonroot /srv/git

COPY images/tide-git-http-server/nginx.conf /etc/nginx/nginx.conf
COPY --chmod=755 images/tide-git-http-server/entrypoint.sh /usr/local/bin/entrypoint.sh

EXPOSE 8080

# Run as non-root UID 1000 — matches D-G3 subagent harness UID.
# T-08-05-04 mitigation: non-root container user.
USER 1000

ENTRYPOINT ["/usr/local/bin/entrypoint.sh"]
