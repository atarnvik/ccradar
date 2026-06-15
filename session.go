package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Session is one live Claude Code session, enriched with its title and the
// Ghostty terminal it maps to (if any).
type Session struct {
	PID       int
	SessionID string
	CWD       string
	Status    string
	UpdatedAt int64 // ms epoch
	Title     string
	GhosttyID string // matched Ghostty terminal id; "" if no tab found
}

func homeDir() string { h, _ := os.UserHomeDir(); return h }

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func sessionsDir() string { return envOr("CLAUDE_SESSIONS_DIR", filepath.Join(homeDir(), ".claude", "sessions")) }
func projectsDir() string { return envOr("CLAUDE_PROJECTS_DIR", filepath.Join(homeDir(), ".claude", "projects")) }
func procMatch() string   { return envOr("CLAUDE_PROC_MATCH", "claude") }

type regFile struct {
	PID       int    `json:"pid"`
	SessionID string `json:"sessionId"`
	CWD       string `json:"cwd"`
	Status    string `json:"status"`
	UpdatedAt int64  `json:"updatedAt"`
}

// pidAlive reports whether the process exists (EPERM still means it's there).
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// killProcess sends SIGTERM to a leftover session process.
func killProcess(pid int) error {
	if pid <= 0 {
		return nil
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// procComm returns the process command name (basename) for pid-reuse guarding.
func procComm(pid int) string {
	out, err := exec.Command("ps", "-o", "comm=", "-p", strconv.Itoa(pid)).Output()
	if err != nil {
		return ""
	}
	return filepath.Base(strings.TrimSpace(string(out)))
}

// loadSessions reads the registry, keeps live Claude sessions, attaches titles,
// matches each to a Ghostty terminal, and returns them sorted by cwd.
func loadSessions() []Session {
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return nil
	}
	terms := ghosttyTerminals()
	used := map[string]bool{}

	var out []Session
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		b, err := os.ReadFile(filepath.Join(sessionsDir(), e.Name()))
		if err != nil {
			continue
		}
		var rf regFile
		if json.Unmarshal(b, &rf) != nil || rf.PID == 0 {
			continue
		}
		if !pidAlive(rf.PID) || procComm(rf.PID) != procMatch() {
			continue
		}
		s := Session{
			PID:       rf.PID,
			SessionID: rf.SessionID,
			CWD:       rf.CWD,
			Status:    rf.Status,
			UpdatedAt: rf.UpdatedAt,
		}
		s.Title = titleFor(rf.SessionID)
		s.GhosttyID = matchTerminal(terms, used, s.CWD, s.Title)
		out = append(out, s)
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CWD != out[j].CWD {
			return out[i].CWD < out[j].CWD
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

// titleFor finds the latest ai-title for a session from its transcript.
func titleFor(sid string) string {
	if sid == "" {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(projectsDir(), "*", sid+".jsonl"))
	if len(matches) == 0 {
		return ""
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return ""
	}
	defer f.Close()

	var title string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024) // allow long transcript lines
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"type":"ai-title"`) {
			continue
		}
		var rec struct {
			AiTitle string `json:"aiTitle"`
		}
		if json.Unmarshal(line, &rec) == nil && rec.AiTitle != "" {
			title = rec.AiTitle // keep last
		}
	}
	return title
}

// fmtAge renders a millisecond delta as a compact age (e.g. 12s, 3m, 2h, 1d).
func fmtAge(deltaMs int64) string {
	if deltaMs < 0 {
		deltaMs = 0
	}
	s := deltaMs / 1000
	switch {
	case s < 60:
		return fmt.Sprintf("%ds", s)
	case s < 3600:
		return fmt.Sprintf("%dm", s/60)
	case s < 86400:
		return fmt.Sprintf("%dh", s/3600)
	default:
		return fmt.Sprintf("%dd", s/86400)
	}
}

func nowMs() int64 { return time.Now().UnixMilli() }
