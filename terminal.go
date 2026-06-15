package main

// terminalDriver drives macOS Terminal.app. Tabs have no stable id, but their
// `tty` is unique and stable, so we use the tty as the surface handle. Terminal
// has no split panes, which keeps things simple.
type terminalDriver struct{}

func (terminalDriver) Name() string { return "terminal" }

const terminalEnumScript = `tell application "Terminal"
  set US to (ASCII character 31)
  set out to ""
  repeat with w in windows
    repeat with t in tabs of w
      set ttyv to ""
      try
        set ttyv to (tty of t) as text
      end try
      set nm to ""
      try
        set nm to (custom title of t) as text
      end try
      if ttyv is not "" then
        set out to out & ttyv & US & ttyv & US & "" & US & nm & linefeed
      end if
    end repeat
  end repeat
  return out
end tell`

func (terminalDriver) Surfaces() []Terminal {
	out, err := osaOutput(terminalEnumScript)
	if err != nil {
		return nil
	}
	return parseSurfaces(out)
}

// Focus the tab whose tty matches (the id we handed out is the tty).
const terminalFocusScript = `on run argv
  set wantTty to item 1 of argv
  tell application "Terminal"
    repeat with w in windows
      repeat with t in tabs of w
        if (tty of t) is wantTty then
          set selected of t to true
          set frontmost of w to true
          activate
          return
        end if
      end repeat
    end repeat
  end tell
end run`

func (terminalDriver) Focus(id string) error {
	return osaRun(terminalFocusScript, id)
}

// argv: 1=working directory, 2=command. Terminal's `do script` opens a new
// window (it has no scriptable "new tab"), so resume lands in a new window.
const terminalResumeScript = `on run argv
  set theCwd to item 1 of argv
  set theCmd to item 2 of argv
  tell application "Terminal"
    activate
    do script "cd " & quoted form of theCwd & " && " & theCmd
  end tell
end run`

func (terminalDriver) OpenResume(cwd, sid string) error {
	return osaRun(terminalResumeScript, cwd, resumeCommand(sid))
}
