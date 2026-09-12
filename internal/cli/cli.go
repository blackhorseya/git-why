// Package cli implements the git-why command: argument handling,
// orchestration of the git queries, and exit codes.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime/debug"

	"github.com/spf13/cobra"

	"github.com/blackhorseya/git-why/internal/git"
	"github.com/blackhorseya/git-why/internal/presenter"
	"github.com/blackhorseya/git-why/internal/target"
)

// Exit codes, as documented in the README.
const (
	exitOK      = 0
	exitFailure = 1 // git failed or returned no usable history
	exitUsage   = 2 // bad arguments or flags
	exitEnv     = 3 // git missing, or not inside a repository
	exitTarget  = 4 // file missing, untracked, or line out of range
)

// historyLimit caps how many commits the line history shows.
const historyLimit = 10

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
	cmd := &cobra.Command{
		Use:   "git-why <file>:<line>",
		Short: "Explain the Git history behind a line of code",
		Long: "git blame tells you who. git-why tells you why.\n\n" +
			"Shows the commit that last changed a line, its author, date and message,\n" +
			"the other files changed with it, and the line's history.",
		Example: "  git-why internal/payment/service.go:87\n" +
			"  git why main.go:42",
		Version:           resolveVersion(version),
		Args:              oneTarget,
		SilenceUsage:      true,
		SilenceErrors:     true,
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
		RunE: func(cmd *cobra.Command, args []string) error {
			return explain(cmd.Context(), args[0], cmd.OutOrStdout())
		},
	}
	cmd.SetVersionTemplate("git-why {{.Version}}\n")
	cmd.SetFlagErrorFunc(func(_ *cobra.Command, err error) error {
		return &usageError{err: err}
	})
	return cmd
}

func oneTarget(_ *cobra.Command, args []string) error {
	switch len(args) {
	case 1:
		return nil
	case 0:
		return &usageError{err: errors.New("missing <file>:<line> argument")}
	default:
		return &usageError{err: fmt.Errorf("expected one <file>:<line> argument, got %d", len(args))}
	}
}

// explain gathers everything git-why reports about the line named by arg
// and renders it to w.
func explain(c context.Context, arg string, w io.Writer) error {
	t, err := target.Parse(arg)
	if err != nil {
		return err
	}
	text, err := t.ReadLine()
	if err != nil {
		return err
	}

	abs, err := filepath.Abs(t.Path)
	if err != nil {
		return fmt.Errorf("resolve %s: %w", t.Path, err)
	}
	repo, err := git.Open(c, filepath.Dir(abs))
	if err != nil {
		return err
	}
	path, err := repo.TrackedPath(c, abs)
	if err != nil {
		return fmt.Errorf("%s: %w", t.Path, err)
	}

	blame, err := repo.Blame(c, path, t.Line)
	if err != nil {
		return err
	}
	commit, err := repo.Commit(c, blame.Hash)
	if err != nil {
		return err
	}
	files, err := repo.ChangedFiles(c, blame.Hash)
	if err != nil {
		return err
	}
	// Ask for one extra commit to learn whether older history was cut off.
	history, err := repo.LineHistory(c, blame.OrigPath, blame.OrigLine, blame.Hash, historyLimit+1)
	if err != nil {
		return err
	}
	truncated := len(history) > historyLimit
	if truncated {
		history = history[:historyLimit]
	}

	return presenter.Render(w, presenter.Report{
		Path:             path,
		Line:             t.Line,
		Text:             text,
		Blame:            blame,
		Commit:           commit,
		ChangedWith:      without(files, path, blame.OrigPath),
		History:          history,
		HistoryTruncated: truncated,
	})
}

// exitCode maps an error returned by the command to the process exit code
// documented in the README:
//
//	exitUsage  (2)  usage error: missing or extra arguments, unknown flags
//	                (*usageError), bad <file>:<line> syntax, invalid line number
//	exitEnv    (3)  environment error: git not found, not inside a repository
//	exitTarget (4)  target error: file does not exist or is a directory, file
//	                is untracked, line out of range
//	exitFailure(1)  everything else: no commits yet, line not committed yet,
//	                no history, git command failures
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
	case errors.Is(err, git.ErrGitNotFound), errors.Is(err, git.ErrNotRepository):
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
	if _, ok := errors.AsType[*usageError](err); ok {
		return "usage: git-why <file>:<line>  (see git-why --help)"
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
	}
	return ""
}

// usageError marks errors caused by how the command was invoked.
type usageError struct {
	err error
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
