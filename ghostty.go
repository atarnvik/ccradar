package main

import (
	"os/exec"
	"strings"
)

// Terminal is one Ghostty terminal surface as seen via AppleScript.
type Terminal struct {
	ID    string
	CWD   string
	Title string // the surface's own title (Ghostty sets it to the Claude title)
}

// Enumerate EVERY Ghostty terminal surface (every split pane in every tab), with
// its own id, working directory, and title. Iterating all surfaces — not just the
// tab's focused one — means we still find a claude pane when a sibling split (logs,
// git, a shell) is the active surface.
// NOTE: Ghostty newer than 1.3.1 also exposes terminal `tty`/`pid`; adding those
// here would allow exact session<->surface matching instead of cwd+title.
const enumScript = `tell application "Ghostty"
  set US to (ASCII character 31)
  set out to ""
  repeat with w in windows
    repeat with t in tabs of w
      repeat with trm in terminals of t
        set cwdv to ""
        try
          set cwdv to (working directory of trm) as text
        end try
        set out to out & (id of trm as text) & US & cwdv & US & (name of trm as text) & linefeed
      end repeat
    end repeat
  end repeat
  return out
end tell`

func ghosttyTerminals() []Terminal {
	out, err := exec.Command("osascript", "-e", enumScript).Output()
	if err != nil {
		return nil // Ghostty not running / not scriptable
	}
	var terms []Terminal
	for _, line := range strings.Split(string(out), "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\x1f")
		if len(p) < 3 {
			continue
		}
		terms = append(terms, Terminal{ID: p[0], CWD: p[1], Title: p[2]})
	}
	return terms
}

// matchTerminal pairs a session to an unclaimed terminal by cwd + title substring.
// An empty title can't be disambiguated, so it returns "".
func matchTerminal(terms []Terminal, used map[string]bool, cwd, title string) string {
	if title == "" {
		return ""
	}
	for _, t := range terms {
		if used[t.ID] {
			continue
		}
		if t.CWD == cwd && strings.Contains(t.Title, title) {
			used[t.ID] = true
			return t.ID
		}
	}
	return ""
}

// Focus a Ghostty terminal by its stable id, bringing its window/tab to front.
const focusScript = `on run argv
  set wantId to item 1 of argv
  tell application "Ghostty"
    repeat with w in windows
      repeat with t in tabs of w
        repeat with trm in terminals of t
          if (id of trm as text) is wantId then
            focus trm
            return
          end if
        end repeat
      end repeat
    end repeat
  end tell
end run`

func focusTerminal(id string) error {
	return exec.Command("osascript", "-e", focusScript, id).Run()
}

// Open a new Ghostty tab in the given directory and run the command there.
// argv: 1=working directory, 2=initial input (command + newline).
const resumeScript = `on run argv
  set theCwd to item 1 of argv
  set theInput to item 2 of argv
  tell application "Ghostty"
    set cfg to new surface configuration
    set initial working directory of cfg to theCwd
    set initial input of cfg to theInput
    if (count of windows) > 0 then
      new tab in front window with configuration cfg
    else
      new window with configuration cfg
    end if
  end tell
end run`

// resumeCommand is what we run/copy to resume a past session in its project dir.
func resumeCommand(sessionID string) string {
	return "claude --resume " + sessionID
}

// openResumeTab opens a new Ghostty tab in cwd that auto-runs the resume command.
func openResumeTab(cwd, sessionID string) error {
	input := resumeCommand(sessionID) + "\n"
	return exec.Command("osascript", "-e", resumeScript, cwd, input).Run()
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
