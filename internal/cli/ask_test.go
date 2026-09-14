package cli

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blackhorseya/git-why/internal/testrepo"
)

// answerFixture is claude's JSON output for a successful answer.
const answerFixture = `{"type":"result","subtype":"success","is_error":false,
"result":"The check exists because gateway retries could charge twice; the pull request looks the payment up first."}`

const notLoggedInFixture = `{"type":"result","subtype":"success","is_error":true,"result":"Not logged in · Please run /login"}`

func TestRunAsk(t *testing.T) {
	r, commits := githubRepo(t)
	gh := testrepo.StubGH(t, pullRequestFixture, "", 0)
	claude := testrepo.StubClaude(t, answerFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "ask", "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	assertInOrder(t, res.stdout,
		"Why does internal/payment/service.go:3 exist?",
		"  The check exists because gateway retries could charge twice; the pull request",
		"  looks the payment up first.",
		"Sources  "+r.Short(commits[0])+" · #42 https://github.com/acme/pay/pull/42",
	)
	if strings.Contains(res.stdout, "Line history") {
		t.Errorf("ask printed the full report:\n%s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("unexpected stderr: %s", res.stderr)
	}

	// The prompt is the report the user would otherwise see, in full.
	prompt := claude.Stdin()
	for _, want := range []string{
		"Why does this line exist?",
		"Current line  internal/payment/service.go:3",
		"fix: prevent duplicate payment processing",
		"Retries from the gateway could charge twice.",
		"#42  fix: prevent duplicate payment processing",
		"Closes #38",
		"Should this be a 409 instead of 500?",
		"Line history",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt lacks %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "\x1b[") {
		t.Errorf("prompt carries ANSI escapes:\n%q", prompt)
	}
	if gh.Calls() != 1 || claude.Calls() != 1 {
		t.Errorf("gh ran %d times, claude %d; want 1 and 1", gh.Calls(), claude.Calls())
	}
}

func TestRunAskOffline(t *testing.T) {
	r, commits := githubRepo(t)
	gh := testrepo.StubGH(t, pullRequestFixture, "", 0)
	claude := testrepo.StubClaude(t, answerFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "ask", "--offline", "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	if !strings.HasSuffix(res.stdout, "Sources  "+r.Short(commits[0])+"\n") {
		t.Errorf("offline answer should cite the commit only:\n%s", res.stdout)
	}
	if gh.Calls() != 0 {
		t.Errorf("gh ran %d times with --offline", gh.Calls())
	}
	if strings.Contains(claude.Stdin(), "Pull request") {
		t.Errorf("offline prompt mentions a pull request:\n%s", claude.Stdin())
	}
}

func TestRunAskExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) []string
		want  int
		hint  string
	}{
		{"no target", func(t *testing.T) []string {
			testrepo.New(t)
			return []string{"ask"}
		}, exitUsage, "usage: git-why ask <file>:<line>"},
		{"claude not installed", func(t *testing.T) []string {
			r, _ := githubRepo(t)
			gitOnlyPATH(t)
			t.Chdir(r.Dir)
			return []string{"ask", "internal/payment/service.go:3"}
		}, exitEnv, "install Claude Code"},
		{"claude not logged in", func(t *testing.T) []string {
			r, _ := githubRepo(t)
			testrepo.StubGH(t, pullRequestFixture, "", 0)
			testrepo.StubClaude(t, notLoggedInFixture, "", 1)
			t.Chdir(r.Dir)
			return []string{"ask", "internal/payment/service.go:3"}
		}, exitEnv, "/login"},
		{"claude failed", func(t *testing.T) []string {
			r, _ := githubRepo(t)
			testrepo.StubGH(t, pullRequestFixture, "", 0)
			testrepo.StubClaude(t, `{"is_error":true,"result":"API Error: 529 overloaded"}`, "", 1)
			t.Chdir(r.Dir)
			return []string{"ask", "internal/payment/service.go:3"}
		}, exitFailure, ""},
		{"target errors come first", func(t *testing.T) []string {
			r, _ := githubRepo(t)
			testrepo.StubClaude(t, answerFixture, "", 0)
			t.Chdir(r.Dir)
			return []string{"ask", "nope.go:1"}
		}, exitTarget, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			res := run(t, tt.setup(t)...)
			if res.code != tt.want {
				t.Errorf("exit code = %d, want %d; stderr:\n%s", res.code, tt.want, res.stderr)
			}
			if !strings.HasPrefix(res.stderr, "git-why: ") {
				t.Errorf("stderr = %q, want a git-why: prefix", res.stderr)
			}
			if tt.hint != "" && !strings.Contains(res.stderr, tt.hint) {
				t.Errorf("stderr lacks %q:\n%s", tt.hint, res.stderr)
			}
			if res.stdout != "" {
				t.Errorf("unexpected stdout:\n%s", res.stdout)
			}
		})
	}
}

func TestRunAskTimeout(t *testing.T) {
	r, _ := githubRepo(t)
	testrepo.StubGH(t, pullRequestFixture, "", 0)
	slowClaude(t)
	old := askTimeout
	askTimeout = 200 * time.Millisecond
	t.Cleanup(func() { askTimeout = old })
	t.Chdir(r.Dir)

	start := time.Now()
	res := run(t, "ask", "internal/payment/service.go:3")
	if took := time.Since(start); took > 5*time.Second {
		t.Errorf("ask took %s; the timeout did not bound the claude call", took)
	}
	if res.code != exitFailure {
		t.Errorf("exit code = %d, want %d; stderr:\n%s", res.code, exitFailure, res.stderr)
	}
	if want := "git-why: claude did not answer within 200ms\n"; res.stderr != want {
		t.Errorf("stderr = %q, want %q", res.stderr, want)
	}
}

// slowClaude puts a claude on PATH that never answers. exec makes the
// timeout's kill land on the sleeping process itself, not a shell wrapper.
func slowClaude(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claude"), []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestAskHelp(t *testing.T) {
	res := run(t, "ask", "--help")
	if res.code != exitOK {
		t.Fatalf("ask --help exit code = %d", res.code)
	}
	for _, want := range []string{"git-why ask <file>:<line>", "Claude Code", "--offline"} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("ask --help missing %q:\n%s", want, res.stdout)
		}
	}
	if !slices.Contains(strings.Fields(run(t, "--help").stdout), "ask") {
		t.Errorf("root --help does not list the ask command")
	}
}
