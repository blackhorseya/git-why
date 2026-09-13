// Package testrepo builds throwaway Git repositories and a fake gh
// executable for integration tests.
package testrepo

import (
	"errors"
	"fmt"
	"io/fs"
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
// the test when git is not installed, and isolates every git and gh process
// in the test (including those started by the code under test) from the
// user's configuration and accounts.
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

	// With no config directory and no token, a real gh exits 4 ("not logged
	// in") before touching the network, so a test that forgets StubGH can
	// never reach GitHub with the developer's credentials.
	t.Setenv("GH_CONFIG_DIR", t.TempDir())
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("GH_ENTERPRISE_TOKEN", "")
	t.Setenv("GITHUB_ENTERPRISE_TOKEN", "")
	t.Setenv("GH_HOST", "")

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

// GHStub is a fake gh executable that replays one canned response.
type GHStub struct {
	// Bin is the path of the fake executable.
	Bin      string
	argsFile string
}

// StubGH installs a fake gh at the front of PATH that writes stdout and
// stderr and exits with code, whatever it is asked. Inspect what it was
// asked with Args.
func StubGH(t testing.TB, stdout, stderr string, code int) *GHStub {
	t.Helper()
	dir := t.TempDir()
	outFile := filepath.Join(dir, "stdout")
	errFile := filepath.Join(dir, "stderr")
	argsFile := filepath.Join(dir, "args")
	for path, content := range map[string]string{outFile: stdout, errFile: stderr} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	script := fmt.Sprintf("#!/bin/sh\nprintf '%%s\\0' \"$@\" > %q\ncat %q\ncat %q >&2\nexit %d\n",
		argsFile, outFile, errFile, code)
	bin := filepath.Join(dir, "gh")
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return &GHStub{Bin: bin, argsFile: argsFile}
}

// Args returns the arguments of the last call to the stub, or nil when it
// never ran.
func (x *GHStub) Args() []string {
	b, err := os.ReadFile(x.argsFile)
	if errors.Is(err, fs.ErrNotExist) || len(b) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSuffix(string(b), "\x00"), "\x00")
}
