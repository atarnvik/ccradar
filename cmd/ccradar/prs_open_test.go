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

// prModel is a history list with one session whose PRs u1..un have the
// given states ("" = unknown), oldest first.
func prModel(states ...string) model {
	var d sessionDetails
	st := map[string]prInfo{}
	for i, s := range states {
		u := "u" + strconv.Itoa(i+1)
		d.PRs = append(d.PRs, prLink{URL: u, Number: i + 1})
		st[u] = prInfo{State: s, Title: "pr " + strconv.Itoa(i+1)}
	}
	m := model{width: 120, height: 20, view: viewHistory, prStatus: st}
	m.history = []HistEntry{{SessionID: "s", CWD: "/w", Title: "Social topics", ModAt: time.Now(), sessionDetails: d}}
	m.rebuild()
	return m
}

func pressKey(m model, k string) model {
	var msg tea.KeyMsg
	switch k {
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)}
	}
	tm, _ := m.Update(msg)
	return tm.(model)
}

func stubBrowser(t *testing.T) *[][]string {
	var opened [][]string
	prev := openURLs
	openURLs = func(urls ...string) error { opened = append(opened, urls); return nil }
	t.Cleanup(func() { openURLs = prev })
	return &opened
}

func TestOpenOpensOnlyOpenPRs(t *testing.T) {
	opened := stubBrowser(t)

	m := pressKey(prModel("MERGED", "OPEN", "CLOSED", "OPEN", "MERGED"), "o")
	if len(*opened) != 1 || strings.Join((*opened)[0], ",") != "u2,u4" {
		t.Fatalf("opened %v, want only the open PRs, oldest first", *opened)
	}
	if m.flash != "opened 2 open PRs · O to pick from all 5" {
		t.Errorf("flash = %q", m.flash)
	}
	if !strings.Contains(m.View(), "o open PRs · O pick") {
		t.Error("help should offer o and O")
	}

	*opened = nil
	m = pressKey(prModel("MERGED", "MERGED", "MERGED"), "o")
	if len(*opened) != 1 || strings.Join((*opened)[0], ",") != "u3" || m.flash != "none open — opened the latest, #3 · O to pick from all 3" {
		t.Errorf("none open: opened %v, flash %q (want the latest)", *opened, m.flash)
	}

	*opened = nil
	m = pressKey(prModel("", ""), "o") // no status known (no gh)
	if strings.Join((*opened)[0], ",") != "u2" {
		t.Errorf("unknown status: opened %v, want the latest", *opened)
	}

	*opened = nil
	many := prModel("OPEN", "OPEN", "OPEN", "OPEN", "OPEN", "OPEN", "MERGED")
	m = pressKey(many, "o")
	if len(*opened) != 0 || m.flash != "press o again to open 6 open PRs" {
		t.Fatalf("6 open, first press: opened %v, flash %q", *opened, m.flash)
	}
	m = pressKey(pressKey(m, "j"), "o") // another key cancels
	if len(*opened) != 0 {
		t.Fatal("o after another key should ask again")
	}
	if m = pressKey(m, "o"); len(*opened) != 1 || len((*opened)[0]) != 6 {
		t.Fatalf("second o should open all 6 open PRs: %v", *opened)
	}
}

func TestPRPicker(t *testing.T) {
	opened := stubBrowser(t)
	m := pressKey(prModel("MERGED", "OPEN", "CLOSED"), "O")
	if !m.picking || len(m.pickPRs) != 3 || m.pickPRs[0].Number != 3 {
		t.Fatalf("picker = %v %+v, want all 3 PRs newest first", m.picking, m.pickPRs)
	}
	out := m.View()
	for _, want := range []string{"PRs · Social topics", "▸ #3 × pr 3", "#2   pr 2", "#1 ⑂ pr 1", "enter open · a open all 3 · esc close"} {
		if !strings.Contains(stripStyles(out), want) {
			t.Errorf("picker view missing %q:\n%s", want, stripStyles(out))
		}
	}
	if got := renderedLines(out); got > m.height {
		t.Errorf("picker overflows: %d lines > %d", got, m.height)
	}

	m = pressKey(pressKey(m, "j"), "j")
	m = pressKey(pressKey(m, "j"), "enter") // cursor stays on the last
	if m.picking || len(*opened) != 1 || (*opened)[0][0] != "u1" || m.flash != "opened PR #1" {
		t.Fatalf("enter: picking=%v opened %v flash %q", m.picking, *opened, m.flash)
	}

	*opened = nil
	m = pressKey(pressKey(m, "O"), "a")
	if m.picking || strings.Join((*opened)[0], ",") != "u1,u2,u3" || m.flash != "opened all 3 PRs" {
		t.Fatalf("a: opened %v flash %q", *opened, m.flash)
	}

	*opened = nil
	m = pressKey(pressKey(m, "O"), "q")
	if m.picking || len(*opened) != 0 {
		t.Fatal("q closes the picker without opening (and without quitting)")
	}

	if m = pressKey(prModel("MERGED"), "O"); m.picking || strings.Join((*opened)[0], ",") != "u1" {
		t.Errorf("O with one PR should just open it: picking=%v %v", m.picking, *opened)
	}
}
