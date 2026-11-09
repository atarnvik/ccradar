package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	tea "github.com/charmbracelet/bubbletea"
)

// notifyScript posts a macOS notification. Arguments are passed via argv so no
// AppleScript string escaping is needed (unicode-safe).
const notifyScript = `on run argv
	display notification (item 2 of argv) with title (item 1 of argv) sound name "Glass"
end run`

// sendNotification posts a native notification; failures are silently ignored
// (e.g. notification permission not yet granted). It prefers terminal-notifier
// (its own app identity → reliable banners, and a clickable -execute action) and
// falls back to osascript, which is delivered under Script Editor's identity and
// can't run a click action.
func sendNotification(title, body, execCmd string) {
	if title == "" {
		title = "ccradar"
	}
	if path, err := exec.LookPath("terminal-notifier"); err == nil {
		args := []string{"-title", title, "-message", body, "-sound", "Glass"}
		if execCmd != "" {
			args = append(args, "-execute", execCmd) // run on click → focus the tab
		}
		_ = exec.Command(path, args...).Run()
		return
	}
	_ = exec.Command("osascript", "-e", notifyScript, title, body).Run()
}

// ---- persisted preferences ----

type config struct {
	Notify  bool   `json:"notify"`
	Preview bool   `json:"preview"`
	Sort    string `json:"sort"` // "recent" or "alpha"
	Cost    bool   `json:"cost"` // show session cost (toggled with $)
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ccradar", "config.json")
	}
	return filepath.Join(homeDir(), ".config", "ccradar", "config.json")
}

// loadConfig reads saved preferences: notifications default on, the preview pane
// off (opt-in via `p`), sort by last-active. Absent keys keep the defaults
// (Unmarshal only sets present fields), so a saved choice is always honored.
func loadConfig() config {
	c := config{Notify: true, Preview: false, Sort: "recent"}
	b, err := os.ReadFile(configPath())
	if err != nil {
		return c
	}
	_ = json.Unmarshal(b, &c)
	return c
}

// saveConfig persists preferences; errors are ignored (best-effort).
func saveConfig(c config) {
	p := configPath()
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	if b, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(p, b, 0o644)
	}
}

// observe diffs the incoming sessions against the last seen statuses and returns
// notification commands when a session leaves "busy" — either finishing (idle)
// or pausing for your input (waiting). prevStatus is always refreshed so
// toggling notify on never misfires on history.
func (m *model) observe(next []Session) []tea.Cmd {
	var cmds []tea.Cmd
	cur := make(map[string]string, len(next))
	for _, s := range next {
		cur[s.SessionID] = s.Status
		if m.notify {
			prev, ok := m.prevStatus[s.SessionID]
			if ok && prev == "busy" && (s.Status == "idle" || s.Status == "waiting") {
				cmds = append(cmds, notifyCmd(notifTitle(s), notifBody(s), notifyExec(s.SessionID)))
			}
		}
	}
	m.prevStatus = cur
	return cmds
}

func notifyCmd(title, body, execCmd string) tea.Cmd {
	return func() tea.Msg {
		sendNotification(title, body, execCmd)
		return nil
	}
}

// notifyExec builds the shell command a notification runs when clicked: re-launch
// this binary headless to focus the session's tab. The terminal is pinned via
// CCRADAR_TERM because the click runs with no $TERM_PROGRAM, and the absolute
// path is used because the click's PATH won't include ~/go/bin or brew.
func notifyExec(sessionID string) string {
	exe, err := os.Executable()
	if err != nil || exe == "" || sessionID == "" {
		return ""
	}
	return fmt.Sprintf("CCRADAR_TERM=%s %s focus %s",
		shellQuote(activeDriver().Name()), shellQuote(exe), shellQuote(sessionID))
}

// focusSessionID re-resolves a session by id and focuses its terminal surface.
func focusSessionID(id string) {
	for _, s := range loadSessions() {
		if s.SessionID == id && s.SurfaceID != "" {
			_ = activeDriver().Focus(s.SurfaceID)
			return
		}
	}
}

// notifTitle / notifBody avoid repeating the directory: when a session has an
// AI title, that headlines the notification and the body carries the status +
// directory; otherwise the status headlines and the directory is the body.
// The wording reflects whether Claude finished (idle) or needs input (waiting).
func notifTitle(s Session) string {
	if s.Title != "" {
		return s.Title
	}
	if s.Status == "waiting" {
		return "⏸ Claude needs your input"
	}
	return "✓ Claude finished responding"
}

func notifBody(s Session) string {
	dir := filepath.Base(s.CWD)
	if s.Title == "" {
		return dir
	}
	if s.Status == "waiting" {
		return "⏸ needs input · " + dir
	}
	return "✓ finished · " + dir
}
