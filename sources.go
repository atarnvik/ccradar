package main

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// A Source is a place ccradar reads Claude sessions from: the local machine, or
// a Docker container (a devcontainer). Local is always present; docker sources
// are added with --docker at startup.
type srcKind int

const (
	srcLocal srcKind = iota
	srcDocker
)

type Source struct {
	kind      srcKind
	label     string // shown in the UI ("" for local)
	selector  string // docker: container name/id or compose service
	claudeDir string // docker: path to ~/.claude inside the container
}

// extraSources holds docker sources parsed from the command line.
var extraSources []Source

// allSources is local first, then any --docker sources.
func allSources() []Source {
	return append([]Source{{kind: srcLocal}}, extraSources...)
}

// parseDockerSource parses "--docker <sel>" or "--docker <sel>=/path".
func parseDockerSource(arg string) Source {
	sel, dir := arg, "/home/vscode/.claude"
	if i := strings.IndexByte(arg, '='); i >= 0 {
		sel, dir = arg[:i], arg[i+1:]
	}
	return Source{kind: srcDocker, label: sel, selector: sel, claudeDir: dir}
}

// resolveContainer maps a selector to a concrete running container id, trying a
// container name, then a compose service label, then the selector as an id.
func resolveContainer(selector string) string {
	try := func(args ...string) string {
		out, err := exec.Command("docker", args...).Output()
		if err != nil {
			return ""
		}
		if id := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); id != "" {
			return id
		}
		return ""
	}
	if id := try("ps", "-q", "-f", "name="+selector); id != "" {
		return id
	}
	if id := try("ps", "-q", "-f", "label=com.docker.compose.service="+selector); id != "" {
		return id
	}
	// Assume it's already a valid id/name; docker exec will fail gracefully if not.
	return selector
}

// dockerShell is the in-container shell used to launch claude on resume. An
// interactive login shell (-lic) so it sources ~/.bashrc, where devcontainers
// usually put claude's PATH (nvm / ~/.local/bin / the native installer). Override
// with CCRADAR_DOCKER_SHELL (e.g. "zsh -lic") if your setup differs.
func dockerShell() string {
	if v := os.Getenv("CCRADAR_DOCKER_SHELL"); strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return "bash -lic"
}

// resumeInvocation returns (cwd, command) to open in a new tab to resume a
// session. Local runs claude directly in cwd; a container session runs it via
// docker exec (in a host tab, so it stays focusable), with -w for the in-
// container dir and an interactive login shell so claude is on PATH.
func resumeInvocation(cwd, container, sid string) (string, string) {
	if container != "" {
		return homeDir(), "docker exec -it -w " + shellQuote(cwd) + " " + shellQuote(container) +
			" " + dockerShell() + " " + shellQuote("claude --resume "+sid)
	}
	return cwd, resumeCommand(sid)
}

// resumeClipboard is the paste-able form of the resume command.
func resumeClipboard(cwd, container, sid string) string {
	if container != "" {
		_, cmd := resumeInvocation(cwd, container, sid)
		return cmd
	}
	return "cd " + shellQuote(cwd) + " && " + resumeCommand(sid)
}

// dockerKill sends SIGTERM to a session's process inside its container.
func dockerKill(container string, pid int) error {
	if container == "" || pid <= 0 {
		return nil
	}
	return exec.Command("docker", "exec", container, "kill", "-TERM", strconv.Itoa(pid)).Run()
}

// dockerScript extracts, per session, a US-delimited row: pid, sessionId, cwd,
// status, updatedAt, alive, title, model, branch, lastPrompt. It uses only
// POSIX sh + grep/sed/tail and reads only the append-only JSON/JSONL (never the
// sqlite db), so it can't disturb Claude's storage.
func dockerScript(claudeDir string) string {
	return `CL=` + shellQuote(claudeDir) + `
US=$(printf '\037')
for f in "$CL"/sessions/*.json; do
  [ -e "$f" ] || continue
  pid=$(sed -n 's/.*"pid":\([0-9][0-9]*\).*/\1/p' "$f" | head -1)
  sid=$(sed -n 's/.*"sessionId":"\([^"]*\)".*/\1/p' "$f" | head -1)
  cwd=$(sed -n 's/.*"cwd":"\([^"]*\)".*/\1/p' "$f" | head -1)
  st=$(sed -n 's/.*"status":"\([^"]*\)".*/\1/p' "$f" | head -1)
  up=$(sed -n 's/.*"updatedAt":\([0-9][0-9]*\).*/\1/p' "$f" | head -1)
  al=0; { [ -n "$pid" ] && kill -0 "$pid" 2>/dev/null; } && al=1
  t=$(ls "$CL"/projects/*/"$sid".jsonl 2>/dev/null | head -1)
  ti=; mo=; br=; pr=
  if [ -n "$t" ]; then
    ti=$(grep '"type":"ai-title"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"aiTitle":"\([^"]*\)".*/\1/p')
    mo=$(grep '"type":"assistant"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"model":"\([^"]*\)".*/\1/p')
    br=$(grep -o '"gitBranch":"[^"]*"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"gitBranch":"\([^"]*\)".*/\1/p')
    pr=$(grep '"type":"last-prompt"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"lastPrompt":"\([^"]*\)".*/\1/p' | cut -c1-200)
  fi
  printf '%s%s%s%s%s%s%s%s%s%s%s%s%s%s%s%s%s%s%s\n' \
    "$pid" "$US" "$sid" "$US" "$cwd" "$US" "$st" "$US" "$up" "$US" "$al" "$US" "$ti" "$US" "$mo" "$US" "$br" "$US" "$pr"
done`
}

// dockerSessions reads live sessions from a container via one `docker exec`.
func dockerSessions(src Source) []Session {
	cid := resolveContainer(src.selector)
	if cid == "" {
		return nil
	}
	out, err := exec.Command("docker", "exec", cid, "sh", "-c", dockerScript(src.claudeDir)).Output()
	if err != nil {
		return nil // docker missing / container down / exec failed → contribute nothing
	}
	return parseDockerSessions(string(out), src.label, cid)
}

// dockerHistoryScript emits, per recent transcript (newest first, capped): mtime,
// sessionId, cwd, title, model, branch, lastPrompt — US-delimited.
func dockerHistoryScript(claudeDir string) string {
	return `CL=` + shellQuote(claudeDir) + `
US=$(printf '\037')
ls -t "$CL"/projects/*/*.jsonl 2>/dev/null | head -60 | while IFS= read -r t; do
  [ -e "$t" ] || continue
  sid=$(basename "$t" .jsonl)
  mt=$(stat -c %Y "$t" 2>/dev/null || echo 0)
  cwd=$(grep -m1 '"cwd"' "$t" 2>/dev/null | sed -n 's/.*"cwd":"\([^"]*\)".*/\1/p')
  [ -n "$cwd" ] || continue
  ti=$(grep '"type":"ai-title"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"aiTitle":"\([^"]*\)".*/\1/p')
  mo=$(grep '"type":"assistant"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"model":"\([^"]*\)".*/\1/p')
  br=$(grep -o '"gitBranch":"[^"]*"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"gitBranch":"\([^"]*\)".*/\1/p')
  pr=$(grep '"type":"last-prompt"' "$t" 2>/dev/null | tail -1 | sed -n 's/.*"lastPrompt":"\([^"]*\)".*/\1/p' | cut -c1-200)
  printf '%s%s%s%s%s%s%s%s%s%s%s%s%s\n' "$mt" "$US" "$sid" "$US" "$cwd" "$US" "$ti" "$US" "$mo" "$US" "$br" "$US" "$pr"
done`
}

// dockerHistory reads recent past sessions from a container, excluding live ones.
func dockerHistory(src Source, activeIDs map[string]bool) []HistEntry {
	cid := resolveContainer(src.selector)
	if cid == "" {
		return nil
	}
	out, err := exec.Command("docker", "exec", cid, "sh", "-c", dockerHistoryScript(src.claudeDir)).Output()
	if err != nil {
		return nil
	}
	return parseDockerHistory(string(out), src.label, cid, activeIDs)
}

// parseDockerHistory turns the history script's rows into HistEntries.
func parseDockerHistory(out, label, container string, activeIDs map[string]bool) []HistEntry {
	var hs []HistEntry
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\x1f")
		if len(p) < 7 {
			continue
		}
		sid := p[1]
		if activeIDs[sid] {
			continue // shown in the Active view already
		}
		mt, _ := strconv.ParseInt(p[0], 10, 64)
		hs = append(hs, HistEntry{
			SessionID: sid, CWD: p[2], Title: p[3], Model: p[4], Branch: p[5],
			LastPrompt: p[6], ModAt: time.Unix(mt, 0), Source: label, Container: container,
		})
	}
	return hs
}
func parseDockerSessions(out, label, container string) []Session {
	var sessions []Session
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		p := strings.Split(line, "\x1f")
		if len(p) < 10 {
			continue
		}
		if p[5] != "1" { // alive flag
			continue
		}
		pid, _ := strconv.Atoi(p[0])
		up, _ := strconv.ParseInt(p[4], 10, 64)
		sessions = append(sessions, Session{
			PID:        pid,
			SessionID:  p[1],
			CWD:        p[2],
			Status:     p[3],
			UpdatedAt:  up,
			Title:      p[6],
			Model:      p[7],
			Branch:     p[8],
			LastPrompt: p[9],
			Source:     label,
			Container:  container,
		})
	}
	return sessions
}
