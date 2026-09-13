package github

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// exitAuthRequired is gh's exit code when it has no credentials.
const exitAuthRequired = 4

// PullRequest is the pull request that brought a commit into a repository.
type PullRequest struct {
	Number   int
	Title    string
	State    string // "OPEN", "CLOSED" or "MERGED"
	MergedAt time.Time
	URL      string
	Author   string // login, empty for a deleted account
	Body     string
	// Issues are the issues the pull request closes.
	Issues []Issue
	// Threads are the review threads on the pull request's diff.
	Threads []Thread
}

// Issue is an issue linked to a pull request.
type Issue struct {
	Number int
	Title  string
	URL    string
	State  string // "OPEN" or "CLOSED"
}

// Thread is one review thread on a pull request's diff.
type Thread struct {
	// Path is the file the thread is attached to.
	Path string
	// Line is the current line number, or 0 when the thread is outdated.
	Line int
	// Author and Body belong to the comment that opened the thread.
	Author string
	Body   string
	URL    string
	// Replies counts the comments after the first one.
	Replies  int
	Resolved bool
	Outdated bool
}

// Client runs the gh executable.
type Client struct {
	bin string
}

// Find locates gh on PATH.
func Find() (*Client, error) {
	bin, err := exec.LookPath("gh")
	if err != nil {
		return nil, ErrGHNotFound
	}
	return &Client{bin: bin}, nil
}

// New returns a client that runs the gh executable at bin.
func New(bin string) *Client {
	return &Client{bin: bin}
}

// PullRequest returns the pull request that introduced the commit with the
// full object id hash into remote. When several pull requests contain the
// commit, the one merged first wins; an unmerged one is returned only when
// no merged one exists.
func (i *Client) PullRequest(c context.Context, remote Remote, hash string) (PullRequest, error) {
	out, err := i.run(c, "api", "graphql", "--hostname", remote.Host,
		"-f", "query="+pullRequestQuery,
		"-f", "owner="+remote.Owner,
		"-f", "name="+remote.Name,
		"-f", "oid="+hash)
	if err != nil {
		if ce, ok := errors.AsType[*CommandError](err); ok {
			err = classify(ce)
		}
		return PullRequest{}, fmt.Errorf("%s@%s: %w", remote, short(hash), err)
	}

	prs, err := parsePullRequests(out)
	if err != nil {
		return PullRequest{}, fmt.Errorf("%s@%s: %w", remote, short(hash), err)
	}
	pr, ok := pick(prs)
	if !ok {
		return PullRequest{}, fmt.Errorf("%s@%s: %w", remote, short(hash), ErrNoPullRequest)
	}
	return pr, nil
}

// classify maps gh's exit status and output onto the package's sentinel
// errors, falling back to the raw CommandError.
func classify(ce *CommandError) error {
	switch {
	case ce.ExitCode == exitAuthRequired:
		return ErrNotLoggedIn
	case strings.Contains(ce.Stderr, "HTTP 401"):
		return ErrBadCredentials
	case strings.Contains(strings.ToLower(ce.Stderr), "rate limit"):
		return ErrRateLimited
	case hasGraphQLError(ce.Stdout, "NOT_FOUND"):
		return ErrNotFound
	}
	return ce
}

func (i *Client) run(c context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(c, i.bin, args...)
	// After a timeout or interrupt kills gh, stop waiting for its stdio pipes
	// as well: a child gh left behind would otherwise keep Run blocked.
	cmd.WaitDelay = time.Second
	cmd.Env = append(os.Environ(),
		"GH_NO_UPDATE_NOTIFIER=1", // never mix update nags into stderr
		"GH_PROMPT_DISABLED=1",    // fail instead of prompting
		"GH_PAGER=cat",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// A timeout or interrupt kills gh; report that rather than the kill.
		if c.Err() != nil {
			return "", c.Err()
		}
		ce := &CommandError{Args: args, ExitCode: -1, Stdout: stdout.String(), Stderr: strings.TrimSpace(stderr.String()), Err: err}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			ce.ExitCode = ee.ExitCode()
		}
		return "", ce
	}
	return stdout.String(), nil
}

func short(hash string) string {
	if len(hash) > 7 {
		return hash[:7]
	}
	return hash
}
