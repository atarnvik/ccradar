package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// statusWidth is the status column: a glyph, then a word for anything but idle.
const statusWidth = 6

// statusText returns the fixed-width status label and its color style. Idle is
// the common case, so it's just a dim glyph; waiting (needs you) stands out.
func statusText(status string) (string, lipgloss.Style) {
	switch status {
	case "busy":
		return "● busy", styWorking
	case "idle":
		return "○     ", styIdle
	case "waiting":
		return "◆ wait", styWait
	default:
		w := status
		if len(w) > 4 {
			w = w[:4]
		}
		return fmt.Sprintf("· %-4s", w), styDim
	}
}

func titleOr(t string) string {
	if t == "" {
		return "(no title)"
	}
	return t
}

// truncPad truncates or space-pads s to exactly n display cells (rune-based).
// A negative n (a terminal too narrow for the layout) is treated as 0.
func truncPad(s string, n int) string {
	n = max(n, 0)
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
	switch {
	case !m.sessionsReady:
		a = " Active (…) "
	case detached > 0:
		a = fmt.Sprintf(" Active (%d, +%d detached) ", open, detached)
	}
	h := fmt.Sprintf(" Historical (%d) ", len(m.history))
	if !m.histReady {
		h = " Historical (…) "
	}
	if m.view == viewActive {
		return styTabOn.Render(a) + " " + styTabOff.Render(h)
	}
	return styTabOff.Render(a) + " " + styTabOn.Render(h)
}

// waitingCount is how many live sessions are waiting on you.
func (m model) waitingCount() int {
	n := 0
	for _, s := range m.sessions {
		if s.Status == "waiting" {
			n++
		}
	}
	return n
}

func (m model) View() string {
	var b strings.Builder
	b.WriteString(m.tabBar())
	if n := m.waitingCount(); n > 0 {
		b.WriteString("  " + styWait.Render(fmt.Sprintf("◆ %d waiting", n)))
	}
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
	if m.top > 0 && !m.picking {
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
		switch {
		case m.view == viewActive && !m.sessionsReady:
			msg = "loading live sessions…"
		case m.view != viewActive && !m.histReady:
			msg = "loading past sessions…"
		case m.view != viewActive:
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
		railW = m.railWidth()
		cw = m.width - railW - railGap
	}

	lines := make([]string, 0, v)
	if m.picking {
		lines = m.pickerLines(cw, v)
	} else {
		for i := m.top; i < end; i++ {
			lines = append(lines, m.renderListLine(i, cw))
		}
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

	if end < len(m.rows) && !m.picking {
		b.WriteString(styHelp.Render(fmt.Sprintf("  ↓ %d more", len(m.rows)-end)) + "\n")
	} else {
		b.WriteString("\n")
	}
	if m.flash != "" {
		b.WriteString(styStatus.Render(trunc("  "+m.flash, m.width)) + "\n")
	}
	var help string
	if m.picking {
		help = fmt.Sprintf("  ↑/↓ move · enter open · a open all %d · esc close", len(m.pickPRs))
	} else if m.finding {
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
		switch n := len(m.selectedDetails().PRs); {
		case n == 1:
			help += " · o open PR"
		case n > 1:
			help += " · o open PRs · O pick"
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
		return styHeader.Render(truncPad(truncEllipsis(r.header, cw), cw))
	case rowDivider:
		return styHelp.Render(truncPad("  ── "+r.header+" ──", cw))
	case rowBlank:
		return strings.Repeat(" ", max(cw, 0))
	}
	plain, colored := m.rowColumns(r, cw)
	if i == m.cursor {
		return "  " + styCursor.Render(truncPad("▸ "+plain, cw-2))
	}
	return padVis("    "+colored, 4+len([]rune(plain)), cw)
}

// costColumnMinWidth / prColumnMinWidth are the narrowest lists that still get
// a cost / PR / context column.
const (
	costColumnMinWidth = 70
	prColumnMinWidth   = 70
	ctxColumnMinWidth  = 70
)

// rowColumns builds the aligned columns for a session/history row: status
// and age (live rows) or age (past rows), the title, then the optional columns
// — cost, PR, background shells, context size (live rows) — and the model.
// The title column flexes so the row fills cw-4 cells (the 4 is the row's
// left indent), keeping the optional columns aligned at the right edge.
func (m model) rowColumns(r row, cw int) (plain, colored string) {
	var lead, leadC string // status/age
	var d sessionDetails
	var title, mdl string
	var tail, tailC []string
	switch r.kind {
	case rowSession:
		s := r.session
		sp, sst := statusText(s.Status)
		age := fmt.Sprintf("%4s", fmtAge(nowMs()-s.UpdatedAt))
		lead, leadC = sp+" "+age, sst.Render(sp)+" "+styDim.Render(age)
		d, title, mdl = s.sessionDetails, s.Title, s.Model
	case rowHist:
		h := r.hist
		age := fmt.Sprintf("%4s", fmtAge(time.Since(h.ModAt).Milliseconds()))
		lead, leadC = age, styDim.Render(age)
		d, title, mdl = h.sessionDetails, h.Title, h.Model
		if m.showCost && cw >= costColumnMinWidth {
			cost := ""
			if h.Cost.known() {
				cost = fmtCost(h.Cost.USD)
			}
			cost = fmt.Sprintf("%5s", cost)
			tail, tailC = append(tail, cost), append(tailC, styDim.Render(cost))
		}
	}
	if m.anyPR && cw >= prColumnMinWidth {
		p, pc := prBadge(d, m.prStatus)
		tail, tailC = append(tail, p), append(tailC, pc)
	}
	if m.anyShell && r.kind == rowSession {
		sh := strings.Repeat(" ", shellColumnWidth)
		if n := len(r.session.Shells); n > 0 {
			sh = truncPad(fmt.Sprintf("⚙%d", n), shellColumnWidth)
		}
		tail, tailC = append(tail, sh), append(tailC, styWorking.Render(sh))
	}
	if m.anyCtx && r.kind == rowSession && cw >= ctxColumnMinWidth {
		ctx := ""
		if n := r.session.Context; n > 0 {
			ctx = fmtTokens(n)
		}
		ctx = fmt.Sprintf("%5s", ctx)
		tail, tailC = append(tail, ctx), append(tailC, styDim.Render(ctx))
	}
	ms := truncPad(modelShort(mdl), 10)
	tail, tailC = append(tail, ms), append(tailC, styModel.Render(ms))
	detached := r.kind == rowSession && r.session.SurfaceID == ""

	fixed := len([]rune(lead)) + 2
	for _, t := range tail {
		fixed += len([]rune(t)) + 2
	}
	tw := max(cw-4-fixed, 8)
	titleF := truncPad(truncEllipsis(displayTitle(d, title), tw), tw)

	plain = lead + "  " + titleF
	colored = leadC + "  " + styTitle.Render(titleF)
	for i := range tail {
		plain += "  " + tail[i]
		colored += "  " + tailC[i]
	}
	if detached {
		colored = styDim.Render(plain) // detached: muted whole row
	}
	return plain, colored
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

// Preview rail sizing. The list keeps at least listMinWidth columns so titles
// stay readable; the rail is never narrower than railMinWidth.
const (
	railGap          = 3 // " │ " between the list and the rail
	railMinWidth     = 28
	railDefaultWidth = 38 // the default on narrower terminals; wider ones get a third
	listMinWidth     = 59 // what the fixed 38-col rail left at the 100-col minimum
	railStep         = 4 // columns per [ / ] press
)

// railWidth is the preview rail's width for the current terminal: the user's
// chosen width (m.railW) or the default, clamped to fit.
func (m model) railWidth() int {
	w := m.railW
	if w <= 0 {
		w = max(railDefaultWidth, m.width/3)
	}
	return min(max(w, railMinWidth), max(m.width-railGap-listMinWidth, railMinWidth))
}

// resizeRail widens (delta > 0) or narrows the preview rail and remembers it.
func (m *model) resizeRail(delta int) {
	if !m.showRail() {
		m.flash = "preview is off — press p to show it"
		return
	}
	before := m.railWidth()
	m.railW = before + delta
	m.railW = m.railWidth() // clamp, so the next press moves from the visible width
	switch {
	case m.railW == before && delta > 0:
		m.flash = fmt.Sprintf("preview %d cols (widest)", m.railW)
	case m.railW == before:
		m.flash = fmt.Sprintf("preview %d cols (narrowest)", m.railW)
	default:
		m.flash = fmt.Sprintf("preview %d cols", m.railW)
	}
	m.savePrefs()
}

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

// selectedDetails is the selected row's session details (zero if none).
func (m model) selectedDetails() sessionDetails {
	if m.cursor >= len(m.rows) {
		return sessionDetails{}
	}
	return m.rows[m.cursor].details()
}
