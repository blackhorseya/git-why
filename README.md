# git-why

> `git blame` tells you **who**. `git-why` tells you **why**.

Point `git-why` at a file and line number, and it pulls together the Git
history behind that line — the commit that introduced it, who wrote it, what
else changed alongside it, and how the line evolved over time — in one
readable screen instead of four Git commands.

```
git-why internal/payment/service.go:87
```

```
Why does this line exist?

Current line
  if err := repository.Exists(ctx, id); err != nil {

Introduced / Changed
  Commit: 8ac912f
  Author: Sean
  Date:   2026-08-21

  fix: prevent duplicate payment processing

Changed with
  internal/payment/repository.go
  internal/payment/service_test.go

Line history
  8ac912f  fix: prevent duplicate payment processing
  a912bc1  chore: change Redis TTL to 24h
  f8219de  feat: initial payment service
```

<!-- TODO(release): replace with docs/demo.gif before v0.1.0 -->

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

`git-why` shells out to your local `git`, so Git must be installed. No
network access, no telemetry, no config files.

## Usage

```
git-why <file>:<line>
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

`git-why` does not reimplement Git. It runs the same commands you would type
by hand and stitches the results together:

| Question | Git command |
|----------|-------------|
| Which commit last touched this line? | `git blame --porcelain -L <n>,<n>` |
| Who, when, and why? | `git show --format=...` |
| What else changed in that commit? | `git diff-tree --name-only` |
| How did this line evolve? | `git log -L <n>,<n>:<file>` |

The line history starts from the commit `blame` reports, using the file name
and line number *as of that commit*, so renames and uncommitted edits
elsewhere in the file do not throw it off.

## Roadmap

`v0.1` is deliberately a small, polished Git utility. Later versions may add:

- **v0.2 — GitHub context**: link commits to pull requests, issues, and
  review discussion.
- **v0.3 — AI explanation**: an optional `git-why ask <file>:<line>` that
  summarises the history into a short "why". The core command will keep
  working without it.

## Development

```
make build    # → ./bin/git-why
make test
make lint
```

Integration tests create throwaway Git repositories under the Go test temp
directory and require `git` on your `PATH`.

## License

[Apache License 2.0](LICENSE)
