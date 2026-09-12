// Package github finds the pull request behind a commit by asking the GitHub
// CLI (gh). Delegating to gh keeps authentication, multiple accounts and
// host configuration out of git-why.
package github

import (
	"net/url"
	"strings"
)

// Remote identifies a repository hosted on GitHub.
type Remote struct {
	Host  string
	Owner string
	Name  string
}

// String returns the repository as owner/name.
func (x Remote) String() string {
	return x.Owner + "/" + x.Name
}

// ParseRemote extracts the repository from a Git remote URL on github.com.
// It understands every form Git accepts: scp-like (git@github.com:o/r.git),
// ssh://, https://, http:// and git://. It reports false for anything else,
// including repositories on other hosts.
func ParseRemote(raw string) (Remote, bool) {
	host, path := splitRemote(strings.TrimSpace(raw))
	if !strings.EqualFold(host, "github.com") {
		return Remote{}, false
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return Remote{}, false
	}
	return Remote{Host: "github.com", Owner: owner, Name: name}, true
}

// splitRemote returns the host and path of a remote URL, or empty strings
// when raw is not a URL (for example a local directory).
func splitRemote(raw string) (host, path string) {
	if strings.Contains(raw, "://") {
		u, err := url.Parse(raw)
		if err != nil {
			return "", ""
		}
		return u.Hostname(), u.Path
	}

	// scp-like syntax: [user@]host:path
	if i := strings.Index(raw, "@"); i >= 0 {
		raw = raw[i+1:]
	}
	host, path, ok := strings.Cut(raw, ":")
	if !ok {
		return "", ""
	}
	return host, path
}
