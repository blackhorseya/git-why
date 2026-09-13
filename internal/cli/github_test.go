package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blackhorseya/git-why/internal/testrepo"
)

// pullRequestFixture is gh's answer for a commit merged through PR #42,
// which closed issue #38 and drew one review thread on the target file and
// one on another file.
const pullRequestFixture = `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[
{"number":42,"title":"fix: prevent duplicate payment processing","state":"MERGED",
 "mergedAt":"2026-01-04T09:00:00Z","url":"https://github.com/acme/pay/pull/42",
 "body":"<!-- template -->\n## Summary\n\nRetries from the gateway could charge twice.\n",
 "author":{"login":"ada"},
 "closingIssuesReferences":{"nodes":[
  {"number":38,"title":"Duplicate charges on gateway retry","url":"https://github.com/acme/pay/issues/38","state":"CLOSED"}]},
 "reviewThreads":{"nodes":[
  {"path":"internal/payment/service.go","line":3,"isResolved":true,"isOutdated":false,
   "comments":{"totalCount":3,"nodes":[{"body":"Should this be a 409 instead of 500?","url":"https://github.com/acme/pay/pull/42#discussion_r1","author":{"login":"grace"}}]}},
  {"path":"internal/payment/repository.go","line":9,"isResolved":false,"isOutdated":false,
   "comments":{"totalCount":1,"nodes":[{"body":"nit: rename","url":"https://github.com/acme/pay/pull/42#discussion_r2","author":{"login":"grace"}}]}}
 ]}}
]}}}}}`

// githubRepo is paymentRepo with origin pointing at GitHub.
func githubRepo(t *testing.T) (*testrepo.Repo, []string) {
	t.Helper()
	r, commits := paymentRepo(t)
	r.Git("remote", "add", "origin", "git@github.com:acme/pay.git")
	return r, commits
}

// TestRunShowsThreadsBeyondFirstPage covers a pull request with more review
// threads than one query returns: the thread on the target file sits on the
// second page and must still be shown.
func TestRunShowsThreadsBeyondFirstPage(t *testing.T) {
	r, _ := githubRepo(t)
	first := strings.Replace(pullRequestFixture, `"reviewThreads":{"nodes":[`, `"reviewThreads":{"pageInfo":{"hasNextPage":true},"nodes":[`, 1)
	pages := `{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":true,"endCursor":"c1"},"nodes":[
  {"path":"internal/payment/repository.go","line":9,"isResolved":false,"isOutdated":false,
   "comments":{"totalCount":1,"nodes":[{"body":"nit: rename","url":"https://github.com/acme/pay/pull/42#discussion_r2","author":{"login":"grace"}}]}}
 ]}}}}}{"data":{"repository":{"pullRequest":{"reviewThreads":{"pageInfo":{"hasNextPage":false,"endCursor":"c2"},"nodes":[
  {"path":"internal/payment/service.go","line":3,"isResolved":false,"isOutdated":false,
   "comments":{"totalCount":1,"nodes":[{"body":"Found on the second page","url":"https://github.com/acme/pay/pull/42#discussion_r3","author":{"login":"linus"}}]}}
 ]}}}}}`
	stub := testrepo.StubGHCalls(t, first, pages)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	assertInOrder(t, res.stdout,
		"Pull request",
		"  #42  fix: prevent duplicate payment processing",
		"Review discussion",
		"  @linus (L3, unresolved)",
		"    Found on the second page",
		"  … 1 more thread on other files",
		"Changed with",
	)
	if strings.Contains(res.stdout, "Should this be a 409") {
		t.Errorf("first-page thread survived the refetch:\n%s", res.stdout)
	}
	if stub.Calls() != 2 || !slices.Contains(stub.Args(), "--paginate") {
		t.Errorf("gh ran %d times, last args %q; want a second --paginate call", stub.Calls(), stub.Args())
	}
}

func TestRunShowsPullRequest(t *testing.T) {
	r, commits := githubRepo(t)
	stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	// The fixture's mergedAt is UTC; the report shows it in local time.
	merged := time.Date(2026, 1, 4, 9, 0, 0, 0, time.UTC).Local().Format("2006-01-02")
	assertInOrder(t, res.stdout,
		"Introduced / Changed",
		"  Retries from the gateway could charge twice.",
		"Pull request",
		"  #42  fix: prevent duplicate payment processing",
		"  by ada · merged "+merged+" · https://github.com/acme/pay/pull/42",
		"  Closes #38  Duplicate charges on gateway retry",
		"  ## Summary",
		"  Retries from the gateway could charge twice.",
		"Review discussion",
		"  @grace (L3, resolved, 2 replies)",
		"    Should this be a 409 instead of 500?",
		"  … 1 more thread on other files",
		"Changed with",
		"Line history",
	)
	if strings.Contains(res.stdout, "template") {
		t.Errorf("HTML comment from the PR template leaked into the output:\n%s", res.stdout)
	}
	if res.stderr != "" {
		t.Errorf("unexpected stderr: %s", res.stderr)
	}

	args := stub.Args()
	for _, want := range []string{"api", "graphql", "--hostname", "github.com", "owner=acme", "name=pay", "oid=" + commits[0]} {
		if !slices.Contains(args, want) {
			t.Errorf("gh was not called with %q: %q", want, args)
		}
	}
}

func TestRunOffline(t *testing.T) {
	r, _ := githubRepo(t)
	stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "--offline", "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	if strings.Contains(res.stdout, "Pull request") {
		t.Errorf("--offline still shows a pull request section:\n%s", res.stdout)
	}
	if args := stub.Args(); args != nil {
		t.Errorf("--offline still ran gh: %q", args)
	}
}

func TestRunIgnoresNonGitHubRemote(t *testing.T) {
	r, _ := paymentRepo(t)
	r.Git("remote", "add", "origin", "https://gitlab.com/acme/pay.git")
	stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	if strings.Contains(res.stdout, "Pull request") {
		t.Errorf("a GitLab remote produced a pull request section:\n%s", res.stdout)
	}
	if args := stub.Args(); args != nil {
		t.Errorf("gh ran for a GitLab remote: %q", args)
	}
}

func TestRunEnterpriseRemote(t *testing.T) {
	t.Run("GH_HOST remote is looked up on that host", func(t *testing.T) {
		r, commits := paymentRepo(t)
		t.Setenv("GH_HOST", "ghe.example.com")
		r.Git("remote", "add", "origin", "git@GHE.example.com:acme/pay.git")
		stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
		t.Chdir(r.Dir)

		res := run(t, "internal/payment/service.go:3")
		if res.code != exitOK {
			t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
		}
		if !strings.Contains(res.stdout, "  #42  fix: prevent duplicate payment processing") {
			t.Errorf("pull request missing from stdout:\n%s", res.stdout)
		}
		args := stub.Args()
		for _, want := range []string{"--hostname", "ghe.example.com", "owner=acme", "name=pay", "oid=" + commits[0]} {
			if !slices.Contains(args, want) {
				t.Errorf("gh was not called with %q: %q", want, args)
			}
		}
	})

	t.Run("other hosts stay ignored", func(t *testing.T) {
		r, _ := paymentRepo(t)
		t.Setenv("GH_HOST", "ghe.example.com")
		r.Git("remote", "add", "origin", "https://gitlab.com/acme/pay.git")
		stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
		t.Chdir(r.Dir)

		res := run(t, "internal/payment/service.go:3")
		if res.code != exitOK {
			t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
		}
		if strings.Contains(res.stdout, "Pull request") {
			t.Errorf("a GitLab remote produced a pull request section:\n%s", res.stdout)
		}
		if args := stub.Args(); args != nil {
			t.Errorf("gh ran for a GitLab remote: %q", args)
		}
	})
}

func TestRunPrefersUpstreamRemote(t *testing.T) {
	r, _ := paymentRepo(t)
	r.Git("remote", "add", "origin", "git@github.com:fork/pay.git")
	r.Git("remote", "add", "upstream", "https://github.com/acme/pay.git")
	stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
	t.Chdir(r.Dir)

	if res := run(t, "internal/payment/service.go:3"); res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	args := stub.Args()
	if !slices.Contains(args, "owner=acme") || slices.Contains(args, "owner=fork") {
		t.Errorf("gh was not asked about upstream: %q", args)
	}
}

func TestRunSkipsRemoteWithoutURL(t *testing.T) {
	r, _ := githubRepo(t)
	// upstream is listed by `git remote` but has no URL to read.
	r.Git("config", "remote.upstream.fetch", "+refs/heads/*:refs/remotes/upstream/*")
	stub := testrepo.StubGH(t, pullRequestFixture, "", 0)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	if !strings.Contains(res.stdout, "  #42  fix: prevent duplicate payment processing\n") {
		t.Errorf("a half-configured upstream hid origin's pull request:\n%s", res.stdout)
	}
	if args := stub.Args(); !slices.Contains(args, "owner=acme") {
		t.Errorf("gh was not asked about origin: %q", args)
	}
}

func TestRunGitHubNotes(t *testing.T) {
	notFound := `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`
	tests := []struct {
		name   string
		stdout string
		stderr string
		code   int
		want   string
	}{
		{"not logged in", "", "To get started with GitHub CLI, please run:  gh auth login", 4,
			"(gh is not logged in; run `gh auth login`, or pass --offline)"},
		{"bad credentials", `{"message":"Bad credentials","status":"401"}`, "gh: Bad credentials (HTTP 401)", 1,
			"(GitHub rejected gh's credentials; run `gh auth login`)"},
		{"repository not visible", notFound, "gh: Could not resolve to a Repository with the name 'acme/pay'.", 1,
			"(acme/pay is not on GitHub or not visible to the gh account; check `gh auth status`)"},
		{"commit not pushed", `{"data":{"repository":{"object":null}}}`, "", 0,
			"(<sha> is not on GitHub yet; push it first)"},
		{"no pull request", `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`, "", 0,
			"(no pull request for <sha>)"},
		{"rate limited", "", "gh: API rate limit exceeded for user ID 1 (HTTP 403)", 1,
			"(GitHub API rate limit exceeded; try again later)"},
		{"network error", "", "error connecting to api.github.com\ncheck your internet connection", 1,
			"(GitHub lookup failed: acme/pay@<sha>: gh api: error connecting to api.github.com)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, commits := githubRepo(t)
			testrepo.StubGH(t, tt.stdout, tt.stderr, tt.code)
			t.Chdir(r.Dir)

			res := run(t, "internal/payment/service.go:3")
			if res.code != exitOK {
				t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
			}
			if res.stderr != "" {
				t.Errorf("a GitHub problem wrote to stderr: %s", res.stderr)
			}
			want := "Pull request\n  " + strings.ReplaceAll(tt.want, "<sha>", r.Short(commits[0])) + "\n"
			if !strings.Contains(res.stdout, want) {
				t.Errorf("stdout missing note %q:\n%s", want, res.stdout)
			}
			if strings.Contains(res.stdout, "Review discussion") {
				t.Errorf("review section shown without a pull request:\n%s", res.stdout)
			}
		})
	}
}

func TestRunGitHubTimeout(t *testing.T) {
	r, _ := githubRepo(t)
	slowGH(t)
	t.Chdir(r.Dir)
	restore := githubTimeout
	githubTimeout = 100 * time.Millisecond
	t.Cleanup(func() { githubTimeout = restore })

	start := time.Now()
	res := run(t, "internal/payment/service.go:3")
	elapsed := time.Since(start)
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	if res.stderr != "" {
		t.Errorf("a GitHub timeout wrote to stderr: %s", res.stderr)
	}
	want := fmt.Sprintf("Pull request\n  (GitHub lookup timed out after %s)\n", githubTimeout)
	if !strings.Contains(res.stdout, want) {
		t.Errorf("stdout missing note %q:\n%s", want, res.stdout)
	}
	// The stub sleeps far longer than the timeout; the report must not
	// wait for it.
	if limit := 3 * time.Second; elapsed > limit {
		t.Errorf("run took %s, timeout of %s did not bound the gh call", elapsed, githubTimeout)
	}
}

// slowGH installs a gh that never answers. It execs sleep so the kill on
// timeout reaches the sleeping process rather than a shell wrapper.
func slowGH(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nexec sleep 30\n"
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestRunWithoutGitHubCLI(t *testing.T) {
	r, _ := githubRepo(t)
	gitOnlyPATH(t)
	t.Chdir(r.Dir)

	res := run(t, "internal/payment/service.go:3")
	if res.code != exitOK {
		t.Fatalf("exit code = %d, stderr:\n%s", res.code, res.stderr)
	}
	want := "Pull request\n  (gh is not installed; get the GitHub CLI from https://cli.github.com, or pass --offline)\n"
	if !strings.Contains(res.stdout, want) {
		t.Errorf("stdout missing %q:\n%s", want, res.stdout)
	}
}

// gitOnlyPATH leaves a PATH on which git is the only executable.
func gitOnlyPATH(t *testing.T) {
	t.Helper()
	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable not found on PATH")
	}
	dir := t.TempDir()
	script := fmt.Sprintf("#!/bin/sh\nexec %q \"$@\"\n", gitBin)
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}
