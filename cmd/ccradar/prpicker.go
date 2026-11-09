package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// openPRs (o) opens the selected session's open PRs, one browser tab each,
// oldest first so the latest ends up in front. With none open (or no status
// known) it opens the latest PR. Past prOpenConfirmAbove it asks first.
func (m *model) openPRs() {
	d := m.selectedDetails()
	latest, ok := d.latestPR()
	if !ok {
		m.flash = "no PR for this session"
		return
	}
	var urls []string
	for _, l := range d.PRs {
		if m.prStatus[l.URL].State == "OPEN" {
			urls = append(urls, l.URL)
		}
	}
	fallback := len(urls) == 0
	if fallback {
		urls = []string{latest.URL}
	}
	if len(urls) > prOpenConfirmAbove && m.pendingOpen != latest.URL {
		m.pendingOpen = latest.URL
		m.flash = fmt.Sprintf("press o again to open %d open PRs", len(urls))
		return
	}
	m.pendingOpen = ""
	if err := openURLs(urls...); err != nil {
		m.flash = "couldn't open PRs: " + err.Error()
		return
	}
	switch {
	case fallback && len(d.PRs) > 1:
		m.flash = fmt.Sprintf("none open — opened the latest, #%d", latest.Number)
	case len(urls) == 1:
		m.flash = fmt.Sprintf("opened PR #%d", prNumber(d.PRs, urls[0]))
	default:
		m.flash = fmt.Sprintf("opened %d open PRs", len(urls))
	}
	if len(d.PRs) > len(urls) {
		m.flash += fmt.Sprintf(" · O to pick from all %d", len(d.PRs))
	}
}

func prNumber(prs []prLink, url string) int {
	for _, l := range prs {
		if l.URL == url {
			return l.Number
		}
	}
	return 0
}

// openPicker (O) lists all of the selected session's PRs, newest first, to
// open one or all. A single PR just opens.
func (m *model) openPicker() {
	d := m.selectedDetails()
	switch len(d.PRs) {
	case 0:
		m.flash = "no PR for this session"
		return
	case 1:
		m.openPRs()
		return
	}
	m.pickPRs = make([]prLink, len(d.PRs))
	for i, l := range d.PRs {
		m.pickPRs[len(d.PRs)-1-i] = l
	}
	m.pickCursor = 0
	m.picking = true
	m.flash = ""
}

// handlePickKey processes keys while the PR picker is open.
func (m model) handlePickKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c":
		return m, tea.Quit
	case "esc", "q", "O":
		m.picking = false
	case "up", "k":
		m.pickCursor = max(m.pickCursor-1, 0)
	case "down", "j":
		m.pickCursor = min(m.pickCursor+1, len(m.pickPRs)-1)
	case "enter":
		l := m.pickPRs[m.pickCursor]
		m.picking = false
		if err := openURLs(l.URL); err != nil {
			m.flash = "couldn't open PR: " + err.Error()
		} else {
			m.flash = fmt.Sprintf("opened PR #%d", l.Number)
		}
	case "a":
		urls := make([]string, len(m.pickPRs))
		for i, l := range m.pickPRs {
			urls[len(m.pickPRs)-1-i] = l.URL // oldest first: the latest ends up in front
		}
		m.picking = false
		if err := openURLs(urls...); err != nil {
			m.flash = "couldn't open PRs: " + err.Error()
		} else {
			m.flash = fmt.Sprintf("opened all %d PRs", len(urls))
		}
	}
	return m, nil
}

// pickerLines renders the picker in place of the list: exactly h lines of
// width w — a title, then the PRs (scrolled to keep the cursor visible).
func (m model) pickerLines(w, h int) []string {
	out := make([]string, 0, h)
	title := fmt.Sprintf("  PRs · %s", firstNonEmpty(displayTitle(m.selectedDetails(), m.selectedTitle()), "session"))
	out = append(out, styHeader.Render(truncPad(title, w)))
	numW := 0
	for _, l := range m.pickPRs {
		numW = max(numW, len(fmt.Sprintf("#%d", l.Number)))
	}
	room := max(h-1, 1)
	top := max(m.pickCursor-room+1, 0)
	for i := top; i < len(m.pickPRs) && len(out) < h; i++ {
		l := m.pickPRs[i]
		pl := prPreviewLine(l, m.prStatus[l.URL], numW, w-4)
		if i == m.pickCursor {
			out = append(out, "  "+styCursor.Render(truncPad("▸ "+pl.plain, w-2)))
		} else {
			out = append(out, padVis("    "+pl.rendered, 4+len([]rune(pl.plain)), w))
		}
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", w))
	}
	return out
}

// selectedTitle is the selected row's ai-title ("" if none).
func (m model) selectedTitle() string {
	if m.cursor >= len(m.rows) {
		return ""
	}
	switch r := m.rows[m.cursor]; r.kind {
	case rowSession:
		return r.session.Title
	case rowHist:
		return r.hist.Title
	}
	return ""
}
