package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func mkRows(n int) []row {
	var rs []row
	for i := 0; i < n; i++ {
		if i%5 == 0 {
			rs = append(rs, row{kind: rowHeader, header: fmt.Sprintf("~/dir-%d", i/5)})
		}
		if i == n/2 {
			rs = append(rs, row{kind: rowDivider, header: "detached"}) // 2-line risk
		}
		rs = append(rs, row{kind: rowSession, session: Session{
			Status: "idle", CWD: "/x", Title: fmt.Sprintf("sess %d", i), SurfaceID: "g",
		}})
	}
	return rs
}

// renderedLines is how many screen lines a frame occupies (no trailing newline).
func renderedLines(out string) int { return strings.Count(out, "\n") + 1 }

func firstSelectable(rows []row) int {
	for i, r := range rows {
		if selectable(r.kind) {
			return i
		}
	}
	return -1
}

func TestSortModes(t *testing.T) {
	ss := []Session{
		{CWD: "/b", Title: "zebra", UpdatedAt: 100},
		{CWD: "/b", Title: "apple", UpdatedAt: 300}, // newest overall
		{CWD: "/a", Title: "mango", UpdatedAt: 200},
	}

	alpha := append([]Session(nil), ss...)
	(&model{sort: sortAlpha}).sortSessions(alpha)
	// directory A→Z, then title A→Z within a dir
	gotA := []string{alpha[0].CWD + ":" + alpha[0].Title, alpha[1].CWD + ":" + alpha[1].Title, alpha[2].CWD + ":" + alpha[2].Title}
	wantA := []string{"/a:mango", "/b:apple", "/b:zebra"}
	for i := range wantA {
		if gotA[i] != wantA[i] {
			t.Fatalf("alpha: got %v want %v", gotA, wantA)
		}
	}

	recent := append([]Session(nil), ss...)
	(&model{sort: sortRecent}).sortSessions(recent)
	// group /b is newest (300) so it comes first, newest row first within it
	gotR := []string{recent[0].CWD + ":" + recent[0].Title, recent[1].CWD + ":" + recent[1].Title, recent[2].CWD + ":" + recent[2].Title}
	wantR := []string{"/b:apple", "/b:zebra", "/a:mango"}
	for i := range wantR {
		if gotR[i] != wantR[i] {
			t.Fatalf("recent: got %v want %v", gotR, wantR)
		}
	}
}

func TestFuzzyMatch(t *testing.T) {
	cases := []struct {
		q, target string
		want      bool
	}{
		{"", "anything", true},                   // empty matches all
		{"math", "Review retirement math", true}, // substring
		{"rtmath", "retirement math", true},      // subsequence across words
		{"fire math", "~/dev/firefly Review math", true}, // two tokens AND
		{"fire xyz", "~/dev/firefly Review math", false}, // one token misses
		{"OPUS", "model opus-4-8", true},                 // case-insensitive
		{"zzz", "nothing here", false},
		{"#28", "~/dev/app fix login #12 #28", true},    // PR number: whole word
		{"#28", "~/dev/app fix login #128 #208", false}, // not a subsequence match
		{"#2", "~/dev/app fix login #28", false},
		{"app #12", "~/dev/app fix login #12", true},
		{"#12", "~/dev/app #12", true},
	}
	for _, c := range cases {
		if got := fuzzyMatch(c.q, c.target); got != c.want {
			t.Errorf("fuzzyMatch(%q,%q)=%v want %v", c.q, c.target, got, c.want)
		}
	}
}

func TestSearchFiltersRows(t *testing.T) {
	m := model{view: viewActive, width: 80, height: 40}
	m.sessions = []Session{
		{CWD: "/dev/firefly", Title: "retirement math", UpdatedAt: 1, SurfaceID: "g"},
		{CWD: "/dev/ccradar", Title: "build dashboard", UpdatedAt: 2, SurfaceID: "g"},
	}
	m.query = "fire"
	m.rebuild()
	if got := m.matchCount(); got != 1 {
		t.Fatalf("query %q: matchCount=%d want 1", m.query, got)
	}
	m.query = "dash"
	m.rebuild()
	if got := m.matchCount(); got != 1 {
		t.Fatalf("query %q: matchCount=%d want 1", m.query, got)
	}
	m.query = ""
	m.rebuild()
	if got := m.matchCount(); got != 2 {
		t.Fatalf("no query: matchCount=%d want 2", got)
	}
}

func TestObserveBusyToIdle(t *testing.T) {
	m := model{notify: true}
	busy := []Session{{SessionID: "a", Status: "busy", CWD: "/x"}}
	idle := []Session{{SessionID: "a", Status: "idle", CWD: "/x"}}

	if cmds := m.observe(busy); len(cmds) != 0 {
		t.Fatalf("first observe should not notify, got %d", len(cmds))
	}
	if cmds := m.observe(idle); len(cmds) != 1 {
		t.Fatalf("busy→idle should notify once, got %d", len(cmds))
	}
	if cmds := m.observe(idle); len(cmds) != 0 {
		t.Fatalf("idle→idle should not notify, got %d", len(cmds))
	}

	// busy→idle but notifications off → nothing
	m.notify = false
	m.observe(busy)
	if cmds := m.observe(idle); len(cmds) != 0 {
		t.Fatalf("notify off should not notify, got %d", len(cmds))
	}
}

func TestObserveBusyToWaiting(t *testing.T) {
	m := model{notify: true}
	busy := []Session{{SessionID: "a", Status: "busy", CWD: "/x"}}
	waiting := []Session{{SessionID: "a", Status: "waiting", CWD: "/x"}}
	idle := []Session{{SessionID: "a", Status: "idle", CWD: "/x"}}

	m.observe(busy) // seed
	if cmds := m.observe(waiting); len(cmds) != 1 {
		t.Fatalf("busy→waiting should notify once, got %d", len(cmds))
	}
	// waiting→idle is not a busy transition → no notification
	if cmds := m.observe(idle); len(cmds) != 0 {
		t.Fatalf("waiting→idle should not notify, got %d", len(cmds))
	}
	// idle→waiting is also not from busy → no notification
	m.observe(idle)
	if cmds := m.observe(waiting); len(cmds) != 0 {
		t.Fatalf("idle→waiting should not notify, got %d", len(cmds))
	}
}

func TestNotifyMessageByStatus(t *testing.T) {
	idle := Session{Title: "Build the thing", CWD: "/x/proj", Status: "idle"}
	wait := Session{Title: "Build the thing", CWD: "/x/proj", Status: "waiting"}
	if got := notifBody(idle); got != "✓ finished · proj" {
		t.Errorf("idle body = %q", got)
	}
	if got := notifBody(wait); got != "⏸ needs input · proj" {
		t.Errorf("waiting body = %q", got)
	}
}

func TestMatchTerminal(t *testing.T) {
	terms := []Terminal{
		{ID: "win-A", Tty: "/dev/ttys001", Title: "logs"},
		{ID: "win-B", Tty: "/dev/ttys002", Title: "claude"},
		{ID: "ghost-1", Tty: "", CWD: "/proj", Title: "Build the thing"},
	}
	used := map[string]bool{}

	// exact tty match wins regardless of title
	if got := matchTerminal(terms, used, Session{Tty: "/dev/ttys002", CWD: "/x"}); got != "win-B" {
		t.Fatalf("tty match: got %q want win-B", got)
	}
	// a claimed surface isn't reused
	if got := matchTerminal(terms, used, Session{Tty: "/dev/ttys002"}); got != "" {
		t.Fatalf("claimed surface reused: got %q", got)
	}
	// no tty → cwd+title fallback (Ghostty), only against tty-less surfaces
	if got := matchTerminal(terms, used, Session{CWD: "/proj", Title: "Build the thing"}); got != "ghost-1" {
		t.Fatalf("cwd+title fallback: got %q want ghost-1", got)
	}
	// unmatched tty and empty title → no match
	if got := matchTerminal(terms, used, Session{Tty: "/dev/ttys099"}); got != "" {
		t.Fatalf("unmatched: got %q", got)
	}
}

func TestMatchUntitled(t *testing.T) {
	terms := []Terminal{
		{ID: "shell", Tty: "", CWD: "/proj", Title: "zsh"},          // not claude
		{ID: "fresh", Tty: "", CWD: "/proj", Title: "Claude Code"},  // a new claude tab
		{ID: "other", Tty: "", CWD: "/elsewhere", Title: "Claude Code"},
	}
	used := map[string]bool{}

	// untitled session in /proj should pair with the "Claude Code" tab there
	if got := matchUntitled(terms, used, Session{CWD: "/proj"}); got != "fresh" {
		t.Fatalf("untitled match: got %q want fresh", got)
	}
	// the shell tab is never matched
	if used["shell"] {
		t.Fatal("plain shell surface should not be claimed")
	}
	// already-claimed surface isn't reused
	if got := matchUntitled(terms, used, Session{CWD: "/proj"}); got != "" {
		t.Fatalf("claimed surface reused: got %q", got)
	}
	// no claude-looking tab in the dir → no match
	if got := matchUntitled(terms, used, Session{CWD: "/nope"}); got != "" {
		t.Fatalf("unexpected match: got %q", got)
	}
}

func TestDetectDriver(t *testing.T) {
	t.Setenv("CCRADAR_TERM", "")
	t.Setenv("TERM_PROGRAM", "iTerm.app")
	if d := detectDriver(); d.Name() != "iterm2" {
		t.Fatalf("iTerm.app → %s", d.Name())
	}
	t.Setenv("TERM_PROGRAM", "Apple_Terminal")
	if d := detectDriver(); d.Name() != "terminal" {
		t.Fatalf("Apple_Terminal → %s", d.Name())
	}
	t.Setenv("TERM_PROGRAM", "ghostty")
	if d := detectDriver(); d.Name() != "ghostty" {
		t.Fatalf("ghostty → %s", d.Name())
	}
	t.Setenv("TERM_PROGRAM", "rex")
	if d := detectDriver(); d.Name() != "rex" {
		t.Fatalf("rex → %s", d.Name())
	}
	t.Setenv("CCRADAR_TERM", "terminal") // override wins
	t.Setenv("TERM_PROGRAM", "ghostty")
	if d := detectDriver(); d.Name() != "terminal" {
		t.Fatalf("override → %s", d.Name())
	}
}

func TestCmpSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"v0.2.0", "v0.2.1", -1},
		{"v0.2.1", "v0.2.1", 0},
		{"v0.3.0", "v0.2.9", 1},
		{"v1.0.0", "v0.9.9", 1},
		{"v0.2.1-rc1", "v0.2.1", 0}, // suffix ignored
		{"0.2.0", "v0.2.1", -1},     // missing leading v
		{"v0.10.0", "v0.9.0", 1},    // numeric, not lexical
	}
	for _, c := range cases {
		if got := cmpSemver(c.a, c.b); got != c.want {
			t.Errorf("cmpSemver(%q,%q)=%d want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestLatestVersionParsesGitHub(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("User-Agent") == "" { // GitHub requires it
			w.WriteHeader(http.StatusForbidden)
			return
		}
		io.WriteString(w, `{"tag_name":"v0.4.1","name":"v0.4.1","draft":false}`)
	}))
	defer srv.Close()
	old := latestReleaseURL
	latestReleaseURL = srv.URL
	defer func() { latestReleaseURL = old }()

	v, err := latestVersion()
	if err != nil || v != "v0.4.1" {
		t.Fatalf("latestVersion()=%q,%v want v0.4.1,nil", v, err)
	}
}

func TestLatestVersionQuietOnRateLimit(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden) // 403 = rate limited
	}))
	defer srv.Close()
	old := latestReleaseURL
	latestReleaseURL = srv.URL
	defer func() { latestReleaseURL = old }()

	if v, err := latestVersion(); v != "" || err != nil {
		t.Fatalf("rate-limited should be quiet: got %q,%v", v, err)
	}
}

func TestLatestIfNewerOptOutAndDevBuild(t *testing.T) {
	// opt-out short-circuits before any network call
	t.Setenv("CCRADAR_NO_UPDATE_CHECK", "1")
	if got := latestIfNewer(); got != "" {
		t.Fatalf("opt-out should return \"\", got %q", got)
	}
	// dev build (no embedded version) also returns "" without hitting the network
	t.Setenv("CCRADAR_NO_UPDATE_CHECK", "")
	if currentVersion() == "" {
		if got := latestIfNewer(); got != "" {
			t.Fatalf("dev build should return \"\", got %q", got)
		}
	}
}

func TestParseDockerSource(t *testing.T) {
	a := parseDockerSource("yogi-dev")
	if a.kind != srcDocker || a.selector != "yogi-dev" || a.label != "yogi-dev" || a.claudeDir != "/home/vscode/.claude" {
		t.Fatalf("default: %+v", a)
	}
	b := parseDockerSource("svc=/root/.claude")
	if b.selector != "svc" || b.claudeDir != "/root/.claude" {
		t.Fatalf("override: %+v", b)
	}
}

func TestParseDockerSessions(t *testing.T) {
	// US-delimited rows as the in-container script emits them.
	us := "\x1f"
	rows := strings.Join([]string{
		strings.Join([]string{"42", "sid-a", "/workspaces/app", "busy", "1000", "1", "Fix the bug", "claude-opus-4-8", "main", "fix the flaky test"}, us),
		strings.Join([]string{"7", "sid-dead", "/workspaces/app", "idle", "900", "0", "old", "claude-sonnet-4-6", "main", "x"}, us), // not alive → skipped
	}, "\n")

	got := parseDockerSessions(rows, "yogi-dev", "container123")
	if len(got) != 1 {
		t.Fatalf("want 1 live session, got %d", len(got))
	}
	s := got[0]
	if s.PID != 42 || s.SessionID != "sid-a" || s.CWD != "/workspaces/app" || s.Status != "busy" ||
		s.UpdatedAt != 1000 || s.Title != "Fix the bug" || s.Model != "claude-opus-4-8" ||
		s.Branch != "main" || s.LastPrompt != "fix the flaky test" ||
		s.Source != "yogi-dev" || s.Container != "container123" || !s.remote() {
		t.Fatalf("parsed wrong: %+v", s)
	}
}

func TestParseDockerHistory(t *testing.T) {
	us := "\x1f"
	rows := strings.Join([]string{
		strings.Join([]string{"1700000000", "sid-h", "/workspaces/app", "Add spans", "claude-opus-4-8", "main", "instrument the pipeline"}, us),
		strings.Join([]string{"1699999999", "sid-live", "/workspaces/app", "Live", "claude-opus-4-8", "main", "x"}, us),
		// last-record timestamp wins over an mtime bumped by bookkeeping records
		strings.Join([]string{"1800000000", "sid-ts", "/workspaces/app", "Old", "claude-opus-4-8", "main", "y", "2026-09-01T10:00:00.000Z"}, us),
	}, "\n")
	got := parseDockerHistory(rows, "yogi", "cid1", map[string]bool{"sid-live": true})
	if len(got) != 2 {
		t.Fatalf("want 2 (live excluded), got %d", len(got))
	}
	h := got[0]
	if h.SessionID != "sid-h" || h.CWD != "/workspaces/app" || h.Title != "Add spans" ||
		h.Branch != "main" || h.LastPrompt != "instrument the pipeline" ||
		h.Source != "yogi" || h.Container != "cid1" || h.ModAt.Unix() != 1700000000 {
		t.Fatalf("wrong: %+v", h)
	}
	if want := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC); !got[1].ModAt.Equal(want) {
		t.Fatalf("ModAt = %v, want record timestamp %v", got[1].ModAt, want)
	}
}

func TestMatchByTitle(t *testing.T) {
	terms := []Terminal{{ID: "host-tab", Tty: "/dev/ttys004", CWD: "/Users/me/x", Title: "✳ Fix the bug"}}
	used := map[string]bool{}
	// container session: tty/cwd differ, but the title reached the host tab
	s := Session{Title: "Fix the bug", CWD: "/workspaces/app", Tty: "/dev/pts/0", Container: "c1"}
	if got := matchByTitle(terms, used, s); got != "host-tab" {
		t.Fatalf("title match: got %q want host-tab", got)
	}
	if got := matchByTitle(terms, used, s); got != "" {
		t.Fatalf("claimed surface reused: %q", got)
	}
}

func TestUnderFilter(t *testing.T) {
	defer func() { dirFilter = "" }()

	dirFilter = "" // no filter → everything passes
	if !underFilter("/anything/at/all") {
		t.Fatal("no filter should match all")
	}

	setDirFilter("/Users/me/src/app")
	cases := []struct {
		cwd  string
		want bool
	}{
		{"/Users/me/src/app", true},          // exact
		{"/Users/me/src/app/api", true},      // subdir
		{"/Users/me/src/app/a/b/c", true},    // deep subdir
		{"/Users/me/src/application", false},  // prefix but not a subdir
		{"/Users/me/src", false},             // parent
		{"/Users/me/other", false},           // sibling
	}
	for _, c := range cases {
		if got := underFilter(c.cwd); got != c.want {
			t.Errorf("underFilter(%q)=%v want %v", c.cwd, got, c.want)
		}
	}
}

func TestPreviewRailFits(t *testing.T) {
	for _, view := range []viewKind{viewActive, viewHistory} {
		m := model{preview: true, width: 120, height: 20, view: view}
		m.sessions = demoSessions()
		m.history = demoHistory()
		m.rebuild()
		out := m.View()
		if got := renderedLines(out); got > m.height {
			t.Fatalf("view %d overflow: %d lines > %d", view, got, m.height)
		}
		for _, ln := range strings.Split(out, "\n") {
			if w := lipgloss.Width(ln); w > m.width {
				t.Fatalf("view %d line too wide: %d > %d", view, w, m.width)
			}
		}
	}
}

func TestScrollKeepsCursorVisibleNoOverflow(t *testing.T) {
	for _, height := range []int{10, 14, 25, 40} {
		m := model{width: 80, height: height}
		m.rows = mkRows(30)
		m.adjustScroll()
		// walk all the way down then back up
		for _, dir := range []int{1, -1} {
			for step := 0; step < 50; step++ {
				out := m.View()
				if lines := renderedLines(out); lines > m.height {
					t.Fatalf("overflow @h=%d: %d lines (cursor=%d top=%d)", m.height, lines, m.cursor, m.top)
				}
				v := m.visibleRows()
				if m.cursor < m.top || m.cursor >= m.top+v {
					t.Fatalf("cursor offscreen @h=%d: cursor=%d not in [%d,%d)", m.height, m.cursor, m.top, m.top+v)
				}
				m.move(dir)
			}
		}
		// after walking all the way up, the cursor must be back at the top row
		if want := firstSelectable(m.rows); m.cursor != want {
			t.Fatalf("@h=%d couldn't return to top: cursor=%d want %d", m.height, m.cursor, want)
		}
		if m.top != 0 {
			t.Fatalf("@h=%d scrolled-up view should have top=0, got %d", m.height, m.top)
		}
	}
}

func TestSortPreference(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if got := parseSort(loadConfig().Sort); got != sortRecent {
		t.Fatalf("default sort = %v, want recent", got)
	}
	model{sort: sortAlpha, notify: true}.savePrefs()
	c := loadConfig()
	if parseSort(c.Sort) != sortAlpha || !c.Notify {
		t.Fatalf("saved prefs not restored: %+v", c)
	}

	// A config saved before sort was remembered falls back to recent.
	if err := os.WriteFile(configPath(), []byte(`{"notify":false,"preview":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if c := loadConfig(); parseSort(c.Sort) != sortRecent || c.Notify || !c.Preview {
		t.Fatalf("legacy config: %+v", c)
	}
}

// A terminal can report 0 columns (e.g. under script(1), or before the first
// resize), which used to panic with a negative truncation width.
func TestZeroWidthDoesNotPanic(t *testing.T) {
	for _, view := range []viewKind{viewActive, viewHistory} {
		m := model{preview: true, view: view}
		m.sessions = demoSessions()
		m.history = demoHistory()
		m.rebuild()
		_ = m.View()
	}
}

func TestRailWidthDefaultsAndClamps(t *testing.T) {
	cases := []struct{ width, chosen, want int }{
		{100, 0, 38},   // narrow terminal: the fixed default
		{150, 0, 50},   // wide terminal: a third
		{240, 0, 80},   //
		{120, 200, 58}, // too wide for the terminal: list keeps 59 cols
		{120, 10, 28},  // too narrow: rail minimum
		{160, 52, 52},  // a chosen width that fits is kept
	}
	for _, c := range cases {
		m := model{width: c.width, railW: c.chosen}
		if got := m.railWidth(); got != c.want {
			t.Errorf("width %d, chosen %d: rail = %d, want %d", c.width, c.chosen, got, c.want)
		}
	}
}

func TestResizeRailKeys(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := model{preview: true, width: 120, height: 20}
	m.sessions = demoSessions()
	m.rebuild()
	press := func(k string) {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		m = tm.(model)
	}
	press("[")
	if m.railWidth() != 44 || m.flash != "preview 44 cols" {
		t.Fatalf("[ → rail %d (%q), want 44", m.railWidth(), m.flash)
	}
	if loadConfig().RailW != 44 {
		t.Error("width not saved")
	}
	for i := 0; i < 20; i++ {
		press("]")
	}
	if m.railWidth() != railMinWidth || !strings.Contains(m.flash, "narrowest") {
		t.Fatalf("after many ] → rail %d (%q)", m.railWidth(), m.flash)
	}
	press("[") // moves from the visible (clamped) width, not a runaway value
	if m.railWidth() != railMinWidth+railStep {
		t.Fatalf("[ after clamping → %d, want %d", m.railWidth(), railMinWidth+railStep)
	}

	m.preview = false
	press("[")
	if !strings.Contains(m.flash, "press p") || m.railW != railMinWidth+railStep {
		t.Errorf("resize with the preview off should only hint: %q, railW %d", m.flash, m.railW)
	}
}

func TestResizedRailNeverOverflows(t *testing.T) {
	for _, width := range []int{100, 120, 180} {
		for _, chosen := range []int{0, 1, 50, 500} {
			for _, view := range []viewKind{viewActive, viewHistory} {
				m := model{preview: true, width: width, height: 20, view: view, railW: chosen}
				m.sessions = demoSessions()
				m.history = demoHistory()
				m.rebuild()
				out := m.View()
				if got := renderedLines(out); got > m.height {
					t.Fatalf("w=%d rail=%d view=%d: %d lines > %d", width, chosen, view, got, m.height)
				}
				for _, ln := range strings.Split(out, "\n") {
					if w := lipgloss.Width(ln); w > width {
						t.Fatalf("w=%d rail=%d view=%d: line %d cols wide", width, chosen, view, w)
					}
				}
			}
		}
	}
}

func TestLoadingStateBeforeFirstLoad(t *testing.T) {
	m := model{width: 120, height: 20}
	out := stripStyles(m.View())
	if !strings.Contains(out, "loading live sessions…") || strings.Contains(out, "no live sessions") {
		t.Fatalf("before the first load:\n%s", out)
	}
	if !strings.Contains(out, "Active (…)") || !strings.Contains(out, "Historical (…)") {
		t.Errorf("tab counts should show … while loading: %q", m.tabBar())
	}

	tm, _ := m.Update(loadedMsg(nil))
	m = tm.(model)
	if out := stripStyles(m.View()); !strings.Contains(out, "no live sessions") || !strings.Contains(out, "Active (0)") {
		t.Errorf("after an empty load:\n%s", out)
	}
	m.view = viewHistory
	m.rebuild()
	if out := stripStyles(m.View()); !strings.Contains(out, "loading past sessions…") {
		t.Errorf("history still loading:\n%s", out)
	}
	tm, _ = m.Update(histMsg(nil))
	m = tm.(model)
	if out := stripStyles(m.View()); !strings.Contains(out, "no past sessions") || !strings.Contains(out, "Historical (0)") {
		t.Errorf("after an empty history load:\n%s", out)
	}
}
