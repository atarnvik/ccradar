package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// Terminal is one terminal surface (a tab, split, or iTerm session) as seen via
// AppleScript.
type Terminal struct {
	ID    string // stable handle used to focus it (driver-specific)
	Tty   string // controlling tty, e.g. /dev/ttys001 ("" if the app hides it)
	CWD   string // working directory ("" if the app doesn't expose it)
	Title string // surface title
}

// TermDriver abstracts a terminal app: enumerate its surfaces, focus one, and
// open a new tab/window that resumes a session. One implementation per app.
type TermDriver interface {
	Name() string
	Surfaces() []Terminal
	Focus(id string) error
	OpenResume(cwd, sid string) error
}

var cachedDriver TermDriver

// activeDriver returns the driver for the terminal ccradar is talking to,
// chosen once from $CCRADAR_TERM or $TERM_PROGRAM.
func activeDriver() TermDriver {
	if demoMode() {
		return demoDriver{}
	}
	if cachedDriver == nil {
		cachedDriver = detectDriver()
	}
	return cachedDriver
}

// detectDriver picks a driver from the environment. $CCRADAR_TERM overrides the
// auto-detected $TERM_PROGRAM (which every supported terminal sets). Unknown
// terminals fall back to Ghostty.
func detectDriver() TermDriver {
	name := os.Getenv("CCRADAR_TERM")
	if name == "" {
		name = os.Getenv("TERM_PROGRAM")
	}
	switch strings.ToLower(name) {
	case "iterm", "iterm2", "iterm.app":
		return itermDriver{}
	case "terminal", "apple_terminal":
		return terminalDriver{}
	default:
		return ghosttyDriver{}
	}
}

// matchTerminal pairs a session to an unclaimed surface: an exact tty match when
// both sides expose one, otherwise cwd + title (the fallback for Ghostty 1.3.1,
// which doesn't report a tty). Returns the surface ID, or "" if none.
func matchTerminal(terms []Terminal, used map[string]bool, s Session) string {
	if s.Tty != "" {
		for _, t := range terms {
			if used[t.ID] {
				continue
			}
			if t.Tty != "" && t.Tty == s.Tty {
				used[t.ID] = true
				return t.ID
			}
		}
	}
	if s.Title == "" {
		return ""
	}
	for _, t := range terms {
		if used[t.ID] || t.Tty != "" {
			continue
		}
		if t.CWD == s.CWD && strings.Contains(t.Title, s.Title) {
			used[t.ID] = true
			return t.ID
		}
	}
	return ""
}

// matchUntitled is a last-resort pairing for a session that has no AI title yet
// (a brand-new chat): match it to an unclaimed, tty-less surface in the same
// directory whose title looks like a fresh Claude tab (e.g. "Claude Code").
// Only relevant on terminals that don't expose a tty (Ghostty 1.3.1) — elsewhere
// the exact tty match in matchTerminal already handles untitled sessions.
func matchUntitled(terms []Terminal, used map[string]bool, s Session) string {
	for _, t := range terms {
		if used[t.ID] || t.Tty != "" {
			continue
		}
		if t.CWD == s.CWD && looksLikeClaude(t.Title) {
			used[t.ID] = true
			return t.ID
		}
	}
	return ""
}

func looksLikeClaude(title string) bool {
	return strings.Contains(strings.ToLower(title), "claude")
}

// parseSurfaces decodes the shared enum output: one surface per line, fields
// "ID US TTY US CWD US TITLE" (US = ASCII 31). Drivers emit empty fields they
// can't provide.
func parseSurfaces(out string) []Terminal {
	var terms []Terminal
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\x1f")
		if len(p) < 4 {
			continue
		}
		terms = append(terms, Terminal{ID: p[0], Tty: p[1], CWD: p[2], Title: p[3]})
	}
	return terms
}

// procTty returns the controlling tty of a pid as /dev/ttysNNN ("" if none).
func procTty(pid int) string {
	out, err := exec.Command("ps", "-o", "tty=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	s := strings.TrimSpace(string(out))
	if s == "" || s == "?" || s == "??" {
		return ""
	}
	if !strings.HasPrefix(s, "/dev/") {
		s = "/dev/" + s
	}
	return s
}

func osaOutput(script string, args ...string) (string, error) {
	out, err := exec.Command("osascript", append([]string{"-e", script}, args...)...).Output()
	return string(out), err
}

func osaRun(script string, args ...string) error {
	return exec.Command("osascript", append([]string{"-e", script}, args...)...).Run()
}

// ---- resume command, shared across drivers ----

// resumeCommand is what we run/copy to resume a past session in its project dir.
func resumeCommand(sessionID string) string {
	return "claude --resume " + sessionID
}

// copyResumeCommand puts a ready-to-paste resume command on the clipboard.
func copyResumeCommand(cwd, sessionID string) error {
	cmd := exec.Command("pbcopy")
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	_, _ = in.Write([]byte("cd " + shellQuote(cwd) + " && " + resumeCommand(sessionID)))
	_ = in.Close()
	return cmd.Wait()
}

// shellQuote single-quotes a string for safe pasting into a shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
