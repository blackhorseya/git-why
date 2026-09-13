package github

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// threadFields selects one page of review threads with the comment that
// opened each thread. Both queries share it so their pages parse into the
// same threadConnection.
const threadFields = `pageInfo { hasNextPage endCursor }
          nodes {
            path line isResolved isOutdated
            comments(first: 1) {
              totalCount
              nodes { body url author { login } }
            }
          }`

// pullRequestQuery fetches, in one round trip, the pull requests that
// contain a commit together with the issues they close and the first
// hundred of their review threads. object is null when the commit is not
// on GitHub; repository is null (with a NOT_FOUND error) when the
// repository is not visible.
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
              ` + threadFields + `
            }
          }
        }
      }
    }
  }
}`

// threadsQuery fetches every review thread of one pull request. It runs
// under gh's --paginate, which needs the $endCursor variable and the
// pageInfo selection to follow the pages, and prints one JSON document per
// page.
const threadsQuery = `query($owner: String!, $name: String!, $number: Int!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100, after: $endCursor) {
        ` + threadFields + `
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
	Errors []graphQLError `json:"errors"`
}

// threadsResponse is one page of threadsQuery.
type threadsResponse struct {
	Data struct {
		Repository *struct {
			PullRequest *struct {
				ReviewThreads threadConnection `json:"reviewThreads"`
			} `json:"pullRequest"`
		} `json:"repository"`
	} `json:"data"`
}

type graphQLError struct {
	Type    string `json:"type"`
	Message string `json:"message"`
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
	ReviewThreads threadConnection `json:"reviewThreads"`
}

type threadConnection struct {
	PageInfo struct {
		HasNextPage bool `json:"hasNextPage"`
	} `json:"pageInfo"`
	Nodes []threadNode `json:"nodes"`
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

// parseThreads reads the pages that gh --paginate prints for threadsQuery:
// whole JSON documents back to back, without a separator.
func parseThreads(out string) ([]Thread, error) {
	var threads []Thread
	dec := json.NewDecoder(strings.NewReader(out))
	for pages := 0; ; pages++ {
		var page threadsResponse
		err := dec.Decode(&page)
		if errors.Is(err, io.EOF) {
			if pages == 0 {
				return nil, errors.New("unexpected gh output: no review thread pages")
			}
			return threads, nil
		}
		if err != nil {
			return nil, fmt.Errorf("unexpected gh output: %w", err)
		}
		if page.Data.Repository == nil {
			return nil, ErrNotFound
		}
		if page.Data.Repository.PullRequest == nil {
			return nil, errors.New("unexpected gh output: review thread page without a pull request")
		}
		for _, t := range page.Data.Repository.PullRequest.ReviewThreads.Nodes {
			threads = append(threads, t.thread())
		}
	}
}

func (x pullRequestNode) pullRequest() PullRequest {
	pr := PullRequest{
		Number:      x.Number,
		Title:       x.Title,
		State:       x.State,
		MergedAt:    x.MergedAt,
		URL:         x.URL,
		Author:      login(x.Author),
		Body:        x.Body,
		Issues:      x.ClosingIssuesReferences.Nodes,
		moreThreads: x.ReviewThreads.PageInfo.HasNextPage,
	}
	for _, t := range x.ReviewThreads.Nodes {
		pr.Threads = append(pr.Threads, t.thread())
	}
	return pr
}

func (x threadNode) thread() Thread {
	thread := Thread{
		Path:     x.Path,
		Line:     x.Line,
		Resolved: x.IsResolved,
		Outdated: x.IsOutdated,
		Replies:  max(x.Comments.TotalCount-1, 0),
	}
	if len(x.Comments.Nodes) > 0 {
		first := x.Comments.Nodes[0]
		thread.Author, thread.Body, thread.URL = login(first.Author), first.Body, first.URL
	}
	return thread
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

// hasGraphQLError reports whether out holds a GraphQL response (or, under
// --paginate, a run of them) carrying an error of the given type.
func hasGraphQLError(out, typ string) bool {
	dec := json.NewDecoder(strings.NewReader(out))
	for {
		var resp struct {
			Errors []graphQLError `json:"errors"`
		}
		if dec.Decode(&resp) != nil {
			return false
		}
		for _, e := range resp.Errors {
			if e.Type == typ {
				return true
			}
		}
	}
}
