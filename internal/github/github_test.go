package github

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/blackhorseya/git-why/internal/testrepo"
)

const testHash = "8ac912f00000000000000000000000000000000a"

var testRemote = Remote{Host: "github.com", Owner: "acme", Name: "pay"}

func TestPullRequest(t *testing.T) {
	stub := testrepo.StubGH(t, fixtureMerged, "", 0)

	pr, err := New(stub.Bin).PullRequest(t.Context(), testRemote, testHash)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Number != 42 || pr.Author != "ada" || len(pr.Issues) != 1 || len(pr.Threads) != 2 {
		t.Fatalf("PullRequest() = %+v", pr)
	}

	args := stub.Args()
	for _, want := range []string{"api", "graphql", "--hostname", "github.com", "owner=acme", "name=pay", "oid=" + testHash} {
		if !slices.Contains(args, want) {
			t.Errorf("gh was not called with %q: %q", want, args)
		}
	}
	if i := slices.Index(args, "query="+pullRequestQuery); i < 0 || args[i-1] != "-f" {
		t.Errorf("query was not passed as a raw -f field: %q", args)
	}
}

func TestPullRequestErrors(t *testing.T) {
	notFound := `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`
	tests := []struct {
		name   string
		stdout string
		stderr string
		code   int
		want   error
	}{
		{"not logged in", "", "To get started with GitHub CLI, please run:  gh auth login", 4, ErrNotLoggedIn},
		{"bad credentials", `{"message":"Bad credentials","status":"401"}`, "gh: Bad credentials (HTTP 401)", 1, ErrBadCredentials},
		{"rate limited", "", "gh: API rate limit exceeded for user ID 1 (HTTP 403)", 1, ErrRateLimited},
		{"repository not visible", notFound, "gh: Could not resolve to a Repository with the name 'acme/pay'.", 1, ErrNotFound},
		{"commit not on GitHub", `{"data":{"repository":{"object":null}}}`, "", 0, ErrCommitNotFound},
		{"no pull request", `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`, "", 0, ErrNoPullRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := testrepo.StubGH(t, tt.stdout, tt.stderr, tt.code)
			_, err := New(stub.Bin).PullRequest(t.Context(), testRemote, testHash)
			if !errors.Is(err, tt.want) {
				t.Fatalf("PullRequest() error = %v, want %v", err, tt.want)
			}
			if !strings.HasPrefix(err.Error(), "acme/pay@8ac912f: ") {
				t.Errorf("error %q does not name the repository and commit", err)
			}
		})
	}
}

func TestPullRequestCommandError(t *testing.T) {
	stub := testrepo.StubGH(t, "", "error connecting to api.github.com\ncheck your internet connection", 1)

	_, err := New(stub.Bin).PullRequest(t.Context(), testRemote, testHash)
	ce, ok := errors.AsType[*CommandError](err)
	if !ok {
		t.Fatalf("PullRequest() error = %T %v, want *CommandError", err, err)
	}
	if ce.ExitCode != 1 || ce.Args[0] != "api" {
		t.Errorf("CommandError = %+v", ce)
	}
	if want := "acme/pay@8ac912f: gh api: error connecting to api.github.com"; err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestPullRequestCanceled(t *testing.T) {
	stub := testrepo.StubGH(t, fixtureMerged, "", 0)
	c, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := New(stub.Bin).PullRequest(c, testRemote, testHash)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("PullRequest() error = %v, want context.Canceled", err)
	}
}

func TestFind(t *testing.T) {
	t.Run("on PATH", func(t *testing.T) {
		stub := testrepo.StubGH(t, "", "", 0)
		client, err := Find()
		if err != nil || client.bin != stub.Bin {
			t.Fatalf("Find() = %+v, %v; want %s", client, err, stub.Bin)
		}
	})
	t.Run("not installed", func(t *testing.T) {
		t.Setenv("PATH", t.TempDir())
		if _, err := Find(); !errors.Is(err, ErrGHNotFound) {
			t.Fatalf("Find() error = %v, want ErrGHNotFound", err)
		}
	})
}
