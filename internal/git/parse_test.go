package git

import (
	"testing"
	"time"
)

const (
	hashA = "56f81483490135019652253a32451bbdb5fdf2c5"
	hashB = "586e3aa01ea5ba304a5661a61ccce3797d2c04ac"
)

func TestParseBlame(t *testing.T) {
	tests := []struct {
		name    string
		out     string
		want    BlameLine
		wantErr bool
	}{
		{
			name: "regular entry",
			out: hashA + " 7 12 1\n" +
				"author Ada\nauthor-mail <ada@example.com>\nauthor-time 1789182373\nauthor-tz +0800\n" +
				"committer Ada\ncommitter-mail <ada@example.com>\ncommitter-time 1789182373\ncommitter-tz +0800\n" +
				"summary change b\nprevious " + hashB + " old/f.txt\nfilename old/f.txt\n\tB\n",
			want: BlameLine{Hash: hashA, OrigLine: 7, OrigPath: "old/f.txt"},
		},
		{
			name: "boundary commit",
			out:  hashA + " 1 1 1\nsummary init\nboundary\nfilename f.txt\n\tA\n",
			want: BlameLine{Hash: hashA, OrigLine: 1, OrigPath: "f.txt", Boundary: true},
		},
		{
			name: "quoted non-ascii filename",
			out:  hashA + " 1 1 1\nfilename \"docs/\\350\\252\\252\\346\\230\\216.md\"\n\tA\n",
			want: BlameLine{Hash: hashA, OrigLine: 1, OrigPath: "docs/說明.md"},
		},
		{
			name: "content line that looks like a header is ignored",
			out:  hashA + " 3 3 1\nfilename f.txt\n\tfilename evil.txt\n",
			want: BlameLine{Hash: hashA, OrigLine: 3, OrigPath: "f.txt"},
		},
		{name: "empty output", out: "", wantErr: true},
		{name: "not a hash", out: "fatal: nope\n", wantErr: true},
		{name: "missing filename", out: hashA + " 1 1 1\nsummary x\n\tA\n", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBlame(tt.out)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("parseBlame() = %+v, want error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseBlame() unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("parseBlame() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseCommits(t *testing.T) {
	record := func(fields ...string) string {
		s := ""
		for i, f := range fields {
			if i > 0 {
				s += "\x1f"
			}
			s += f
		}
		return s + "\x00"
	}

	out := record(hashA, "56f8148", "Ada Lovelace", "ada@example.com", "2026-09-12T11:10:31+08:00",
		"fix: prevent duplicate payment processing", "Body line 1\nBody line 2\n") +
		"\n" + // some git versions emit a newline between -z records
		record(hashB, "586e3aa", "Grace Hopper", "grace@example.com", "2026-09-01T09:00:00Z",
			"feat: initial payment service", "")

	got, err := parseCommits(out)
	if err != nil {
		t.Fatalf("parseCommits() unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("parseCommits() returned %d commits, want 2", len(got))
	}

	first := got[0]
	if first.Hash != hashA || first.ShortHash != "56f8148" || first.AuthorName != "Ada Lovelace" ||
		first.AuthorEmail != "ada@example.com" || first.Subject != "fix: prevent duplicate payment processing" {
		t.Errorf("first commit = %+v", first)
	}
	if first.Body != "Body line 1\nBody line 2" {
		t.Errorf("first commit body = %q", first.Body)
	}
	if want := time.Date(2026, 9, 12, 3, 10, 31, 0, time.UTC); !first.AuthorDate.Equal(want) {
		t.Errorf("first commit date = %v, want %v", first.AuthorDate, want)
	}
	if got[1].Body != "" || got[1].Subject != "feat: initial payment service" {
		t.Errorf("second commit = %+v", got[1])
	}
}

func TestParseCommitsWithoutBodyField(t *testing.T) {
	out := hashA + "\x1f56f8148\x1fAda\x1fada@example.com\x1f2026-09-12T11:10:31+08:00\x1fsubject\x00"
	got, err := parseCommits(out)
	if err != nil {
		t.Fatalf("parseCommits() unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].Subject != "subject" || got[0].Body != "" {
		t.Fatalf("parseCommits() = %+v", got)
	}
}

func TestParseCommitsErrors(t *testing.T) {
	for name, out := range map[string]string{
		"too few fields": hashA + "\x1f56f8148\x1fAda\x00",
		"bad date":       hashA + "\x1f56f8148\x1fAda\x1fa@x\x1fyesterday\x1fsubject\x1f\x00",
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := parseCommits(out); err == nil {
				t.Fatalf("parseCommits() = %+v, want error", got)
			}
		})
	}
}

func TestParseNames(t *testing.T) {
	got := parseNames("a.go\x00dir/b c.go\x00docs/說明.md\x00")
	want := []string{"a.go", "dir/b c.go", "docs/說明.md"}
	if len(got) != len(want) {
		t.Fatalf("parseNames() = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("parseNames() = %q, want %q", got, want)
		}
	}
	if got := parseNames(""); len(got) != 0 {
		t.Fatalf("parseNames(\"\") = %q, want empty", got)
	}
}

func TestIsZeroHash(t *testing.T) {
	if !isZeroHash("0000000000000000000000000000000000000000") {
		t.Error("isZeroHash(40 zeros) = false")
	}
	if isZeroHash(hashA) {
		t.Errorf("isZeroHash(%s) = true", hashA)
	}
}
