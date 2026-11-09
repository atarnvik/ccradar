package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

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
	needs := 0
	for _, ag := range m.agents {
		if ag.needsYou() {
			needs++
		}
	}
	g := fmt.Sprintf(" Agents (%d) ", len(m.agents))
	switch {
	case needs == 1:
		g = fmt.Sprintf(" Agents (%d, 1 needs you) ", len(m.agents))
	case needs > 1:
		g = fmt.Sprintf(" Agents (%d, %d need you) ", len(m.agents), needs)
	}
	tabs := []string{a, h, g}
	for i := range tabs {
		if viewKind(i) == m.view {
			tabs[i] = styTabOn.Render(tabs[i])
		} else {
			tabs[i] = styTabOff.Render(tabs[i])
		}
	}
	return strings.Join(tabs, " ")
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
		switch m.view {
		case viewHistory:
			msg = "no past sessions"
		case viewAgents:
			msg = "no background agents (start one with claude --bg)"
			if !m.agentsLoaded {
				msg = "loading background agents…"
			}
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
		switch m.view {
		case viewActive:
			help += "focus tab · x kill detached"
		case viewHistory:
			help += "resume (new tab) · c copy cmd"
		case viewAgents:
			help += "attach (new tab) · c copy cmd"
		}
		if _, ok := m.selectedDetails().latestPR(); ok {
			help += " · o open PR"
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

// costColumnMinWidth / prColumnMinWidth are the narrowest lists that still get
// a cost / PR column.
const (
	costColumnMinWidth = 70
	prColumnMinWidth   = 70
)

// rowColumns builds the aligned columns for a session/history row. The title
// column flexes so the plain text is exactly cw-4 cells wide (the 4 is the row's
// left indent), keeping the highlighted and normal forms identical.
func (m model) rowColumns(r row, cw int) (plain, colored string) {
	switch r.kind {
	case rowSession:
		s := r.session
		sp, sst := statusText(s.Status)
		age := fmt.Sprintf("%4s", fmtAge(nowMs()-s.UpdatedAt))
		showPR := m.anyPR && cw >= prColumnMinWidth
		tw := cw - 40
		if showPR {
			tw -= prColumnWidth + 2
		}
		if m.anyShell {
			tw -= shellColumnWidth + 2
		}
		if tw < 8 {
			tw = 8
		}
		titleF := truncPad(displayTitle(s.sessionDetails, s.Title), tw)
		mdl := truncPad(modelShort(s.Model), 10)
		pr, prC := "", ""
		if showPR {
			pr, prC = prBadge(s.sessionDetails, m.prStatus)
			pr, prC = pr+"  ", prC+"  "
		}
		if m.anyShell {
			sh := strings.Repeat(" ", shellColumnWidth)
			if n := len(s.Shells); n > 0 {
				sh = truncPad(fmt.Sprintf("⚙%d", n), shellColumnWidth)
			}
			pr, prC = pr+sh+"  ", prC+styBusy.Render(sh)+"  "
		}
		if s.SurfaceID != "" {
			loc := truncPad("→ tab", 8)
			plain = fmt.Sprintf("%s %s  %s  %s%s  %s", sp, age, titleF, pr, mdl, loc)
			colored = fmt.Sprintf("%s %s  %s  %s%s  %s",
				sst.Render(sp), styDim.Render(age), styTitle.Render(titleF), prC, styModel.Render(mdl), styTab.Render(loc))
		} else {
			loc := truncPad(fmt.Sprintf("pid %d", s.PID), 8)
			plain = fmt.Sprintf("%s %s  %s  %s%s  %s", sp, age, titleF, pr, mdl, loc)
			colored = styDim.Render(plain) // detached: muted whole row
		}
	case rowAgent:
		a := r.agent
		sp, sst := agentStateText(a.State)
		age := fmt.Sprintf("%4s", fmtAge(time.Since(a.UpdatedAt).Milliseconds()))
		showPR := m.anyPR && cw >= prColumnMinWidth
		tw := cw - 40
		if showPR {
			tw -= prColumnWidth + 2
		}
		if tw < 8 {
			tw = 8
		}
		titleF := truncPad(firstNonEmpty(a.Name, a.ID), tw)
		mdl := truncPad(modelShort(a.Model), 10)
		pr, prC := "", ""
		if showPR {
			pr, prC = prBadge(a.sessionDetails, m.prStatus)
			pr, prC = pr+"  ", prC+"  "
		}
		loc := truncPad("⇢ attach", 8)
		plain = fmt.Sprintf("%s %s  %s  %s%s  %s", sp, age, titleF, pr, mdl, loc)
		colored = fmt.Sprintf("%s %s  %s  %s%s  %s",
			sst.Render(sp), styDim.Render(age), styTitle.Render(titleF), prC, styModel.Render(mdl), styTab.Render(loc))
	case rowHist:
		h := r.hist
		age := fmt.Sprintf("%4s", fmtAge(time.Since(h.ModAt).Milliseconds()))
		showCost := m.showCost && cw >= costColumnMinWidth
		showPR := m.anyPR && cw >= prColumnMinWidth
		tw := cw - 32
		if showCost {
			tw -= 7 // "$0.00" + 2 spaces
		}
		if showPR {
			tw -= prColumnWidth + 2
		}
		if tw < 8 {
			tw = 8
		}
		titleF := truncPad(displayTitle(h.sessionDetails, h.Title), tw)
		mdl := truncPad(modelShort(h.Model), 10)
		resume := truncPad("↻ resume", 8)
		cost, costC := "", ""
		if showCost {
			if h.Cost.known() {
				cost = fmtCost(h.Cost.USD)
			}
			cost = fmt.Sprintf("%5s", cost)
			cost, costC = cost+"  ", styDim.Render(cost)+"  "
		}
		pr, prC := "", ""
		if showPR {
			pr, prC = prBadge(h.sessionDetails, m.prStatus)
			pr, prC = pr+"  ", prC+"  "
		}
		plain = fmt.Sprintf("%s  %s  %s%s%s  %s", age, titleF, cost, pr, mdl, resume)
		colored = fmt.Sprintf("%s  %s  %s%s%s  %s",
			styDim.Render(age), styTitle.Render(titleF), costC, prC, styModel.Render(mdl), styTab.Render(resume))
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
		var title, source, dir, branch, model, status, age, prompt, reply, cost string
		var d sessionDetails
		var shells []bgShell
		var agent *Agent
		switch r := m.rows[m.cursor]; r.kind {
		case rowSession:
			s := r.session
			d = s.sessionDetails
			title, source, dir, branch = displayTitle(d, s.Title), s.Source, dirDisplay(s.CWD), s.Branch
			model, status, age = modelShort(s.Model), s.Status, fmtAge(nowMs()-s.UpdatedAt)
			prompt, reply = s.LastPrompt, s.LastReply
			shells = s.Shells
		case rowAgent:
			a := r.agent
			agent, d = &a, a.sessionDetails
			title, dir, branch = firstNonEmpty(a.Name, a.ID), dirDisplay(a.CWD), a.Branch
			model, status, age = modelShort(a.Model), a.State, fmtAge(time.Since(a.UpdatedAt).Milliseconds())
			reply = a.LastReply
			if a.Result != "" {
				reply = "" // the result says it better
			}
		case rowHist:
			hh := r.hist
			d = hh.sessionDetails
			title, dir, branch = displayTitle(d, hh.Title), dirDisplay(hh.CWD), hh.Branch
			model, age = modelShort(hh.Model), fmtAge(time.Since(hh.ModAt).Milliseconds())
			prompt, reply = hh.LastPrompt, hh.LastReply
			if m.showCost {
				cost = costLine(d.Cost) // a live session's cost-state is from an earlier run
			}
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
		if cost != "" {
			push(cost, styDim)
		}
		if agent != nil {
			for _, f := range []struct{ label, text string }{
				{"▸ Needs you", agent.Needs}, {"▸ Status", agent.Detail},
				{"▸ Result", agent.Result}, {"▸ Asked", agent.Intent},
			} {
				if f.text != "" {
					segs = append(segs, seg{"", styDim})
					push(f.label, styPrevLbl)
					push(f.text, styPrevBody)
				}
			}
		}
		if len(shells) > 0 {
			segs = append(segs, seg{"", styDim})
			push("▸ Background shells", styPrevLbl)
			for _, sh := range shells {
				desc := sh.Desc
				if desc == "" {
					desc = sh.ID
				}
				if !sh.Started.IsZero() {
					desc += " · " + fmtAge(time.Since(sh.Started).Milliseconds())
				}
				push("⚙ "+desc, styPrevBody)
			}
		}
		if len(d.PRs) > 0 {
			segs = append(segs, seg{"", styDim})
			push("▸ PRs · o opens the latest", styPrevLbl)
			for i := len(d.PRs) - 1; i >= 0 && i >= len(d.PRs)-prPreviewMax; i-- {
				info := m.prStatus[d.PRs[i].URL]
				push(prSummary(d.PRs[i], info), prStyle(info))
				if info.Title != "" {
					push(info.Title, styPrevBody)
				}
			}
		}
		if why := m.selectedWhy(); why != "" {
			segs = append(segs, seg{"", styDim})
			push("▸ Match", styPrevLbl)
			push(why, styPrevBody)
		}
		if d.Recap != "" {
			segs = append(segs, seg{"", styDim})
			label := "▸ Recap"
			if !d.RecapAt.IsZero() {
				label += " · " + fmtAge(time.Since(d.RecapAt).Milliseconds()) + " ago"
			}
			push(label, styPrevLbl)
			push(d.Recap, styPrevBody)
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

// selectedDetails is the selected row's session details (zero if none).
func (m model) selectedDetails() sessionDetails {
	if m.cursor >= len(m.rows) {
		return sessionDetails{}
	}
	return m.rows[m.cursor].details()
}
