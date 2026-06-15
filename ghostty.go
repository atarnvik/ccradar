package main

// ghosttyDriver drives Ghostty via its AppleScript API.
type ghosttyDriver struct{}

func (ghosttyDriver) Name() string { return "ghostty" }

// Enumerate EVERY Ghostty terminal surface (every split pane in every tab). We
// iterate all surfaces — not just the focused one — so we still find a claude
// pane when a sibling split (logs, git, a shell) is the active surface. Ghostty
// >1.3.1 reports `tty`; we read it when present so matching upgrades to exact
// tty pairing automatically (1.3.1 leaves it empty → cwd+title fallback).
const ghosttyEnumScript = `tell application "Ghostty"
  set US to (ASCII character 31)
  set out to ""
  repeat with w in windows
    repeat with t in tabs of w
      repeat with trm in terminals of t
        set ttyv to ""
        try
          set ttyv to (tty of trm) as text
        end try
        set cwdv to ""
        try
          set cwdv to (working directory of trm) as text
        end try
        set out to out & (id of trm as text) & US & ttyv & US & cwdv & US & (name of trm as text) & linefeed
      end repeat
    end repeat
  end repeat
  return out
end tell`

func (ghosttyDriver) Surfaces() []Terminal {
	out, err := osaOutput(ghosttyEnumScript)
	if err != nil {
		return nil // Ghostty not running / not scriptable
	}
	return parseSurfaces(out)
}

// Focus a Ghostty terminal by its stable id, bringing its window/tab to front.
const ghosttyFocusScript = `on run argv
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

func (ghosttyDriver) Focus(id string) error {
	return osaRun(ghosttyFocusScript, id)
}

// Open a new Ghostty tab in the given directory and run the command there.
// argv: 1=working directory, 2=initial input (command + newline).
const ghosttyResumeScript = `on run argv
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

func (ghosttyDriver) OpenResume(cwd, sid string) error {
	return osaRun(ghosttyResumeScript, cwd, resumeCommand(sid)+"\n")
}
