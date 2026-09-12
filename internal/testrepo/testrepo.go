// Package testrepo builds throwaway Git repositories for integration tests.
package testrepo

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Epoch is the author date of the first commit made through Repo.Commit.
// Each later commit is one day after the previous one.
var Epoch = time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)

// Repo is a temporary Git repository with deterministic identity and dates.
type Repo struct {
	t       testing.TB
	Dir     string
	commits int
}

// New initialises an empty repository in a fresh temp directory. It skips
// the test when git is not installed, and isolates every git process in the
// test (including those started by the code under test) from the user's
// global and system Git configuration.
func New(t testing.TB) *Repo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git executable not found on PATH")
	}

	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Ada Lovelace")
	t.Setenv("GIT_AUTHOR_EMAIL", "ada@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Ada Lovelace")
	t.Setenv("GIT_COMMITTER_EMAIL", "ada@example.com")

	x := &Repo{t: t, Dir: t.TempDir()}
	x.Git("init", "-q", "-b", "main")
	return x
}

// Git runs a git command in the repository and returns its trimmed stdout.
func (x *Repo) Git(args ...string) string {
	x.t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = x.Dir
	out, err := cmd.Output()
	if err != nil {
		var stderr string
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			stderr = string(ee.Stderr)
		}
		x.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, stderr)
	}
	return strings.TrimSpace(string(out))
}

// Write creates or replaces a file, creating parent directories as needed.
func (x *Repo) Write(path, content string) {
	x.t.Helper()
	full := filepath.Join(x.Dir, path)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		x.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		x.t.Fatal(err)
	}
}

// Commit stages every change and commits it with the given message. The
// commit's date advances one day per call, so history order is stable.
// It returns the full hash of the new commit.
func (x *Repo) Commit(message string) string {
	x.t.Helper()
	when := Epoch.AddDate(0, 0, x.commits).Format(time.RFC3339)
	x.commits++

	x.t.Setenv("GIT_AUTHOR_DATE", when)
	x.t.Setenv("GIT_COMMITTER_DATE", when)
	x.Git("add", "-A")
	x.Git("commit", "-q", "--no-verify", "-m", message)
	return x.Git("rev-parse", "HEAD")
}

// Short returns the abbreviated form of a commit hash as git prints it.
func (x *Repo) Short(hash string) string {
	x.t.Helper()
	return x.Git("rev-parse", "--short", hash)
}

// Path returns the absolute path of a file inside the repository.
func (x *Repo) Path(rel string) string {
	return filepath.Join(x.Dir, rel)
}

// String implements fmt.Stringer for nicer test failure output.
func (x *Repo) String() string {
	return fmt.Sprintf("testrepo(%s)", x.Dir)
}
