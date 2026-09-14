# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`git-why <file>:<line>` — a small Go CLI that stitches `git blame`, `git log` and friends into one human-readable report about why a line exists. v0.1 covered local Git history; v0.2 adds the pull request behind the commit (title, description, closed issues, review threads) fetched through the GitHub CLI; v0.3 adds `git-why ask <file>:<line>`, which feeds that same report to Claude Code (`claude -p`) and prints a few sentences plus a sources line. Still no network code of our own, no config files, no persistent state: everything external goes through `git`, `gh` and `claude`. The core command must keep working without `gh` and `claude`. When scope is unclear, pick the smaller implementation.

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

Six packages under `internal/`, orchestrated by `cli.gather` in `internal/cli/cli.go`:

```
target.Parse → Target.ReadLine        validate syntax, file exists, line in range (no git yet)
git.Open(dir of file)                 find git, repo toplevel, ensure HEAD exists
Repo.TrackedPath                      repo-relative path + untracked check
Repo.Blame                            commit + the line's number/path *in that commit*
Repo.Commit / ChangedFiles / LineHistory
cli.pullRequest                       unless --offline: GitHub remote? → github.Client.PullRequest via gh
presenter.Render                      git-why <file>:<line>
presenter.Render → claude.Client.Ask → presenter.RenderAnswer     git-why ask <file>:<line>
```

`cli.explain` is gather + Render. `cli.ask` looks up `claude` first (a missing one fails before any git work), gathers the same report, renders it plain into a buffer (`ansi.Strip` guards against a color-forcing environment), sends that as the prompt and renders the answer with a `Sources` line (commit, and the PR when there is one). `--offline` is a persistent flag so it works on both commands.

`cmd/git-why/main.go` only wires signals and calls `cli.Run`, which returns an exit code instead of calling `os.Exit` — e2e tests call `Run` in-process.

### Git client invariants (`internal/git`)

- Every command runs from the repo top-level (`Repo.root`), so all paths in and out are repo-relative. The one exception is `TrackedPath`, which runs `ls-files --full-name` from the file's own directory with just the basename, letting git compute the repo-relative path (avoids symlinked-tempdir path arithmetic, e.g. macOS `/var` → `/private/var`).
- `LineHistory` must start from the blame commit using `BlameLine.OrigPath` / `OrigLine`, never HEAD or the working-tree line number. Blame reads the working tree while `log -L` reads commits; this is what keeps renames and uncommitted edits above the line from breaking history.
- Only parse machine formats: `blame --porcelain`, `-z`, and `commitFormat` (`%x1f`-separated, body last). Parsers in `parse.go` are pure functions with fixture-string unit tests; `git.go` is covered by integration tests.
- `run` sets `LC_ALL=C` because some error detection matches stderr (e.g. "not a git repository"), and `GIT_LITERAL_PATHSPECS=1` so `*` / `[` in file names aren't globbed.
- Changed files use `git log -1 --diff-merges=first-parent`, not `diff-tree`: `diff-tree` ignores `--first-parent` on merges and lists every parent's diff. This sets the minimum Git version to 2.31 (documented in README).
- Blame porcelain marks *every* parentless commit as `boundary`, including a normal repo's first commit. `Blame` only keeps `Boundary` when `rev-parse --is-shallow-repository` is true. For a boundary commit `cli.explain` skips `ChangedFiles` (git would diff against the empty tree and list the whole repository) and the presenter says the files are unknown.
- `RemoteURL` uses `git remote get-url` (not `git config`) so `url.<base>.insteadOf` rewrites are applied.

### GitHub client invariants (`internal/github`)

- All GitHub access is `gh api graphql`. `pullRequestQuery` (`parse.go`) fetches in one call the commit's `associatedPullRequests` with `closingIssuesReferences` and the first 100 `reviewThreads`. Only when the picked pull request reports `pageInfo.hasNextPage` does `Client.threads` make a second call, `threadsQuery` under `--paginate`: gh follows `endCursor` itself and prints one JSON document per page with no separator (`parseThreads` decodes them in a loop), and the result replaces the first page rather than extending it. `$number` is a GraphQL `Int`, so it is passed with `-F`; `-f` sends a string the API refuses. `associatedPullRequests(first: 5)` and `closingIssuesReferences(first: 5)` stay unpaginated on purpose. gh owns authentication, `GH_TOKEN`, multiple accounts and hosts; git-why never sees a token. Don't add a REST/HTTP client.
- `ParseRemote` accepts remotes on the hosts returned by `github.Hosts()`: github.com plus the GitHub Enterprise Server named by `GH_HOST` (the same variable gh reads). `Remote.Host` is passed to gh as `--hostname`, so gh picks that host's credentials.
- Error mapping is driven by gh's observable behaviour, verified against the real API: exit 4 → `ErrNotLoggedIn`; stderr `HTTP 401` → `ErrBadCredentials`; stderr "rate limit" → `ErrRateLimited`; stdout GraphQL `errors[].type == NOT_FOUND` → `ErrNotFound` (repo missing or not visible); `object: null` → `ErrCommitNotFound` (not pushed); empty `nodes` → `ErrNoPullRequest`. Anything else stays a `*CommandError` whose message is gh's first stderr line.
- `pick` prefers the earliest-merged PR (the one that introduced the commit) over later/unmerged ones.
- In `cli`, a GitHub problem never changes the exit code or writes to stderr: `cli.pullRequest` turns it into `presenter.GitHub.Note`, shown dimmed under "Pull request". A nil `presenter.GitHub` (offline, no GitHub remote) omits the section entirely. The gh call is bounded by `githubTimeout` (a package variable so tests can shrink it); a parent-context cancel (Ctrl-C) still aborts the command. `run` sets `cmd.WaitDelay` so a killed gh that left a child holding the stdio pipes cannot keep `Run` blocked past the bound.
- Remote preference is `upstream`, then `origin`, then the rest (`cli.preferRemotes`) — forks keep their PRs upstream.

### Claude client invariants (`internal/claude`)

- `ask` runs `claude -p --output-format json --tools "" --no-session-persistence --strict-mcp-config --setting-sources "" --system-prompt <claude.SystemPrompt>` with the prompt on **stdin** (claude reads a non-terminal stdin as the prompt regardless of arguments, and an empty one is an error). Every flag was measured: without `--setting-sources ""` the user's settings, `CLAUDE.md` files and hooks load, which took an answer from 7 s / $0.03 to 70 s / $0.49 and made it ignore the system prompt's language; `--strict-mcp-config` and `--tools ""` keep MCP servers and tools out; `--no-session-persistence` leaves nothing on disk. `--bare` (and `CLAUDE_CODE_SIMPLE=1`) would isolate too but skip the keychain, so an OAuth login reports "Not logged in" — don't use them. Don't pass `--model`; with settings bypassed claude uses its built-in default.
- The JSON result is judged by `is_error`, not `subtype`: a not-logged-in claude prints `is_error: true` with `subtype: "success"` and exits 1. Error text lands in `result` on stdout; stderr is empty or noise, except for an unknown flag (`error: unknown option '--x'`, empty stdout). `parseResult` is a pure function; `Ask` maps `result` starting with "Not logged in" → `ErrNotLoggedIn`, any other failure → `*CommandError` whose message prefers `result`'s first line, then stderr's. An exit-0 empty `result` is an error, never an empty answer.
- The call is bounded by `askTimeout` (3 min, a package variable so tests can shrink it; answers measured under 10 s); `cmd.WaitDelay` as for gh. Verified against Claude Code 2.1.270.

### Errors and exit codes

Sentinel errors live in `target` (`ErrSyntax`, `ErrLine`, `ErrNotFound`, `ErrIsDir`, `ErrOutOfRange`), `git` (`ErrGitNotFound`, `ErrNotRepository`, `ErrNoCommits`, `ErrUntracked`, `ErrNotCommitted`, `ErrNoHistory`, plus `*CommandError`), `github` (see above; these never reach `Run`) and `claude` (`ErrClaudeNotFound`, `ErrNotLoggedIn` → exit 3; `*CommandError` and the timeout → exit 1; these do reach `Run`, since `ask` without an answer has nothing to print). Cobra argument/flag errors are wrapped in `*usageError` via the `Args` func and `SetFlagErrorFunc`, carrying the invoked command's synopsis for the hint. `cli.hint` maps errors to a `hint:` line, `cli.exitCode` maps them to exit codes, and `cli.githubNote` maps GitHub errors to the in-report note.

The exit-code table (0 ok, 1 git/history failure, 2 usage, 3 environment, 4 target) is documented in README.md and pinned by `TestExitCodes` in `internal/cli/cli_test.go` — change both together.

### Output

`presenter.Render` builds styled text with lipgloss v2 (`charm.land/lipgloss/v2`) and writes through `lipgloss.Fprint`, which strips ANSI automatically when the writer isn't a color terminal. Tests therefore see plain text. Sections in order: Current line, Introduced / Changed, Pull request, Review discussion (both GitHub sections only when GitHub was consulted; Review discussion only when a PR was found), Changed with, Line history. `presenter.RenderAnswer` prints the `ask` output: a title, the explanation wrapped at `maxProseWidth`, and one `Sources` line. If you change the output format, update the golden strings in `presenter_test.go` / `presenter/github_test.go` / `presenter/answer_test.go`, the e2e assertions in `cli_test.go` / `cli/github_test.go` / `cli/ask_test.go`, and the examples in README.md (README's first screen is the product demo).

## Testing notes

- `internal/testrepo` builds throwaway repos: it isolates git config with `t.Setenv` (`GIT_CONFIG_GLOBAL=/dev/null`, `GIT_CONFIG_NOSYSTEM=1`, fixed identity) and gives each `Commit` a deterministic date (`testrepo.Epoch` + 1 day per commit). It also points `GH_CONFIG_DIR` at an empty temp dir and blanks `GH_TOKEN`/`GITHUB_TOKEN`/`GH_ENTERPRISE_TOKEN`/`GITHUB_ENTERPRISE_TOKEN`/`GH_HOST`, so a real gh reached by accident exits 4 without touching the network or the developer's accounts; likewise `CLAUDE_CONFIG_DIR` points at an empty temp dir and `ANTHROPIC_API_KEY` is blanked, so a real claude reached by accident says "Not logged in" instead of spending money. Because of `t.Setenv` / `t.Chdir`, these tests can't use `t.Parallel`.
- Tests never call the real gh or claude. `testrepo.StubGH(t, stdout, stderr, exitCode)` and `testrepo.StubClaude(...)` put a fake script first on PATH and record its arguments (`Args()`) and stdin (`Stdin()`), so tests assert both the rendered output and the exact invocation. `testrepo.StubGHCalls(t, first, second, …)` answers successive calls in order (the pull request, then its thread pages) and `Calls()` counts them. To simulate "gh not installed" without losing git, see `gitOnlyPATH` in `cli/github_test.go`; `slowClaude` in `cli/ask_test.go` is a claude that never answers.
- Assert on hashes via `r.Short(hash)` rather than hard-coding them.

## Code conventions

- Go 1.26: prefer `errors.AsType[T]` and `strings.SplitSeq` (gopls flags the older forms).
- Receivers: `i` for the infrastructure types (`*git.Repo`, `*github.Client`), `x` for value/domain types and error types; context parameters are named `c`.
- Version is injected with `-X main.version=...` (Taskfile, GoReleaser uses `v{{ .Version }}`); `cli.resolveVersion` falls back to `debug.ReadBuildInfo` for `go install ...@vX`.
