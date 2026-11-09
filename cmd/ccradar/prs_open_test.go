package main

import (
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestPRBadgeCountsOtherPRs(t *testing.T) {
	prs := func(n int) sessionDetails {
		var d sessionDetails
		for i := 1; i <= n; i++ {
			d.PRs = append(d.PRs, prLink{URL: strconv.Itoa(i), Number: 5700 + i})
		}
		return d
	}
	st := map[string]prInfo{"3": {State: "OPEN", Checks: "SUCCESS"}}
	if plain, colored := prBadge(prs(3), st); plain != "#5703 ✓ +2" || lipgloss.Width(colored) != prColumnWidth {
		t.Errorf("3 PRs: %q (width %d)", plain, lipgloss.Width(colored))
	}
	if plain, _ := prBadge(prs(12), nil); plain != "#5712   9+" {
		t.Errorf("12 PRs: %q", plain)
	}
}

func TestOpenPRsOpensAllWithConfirmAboveFive(t *testing.T) {
	var opened [][]string
	prev := openURLs
	openURLs = func(urls ...string) error { opened = append(opened, urls); return nil }
	t.Cleanup(func() { openURLs = prev })

	mk := func(n int) model {
		var d sessionDetails
		for i := 1; i <= n; i++ {
			d.PRs = append(d.PRs, prLink{URL: "u" + strconv.Itoa(i), Number: i})
		}
		m := model{width: 120, height: 20}
		m.history = []HistEntry{{SessionID: "s", CWD: "/w", ModAt: time.Now(), sessionDetails: d}}
		m.view = viewHistory
		m.rebuild()
		return m
	}
	press := func(m model, k string) model {
		tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
		return tm.(model)
	}

	m := press(mk(3), "o")
	if len(opened) != 1 || strings.Join(opened[0], ",") != "u1,u2,u3" || m.flash != "opened 3 PRs" {
		t.Fatalf("3 PRs: opened %v, flash %q (want all, oldest first)", opened, m.flash)
	}
	if !strings.Contains(m.View(), "o open 3 PRs") {
		t.Error("help should say how many PRs o opens")
	}

	opened = nil
	m = press(mk(8), "o")
	if len(opened) != 0 || m.flash != "press o again to open all 8 PRs" {
		t.Fatalf("8 PRs, first press: opened %v, flash %q", opened, m.flash)
	}
	m = press(press(m, "j"), "o") // another key cancels; o asks again
	if len(opened) != 0 {
		t.Fatal("o after another key should ask again, not open")
	}
	m = press(m, "o")
	if len(opened) != 1 || len(opened[0]) != 8 {
		t.Fatalf("second o should open all 8: %v", opened)
	}

	opened = nil
	if m = press(mk(1), "o"); m.flash != "opened PR #1" || len(opened[0]) != 1 {
		t.Errorf("single PR: %q %v", m.flash, opened)
	}
}
