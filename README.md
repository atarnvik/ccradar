# ccradar

A terminal dashboard to find, switch to, and resume your **Claude Code** sessions
across **Ghostty**, **iTerm2**, **Terminal.app**, and **Rex** tabs.

If you run lots of Claude Code sessions in different tabs/projects, `ccradar` gives
you one screen to see them all, jump straight to the right native tab, and bring
old sessions back to life — no tmux, no fork. It reads Claude Code's own local
data and drives your terminal through its AppleScript API. No daemon; the only
network calls are the update check and PR status lookups through your `gh`.

![ccradar demo](docs/demo.gif)

## Features

- **Works with Ghostty, iTerm2, Terminal.app, and Rex** — auto-detected from
  `$TERM_PROGRAM`. iTerm2/Terminal/Rex match sessions to tabs *exactly* by tty;
  Ghostty matches by directory + title (and upgrades to tty on versions newer
  than 1.3.1).
- **Active view** — every live session, grouped by directory. `enter` focuses its
  real tab (even when it's an inactive iTerm/Ghostty split pane next to logs/git).
- **Detached section** — live sessions with no open tab (closed tab / tmux / ssh)
  are shown separately with their pid; `x` cleans up a leftover.
- **Historical view** — past sessions reconstructed from transcripts. `enter` opens
  a new tab and runs `claude --resume <id>` in the original directory; `c` copies
  the command instead.
- **Preview pane** — a right rail (off by default; press `p`) showing the selected
  session's directory, git branch, model/status, Claude's latest **recap** of where
  the session stands (the "while you were away" summary), and its last prompt +
  last reply, so you can tell sessions apart at a glance. Auto-hidden on narrow
  terminals. It takes about a third of a wide terminal; move the divider with
  `[` (wider preview) and `]` (narrower), and the width is remembered.
- **Background shells** — live sessions that left background commands running
  (dev servers, watchers, long test runs) show `⚙N`, and the preview lists what
  each one is and how long it has been up, so you know which tab is holding
  port 3000 before you close it.
- **Cost per past session** (off by default; press `$` to toggle) — the
  Historical view shows what each session cost (and the preview its lines
  added/removed), from the totals Claude Code records when a session exits.
- **PR badges** — every session that opened or worked on a pull request shows
  its latest PR number, colored by state (green open, dim draft, purple merged,
  red closed) and followed by a glyph: CI for open PRs (✓ passing, ✗ failing,
  • running), `⑂` merged, `×` closed. The preview lists the session's recent
  PRs the same way with their titles. A session with several PRs shows the
  latest plus a count (`#5752 ✓ +2`). `o` opens the session's PRs that are still
  open, one browser tab each (or the latest if none are), and `O` lists all of
  them to open one (`enter`) or all (`a`). Statuses come from one batched `gh api graphql` call
  (refreshed every 5 minutes; merged/closed PRs are fetched once) using your
  existing `gh` login. With several accounts logged in (`gh auth login` again
  adds one), each PR is fetched with an account that can see it, so a work
  org and personal repos both get statuses. Without `gh` you still get the numbers; set
  `CCRADAR_NO_PR_STATUS=1` to skip the lookups.
- **Starts warm** — transcript scans and PR statuses are cached in
  `~/Library/Caches/ccradar`, so a fresh launch (say, a popup on a hotkey) shows
  history and PR badges at once and only refreshes what changed. Safe to delete.
- **Your session names win** — a name you gave a session is shown instead of the
  auto-generated title (both are searchable).
- **Notifications** — get a native macOS notification the moment a session leaves
  **busy** — either finishing (**idle**) or pausing for your input (**waiting**).
  **Click the notification to jump straight to that session's tab.** Toggle with
  `n`; the choice is remembered. (Click-to-jump needs `terminal-notifier`, which
  the Homebrew install pulls in automatically.)
- **Fuzzy search** (`/`) across directory + session title, and a **sort toggle**
  (`s`) between last-active (the default) and alphabetical; the choice is
  remembered.
- **Find a session from a loose description** (`?`): describe it the way you
  remember it ("the one where the evals broke on search tools") and Claude picks
  the matching past sessions, best first, each with a one-line reason. It sends
  only a compact catalog of recent sessions (directory, age, branch, title, a few
  of your prompts) — never full transcripts — to a fast model (`haiku`; override
  with `CCRADAR_FIND_MODEL`) via headless `claude -p`, so it uses your existing
  Claude Code login. Each lookup takes a few seconds and isn't saved to history.
- Each row shows the **model** in use; live auto-refresh, status/age at a glance,
  directory grouping.
- **Update check** — on launch ccradar checks GitHub for the latest release and,
  if newer, shows `⬆ vX.Y.Z` plus the right upgrade command for how you installed
  (`brew upgrade` vs `go install`). Best-effort and disableable with
  `CCRADAR_NO_UPDATE_CHECK=1`.

## Install

**Homebrew** (no Go needed):

```sh
brew install atarnvik/tap/ccradar
```

**Go:**

```sh
go install github.com/atarnvik/ccradar/cmd/ccradar@latest
```

Then:

```sh
ccradar                          # all local sessions
ccradar ~/src/app                # only sessions in that directory and its subdirectories
ccradar --docker my-devcontainer # local + sessions running inside a container (see below)
ccradar --version                # print the installed version
```

Or build from source:

```sh
git clone https://github.com/atarnvik/ccradar.git
cd ccradar
go build -o ccradar ./cmd/ccradar && ./ccradar
```

## Requirements

- **macOS** with a supported terminal: **[Ghostty](https://ghostty.org)** (tested
  on 1.3.1), **iTerm2**, **Terminal.app**, or **[Rex](https://www.superlogical.com/rex)**. The terminal is auto-detected from
  `$TERM_PROGRAM`; override with `CCRADAR_TERM=ghostty|iterm|terminal|rex`.
- **[Claude Code](https://claude.com/claude-code)** CLI (`claude`) for resume and
  the `?` finder.
- **Go** 1.24+ to install/build.
- First run triggers a one-time macOS **Automation** permission prompt — allow
  your terminal to control the terminal app. The first notification may likewise prompt to
  allow notifications.
- [`terminal-notifier`](https://github.com/julienXX/terminal-notifier) for
  reliable notification banners — **installed automatically with the Homebrew
  cask**. If you installed via `go install`, add it with
  `brew install terminal-notifier`. Without it, ccradar falls back to `osascript`,
  whose notifications appear as **Script Editor**; if those only land silently in
  Notification Center, set Script Editor to **Banners/Alerts** and **Deliver
  Prominently** in *System Settings → Notifications*.

## Keys

| Key | Action |
| --- | --- |
| `↑`/`↓` or `j`/`k` | move |
| `tab` (or `1`/`2`) | switch Active / Historical |
| `enter` | Active: focus tab · Historical: resume in a new tab |
| `/` | fuzzy search (directory + title); `enter` keep, `esc` clear |
| `?` | find a past session by description (asks Claude); `esc` back to the full list |
| `s` | toggle sort: last-active (default) ↔ alphabetical (remembered across runs) |
| `p` | toggle the preview pane |
| `n` | toggle busy→idle notifications (remembered across runs) |
| `c` | Historical: copy `cd <dir> && claude --resume <id>` to clipboard |
| `x` | Active: kill a detached (no-tab) session (press twice to confirm) |
| `r` | refresh · `q` quit |

## How it works

| Source | Used for |
| --- | --- |
| `~/.claude/sessions/<pid>.json` | live registry: pid, cwd, status, heartbeat |
| `~/.claude/projects/*/<sid>.jsonl` | session title (`ai-title`) + model + history |
| `ps -o tty=` | the session process's controlling tty |
| `osascript` → your terminal | enumerate surfaces, focus / open tabs |

Each terminal has a small driver (`cmd/ccradar/term_*.go`) behind
a common interface. A session is paired to a surface **exactly by tty** on
iTerm2/Terminal.app (`ps` gives the pid's tty; the terminal reports each
tab/session's tty). On Ghostty 1.3.1, which doesn't expose a tty, it falls back
to **directory + title** — and upgrades to tty automatically on newer Ghostty.
Rex is driven through its `rex` CLI instead of AppleScript: one `rex do` script
lists every pane in every Rex session with its shell pid, so matching is exact by
tty even for sessions no window is showing.
`focus` brings the matched tab/pane to the front (on Rex, switching the app to its
session); resume opens a new tab (iTerm/Ghostty/Rex) or window (Terminal.app)
running `claude --resume`.

Sessions are classified by tab reachability: **open** (focusable), **detached**
(live but no tab), **historical** (no process). Heartbeat isn't used for liveness —
idle sessions stop heartbeating — so liveness is the live pid plus the tab match.

## Devcontainers

If you run Claude *inside* a container, add it as an extra source (repeatable):

```sh
ccradar --docker my-devcontainer          # container name or compose service
ccradar --docker my-devcontainer=/root/.claude   # if ~/.claude lives elsewhere
```

Both the **Active and Historical** views are read from the container over a
single `docker exec` per refresh (only the append-only JSON/JSONL — never the
sqlite db, so no WAL/lock risk). Sessions are grouped into **sections** — a
`local` section, then one per container (`⬢ <name>`) — each with its usual
directory sub-groups.

Because a container's tty and cwd are namespaced, sessions are matched to a host
tab **by title**, which works because Claude's title escape still reaches the
host Ghostty tab. `enter` focuses that tab, or (if none) opens a new host tab
that `docker exec`s in and runs `claude --resume`; `x` kills inside the
container. Requires `docker` on your PATH.

Resume runs as the session's owning user (inferred from the path —
`/home/vscode/.claude` → `vscode`) with that user's `HOME`, via an interactive
login shell (`bash -lic`) so it picks up `claude` from their `~/.bashrc` (nvm,
`~/.local/bin`, the native installer, etc.). If your container uses a different
shell, set `CCRADAR_DOCKER_SHELL`, e.g. `CCRADAR_DOCKER_SHELL='zsh -lic'`.

## Limitations

- macOS only (built on each terminal's AppleScript API).
- Terminal.app has no scriptable "new tab", so **resume opens a new window** there.
- Ghostty 1.3.1 matches by cwd + title (a brand-new session appears once it has a
  title, usually seconds); iTerm2/Terminal.app match instantly and exactly by tty.

## License

MIT — see [LICENSE](LICENSE).
