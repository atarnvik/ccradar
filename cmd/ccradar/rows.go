package main

import (
	"sort"
	"strings"
	"time"
)

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
			ti, tj := strings.ToLower(displayTitle(ss[i].sessionDetails, ss[i].Title)), strings.ToLower(displayTitle(ss[j].sessionDetails, ss[j].Title))
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
			ti, tj := strings.ToLower(displayTitle(hs[i].sessionDetails, hs[i].Title)), strings.ToLower(displayTitle(hs[j].sessionDetails, hs[j].Title))
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
