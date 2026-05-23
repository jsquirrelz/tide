# tide-demo-init

Bootstrap a local-only bare git repo from the embedded
`examples/tide-demo-fixture/` content. Used by the TIDE **medium** sample
(Phase 5 D-B3 — `examples/projects/medium/`) to seed a `file://`-backed
git remote on a PersistentVolumeClaim so operators can run TIDE end-to-end
against real Claude (~$5) without depending on any external public repo.

## Purpose

`examples/projects/medium/project.yaml` declares `targetRepo: file:///demo-remote.git`.
TIDE's clone Job needs that bare repo to exist before it can clone. This
binary creates it: `git init --bare <bootstrap-dir>` + embedded fixture
content + seeding commit + push to the bare repo. Runs once as an
in-cluster Job (`examples/projects/medium/demo-remote-init-job.yaml`)
mounted on the shared `demo-remote-pvc`, then exits.

## Usage

```
tide-demo-init --bootstrap-dir <path>
```

| Flag                | Required | Description                                                                       |
| ------------------- | -------- | --------------------------------------------------------------------------------- |
| `--bootstrap-dir`   | yes      | Filesystem path at which to create the bare repo (e.g. `/workspace/demo-remote.git`). Refuses to overwrite an existing directory. |

## Exit codes

| Code | Meaning                                                                |
| ---- | ---------------------------------------------------------------------- |
| 0    | Success — bare repo created at `--bootstrap-dir` and populated.       |
| 1    | Generic failure (git init / fixture extract / commit / push).         |
| 2    | Invariant violation (`--bootstrap-dir` empty, or target dir exists).  |

## Build (local)

The `//go:embed all:fixture` directive in `main.go` requires a sibling
`cmd/tide-demo-init/fixture/` directory at compile time. That directory is
materialized from the source-of-truth `examples/tide-demo-fixture/` via
`go generate`. The fixture directory is gitignored.

```bash
go generate ./cmd/tide-demo-init/...
go build ./cmd/tide-demo-init/
```

To run the unit tests:

```bash
go generate ./cmd/tide-demo-init/...
go test ./cmd/tide-demo-init/
```

## Build (Docker)

The Dockerfile at `images/tide-demo-init/Dockerfile` handles the fixture
positioning automatically:

```bash
docker build -t ghcr.io/jsquirrelz/tide-demo-init:v1.0.0 \
             -f images/tide-demo-init/Dockerfile .
```

The Dockerfile COPYs `examples/tide-demo-fixture/` to
`cmd/tide-demo-init/fixture/` inside the build context before running
`go build`. No `go generate` step needed in the container build.

## Submodule shim

The fixture SOT at `examples/tide-demo-fixture/` ships its own `go.mod` /
`go.sum` (it's a tiny standalone Go module the medium-sample's Claude
task operates on). Embedding a sibling `go.mod` would make Go treat
`fixture/` as a different module and reject the `//go:embed` directive
("cannot embed directory ...: in different module").

The `go:generate` directive and the Dockerfile both rename `go.mod` →
`go.mod.txt` and `go.sum` → `go.sum.txt` when materializing `fixture/`.
At unpack time, `cmd/tide-demo-init/main.go`'s `restoreShimmedName`
helper reverses the rename so the bare repo's working tree carries the
canonical filenames — byte-for-byte equivalent to the SOT.

This shim has no behavioral impact on the medium sample — operators see
a normal Go scaffold once the bare repo is cloned.

## How it's used in the medium sample

The medium sample (`examples/projects/medium/`) declares:

1. **`demo-remote-pvc.yaml`** — small `ReadWriteOnce` PVC (`demo-remote-pvc`,
   100Mi) at which the bare repo will live.
2. **`demo-remote-init-job.yaml`** — `batch/v1` Job that runs
   `ghcr.io/jsquirrelz/tide-demo-init:v1.0.0` (this binary's image)
   mounted on the PVC, with args
   `--bootstrap-dir=/workspace/demo-remote.git`. The Job exits on success;
   the operator then proceeds to apply the Project.
3. **`project.yaml`** — TIDE `Project` with
   `targetRepo: file:///demo-remote.git`. TIDE's existing clone Job (which
   also mounts `demo-remote-pvc`) finds the bare repo at the same path and
   clones it like any other git remote.

See `examples/projects/medium/README.md` for the full apply sequence.

## Related

- [`examples/tide-demo-fixture/`](../../examples/tide-demo-fixture/) — source-of-truth scaffold content (MIT-licensed)
- [`examples/projects/medium/`](../../examples/projects/medium/) — medium-sample manifests
- [`images/tide-demo-init/Dockerfile`](../../images/tide-demo-init/Dockerfile) — container image build
- [`cmd/tide-push/`](../tide-push/) — analog binary pattern (boundary-push)
