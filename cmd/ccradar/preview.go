package main

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Line caps for the preview's longer sections; the full text is one keypress
// away in the session itself.
const (
	previewTitleLines  = 2
	previewRecapLines  = 5
	previewPromptLines = 3
	previewReplyLines  = 6
	exchangeLabelWidth = 7 // "claude "
)

// pline is one preview line: its plain text (for width) and how it renders.
type pline struct{ plain, rendered string }

type previewBuilder struct {
	w     int
	lines []pline
}

func (b *previewBuilder) add(text string, style lipgloss.Style) {
	b.lines = append(b.lines, pline{text, style.Render(text)})
}

// wrapped adds text wrapped to the pane, at most max lines (0 = no cap).
func (b *previewBuilder) wrapped(text string, style lipgloss.Style, max int) {
	for _, ln := range capLines(wrapText(text, b.w), max, b.w) {
		b.add(ln, style)
	}
}

// heading starts a section: a blank line, then "LABEL · detail ────".
func (b *previewBuilder) heading(label, detail string) {
	b.lines = append(b.lines, pline{})
	text := strings.ToUpper(label)
	rendered := styPrevLbl.Render(text)
	if detail != "" {
		text += " · " + detail
		rendered += styDim.Render(" · " + detail)
	}
	if n := len([]rune(text)); n+2 <= b.w {
		rule := " " + strings.Repeat("─", b.w-n-1)
		text += rule
		rendered += styRule.Render(rule)
	} else {
		text = trunc(text, b.w)
		rendered = styPrevLbl.Render(text)
	}
	b.lines = append(b.lines, pline{text, rendered})
}

// labeled adds text under a fixed-width label in the left gutter ("you",
// "claude"), keeping the text's own line breaks and bullets.
func (b *previewBuilder) labeled(label, text string, max int) {
	body := capLines(wrapMarkdown(text, b.w-exchangeLabelWidth), max, b.w-exchangeLabelWidth)
	for i, ln := range body {
		gutter := strings.Repeat(" ", exchangeLabelWidth)
		if i == 0 {
			gutter = truncPad(label, exchangeLabelWidth)
		}
		b.lines = append(b.lines, pline{gutter + ln, styDim.Render(gutter) + styPrevBody.Render(ln)})
	}
}

// render returns exactly h lines, each padded to the pane width.
func (b *previewBuilder) render(h int) []string {
	out := make([]string, h)
	for i := range out {
		if i < len(b.lines) {
			out[i] = padVis(b.lines[i].rendered, len([]rune(b.lines[i].plain)), b.w)
		} else {
			out[i] = strings.Repeat(" ", b.w)
		}
	}
	return out
}

// previewLines renders the selected row's detail pane: exactly h lines of
// width w. A header (title, directory, branch/model/status), then sections:
// the finder's match reason, the recap, PRs, background shells, and the
// latest exchange.
func (m model) previewLines(w, h int) []string {
	b := &previewBuilder{w: w}
	if m.cursor >= len(m.rows) {
		return b.render(h)
	}
	var title, source, dir, prompt, reply, cost string
	var meta []string
	var d sessionDetails
	var shells []bgShell
	switch r := m.rows[m.cursor]; r.kind {
	case rowSession:
		s := r.session
		d, shells = s.sessionDetails, s.Shells
		title, source, dir = displayTitle(d, s.Title), s.Source, dirDisplay(s.CWD)
		meta = []string{s.Branch, modelShort(s.Model), s.Status + " " + fmtAge(nowMs()-s.UpdatedAt)}
		prompt, reply = s.LastPrompt, s.LastReply
	case rowHist:
		hh := r.hist
		d = hh.sessionDetails
		title, source, dir = displayTitle(d, hh.Title), hh.Source, dirDisplay(hh.CWD)
		meta = []string{hh.Branch, modelShort(hh.Model), fmtAge(time.Since(hh.ModAt).Milliseconds()) + " ago"}
		prompt, reply = hh.LastPrompt, hh.LastReply
		if m.showCost {
			cost = costLine(d.Cost) // a live session's cost-state is from an earlier run
		}
	default:
		return b.render(h)
	}

	b.wrapped(title, styPrevTtl, previewTitleLines)
	if source != "" {
		b.add(trunc("⬢ "+source, w), styPrevLbl)
	}
	b.wrapped(dir, styDim, 2)
	b.wrapped(joinNonEmpty(meta, " · "), styDim, 2)
	if cost != "" {
		b.wrapped(cost, styDim, 1)
	}

	if why := m.selectedWhy(); why != "" {
		b.heading("Match", "")
		b.wrapped(why, styPrevBody, 0)
	}
	if d.Recap != "" {
		ago := ""
		if !d.RecapAt.IsZero() {
			ago = fmtAge(time.Since(d.RecapAt).Milliseconds()) + " ago"
		}
		b.heading("Recap", ago)
		b.wrapped(d.Recap, styPrevBody, previewRecapLines)
	}
	if len(d.PRs) > 0 {
		b.heading("PRs", "")
		first := max(len(d.PRs)-prPreviewMax, 0)
		numW := 0
		for _, l := range d.PRs[first:] {
			numW = max(numW, len(fmt.Sprintf("#%d", l.Number)))
		}
		for i := len(d.PRs) - 1; i >= first; i-- {
			b.lines = append(b.lines, prPreviewLine(d.PRs[i], m.prStatus[d.PRs[i].URL], numW, w))
		}
		if first > 0 {
			b.add(fmt.Sprintf("+%d older · O to pick from all %d", first, len(d.PRs)), styDim)
		}
	}
	if len(shells) > 0 {
		b.heading("Shells", "")
		for _, sh := range shells {
			desc := firstNonEmpty(sh.Desc, sh.ID)
			if !sh.Started.IsZero() {
				desc += " · " + fmtAge(time.Since(sh.Started).Milliseconds())
			}
			b.add(truncEllipsis("⚙ "+desc, w), styPrevBody)
		}
	}
	if prompt != "" || reply != "" {
		b.heading("Last exchange", "")
		if prompt != "" {
			b.labeled("you", prompt, previewPromptLines)
		}
		if reply != "" {
			b.labeled("claude", reply, previewReplyLines)
		}
	}
	return b.render(h)
}

// prPreviewLine is one PR in the preview: "#23 ✓ title…", the number colored
// by state and the glyph showing CI for open PRs.
func prPreviewLine(l prLink, info prInfo, numW, w int) pline {
	num := fmt.Sprintf("%-*s", numW, fmt.Sprintf("#%d", l.Number))
	glyph, gs := stateGlyph(info)
	rest := info.Title
	if rest == "" && info.State != "" {
		rest = strings.ToLower(info.State)
	}
	rest = truncEllipsis(rest, w-len([]rune(num))-3)
	plain := num + " " + glyph + " " + rest
	return pline{plain, prStyle(info).Render(num) + " " + gs.Render(glyph) + " " + styPrevBody.Render(rest)}
}

var (
	mdLink    = regexp.MustCompile(`\[([^\]]+)\]\([^)]+\)`)
	mdHeading = regexp.MustCompile(`^#{1,6}\s+`)
	mdBullet  = regexp.MustCompile(`^\s*[-*+]\s+`)
)

// wrapMarkdown wraps Claude's markdown to width w for plain display: emphasis
// and code markers are dropped, links keep their text, code fences and blank
// lines go, and bullets become "•" with a hanging indent.
func wrapMarkdown(s string, w int) []string {
	var out []string
	for _, ln := range strings.Split(s, "\n") {
		ln = strings.TrimSpace(ln)
		if ln == "" || strings.HasPrefix(ln, "```") {
			continue
		}
		ln = mdHeading.ReplaceAllString(ln, "")
		ln = mdLink.ReplaceAllString(ln, "$1")
		ln = strings.NewReplacer("**", "", "__", "", "`", "").Replace(ln)
		if mdBullet.MatchString(ln) {
			for i, part := range wrapText(mdBullet.ReplaceAllString(ln, ""), w-2) {
				if i == 0 {
					out = append(out, "• "+part)
				} else {
					out = append(out, "  "+part)
				}
			}
			continue
		}
		out = append(out, wrapText(ln, w)...)
	}
	return out
}

// capLines keeps at most max lines (0 = all), ending a cut with "…".
func capLines(lines []string, max, w int) []string {
	if max <= 0 || len(lines) <= max {
		return lines
	}
	lines = append([]string(nil), lines[:max]...)
	last := []rune(strings.TrimRight(lines[max-1], " "))
	if len(last) >= w {
		last = last[:w-1]
	}
	lines[max-1] = string(last) + "…"
	return lines
}

// truncEllipsis cuts s to n runes, ending with "…" when it had to cut.
func truncEllipsis(s string, n int) string {
	r := []rune(s)
	if n <= 0 {
		return ""
	}
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

func joinNonEmpty(parts []string, sep string) string {
	var keep []string
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, sep)
}
