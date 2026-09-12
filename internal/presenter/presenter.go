// Package presenter renders a git-why report for humans in a terminal.
package presenter

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"charm.land/lipgloss/v2"

	"github.com/blackhorseya/git-why/internal/git"
)

const (
	// maxChangedFiles caps the "Changed with" list for sweeping commits.
	maxChangedFiles = 10
	// maxTextWidth caps how much of the current line is shown.
	maxTextWidth = 100
	dateLayout   = "2006-01-02"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	headingStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Blue)
	hashStyle    = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	codeStyle    = lipgloss.NewStyle().Foreground(lipgloss.Green)
	subjectStyle = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
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
}

// Render writes the report to w. Colors are used only when w is a terminal
// that supports them; otherwise the output is plain text.
func Render(w io.Writer, r Report) error {
	sections := []string{
		titleStyle.Render("Why does this line exist?"),
		currentLine(r),
		introduced(r),
		changedWith(r.ChangedWith),
		history(r),
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

func lines(ls ...string) string {
	return strings.Join(ls, "\n")
}
