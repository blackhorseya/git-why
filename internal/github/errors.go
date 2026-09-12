package github

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrGHNotFound means no gh executable is available on PATH.
	ErrGHNotFound = errors.New("gh executable not found")
	// ErrNotLoggedIn means gh has no credentials for the host.
	ErrNotLoggedIn = errors.New("gh is not logged in")
	// ErrBadCredentials means GitHub rejected the credentials gh used.
	ErrBadCredentials = errors.New("GitHub rejected gh's credentials")
	// ErrRateLimited means the GitHub API rate limit is exhausted.
	ErrRateLimited = errors.New("GitHub API rate limit exceeded")
	// ErrNotFound means the repository does not exist on GitHub or is not
	// visible to the account gh is using.
	ErrNotFound = errors.New("repository not found on GitHub")
	// ErrCommitNotFound means GitHub does not have the commit yet.
	ErrCommitNotFound = errors.New("commit not found on GitHub")
	// ErrNoPullRequest means no pull request is associated with the commit.
	ErrNoPullRequest = errors.New("no pull request for commit")
)

// CommandError reports a gh invocation that exited unsuccessfully.
type CommandError struct {
	Args     []string
	ExitCode int
	Stdout   string
	Stderr   string
	Err      error
}

func (x *CommandError) Error() string {
	name := "gh"
	if len(x.Args) > 0 {
		name += " " + x.Args[0]
	}
	if msg := firstLine(x.Stderr); msg != "" {
		return fmt.Sprintf("%s: %s", name, strings.TrimPrefix(msg, "gh: "))
	}
	return fmt.Sprintf("%s: %v", name, x.Err)
}

func (x *CommandError) Unwrap() error {
	return x.Err
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
