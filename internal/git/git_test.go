package git_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	"github.com/blackhorseya/git-why/internal/git"
	"github.com/blackhorseya/git-why/internal/testrepo"
)

func TestOpen(t *testing.T) {
	t.Run("repository root", func(t *testing.T) {
		r := testrepo.New(t)
		r.Write("sub/f.txt", "x\n")
		r.Commit("init")

		repo, err := git.Open(t.Context(), r.Path("sub"))
		if err != nil {
			t.Fatalf("Open() unexpected error: %v", err)
		}
		want, _ := filepath.EvalSymlinks(r.Dir)
		if repo.Root() != want {
			t.Fatalf("Root() = %q, want %q", repo.Root(), want)
		}
	})

	t.Run("not a repository", func(t *testing.T) {
		testrepo.New(t) // isolates git config for this test
		_, err := git.Open(t.Context(), t.TempDir())
		if !errors.Is(err, git.ErrNotRepository) {
			t.Fatalf("Open() error = %v, want ErrNotRepository", err)
		}
	})

	t.Run("no commits", func(t *testing.T) {
		r := testrepo.New(t)
		_, err := git.Open(t.Context(), r.Dir)
		if !errors.Is(err, git.ErrNoCommits) {
			t.Fatalf("Open() error = %v, want ErrNoCommits", err)
		}
	})

	t.Run("git not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		_, err := git.Open(t.Context(), t.TempDir())
		if !errors.Is(err, git.ErrGitNotFound) {
			t.Fatalf("Open() error = %v, want ErrGitNotFound", err)
		}
	})
}

func TestTrackedPath(t *testing.T) {
	r := testrepo.New(t)
	r.Write("internal/pay/service.go", "package pay\n")
	r.Write("docs/說明.md", "hello\n")
	r.Write("glob[1].txt", "literal\n")
	r.Commit("init")
	r.Write("internal/pay/new.go", "package pay\n")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}

	for _, rel := range []string{"internal/pay/service.go", "docs/說明.md", "glob[1].txt"} {
		got, err := repo.TrackedPath(t.Context(), r.Path(rel))
		if err != nil {
			t.Fatalf("TrackedPath(%s) unexpected error: %v", rel, err)
		}
		if got != rel {
			t.Fatalf("TrackedPath(%s) = %q", rel, got)
		}
	}

	if _, err := repo.TrackedPath(t.Context(), r.Path("internal/pay/new.go")); !errors.Is(err, git.ErrUntracked) {
		t.Fatalf("TrackedPath(untracked) error = %v, want ErrUntracked", err)
	}
}

func TestBlameFollowsRenamesAndLocalEdits(t *testing.T) {
	r := testrepo.New(t)
	r.Write("a.txt", "x\ny\n")
	first := r.Commit("feat: add a")
	r.Write("a.txt", "X\ny\n")
	second := r.Commit("fix: capitalise x")
	r.Git("mv", "a.txt", "b.txt")
	r.Commit("refactor: rename a to b")

	// An uncommitted insertion above the line shifts its working-tree number.
	r.Write("b.txt", "new\nX\ny\n")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}

	b, err := repo.Blame(t.Context(), "b.txt", 2)
	if err != nil {
		t.Fatalf("Blame() unexpected error: %v", err)
	}
	want := git.BlameLine{Hash: second, OrigLine: 1, OrigPath: "a.txt"}
	if b != want {
		t.Fatalf("Blame() = %+v, want %+v", b, want)
	}

	history, err := repo.LineHistory(t.Context(), b.OrigPath, b.OrigLine, b.Hash, 10)
	if err != nil {
		t.Fatalf("LineHistory() unexpected error: %v", err)
	}
	if got := hashes(history); !slices.Equal(got, []string{second, first}) {
		t.Fatalf("LineHistory() = %v, want [%s %s]", got, second, first)
	}

	if _, err := repo.Blame(t.Context(), "b.txt", 1); !errors.Is(err, git.ErrNotCommitted) {
		t.Fatalf("Blame(uncommitted line) error = %v, want ErrNotCommitted", err)
	}
}

func TestBlameNonASCIIPath(t *testing.T) {
	r := testrepo.New(t)
	r.Write("docs/說明.md", "hello\n")
	hash := r.Commit("docs: add readme")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	b, err := repo.Blame(t.Context(), "docs/說明.md", 1)
	if err != nil {
		t.Fatalf("Blame() unexpected error: %v", err)
	}
	// A normal repository's first commit is not a shallow boundary.
	if b.Hash != hash || b.OrigPath != "docs/說明.md" || b.Boundary {
		t.Fatalf("Blame() = %+v", b)
	}
}

func TestBlameMarksShallowBoundary(t *testing.T) {
	r := testrepo.New(t)
	r.Write("f.txt", "a\nb\n")
	r.Commit("init")
	r.Write("f.txt", "a\nB\n")
	r.Commit("change b")

	clone := filepath.Join(t.TempDir(), "clone")
	r.Git("clone", "-q", "--depth", "1", "file://"+r.Dir, clone)

	repo, err := git.Open(t.Context(), clone)
	if err != nil {
		t.Fatal(err)
	}
	// Line 1 was written by "init", which the shallow clone cut off.
	b, err := repo.Blame(t.Context(), "f.txt", 1)
	if err != nil {
		t.Fatalf("Blame() unexpected error: %v", err)
	}
	if !b.Boundary {
		t.Fatalf("Blame() = %+v, want Boundary in a shallow clone", b)
	}
}

func TestLineHistoryLimit(t *testing.T) {
	r := testrepo.New(t)
	var commits []string
	for _, v := range []string{"1", "2", "3", "4"} {
		r.Write("f.txt", v+"\n")
		commits = append(commits, r.Commit("set "+v))
	}

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	history, err := repo.LineHistory(t.Context(), "f.txt", 1, commits[3], 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := hashes(history); !slices.Equal(got, []string{commits[3], commits[2]}) {
		t.Fatalf("LineHistory(limit 2) = %v", got)
	}
}

func TestCommit(t *testing.T) {
	r := testrepo.New(t)
	r.Write("f.txt", "x\n")
	r.Commit("init")
	r.Write("f.txt", "y\n")
	hash := r.Commit("fix: prevent duplicate payment processing\n\nRetries could charge twice.\n\nCheck the key first.")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	got, err := repo.Commit(t.Context(), hash)
	if err != nil {
		t.Fatalf("Commit() unexpected error: %v", err)
	}

	if got.Hash != hash || got.ShortHash != r.Short(hash) {
		t.Errorf("Commit() hash = %s / %s", got.Hash, got.ShortHash)
	}
	if got.AuthorName != "Ada Lovelace" || got.AuthorEmail != "ada@example.com" {
		t.Errorf("Commit() author = %s <%s>", got.AuthorName, got.AuthorEmail)
	}
	if want := testrepo.Epoch.AddDate(0, 0, 1); !got.AuthorDate.Equal(want) {
		t.Errorf("Commit() date = %v, want %v", got.AuthorDate, want)
	}
	if got.Subject != "fix: prevent duplicate payment processing" {
		t.Errorf("Commit() subject = %q", got.Subject)
	}
	if got.Body != "Retries could charge twice.\n\nCheck the key first." {
		t.Errorf("Commit() body = %q", got.Body)
	}
}

func TestChangedFiles(t *testing.T) {
	r := testrepo.New(t)
	r.Write("service.go", "a\n")
	root := r.Commit("init")
	r.Write("service.go", "b\n")
	r.Write("repository.go", "r\n")
	r.Write("docs/說明.md", "d\n")
	change := r.Commit("change several files")

	r.Git("checkout", "-q", "-b", "feature")
	r.Write("feature.go", "f\n")
	r.Commit("add feature")
	r.Git("checkout", "-q", "main")
	r.Write("hotfix.go", "h\n")
	r.Commit("add hotfix")
	r.Git("merge", "-q", "--no-edit", "feature")
	merge := r.Git("rev-parse", "HEAD")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}

	tests := map[string]struct {
		hash string
		want []string
	}{
		"root commit":  {root, []string{"service.go"}},
		"regular":      {change, []string{"docs/說明.md", "repository.go", "service.go"}},
		"merge commit": {merge, []string{"feature.go"}},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := repo.ChangedFiles(t.Context(), tt.hash)
			if err != nil {
				t.Fatalf("ChangedFiles() unexpected error: %v", err)
			}
			if !slices.Equal(got, tt.want) {
				t.Fatalf("ChangedFiles() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestCommandError(t *testing.T) {
	r := testrepo.New(t)
	r.Write("f.txt", "x\n")
	r.Commit("init")

	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = repo.Commit(t.Context(), "does-not-exist")
	ce, ok := errors.AsType[*git.CommandError](err)
	if !ok {
		t.Fatalf("Commit(bad ref) error = %v, want *CommandError", err)
	}
	if ce.ExitCode == 0 || ce.Stderr == "" {
		t.Fatalf("CommandError = %+v", ce)
	}
}

func hashes(commits []git.Commit) []string {
	out := make([]string, len(commits))
	for i, c := range commits {
		out[i] = c.Hash
	}
	return out
}
