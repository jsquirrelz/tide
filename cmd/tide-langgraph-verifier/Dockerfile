# syntax=docker/dockerfile:1
# Build the tide-langgraph-verifier runtime image (EVAL-01/EVAL-02, Phase 48).
# Invoked from REPO ROOT:
#   docker build -t ghcr.io/jsquirrelz/tide-langgraph-verifier:test \
#                -f cmd/tide-langgraph-verifier/Dockerfile .
#
# Single-stage: no Go build stage needed (unlike images/claude-subagent/Dockerfile)
# — this image is pure Python, decoding envelopes and running the LangGraph
# agent directly; it never imports pkg/dispatch (import-firewalled to Go, D-03).
#
# Base digest re-resolved live (docker pull python:3.13-slim-bookworm) same day
# as RESEARCH.md's citation — zero drift in this window. Re-verify at actual
# build time per D-10's "1.x moves ~weekly" discipline; this is the BASE image,
# not the pinned Python packages, but it is rebuilt periodically upstream too.
FROM python:3.13-slim-bookworm@sha256:9d7f287598e1a5a978c015ee176d8216435aaf335ed69ac3c38dd1bbb10e8d64

RUN apt-get update \
    && apt-get install -y --no-install-recommends git \
    && rm -rf /var/lib/apt/lists/* \
    # git 2.35+ refuses to operate inside a repo it doesn't consider itself the
    # owner of ("detected dubious ownership") the moment container uid mapping
    # differs subtly from the mounting host's. The pod is the trust boundary
    # (hermetic, single-purpose) — mark all repos safe at the system level,
    # mirroring images/claude-subagent/Dockerfile. Covers the git_read tool's
    # subprocess shell-outs against the already-materialized Task worktree.
    && git config --system --add safe.directory '*'

WORKDIR /app

# Layer 1 of two independent supply-chain enforcement layers (RESEARCH §Code
# Examples): `make verify-langgraph-pins` greps requirements.in for a careless
# range/unpinned specifier edit; `--require-hashes` here is pip's OWN
# enforcement that installing from the hash-locked requirements.txt cannot
# silently accept a substituted wheel — a distinct failure class from the
# grep gate, closed at build time regardless of what edited requirements.in.
COPY cmd/tide-langgraph-verifier/requirements.txt .
RUN pip install --no-cache-dir --require-hashes -r requirements.txt

# Runtime source only — never requirements-dev.txt/requirements-dev.in/
# pyproject.toml/verifier/tests/. The .dockerignore re-include
# (`!cmd/tide-langgraph-verifier/**`) admits the whole tree so the build
# context has it available; this explicit per-file COPY (not a blanket
# `verifier/`) is what actually keeps the dev lockfile and test suite out of
# the shipped image layers.
COPY cmd/tide-langgraph-verifier/verifier/__init__.py \
     cmd/tide-langgraph-verifier/verifier/__main__.py \
     cmd/tide-langgraph-verifier/verifier/envelope.py \
     cmd/tide-langgraph-verifier/verifier/tools.py \
     cmd/tide-langgraph-verifier/verifier/agent.py \
     cmd/tide-langgraph-verifier/verifier/verdict.py \
     /app/verifier/

# `python -m verifier` resolves the package from sys.path, and `-m` prepends the
# process CWD — which is /workspace in the dispatched pod (jobspec sets
# WorkingDir on the subagent container), not /app. PYTHONPATH pins the package
# location so the entrypoint resolves from any CWD.
ENV PYTHONPATH=/app

USER 1000

# Build-time entrypoint guard: pytest imports the package off the source tree
# and never exercises the shipped ENTRYPOINT, so a packaging break (file missing
# from the COPY above, or a CWD-dependent import) only surfaced at live dispatch.
# Run from / — NOT /app — as uid 1000 to prove the import chain the entrypoint
# needs works from an arbitrary CWD. __main__ is `if __name__` guarded, so
# importing it executes no agent code. Fails the build for every consumer of
# this Dockerfile (make targets, test-int-kind-prep, readonly script, release).
RUN cd / && python -c "import verifier, verifier.__main__"

# Consumed by ChatAnthropic's httpx transport to trust the credproxy sidecar's
# freshly-minted self-signed CA (D-06/D-07) — the sidecar writes ca.crt to this
# shared emptyDir path (internal/credproxy/cert.go). The image is a pure
# consumer of the env var; it has no TLS logic of its own beyond that trust.
ENV SSL_CERT_FILE=/etc/tide/proxy/ca.crt

ENTRYPOINT ["python", "-m", "verifier"]
