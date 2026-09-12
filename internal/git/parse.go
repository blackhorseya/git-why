package git

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// commitFormat separates fields with the ASCII unit separator. The body is
// last so it may safely contain anything, including the separator itself.
// Combined with -z, each record ends in a NUL byte.
const commitFormat = "%H%x1f%h%x1f%aN%x1f%aE%x1f%aI%x1f%s%x1f%b"

const commitFields = 7

// parseBlame reads the first entry of `git blame --porcelain` output.
func parseBlame(out string) (BlameLine, error) {
	lines := strings.Split(out, "\n")
	header := strings.Fields(lines[0])
	if len(header) < 3 || !isHash(header[0]) {
		return BlameLine{}, fmt.Errorf("unexpected blame output: %q", lines[0])
	}

	orig, err := strconv.Atoi(header[1])
	if err != nil {
		return BlameLine{}, fmt.Errorf("unexpected blame line number %q: %w", header[1], err)
	}

	b := BlameLine{Hash: header[0], OrigLine: orig}
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "\t") {
			break // the line content ends the entry
		}
		key, val, _ := strings.Cut(l, " ")
		switch key {
		case "filename":
			b.OrigPath = unquote(val)
		case "boundary":
			b.Boundary = true
		}
	}

	if b.OrigPath == "" {
		return BlameLine{}, fmt.Errorf("blame output for %s has no filename", b.Hash)
	}
	return b, nil
}

// parseCommits reads NUL-terminated records produced with commitFormat.
func parseCommits(out string) ([]Commit, error) {
	var commits []Commit
	for rec := range strings.SplitSeq(out, "\x00") {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}

		f := strings.SplitN(rec, "\x1f", commitFields)
		if len(f) < commitFields-1 {
			return nil, fmt.Errorf("unexpected log record: %q", rec)
		}

		date, err := time.Parse(time.RFC3339, f[4])
		if err != nil {
			return nil, fmt.Errorf("unexpected author date %q: %w", f[4], err)
		}

		c := Commit{
			Hash:        f[0],
			ShortHash:   f[1],
			AuthorName:  f[2],
			AuthorEmail: f[3],
			AuthorDate:  date,
			Subject:     f[5],
		}
		if len(f) == commitFields {
			c.Body = strings.TrimSpace(f[6])
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// parseNames splits NUL-terminated path lists such as `-z --name-only`.
func parseNames(out string) []string {
	var names []string
	for n := range strings.SplitSeq(out, "\x00") {
		if n != "" {
			names = append(names, n)
		}
	}
	return names
}

// unquote reverses Git's C-style quoting of unusual file names.
func unquote(s string) string {
	if !strings.HasPrefix(s, `"`) {
		return s
	}
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return s
}

// isHash reports whether s looks like a full SHA-1 or SHA-256 object name.
func isHash(s string) bool {
	if len(s) != 40 && len(s) != 64 {
		return false
	}
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdef", r) {
			return false
		}
	}
	return true
}

// isZeroHash reports whether s is the all-zero name blame uses for lines
// that are not committed yet.
func isZeroHash(s string) bool {
	return strings.Trim(s, "0") == ""
}
