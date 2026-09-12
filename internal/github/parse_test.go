package github

import (
	"errors"
	"reflect"
	"testing"
	"time"
)

// fixtureMerged is what gh prints for pullRequestQuery when the commit was
// merged through one pull request with a linked issue and two review
// threads, one of them outdated and opened by a deleted account.
const fixtureMerged = `{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[
{"number":42,"title":"fix: prevent duplicate payment processing","state":"MERGED",
 "mergedAt":"2026-08-21T09:00:00Z","url":"https://github.com/acme/pay/pull/42",
 "body":"Retries could charge twice.","author":{"login":"ada"},
 "closingIssuesReferences":{"nodes":[
  {"number":38,"title":"Duplicate charges","url":"https://github.com/acme/pay/issues/38","state":"CLOSED"}]},
 "reviewThreads":{"nodes":[
  {"path":"internal/payment/service.go","line":87,"isResolved":true,"isOutdated":false,
   "comments":{"totalCount":3,"nodes":[{"body":"Should this be a 409?","url":"https://github.com/acme/pay/pull/42#discussion_r1","author":{"login":"grace"}}]}},
  {"path":"README.md","line":null,"isResolved":false,"isOutdated":true,
   "comments":{"totalCount":1,"nodes":[{"body":"typo","url":"https://github.com/acme/pay/pull/42#discussion_r2","author":null}]}}
 ]}}
]}}}}}`

func TestParsePullRequests(t *testing.T) {
	got, err := parsePullRequests(fixtureMerged)
	if err != nil {
		t.Fatal(err)
	}

	want := []PullRequest{{
		Number:   42,
		Title:    "fix: prevent duplicate payment processing",
		State:    "MERGED",
		MergedAt: time.Date(2026, 8, 21, 9, 0, 0, 0, time.UTC),
		URL:      "https://github.com/acme/pay/pull/42",
		Author:   "ada",
		Body:     "Retries could charge twice.",
		Issues: []Issue{
			{Number: 38, Title: "Duplicate charges", URL: "https://github.com/acme/pay/issues/38", State: "CLOSED"},
		},
		Threads: []Thread{
			{Path: "internal/payment/service.go", Line: 87, Author: "grace", Body: "Should this be a 409?",
				URL: "https://github.com/acme/pay/pull/42#discussion_r1", Replies: 2, Resolved: true},
			{Path: "README.md", Body: "typo", URL: "https://github.com/acme/pay/pull/42#discussion_r2", Outdated: true},
		},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parsePullRequests() =\n%+v\nwant\n%+v", got, want)
	}
}

func TestParsePullRequestsErrors(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want error
	}{
		{"repository not visible", `{"data":{"repository":null}}`, ErrNotFound},
		{"commit not on GitHub", `{"data":{"repository":{"object":null}}}`, ErrCommitNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := parsePullRequests(tt.out); !errors.Is(err, tt.want) {
				t.Fatalf("parsePullRequests() error = %v, want %v", err, tt.want)
			}
		})
	}

	if _, err := parsePullRequests("not json"); err == nil {
		t.Fatal("parsePullRequests(not json) succeeded")
	}
	prs, err := parsePullRequests(`{"data":{"repository":{"object":{"associatedPullRequests":{"nodes":[]}}}}}`)
	if err != nil || len(prs) != 0 {
		t.Fatalf("parsePullRequests(no PRs) = %v, %v", prs, err)
	}
}

func TestPick(t *testing.T) {
	merged := func(n int, day int) PullRequest {
		return PullRequest{Number: n, State: "MERGED", MergedAt: time.Date(2026, 1, day, 0, 0, 0, 0, time.UTC)}
	}
	open := PullRequest{Number: 7, State: "OPEN"}

	tests := []struct {
		name string
		prs  []PullRequest
		want int
		ok   bool
	}{
		{"none", nil, 0, false},
		{"only open", []PullRequest{open}, 7, true},
		{"merged beats open", []PullRequest{open, merged(2, 5)}, 2, true},
		{"earliest merge wins", []PullRequest{merged(3, 9), merged(2, 5), merged(4, 7)}, 2, true},
	}
	for _, tt := range tests {
		got, ok := pick(tt.prs)
		if ok != tt.ok || got.Number != tt.want {
			t.Errorf("%s: pick() = #%d, %v; want #%d, %v", tt.name, got.Number, ok, tt.want, tt.ok)
		}
	}
}

func TestHasGraphQLError(t *testing.T) {
	out := `{"data":{"repository":null},"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository"}]}`
	if !hasGraphQLError(out, "NOT_FOUND") {
		t.Error("NOT_FOUND not detected")
	}
	if hasGraphQLError(out, "RATE_LIMITED") {
		t.Error("RATE_LIMITED detected in a NOT_FOUND response")
	}
	if hasGraphQLError("", "NOT_FOUND") || hasGraphQLError(`{"message":"Bad credentials"}`, "NOT_FOUND") {
		t.Error("error detected in a response without GraphQL errors")
	}
}
