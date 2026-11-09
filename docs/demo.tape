# Regenerate with:  vhs docs/demo.tape   (from the repo root)
Output docs/demo.gif

Set Shell "zsh"
Set FontSize 15
Set Width 1400
Set Height 700
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 90ms

# Build the demo binary off-camera, with a clean config and cache so your own
# preferences don't show up in the recording.
Hide
Type "rm -rf /tmp/ccdemo && mkdir -p /tmp/ccdemo && go build -o /tmp/ccdemo/ccradar ./cmd/ccradar && export PATH=/tmp/ccdemo:$PATH CCRADAR_DEMO=1 XDG_CONFIG_HOME=/tmp/ccdemo/config CCRADAR_CACHE_DIR=/tmp/ccdemo/cache && clear"
Enter
Sleep 8s
Show

# Every live session, grouped by directory.
Type "ccradar"
Enter
Sleep 2.5s

# The preview pane: recap, PRs, background shells, last exchange.
Type "p"
Sleep 1.5s
Down@700ms 2
Sleep 2.5s
Down@700ms 2
Sleep 2s

# Search by PR number.
Type "/"
Sleep 400ms
Type "#405"
Sleep 2s
Escape
Sleep 800ms

# Past sessions, ready to resume.
Tab
Sleep 1.5s
Down@700ms 1
Sleep 2.5s

# Worktrees whose PRs are all merged.
Type "W"
Sleep 2.5s
Escape
Sleep 800ms

Type "q"
Sleep 800ms
