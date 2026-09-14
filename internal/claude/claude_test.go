package claude

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/blackhorseya/git-why/internal/testrepo"
)

func TestAsk(t *testing.T) {
	stub := testrepo.StubClaude(t, fixtureAnswer, "", 0)

	answer, err := New(stub.Bin).Ask(t.Context(), "Why does this line exist?\n\nCurrent line  main.go:1\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := "The timeout is a package-level variable rather than a constant so tests can override it."; answer != want {
		t.Errorf("Ask() = %q, want %q", answer, want)
	}
	if got := stub.Stdin(); got != "Why does this line exist?\n\nCurrent line  main.go:1\n" {
		t.Errorf("prompt on stdin = %q", got)
	}

	// Every isolation flag matters (see the comment on args); pin them.
	args := stub.Args()
	for _, want := range [][]string{
		{"-p"}, {"--output-format", "json"}, {"--tools", ""}, {"--no-session-persistence"},
		{"--strict-mcp-config"}, {"--setting-sources", ""}, {"--system-prompt", SystemPrompt},
	} {
		i := slices.Index(args, want[0])
		if i < 0 || (len(want) > 1 && (i+1 >= len(args) || args[i+1] != want[1])) {
			t.Errorf("claude was not run with %q: %q", want, args)
		}
	}
	for _, bad := range []string{"--bare", "--model"} {
		if slices.Contains(args, bad) {
			t.Errorf("claude was run with %s: %q", bad, args)
		}
	}
}

func TestAskErrors(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		stderr string
		code   int
		want   error  // sentinel, or nil when any error will do
		msg    string // expected Error() text, or ""
	}{
		{"not logged in", fixtureNotLoggedIn, "", 1, ErrNotLoggedIn, ""},
		{"unknown option", "", "error: unknown option '--setting-sources'", 1, nil, "claude: unknown option '--setting-sources'"},
		{"model error", `{"is_error":true,"result":"There's an issue with the selected model (x). It may not exist."}`, "", 1, nil,
			"claude: There's an issue with the selected model (x). It may not exist."},
		{"error flagged with exit 0", `{"is_error":true,"result":"API Error: 529 overloaded"}`, "", 0, nil, "claude: API Error: 529 overloaded"},
		{"empty answer", `{"is_error":false,"result":"  "}`, "", 0, nil, "claude returned an empty answer"},
		{"not json", "pong", "", 0, nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := testrepo.StubClaude(t, tt.stdout, tt.stderr, tt.code)
			_, err := New(stub.Bin).Ask(t.Context(), "prompt")
			if err == nil {
				t.Fatal("Ask() succeeded")
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Errorf("Ask() error = %v, want %v", err, tt.want)
			}
			if tt.msg != "" && err.Error() != tt.msg {
				t.Errorf("Error() = %q, want %q", err, tt.msg)
			}
		})
	}
}

func TestAskCommandError(t *testing.T) {
	stub := testrepo.StubClaude(t, "", "error: unknown option '--x'", 1)

	_, err := New(stub.Bin).Ask(t.Context(), "prompt")
	ce, ok := errors.AsType[*CommandError](err)
	if !ok {
		t.Fatalf("Ask() error = %T %v, want *CommandError", err, err)
	}
	if ce.ExitCode != 1 || ce.Args[0] != "-p" || !strings.Contains(ce.Stderr, "unknown option") {
		t.Errorf("CommandError = %+v", ce)
	}
}

func TestAskCanceled(t *testing.T) {
	stub := testrepo.StubClaude(t, fixtureAnswer, "", 0)
	c, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := New(stub.Bin).Ask(c, "prompt")
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Ask() error = %v, want context.Canceled", err)
	}
}

func TestFind(t *testing.T) {
	t.Run("on PATH", func(t *testing.T) {
		stub := testrepo.StubClaude(t, "", "", 0)
		client, err := Find()
		if err != nil || client.bin != stub.Bin {
			t.Fatalf("Find() = %+v, %v; want %s", client, err, stub.Bin)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := Find(); !errors.Is(err, ErrClaudeNotFound) {
			t.Fatalf("Find() error = %v, want ErrClaudeNotFound", err)
		}
	})
}
