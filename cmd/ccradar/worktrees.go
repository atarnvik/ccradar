package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// A stale worktree is a linked git worktree whose branch has pull requests on
// GitHub and all of them are merged or closed. ccradar finds the repositories
// your sessions ran in, lists their worktrees with `git worktree list`, asks
// GitHub (through gh, like PR badges) for PRs whose head is each worktree's
// branch, and offers to remove the stale ones under W with
// `git worktree remove` — which refuses a worktree with uncommitted changes,
// and leaves the branch alone. A worktree a live session is in (by the
// process's actual working directory) is never offered.

// staleWT is one removable worktree.
type staleWT struct {
	Path, Main, Branch string
	PRs                []wtPR // newest first
	Mod                time.Time
}

type wtPR struct {
	Number int
	State  string
}

type wtScanMsg struct{ stale []staleWT }

// startWTScan kicks off a scan over the loaded sessions' directories, unless
// one is running or PR lookups are off.
func (m *model) startWTScan() tea.Cmd {
	if demoMode() && !m.wtBusy {
		m.wtBusy = true
		return func() tea.Msg { return wtScanMsg{demoStaleWorktrees()} }
	}
	if m.wtBusy || !prStatusEnabled() {
		return nil
	}
	seen := map[string]bool{}
	var dirs, busy []string
	var pids []int
	add := func(d string) {
		if d != "" && !seen[d] {
			seen[d] = true
			dirs = append(dirs, d)
		}
	}
	for _, s := range m.sessions {
		if !s.remote() {
			add(s.CWD)
			busy = append(busy, s.CWD)
			pids = append(pids, s.PID)
		}
	}
	for _, h := range m.history {
		if h.Container == "" {
			add(h.CWD)
		}
	}
	if len(dirs) == 0 {
		return nil
	}
	m.wtBusy = true
	return func() tea.Msg {
		return wtScanMsg{scanWorktrees(dirs, append(busy, processCwds(pids)...))}
	}
}

// processCwds is the current working directory of each pid (a session may
// have moved into a worktree since it started).
func processCwds(pids []int) []string {
	if len(pids) == 0 {
		return nil
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	out, _ := exec.Command("lsof", "-a", "-d", "cwd", "-p", strings.Join(list, ","), "-Fn").Output()
	var dirs []string
	for _, ln := range strings.Split(string(out), "\n") {
		if d, ok := strings.CutPrefix(ln, "n"); ok && d != "" {
			dirs = append(dirs, d)
		}
	}
	return dirs
}

// gitRepoOf returns the main checkout of the repository dir is in ("" if
// none).
func gitRepoOf(dir string) string {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--path-format=absolute", "--git-common-dir").Output()
	if err != nil {
		return ""
	}
	common := filepath.Clean(strings.TrimSpace(string(out)))
	if filepath.Base(common) != ".git" {
		return "" // a bare repository: no main checkout
	}
	return filepath.Dir(common)
}

// worktree is one entry of `git worktree list --porcelain`.
type worktree struct {
	Path, Branch string
	Skip         bool // the main checkout, detached, locked or prunable
}

func listWorktrees(main string) []worktree {
	out, err := exec.Command("git", "-C", main, "worktree", "list", "--porcelain").Output()
	if err != nil {
		return nil
	}
	var wts []worktree
	for i, block := range strings.Split(strings.TrimSpace(string(out)), "\n\n") {
		var w worktree
		w.Skip = i == 0 // the first entry is the main checkout
		for _, ln := range strings.Split(block, "\n") {
			k, v, _ := strings.Cut(ln, " ")
			switch k {
			case "worktree":
				w.Path = v
			case "branch":
				w.Branch = strings.TrimPrefix(v, "refs/heads/")
			case "detached", "locked", "prunable", "bare":
				w.Skip = true
			}
		}
		if w.Branch == "" {
			w.Skip = true
		}
		wts = append(wts, w)
	}
	return wts
}

var ghRemote = regexp.MustCompile(`github\.com[:/]([A-Za-z0-9_.-]+)/([A-Za-z0-9_.-]+?)(?:\.git)?/?$`)

// githubRepo is the owner/name of main's origin on github.com ("" if none).
func githubRepo(main string) string {
	out, err := exec.Command("git", "-C", main, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	if sm := ghRemote.FindStringSubmatch(strings.TrimSpace(string(out))); sm != nil {
		return sm[1] + "/" + sm[2]
	}
	return ""
}

// scanWorktrees finds stale worktrees in the repos holding dirs, skipping any
// that contain one of busyDirs.
func scanWorktrees(dirs, busyDirs []string) []staleWT {
	mains := map[string]bool{}
	var mu sync.Mutex
	parallel(dirs, 8, func(d string) {
		if st, err := os.Stat(d); err != nil || !st.IsDir() {
			return
		}
		if main := gitRepoOf(d); main != "" {
			mu.Lock()
			mains[main] = true
			mu.Unlock()
		}
	})
	var stale []staleWT
	for main := range mains {
		repo := githubRepo(main)
		if repo == "" {
			continue
		}
		var cands []worktree
		for _, w := range listWorktrees(main) {
			if !w.Skip && !containsAny(w.Path, busyDirs) {
				cands = append(cands, w)
			}
		}
		prs := lookupBranchPRs(repo, cands)
		for _, w := range cands {
			list := prs[w.Branch]
			if len(list) == 0 {
				continue
			}
			done := true
			for _, p := range list {
				done = done && (p.State == "MERGED" || p.State == "CLOSED")
			}
			if !done {
				continue
			}
			st, err := os.Stat(w.Path)
			if err != nil {
				continue
			}
			stale = append(stale, staleWT{Path: w.Path, Main: main, Branch: w.Branch, PRs: list, Mod: st.ModTime()})
		}
	}
	sort.Slice(stale, func(i, j int) bool { return stale[i].Mod.After(stale[j].Mod) })
	return stale
}

// containsAny reports whether any of dirs is root or inside it.
func containsAny(root string, dirs []string) bool {
	for _, d := range dirs {
		if d == root || strings.HasPrefix(d, root+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func parallel(items []string, n int, f func(string)) {
	var wg sync.WaitGroup
	sem := make(chan struct{}, n)
	for _, it := range items {
		wg.Add(1)
		sem <- struct{}{}
		go func() { defer wg.Done(); defer func() { <-sem }(); f(it) }()
	}
	wg.Wait()
}

// lookupBranchPRs is branchPRs; tests replace it to stay offline.
var lookupBranchPRs = branchPRs

var branchName = regexp.MustCompile(`^[A-Za-z0-9._/@+-]+$`)

// branchPRs asks GitHub for the PRs whose head is each worktree's branch,
// trying gh's accounts like fetchPRs (the one that worked for the owner
// first). Branches it couldn't look up are missing from the result.
func branchPRs(repo string, wts []worktree) map[string][]wtPR {
	owner, name, _ := strings.Cut(repo, "/")
	var branches []string
	for _, w := range wts {
		if branchName.MatchString(w.Branch) {
			branches = append(branches, w.Branch)
		}
	}
	got := map[string][]wtPR{}
	if len(branches) == 0 {
		return got
	}
	accounts := ghAccounts()
	probe := prLink{Repo: repo}
	tried := map[int]bool{}
	for {
		i := pickAccount(probe, accounts, tried)
		if i < 0 {
			return got
		}
		tried[i] = true
		ok := false
		for start := 0; start < len(branches); start += prBatch {
			batch := branches[start:min(start+prBatch, len(branches))]
			res, seen := queryBranchPRs(owner, name, batch, accounts[i])
			ok = ok || seen
			for b, prs := range res {
				got[b] = prs
			}
		}
		if ok {
			rememberOwnerAccount(probe, accounts[i].login)
			return got
		}
	}
}

// queryBranchPRs runs one aliased GraphQL query for branches; seen is false
// when the account couldn't see the repository at all.
func queryBranchPRs(owner, name string, branches []string, acct ghAccount) (map[string][]wtPR, bool) {
	var b strings.Builder
	aliases := map[string]string{}
	for i, br := range branches {
		alias := fmt.Sprintf("b%d", i)
		aliases[alias] = br
		fmt.Fprintf(&b, `%s: repository(owner: %q, name: %q) { pullRequests(headRefName: %q, first: 20, orderBy: {field: CREATED_AT, direction: DESC}) { nodes { number state } } } `,
			alias, owner, name, br)
	}
	ctx, cancel := context.WithTimeout(context.Background(), prTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, ghBin(), "api", "graphql", "-f", "query=query { "+b.String()+"}")
	if acct.token != "" {
		cmd.Env = append(os.Environ(), "GH_TOKEN="+acct.token)
	}
	out, _ := cmd.Output()
	var resp struct {
		Data map[string]*struct {
			PullRequests struct {
				Nodes []wtPR `json:"nodes"`
			} `json:"pullRequests"`
		} `json:"data"`
	}
	got := map[string][]wtPR{}
	if json.Unmarshal(bytes.TrimSpace(out), &resp) != nil {
		return got, false
	}
	seen := false
	for alias, r := range resp.Data {
		if br, ok := aliases[alias]; ok && r != nil {
			seen = true
			got[br] = r.PullRequests.Nodes
		}
	}
	return got, seen
}

// staleIn returns the stale worktree dir is in (ok false if none), for
// header tags.
func (m model) staleIn(dir string) (staleWT, bool) {
	for _, s := range m.wtStale {
		if containsAny(s.Path, []string{dir}) {
			return s, true
		}
	}
	return staleWT{}, false
}

// openWorktrees (W) lists the stale worktrees and refreshes the scan.
func (m *model) openWorktrees() tea.Cmd {
	cmd := m.startWTScan()
	if len(m.wtStale) == 0 {
		if m.wtBusy {
			m.flash = "looking for stale worktrees…"
			m.wtOpenWhenDone = true
		} else {
			m.flash = "no stale worktrees (a worktree is stale when its branch's PRs are all merged or closed)"
		}
		return cmd
	}
	m.wtPicking, m.wtCursor, m.wtArmed = true, 0, ""
	m.flash = ""
	return cmd
}

// worktreesScanned takes in a scan's result.
func (m *model) worktreesScanned(msg wtScanMsg) {
	m.wtBusy = false
	m.wtStale = msg.stale
	if m.wtOpenWhenDone {
		m.wtOpenWhenDone = false
		if len(m.wtStale) > 0 {
			m.wtPicking, m.wtCursor, m.wtArmed = true, 0, ""
			m.flash = ""
		} else {
			m.flash = "no stale worktrees (a worktree is stale when its branch's PRs are all merged or closed)"
		}
	}
	if m.wtPicking {
		m.wtCursor = min(m.wtCursor, max(len(m.wtStale)-1, 0))
		if len(m.wtStale) == 0 {
			m.wtPicking = false
		}
	}
}

type wtRemovedMsg struct {
	removed []string
	failed  map[string]string // path → git's reason
}

// removeWorktreesCmd runs `git worktree remove` for each worktree, in turn.
func removeWorktreesCmd(wts []staleWT) tea.Cmd {
	return func() tea.Msg {
		msg := wtRemovedMsg{failed: map[string]string{}}
		for _, w := range wts {
			if demoMode() {
				msg.removed = append(msg.removed, w.Path) // pretend: demo paths aren't real
				continue
			}
			out, err := exec.Command("git", "-C", w.Main, "worktree", "remove", w.Path).CombinedOutput()
			if err != nil {
				reason := strings.TrimSpace(string(out))
				if reason == "" {
					reason = err.Error()
				}
				msg.failed[w.Path] = strings.TrimPrefix(reason, "fatal: ")
				continue
			}
			msg.removed = append(msg.removed, w.Path)
		}
		return msg
	}
}

// handleWorktreeKey processes keys while the worktree list is open. Removing
// takes a second press of the same key.
func (m model) handleWorktreeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	armed := m.wtArmed
	m.wtArmed = ""
	switch key {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "W":
		m.wtPicking = false
		m.flash = ""
	case "up", "k":
		m.wtCursor = max(m.wtCursor-1, 0)
		m.flash = ""
	case "down", "j":
		m.wtCursor = min(m.wtCursor+1, len(m.wtStale)-1)
		m.flash = ""
	case "enter", "d":
		w := m.wtStale[m.wtCursor]
		if armed != w.Path {
			m.wtArmed = w.Path
			m.flash = "press " + key + " again to remove " + wtName(w) + " (branch " + w.Branch + " is kept)"
			return m, nil
		}
		m.flash = "removing " + wtName(w) + "…"
		return m, removeWorktreesCmd([]staleWT{w})
	case "a":
		if armed != "all" {
			m.wtArmed = "all"
			m.flash = fmt.Sprintf("press a again to remove all %d worktrees (branches are kept)", len(m.wtStale))
			return m, nil
		}
		m.flash = fmt.Sprintf("removing %d worktrees…", len(m.wtStale))
		return m, removeWorktreesCmd(m.wtStale)
	}
	return m, nil
}

// worktreesRemoved drops removed worktrees from the list and reports.
func (m *model) worktreesRemoved(msg wtRemovedMsg) {
	gone := map[string]bool{}
	for _, p := range msg.removed {
		gone[p] = true
	}
	var keep []staleWT
	for _, s := range m.wtStale {
		if !gone[s.Path] {
			keep = append(keep, s)
		}
	}
	m.wtStale = keep
	switch {
	case len(msg.failed) == 0 && len(msg.removed) == 1:
		m.flash = "removed " + headerDir(msg.removed[0])
	case len(msg.failed) == 0:
		m.flash = fmt.Sprintf("removed %d worktrees", len(msg.removed))
	default:
		var path, reason string
		for path, reason = range msg.failed {
			break
		}
		m.flash = fmt.Sprintf("couldn't remove %s: %s", headerDir(path), firstLine(reason))
		if len(msg.removed) > 0 || len(msg.failed) > 1 {
			m.flash = fmt.Sprintf("removed %d, %d failed · %s", len(msg.removed), len(msg.failed), m.flash)
		}
	}
	m.wtCursor = min(m.wtCursor, max(len(m.wtStale)-1, 0))
	if len(m.wtStale) == 0 {
		m.wtPicking = false
	}
}

// wtName is how the list names a worktree: its path, shortened like headers.
func wtName(w staleWT) string { return headerDir(w.Path) }

// wtPickerLines renders the worktree list in place of the session list:
// exactly h lines of width w.
func (m model) wtPickerLines(w, h int) []string {
	out := make([]string, 0, h)
	out = append(out, styHeader.Render(truncPad("  Stale worktrees · every PR for the branch merged or closed", w)))
	nameW := 0
	for _, s := range m.wtStale {
		nameW = max(nameW, len([]rune(wtName(s))))
	}
	nameW = min(nameW, max(w-30, 12))
	room := max(h-1, 1)
	top := max(m.wtCursor-room+1, 0)
	for i := top; i < len(m.wtStale) && len(out) < h; i++ {
		s := m.wtStale[i]
		name := truncPad(truncEllipsis(wtName(s), nameW), nameW)
		latest := s.PRs[0]
		info := prInfo{State: latest.State}
		glyph, gs := stateGlyph(info)
		pr := fmt.Sprintf("#%d", latest.Number) + morePRs(len(s.PRs)-1)
		age := fmtAge(time.Since(s.Mod).Milliseconds())
		plain := name + "  " + glyph + " " + pr + "  " + age
		if i == m.wtCursor {
			out = append(out, "  "+styCursor.Render(truncPad("▸ "+plain, w-2)))
			continue
		}
		colored := styTitle.Render(name) + "  " + gs.Render(glyph) + " " + prStyle(info).Render(pr) + "  " + styDim.Render(age)
		out = append(out, padVis("    "+colored, 4+len([]rune(plain)), w))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}
