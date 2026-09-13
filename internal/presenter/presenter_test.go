package presenter

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blackhorseya/git-why/internal/git"
)

// day builds fixture dates in the local zone: the presenter converts
// GitHub's UTC timestamps with Local(), which must not move the date.
func day(d int) time.Time {
	return time.Date(2026, 8, d, 10, 0, 0, 0, time.Local)
}

func sampleReport() Report {
	fix := git.Commit{
		Hash:        "8ac912f0000000000000000000000000000000000",
		ShortHash:   "8ac912f",
		AuthorName:  "Sean",
		AuthorEmail: "sean@example.com",
		AuthorDate:  day(21),
		Subject:     "fix: prevent duplicate payment processing",
		Body:        "Retries from the gateway could charge twice.\n\nCheck the idempotency key before charging.",
	}
	return Report{
		Path:   "internal/payment/service.go",
		Line:   87,
		Text:   "\tif err := repository.Exists(ctx, id); err != nil {",
		Blame:  git.BlameLine{Hash: fix.Hash, OrigLine: 87, OrigPath: "internal/payment/service.go"},
		Commit: fix,
		ChangedWith: []string{
			"internal/payment/repository.go",
			"internal/payment/service_test.go",
		},
		History: []git.Commit{
			fix,
			{ShortHash: "a912bc1", AuthorDate: day(20), Subject: "chore: change Redis TTL to 24h"},
			{ShortHash: "f8219de", AuthorDate: day(1), Subject: "feat: initial payment service"},
		},
	}
}

func TestRender(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}

	want := `Why does this line exist?

Current line  internal/payment/service.go:87
  if err := repository.Exists(ctx, id); err != nil {

Introduced / Changed
  Commit: 8ac912f
  Author: Sean <sean@example.com>
  Date:   2026-08-21

  fix: prevent duplicate payment processing

  Retries from the gateway could charge twice.

  Check the idempotency key before charging.

Changed with
  internal/payment/repository.go
  internal/payment/service_test.go

Line history
  8ac912f  2026-08-21  fix: prevent duplicate payment processing
  a912bc1  2026-08-20  chore: change Redis TTL to 24h
  f8219de  2026-08-01  feat: initial payment service
`
	if got := buf.String(); got != want {
		t.Fatalf("Render() mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRenderPlainWhenNotATerminal(t *testing.T) {
	var buf bytes.Buffer
	if err := Render(&buf, sampleReport()); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "\x1b[") {
		t.Fatalf("Render() to a non-terminal emitted ANSI escapes:\n%q", buf.String())
	}
}

func TestRenderEdgeCases(t *testing.T) {
	r := sampleReport()
	r.Text = "   "
	r.Blame.OrigPath = "internal/payments/service.go"
	r.Blame.OrigLine = 80
	r.HistoryTruncated = true
	r.ChangedWith = nil
	for i := range 12 {
		r.ChangedWith = append(r.ChangedWith, fmt.Sprintf("file%02d.go", i))
	}

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{
		"  (empty line)\n",
		"  Path:   internal/payments/service.go (renamed since)\n",
		"  file09.go\n  … and 2 more\n",
		"  … more: git log -L 80,80:internal/payments/service.go 8ac912f\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "file10.go") {
		t.Errorf("Render() listed more than %d changed files:\n%s", maxChangedFiles, got)
	}
}

func TestRenderShallowBoundary(t *testing.T) {
	r := sampleReport()
	r.Blame.Boundary = true
	r.ChangedWith = nil // cli leaves the files unknown for a boundary commit

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	got := buf.String()

	for _, want := range []string{
		"Changed with\n  (shallow clone: parent commit missing, changed files unknown)\n",
		"  (shallow clone: older history may be missing)\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("Render() missing %q in:\n%s", want, got)
		}
	}
	if strings.Contains(got, "(no other files)") {
		t.Errorf("Render() reported no files instead of unknown files:\n%s", got)
	}
}

func TestRenderNoOtherFiles(t *testing.T) {
	r := sampleReport()
	r.ChangedWith = nil

	var buf bytes.Buffer
	if err := Render(&buf, r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "Changed with\n  (no other files)\n") {
		t.Fatalf("Render() = \n%s", buf.String())
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"exactly10!", 10, "exactly10!"},
		{"this is too long", 10, "this is t…"},
		{"說明說明說明", 4, "說明說…"},
	}
	for _, tt := range tests {
		if got := truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}
