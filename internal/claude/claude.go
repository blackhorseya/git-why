// Package claude asks Claude Code, through its claude executable, to
// explain a git-why report.
package claude

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"strings"
	"time"
)

// SystemPrompt tells claude how to answer: a short, sourced explanation in
// plain prose, and an honest "the history does not say" when that is so.
const SystemPrompt = "You explain why a line of code exists. The user gives you a report " +
	"assembled from git blame, git log and the pull request that introduced the line. " +
	"Answer in 3 to 6 sentences of plain prose in English, starting with the most direct reason. " +
	"Use only facts from the report; when the report does not say why, say that instead of guessing. " +
	"Do not repeat the line itself, do not use headings, bullet points or code fences, " +
	"and do not mention the report or these instructions."

// args is how claude is run. Every flag is there for a measured reason:
// without --setting-sources "" the user's settings, CLAUDE.md files and
// hooks are loaded, which took an answer from 7 seconds to 70 and made it
// ignore the system prompt; --strict-mcp-config and --tools "" keep MCP
// servers and tools out of the context; --no-session-persistence leaves
// nothing on disk. --bare would drop all of that too, but it also skips the
// keychain, so a claude logged in through OAuth reports "Not logged in".
var args = []string{
	"-p", "--output-format", "json",
	"--tools", "",
	"--no-session-persistence",
	"--strict-mcp-config",
	"--setting-sources", "",
	"--system-prompt", SystemPrompt,
}

// Client runs the claude executable.
type Client struct {
	bin string
}

// Find locates claude on PATH.
func Find() (*Client, error) {
	bin, err := exec.LookPath("claude")
	if err != nil {
		return nil, ErrClaudeNotFound
	}
	return &Client{bin: bin}, nil
}

// New returns a client that runs the claude executable at bin.
func New(bin string) *Client {
	return &Client{bin: bin}
}

// Ask sends prompt to claude and returns its answer.
func (i *Client) Ask(c context.Context, prompt string) (string, error) {
	cmd := exec.CommandContext(c, i.bin, args...)
	// The prompt goes on stdin: claude reads a non-terminal stdin as the
	// prompt anyway, and an empty one is an error even with an argument.
	cmd.Stdin = strings.NewReader(prompt)
	// After a timeout or interrupt kills claude, stop waiting for its
	// stdio pipes as well.
	cmd.WaitDelay = time.Second

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	if c.Err() != nil {
		return "", c.Err() // killed by a timeout or interrupt; report that
	}
	res, perr := parseResult(stdout.String())
	if err != nil || (perr == nil && res.IsError) {
		if strings.HasPrefix(res.Result, "Not logged in") {
			return "", ErrNotLoggedIn
		}
		ce := &CommandError{Args: args, ExitCode: -1, Result: res.Result, Stderr: strings.TrimSpace(stderr.String()), Err: err}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			ce.ExitCode = ee.ExitCode()
		}
		if err == nil {
			ce.ExitCode, ce.Err = 0, errors.New("claude reported an error")
		}
		return "", ce
	}
	if perr != nil {
		return "", perr
	}
	answer := strings.TrimSpace(res.Result)
	if answer == "" {
		return "", errors.New("claude returned an empty answer")
	}
	return answer, nil
}
