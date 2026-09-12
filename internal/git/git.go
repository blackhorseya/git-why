// Package git runs the local git executable and turns its output into
// plain Go values. It never reimplements Git itself.
package git

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Commit is the metadata of a single commit.
type Commit struct {
	Hash        string
	ShortHash   string
	AuthorName  string
	AuthorEmail string
	AuthorDate  time.Time
	Subject     string
	Body        string
}

// BlameLine is the blame result for one line of the working tree file.
type BlameLine struct {
	// Hash is the commit that last changed the line.
	Hash string
	// OrigLine is the line's number in Hash's version of the file.
	OrigLine int
	// OrigPath is the file's repository-relative path in Hash, which differs
	// from the current path when the file was renamed later.
	OrigPath string
	// Boundary is set when Hash is where a shallow clone's history is cut
	// off, so older history of the line may be missing.
	Boundary bool
}

// Repo runs git commands against one repository. Every command runs from
// the top-level directory, so all paths in and out are repository-relative.
type Repo struct {
	bin  string
	root string
}

// Open finds the git executable and the repository that contains dir, and
// checks that the repository has at least one commit.
func Open(c context.Context, dir string) (*Repo, error) {
	bin, err := exec.LookPath("git")
	if err != nil {
		return nil, ErrGitNotFound
	}

	out, err := run(c, bin, dir, "rev-parse", "--show-toplevel")
	if err != nil {
		if ce, ok := errors.AsType[*CommandError](err); ok && ce.stderrContains("not a git repository") {
			return nil, fmt.Errorf("%w: %s", ErrNotRepository, dir)
		}
		return nil, err
	}

	i := &Repo{bin: bin, root: strings.TrimRight(out, "\n")}
	if _, err := i.git(c, "rev-parse", "--verify", "-q", "HEAD"); err != nil {
		if ce, ok := errors.AsType[*CommandError](err); ok && ce.ExitCode == 1 {
			return nil, ErrNoCommits
		}
		return nil, err
	}
	return i, nil
}

// Root returns the repository's top-level directory.
func (i *Repo) Root() string {
	return i.root
}

// TrackedPath returns the repository-relative path of the file at abs, or
// ErrUntracked when Git does not track it.
func (i *Repo) TrackedPath(c context.Context, abs string) (string, error) {
	// Ask git from the file's own directory and let it compute the
	// repository-relative name; this sidesteps symlinked temp dirs and
	// other path arithmetic pitfalls.
	out, err := run(c, i.bin, filepath.Dir(abs), "ls-files", "--full-name", "--error-unmatch", "-z", "--", filepath.Base(abs))
	if err != nil {
		if ce, ok := errors.AsType[*CommandError](err); ok && ce.ExitCode == 1 {
			return "", ErrUntracked
		}
		return "", err
	}

	names := parseNames(out)
	if len(names) == 0 {
		return "", ErrUntracked
	}
	return names[0], nil
}

// Blame returns the commit that last changed line n of path in the working
// tree, or ErrNotCommitted when the line has local changes.
func (i *Repo) Blame(c context.Context, path string, n int) (BlameLine, error) {
	out, err := i.git(c, "blame", "--porcelain", "-L", lineRange(n), "--", path)
	if err != nil {
		return BlameLine{}, err
	}

	b, err := parseBlame(out)
	if err != nil {
		return BlameLine{}, err
	}
	if isZeroHash(b.Hash) {
		return BlameLine{}, fmt.Errorf("%w: %s:%d", ErrNotCommitted, path, n)
	}

	// Blame flags every parentless commit as a boundary, including a normal
	// repository's first commit. Only in a shallow clone does it mean older
	// history is missing.
	if b.Boundary {
		out, err := i.git(c, "rev-parse", "--is-shallow-repository")
		if err != nil {
			return BlameLine{}, err
		}
		b.Boundary = strings.TrimSpace(out) == "true"
	}
	return b, nil
}

// Commit returns the metadata of a single commit.
func (i *Repo) Commit(c context.Context, hash string) (Commit, error) {
	out, err := i.git(c, "log", "-1", "-z", "--no-color", "--no-show-signature", "--format="+commitFormat, hash)
	if err != nil {
		return Commit{}, err
	}

	commits, err := parseCommits(out)
	if err != nil {
		return Commit{}, err
	}
	if len(commits) != 1 {
		return Commit{}, fmt.Errorf("expected 1 commit for %s, got %d", hash, len(commits))
	}
	return commits[0], nil
}

// ChangedFiles lists the repository-relative paths changed by a commit. For
// a merge commit it lists the changes relative to the first parent.
func (i *Repo) ChangedFiles(c context.Context, hash string) ([]string, error) {
	// diff-tree ignores --first-parent for merges and diffs against every
	// parent, so use log's --diff-merges instead.
	out, err := i.git(c, "log", "-1", "--diff-merges=first-parent", "-M", "--name-only", "-z", "--format=", hash)
	if err != nil {
		return nil, err
	}
	return parseNames(out), nil
}

// LineHistory returns up to limit commits, newest first, that changed line n
// of path, starting from commit from. Pass the values from a BlameLine so
// that n and path refer to that commit's version of the file.
func (i *Repo) LineHistory(c context.Context, path string, n int, from string, limit int) ([]Commit, error) {
	out, err := i.git(c, "log", "-z", "-s", "--no-color", "--no-show-signature",
		"--format="+commitFormat, "-n", strconv.Itoa(limit), "-L", lineRange(n)+":"+path, from)
	if err != nil {
		return nil, err
	}

	commits, err := parseCommits(out)
	if err != nil {
		return nil, err
	}
	if len(commits) == 0 {
		return nil, fmt.Errorf("%w: %s:%d", ErrNoHistory, path, n)
	}
	return commits, nil
}

// Remotes lists the names of the repository's remotes.
func (i *Repo) Remotes(c context.Context) ([]string, error) {
	out, err := i.git(c, "remote")
	if err != nil {
		return nil, err
	}
	return strings.Fields(out), nil
}

// RemoteURL returns the fetch URL of the named remote. Git applies any
// url.<base>.insteadOf rewriting, so the result is the URL actually used.
func (i *Repo) RemoteURL(c context.Context, name string) (string, error) {
	out, err := i.git(c, "remote", "get-url", "--", name)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func (i *Repo) git(c context.Context, args ...string) (string, error) {
	return run(c, i.bin, i.root, args...)
}

func run(c context.Context, bin, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(c, bin, args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"LC_ALL=C",                // stable, untranslated error messages
		"GIT_LITERAL_PATHSPECS=1", // treat '*' and '[' in file names literally
		"GIT_OPTIONAL_LOCKS=0",    // never contend with a concurrent git in an editor
		"GIT_PAGER=cat",
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		ce := &CommandError{Args: args, ExitCode: -1, Stderr: strings.TrimSpace(stderr.String()), Err: err}
		if ee, ok := errors.AsType[*exec.ExitError](err); ok {
			ce.ExitCode = ee.ExitCode()
		}
		return "", ce
	}
	return stdout.String(), nil
}

func lineRange(n int) string {
	return fmt.Sprintf("%d,%d", n, n)
}
