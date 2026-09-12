package presenter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/blackhorseya/git-why/internal/github"
)

func samplePullRequest() *github.PullRequest {
	return &github.PullRequest{
		Number:   42,
		Title:    "fix: prevent duplicate payment processing",
		State:    "MERGED",
		MergedAt: day(21),
		URL:      "https://github.com/acme/pay/pull/42",
		Author:   "sean",
		Body:     "<!-- Describe your change -->\r\n## Why\r\n\r\n\r\nRetries from the gateway could charge twice.\r\n\r\n",
		Issues: []github.Issue{
			{Number: 38, Title: "Duplicate charges on gateway retry", State: "CLOSED"},
			{Number: 40, Title: "Audit retries", State: "OPEN"},
		},
		Threads: []github.Thread{
			{Path: "internal/payment/service.go", Line: 87, Author: "grace", Resolved: true, Replies: 2,
				Body: "```suggestion\nreturn ErrConflict\n```\nShould this be a 409 instead of 500?"},
			{Path: "internal/payment/service.go", Outdated: true, Body: "nit"},
			{Path: "internal/payment/repository.go", Line: 9, Author: "grace", Body: "rename?"},
		},
	}
}

func render(t *testing.T, r Report) string {
	t.Helper()
	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	return buf.String()
}

func TestRenderPullRequest(t *testing.T) {
	r := sampleReport()
	r.GitHub = &GitHub{PullRequest: samplePullRequest()}

	want := `  Check the idempotency key before charging.

Pull request
  #42  fix: prevent duplicate payment processing
  by sean · merged 2026-08-21 · https://github.com/acme/pay/pull/42
  Closes #38  Duplicate charges on gateway retry
  Closes #40  Audit retries (still open)

  ## Why

  Retries from the gateway could charge twice.

Review discussion
  @grace (L87, resolved, 2 replies)
    Should this be a 409 instead of 500?
  @ghost (outdated, unresolved)
    nit
  … 1 more thread on other files

Changed with
`
	if got := render(t, r); !strings.Contains(got, want) {
		t.Fatalf("Render() missing pull request section\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderPullRequestStates(t *testing.T) {
	for state, want := range map[string]string{
		"OPEN":   "  by sean · open · https://github.com/acme/pay/pull/42\n",
		"CLOSED": "  by sean · closed without merging · https://github.com/acme/pay/pull/42\n",
	} {
		r := sampleReport()
		pr := samplePullRequest()
		pr.State = state
		r.GitHub = &GitHub{PullRequest: pr}
		if got := render(t, r); !strings.Contains(got, want) {
			t.Errorf("Render(%s) missing %q in:\n%s", state, want, got)
		}
	}
}

func TestRenderGitHubNote(t *testing.T) {
	r := sampleReport()
	r.GitHub = &GitHub{Note: "no pull request for 8ac912f"}

	want := "Pull request\n  (no pull request for 8ac912f)\n\nChanged with\n"
	if got := render(t, r); !strings.Contains(got, want) {
		t.Fatalf("Render() missing %q in:\n%s", want, got)
	}
	if strings.Contains(render(t, r), "Review discussion") {
		t.Fatal("Render() shows a review section without a pull request")
	}
}

func TestRenderWithoutGitHub(t *testing.T) {
	got := render(t, sampleReport())
	for _, s := range []string{"Pull request", "Review discussion"} {
		if strings.Contains(got, s) {
			t.Errorf("Render() shows %q although GitHub was not consulted:\n%s", s, got)
		}
	}
}

func TestRenderReviewDiscussionLimits(t *testing.T) {
	r := sampleReport()
	pr := samplePullRequest()
	pr.Threads = nil
	for i := range maxThreads + 2 {
		pr.Threads = append(pr.Threads, github.Thread{Path: r.Path, Line: i + 1, Author: "grace", Body: fmt.Sprintf("thread %d", i)})
	}
	r.GitHub = &GitHub{PullRequest: pr}
	got := render(t, r)
	if n := strings.Count(got, "\n    thread "); n != maxThreads {
		t.Errorf("Render() shows %d threads, want %d:\n%s", n, maxThreads, got)
	}
	if !strings.Contains(got, "  … and 2 more threads on this file\n") {
		t.Errorf("Render() missing the overflow note:\n%s", got)
	}

	pr.Threads = []github.Thread{{Path: "other.go", Body: "x"}, {Path: "another.go", Body: "y"}}
	got = render(t, r)
	if want := "Review discussion\n  (no review comments on this file; 2 threads elsewhere in the pull request)\n"; !strings.Contains(got, want) {
		t.Errorf("Render() missing %q in:\n%s", want, got)
	}

	pr.Threads = nil
	got = render(t, r)
	if want := "Review discussion\n  (no review comments on this file)\n"; !strings.Contains(got, want) {
		t.Errorf("Render() missing %q in:\n%s", want, got)
	}
}

func TestPullRequestBody(t *testing.T) {
	long := strings.Repeat("line\n", maxBodyLines+3)
	tests := []struct {
		name, in, want string
	}{
		{"empty", "", ""},
		{"only a template comment", "<!-- fill me in -->\n\n", ""},
		{"comment spanning lines", "<!--\nfill me in\n-->\nKeep this.", "Keep this."},
		{"blank runs collapse", "a\n\n\n\nb\n\n", "a\n\nb"},
		{"trailing spaces trimmed", "a   \nb\t", "a\nb"},
		{"capped", long, strings.TrimSuffix(strings.Repeat("line\n", maxBodyLines), "\n") + "\n…"},
	}
	for _, tt := range tests {
		if got := pullRequestBody(tt.in); got != tt.want {
			t.Errorf("%s: pullRequestBody(%q) = %q, want %q", tt.name, tt.in, got, tt.want)
		}
	}
}

func TestFirstLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Should this be a 409?", "Should this be a 409?"},
		{"\n\n  Leading blanks  \nmore", "Leading blanks"},
		{"```suggestion\nreturn ErrConflict\n```\nUse the sentinel.", "Use the sentinel."},
		{"```suggestion\nreturn ErrConflict\n```", "(suggested change)"},
		{"", "(suggested change)"},
	}
	for _, tt := range tests {
		if got := firstLine(tt.in); got != tt.want {
			t.Errorf("firstLine(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
