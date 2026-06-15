package main

import (
	"fmt"
	"strings"
	"testing"
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
	t.Setenv("CCRADAR_TERM", "terminal") // override wins
	t.Setenv("TERM_PROGRAM", "ghostty")
	if d := detectDriver(); d.Name() != "terminal" {
		t.Fatalf("override → %s", d.Name())
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
