# Regenerate with:  vhs docs/demo.tape   (from the repo root)
Output docs/demo.gif

Set Shell "zsh"
Set FontSize 16
Set Width 1000
Set Height 640
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms

# Build the demo binary off-camera.
Hide
Type "mkdir -p /tmp/ccdemo && go build -o /tmp/ccdemo/ccradar . && export PATH=/tmp/ccdemo:$PATH && export CCRADAR_DEMO=1 && clear"
Enter
Sleep 6s
Show

# Launch the dashboard.
Type "ccradar"
Enter
Sleep 2.5s

# Move through the active sessions.
Down@600ms 4
Sleep 1s

# Fuzzy search across directory + title.
Type "/"
Sleep 500ms
Type "web"
Sleep 1.8s
Escape
Sleep 800ms

# Toggle sort: alphabetical <-> last-active.
Type "s"
Sleep 1.6s
Type "s"
Sleep 800ms

# Switch to the Historical view and browse.
Tab
Sleep 1.6s
Down@650ms 2
Sleep 1.4s

# Back to Active, then quit.
Tab
Sleep 1.4s
Type "q"
Sleep 1s
