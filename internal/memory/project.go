package memory

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// DetectID returns a stable identifier for the project rooted at dir, used to
// scope memory notes. It prefers the normalized git remote ("owner/repo"); with
// no remote it falls back to the absolute path. This mirrors how engram scopes
// memory per project.
func DetectID(dir string) string {
	if r := gitRemote(dir); r != "" {
		return r
	}
	if abs, err := filepath.Abs(dir); err == nil {
		return abs
	}
	return dir
}

func gitRemote(dir string) string {
	cmd := exec.Command("git", "-C", dir, "remote", "get-url", "origin")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return normalizeRemote(strings.TrimSpace(string(out)))
}

// normalizeRemote turns a git URL into "owner/repo", lowercased. Unrecognized
// shapes are returned trimmed of a trailing ".git".
func normalizeRemote(url string) string {
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.HasPrefix(url, "git@"):
		// git@github.com:owner/repo
		if _, rest, ok := strings.Cut(url, ":"); ok {
			url = rest
		}
	case strings.Contains(url, "://"):
		// https://github.com/owner/repo  |  ssh://git@host/owner/repo
		if _, rest, ok := strings.Cut(url, "://"); ok {
			if _, path, ok := strings.Cut(rest, "/"); ok {
				url = path
			}
		}
	}
	url = strings.TrimPrefix(url, "git@")
	parts := strings.Split(strings.Trim(url, "/"), "/")
	if len(parts) >= 2 {
		return strings.ToLower(strings.Join(parts[len(parts)-2:], "/"))
	}
	return strings.ToLower(url)
}
