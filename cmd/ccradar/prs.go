package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// prLink is a pull request a session opened or worked on (a pr-link record).
type prLink struct {
	URL    string
	Repo   string // owner/name
	Number int
}

// prInfo is a PR's live status from GitHub. A zero State means the lookup
// failed (no gh, no access); it is retried after prRefresh like any other.
type prInfo struct {
	State   string // OPEN, MERGED, CLOSED
	Draft   bool
	Title   string
	Checks  string // rollup of the head commit: SUCCESS, FAILURE, ERROR, PENDING, EXPECTED, or ""
	Review  string // APPROVED, CHANGES_REQUESTED, REVIEW_REQUIRED, or ""
	fetched time.Time
}

const (
	prRefresh = 5 * time.Minute // how often an open PR's status is re-fetched
	prBatch   = 40              // PRs per GraphQL request
	prTimeout = 20 * time.Second
)

func ghBin() string { return envOr("CCRADAR_GH", "gh") }

// prStatusEnabled reports whether ccradar may look PRs up with gh. Off in demo
// mode and with CCRADAR_NO_PR_STATUS=1.
func prStatusEnabled() bool { return !demoMode() && os.Getenv("CCRADAR_NO_PR_STATUS") == "" }

// scanPRLink records a pr-link line. A PR seen again moves to the end, so the
// last entry is always the one the session touched most recently.
func (d *sessionDetails) scanPRLink(line []byte) {
	var r struct {
		Type   string `json:"type"`
		Number int    `json:"prNumber"`
		URL    string `json:"prUrl"`
		Repo   string `json:"prRepository"`
	}
	if json.Unmarshal(line, &r) != nil || r.Type != "pr-link" || r.URL == "" || r.Number == 0 {
		return
	}
	for i, p := range d.PRs {
		if p.URL == r.URL {
			d.PRs = append(d.PRs[:i], d.PRs[i+1:]...)
			break
		}
	}
	d.PRs = append(d.PRs, prLink{URL: r.URL, Repo: r.Repo, Number: r.Number})
}

// latestPR is the PR a session touched last.
func (d sessionDetails) latestPR() (prLink, bool) {
	if len(d.PRs) == 0 {
		return prLink{}, false
	}
	return d.PRs[len(d.PRs)-1], true
}

// duePRs returns the links whose status is unknown or stale. Merged and closed
// PRs are final, so they are fetched once.
func duePRs(links []prLink, have map[string]prInfo, now time.Time) []prLink {
	var due []prLink
	seen := map[string]bool{}
	for _, l := range links {
		if seen[l.URL] {
			continue
		}
		seen[l.URL] = true
		info, ok := have[l.URL]
		if ok && (info.State == "MERGED" || info.State == "CLOSED" || now.Sub(info.fetched) < prRefresh) {
			continue
		}
		due = append(due, l)
	}
	return due
}

type prStatusMsg map[string]prInfo

func prStatusCmd(links []prLink) tea.Cmd {
	return func() tea.Msg { return prStatusMsg(fetchPRs(links)) }
}

// fetchPRs looks the links up with `gh api graphql`, a batch per request. Every
// link gets an entry (zero State on failure) so failures back off too.
func fetchPRs(links []prLink) map[string]prInfo {
	now := time.Now()
	out := make(map[string]prInfo, len(links))
	for _, l := range links {
		out[l.URL] = prInfo{fetched: now}
	}
	for start := 0; start < len(links); start += prBatch {
		batch := links[start:min(start+prBatch, len(links))]
		query, aliases := prQuery(batch)
		if query == "" {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), prTimeout)
		// gh exits non-zero when any PR in the batch is inaccessible, but still
		// prints the rest, so the output is parsed regardless.
		stdout, _ := exec.CommandContext(ctx, ghBin(), "api", "graphql", "-f", "query="+query).Output()
		cancel()
		for url, info := range parsePRResponse(stdout, aliases) {
			info.fetched = now
			out[url] = info
		}
	}
	return out
}

var repoPart = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

// prQuery builds one GraphQL query fetching every link under an alias (p0, p1,
// …). aliases maps each alias back to its URL. Links with a malformed repo are
// skipped, which also keeps them from injecting into the query.
func prQuery(links []prLink) (query string, aliases map[string]string) {
	aliases = map[string]string{}
	var b strings.Builder
	for _, l := range links {
		owner, name, ok := strings.Cut(l.Repo, "/")
		if !ok || !repoPart.MatchString(owner) || !repoPart.MatchString(name) {
			continue
		}
		alias := fmt.Sprintf("p%d", len(aliases))
		aliases[alias] = l.URL
		fmt.Fprintf(&b, `%s: repository(owner: %q, name: %q) { pullRequest(number: %d) { state isDraft title reviewDecision commits(last: 1) { nodes { commit { statusCheckRollup { state } } } } } } `,
			alias, owner, name, l.Number)
	}
	if len(aliases) == 0 {
		return "", aliases
	}
	return "query { " + b.String() + "}", aliases
}

// parsePRResponse maps a GraphQL response back to PR URLs.
func parsePRResponse(out []byte, aliases map[string]string) map[string]prInfo {
	var resp struct {
		Data map[string]*struct {
			PullRequest *struct {
				State          string `json:"state"`
				IsDraft        bool   `json:"isDraft"`
				Title          string `json:"title"`
				ReviewDecision string `json:"reviewDecision"`
				Commits        struct {
					Nodes []struct {
						Commit struct {
							StatusCheckRollup *struct {
								State string `json:"state"`
							} `json:"statusCheckRollup"`
						} `json:"commit"`
					} `json:"nodes"`
				} `json:"commits"`
			} `json:"pullRequest"`
		} `json:"data"`
	}
	got := map[string]prInfo{}
	if json.Unmarshal(bytes.TrimSpace(out), &resp) != nil {
		return got
	}
	for alias, repo := range resp.Data {
		url, ok := aliases[alias]
		if !ok || repo == nil || repo.PullRequest == nil {
			continue
		}
		pr := repo.PullRequest
		info := prInfo{State: pr.State, Draft: pr.IsDraft, Title: pr.Title, Review: pr.ReviewDecision}
		if n := pr.Commits.Nodes; len(n) > 0 && n[0].Commit.StatusCheckRollup != nil {
			info.Checks = n[0].Commit.StatusCheckRollup.State
		}
		got[url] = info
	}
	return got
}

// prPreviewMax is how many of a session's latest PRs the preview lists (and
// have their status looked up).
const prPreviewMax = 5

// prOpenConfirmAbove is how many PRs `o` opens without asking first.
const prOpenConfirmAbove = 5

// prColumnWidth is the badge column: "#12345", a state glyph, and " +N" when
// the session has more PRs than the one shown.
const prColumnWidth = 10

// prBadge renders a row's PR column (exactly prColumnWidth cells): the latest
// PR's number colored by state, its state glyph (see stateGlyph), and how many
// other PRs the session has.
func prBadge(d sessionDetails, status map[string]prInfo) (plain, colored string) {
	l, ok := d.latestPR()
	if !ok {
		blank := strings.Repeat(" ", prColumnWidth)
		return blank, blank
	}
	num := truncPad(fmt.Sprintf("#%d", l.Number), 6)
	info := status[l.URL]
	glyph, gs := stateGlyph(info)
	more := morePRs(len(d.PRs) - 1)
	return num + glyph + more, prStyle(info).Render(num) + gs.Render(glyph) + styDim.Render(more)
}

// morePRs is the badge's 3-cell "other PRs" count: " +2", " 9+" past nine.
func morePRs(n int) string {
	switch {
	case n <= 0:
		return "   "
	case n <= 9:
		return fmt.Sprintf(" +%d", n)
	}
	return " 9+"
}

// Glyphs for PRs that are no longer open. ⑂ stands in for GitHub's merge
// icon, which only exists in Nerd Fonts.
const (
	prMergedGlyph = "⑂"
	prClosedGlyph = "×"
)

// stateGlyph is the one-cell marker after a PR's number: CI status for an
// open PR (✓ passing, ✗ failing, • running), ⑂ merged, × closed.
func stateGlyph(info prInfo) (string, lipgloss.Style) {
	switch info.State {
	case "MERGED":
		return prMergedGlyph, styPRMerged
	case "CLOSED":
		return prClosedGlyph, styPRClosed
	case "OPEN":
	default:
		return " ", styDim
	}
	switch info.Checks {
	case "SUCCESS":
		return "✓", styPROpen
	case "FAILURE", "ERROR":
		return "✗", styPRClosed
	case "PENDING", "EXPECTED":
		return "•", styBusy
	}
	return " ", styDim
}

func prStyle(info prInfo) lipgloss.Style {
	switch {
	case info.State == "OPEN" && !info.Draft:
		return styPROpen
	case info.State == "MERGED":
		return styPRMerged
	case info.State == "CLOSED":
		return styPRClosed
	}
	return styDim // draft or unknown
}

// openURLs opens URLs in the default browser, one tab each, in order (so the
// last ends up in front). A variable so tests can stand in for the browser.
var openURLs = func(urls ...string) error { return exec.Command("open", urls...).Start() }
