package main

import (
	"fmt"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

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
		cmds := m.observe([]Session(msg)) // detect busy→idle before swapping in
		m.sessions = []Session(msg)
		// Load history once now that we know the active IDs (to dedup), so the
		// Historical tab shows an accurate count from the start, not after a visit.
		m.sessionsReady = true
		if !m.histRequested {
			m.histRequested = true
			cmds = append(cmds, loadHistCmd(activeSessionIDs(m.sessions)))
		}
		if m.view == viewActive {
			m.rebuild()
		}
		if c := m.maybeFetchPRs(); c != nil {
			cmds = append(cmds, c)
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
	case histMsg:
		m.history = []HistEntry(msg)
		m.histReady = true
		if m.view == viewHistory {
			m.rebuild()
		}
		var scan tea.Cmd
		if !m.wtStarted {
			m.wtStarted = true
			scan = m.startWTScan()
		}
		return m, tea.Batch(m.maybeFetchPRs(), scan)
	case prStatusMsg:
		m.prBusy = false
		if m.prStatus == nil {
			m.prStatus = map[string]prInfo{}
		}
		for url, info := range msg {
			m.prStatus[url] = info
		}
		return m, savePRCacheCmd(m.prStatus)
	case wtScanMsg:
		m.worktreesScanned(msg)
	case wtRemovedMsg:
		m.worktreesRemoved(msg)
	case findMsg:
		if msg.seq != m.findSeq {
			return m, nil // superseded or cancelled
		}
		m.findBusy = false
		if msg.err != nil {
			m.flash = "find failed: " + msg.err.Error()
			return m, nil
		}
		m.findHits = msg.hits
		m.cursor, m.top = 0, 0
		m.rebuild()
		if len(msg.hits) == 0 {
			m.flash = "Claude found no matching session · esc for the full list"
		}
	case updateMsg:
		if v := string(msg); v != "" {
			m.latestVer = v
			// One-time nudge with the actual command; cleared by any keypress.
			m.flash = "update " + v + " available — " + upgradeHint()
		}
	case tea.KeyMsg:
		if m.picking {
			return m.handlePickKey(msg)
		}
		if m.wtPicking {
			return m.handleWorktreeKey(msg)
		}
		if m.finding {
			return m.handleFindKey(msg)
		}
		if m.searching {
			return m.handleSearchKey(msg)
		}
		if msg.String() != "x" {
			m.pendingKill = 0 // any other key cancels a pending kill
		}
		if msg.String() != "o" {
			m.pendingOpen = "" // …or a pending open of many PRs
		}
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.findBusy || m.findHits != nil { // first esc drops the finder
				m.clearFind()
				m.flash = ""
				return m, nil
			}
			if m.query != "" { // then an active filter
				m.query = ""
				m.cursor, m.top = 0, 0
				m.rebuild()
				m.flash = ""
				return m, nil
			}
			return m, tea.Quit
		case "/":
			m.searching = true
			m.pendingKill = 0
			m.flash = ""
		case "?":
			m.finding = true
			m.pendingKill = 0
			m.flash = ""
			if m.view != viewHistory {
				return m.switchView("2")
			}
		case "up", "k":
			m.move(-1)
		case "down", "j":
			m.move(1)
		case "tab", "1", "2", "left", "right", "h", "l":
			return m.switchView(msg.String())
		case "s":
			if m.sort == sortAlpha {
				m.sort = sortRecent
			} else {
				m.sort = sortAlpha
			}
			m.savePrefs()
			m.cursor = 0
			m.top = 0
			m.rebuild()
			m.flash = "sort: " + m.sortLabel()
		case "n":
			m.notify = !m.notify
			m.savePrefs()
			if m.notify {
				m.flash = "notifications on (Claude finishes / needs input)"
			} else {
				m.flash = "notifications off"
			}
		case "p":
			m.preview = !m.preview
			m.savePrefs()
			m.flash = "preview " + onOff(m.preview)
			if m.preview {
				m.flash += " · [ ] to resize"
			}
		case "[": // lean left: the divider moves left, the preview grows
			m.resizeRail(railStep)
		case "]":
			m.resizeRail(-railStep)
		case "$": // deliberately left out of the help line
			m.showCost = !m.showCost
			m.savePrefs()
			m.flash = "cost " + onOff(m.showCost)
		case "r":
			if m.view == viewActive {
				return m, loadActiveCmd()
			}
			return m, loadHistCmd(activeSessionIDs(m.sessions))
		case "enter":
			if m.activate() && popupMode {
				return m, tea.Quit
			}
		case "c":
			m.copyResume()
		case "o":
			m.openPRs()
		case "O":
			m.openPicker()
		case "W":
			return m, m.openWorktrees()
		case "x":
			m.killSelected()
		}
	}
	return m, nil
}

// handleSearchKey processes keys while the search query is being edited.
func (m model) handleSearchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEnter:
		m.searching = false // keep the filter, return to navigation
		return m, nil
	case tea.KeyEsc:
		m.searching = false // cancel: drop the filter entirely
		m.query = ""
		m.cursor, m.top = 0, 0
		m.rebuild()
		return m, nil
	case tea.KeyUp:
		m.move(-1)
		return m, nil
	case tea.KeyDown:
		m.move(1)
		return m, nil
	case tea.KeyBackspace, tea.KeyDelete:
		if r := []rune(m.query); len(r) > 0 {
			m.query = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.query = ""
	case tea.KeySpace:
		m.query += " "
	case tea.KeyRunes:
		m.query += string(msg.Runes)
	default:
		return m, nil
	}
	m.cursor, m.top = 0, 0
	m.rebuild()
	return m, nil
}

// handleFindKey processes keys while the finder description is being typed.
// Nothing is sent until enter.
func (m model) handleFindKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC:
		return m, tea.Quit
	case tea.KeyEnter:
		m.finding = false
		q := strings.TrimSpace(m.findQuery)
		if q == "" {
			return m, nil
		}
		m.findSeq++
		m.findBusy = true
		return m, findCmd(m.findSeq, q, append([]HistEntry(nil), m.history...))
	case tea.KeyEsc:
		m.finding = false
	case tea.KeyBackspace, tea.KeyDelete:
		if r := []rune(m.findQuery); len(r) > 0 {
			m.findQuery = string(r[:len(r)-1])
		}
	case tea.KeyCtrlU:
		m.findQuery = ""
	case tea.KeySpace:
		m.findQuery += " "
	case tea.KeyRunes:
		m.findQuery += string(msg.Runes)
	}
	return m, nil
}

// clearFind drops finder results and ignores any reply still in flight.
func (m *model) clearFind() {
	m.findSeq++
	m.findBusy = false
	m.findHits = nil
	m.cursor, m.top = 0, 0
	m.rebuild()
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

// activate focuses or resumes the selected row, and reports whether it took the
// user somewhere (so --popup can close).
func (m *model) activate() bool {
	if m.cursor >= len(m.rows) {
		return false
	}
	r := m.rows[m.cursor]
	switch r.kind {
	case rowSession:
		s := r.session
		switch {
		case s.SurfaceID != "":
			if err := activeDriver().Focus(s.SurfaceID); err != nil {
				m.flash = "couldn't focus tab: " + err.Error()
				return false
			}
			m.flash = "→ focused " + filepath.Base(s.CWD)
			return true
		case s.remote():
			// live container session with no matched host tab → open one exec'd in
			cwd, cmd := resumeInvocation(s.CWD, s.Container, s.Source, s.SessionID)
			if err := activeDriver().OpenResume(cwd, cmd); err != nil {
				_ = copyToClipboard(resumeClipboard(s.CWD, s.Container, s.Source, s.SessionID))
				m.flash = "couldn't open tab — command copied"
				return false
			}
			m.flash = "↻ opening " + filepath.Base(s.CWD) + " (" + s.Source + ")"
			return true
		default:
			m.flash = fmt.Sprintf("detached (pid %d) — no tab to focus; press x to kill", s.PID)
		}
	case rowHist:
		h := r.hist
		cwd, cmd := resumeInvocation(h.CWD, h.Container, h.Source, h.SessionID)
		if err := activeDriver().OpenResume(cwd, cmd); err != nil {
			_ = copyToClipboard(resumeClipboard(h.CWD, h.Container, h.Source, h.SessionID))
			m.flash = "couldn't open tab — resume command copied to clipboard"
			return false
		}
		m.flash = "↻ resuming " + filepath.Base(h.CWD) + " in a new tab"
		return true
	}
	return false
}

func (m *model) killSelected() {
	if m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	if r.kind != rowSession || r.session.SurfaceID != "" {
		m.flash = "x only kills detached (no-tab) sessions"
		return
	}
	pid := r.session.PID
	if m.pendingKill == pid {
		var err error
		if r.session.remote() {
			err = dockerKill(r.session.Container, pid) // kill inside the container
		} else {
			err = killProcess(pid)
		}
		if err != nil {
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
	switch r.kind {
	case rowHist:
		if copyToClipboard(resumeClipboard(r.hist.CWD, r.hist.Container, r.hist.Source, r.hist.SessionID)) == nil {
			m.flash = "copied resume command"
		}
	case rowSession:
		if r.session.remote() {
			if copyToClipboard(resumeClipboard(r.session.CWD, r.session.Container, r.session.Source, r.session.SessionID)) == nil {
				m.flash = "copied resume command"
			}
		}
	}
}
