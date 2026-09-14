package presenter

import (
	"bytes"
	"strings"
	"testing"

	"github.com/blackhorseya/git-why/internal/github"
)

func TestRenderAnswer(t *testing.T) {
	r := sampleReport()
	r.GitHub = &GitHub{PullRequest: &github.PullRequest{Number: 42, URL: "https://github.com/acme/pay/pull/42"}}
	text := "The check exists because gateway retries could reach the charge endpoint twice within the TTL window, " +
		"so the service now looks the payment up first and treats the second attempt as a no-op. " +
		"It came in with pull request #42, which closed the duplicate-charges issue.\n"

	var out bytes.Buffer
	if err := RenderAnswer(&out, Answer{Report: r, Text: text}); err != nil {
		t.Fatal(err)
	}
	want := `Why does internal/payment/service.go:87 exist?

  The check exists because gateway retries could reach the charge endpoint twice
  within the TTL window, so the service now looks the payment up first and
  treats the second attempt as a no-op. It came in with pull request #42, which
  closed the duplicate-charges issue.

Sources  8ac912f · #42 https://github.com/acme/pay/pull/42
`
	if out.String() != want {
		t.Errorf("RenderAnswer() =\n%s\nwant\n%s", out.String(), want)
	}
	for _, l := range strings.Split(out.String(), "\n") {
		if len(l) > maxProseWidth {
			t.Errorf("line wider than %d: %q", maxProseWidth, l)
		}
	}
}

func TestRenderAnswerWithoutPullRequest(t *testing.T) {
	r := sampleReport()
	r.GitHub = &GitHub{Note: "no pull request for 8ac912f"}

	var out bytes.Buffer
	if err := RenderAnswer(&out, Answer{Report: r, Text: "Short answer."}); err != nil {
		t.Fatal(err)
	}
	want := "Why does internal/payment/service.go:87 exist?\n\n  Short answer.\n\nSources  8ac912f\n"
	if out.String() != want {
		t.Errorf("RenderAnswer() =\n%q\nwant\n%q", out.String(), want)
	}
}
