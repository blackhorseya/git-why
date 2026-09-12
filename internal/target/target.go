// Package target parses and validates the <file>:<line> argument.
package target

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
)

var (
	// ErrSyntax means the argument is not shaped like <file>:<line>.
	ErrSyntax = errors.New("invalid target")
	// ErrLine means the line part is not a positive integer.
	ErrLine = errors.New("invalid line number")
	// ErrNotFound means the file does not exist.
	ErrNotFound = errors.New("file not found")
	// ErrIsDir means the path points at a directory.
	ErrIsDir = errors.New("path is a directory")
	// ErrOutOfRange means the file has fewer lines than requested.
	ErrOutOfRange = errors.New("line out of range")
)

// Target is a file path and a 1-based line number.
type Target struct {
	Path string
	Line int
}

// String formats the target back into <file>:<line> form.
func (x Target) String() string {
	return fmt.Sprintf("%s:%d", x.Path, x.Line)
}

// Parse splits s at its last colon into a path and a line number.
func Parse(s string) (Target, error) {
	i := strings.LastIndexByte(s, ':')
	if i <= 0 || i == len(s)-1 {
		return Target{}, fmt.Errorf("%w %q: expected <file>:<line>", ErrSyntax, s)
	}

	path, num := s[:i], s[i+1:]
	for _, r := range num {
		if r < '0' || r > '9' {
			return Target{}, fmt.Errorf("%w %q: must be a positive integer", ErrLine, num)
		}
	}

	n, err := strconv.Atoi(num)
	if err != nil {
		return Target{}, fmt.Errorf("%w %q: number too large", ErrLine, num)
	}
	if n == 0 {
		return Target{}, fmt.Errorf("%w 0: line numbers start at 1", ErrLine)
	}

	return Target{Path: path, Line: n}, nil
}

// ReadLine verifies that the target file exists and contains the requested
// line, and returns that line's text without its line terminator.
func (x Target) ReadLine() (string, error) {
	info, err := os.Stat(x.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("%w: %s", ErrNotFound, x.Path)
	}
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", x.Path, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%w: %s", ErrIsDir, x.Path)
	}

	data, err := os.ReadFile(x.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", x.Path, err)
	}

	lines := splitLines(data)
	if x.Line > len(lines) {
		return "", fmt.Errorf("%w: %s has %d lines, requested line %d", ErrOutOfRange, x.Path, len(lines), x.Line)
	}

	return strings.TrimSuffix(lines[x.Line-1], "\r"), nil
}

// splitLines splits data the way Git counts lines: a trailing newline does
// not start an extra empty line, and an empty file has no lines.
func splitLines(data []byte) []string {
	if len(data) == 0 {
		return nil
	}
	data = bytes.TrimSuffix(data, []byte("\n"))
	return strings.Split(string(data), "\n")
}
