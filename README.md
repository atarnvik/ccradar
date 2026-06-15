# ccradar

A terminal dashboard to find, switch to, and resume your **Claude Code** sessions
across **Ghostty** tabs.

If you run lots of Claude Code sessions in different tabs/projects, `ccradar` gives
you one screen to see them all, jump straight to the right native tab, and bring
old sessions back to life — no tmux, no Ghostty fork. It reads Claude Code's own
local data and drives Ghostty through its AppleScript API. No daemon, no network.

## Features

- **Active view** — every live session, grouped by directory. `enter` focuses its
  real Ghostty tab (even when it's an inactive split pane next to logs/git).
- **Detached section** — live sessions with no open tab (closed tab / tmux / ssh)
  are shown separately with their pid; `x` cleans up a leftover.
- **Historical view** — past sessions reconstructed from transcripts. `enter` opens
  a new tab and runs `claude --resume <id>` in the original directory; `c` copies
  the command instead.
- Live auto-refresh, status/age at a glance, fuzzy directory grouping.

## Install

```sh
go install github.com/atarnvik/ccradar@latest
ccradar
```

Or build from source:

```sh
git clone https://github.com/atarnvik/ccradar.git
cd ccradar
go build -o ccradar . && ./ccradar
```

## Requirements

- **macOS** with **[Ghostty](https://ghostty.org)** (uses its AppleScript API;
  tested on 1.3.1).
- **[Claude Code](https://claude.com/claude-code)** CLI (`claude`) for resume.
- **Go** 1.24+ to install/build.
- First run triggers a one-time macOS **Automation** permission prompt — allow
  your terminal to control Ghostty.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`↓` or `j`/`k` | move |
| `tab` (or `1`/`2`) | switch Active / Historical |
| `enter` | Active: focus tab · Historical: resume in a new tab |
| `c` | Historical: copy `cd <dir> && claude --resume <id>` to clipboard |
| `x` | Active: kill a detached (no-tab) session (press twice to confirm) |
| `r` | refresh · `q` quit |

## How it works

| Source | Used for |
| --- | --- |
| `~/.claude/sessions/<pid>.json` | live registry: pid, cwd, status, heartbeat |
| `~/.claude/projects/*/<sid>.jsonl` | session title (`ai-title`) + history |
| `osascript` → Ghostty | enumerate terminal surfaces, focus / open tabs |

A session is paired to a Ghostty surface by **working directory + title** (Ghostty
sets each surface's title to the Claude session title), then `focus` brings that
native tab/pane to the front. Resuming uses Ghostty's `new tab` with an initial
working directory and command.

Sessions are classified by tab reachability: **open** (focusable), **detached**
(live but no tab), **historical** (no process). Heartbeat isn't used for liveness —
idle sessions stop heartbeating — so liveness is the live pid plus the tab match.

## Limitations

- macOS + Ghostty only (it's built on Ghostty's AppleScript API).
- Matching is by cwd + title; a brand-new session shows up once it has a title
  (usually seconds). When Ghostty exposes terminal `tty`/`pid` (newer than 1.3.1),
  matching can become exact — see `enumScript` in `ghostty.go`.

## License

MIT — see [LICENSE](LICENSE).
