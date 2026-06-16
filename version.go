package main

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"strconv"
	"strings"
	"time"
)

// moduleProxyLatest is Go's module proxy endpoint for the newest tagged version
// — the same source `go install …@latest` consults. No auth, no GitHub rate limits.
const moduleProxyLatest = "https://proxy.golang.org/github.com/atarnvik/ccradar/@latest"

// version is injected at release time via -ldflags "-X main.version=...".
// (GoReleaser builds with `go build`, so runtime build info has no tag.)
var version string

// currentVersion is the running version (e.g. "v0.2.1"), or "" for a local/dev
// build where there's nothing to compare against. It prefers the ldflags value
// and falls back to module build info (the `go install …@version` path).
func currentVersion() string {
	if version != "" {
		if !strings.HasPrefix(version, "v") {
			return "v" + version
		}
		return version
	}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	v := bi.Main.Version
	if v == "" || v == "(devel)" {
		return ""
	}
	return v
}

// upgradeHint returns the command to update, based on how ccradar was installed:
// `brew upgrade` for a Homebrew install, otherwise `go install`.
func upgradeHint() string {
	if exe, err := os.Executable(); err == nil {
		if resolved, e := filepath.EvalSymlinks(exe); e == nil {
			exe = resolved // a cask symlinks bin/ccradar into the Caskroom
		}
		if strings.Contains(exe, "/Caskroom/") || strings.Contains(exe, "/Cellar/") {
			return "brew upgrade ccradar"
		}
	}
	return "go install github.com/atarnvik/ccradar@latest"
}

// displayVersion is the human-readable version for `ccradar --version`.
func displayVersion() string {
	if v := currentVersion(); v != "" {
		return "ccradar " + v
	}
	return "ccradar (dev build)"
}

// latestVersion asks the module proxy for the newest published version.
func latestVersion() (string, error) {
	c := http.Client{Timeout: 3 * time.Second}
	resp, err := c.Get(moduleProxyLatest)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var r struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", err
	}
	return r.Version, nil
}

// latestIfNewer returns the latest version when it's newer than the running
// build, else "". It's silent (returns "") on any error, dev build, or opt-out.
func latestIfNewer() string {
	if os.Getenv("CCRADAR_NO_UPDATE_CHECK") != "" {
		return ""
	}
	cur := currentVersion()
	if cur == "" {
		return ""
	}
	latest, err := latestVersion()
	if err != nil || latest == "" {
		return ""
	}
	if cmpSemver(cur, latest) < 0 {
		return latest
	}
	return ""
}

// cmpSemver compares vMAJOR.MINOR.PATCH tags: -1 if a<b, 0 if equal, 1 if a>b.
// Pre-release/build suffixes are ignored (good enough for update nudging).
func cmpSemver(a, b string) int {
	pa, pb := parseSemver(a), parseSemver(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	return 0
}

func parseSemver(s string) [3]int {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	var out [3]int
	for i, p := range strings.SplitN(s, ".", 3) {
		if i > 2 {
			break
		}
		out[i], _ = strconv.Atoi(p)
	}
	return out
}
