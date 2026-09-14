package claude

import (
	"errors"
	"fmt"
	"strings"
)

var (
	// ErrClaudeNotFound means no claude executable is available on PATH.
	ErrClaudeNotFound = errors.New("claude executable not found")
	// ErrNotLoggedIn means claude has no credentials.
	ErrNotLoggedIn = errors.New("claude is not logged in")
)

// CommandError reports a claude invocation that failed.
type CommandError struct {
	Args     []string
	ExitCode int
	// Result is the error text claude put in its JSON result, if any:
	// claude reports most failures there, on stdout, not on stderr.
	Result string
	Stderr string
	Err    error
}

func (x *CommandError) Error() string {
	if msg := firstLine(x.Result); msg != "" {
		return "claude: " + msg
	}
	if msg := firstLine(x.Stderr); msg != "" {
		return "claude: " + strings.TrimPrefix(msg, "error: ")
	}
	return fmt.Sprintf("claude: %v", x.Err)
}

func (x *CommandError) Unwrap() error {
	return x.Err
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return strings.TrimSpace(s)
}
