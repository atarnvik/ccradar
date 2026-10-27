package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
	styModel    = lipgloss.NewStyle().Foreground(lipgloss.Color("103"))
	stySearch   = lipgloss.NewStyle().Foreground(lipgloss.Color("227")).Bold(true)
	styUpdate   = lipgloss.NewStyle().Foreground(lipgloss.Color("42")).Bold(true)
	styPrevTtl  = lipgloss.NewStyle().Foreground(lipgloss.Color("252")).Bold(true)
	styPrevLbl  = lipgloss.NewStyle().Foreground(lipgloss.Color("44")).Bold(true)
	styPrevBody = lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
	stySection  = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
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

type sortMode int

const (
	sortRecent sortMode = iota // groups & rows by last-active, newest first (default)
	sortAlpha                  // directory A→Z, then title A→Z
)

type rowKind int

const (
	rowSection rowKind = iota // a source section header (Local / a container)
	rowHeader                 // a directory header within a section
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
	view       viewKind
	sort       sortMode
	searching  bool   // editing the search query
	query      string // active fuzzy filter ("" = no filter)
	finding    bool   // editing the finder (?) description
	findQuery  string // description sent to Claude
	findSeq    int    // bumped per ask (and on cancel) so stale replies are dropped
	findBusy   bool   // waiting on Claude
	findHits   map[string]findHit // session id → rank/reason; nil = no finder filter
	notify     bool   // send a notification on busy→idle
	preview    bool   // show the right-hand preview pane
	latestVer  string // newer available version ("" = up to date / unknown)
	histLoaded bool   // history fetched at least once (for an accurate count upfront)
	prevStatus map[string]string // sessionID → last seen status (transition tracking)
	sessions   []Session
	history    []HistEntry
	rows       []row
	cursor     int
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

func (m model) Init() tea.Cmd { return tea.Batch(loadActiveCmd(), tickCmd(), checkUpdateCmd()) }

type updateMsg string

// checkUpdateCmd asks the module proxy (once, on startup) whether a newer
// version exists. Silent on any failure.
func checkUpdateCmd() tea.Cmd {
	return func() tea.Msg { return updateMsg(latestIfNewer()) }
}

// observe diffs the incoming sessions against the last seen statuses and returns
// notification commands when a session leaves "busy" — either finishing (idle)
// or pausing for your input (waiting). prevStatus is always refreshed so
// toggling notify on never misfires on history.
func (m *model) observe(next []Session) []tea.Cmd {
	var cmds []tea.Cmd
	cur := make(map[string]string, len(next))
	for _, s := range next {
		cur[s.SessionID] = s.Status
		if m.notify {
			prev, ok := m.prevStatus[s.SessionID]
			if ok && prev == "busy" && (s.Status == "idle" || s.Status == "waiting") {
				cmds = append(cmds, notifyCmd(notifTitle(s), notifBody(s), notifyExec(s.SessionID)))
			}
		}
	}
	m.prevStatus = cur
	return cmds
}

func notifyCmd(title, body, execCmd string) tea.Cmd {
	return func() tea.Msg {
		sendNotification(title, body, execCmd)
		return nil
	}
}

// notifyExec builds the shell command a notification runs when clicked: re-launch
// this binary headless to focus the session's tab. The terminal is pinned via
// CCRADAR_TERM because the click runs with no $TERM_PROGRAM, and the absolute
// path is used because the click's PATH won't include ~/go/bin or brew.
func notifyExec(sessionID string) string {
	exe, err := os.Executable()
	if err != nil || exe == "" || sessionID == "" {
		return ""
	}
	return fmt.Sprintf("CCRADAR_TERM=%s %s focus %s",
		shellQuote(activeDriver().Name()), shellQuote(exe), shellQuote(sessionID))
}

// focusSessionID re-resolves a session by id and focuses its terminal surface.
func focusSessionID(id string) {
	for _, s := range loadSessions() {
		if s.SessionID == id && s.SurfaceID != "" {
			_ = activeDriver().Focus(s.SurfaceID)
			return
		}
	}
}

// notifTitle / notifBody avoid repeating the directory: when a session has an
// AI title, that headlines the notification and the body carries the status +
// directory; otherwise the status headlines and the directory is the body.
// The wording reflects whether Claude finished (idle) or needs input (waiting).
func notifTitle(s Session) string {
	if s.Title != "" {
		return s.Title
	}
	if s.Status == "waiting" {
		return "⏸ Claude needs your input"
	}
	return "✓ Claude finished responding"
}

func notifBody(s Session) string {
	dir := filepath.Base(s.CWD)
	if s.Title == "" {
		return dir
	}
	if s.Status == "waiting" {
		return "⏸ needs input · " + dir
	}
	return "✓ finished · " + dir
}

func dirDisplay(cwd string) string {
	home := homeDir()
	if home != "" && strings.HasPrefix(cwd, home) {
		return "~" + cwd[len(home):]
	}
	return cwd
}

func (m *model) rebuild() {
	var rows []row

	// dirGroup appends directory headers + rows for one contiguous group.
	dirGroupSessions := func(ss []Session) {
		prev := ""
		for _, s := range ss {
			if s.CWD != prev {
				prev = s.CWD
				rows = append(rows, row{kind: rowHeader, header: dirDisplay(s.CWD)})
			}
			rows = append(rows, row{kind: rowSession, session: s})
		}
	}

	if m.view == viewActive {
		m.sortSessions(m.sessions)
		order := sessionSources(m.sessions)
		sectioned := len(order) > 1
		for _, src := range order {
			var open, detached []Session
			for _, s := range m.sessions {
				if s.Source != src || !fuzzyMatch(m.query, sessionHay(s)) {
					continue
				}
				if s.SurfaceID != "" {
					open = append(open, s)
				} else {
					detached = append(detached, s)
				}
			}
			if len(open) == 0 && len(detached) == 0 {
				continue
			}
			if sectioned {
				rows = append(rows, row{kind: rowSection, header: sectionLabel(src)})
			}
			dirGroupSessions(open)
			if len(detached) > 0 {
				rows = append(rows, row{kind: rowDivider, header: "detached · no matching tab"})
				dirGroupSessions(detached)
			}
		}
	} else if m.findHits != nil {
		// Finder results: best match first, with a directory header whenever the
		// directory changes (rank order beats grouping here).
		var hits []HistEntry
		for _, h := range m.history {
			if _, ok := m.findHits[h.SessionID]; ok && fuzzyMatch(m.query, histHay(h)) {
				hits = append(hits, h)
			}
		}
		sort.SliceStable(hits, func(i, j int) bool {
			return m.findHits[hits[i].SessionID].rank < m.findHits[hits[j].SessionID].rank
		})
		prev := ""
		for _, h := range hits {
			hdr := dirDisplay(h.CWD)
			if h.Source != "" {
				hdr = sectionLabel(h.Source) + "  " + hdr
			}
			if hdr != prev {
				prev = hdr
				rows = append(rows, row{kind: rowHeader, header: hdr})
			}
			rows = append(rows, row{kind: rowHist, hist: h})
		}
	} else {
		m.sortHistory(m.history)
		order := histSources(m.history)
		sectioned := len(order) > 1
		for _, src := range order {
			prev := ""
			var any bool
			for _, h := range m.history {
				if h.Source != src || !fuzzyMatch(m.query, histHay(h)) {
					continue
				}
				if !any && sectioned {
					rows = append(rows, row{kind: rowSection, header: sectionLabel(src)})
				}
				any = true
				if h.CWD != prev {
					prev = h.CWD
					rows = append(rows, row{kind: rowHeader, header: dirDisplay(h.CWD)})
				}
				rows = append(rows, row{kind: rowHist, hist: h})
			}
		}
	}
	m.rows = rows
	m.clampCursor()
}

// sectionLabel names a source section.
func sectionLabel(src string) string {
	if src == "" {
		return "local"
	}
	return "⬢ " + src
}

// sessionSources / histSources list the distinct sources present, local ("")
// first, then containers in first-seen order.
func sessionSources(ss []Session) []string {
	order := []string{""}
	seen := map[string]bool{"": true}
	for _, s := range ss {
		if !seen[s.Source] {
			seen[s.Source] = true
			order = append(order, s.Source)
		}
	}
	return order
}

func histSources(hs []HistEntry) []string {
	order := []string{""}
	seen := map[string]bool{"": true}
	for _, h := range hs {
		if !seen[h.Source] {
			seen[h.Source] = true
			order = append(order, h.Source)
		}
	}
	return order
}

// sortSessions orders sessions per the current sort mode, keeping each
// directory's sessions contiguous so the grouped headers stay intact.
func (m model) sortSessions(ss []Session) {
	if m.sort == sortAlpha {
		sort.SliceStable(ss, func(i, j int) bool {
			if ss[i].CWD != ss[j].CWD {
				return ss[i].CWD < ss[j].CWD
			}
			ti, tj := strings.ToLower(titleOr(ss[i].Title)), strings.ToLower(titleOr(ss[j].Title))
			if ti != tj {
				return ti < tj
			}
			return ss[i].UpdatedAt > ss[j].UpdatedAt
		})
		return
	}
	latest := map[string]int64{}
	for _, s := range ss {
		if s.UpdatedAt > latest[s.CWD] {
			latest[s.CWD] = s.UpdatedAt
		}
	}
	sort.SliceStable(ss, func(i, j int) bool {
		if li, lj := latest[ss[i].CWD], latest[ss[j].CWD]; li != lj {
			return li > lj
		}
		if ss[i].CWD != ss[j].CWD {
			return ss[i].CWD < ss[j].CWD
		}
		return ss[i].UpdatedAt > ss[j].UpdatedAt
	})
}

// sortHistory mirrors sortSessions for past sessions (activity = ModAt).
func (m model) sortHistory(hs []HistEntry) {
	if m.sort == sortAlpha {
		sort.SliceStable(hs, func(i, j int) bool {
			if hs[i].CWD != hs[j].CWD {
				return hs[i].CWD < hs[j].CWD
			}
			ti, tj := strings.ToLower(titleOr(hs[i].Title)), strings.ToLower(titleOr(hs[j].Title))
			if ti != tj {
				return ti < tj
			}
			return hs[i].ModAt.After(hs[j].ModAt)
		})
		return
	}
	latest := map[string]time.Time{}
	for _, h := range hs {
		if h.ModAt.After(latest[h.CWD]) {
			latest[h.CWD] = h.ModAt
		}
	}
	sort.SliceStable(hs, func(i, j int) bool {
		if li, lj := latest[hs[i].CWD], latest[hs[j].CWD]; !li.Equal(lj) {
			return li.After(lj)
		}
		if hs[i].CWD != hs[j].CWD {
			return hs[i].CWD < hs[j].CWD
		}
		return hs[i].ModAt.After(hs[j].ModAt)
	})
}

func (m model) sortLabel() string {
	if m.sort == sortRecent {
		return "recent"
	}
	return "alpha"
}

// parseSort is the inverse of sortLabel; anything but "alpha" means recent.
func parseSort(label string) sortMode {
	if label == "alpha" {
		return sortAlpha
	}
	return sortRecent
}

// savePrefs persists the toggleable preferences (notify, preview, sort).
func (m model) savePrefs() {
	saveConfig(config{Notify: m.notify, Preview: m.preview, Sort: m.sortLabel()})
}

func onOff(b bool) string {
	if b {
		return "on"
	}
	return "off"
}

// sessionHay / histHay are the searchable text for a row: directory + title +
// model, so a query can match the folder, the session name, or the model.
func sessionHay(s Session) string {
	return dirDisplay(s.CWD) + " " + titleOr(s.Title) + " " + modelShort(s.Model)
}
func histHay(h HistEntry) string {
	return dirDisplay(h.CWD) + " " + titleOr(h.Title) + " " + modelShort(h.Model)
}

// fuzzyMatch reports whether every whitespace-separated token of query is a
// case-insensitive subsequence of target (tokens AND together). Empty query
// always matches.
func fuzzyMatch(query, target string) bool {
	t := strings.ToLower(target)
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		if !subsequence(tok, t) {
			return false
		}
	}
	return true
}

// subsequence reports whether all runes of q appear in t in order.
func subsequence(q, t string) bool {
	qr := []rune(q)
	if len(qr) == 0 {
		return true
	}
	qi := 0
	for _, tr := range t {
		if tr == qr[qi] {
			if qi++; qi == len(qr) {
				return true
			}
		}
	}
	return false
}

// matchCount is the number of selectable (session/hist) rows currently shown.
func (m model) matchCount() int {
	n := 0
	for _, r := range m.rows {
		if selectable(r.kind) {
			n++
		}
	}
	return n
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
	// Don't strand only-header rows above the window: if nothing selectable is
	// hidden above the cursor, pull the top up so those headers reappear (this
	// is what lets you scroll fully back to the top).
	if m.top > 0 && m.cursor < v {
		selectableAbove := false
		for i := 0; i < m.top; i++ {
			if selectable(m.rows[i].kind) {
				selectableAbove = true
				break
			}
		}
		if !selectableAbove {
			m.top = 0
		}
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
		cmds := m.observe([]Session(msg)) // detect busy→idle before swapping in
		m.sessions = []Session(msg)
		// Load history once now that we know the active IDs (to dedup), so the
		// Historical tab shows an accurate count from the start, not after a visit.
		if !m.histLoaded {
			m.histLoaded = true
			cmds = append(cmds, loadHistCmd(activeSessionIDs(m.sessions)))
		}
		if m.view == viewActive {
			m.rebuild()
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
	case histMsg:
		m.history = []HistEntry(msg)
		if m.view == viewHistory {
			m.rebuild()
		}
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
		if m.finding {
			return m.handleFindKey(msg)
		}
		if m.searching {
			return m.handleSearchKey(msg)
		}
		if msg.String() != "x" {
			m.pendingKill = 0 // any other key cancels a pending kill
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

func (m *model) activate() {
	if m.cursor >= len(m.rows) {
		return
	}
	r := m.rows[m.cursor]
	switch r.kind {
	case rowSession:
		s := r.session
		switch {
		case s.SurfaceID != "":
			_ = activeDriver().Focus(s.SurfaceID)
			m.flash = "→ focused " + filepath.Base(s.CWD)
		case s.remote():
			// live container session with no matched host tab → open one exec'd in
			cwd, cmd := resumeInvocation(s.CWD, s.Container, s.Source, s.SessionID)
			if err := activeDriver().OpenResume(cwd, cmd); err != nil {
				_ = copyToClipboard(resumeClipboard(s.CWD, s.Container, s.Source, s.SessionID))
				m.flash = "couldn't open tab — command copied"
			} else {
				m.flash = "↻ opening " + filepath.Base(s.CWD) + " (" + s.Source + ")"
			}
		default:
			m.flash = fmt.Sprintf("detached (pid %d) — no tab to focus; press x to kill", s.PID)
		}
	case rowHist:
		h := r.hist
		cwd, cmd := resumeInvocation(h.CWD, h.Container, h.Source, h.SessionID)
		if err := activeDriver().OpenResume(cwd, cmd); err != nil {
			_ = copyToClipboard(resumeClipboard(h.CWD, h.Container, h.Source, h.SessionID))
			m.flash = "couldn't open tab — resume command copied to clipboard"
		} else {
			m.flash = "↻ resuming " + filepath.Base(h.CWD) + " in a new tab"
		}
	}
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
		if s.SurfaceID != "" {
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
	b.WriteString(m.tabBar())
	b.WriteString("  " + styDim.Render("via "+activeDriver().Name()))
	if dirFilter != "" {
		b.WriteString(styDim.Render("  ⌂ " + dirDisplay(dirFilter)))
	}
	if m.latestVer != "" {
		// Keep this short so the top line doesn't wrap (which would overflow the
		// frame). The full install command is shown once via the flash line.
		b.WriteString("  " + styUpdate.Render("⬆ "+m.latestVer))
	}
	b.WriteString("\n")
	// Second line is the search or finder bar (when active) or a blank spacer,
	// so the fixed line count stays the same.
	switch {
	case m.searching:
		b.WriteString(m.searchLine() + "\n")
	case m.view == viewHistory && (m.finding || m.findBusy || m.findHits != nil):
		b.WriteString(m.findLine() + "\n")
	case m.query != "":
		b.WriteString(m.searchLine() + "\n")
	default:
		b.WriteString("\n")
	}

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
		msg := "no live sessions"
		if m.view != viewActive {
			msg = "no past sessions"
		}
		if m.query != "" {
			msg = "no matches for “" + m.query + "”"
		}
		if m.view == viewHistory && m.findHits != nil {
			msg = "no sessions matched “" + m.findQuery + "”"
		}
		b.WriteString(styDim.Render("  "+msg) + "\n")
	}

	v := m.visibleRows()
	end := m.top + v
	if end > len(m.rows) {
		end = len(m.rows)
	}

	// Right preview rail when enabled and the terminal is wide enough.
	showRail := m.showRail()
	cw := m.width
	railW := 0
	if showRail {
		railW = 38
		cw = m.width - railW - 3 // " │ " separator = 3 cells
	}

	lines := make([]string, 0, v)
	for i := m.top; i < end; i++ {
		lines = append(lines, m.renderListLine(i, cw))
	}

	if showRail {
		blank := strings.Repeat(" ", cw)
		for len(lines) < v { // pad so the rail reads as a full-height panel
			lines = append(lines, blank)
		}
		right := m.previewLines(railW, v)
		for i := 0; i < v; i++ {
			b.WriteString(lines[i] + " " + styDim.Render("│") + " " + right[i] + "\n")
		}
	} else {
		for _, ln := range lines {
			b.WriteString(ln + "\n")
		}
	}

	if end < len(m.rows) {
		b.WriteString(styHelp.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)) + "\n")
	} else {
		b.WriteString("\n")
	}
	if m.flash != "" {
		b.WriteString(styStatus.Render(trunc("  "+m.flash, m.width)) + "\n")
	}
	var help string
	if m.finding {
		help = "  describe the session you're looking for · enter ask Claude · esc cancel"
	} else if m.searching {
		help = "  type to filter · enter accept · esc clear · ↑/↓ move"
	} else {
		help = "  ↑/↓ move · tab switch view · enter "
		if m.view == viewActive {
			help += "focus tab · x kill detached"
		} else {
			help += "resume (new tab) · c copy cmd"
		}
		help += " · / search · ? find · s sort:" + m.sortLabel() + " · p preview:" + onOff(m.preview) + " · n notify:" + onOff(m.notify) + " · r refresh · q quit"
	}
	b.WriteString(styHelp.Render(trunc(help, m.width)))
	// No trailing newline: a final "\n" would make Bubble Tea count an extra
	// (empty) line, pushing the frame one row past the terminal height.
	return b.String()
}

// trunc cuts s to at most n display runes (n<=0 means no limit), so a long
// single-styled line can't wrap onto a second screen row.
func trunc(s string, n int) string {
	if n <= 0 {
		return s
	}
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// padVis pads a styled string (whose visible width is vis) with spaces to cw.
func padVis(s string, vis, cw int) string {
	if vis < cw {
		return s + strings.Repeat(" ", cw-vis)
	}
	return s
}

// renderListLine renders row i as a styled line of exactly cw visible cells.
func (m model) renderListLine(i, cw int) string {
	r := m.rows[i]
	switch r.kind {
	case rowSection:
		s := "━━ " + r.header + " "
		if pad := cw - len([]rune(s)); pad > 0 {
			s += strings.Repeat("━", pad)
		}
		return stySection.Render(truncPad(s, cw))
	case rowHeader:
		return styHeader.Render(truncPad(r.header, cw))
	case rowDivider:
		return styHelp.Render(truncPad("  ── "+r.header+" ──", cw))
	}
	plain, colored := m.rowColumns(r, cw)
	if i == m.cursor {
		return "  " + styCursor.Render(truncPad("▸ "+plain, cw-2))
	}
	return padVis("    "+colored, 4+len([]rune(plain)), cw)
}

// rowColumns builds the aligned columns for a session/history row. The title
// column flexes so the plain text is exactly cw-4 cells wide (the 4 is the row's
// left indent), keeping the highlighted and normal forms identical.
func (m model) rowColumns(r row, cw int) (plain, colored string) {
	switch r.kind {
	case rowSession:
		s := r.session
		sp, sst := statusText(s.Status)
		age := fmt.Sprintf("%4s", fmtAge(nowMs()-s.UpdatedAt))
		tw := cw - 40
		if tw < 8 {
			tw = 8
		}
		titleF := truncPad(titleOr(s.Title), tw)
		mdl := truncPad(modelShort(s.Model), 10)
		if s.SurfaceID != "" {
			loc := truncPad("→ tab", 8)
			plain = fmt.Sprintf("%s %s  %s  %s  %s", sp, age, titleF, mdl, loc)
			colored = fmt.Sprintf("%s %s  %s  %s  %s",
				sst.Render(sp), styDim.Render(age), styTitle.Render(titleF), styModel.Render(mdl), styTab.Render(loc))
		} else {
			loc := truncPad(fmt.Sprintf("pid %d", s.PID), 8)
			plain = fmt.Sprintf("%s %s  %s  %s  %s", sp, age, titleF, mdl, loc)
			colored = styDim.Render(plain) // detached: muted whole row
		}
	case rowHist:
		h := r.hist
		age := fmt.Sprintf("%4s", fmtAge(time.Since(h.ModAt).Milliseconds()))
		tw := cw - 32
		if tw < 8 {
			tw = 8
		}
		titleF := truncPad(titleOr(h.Title), tw)
		mdl := truncPad(modelShort(h.Model), 10)
		resume := truncPad("↻ resume", 8)
		plain = fmt.Sprintf("%s  %s  %s  %s", age, titleF, mdl, resume)
		colored = fmt.Sprintf("%s  %s  %s  %s",
			styDim.Render(age), styTitle.Render(titleF), styModel.Render(mdl), styTab.Render(resume))
	}
	return plain, colored
}

// previewLines renders the selected session's detail panel: exactly h lines,
// each w cells wide (title, dir/branch/model/status/age, last prompt, last reply).
func (m model) previewLines(w, h int) []string {
	type seg struct {
		text  string
		style lipgloss.Style
	}
	var segs []seg
	push := func(text string, style lipgloss.Style) {
		for _, ln := range wrapText(text, w) {
			segs = append(segs, seg{ln, style})
		}
	}

	if m.cursor < len(m.rows) {
		var title, source, dir, branch, model, status, age, prompt, reply string
		switch r := m.rows[m.cursor]; r.kind {
		case rowSession:
			s := r.session
			title, source, dir, branch = titleOr(s.Title), s.Source, dirDisplay(s.CWD), s.Branch
			model, status, age = modelShort(s.Model), s.Status, fmtAge(nowMs()-s.UpdatedAt)
			prompt, reply = s.LastPrompt, s.LastReply
		case rowHist:
			hh := r.hist
			title, dir, branch = titleOr(hh.Title), dirDisplay(hh.CWD), hh.Branch
			model, age = modelShort(hh.Model), fmtAge(time.Since(hh.ModAt).Milliseconds())
			prompt, reply = hh.LastPrompt, hh.LastReply
		}
		push(title, styPrevTtl)
		if source != "" {
			push("⬢ "+source, styPrevLbl)
		}
		meta := dir
		if branch != "" {
			meta += " · " + branch
		}
		push(meta, styDim)
		line2 := model
		if status != "" {
			line2 += " · " + status
		}
		if age != "" {
			line2 += " · " + age
		}
		push(line2, styDim)
		if why := m.selectedWhy(); why != "" {
			segs = append(segs, seg{"", styDim})
			push("▸ Match", styPrevLbl)
			push(why, styPrevBody)
		}
		if prompt != "" {
			segs = append(segs, seg{"", styDim})
			push("▸ You", styPrevLbl)
			push(prompt, styPrevBody)
		}
		if reply != "" {
			segs = append(segs, seg{"", styDim})
			push("▸ Claude", styPrevLbl)
			push(reply, styPrevBody)
		}
	}

	out := make([]string, h)
	for i := 0; i < h; i++ {
		if i < len(segs) {
			out[i] = padVis(segs[i].style.Render(segs[i].text), len([]rune(segs[i].text)), w)
		} else {
			out[i] = strings.Repeat(" ", w)
		}
	}
	return out
}

// wrapText greedily wraps s to width w (hard-breaking over-long words).
func wrapText(s string, w int) []string {
	if w < 1 {
		w = 1
	}
	var out []string
	line := ""
	flush := func() {
		out = append(out, line)
		line = ""
	}
	for _, word := range strings.Fields(s) {
		for len([]rune(word)) > w { // break a word longer than the column
			if line != "" {
				flush()
			}
			r := []rune(word)
			out = append(out, string(r[:w]))
			word = string(r[w:])
		}
		switch {
		case line == "":
			line = word
		case len([]rune(line))+1+len([]rune(word)) <= w:
			line += " " + word
		default:
			flush()
			line = word
		}
	}
	if line != "" {
		flush()
	}
	return out
}

// searchLine renders the filter bar: the query (with a cursor while editing)
// and how many rows currently match.
func (m model) searchLine() string {
	cursor := ""
	if m.searching {
		cursor = "▌"
	}
	n := m.matchCount()
	unit := "matches"
	if n == 1 {
		unit = "match"
	}
	return stySearch.Render("  /"+m.query+cursor) +
		styHelp.Render(fmt.Sprintf("   %d %s", n, unit))
}

// showRail reports whether the preview pane fits and is enabled.
func (m model) showRail() bool { return m.preview && len(m.rows) > 0 && m.width >= 100 }

// selectedWhy is the finder's reason for the selected row ("" outside results).
func (m model) selectedWhy() string {
	if m.findHits == nil || m.cursor >= len(m.rows) || m.rows[m.cursor].kind != rowHist {
		return ""
	}
	return m.findHits[m.rows[m.cursor].hist.SessionID].why
}

// findLine renders the finder bar: the description, then progress, or the
// match count plus (when the preview pane is hidden) the selected row's reason.
func (m model) findLine() string {
	if m.finding {
		return stySearch.Render("  ? " + m.findQuery + "▌")
	}
	// Once sent, shorten a long description so the status and reason fit.
	q := "  ? " + m.findQuery
	if limit := m.width / 3; m.width > 0 && len([]rune(m.findQuery)) > limit {
		q = "  ? " + trunc(m.findQuery, limit-1) + "…"
	}
	if m.findBusy {
		return stySearch.Render(q) + styHelp.Render("   asking Claude ("+findModel()+")…")
	}
	n := m.matchCount()
	unit := "matches"
	if n == 1 {
		unit = "match"
	}
	info := fmt.Sprintf("   %d %s · esc for all", n, unit)
	if why := m.selectedWhy(); why != "" && !m.showRail() {
		info += " · " + why
	}
	if m.width > 0 {
		info = trunc(info, m.width-len([]rune(q)))
	}
	return stySearch.Render(q) + styHelp.Render(info)
}

// ---- entry ----

func main() {
	// Headless: focus a session's tab by id (used by notification click-through).
	if len(os.Args) > 2 && os.Args[1] == "focus" {
		focusSessionID(os.Args[2])
		return
	}
	var mode, path string
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch a := args[i]; a {
		case "dump", "render":
			mode = a
		case "-h", "--help", "help":
			printUsage()
			return
		case "-v", "--version", "version":
			fmt.Println(displayVersion())
			return
		case "--docker":
			if i+1 < len(args) {
				extraSources = append(extraSources, parseDockerSource(args[i+1]))
				i++
			}
		default:
			if !strings.HasPrefix(a, "-") {
				path = a // a directory to scope to
			}
		}
	}
	if path != "" {
		setDirFilter(path)
	}
	if mode != "" {
		debugMode(mode)
		return
	}
	cfg := loadConfig()
	if _, err := tea.NewProgram(model{notify: cfg.Notify, preview: cfg.Preview, sort: parseSort(cfg.Sort)}, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Println(`ccradar — Claude Code session dashboard for Ghostty

Usage:
  ccradar [dir]         scope to a directory and its subdirectories (e.g. ccradar ~/src/app)
  ccradar --docker <c>  also read sessions from a container (name or compose service;
                        repeatable; --docker <c>=/path to override ~/.claude)
  ccradar               show all local sessions

Terminal: auto-detected from $TERM_PROGRAM (Ghostty, iTerm2, Terminal.app).
Override with CCRADAR_TERM=ghostty|iterm|terminal.

Keys: ↑/↓ move · tab switch view · / search · s sort · n notify · enter focus/resume · q quit`)
}

func debugMode(which string) {
	sessions := loadSessions()
	if which == "dump" {
		fmt.Println("# active")
		for _, s := range sessions {
			tab := s.SurfaceID
			if tab == "" {
				tab = "[no tab]"
			}
			fmt.Printf("%-7s %4s  %-22s %-10s %-46s %s\n",
				s.Status, fmtAge(nowMs()-s.UpdatedAt), filepath.Base(s.CWD), modelShort(s.Model), titleOr(s.Title), tab)
		}
		fmt.Println("# history")
		for _, h := range loadHistory(activeSessionIDs(sessions)) {
			fmt.Printf("%4s  %-22s %-10s %-46s %s\n",
				fmtAge(time.Since(h.ModAt).Milliseconds()), filepath.Base(h.CWD), modelShort(h.Model), titleOr(h.Title), h.SessionID)
		}
		return
	}
	// render: one frame of each view
	m := model{sessions: sessions, history: loadHistory(activeSessionIDs(sessions)), preview: true, width: 120, height: 24}
	m.rebuild()
	fmt.Println("=== ACTIVE ===")
	fmt.Print(m.View())
	m.view = viewHistory
	m.cursor = 0
	m.rebuild()
	fmt.Println("\n=== HISTORICAL ===")
	fmt.Print(m.View())
}
