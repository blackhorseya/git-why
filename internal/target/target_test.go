package target

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		in      string
		want    Target
		wantErr error
	}{
		{in: "main.go:42", want: Target{Path: "main.go", Line: 42}},
		{in: "internal/payment/service.go:1", want: Target{Path: "internal/payment/service.go", Line: 1}},
		{in: "weird:name.go:7", want: Target{Path: "weird:name.go", Line: 7}},
		{in: "main.go", wantErr: ErrSyntax},
		{in: "main.go:", wantErr: ErrSyntax},
		{in: ":42", wantErr: ErrSyntax},
		{in: "", wantErr: ErrSyntax},
		{in: "main.go:0", wantErr: ErrLine},
		{in: "main.go:-1", wantErr: ErrLine},
		{in: "main.go:+1", wantErr: ErrLine},
		{in: "main.go:abc", wantErr: ErrLine},
		{in: "main.go:4 2", wantErr: ErrLine},
		{in: "main.go:99999999999999999999", wantErr: ErrLine},
	}

	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := Parse(tt.in)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Parse(%q) error = %v, want %v", tt.in, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("Parse(%q) unexpected error: %v", tt.in, err)
			}
			if got != tt.want {
				t.Fatalf("Parse(%q) = %+v, want %+v", tt.in, got, tt.want)
			}
		})
	}
}

func TestReadLine(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		t.Helper()
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	lf := write("lf.txt", "one\ntwo\nthree\n")
	noEOL := write("noeol.txt", "one\ntwo")
	crlf := write("crlf.txt", "one\r\ntwo\r\n")
	empty := write("empty.txt", "")

	tests := []struct {
		name    string
		target  Target
		want    string
		wantErr error
	}{
		{name: "first line", target: Target{lf, 1}, want: "one"},
		{name: "last line", target: Target{lf, 3}, want: "three"},
		{name: "past trailing newline", target: Target{lf, 4}, wantErr: ErrOutOfRange},
		{name: "no trailing newline", target: Target{noEOL, 2}, want: "two"},
		{name: "crlf", target: Target{crlf, 2}, want: "two"},
		{name: "empty file", target: Target{empty, 1}, wantErr: ErrOutOfRange},
		{name: "missing file", target: Target{filepath.Join(dir, "nope.txt"), 1}, wantErr: ErrNotFound},
		{name: "directory", target: Target{dir, 1}, wantErr: ErrIsDir},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.target.ReadLine()
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("ReadLine() error = %v, want %v", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("ReadLine() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("ReadLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
