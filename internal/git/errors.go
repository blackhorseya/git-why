package git

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrGitNotFound means no git executable is available on PATH.
	ErrGitNotFound = errors.New("git executable not found")
	// ErrNotRepository means the directory is not inside a Git work tree.
	ErrNotRepository = errors.New("not inside a git repository")
	// ErrNoCommits means the repository exists but HEAD has no commits yet.
	ErrNoCommits = errors.New("repository has no commits yet")
	// ErrUntracked means the file exists but Git does not track it.
	ErrUntracked = errors.New("file is not tracked by git")
	// ErrNotCommitted means the line only exists in the working tree or index.
	ErrNotCommitted = errors.New("line is not committed yet")
	// ErrNoHistory means git returned no history for the line.
	ErrNoHistory = errors.New("no history found for line")
)

// CommandError reports a git invocation that exited unsuccessfully.
type CommandError struct {
	Args     []string
	ExitCode int
	Stderr   string
	Err      error
}

func (x *CommandError) Error() string {
	name := "git"
	if len(x.Args) > 0 {
		name += " " + x.Args[0]
	}
	if x.Stderr != "" {
		return fmt.Sprintf("%s: %s", name, x.Stderr)
	}
	return fmt.Sprintf("%s: %v", name, x.Err)
}

func (x *CommandError) Unwrap() error {
	return x.Err
}

func (x *CommandError) stderrContains(s string) bool {
	return strings.Contains(x.Stderr, s)
}
