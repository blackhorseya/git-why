package git_test

import (
	"errors"
	"slices"
	"testing"

	"github.com/blackhorseya/git-why/internal/git"
	"github.com/blackhorseya/git-why/internal/testrepo"
)

func TestRemotes(t *testing.T) {
	r := testrepo.New(t)
	r.Write("f.txt", "x\n")
	r.Commit("init")
	repo, err := git.Open(t.Context(), r.Dir)
	if err != nil {
		t.Fatal(err)
	}

	names, err := repo.Remotes(t.Context())
	if err != nil || len(names) != 0 {
		t.Fatalf("Remotes() on a repo without remotes = %v, %v", names, err)
	}

	r.Git("remote", "add", "origin", "git@github.com:acme/pay.git")
	r.Git("remote", "add", "upstream", "https://github.com/acme/upstream.git")
	r.Git("config", "url.https://github.com/.insteadOf", "gh:")
	r.Git("remote", "add", "mirror", "gh:acme/mirror.git")

	names, err = repo.Remotes(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(names)
	if want := []string{"mirror", "origin", "upstream"}; !slices.Equal(names, want) {
		t.Fatalf("Remotes() = %v, want %v", names, want)
	}

	for name, want := range map[string]string{
		"origin": "git@github.com:acme/pay.git",
		"mirror": "https://github.com/acme/mirror.git", // insteadOf applied
	} {
		got, err := repo.RemoteURL(t.Context(), name)
		if err != nil {
			t.Fatalf("RemoteURL(%s) unexpected error: %v", name, err)
		}
		if got != want {
			t.Errorf("RemoteURL(%s) = %q, want %q", name, got, want)
		}
	}

	_, err = repo.RemoteURL(t.Context(), "nope")
	if _, ok := errors.AsType[*git.CommandError](err); !ok {
		t.Fatalf("RemoteURL(nope) error = %v, want *CommandError", err)
	}
}
