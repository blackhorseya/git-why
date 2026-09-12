# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`git-why <file>:<line>` — a small Go CLI that stitches `git blame`, `git log` and friends into one human-readable report about why a line exists. v0.1 is deliberately small: local Git only, no network, no GitHub API, no LLM, no config files, no persistent state. When scope is unclear, pick the smaller implementation. GitHub context (v0.2) and an optional `git-why ask` AI mode (v0.3) are future work — don't build toward them yet.

## Commands

Dev commands use [Task](https://taskfile.dev) (`Taskfile.yml`); binaries go to `./bin/`.

```
task build      # ./bin/git-why, version stamped from git describe
task test       # go test -race ./...  (integration tests need git on PATH)
task lint       # go vet + golangci-lint (v2 config in .golangci.yml)
task fmt        # gofmt + goimports via golangci-lint fmt
task snapshot   # goreleaser release --snapshot --clean → ./dist
```

Single test: `go test ./internal/git -run TestBlameFollowsRenamesAndLocalEdits -v`
Validate release config: `goreleaser check`. Releases are cut by pushing a `v*` tag (`.github/workflows/release.yml`).

## Architecture

Four packages under `internal/`, orchestrated by `cli.explain` in `internal/cli/cli.go`:

```
target.Parse → Target.ReadLine        validate syntax, file exists, line in range (no git yet)
git.Open(dir of file)                 find git, repo toplevel, ensure HEAD exists
Repo.TrackedPath                      repo-relative path + untracked check
Repo.Blame                            commit + the line's number/path *in that commit*
Repo.Commit / ChangedFiles / LineHistory
presenter.Render
```

`cmd/git-why/main.go` only wires signals and calls `cli.Run`, which returns an exit code instead of calling `os.Exit` — e2e tests call `Run` in-process.

### Git client invariants (`internal/git`)

- Every command runs from the repo top-level (`Repo.root`), so all paths in and out are repo-relative. The one exception is `TrackedPath`, which runs `ls-files --full-name` from the file's own directory with just the basename, letting git compute the repo-relative path (avoids symlinked-tempdir path arithmetic, e.g. macOS `/var` → `/private/var`).
- `LineHistory` must start from the blame commit using `BlameLine.OrigPath` / `OrigLine`, never HEAD or the working-tree line number. Blame reads the working tree while `log -L` reads commits; this is what keeps renames and uncommitted edits above the line from breaking history.
- Only parse machine formats: `blame --porcelain`, `-z`, and `commitFormat` (`%x1f`-separated, body last). Parsers in `parse.go` are pure functions with fixture-string unit tests; `git.go` is covered by integration tests.
- `run` sets `LC_ALL=C` because some error detection matches stderr (e.g. "not a git repository"), and `GIT_LITERAL_PATHSPECS=1` so `*` / `[` in file names aren't globbed.
- Changed files use `git log -1 --diff-merges=first-parent`, not `diff-tree`: `diff-tree` ignores `--first-parent` on merges and lists every parent's diff. This sets the minimum Git version to 2.31 (documented in README).
- Blame porcelain marks *every* parentless commit as `boundary`, including a normal repo's first commit. `Blame` only keeps `Boundary` when `rev-parse --is-shallow-repository` is true.

### Errors and exit codes

Sentinel errors live in `target` (`ErrSyntax`, `ErrLine`, `ErrNotFound`, `ErrIsDir`, `ErrOutOfRange`) and `git` (`ErrGitNotFound`, `ErrNotRepository`, `ErrNoCommits`, `ErrUntracked`, `ErrNotCommitted`, `ErrNoHistory`, plus `*CommandError`). Cobra argument/flag errors are wrapped in `*usageError` via the `Args` func and `SetFlagErrorFunc`. `cli.hint` maps errors to a `hint:` line and `cli.exitCode` maps them to exit codes.

The exit-code table (0 ok, 1 git/history failure, 2 usage, 3 environment, 4 target) is documented in README.md and pinned by `TestExitCodes` in `internal/cli/cli_test.go` — change both together.

### Output

`presenter.Render` builds styled text with lipgloss v2 (`charm.land/lipgloss/v2`) and writes through `lipgloss.Fprint`, which strips ANSI automatically when the writer isn't a color terminal. Tests therefore see plain text. If you change the output format, update the golden string in `presenter_test.go`, the e2e assertions in `cli_test.go`, and the example in README.md (README's first screen is the product demo).

## Testing notes

- `internal/testrepo` builds throwaway repos: it isolates git config with `t.Setenv` (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, fixed identity) and gives each `Commit` a deterministic date (`testrepo.Epoch` + 1 day per commit). Because of `t.Setenv` / `t.Chdir`, these tests can't use `t.Parallel`.
- Assert on hashes via `r.Short(hash)` rather than hard-coding them.

## Code conventions

- Go 1.26: prefer `errors.AsType[T]` and `strings.SplitSeq` (gopls flags the older forms).
- Receivers: `i` for the infrastructure type (`*git.Repo`), `x` for value/domain types and error types; context parameters are named `c`.
- Version is injected with `-X main.version=...` (Taskfile, GoReleaser uses `v{{ .Version }}`); `cli.resolveVersion` falls back to `debug.ReadBuildInfo` for `go install ...@vX`.
