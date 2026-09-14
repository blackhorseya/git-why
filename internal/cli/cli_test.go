package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/blackhorseya/git-why/internal/testrepo"
)

type result struct {
	code   int
	stdout string
	stderr string
}

func run(t *testing.T, args ...string) result {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := Run(t.Context(), "v1.2.3", args, &stdout, &stderr)
	return result{code: code, stdout: stdout.String(), stderr: stderr.String()}
}

// paymentRepo builds a small history for internal/payment/service.go where
// line 3 is changed by three commits and line 1 by an unrelated fourth one.
func paymentRepo(t *testing.T) (*testrepo.Repo, []string) {
	t.Helper()
	r := testrepo.New(t)

	r.Write("internal/payment/service.go", "package payment\n\nconst ttl = 1\n")
	initial := r.Commit("feat: initial payment service")

	r.Write("internal/payment/service.go", "package payment\n\nconst ttl = 24\n")
	ttl := r.Commit("chore: change Redis TTL to 24h")

	r.Write("internal/payment/service.go", "package payment\n\nif err := repository.Exists(ctx, id); err != nil {\n")
	r.Write("internal/payment/repository.go", "package payment\n")
	r.Write("internal/payment/service_test.go", "package payment\n")
	fix := r.Commit("fix: prevent duplicate payment processing\n\nRetries from the gateway could charge twice.")

	r.Write("internal/payment/service.go", "package payment // v2\n\nif err := repository.Exists(ctx, id); err != nil {\n")
	r.Commit("docs: tag package")

	return r, []string{fix, ttl, initial}
}

func TestRunShallowClone(t *testing.T) {
	r, _ := paymentRepo(t)
	clone := filepath.Join(t.TempDir(), "clone")
	r.Git("clone", "-q", "--depth", "1", "file://"+r.Dir, clone)
	t.Chdir(clone)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	assertInOrder(t, res.stdout,
		"Changed with",
		"  (shallow clone: parent commit missing, changed files unknown)",
		"Line history",
		"  (shallow clone: older history may be missing)",
	)
	// Without a parent, git would diff against the empty tree and list
	// every file in the repository.
	if strings.Contains(res.stdout, "repository.go") {
		t.Errorf("Changed with listed the whole tree of a shallow clone:\n%s", res.stdout)
	}
}

func TestRunExplainsLine(t *testing.T) {
	r, commits := paymentRepo(t)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}

	fix, ttl, initial := r.Short(commits[0]), r.Short(commits[1]), r.Short(commits[2])
	assertInOrder(t, res.stdout,
		"Why does this line exist?",
		"Current line  internal/payment/service.go:3",
		"  if err := repository.Exists(ctx, id); err != nil {",
		"Introduced / Changed",
		"  Commit: "+fix,
		"  Author: Ada Lovelace <ada@example.com>",
		"  Date:   2026-01-03",
		"  fix: prevent duplicate payment processing",
		"  Retries from the gateway could charge twice.",
		"Changed with",
		"  internal/payment/repository.go",
		"  internal/payment/service_test.go",
		"Line history",
		"  "+fix+"  2026-01-03  fix: prevent duplicate payment processing",
		"  "+ttl+"  2026-01-02  chore: change Redis TTL to 24h",
		"  "+initial+"  2026-01-01  feat: initial payment service",
	)
	if strings.Contains(res.stdout, "docs: tag package") {
		t.Errorf("history includes a commit that did not touch the line:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "Changed with\n  internal/payment/service.go") {
		t.Errorf("Changed with lists the target file itself:\n%s", res.stdout)
	}
	if strings.Contains(res.stdout, "Pull request") {
		t.Errorf("a repository without remotes shows a pull request section:\n%s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("unexpected stderr: %s", res.stderr)
	}
}

func TestRunFromSubdirectory(t *testing.T) {
	r, commits := paymentRepo(t)
	t.Chdir(r.Path("internal"))

	res := run(t, "payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	assertInOrder(t, res.stdout,
		"Current line  internal/payment/service.go:3",
		"  Commit: "+r.Short(commits[0]),
		"Line history",
		"  "+r.Short(commits[2])+"  2026-01-01  feat: initial payment service",
	)
}

func TestRunAfterRenameWithLocalEdits(t *testing.T) {
	r, commits := paymentRepo(t)
	r.Git("mv", "internal/payment/service.go", "internal/payment/payment.go")
	r.Commit("refactor: rename service.go")
	// Uncommitted lines above the target shift its working-tree number to 5.
	r.Write("internal/payment/payment.go",
		"// local note\n// another\npackage payment // v2\n\nif err := repository.Exists(ctx, id); err != nil {\n")
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/payment.go:5")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	assertInOrder(t, res.stdout,
		"  Commit: "+r.Short(commits[0]),
		"  Path:   internal/payment/service.go (renamed since)",
		"Line history",
		"  "+r.Short(commits[0]),
		"  "+r.Short(commits[1]),
		"  "+r.Short(commits[2]),
	)
}

func TestRunTruncatesLongHistory(t *testing.T) {
	r := testrepo.New(t)
	for i := range historyLimit + 2 {
		r.Write("f.txt", fmt.Sprintf("v%d\n", i))
		r.Commit(fmt.Sprintf("set v%d", i))
	}
	t.Chdir(r.Dir)

	res := run(t, "f.txt:1")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	_, hist, _ := strings.Cut(res.stdout, "Line history\n")
	if n := strings.Count(hist, "  set v"); n != historyLimit {
		t.Errorf("history shows %d commits, want %d:\n%s", n, historyLimit, res.stdout)
	}
	if !strings.Contains(res.stdout, "… more: git log -L 1,1:f.txt ") {
		t.Errorf("truncated history has no hint:\n%s", res.stdout)
	}
}

func TestVersionAndHelp(t *testing.T) {
	if res := run(t, "--version"); res.code != exitOK || res.stdout != "git-why v1.2.3\n" {
		t.Errorf("--version = %d %q", res.code, res.stdout)
	}

	res := run(t, "--help")
	if res.code != exitOK {
		t.Fatalf("--help exit code = %d", res.code)
	}
	for _, want := range []string{"git blame tells you who. git-why tells you why.", "git-why <file>:<line>", "--version", "--offline", "ask "} {
		if !strings.Contains(res.stdout, want) {
			t.Errorf("--help missing %q:\n%s", want, res.stdout)
		}
	}
	if strings.Contains(res.stdout, "completion") {
		t.Errorf("--help advertises a completion command:\n%s", res.stdout)
	}
}

func TestErrorMessages(t *testing.T) {
	r := testrepo.New(t)
	r.Write("tracked.go", "package x\n")
	r.Commit("init")
	r.Write("untracked.go", "package x\n")
	t.Chdir(r.Dir)

	res := run(t, "untracked.go:1")
	want := "git-why: untracked.go: file is not tracked by git\nhint: "
	if !strings.HasPrefix(res.stderr, want) {
		t.Errorf("stderr = %q, want prefix %q", res.stderr, want)
	}
	if res.stdout != "" {
		t.Errorf("stdout on error = %q", res.stdout)
	}

	res = run(t, "tracked.go:9")
	if !strings.Contains(res.stderr, "tracked.go has 1 lines, requested line 9") {
		t.Errorf("stderr = %q", res.stderr)
	}
}

// TestExitCodes pins the exit-code table documented in the README.
func TestExitCodes(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T) []string
		want  int
	}{
		{"no arguments", func(t *testing.T) []string { return nil }, exitUsage},
		{"too many arguments", func(t *testing.T) []string { return []string{"a.go:1", "b.go:2"} }, exitUsage},
		{"unknown flag", func(t *testing.T) []string { return []string{"--bogus", "a.go:1"} }, exitUsage},
		{"missing line", func(t *testing.T) []string { return []string{"main.go"} }, exitUsage},
		{"line zero", func(t *testing.T) []string { return []string{"main.go:0"} }, exitUsage},
		{"line not a number", func(t *testing.T) []string { return []string{"main.go:abc"} }, exitUsage},

		{"not a repository", func(t *testing.T) []string {
			testrepo.New(t) // isolate git config
			dir := t.TempDir()
			writeFile(t, filepath.Join(dir, "f.go"), "x\n")
			return []string{filepath.Join(dir, "f.go") + ":1"}
		}, exitEnv},
		{"git not installed", func(t *testing.T) []string {
			r := committedRepo(t)
			t.Setenv("PATH", t.TempDir())
			return []string{r.Path("f.go") + ":1"}
		}, exitEnv},

		{"file does not exist", func(t *testing.T) []string {
			r := committedRepo(t)
			return []string{r.Path("nope.go") + ":1"}
		}, exitTarget},
		{"path is a directory", func(t *testing.T) []string {
			r := committedRepo(t)
			return []string{r.Dir + ":1"}
		}, exitTarget},
		{"line out of range", func(t *testing.T) []string {
			r := committedRepo(t)
			return []string{r.Path("f.go") + ":3"}
		}, exitTarget},
		{"untracked file", func(t *testing.T) []string {
			r := committedRepo(t)
			r.Write("new.go", "x\n")
			return []string{r.Path("new.go") + ":1"}
		}, exitTarget},

		{"repository without commits", func(t *testing.T) []string {
			r := testrepo.New(t)
			r.Write("f.go", "x\n")
			r.Git("add", "f.go")
			return []string{r.Path("f.go") + ":1"}
		}, exitFailure},
		{"uncommitted line", func(t *testing.T) []string {
			r := committedRepo(t)
			r.Write("f.go", "x\ny\nlocal\n")
			return []string{r.Path("f.go") + ":3"}
		}, exitFailure},
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
		})
	}
}

func committedRepo(t *testing.T) *testrepo.Repo {
	t.Helper()
	r := testrepo.New(t)
	r.Write("f.go", "x\ny\n")
	r.Commit("init")
	return r
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// assertInOrder checks that each want string appears in s after the
// previous one.
func assertInOrder(t *testing.T, s string, wants ...string) {
	t.Helper()
	rest := s
	for _, w := range wants {
		i := strings.Index(rest, w)
		if i < 0 {
			t.Fatalf("output missing %q (or out of order) in:\n%s", w, s)
		}
		rest = rest[i+len(w):]
	}
}
