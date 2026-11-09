package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"sort"
	"strings"
	"sync"
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

// savePRCacheCmd writes a snapshot of the statuses to disk off the UI loop.
func savePRCacheCmd(status map[string]prInfo) tea.Cmd {
	snap := make(map[string]prInfo, len(status))
	for url, info := range status {
		snap[url] = info
	}
	return func() tea.Msg { savePRCache(snap); return nil }
}

func prStatusCmd(links []prLink) tea.Cmd {
	return func() tea.Msg { return prStatusMsg(fetchPRs(links)) }
}

// fetchPRs looks the links up with `gh api graphql`, trying every github.com
// account gh is logged into: PRs one account can't see (a private repo of
// another org) are retried with the next, and the account that worked is
// remembered per repo owner so later refreshes go straight to it. Every link
// gets an entry (zero State on failure) so failures back off too.
func fetchPRs(links []prLink) map[string]prInfo {
	now := time.Now()
	out := make(map[string]prInfo, len(links))
	for _, l := range links {
		out[l.URL] = prInfo{fetched: now}
	}
	if len(links) == 0 {
		return out
	}
	accounts := ghAccounts()
	tried := map[string]map[int]bool{} // url → accounts already asked
	remaining := links
	for round := 0; round < len(accounts) && len(remaining) > 0; round++ {
		groups := map[int][]prLink{}
		for _, l := range remaining {
			if i := pickAccount(l, accounts, tried[l.URL]); i >= 0 {
				if tried[l.URL] == nil {
					tried[l.URL] = map[int]bool{}
				}
				tried[l.URL][i] = true
				groups[i] = append(groups[i], l)
			}
		}
		// Each account's lookups run in parallel.
		found := make([]map[string]prInfo, len(accounts))
		var wg sync.WaitGroup
		for i := range accounts {
			if len(groups[i]) > 0 {
				wg.Add(1)
				go func() { defer wg.Done(); found[i] = queryPRs(groups[i], accounts[i]) }()
			}
		}
		wg.Wait()
		var next []prLink
		for i := range accounts {
			batch := groups[i]
			for _, l := range batch {
				info, ok := found[i][l.URL]
				if !ok {
					next = append(next, l)
					continue
				}
				info.fetched = now
				out[l.URL] = info
				rememberOwnerAccount(l, accounts[i].login)
			}
		}
		remaining = next
	}
	return out
}

// queryPRs fetches links as one account, a batch per request, in parallel.
func queryPRs(links []prLink, acct ghAccount) map[string]prInfo {
	got := map[string]prInfo{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for start := 0; start < len(links); start += prBatch {
		query, aliases := prQuery(links[start:min(start+prBatch, len(links))])
		if query == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := queryPRBatch(query, aliases, acct)
			mu.Lock()
			for url, info := range res {
				got[url] = info
			}
			mu.Unlock()
		}()
	}
	wg.Wait()
	return got
}

// queryPRBatch runs one GraphQL request as acct.
func queryPRBatch(query string, aliases map[string]string, acct ghAccount) map[string]prInfo {
	ctx, cancel := context.WithTimeout(context.Background(), prTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghBin(), "api", "graphql", "-f", "query="+query)
	if acct.token != "" {
		// Last GH_TOKEN wins, overriding any inherited one (or a wrapper's).
		cmd.Env = append(os.Environ(), "GH_TOKEN="+acct.token)
	}
	// gh exits non-zero when any PR in the batch is inaccessible, but still
	// prints the rest, so the output is parsed regardless.
	stdout, _ := cmd.Output()
	return parsePRResponse(stdout, aliases)
}

// ghAccount is a github.com account gh is logged into. An empty token means
// "whatever gh picks by default".
type ghAccount struct{ login, token string }

// ghAccounts lists gh's github.com accounts, active first, with their tokens.
// Without that list (an older gh, a GH_TOKEN-only setup) it returns gh's
// default account alone, which is how lookups worked before.
func ghAccounts() []ghAccount {
	ctx, cancel := context.WithTimeout(context.Background(), prTimeout)
	defer cancel()
	// Exits non-zero if any account has a problem; the JSON is still printed.
	out, _ := exec.CommandContext(ctx, ghBin(), "auth", "status", "--json", "hosts").Output()
	var st struct {
		Hosts map[string][]struct {
			Login  string `json:"login"`
			State  string `json:"state"`
			Active bool   `json:"active"`
		} `json:"hosts"`
	}
	_ = json.Unmarshal(out, &st)
	entries := st.Hosts["github.com"]
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Active && !entries[j].Active })
	// Tokens are fetched in parallel; order (active first) is kept.
	tokens := make([]string, len(entries))
	var wg sync.WaitGroup
	for i, e := range entries {
		if e.State != "success" || e.Login == "" {
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			if tok, err := exec.CommandContext(ctx, ghBin(), "auth", "token", "--user", e.Login).Output(); err == nil {
				tokens[i] = strings.TrimSpace(string(tok))
			}
		}()
	}
	wg.Wait()
	var accts []ghAccount
	for i, e := range entries {
		if tokens[i] != "" {
			accts = append(accts, ghAccount{login: e.Login, token: tokens[i]})
		}
	}
	if len(accts) == 0 {
		return []ghAccount{{}}
	}
	return accts
}

// ownerAccounts remembers which account could see each repo owner's PRs.
var ownerAccounts = struct {
	sync.Mutex
	m map[string]string
}{m: map[string]string{}}

func repoOwner(l prLink) string { owner, _, _ := strings.Cut(l.Repo, "/"); return strings.ToLower(owner) }

func rememberOwnerAccount(l prLink, login string) {
	if login == "" {
		return
	}
	ownerAccounts.Lock()
	ownerAccounts.m[repoOwner(l)] = login
	ownerAccounts.Unlock()
}

// pickAccount chooses the next account to ask about a PR: the one that worked
// for its owner before, else the first not yet tried (-1 when all were).
func pickAccount(l prLink, accounts []ghAccount, tried map[int]bool) int {
	ownerAccounts.Lock()
	known := ownerAccounts.m[repoOwner(l)]
	ownerAccounts.Unlock()
	for i, a := range accounts {
		if known != "" && a.login == known && !tried[i] {
			return i
		}
	}
	for i := range accounts {
		if !tried[i] {
			return i
		}
	}
	return -1
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

// prPreviewMax is how many of a session's latest PRs the preview lists.
const prPreviewMax = 5

// prOpenConfirmAbove is how many PRs `o` opens without asking first.
const prOpenConfirmAbove = 5

// prColumnWidth is the badge column: a state glyph, "#12345", and " +N" when
// the session has other PRs.
const prColumnWidth = 12

// prBadge renders a row's PR column (exactly prColumnWidth cells): the latest
// PR's state glyph (see stateGlyph), its number colored by state, and how many
// other PRs the session has. The glyph comes first so numbers line up.
func prBadge(d sessionDetails, status map[string]prInfo) (plain, colored string) {
	l, ok := d.latestPR()
	if !ok {
		blank := strings.Repeat(" ", prColumnWidth)
		return blank, blank
	}
	info := status[l.URL]
	glyph, gs := stateGlyph(info)
	num := fmt.Sprintf("#%d", l.Number)
	more := morePRs(len(d.PRs) - 1)
	pad := strings.Repeat(" ", max(prColumnWidth-2-len(num)-len(more), 0))
	return glyph + " " + num + more + pad, gs.Render(glyph) + " " + prStyle(info).Render(num) + styDim.Render(more) + pad
}

// morePRs is the badge's "other PRs" count: " +2", capped at " +99".
func morePRs(n int) string {
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf(" +%d", min(n, 99))
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
