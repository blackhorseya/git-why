// Package github finds the pull request behind a commit by asking the GitHub
// CLI (gh). Delegating to gh keeps authentication, multiple accounts and
// host configuration out of git-why.
package github

import (
	"net/url"
	"os"
	"slices"
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

// Hosts returns the GitHub hosts git-why recognises: github.com and, when
// GH_HOST is set, the GitHub Enterprise Server it names. gh reads the same
// variable, so both agree on which host's credentials to use.
func Hosts() []string {
	hosts := []string{"github.com"}
	if h := strings.ToLower(strings.TrimSpace(os.Getenv("GH_HOST"))); h != "" && h != "github.com" {
		hosts = append(hosts, h)
	}
	return hosts
}

// ParseRemote extracts the repository from a Git remote URL on one of hosts
// (compared case-insensitively). It understands every form Git accepts:
// scp-like (git@github.com:o/r.git), ssh://, https://, http:// and git://.
// It reports false for anything else, including repositories on other
// hosts.
func ParseRemote(raw string, hosts []string) (Remote, bool) {
	host, path := splitRemote(strings.TrimSpace(raw))
	host = strings.ToLower(host)
	if host == "" || !slices.Contains(hosts, host) {
		return Remote{}, false
	}

	path = strings.TrimSuffix(strings.Trim(path, "/"), ".git")
	owner, name, ok := strings.Cut(path, "/")
	if !ok || owner == "" || name == "" || strings.Contains(name, "/") {
		return Remote{}, false
	}
	return Remote{Host: host, Owner: owner, Name: name}, true
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
