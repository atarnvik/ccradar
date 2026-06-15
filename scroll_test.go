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
		rs = append(rs, row{kind: rowSession, session: Session{
			Status: "idle", CWD: "/x", Title: fmt.Sprintf("sess %d", i), GhosttyID: "g",
		}})
	}
	return rs
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
		{CWD: "/dev/firefly", Title: "retirement math", UpdatedAt: 1, GhosttyID: "g"},
		{CWD: "/dev/ccradar", Title: "build dashboard", UpdatedAt: 2, GhosttyID: "g"},
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

func TestScrollKeepsCursorVisibleNoOverflow(t *testing.T) {
	m := model{width: 80, height: 14}
	m.rows = mkRows(30)
	m.adjustScroll()
	// walk all the way down then back up
	for _, dir := range []int{1, -1} {
		for step := 0; step < 40; step++ {
			out := m.View()
			if lines := strings.Count(out, "\n"); lines > m.height {
				t.Fatalf("overflow: %d lines > height %d (cursor=%d top=%d)", lines, m.height, m.cursor, m.top)
			}
			v := m.visibleRows()
			if m.cursor < m.top || m.cursor >= m.top+v {
				t.Fatalf("cursor offscreen: cursor=%d not in [%d,%d)", m.cursor, m.top, m.top+v)
			}
			m.move(dir)
		}
	}
}
