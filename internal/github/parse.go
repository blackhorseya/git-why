package github

import (
	"encoding/json"
	"fmt"
	"time"
)

// pullRequestQuery fetches, in one round trip, the pull requests that
// contain a commit together with the issues they close and their review
// threads. object is null when the commit is not on GitHub; repository is
// null (with a NOT_FOUND error) when the repository is not visible.
const pullRequestQuery = `query($owner: String!, $name: String!, $oid: GitObjectID!) {
  repository(owner: $owner, name: $name) {
    object(oid: $oid) {
      ... on Commit {
        associatedPullRequests(first: 5) {
          nodes {
            number title state mergedAt url body
            author { login }
            closingIssuesReferences(first: 5) {
              nodes { number title url state }
            }
            reviewThreads(first: 100) {
              nodes {
                path line isResolved isOutdated
                comments(first: 1) {
                  totalCount
                  nodes { body url author { login } }
                }
              }
            }
          }
        }
      }
    }
  }
}`

type response struct {
	Data struct {
		Repository *struct {
			Object *struct {
				AssociatedPullRequests struct {
					Nodes []pullRequestNode `json:"nodes"`
				} `json:"associatedPullRequests"`
			} `json:"object"`
		} `json:"repository"`
	} `json:"data"`
	Errors []struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"errors"`
}

type actor struct {
	Login string `json:"login"`
}

type pullRequestNode struct {
	Number                  int       `json:"number"`
	Title                   string    `json:"title"`
	State                   string    `json:"state"`
	MergedAt                time.Time `json:"mergedAt"`
	URL                     string    `json:"url"`
	Body                    string    `json:"body"`
	Author                  *actor    `json:"author"`
	ClosingIssuesReferences struct {
		Nodes []Issue `json:"nodes"`
	} `json:"closingIssuesReferences"`
	ReviewThreads struct {
		Nodes []threadNode `json:"nodes"`
	} `json:"reviewThreads"`
}

type threadNode struct {
	Path       string `json:"path"`
	Line       int    `json:"line"`
	IsResolved bool   `json:"isResolved"`
	IsOutdated bool   `json:"isOutdated"`
	Comments   struct {
		TotalCount int `json:"totalCount"`
		Nodes      []struct {
			Body   string `json:"body"`
			URL    string `json:"url"`
			Author *actor `json:"author"`
		} `json:"nodes"`
	} `json:"comments"`
}

// parsePullRequests reads the JSON that gh prints for pullRequestQuery.
func parsePullRequests(out string) ([]PullRequest, error) {
	var resp response
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		return nil, fmt.Errorf("unexpected gh output: %w", err)
	}
	if resp.Data.Repository == nil {
		return nil, ErrNotFound
	}
	if resp.Data.Repository.Object == nil {
		return nil, ErrCommitNotFound
	}

	var prs []PullRequest
	for _, n := range resp.Data.Repository.Object.AssociatedPullRequests.Nodes {
		prs = append(prs, n.pullRequest())
	}
	return prs, nil
}

func (x pullRequestNode) pullRequest() PullRequest {
	pr := PullRequest{
		Number:   x.Number,
		Title:    x.Title,
		State:    x.State,
		MergedAt: x.MergedAt,
		URL:      x.URL,
		Author:   login(x.Author),
		Body:     x.Body,
		Issues:   x.ClosingIssuesReferences.Nodes,
	}
	for _, t := range x.ReviewThreads.Nodes {
		thread := Thread{
			Path:     t.Path,
			Line:     t.Line,
			Resolved: t.IsResolved,
			Outdated: t.IsOutdated,
			Replies:  max(t.Comments.TotalCount-1, 0),
		}
		if len(t.Comments.Nodes) > 0 {
			first := t.Comments.Nodes[0]
			thread.Author, thread.Body, thread.URL = login(first.Author), first.Body, first.URL
		}
		pr.Threads = append(pr.Threads, thread)
	}
	return pr
}

func login(a *actor) string {
	if a == nil {
		return ""
	}
	return a.Login
}

// pick chooses the pull request that introduced the commit: the earliest
// merged one, or else the first one GitHub listed.
func pick(prs []PullRequest) (PullRequest, bool) {
	best := -1
	for i, pr := range prs {
		if pr.State == "MERGED" && (best < 0 || pr.MergedAt.Before(prs[best].MergedAt)) {
			best = i
		}
	}
	if best >= 0 {
		return prs[best], true
	}
	if len(prs) > 0 {
		return prs[0], true
	}
	return PullRequest{}, false
}

// hasGraphQLError reports whether out is a GraphQL response carrying an
// error of the given type.
func hasGraphQLError(out, typ string) bool {
	var resp response
	if json.Unmarshal([]byte(out), &resp) != nil {
		return false
	}
	for _, e := range resp.Errors {
		if e.Type == typ {
			return true
		}
	}
	return false
}
