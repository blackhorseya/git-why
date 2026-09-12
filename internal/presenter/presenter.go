// Package presenter renders a git-why report for humans in a terminal.
package presenter

import (
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/blackhorseya/git-why/internal/git"
	"github.com/blackhorseya/git-why/internal/github"
)

const (
	// maxChangedFiles caps the "Changed with" list for sweeping commits.
	maxChangedFiles = 10
	// maxTextWidth caps how much of a line of code or a comment is shown.
	maxTextWidth = 100
	// maxBodyLines caps the pull request description.
	maxBodyLines = 8
	// maxThreads caps the review threads shown for the target file.
	maxThreads = 3
	dateLayout = "2006-01-02"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue)
	hashStyle    = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Green)
	loginStyle   = lipgloss.NewStyle().Foreground(lipgloss.Cyan)
	subjectStyle = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)

	// htmlComment matches the hidden instructions pull request templates
	// leave in a description.
	htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)
	// fencedCode matches code blocks, such as suggested changes in reviews.
	fencedCode = regexp.MustCompile("(?s)```.*?```")
)

// Report is everything git-why knows about one line.
type Report struct {
	// Path is the repository-relative path of the target file.
	Path string
	// Line is the 1-based line number in the working tree.
	Line int
	// Text is the current content of the line.
	Text string
	// Blame locates the line in the commit that last changed it.
	Blame git.BlameLine
	// Commit is the commit that last changed the line.
	Commit git.Commit
	// ChangedWith lists the other files changed by Commit.
	ChangedWith []string
	// History lists commits that changed the line, newest first.
	History []git.Commit
	// HistoryTruncated is set when older commits were left out of History.
	HistoryTruncated bool
	// GitHub is the pull request context for Commit, or nil when git-why
	// did not look for one (offline, or no GitHub remote).
	GitHub *GitHub
}

// GitHub is the pull request behind a commit, or a Note saying why there
// is none to show.
type GitHub struct {
	PullRequest *github.PullRequest
	Note        string
}

// Render writes the report to w. Colors are used only when w is a terminal
// that supports them; otherwise the output is plain text.
func Render(w io.Writer, r Report) error {
	var sections []string
	for _, s := range []string{
		titleStyle.Render("Why does this line exist?"),
		currentLine(r),
		introduced(r),
		pullRequest(r),
		reviewDiscussion(r),
		changedWith(r.ChangedWith),
		history(r),
	} {
		if s != "" {
			sections = append(sections, s)
		}
	}
	_, err := lipgloss.Fprint(w, strings.Join(sections, "\n\n")+"\n")
	return err
}

func currentLine(r Report) string {
	loc := dimStyle.Render(fmt.Sprintf("%s:%d", r.Path, r.Line))
	text := strings.TrimSpace(r.Text)
	if text == "" {
		text = dimStyle.Render("(empty line)")
	} else {
		text = codeStyle.Render(truncate(text, maxTextWidth))
	}
	return lines(headingStyle.Render("Current line")+"  "+loc, "  "+text)
}

func introduced(r Report) string {
	c := r.Commit
	out := []string{
		headingStyle.Render("Introduced / Changed"),
		"  Commit: " + hashStyle.Render(c.ShortHash),
		"  Author: " + c.AuthorName + " " + dimStyle.Render("<"+c.AuthorEmail+">"),
		"  Date:   " + c.AuthorDate.Format(dateLayout),
	}
	if r.Blame.OrigPath != "" && r.Blame.OrigPath != r.Path {
		out = append(out, "  Path:   "+r.Blame.OrigPath+" "+dimStyle.Render("(renamed since)"))
	}

	out = append(out, "", "  "+subjectStyle.Render(c.Subject))
	if c.Body != "" {
		out = append(out, "")
		out = append(out, indent(c.Body)...)
	}
	return lines(out...)
}

// pullRequest renders the pull request section, or nothing when GitHub
// was not consulted.
func pullRequest(r Report) string {
	g := r.GitHub
	if g == nil {
		return ""
	}
	heading := headingStyle.Render("Pull request")
	if g.PullRequest == nil {
		return lines(heading, "  "+dimStyle.Render("("+g.Note+")"))
	}

	pr := g.PullRequest
	out := []string{
		heading,
		"  " + hashStyle.Render("#"+strconv.Itoa(pr.Number)) + "  " + subjectStyle.Render(pr.Title),
		"  " + dimStyle.Render(pullRequestMeta(pr)),
	}
	for _, is := range pr.Issues {
		l := "  Closes " + hashStyle.Render("#"+strconv.Itoa(is.Number)) + "  " + is.Title
		if is.State == "OPEN" {
			l += " " + dimStyle.Render("(still open)")
		}
		out = append(out, l)
	}
	if body := pullRequestBody(pr.Body); body != "" {
		out = append(out, "")
		out = append(out, indent(body)...)
	}
	return lines(out...)
}

func pullRequestMeta(pr *github.PullRequest) string {
	var parts []string
	if pr.Author != "" {
		parts = append(parts, "by "+pr.Author)
	}
	switch pr.State {
	case "MERGED":
		parts = append(parts, "merged "+pr.MergedAt.Format(dateLayout))
	case "OPEN":
		parts = append(parts, "open")
	default:
		parts = append(parts, "closed without merging")
	}
	if pr.URL != "" {
		parts = append(parts, pr.URL)
	}
	return strings.Join(parts, " · ")
}

// pullRequestBody tidies a description for the terminal: template comments
// go, runs of blank lines collapse, and long descriptions stop at
// maxBodyLines.
func pullRequestBody(body string) string {
	body = htmlComment.ReplaceAllString(body, "")
	var out []string
	for l := range strings.SplitSeq(strings.ReplaceAll(body, "\r\n", "\n"), "\n") {
		l = strings.TrimRight(l, " \t")
		if l == "" && (len(out) == 0 || out[len(out)-1] == "") {
			continue
		}
		out = append(out, l)
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	if len(out) > maxBodyLines {
		out = append(out[:maxBodyLines:maxBodyLines], "…")
	}
	return strings.Join(out, "\n")
}

// reviewDiscussion renders the review threads on the target file.
func reviewDiscussion(r Report) string {
	if r.GitHub == nil || r.GitHub.PullRequest == nil {
		return ""
	}
	var mine []github.Thread
	others := 0
	for _, t := range r.GitHub.PullRequest.Threads {
		if t.Path == r.Path || t.Path == r.Blame.OrigPath {
			mine = append(mine, t)
		} else {
			others++
		}
	}

	out := []string{headingStyle.Render("Review discussion")}
	if len(mine) == 0 {
		note := "(no review comments on this file)"
		if others > 0 {
			note = fmt.Sprintf("(no review comments on this file; %s elsewhere in the pull request)", plural(others, "thread"))
		}
		return lines(out[0], "  "+dimStyle.Render(note))
	}

	for i, t := range mine {
		if i == maxThreads {
			out = append(out, "  "+dimStyle.Render(fmt.Sprintf("… and %s on this file", plural(len(mine)-maxThreads, "more thread"))))
			break
		}
		author := t.Author
		if author == "" {
			author = "ghost" // how GitHub shows a deleted account
		}
		out = append(out,
			"  "+loginStyle.Render("@"+author)+" "+dimStyle.Render("("+threadMeta(t)+")"),
			"    "+truncate(firstLine(t.Body), maxTextWidth))
	}
	if others > 0 {
		out = append(out, "  "+dimStyle.Render(fmt.Sprintf("… %s on other files", plural(others, "more thread"))))
	}
	return lines(out...)
}

func threadMeta(t github.Thread) string {
	var parts []string
	if t.Line > 0 {
		parts = append(parts, "L"+strconv.Itoa(t.Line))
	}
	if t.Outdated {
		parts = append(parts, "outdated")
	}
	if t.Resolved {
		parts = append(parts, "resolved")
	} else {
		parts = append(parts, "unresolved")
	}
	switch t.Replies {
	case 0:
	case 1:
		parts = append(parts, "1 reply")
	default:
		parts = append(parts, fmt.Sprintf("%d replies", t.Replies))
	}
	return strings.Join(parts, ", ")
}

func changedWith(files []string) string {
	out := []string{headingStyle.Render("Changed with")}
	if len(files) == 0 {
		return lines(append(out, "  "+dimStyle.Render("(no other files)"))...)
	}

	for i, f := range files {
		if i == maxChangedFiles {
			out = append(out, "  "+dimStyle.Render(fmt.Sprintf("… and %d more", len(files)-maxChangedFiles)))
			break
		}
		out = append(out, "  "+f)
	}
	return lines(out...)
}

func history(r Report) string {
	out := []string{headingStyle.Render("Line history")}
	for _, c := range r.History {
		out = append(out, fmt.Sprintf("  %s  %s  %s",
			hashStyle.Render(c.ShortHash), dimStyle.Render(c.AuthorDate.Format(dateLayout)), c.Subject))
	}

	if r.HistoryTruncated {
		b := r.Blame
		more := fmt.Sprintf("… more: git log -L %d,%d:%s %s", b.OrigLine, b.OrigLine, b.OrigPath, r.Commit.ShortHash)
		out = append(out, "  "+dimStyle.Render(more))
	}
	if r.Blame.Boundary {
		out = append(out, "  "+dimStyle.Render("(shallow clone: older history may be missing)"))
	}
	return lines(out...)
}

// firstLine returns the first line of prose in a comment, skipping fenced
// code such as a suggested change.
func firstLine(s string) string {
	s = fencedCode.ReplaceAllString(s, "")
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return "(suggested change)"
}

// indent prefixes every non-empty line with two spaces.
func indent(s string) []string {
	var out []string
	for l := range strings.SplitSeq(s, "\n") {
		if l = strings.TrimRight(l, " \t"); l != "" {
			l = "  " + l
		}
		out = append(out, l)
	}
	return out
}

// truncate shortens s to at most n runes, marking the cut with an ellipsis.
func truncate(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return string([]rune(s)[:n-1]) + "…"
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

func lines(ls ...string) string {
	return strings.Join(ls, "\n")
}
