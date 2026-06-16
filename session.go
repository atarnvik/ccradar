package main

import (
	"bufio"
	"bytes"
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
	Model     string // model of the last non-sidechain assistant turn
	Tty       string // controlling tty of the process (for exact surface matching)
	SurfaceID string // matched terminal surface id; "" if no tab found
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

// dirFilter, when set, scopes ccradar to a directory and its subdirectories.
var dirFilter string

// setDirFilter records the directory scope, expanding ~ and resolving to an
// absolute, cleaned path.
func setDirFilter(p string) {
	if p == "~" || strings.HasPrefix(p, "~/") {
		p = filepath.Join(homeDir(), p[1:])
	}
	if abs, err := filepath.Abs(p); err == nil {
		p = abs
	}
	dirFilter = filepath.Clean(p)
}

// underFilter reports whether cwd is the filter directory or below it. With no
// filter set it always returns true.
func underFilter(cwd string) bool {
	if dirFilter == "" {
		return true
	}
	cwd = filepath.Clean(cwd)
	return cwd == dirFilter || strings.HasPrefix(cwd, dirFilter+string(filepath.Separator))
}

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
	if demoMode() {
		return demoSessions()
	}
	entries, err := os.ReadDir(sessionsDir())
	if err != nil {
		return nil
	}
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
		if !underFilter(rf.CWD) {
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
		s.Title, s.Model = metaFor(rf.SessionID)
		s.Tty = procTty(rf.PID)
		out = append(out, s)
	}

	// Pair sessions to terminal surfaces in two passes so an untitled session
	// can't steal a titled one's surface. Pass 1: exact (tty) or cwd+title.
	// Pass 2: not-yet-titled sessions → a fresh Claude tab in the same dir
	// (needed on terminals without tty, e.g. Ghostty 1.3.1).
	terms := activeDriver().Surfaces()
	used := map[string]bool{}
	for i := range out {
		out[i].SurfaceID = matchTerminal(terms, used, out[i])
	}
	for i := range out {
		if out[i].SurfaceID == "" && out[i].Title == "" {
			out[i].SurfaceID = matchUntitled(terms, used, out[i])
		}
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CWD != out[j].CWD {
			return out[i].CWD < out[j].CWD
		}
		return out[i].UpdatedAt > out[j].UpdatedAt
	})
	return out
}

// metaFor returns the latest ai-title and the model of the last non-sidechain
// assistant turn for a session, in a single pass over its transcript.
func metaFor(sid string) (title, model string) {
	if sid == "" {
		return "", ""
	}
	matches, _ := filepath.Glob(filepath.Join(projectsDir(), "*", sid+".jsonl"))
	if len(matches) == 0 {
		return "", ""
	}
	f, err := os.Open(matches[0])
	if err != nil {
		return "", ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024) // allow long transcript lines
	for sc.Scan() {
		line := sc.Bytes()
		if t, ok := scanTitle(line); ok {
			title = t // keep last
		}
		if mdl, ok := scanModel(line); ok {
			model = mdl // keep last main-thread model
		}
	}
	return title, model
}

// scanTitle extracts an ai-title from a transcript line, if present.
func scanTitle(line []byte) (string, bool) {
	if !bytes.Contains(line, []byte(`"type":"ai-title"`)) {
		return "", false
	}
	var rec struct {
		AiTitle string `json:"aiTitle"`
	}
	if json.Unmarshal(line, &rec) == nil && rec.AiTitle != "" {
		return rec.AiTitle, true
	}
	return "", false
}

// scanModel extracts the model from a non-sidechain assistant line, if present.
// Sidechain (sub-agent) turns are skipped so the main thread's model wins.
func scanModel(line []byte) (string, bool) {
	if !bytes.Contains(line, []byte(`"type":"assistant"`)) {
		return "", false
	}
	var rec struct {
		IsSidechain bool `json:"isSidechain"`
		Message     struct {
			Model string `json:"model"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &rec) == nil && !rec.IsSidechain && rec.Message.Model != "" {
		return rec.Message.Model, true
	}
	return "", false
}

// modelShort trims the "claude-" prefix and a trailing date snapshot for display
// (e.g. "claude-opus-4-8" -> "opus-4-8", "claude-haiku-4-5-20251001" -> "haiku-4-5").
func modelShort(m string) string {
	if m == "" {
		return ""
	}
	m = strings.TrimPrefix(m, "claude-")
	if i := strings.LastIndex(m, "-"); i > 0 {
		if tail := m[i+1:]; len(tail) >= 6 && isAllDigits(tail) {
			m = m[:i]
		}
	}
	return m
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
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
