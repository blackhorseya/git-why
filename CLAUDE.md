# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`git-why <file>:<line>` — a small Go CLI that stitches `git blame`, `git log` and friends into one human-readable report about why a line exists. v0.1 covered local Git history; v0.2 adds the pull request behind the commit (title, description, closed issues, review threads) fetched through the GitHub CLI. Still no network code of our own, no LLM, no config files, no persistent state. When scope is unclear, pick the smaller implementation. An optional `git-why ask` AI mode (v0.3) is future work — don't build toward it yet.

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

Five packages under `internal/`, orchestrated by `cli.explain` in `internal/cli/cli.go`:

```
target.Parse → Target.ReadLine        validate syntax, file exists, line in range (no git yet)
git.Open(dir of file)                 find git, repo toplevel, ensure HEAD exists
Repo.TrackedPath                      repo-relative path + untracked check
Repo.Blame                            commit + the line's number/path *in that commit*
Repo.Commit / ChangedFiles / LineHistory
cli.pullRequest                       unless --offline: GitHub remote? → github.Client.PullRequest via gh
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
- `RemoteURL` uses `git remote get-url` (not `git config`) so `url.<base>.insteadOf` rewrites are applied.

### GitHub client invariants (`internal/github`)

- All GitHub access is one `gh api graphql` call (`pullRequestQuery` in `parse.go`): the commit's `associatedPullRequests` with `closingIssuesReferences` and `reviewThreads`. gh owns authentication, `GH_TOKEN`, multiple accounts and hosts; git-why never sees a token. Don't add a REST/HTTP client.
- `ParseRemote` accepts remotes on the hosts returned by `github.Hosts()`: github.com plus the GitHub Enterprise Server named by `GH_HOST` (the same variable gh reads). `Remote.Host` is passed to gh as `--hostname`, so gh picks that host's credentials.
- Error mapping is driven by gh's observable behaviour, verified against the real API: exit 4 → `ErrNotLoggedIn`; stderr `HTTP 401` → `ErrBadCredentials`; stderr "rate limit" → `ErrRateLimited`; stdout GraphQL `errors[].type == NOT_FOUND` → `ErrNotFound` (repo missing or not visible); `object: null` → `ErrCommitNotFound` (not pushed); empty `nodes` → `ErrNoPullRequest`. Anything else stays a `*CommandError` whose message is gh's first stderr line.
- `pick` prefers the earliest-merged PR (the one that introduced the commit) over later/unmerged ones.
- In `cli`, a GitHub problem never changes the exit code or writes to stderr: `cli.pullRequest` turns it into `presenter.GitHub.Note`, shown dimmed under "Pull request". A nil `presenter.GitHub` (offline, no GitHub remote) omits the section entirely. The gh call is bounded by `githubTimeout` (a package variable so tests can shrink it); a parent-context cancel (Ctrl-C) still aborts the command. `run` sets `cmd.WaitDelay` so a killed gh that left a child holding the stdio pipes cannot keep `Run` blocked past the bound.
- Remote preference is `upstream`, then `origin`, then the rest (`cli.preferRemotes`) — forks keep their PRs upstream.

### Errors and exit codes

Sentinel errors live in `target` (`ErrSyntax`, `ErrLine`, `ErrNotFound`, `ErrIsDir`, `ErrOutOfRange`), `git` (`ErrGitNotFound`, `ErrNotRepository`, `ErrNoCommits`, `ErrUntracked`, `ErrNotCommitted`, `ErrNoHistory`, plus `*CommandError`) and `github` (see above; these never reach `Run`). Cobra argument/flag errors are wrapped in `*usageError` via the `Args` func and `SetFlagErrorFunc`. `cli.hint` maps errors to a `hint:` line, `cli.exitCode` maps them to exit codes, and `cli.githubNote` maps GitHub errors to the in-report note.

The exit-code table (0 ok, 1 git/history failure, 2 usage, 3 environment, 4 target) is documented in README.md and pinned by `TestExitCodes` in `internal/cli/cli_test.go` — change both together.

### Output

`presenter.Render` builds styled text with lipgloss v2 (`charm.land/lipgloss/v2`) and writes through `lipgloss.Fprint`, which strips ANSI automatically when the writer isn't a color terminal. Tests therefore see plain text. Sections in order: Current line, Introduced / Changed, Pull request, Review discussion (both GitHub sections only when GitHub was consulted; Review discussion only when a PR was found), Changed with, Line history. If you change the output format, update the golden strings in `presenter_test.go` / `presenter/github_test.go`, the e2e assertions in `cli_test.go` / `cli/github_test.go`, and the example in README.md (README's first screen is the product demo).

## Testing notes

- `internal/testrepo` builds throwaway repos: it isolates git config with `t.Setenv` (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, fixed identity) and gives each `Commit` a deterministic date (`testrepo.Epoch` + 1 day per commit). It also points `GH_CONFIG_DIR` at an empty temp dir and blanks `GH_TOKEN`/`GITHUB_TOKEN`/`GH_ENTERPRISE_TOKEN`/`GITHUB_ENTERPRISE_TOKEN`/`GH_HOST`, so a real gh reached by accident exits 4 without touching the network or the developer's accounts. Because of `t.Setenv` / `t.Chdir`, these tests can't use `t.Parallel`.
- Tests never call the real gh. `testrepo.StubGH(t, stdout, stderr, exitCode)` puts a fake `gh` script first on PATH and records its arguments (`Args()`), so tests assert both the rendered output and the exact gh invocation. To simulate "gh not installed" without losing git, see `gitOnlyPATH` in `cli/github_test.go`.
- Assert on hashes via `r.Short(hash)` rather than hard-coding them.

## Code conventions

- Go 1.26: prefer `errors.AsType[T]` and `strings.SplitSeq` (gopls flags the older forms).
- Receivers: `i` for the infrastructure types (`*git.Repo`, `*github.Client`), `x` for value/domain types and error types; context parameters are named `c`.
- Version is injected with `-X main.version=...` (Taskfile, GoReleaser uses `v{{ .Version }}`); `cli.resolveVersion` falls back to `debug.ReadBuildInfo` for `go install ...@vX`.
