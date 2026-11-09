package main

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestWrapMarkdown(t *testing.T) {
	md := "I added **the keys** and opened [#23](https://github.com/o/r/pull/23).\n\n" +
		"## Details\n- `[` moves the divider left so the preview gets wider\n* `]` moves it right\n" +
		"```sh\ngo build ./cmd/ccradar\n```\nDone."
	got := strings.Join(wrapMarkdown(md, 30), "\n")
	want := strings.Join([]string{
		"I added the keys and opened",
		"#23.",
		"Details",
		"• [ moves the divider left so",
		"  the preview gets wider",
		"• ] moves it right",
		"go build ./cmd/ccradar",
		"Done.",
	}, "\n")
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCapLines(t *testing.T) {
	lines := []string{"one", "two", "three"}
	if got := capLines(lines, 0, 10); len(got) != 3 {
		t.Errorf("no cap: %v", got)
	}
	if got := capLines(lines, 2, 10); strings.Join(got, "|") != "one|two…" {
		t.Errorf("cap 2: %v", got)
	}
	if got := capLines([]string{"aaaaa", "bbbbb"}, 1, 5); got[0] != "aaaa…" {
		t.Errorf("full-width line keeps its width with the ellipsis: %q", got[0])
	}
	if lines[1] != "two" {
		t.Error("capLines modified its input")
	}
}

func TestPreviewLayout(t *testing.T) {
	long := strings.Repeat("Lots of detail here. ", 40)
	m := model{preview: true, width: 120, height: 40, prStatus: map[string]prInfo{
		"u23": {State: "OPEN", Checks: "SUCCESS", Title: "feat: resize the preview pane"},
	}}
	m.sessions = []Session{{
		SessionID: "s", CWD: "/w/app", Status: "idle", SurfaceID: "t", UpdatedAt: nowMs(),
		Title: "Go TUI repo organization", Branch: "main", Model: "claude-opus-5-5",
		LastPrompt: "yes", LastReply: "Opened **#23**.\n- one\n- two\n" + long,
		Shells: []bgShell{{ID: "b", Desc: "Serve a demo page on :8765", Started: time.Now()}},
		sessionDetails: sessionDetails{
			Recap: long, RecapAt: time.Now().Add(-2 * time.Minute),
			PRs:   []prLink{{URL: "u23", Number: 23}},
		},
	}}
	m.rebuild()
	const w = 50
	lines := m.previewLines(w, 40)
	var plain []string
	for _, ln := range lines {
		if lipgloss.Width(ln) != w {
			t.Fatalf("line width %d, want %d: %q", lipgloss.Width(ln), w, ln)
		}
		plain = append(plain, strings.TrimRight(stripStyles(ln), " "))
	}
	text := strings.Join(plain, "\n")

	// Sections in order, each with a full-width rule heading.
	var order []string
	for _, ln := range plain {
		if strings.Contains(ln, " ──") {
			order = append(order, strings.Fields(ln)[0])
			if len([]rune(ln)) != w {
				t.Errorf("heading %q is %d wide, want %d", ln, len([]rune(ln)), w)
			}
		}
	}
	if got := strings.Join(order, ","); got != "RECAP,PRS,SHELLS,LAST" {
		t.Fatalf("sections = %s\n%s", got, text)
	}
	if !strings.Contains(text, "RECAP · 2m ago ─") || !strings.Contains(text, "#23 ✓ feat: resize the preview pane") {
		t.Errorf("recap age or PR line missing:\n%s", text)
	}
	if !strings.Contains(text, "main · opus-5-5 · idle") {
		t.Errorf("meta line missing:\n%s", text)
	}
	if strings.Contains(text, "**") || !strings.Contains(text, "claude Opened #23.") || !strings.Contains(text, "       • one") {
		t.Errorf("reply not cleaned up:\n%s", text)
	}
	if recap := strings.Count(text, "Lots of detail"); recap > 40 {
		t.Errorf("long text not capped")
	}
	if !strings.Contains(text, "…") {
		t.Errorf("capped sections should end with …:\n%s", text)
	}
}

// stripStyles removes ANSI escape sequences.
func stripStyles(s string) string {
	var b strings.Builder
	esc := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			esc = true
		case esc && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z'):
			esc = false
		case !esc:
			b.WriteRune(r)
		}
	}
	return b.String()
}
