# Release checklist

Steps to verify before tagging a TIDE release. This is a reference; the
authoritative install and production guidance lives in
[docs/INSTALL.md](INSTALL.md) and [docs/production.md](production.md).

## Pre-tag checks

1. **Tests green.** `make test-int` exits 0 and the Ginkgo summary shows no
   failures. Read the `MAKE_EXIT` line AND `grep -nE '^--- FAIL|^FAIL\s'` the
   log — a red Go test fails the package even when the Ginkgo summary prints
   `SUCCESS` (Phase 7 lesson).

2. **Lint clean.** `golangci-lint run ./...` exits 0 (gosec, errcheck,
   staticcheck all on per `CLAUDE.md` stack conventions).

3. **Pricing table verified (D-03).** Run the drift check against the live
   Anthropic pricing page and resolve any drift in
   `internal/subagent/anthropic/pricing.go` before tagging:

   ```sh
   ./hack/check-pricing-drift.sh
   ```

   Exit 0 means the compiled table matches the live page — safe to tag.
   Exit 1 means drift — update `internal/subagent/anthropic/pricing.go` with
   the quoted model/dimension/cents values and re-run until exit 0.
   Exit 2 means the fetch failed — resolve network access and retry before
   tagging; do not skip this step.

   The weekly `.github/workflows/pricing-drift.yaml` automation opens a
   `pricing-drift`-labeled issue when drift is detected between releases.
   Resolve any open `pricing-drift` issue before tagging.

4. **Chart contract intact.** The rendered default must not point any image
   field at the stub subagent:

   ```sh
   helm template charts/tide | grep -cE '(image:|value:).*tide-stub-subagent'
   ```

   Must print `0` (grep exits 1 on zero matches — read the count, not the
   exit code). Don't grep `values.yaml` for `stub`: it legitimately mentions
   the stub in comments and in the `images.stubSubagent` build-tooling keys,
   and the rendered template also carries a stub opt-in *comment* — only an
   `image:`/`value:` field referencing the stub is a contract break. The
   binary catches up to the chart — never change chart defaults to match a
   broken binary.

5. **go.mod tidy.** `go mod tidy && git diff --exit-code go.mod go.sum` exits 0.

6. **goreleaser dry-run.** `goreleaser release --snapshot --skip=publish --clean`
   builds all 5 platform binaries and 7 component images without errors.

## Tagging

```sh
git tag -a vX.Y.Z -m "vX.Y.Z"
git push origin vX.Y.Z
```

The `release.yaml` workflow fires on the tag push and handles GHCR image
pushes, Helm chart packaging, and GitHub release asset uploads.
