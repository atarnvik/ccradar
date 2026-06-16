package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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
	Notify  bool `json:"notify"`
	Preview bool `json:"preview"`
}

func configPath() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return filepath.Join(d, "ccradar", "config.json")
	}
	return filepath.Join(homeDir(), ".config", "ccradar", "config.json")
}

// loadConfig reads saved preferences: notifications default on, the preview pane
// off (opt-in via `p`). Absent keys keep the defaults (Unmarshal only sets
// present fields), so a saved choice is always honored.
func loadConfig() config {
	c := config{Notify: true, Preview: false}
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
