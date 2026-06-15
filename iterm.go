package main

// itermDriver drives iTerm2 via its AppleScript API. iTerm exposes a stable
// session `id` and `tty`, so matching is exact (by tty) and split panes Just Work
// — each split is its own session with its own tty.
type itermDriver struct{}

func (itermDriver) Name() string { return "iterm2" }

const itermEnumScript = `tell application "iTerm"
  set US to (ASCII character 31)
  set out to ""
  repeat with w in windows
    repeat with t in tabs of w
      repeat with s in sessions of t
        set ttyv to ""
        try
          set ttyv to (tty of s) as text
        end try
        set nm to ""
        try
          set nm to (name of s) as text
        end try
        set out to out & (id of s) & US & ttyv & US & "" & US & nm & linefeed
      end repeat
    end repeat
  end repeat
  return out
end tell`

func (itermDriver) Surfaces() []Terminal {
	out, err := osaOutput(itermEnumScript)
	if err != nil {
		return nil
	}
	return parseSurfaces(out)
}

const itermFocusScript = `on run argv
  set wantId to item 1 of argv
  tell application "iTerm"
    repeat with w in windows
      repeat with t in tabs of w
        repeat with s in sessions of t
          if (id of s) is wantId then
            select w
            tell t to select
            tell s to select
            activate
            return
          end if
        end repeat
      end repeat
    end repeat
  end tell
end run`

func (itermDriver) Focus(id string) error {
	return osaRun(itermFocusScript, id)
}

// argv: 1=working directory, 2=command to run.
const itermResumeScript = `on run argv
  set theCwd to item 1 of argv
  set theCmd to item 2 of argv
  tell application "iTerm"
    activate
    if (count of windows) = 0 then
      create window with default profile
    else
      tell current window to create tab with default profile
    end if
    tell current session of current window
      write text "cd " & quoted form of theCwd & " && " & theCmd
    end tell
  end tell
end run`

func (itermDriver) OpenResume(cwd, sid string) error {
	return osaRun(itermResumeScript, cwd, resumeCommand(sid))
}
