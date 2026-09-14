// Package cli implements the git-why command: argument handling,
// orchestration of the git queries, and exit codes.
package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"
	"slices"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/spf13/cobra"

	"github.com/blackhorseya/git-why/internal/claude"
	"github.com/blackhorseya/git-why/internal/git"
	"github.com/blackhorseya/git-why/internal/github"
	"github.com/blackhorseya/git-why/internal/presenter"
	"github.com/blackhorseya/git-why/internal/target"
)

// Exit codes, as documented in the README.
const (
	exitOK      = 0
	exitFailure = 1 // git failed or returned no usable history; claude failed
	exitUsage   = 2 // bad arguments or flags
	exitEnv     = 3 // git or claude missing, not logged in, not inside a repository
	exitTarget  = 4 // file missing, untracked, or line out of range
)

// historyLimit caps how many commits the line history shows.
const historyLimit = 10

// githubTimeout bounds the gh call so a slow network cannot hang git-why.
// A variable so tests can shrink it.
var githubTimeout = 10 * time.Second

// askTimeout bounds the claude call. Answers took under ten seconds when
// measured; the bound only stops a hung model from hanging git-why.
var askTimeout = 3 * time.Minute

// Run executes git-why with args (excluding the program name) and returns
// the process exit code.
func Run(c context.Context, version string, args []string, stdout, stderr io.Writer) int {
	cmd := newCommand(version)
	cmd.SetArgs(args)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	err := cmd.ExecuteContext(c)
	if err == nil {
		return exitOK
	}

	// Nothing useful can be done if stderr itself is broken.
	_, _ = fmt.Fprintf(stderr, "git-why: %v\n", err)
	if h := hint(err); h != "" {
		_, _ = fmt.Fprintf(stderr, "hint: %s\n", h)
	}
	return exitCode(err)
}

func newCommand(version string) *cobra.Command {
	var offline bool
	cmd := &cobra.Command{
		Use:   "git-why <file>:<line>",
		Short: "Explain the Git history behind a line of code",
		Long: "git blame tells you who. git-why tells you why.\n\n" +
			"Shows the commit that last changed a line, its author, date and message,\n" +
			"the other files changed with it, and the line's history.\n\n" +
			"When the repository has a GitHub remote, the pull request behind the commit\n" +
			"and its review discussion are shown too, fetched with the GitHub CLI (gh).",
		Example: "  git-why internal/payment/service.go:87\n" +
			"  git why main.go:42\n" +
			"  git why --offline main.go:42",
		Version:           resolveVersion(version),
		Args:              oneTarget,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return explain(cmd.Context(), args[0], offline, cmd.OutOrStdout())
		},
	}
	cmd.PersistentFlags().BoolVar(&offline, "offline", false, "skip the GitHub pull request lookup")
	cmd.SetVersionTemplate("git-why {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		return &usageError{err: err, usage: usageLine(cmd)}
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "ask <file>:<line>",
		Short: "Explain in a few sentences why the line exists, using Claude",
		Long: "Builds the same report as git-why and asks Claude Code (claude) to sum it up\n" +
			"in a few sentences. The report — commit messages, pull request text, review\n" +
			"comments and the line itself — is sent to the Claude account claude is logged\n" +
			"in with. claude runs without tools, settings, CLAUDE.md files or MCP servers.",
		Example: "  git-why ask internal/payment/service.go:87\n" +
			"  git why ask --offline main.go:42",
		Args:          oneTarget,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return ask(cmd.Context(), args[0], offline, cmd.OutOrStdout())
		},
	})
	return cmd
}

func oneTarget(cmd *cobra.Command, args []string) error {
	switch len(args) {
	case 1:
		return nil
	case 0:
		return &usageError{err: errors.New("missing <file>:<line> argument"), usage: usageLine(cmd)}
	default:
		return &usageError{err: fmt.Errorf("expected one <file>:<line> argument, got %d", len(args)), usage: usageLine(cmd)}
	}
}

// usageLine is the one-line synopsis of cmd, e.g. "git-why ask <file>:<line>".
func usageLine(cmd *cobra.Command) string {
	return cmd.CommandPath() + " <file>:<line>"
}

// explain gathers everything git-why reports about the line named by arg
// and renders it to w. With offline set, GitHub is not consulted.
func explain(c context.Context, arg string, offline bool, w io.Writer) error {
	r, err := gather(c, arg, offline)
	if err != nil {
		return err
	}
	return presenter.Render(w, r)
}

// ask gathers the same report, hands it to claude and writes the answer
// to w. claude is looked up first so a missing one fails before any git
// work is done.
func ask(c context.Context, arg string, offline bool, w io.Writer) error {
	client, err := claude.Find()
	if err != nil {
		return err
	}
	r, err := gather(c, arg, offline)
	if err != nil {
		return err
	}
	// The prompt is the report as the user would see it, minus any color
	// a forcing environment variable could have added.
	var report bytes.Buffer
	if err := presenter.Render(&report, r); err != nil {
		return err
	}
	ac, cancel := context.WithTimeout(c, askTimeout)
	defer cancel()
	answer, err := client.Ask(ac, ansi.Strip(report.String()))
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) && c.Err() == nil {
			return fmt.Errorf("claude did not answer within %s", askTimeout)
		}
		return err
	}
	return presenter.RenderAnswer(w, presenter.Answer{Report: r, Text: answer})
}

// gather runs the git queries (and, unless offline, the GitHub lookup)
// behind the line named by arg.
func gather(c context.Context, arg string, offline bool) (presenter.Report, error) {
	t, err := target.Parse(arg)
	if err != nil {
		return presenter.Report{}, err
	}
	text, err := t.ReadLine()
	if err != nil {
		return presenter.Report{}, err
	}

	abs, err := filepath.Abs(t.Path)
	if err != nil {
		return presenter.Report{}, fmt.Errorf("resolve %s: %w", t.Path, err)
	}
	repo, err := git.Open(c, filepath.Dir(abs))
	if err != nil {
		return presenter.Report{}, err
	}
	path, err := repo.TrackedPath(c, abs)
	if err != nil {
		return presenter.Report{}, fmt.Errorf("%s: %w", t.Path, err)
	}

	blame, err := repo.Blame(c, path, t.Line)
	if err != nil {
		return presenter.Report{}, err
	}
	commit, err := repo.Commit(c, blame.Hash)
	if err != nil {
		return presenter.Report{}, err
	}
	// A shallow boundary has no parent locally: git would diff the commit
	// against nothing and list the whole tree, so leave the files unknown.
	var files []string
	if !blame.Boundary {
		files, err = repo.ChangedFiles(c, blame.Hash)
		if err != nil {
			return presenter.Report{}, err
		}
	}
	// Ask for one extra commit to learn whether older history was cut off.
	history, err := repo.LineHistory(c, blame.OrigPath, blame.OrigLine, blame.Hash, historyLimit+1)
	if err != nil {
		return presenter.Report{}, err
	}
	truncated := len(history) > historyLimit
	if truncated {
		history = history[:historyLimit]
	}

	var gh *presenter.GitHub
	if !offline {
		gh = pullRequest(c, repo, commit)
		if err := c.Err(); err != nil {
			return presenter.Report{}, err // interrupted while waiting for gh
		}
	}

	return presenter.Report{
		Path:             path,
		Line:             t.Line,
		Text:             text,
		Blame:            blame,
		Commit:           commit,
		ChangedWith:      without(files, path, blame.OrigPath),
		History:          history,
		HistoryTruncated: truncated,
		GitHub:           gh,
	}, nil
}

// pullRequest looks up the pull request behind commit through gh. It
// returns nil when the repository has no GitHub remote. Any other problem
// becomes a note in the report instead of an error: GitHub context is a
// bonus on top of the local history, never a reason to withhold it.
func pullRequest(c context.Context, repo *git.Repo, commit git.Commit) *presenter.GitHub {
	remote, ok, err := githubRemote(c, repo)
	if err != nil {
		return &presenter.GitHub{Note: "GitHub lookup failed: " + err.Error()}
	}
	if !ok {
		return nil
	}

	pr, err := lookupPullRequest(c, remote, commit.Hash)
	if err != nil {
		return &presenter.GitHub{Note: githubNote(err, remote, commit.ShortHash)}
	}
	return &presenter.GitHub{PullRequest: &pr}
}

func lookupPullRequest(c context.Context, remote github.Remote, hash string) (github.PullRequest, error) {
	gh, err := github.Find()
	if err != nil {
		return github.PullRequest{}, err
	}
	c, cancel := context.WithTimeout(c, githubTimeout)
	defer cancel()
	return gh.PullRequest(c, remote, hash)
}

// githubRemote finds the GitHub repository the working copy tracks. A fork
// usually keeps the pull requests on upstream, so that remote is preferred
// over origin, which is preferred over any other.
func githubRemote(c context.Context, repo *git.Repo) (github.Remote, bool, error) {
	names, err := repo.Remotes(c)
	if err != nil {
		return github.Remote{}, false, err
	}
	hosts := github.Hosts()
	for _, name := range preferRemotes(names) {
		url, err := repo.RemoteURL(c, name)
		if err != nil {
			continue // a half-configured remote should not hide the others
		}
		if remote, ok := github.ParseRemote(url, hosts); ok {
			return remote, true, nil
		}
	}
	return github.Remote{}, false, nil
}

// preferRemotes orders remote names as upstream, origin, then the rest in
// the order given.
func preferRemotes(names []string) []string {
	preferred := []string{"upstream", "origin"}
	var out []string
	for _, p := range preferred {
		if slices.Contains(names, p) {
			out = append(out, p)
		}
	}
	for _, n := range names {
		if !slices.Contains(preferred, n) {
			out = append(out, n)
		}
	}
	return out
}

// githubNote explains, in one line, why the pull request could not be shown.
func githubNote(err error, remote github.Remote, short string) string {
	switch {
	case errors.Is(err, github.ErrGHNotFound):
		return "gh is not installed; get the GitHub CLI from https://cli.github.com, or pass --offline"
	case errors.Is(err, github.ErrNotLoggedIn):
		return "gh is not logged in; run `gh auth login`, or pass --offline"
	case errors.Is(err, github.ErrBadCredentials):
		return "GitHub rejected gh's credentials; run `gh auth login`"
	case errors.Is(err, github.ErrNotFound):
		return fmt.Sprintf("%s is not on GitHub or not visible to the gh account; check `gh auth status`", remote)
	case errors.Is(err, github.ErrCommitNotFound):
		return fmt.Sprintf("%s is not on GitHub yet; push it first", short)
	case errors.Is(err, github.ErrNoPullRequest):
		return fmt.Sprintf("no pull request for %s", short)
	case errors.Is(err, github.ErrRateLimited):
		return "GitHub API rate limit exceeded; try again later"
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("GitHub lookup timed out after %s", githubTimeout)
	}
	return "GitHub lookup failed: " + err.Error()
}

// exitCode maps an error returned by the command to the process exit code
// documented in the README:
//
//	exitUsage  (2)  usage error: missing or extra arguments, unknown flags
//	                (*usageError), bad <file>:<line> syntax, invalid line number
//	exitEnv    (3)  environment error: git not found, not inside a repository,
//	                claude not found or not logged in (ask)
//	exitTarget (4)  target error: file does not exist or is a directory, file
//	                is untracked, line out of range
//	exitFailure(1)  everything else: no commits yet, line not committed yet,
//	                no history, git command failures, claude failures
//
// Errors arrive wrapped with context, so match with errors.Is / errors.AsType
// rather than ==.
func exitCode(err error) int {
	if _, ok := errors.AsType[*usageError](err); ok {
		return exitUsage
	}

	switch {
	case errors.Is(err, target.ErrSyntax), errors.Is(err, target.ErrLine):
		return exitUsage
	case errors.Is(err, git.ErrGitNotFound), errors.Is(err, git.ErrNotRepository),
		errors.Is(err, claude.ErrClaudeNotFound), errors.Is(err, claude.ErrNotLoggedIn):
		return exitEnv
	case errors.Is(err, target.ErrNotFound), errors.Is(err, target.ErrIsDir),
		errors.Is(err, target.ErrOutOfRange), errors.Is(err, git.ErrUntracked):
		return exitTarget
	}
	return exitFailure
}

// hint suggests how to fix err, or returns "" when there is nothing useful
// to add beyond the error message itself.
func hint(err error) string {
	if ue, ok := errors.AsType[*usageError](err); ok {
		return fmt.Sprintf("usage: %s  (see git-why --help)", ue.usage)
	}

	switch {
	case errors.Is(err, target.ErrSyntax), errors.Is(err, target.ErrLine):
		return "usage: git-why <file>:<line>, for example: git-why main.go:42"
	case errors.Is(err, target.ErrNotFound):
		return "the path is resolved relative to the current directory"
	case errors.Is(err, target.ErrIsDir):
		return "point git-why at a file, not a directory"
	case errors.Is(err, git.ErrGitNotFound):
		return "install Git and make sure `git` is on your PATH"
	case errors.Is(err, git.ErrNotRepository):
		return "run git-why on a file inside a Git work tree"
	case errors.Is(err, git.ErrNoCommits):
		return "make a first commit, then try again"
	case errors.Is(err, git.ErrUntracked):
		return "git-why explains committed history; `git add` and commit the file first"
	case errors.Is(err, git.ErrNotCommitted):
		return "this line has uncommitted changes; commit them or pick another line"
	case errors.Is(err, claude.ErrClaudeNotFound):
		return "install Claude Code from https://claude.com/claude-code and log in with `claude`; plain `git-why <file>:<line>` needs no AI"
	case errors.Is(err, claude.ErrNotLoggedIn):
		return "run `claude` and log in with /login"
	}
	return ""
}

// usageError marks errors caused by how the command was invoked.
type usageError struct {
	err error
	// usage is the synopsis of the command that was invoked.
	usage string
}

func (x *usageError) Error() string {
	return x.err.Error()
}

func (x *usageError) Unwrap() error {
	return x.err
}

// resolveVersion prefers the version stamped at build time and falls back to
// the module version recorded by `go install module@version`.
func resolveVersion(stamped string) string {
	if stamped != "" && stamped != "dev" {
		return stamped
	}
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" && info.Main.Version != "(devel)" {
		return info.Main.Version
	}
	return "dev"
}

// without returns files minus any of the excluded paths, keeping order.
func without(files []string, exclude ...string) []string {
	var out []string
	for _, f := range files {
		skip := false
		for _, e := range exclude {
			if f == e {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, f)
		}
	}
	return out
}
