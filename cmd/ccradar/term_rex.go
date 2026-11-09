package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// rexDriver drives Rex (superlogical.com/rex) through its CLI. Rex reports every
// pane in every session together with the process running in it, so matching is
// exact by tty, and it covers panes in sessions that no window is showing.
// A surface ID is "<session_id> <block_id>".
type rexDriver struct{}

func (rexDriver) Name() string { return "rex" }

const rexBundleID = "com.superlogical.rex"

// rexBin finds the rex CLI. Notification clicks run with a minimal PATH, so fall
// back to where the Rex app installs it.
func rexBin() string {
	if p, err := exec.LookPath("rex"); err == nil {
		return p
	}
	return filepath.Join(homeDir(), "Library", "Application Support", "com.superlogical.rex",
		"RexServer", "Versions", "Current", "Rex Server.app", "Contents", "MacOS", "rex")
}

// rexDo runs a Lua script with `rex do` and returns what it printed (JSON).
func rexDo(script string, args map[string]string) (string, error) {
	argv := []string{"do", "-e", script}
	if args != nil {
		b, _ := json.Marshal(args)
		argv = append(argv, "--args", string(b))
	}
	out, err := exec.Command(rexBin(), argv...).Output()
	return string(out), err
}

// One call lists every placed terminal pane across all sessions with its shell
// pid, foreground cwd, and title. Calls into a session need the script attached
// to it first.
const rexSurfacesLua = `
local T = "com.superlogical.terminal"
local out = {}
local ss = rex.call("session.list") or {sessions = {}}
for _, s in ipairs(ss.sessions or {}) do
  local sid = s.session_id
  rex.session.attach{session_id = sid}
  local bl = rex.session.list_blocks{session_id = sid}
  for _, b in ipairs((bl and bl.blocks) or {}) do
    if b.creator_name == T and b.placement == "placed" then
      local ref = {session_id = sid, block_id = b.block_id}
      local p = rex.block.call(T, "process", ref) or {}
      local t = rex.block.call(T, "title", ref) or {}
      out[#out + 1] = {
        id = sid .. " " .. b.block_id,
        pid = p.child and p.child.pid or 0,
        cwd = p.foreground and p.foreground.cwd or "",
        title = t.title or "",
      }
    end
  end
  rex.session.detach{session_id = sid}
end
return out`

type rexSurface struct {
	ID    string `json:"id"`
	PID   int    `json:"pid"`
	CWD   string `json:"cwd"`
	Title string `json:"title"`
}

func (rexDriver) Surfaces() []Terminal {
	out, err := rexDo(rexSurfacesLua, nil)
	if err != nil {
		return nil // Rex not running
	}
	return parseRexSurfaces(out, procTtys)
}

// parseRexSurfaces decodes the script's output and fills in each pane's tty from
// its shell pid. An empty Lua table prints as {} rather than [].
func parseRexSurfaces(out string, ttys func([]int) map[int]string) []Terminal {
	out = strings.TrimSpace(out)
	if out == "" || out == "{}" {
		return nil
	}
	var surfaces []rexSurface
	if json.Unmarshal([]byte(out), &surfaces) != nil {
		return nil
	}
	pids := make([]int, 0, len(surfaces))
	for _, s := range surfaces {
		if s.PID > 0 {
			pids = append(pids, s.PID)
		}
	}
	byPid := ttys(pids)
	terms := make([]Terminal, 0, len(surfaces))
	for _, s := range surfaces {
		terms = append(terms, Terminal{ID: s.ID, Tty: byPid[s.PID], CWD: s.CWD, Title: s.Title})
	}
	return terms
}

// procTtys looks up the controlling tty of several pids with one ps call.
func procTtys(pids []int) map[int]string {
	ttys := map[int]string{}
	if len(pids) == 0 {
		return ttys
	}
	list := make([]string, len(pids))
	for i, p := range pids {
		list[i] = strconv.Itoa(p)
	}
	out, _ := exec.Command("ps", "-o", "pid=,tty=", "-p", strings.Join(list, ",")).Output()
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[1] == "?" || f[1] == "??" {
			continue
		}
		pid, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		ttys[pid] = "/dev/" + strings.TrimPrefix(f[1], "/dev/")
	}
	return ttys
}

// Focus the pane, then ask the app to show its session and tab. The tab must be
// named: the app keeps its own tab selection, so selecting only the session does
// nothing when ccradar is already in it.
const rexFocusLua = `
local sid, bid = rex.args.session_id, rex.args.block_id
rex.session.attach{session_id = sid}
local _, err = rex.session.focus_block{session_id = sid, block_id = bid}
if err then error(err, 0) end
local d = rex.session.describe_block{session_id = sid, block_id = bid}
rex.client.queue("session.select", {session_id = sid, window_id = d and d.block and d.block.window_id})`

func (rexDriver) Focus(id string) error {
	sid, bid, _ := strings.Cut(id, " ")
	_, err := rexDo(rexFocusLua, map[string]string{"session_id": sid, "block_id": bid})
	_ = exec.Command("open", "-b", rexBundleID).Run() // notification clicks start in the background
	return err
}

// Open a tab in ccradar's own session (or the first session when run outside
// Rex), type the command into its shell, and show it. The tab gets no label:
// an unlabeled tab shows its pane's title, which Claude keeps current.
const rexResumeLua = `
local sid = rex.args.session_id
if sid == "" then
  local ss = rex.call("session.list") or {}
  sid = ss.sessions and ss.sessions[1] and ss.sessions[1].session_id
  if not sid then error("no Rex session", 0) end
end
rex.session.attach{session_id = sid}
local w, err = rex.session.new_window{
  session_id = sid,
  layout = rex.layout.block{
    flavor = "com.superlogical.terminal.shell",
    options = {cwd = rex.args.cwd, initial_input = rex.args.input},
  },
}
if err then error(err, 0) end
pcall(rex.client.queue, "session.select", {session_id = sid, window_id = w.window_id})`

func (rexDriver) OpenResume(cwd, command string) error {
	_, err := rexDo(rexResumeLua, map[string]string{
		"session_id": os.Getenv("REX_SESSION"),
		"cwd":        cwd,
		"input":      command + "\n",
	})
	return err
}
