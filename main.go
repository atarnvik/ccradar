package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ---- styles ----

var (
	styHeader   = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	styBusy     = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	styIdle     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styWait     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	styDim      = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	styTitle    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	styTab      = lipgloss.NewStyle().Foreground(lipgloss.Color("44"))
	styCursor   = lipgloss.NewStyle().Background(lipgloss.Color("25")).Foreground(lipgloss.Color("231")).Bold(true)
	styHelp     = lipgloss.NewStyle().Foreground(lipgloss.Color("240"))
	styStatus   = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	styTabOn    = lipgloss.NewStyle().Background(lipgloss.Color("44")).Foreground(lipgloss.Color("16")).Bold(true)
	styTabOff   = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
)

// ---- model ----

type viewKind int

const (
	viewActive viewKind = iota
	viewHistory
)

type rowKind int

const (
	rowHeader rowKind = iota
	rowDivider
	rowSession
	rowHist
)

func selectable(k rowKind) bool { return k == rowSession || k == rowHist }

type row struct {
	kind    rowKind
	header  string
	session Session
	hist    HistEntry
}

type model struct {
	view     viewKind
	sessions []Session
	history  []HistEntry
	rows     []row
	cursor   int
	top      int // index of first visible row (scroll offset)
	width    int
	height   int
	flash    string
	pendingKill int // pid armed for kill confirmation (0 = none)
}

type tickMsg time.Time
type loadedMsg []Session
type histMsg []HistEntry

func tickCmd() tea.Cmd {
	return tea.Tick(2*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}
func loadActiveCmd() tea.Cmd {
	return func() tea.Msg { return loadedMsg(loadSessions()) }
}
func loadHistCmd(activeIDs map[string]bool) tea.Cmd {
	return func() tea.Msg { return histMsg(loadHistory(activeIDs)) }
}

func (m model) Init() tea.Cmd { return tea.Batch(loadActiveCmd(), tickCmd()) }

func dirDisplay(cwd string) string {
	home := homeDir()
	if home != "" && strings.HasPrefix(cwd, home) {
		return "~" + cwd[len(home):]
	}
	return cwd
}

func (m *model) rebuild() {
	var rows []row
	prev := ""
	header := func(cwd string) {
		if cwd != prev {
			prev = cwd
			rows = append(rows, row{kind: rowHeader, header: dirDisplay(cwd)})
		}
	}
	if m.view == viewActive {
		// Partition: focusable (has a Ghostty tab) vs detached (live but no tab).
		var open, detached []Session
		for _, s := range m.sessions {
			if s.GhosttyID != "" {
				open = append(open, s)
			} else {
				detached = append(detached, s)
			}
		}
		for _, s := range open {
			header(s.CWD)
			rows = append(rows, row{kind: rowSession, session: s})
		}
		if len(detached) > 0 {
			rows = append(rows, row{kind: rowDivider, header: "detached · no open tab (closed tab / tmux / ssh)"})
			for _, s := range detached {
				rows = append(rows, row{kind: rowSession, session: s})
			}
		}
	} else {
		for _, h := range m.history {
			header(h.CWD)
			rows = append(rows, row{kind: rowHist, hist: h})
		}
	}
	m.rows = rows
	m.clampCursor()
}

// chromeRows is the number of fixed (non-list) lines the view draws:
// tab bar, blank, sticky-header line, blank-before-footer, help, and flash.
func (m model) chromeRows() int {
	n := 5 // tabbar, blank, sticky line, blank-before-footer, help
	if m.flash != "" {
		n++
	}
	return n
}

// visibleRows is how many list rows fit on screen. Returns all rows when the
// window size isn't known yet (e.g. headless render) so nothing is hidden.
func (m model) visibleRows() int {
	if m.height <= 0 {
		return len(m.rows)
	}
	v := m.height - m.chromeRows()
	if v < 1 {
		v = 1
	}
	return v
}

// adjustScroll keeps the cursor inside the visible window.
func (m *model) adjustScroll() {
	v := m.visibleRows()
	if m.cursor < m.top {
		m.top = m.cursor
	}
	if m.cursor >= m.top+v {
		m.top = m.cursor - v + 1
	}
	if maxTop := len(m.rows) - v; m.top > maxTop {
		m.top = maxTop
	}
	if m.top < 0 {
		m.top = 0
	}
}

func (m *model) clampCursor() {
	defer m.adjustScroll()
	if len(m.rows) == 0 {
		m.cursor = 0
		return
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if !selectable(m.rows[m.cursor].kind) {
		for i := m.cursor; i < len(m.rows); i++ {
			if selectable(m.rows[i].kind) {
				m.cursor = i
				return
			}
		}
		for i := m.cursor; i >= 0; i-- {
			if selectable(m.rows[i].kind) {
				m.cursor = i
				return
			}
		}
	}
}

func (m *model) move(delta int) {
	i := m.cursor
	for {
		i += delta
		if i < 0 || i >= len(m.rows) {
			break
		}
		if selectable(m.rows[i].kind) {
			m.cursor = i
			break
		}
	}
	m.adjustScroll()
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.adjustScroll()
	case tickMsg:
		cmds := []tea.Cmd{loadActiveCmd(), tickCmd()}
		if m.view == viewHistory {
			cmds = append(cmds, loadHistCmd(activeSessionIDs(m.sessions)))
		}
		return m, tea.Batch(cmds...)
	case loadedMsg:
		m.sessions = []Session(msg)
		if m.view == viewActive {
			m.rebuild()
		}
	case histMsg:
		m.history = []HistEntry(msg)
		if m.view == viewHistory {
			m.rebuild()
		}
	case tea.KeyMsg:
		if msg.String() != "x" {
			m.pendingKill = 0 // any other key cancels a pending kill
		}
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "tab", "1", "2", "left", "right", "h", "l":
			return m.switchView(msg.String())
		case "r":
			if m.view == viewActive {
				return m, loadActiveCmd()
			}
			return m, loadHistCmd(activeSessionIDs(m.sessions))
		case "enter":
			m.activate()
		case "c":
			m.copyResume()
		case "x":
			m.killSelected()
		}
	}
	return m, nil
}

func (m model) switchView(key string) (tea.Model, tea.Cmd) {
	target := m.view
	switch key {
	case "1":
		target = viewActive
	case "2":
		target = viewHistory
	default: // tab / arrows toggle
		if m.view == viewActive {
			target = viewHistory
		} else {
			target = viewActive
		}
	}
	if target == m.view {
		return m, nil
	}
	m.view = target
	m.cursor = 0
	m.top = 0
	m.flash = ""
	m.pendingKill = 0
	m.rebuild()
	if m.view == viewHistory {
		return m, loadHistCmd(activeSessionIDs(m.sessions)) // refresh on entry
	}
	return m, nil
}

func (m *model) activate() {
	if m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	switch r.kind {
	case rowSession:
		if r.session.GhosttyID != "" {
			_ = focusTerminal(r.session.GhosttyID)
			m.flash = "→ focused " + filepath.Base(r.session.CWD)
		} else {
			m.flash = fmt.Sprintf("detached (pid %d) — no tab to focus; press x to kill", r.session.PID)
		}
	case rowHist:
		if err := openResumeTab(r.hist.CWD, r.hist.SessionID); err != nil {
			_ = copyResumeCommand(r.hist.CWD, r.hist.SessionID)
			m.flash = "couldn't open tab — resume command copied to clipboard"
		} else {
			m.flash = "↻ resuming " + filepath.Base(r.hist.CWD) + " in a new tab"
		}
	}
}

func (m *model) killSelected() {
	if m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	if r.kind != rowSession || r.session.GhosttyID != "" {
		m.flash = "x only kills detached (no-tab) sessions"
		return
	}
	pid := r.session.PID
	if m.pendingKill == pid {
		if err := killProcess(pid); err != nil {
			m.flash = fmt.Sprintf("kill pid %d failed: %v", pid, err)
		} else {
			m.flash = fmt.Sprintf("sent SIGTERM to pid %d", pid)
		}
		m.pendingKill = 0
	} else {
		m.pendingKill = pid
		m.flash = fmt.Sprintf("press x again to kill pid %d (%s)", pid, filepath.Base(r.session.CWD))
	}
}

func (m *model) copyResume() {
	if m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	if r.kind == rowHist {
		if copyResumeCommand(r.hist.CWD, r.hist.SessionID) == nil {
			m.flash = "copied: cd … && " + resumeCommand(r.hist.SessionID)
		}
	}
}

// ---- view ----

// statusText returns the fixed-width (7-col) status label and its color style.
func statusText(status string) (string, lipgloss.Style) {
	switch status {
	case "busy":
		return "● busy ", styBusy
	case "idle":
		return "○ idle ", styIdle
	case "waiting":
		return "◆ wait ", styWait
	default:
		w := status
		if len(w) > 5 {
			w = w[:5]
		}
		return fmt.Sprintf("· %-5s", w), styDim
	}
}

func titleOr(t string) string {
	if t == "" {
		return "(no title)"
	}
	return t
}

// truncPad truncates or space-pads s to exactly n display cells (rune-based).
func truncPad(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s + strings.Repeat(" ", n-len(r))
}

func (m model) tabBar() string {
	open, detached := 0, 0
	for _, s := range m.sessions {
		if s.GhosttyID != "" {
			open++
		} else {
			detached++
		}
	}
	a := fmt.Sprintf(" Active (%d) ", open)
	if detached > 0 {
		a = fmt.Sprintf(" Active (%d, +%d detached) ", open, detached)
	}
	h := fmt.Sprintf(" Historical (%d) ", len(m.history))
	if m.view == viewActive {
		return styTabOn.Render(a) + " " + styTabOff.Render(h)
	}
	return styTabOff.Render(a) + " " + styTabOn.Render(h)
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.tabBar() + "\n\n")

	// Sticky line: when scrolled, show the directory header that governs the
	// first visible row (context) plus how many rows are hidden above.
	if m.top > 0 {
		sticky := -1
		for j := m.top - 1; j >= 0; j-- {
			if m.rows[j].kind == rowHeader {
				sticky = j
				break
			}
		}
		hint := styHelp.Render(fmt.Sprintf("↑ %d more", m.top))
		if sticky >= 0 {
			b.WriteString(styHeader.Render(m.rows[sticky].header) + "  " + hint + "\n")
		} else {
			b.WriteString("  " + hint + "\n")
		}
	} else {
		b.WriteString("\n")
	}

	if len(m.rows) == 0 {
		if m.view == viewActive {
			b.WriteString(styDim.Render("  no live sessions") + "\n")
		} else {
			b.WriteString(styDim.Render("  no past sessions") + "\n")
		}
	}

	v := m.visibleRows()
	end := m.top + v
	if end > len(m.rows) {
		end = len(m.rows)
	}
	for i := m.top; i < end; i++ {
		r := m.rows[i]
		if r.kind == rowHeader {
			b.WriteString(styHeader.Render(r.header) + "\n")
			continue
		}
		if r.kind == rowDivider {
			b.WriteString("\n" + styHelp.Render("  ── "+r.header+" ──") + "\n")
			continue
		}

		// Build plain and colored variants with identical column widths so the
		// row lines up whether it's the highlighted (plain) or normal (colored) form.
		var plain, colored string
		switch r.kind {
		case rowSession:
			s := r.session
			sp, sst := statusText(s.Status)
			age := fmt.Sprintf("%4s", fmtAge(nowMs()-s.UpdatedAt))
			titleF := truncPad(titleOr(s.Title), 44)
			if s.GhosttyID != "" {
				loc := "→ tab"
				plain = fmt.Sprintf("%s %s  %s  %s", sp, age, titleF, loc)
				colored = fmt.Sprintf("%s %s  %s  %s",
					sst.Render(sp), styDim.Render(age), styTitle.Render(titleF), styTab.Render(loc))
			} else {
				loc := fmt.Sprintf("pid %d", s.PID)
				plain = fmt.Sprintf("%s %s  %s  %s", sp, age, titleF, loc)
				colored = styDim.Render(plain) // detached: muted whole row
			}
		case rowHist:
			h := r.hist
			age := fmt.Sprintf("%4s", fmtAge(time.Since(h.ModAt).Milliseconds()))
			titleF := truncPad(titleOr(h.Title), 44)
			plain = fmt.Sprintf("%s  %s  %s", age, titleF, "↻ resume")
			colored = fmt.Sprintf("%s  %s  %s",
				styDim.Render(age), styTitle.Render(titleF), styTab.Render("↻ resume"))
		}

		if i == m.cursor {
			text := "▸ " + plain
			sel := styCursor
			if m.width > 2 {
				sel = sel.Width(m.width - 2) // fill the row for a solid bar
			}
			b.WriteString("  " + sel.Render(text) + "\n")
		} else {
			b.WriteString("    " + colored + "\n")
		}
	}

	if end < len(m.rows) {
		b.WriteString(styHelp.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)) + "\n")
	} else {
		b.WriteString("\n")
	}
	if m.flash != "" {
		b.WriteString(styStatus.Render("  "+m.flash) + "\n")
	}
	help := "  ↑/↓ move · tab switch view · enter "
	if m.view == viewActive {
		help += "focus tab · x kill detached"
	} else {
		help += "resume (new tab) · c copy cmd"
	}
	help += " · r refresh · q quit"
	b.WriteString(styHelp.Render(help) + "\n")
	return b.String()
}

// ---- entry ----

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "dump" || os.Args[1] == "render") {
		debugMode(os.Args[1])
		return
	}
	if _, err := tea.NewProgram(model{}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func debugMode(which string) {
	sessions := loadSessions()
	if which == "dump" {
		fmt.Println("# active")
		for _, s := range sessions {
			tab := s.GhosttyID
			if tab == "" {
				tab = "[no tab]"
			}
			fmt.Printf("%-7s %4s  %-22s %-46s %s\n",
				s.Status, fmtAge(nowMs()-s.UpdatedAt), filepath.Base(s.CWD), titleOr(s.Title), tab)
		}
		fmt.Println("# history")
		for _, h := range loadHistory(activeSessionIDs(sessions)) {
			fmt.Printf("%4s  %-22s %-46s %s\n",
				fmtAge(time.Since(h.ModAt).Milliseconds()), filepath.Base(h.CWD), titleOr(h.Title), h.SessionID)
		}
		return
	}
	// render: one frame of each view
	m := model{sessions: sessions, history: loadHistory(activeSessionIDs(sessions)), width: 100, height: 40}
	m.rebuild()
	fmt.Println("=== ACTIVE ===")
	fmt.Print(m.View())
	m.view = viewHistory
	m.cursor = 0
	m.rebuild()
	fmt.Println("\n=== HISTORICAL ===")
	fmt.Print(m.View())
}
