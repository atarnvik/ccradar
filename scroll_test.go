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
