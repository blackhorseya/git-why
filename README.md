# git-why

> `git blame` tells you **who**. `git-why` tells you **why**.

Point `git-why` at a file and line number, and it pulls together the history
behind that line — the commit that introduced it, who wrote it, the pull
request it came in with and what reviewers said, what else changed alongside
it, and how the line evolved over time — in one readable screen instead of
four Git commands and a browser tab.

```
git-why internal/payment/service.go:87
```

```
Why does this line exist?

Current line  internal/payment/service.go:87
  if err := repository.Exists(ctx, id); err != nil {

Introduced / Changed
  Commit: 8ac912f
  Author: Sean <sean@example.com>
  Date:   2026-08-21

  fix: prevent duplicate payment processing

  Retries from the gateway could charge twice.

Pull request
  #42  fix: prevent duplicate payment processing
  by sean · merged 2026-08-21 · https://github.com/acme/pay/pull/42
  Closes #38  Duplicate charges on gateway retry

  Gateway retries hit the charge endpoint twice inside the TTL window.
  Look the payment up first so the second attempt is a no-op.

Review discussion
  @grace (L87, resolved, 2 replies)
    Should this be a 409 instead of 500?

Changed with
  internal/payment/repository.go
  internal/payment/service_test.go

Line history
  8ac912f  2026-08-21  fix: prevent duplicate payment processing
  a912bc1  2026-08-20  chore: change Redis TTL to 24h
  f8219de  2026-08-01  feat: initial payment service
```

## Install

**Go**

```
go install github.com/blackhorseya/git-why/cmd/git-why@latest
```

**Prebuilt binaries** (macOS / Linux)

Download the archive for your platform from the
[releases page](https://github.com/blackhorseya/git-why/releases), then put
`git-why` somewhere on your `PATH`:

```
tar -xzf git-why_*_darwin_arm64.tar.gz
mv git-why /usr/local/bin/
```

`git-why` shells out to your local `git`, so Git 2.31 or newer must be
installed. The pull request and review sections need the
[GitHub CLI](https://cli.github.com) (`gh`) logged in; without it, everything
else still works.

## Usage

```
git-why <file>:<line>
git-why --offline <file>:<line>
git-why --version
git-why --help
```

Run it from anywhere inside a Git repository. The path is resolved relative
to your current directory, just like `git blame`.

Because `git-why` lives on your `PATH` with a `git-` prefix, Git also picks
it up as a subcommand:

```
git why internal/payment/service.go:87
```

### GitHub context

When the repository has a remote on github.com, `git-why` also looks up the
pull request that brought the commit in and shows:

- the pull request's title, author, merge date and description;
- the issues it closes;
- the review threads on the file you asked about, with their resolved
  state (threads on other files are only counted).

The lookup goes through `gh`, so it uses whatever account `gh auth login`
set up — including `GH_TOKEN` and multiple accounts — and `git-why` never
handles credentials itself. When several remotes exist, `upstream` is tried
first (a fork's pull requests live there), then `origin`, then the rest.

A GitHub problem never changes the exit code or hides the local history: the
`Pull request` section just explains what happened, for example that the
commit has not been pushed yet, that `gh` is not logged in, or that the
commit was pushed without a pull request.

Pass `--offline` to skip the lookup entirely. That is also the only network
access `git-why` ever makes — no telemetry, no config files.

### Exit codes

| Code | Meaning |
|------|---------|
| `0`  | Success |
| `1`  | Git failed or returned no usable history |
| `2`  | Usage error — bad `<file>:<line>` syntax, missing argument, invalid line number |
| `3`  | Environment error — not inside a Git repository, or `git` not found |
| `4`  | Target error — file does not exist, is untracked, or the line is out of range |

Every error message says what went wrong and, where it helps, what to do
about it.

## How it works

`git-why` does not reimplement Git or talk to GitHub on its own. It runs the
same commands you would type by hand and stitches the results together:

| Question | Command |
|----------|---------|
| Which commit last touched this line? | `git blame --porcelain -L <n>,<n>` |
| Who, when, and why? | `git log -1 --format=...` |
| What else changed in that commit? | `git log -1 --name-only --diff-merges=first-parent` |
| How did this line evolve? | `git log -L <n>,<n>:<file>` |
| Which pull request, which issues, what did reviewers say? | `gh api graphql` on the commit's `associatedPullRequests` |

The line history starts from the commit `blame` reports, using the file name
and line number *as of that commit*, so renames and uncommitted edits
elsewhere in the file do not throw it off.

## Roadmap

- **v0.1 — Git history** ✓
- **v0.2 — GitHub context** ✓: pull request, linked issues, review threads.
- **v0.3 — AI explanation**: an optional `git-why ask <file>:<line>` that
  summarises the history into a short "why". The core command will keep
  working without it.

## Development

Developer commands use [Task](https://taskfile.dev) (`brew install go-task`):

```
task build    # → ./bin/git-why
task test
task lint
```

Integration tests create throwaway Git repositories under the Go test temp
directory and require `git` on your `PATH`. GitHub is never contacted from
tests: they run a fake `gh` that replays canned responses.

## License

[Apache License 2.0](LICENSE)
