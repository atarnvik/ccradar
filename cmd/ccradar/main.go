package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// popupMode (--popup) quits after enter takes the user to a session, for running
// ccradar as a throwaway picker, e.g. in a floating pane bound to a hotkey.
var popupMode bool

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
		case "--popup":
			popupMode = true
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
	if popupMode {
		cfg.Notify = false // a short-lived popup shouldn't duplicate a long-running ccradar's notifications
	}
	m := model{notify: cfg.Notify, preview: cfg.Preview, sort: parseSort(cfg.Sort), showCost: cfg.Cost}
	if demoMode() {
		m.prStatus = demoPRStatus()
	}
	if _, err := tea.NewProgram(m, tea.WithAltScreen()).Run(); err != nil {
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
  ccradar --popup       quit after enter jumps to or resumes a session (for hotkey popups)

Terminal: auto-detected from $TERM_PROGRAM (Ghostty, iTerm2, Terminal.app, Rex).
Override with CCRADAR_TERM=ghostty|iterm|terminal|rex.

Keys: ↑/↓ move · tab/1/2/3 switch view · / search · s sort · n notify · o open PR
      enter focus/resume/attach · q quit`)
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
	m := model{sessions: sessions, agents: loadAgents(), agentsLoaded: true, preview: true, width: 120, height: 24}
	m.history = loadHistory(m.hiddenFromHistory())
	m.prStatus = fetchPRs(m.prLinksDue())
	if demoMode() {
		m.prStatus = demoPRStatus()
	}
	m.rebuild()
	fmt.Println("=== ACTIVE ===")
	fmt.Print(m.View())
	m.view = viewHistory
	m.cursor = 0
	m.rebuild()
	fmt.Println("\n=== HISTORICAL ===")
	fmt.Print(m.View())
	m.view = viewAgents
	m.cursor = 0
	m.rebuild()
	fmt.Println("\n=== AGENTS ===")
	fmt.Print(m.View())
}
