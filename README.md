# ccradar

A dashboard for your Claude Code sessions. See every session across your
terminal tabs, jump to the one you need, and pick up old ones where you left off.

![ccradar demo](docs/demo.gif)

## What you get

- **All your live sessions in one list**, grouped by directory, with status,
  last activity, model, and how much context each one is using. Sessions
  waiting on you are highlighted and counted at the top.
- **Jump to a session's tab** with `enter`, including split panes.
- **Resume past sessions** from the Historical tab. `enter` opens a new tab
  running `claude --resume` in the right directory.
- **A preview pane** (`p`) with Claude's latest recap, the session's pull
  requests, background shells, and the last prompt and reply.
- **Pull request status** on every session that opened a PR: open, draft,
  merged or closed, with CI checks. `o` opens them in your browser.
- **Notifications** when a session finishes or needs your input. Click one to
  jump to that tab.
- **Search** by directory, title or PR number (`/`), or describe a past session
  in your own words and let Claude find it (`?`).
- **Background shells** each session left running, like a dev server on :3000.
- **Worktree cleanup** (`W`): lists git worktrees whose pull requests are all
  merged or closed, and removes them for you.
- **Devcontainer sessions** next to your local ones.

Works with Ghostty, iTerm2, Terminal.app and Rex on macOS.

## Install

```sh
brew install atarnvik/tap/ccradar
```

or with Go 1.24+:

```sh
go install github.com/atarnvik/ccradar/cmd/ccradar@latest
```

## Usage

```sh
ccradar                           # all your sessions
ccradar ~/src/app                 # sessions in this directory and below
ccradar --docker my-devcontainer  # add sessions from a container
ccradar --popup                   # quit after jumping, for a hotkey popup
```

The first time you jump to a tab, macOS asks to let your terminal control the
terminal app. Allow it once.

## Keys

| Key | Action |
| --- | --- |
| `↑` `↓` / `j` `k` | move |
| `tab`, `1`, `2` | switch between Active and Historical |
| `enter` | jump to the session's tab, or resume a past session |
| `p` | show or hide the preview pane |
| `[` `]` | make the preview wider or narrower |
| `/` | search by directory, title or `#123` |
| `?` | describe a past session and let Claude find it |
| `o` | open the session's open PRs |
| `O` | pick from all of the session's PRs |
| `c` | copy the resume command |
| `x` | stop a session that has no tab (press twice) |
| `W` | list stale worktrees; `enter` removes one, `a` removes all (press twice) |
| `s` | sort by last activity or by name |
| `n` | turn notifications on or off |
| `$` | show what past sessions cost |
| `r` | refresh |
| `q` | quit |

ccradar remembers your sort, preview, notification and cost choices between runs.

## Pull requests and worktrees

PR status and worktree cleanup use your [`gh`](https://cli.github.com) login.
If you're logged into more than one GitHub account, each PR is looked up with an
account that can see it.

Worktree cleanup runs `git worktree remove`, so git keeps any worktree with
uncommitted changes, and the branch stays. Worktrees a live session is working
in are left out of the list.

## Devcontainers

If you run Claude Code inside a container, add it with `--docker` (repeatable):

```sh
ccradar --docker my-devcontainer
ccradar --docker my-devcontainer=/root/.claude   # if Claude's data lives elsewhere
```

Container sessions get their own section. `enter` jumps to the session's tab,
or opens a new one that runs `docker exec` and resumes the session as the
container's user.

## Settings

| Variable | Effect |
| --- | --- |
| `CCRADAR_TERM` | force a terminal: `ghostty`, `iterm`, `terminal` or `rex` |
| `CCRADAR_FIND_MODEL` | model for `?` (default `haiku`) |
| `CCRADAR_DOCKER_SHELL` | shell for resuming in a container (default `bash -lic`) |
| `CCRADAR_NO_PR_STATUS=1` | turn off PR status and worktree lookups |
| `CCRADAR_NO_UPDATE_CHECK=1` | turn off the check for new versions |

## How it works

ccradar reads the session files Claude Code keeps in `~/.claude`, and finds
and focuses tabs through your terminal's AppleScript API (Rex through its
`rex` CLI). Notifications use
[`terminal-notifier`](https://github.com/julienXX/terminal-notifier), which the
Homebrew install includes; with `go install`, run
`brew install terminal-notifier`.

## Build from source

```sh
git clone https://github.com/atarnvik/ccradar.git
cd ccradar
go build -o ccradar ./cmd/ccradar && ./ccradar
```

To re-record the demo GIF, run `vhs docs/demo.tape`.

## License

MIT. See [LICENSE](LICENSE).
