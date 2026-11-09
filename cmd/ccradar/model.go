package main

import (
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

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

// details is the row's session details (zero for headers and dividers).
func (r row) details() sessionDetails {
	switch r.kind {
	case rowSession:
		return r.session.sessionDetails
	case rowHist:
		return r.hist.sessionDetails
	}
	return sessionDetails{}
}

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
	prStatus   map[string]prInfo // PR URL → live status from gh
	prBusy     bool              // a PR status lookup is in flight
	anyPR      bool              // some row in the current list has a PR (shows the PR column)
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
