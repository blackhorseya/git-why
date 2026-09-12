package github

import "testing"

func TestParseRemote(t *testing.T) {
	tests := []struct {
		raw  string
		want Remote
		ok   bool
	}{
		{"git@github.com:acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"git@github.com:acme/pay", Remote{"github.com", "acme", "pay"}, true},
		{"git@github.com:/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"github.com:acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"ssh://git@github.com/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"ssh://git@github.com:22/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"https://github.com/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"https://github.com/acme/pay", Remote{"github.com", "acme", "pay"}, true},
		{"https://github.com/acme/pay/", Remote{"github.com", "acme", "pay"}, true},
		{"https://GitHub.com/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"https://oauth2:secret@github.com/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"git://github.com/acme/pay.git", Remote{"github.com", "acme", "pay"}, true},
		{"https://github.com/acme/my.repo.git", Remote{"github.com", "acme", "my.repo"}, true},
		{"  https://github.com/acme/pay.git\n", Remote{"github.com", "acme", "pay"}, true},

		{"https://gitlab.com/acme/pay.git", Remote{}, false},
		{"git@github.example.com:acme/pay.git", Remote{}, false},
		{"https://github.com/acme", Remote{}, false},
		{"https://github.com/acme/pay/extra.git", Remote{}, false},
		{"https://github.com/", Remote{}, false},
		{"/srv/git/pay.git", Remote{}, false},
		{`C:\repos\pay`, Remote{}, false},
		{"", Remote{}, false},
	}
	for _, tt := range tests {
		got, ok := ParseRemote(tt.raw)
		if ok != tt.ok || got != tt.want {
			t.Errorf("ParseRemote(%q) = %+v, %v; want %+v, %v", tt.raw, got, ok, tt.want, tt.ok)
		}
	}
}

func TestRemoteString(t *testing.T) {
	if got := (Remote{"github.com", "acme", "pay"}).String(); got != "acme/pay" {
		t.Fatalf("String() = %q", got)
	}
}
