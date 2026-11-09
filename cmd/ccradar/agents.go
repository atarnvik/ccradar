package main

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Agent is a background Claude Code session (`claude --bg`), as listed by
// `claude agents` and enriched from its job state and transcript.
type Agent struct {
	ID        string // short id that `claude attach` takes
	SessionID string
	CWD       string
	Name      string
	State     string // blocked, working, done, …
	Needs     string // the question it is waiting on you for
	Detail    string // one-line progress
	Result    string // its final output, once done
	Intent    string // what it was asked to do
	UpdatedAt time.Time
	Model     string
	Branch    string
	LastReply string
	sessionDetails
}

const (
	agentsTimeout = 10 * time.Second
	// agentsPollIdle is how often the agent list is refreshed while another tab
	// is shown (for the tab count and "needs you" notifications).
	agentsPollIdle = 15 * time.Second
)

func jobsDir() string { return envOr("CLAUDE_JOBS_DIR", filepath.Join(homeDir(), ".claude", "jobs")) }

// needsYou reports whether the agent is waiting on the user.
func (a Agent) needsYou() bool { return a.State == "blocked" }

type agentsMsg []Agent

func loadAgentsCmd() tea.Cmd { return func() tea.Msg { return agentsMsg(loadAgents()) } }

// loadAgents lists background sessions via `claude agents --json --all`, the
// supported interface, and adds what the job state file and transcript know.
// Any failure (no claude, an older version without agents) yields none.
func loadAgents() []Agent {
	if demoMode() {
		return demoAgents()
	}
	ctx, cancel := context.WithTimeout(context.Background(), agentsTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, findBin(), "agents", "--json", "--all").Output()
	if err != nil {
		return nil
	}
	agents := parseAgents(out)
	for i := range agents {
		enrichAgent(&agents[i])
	}
	sortAgents(agents)
	return agents
}

// parseAgents keeps the background entries of `claude agents --json`, which
// also lists interactive sessions.
func parseAgents(out []byte) []Agent {
	var raw []struct {
		ID        string `json:"id"`
		Kind      string `json:"kind"`
		SessionID string `json:"sessionId"`
		CWD       string `json:"cwd"`
		Name      string `json:"name"`
		State     string `json:"state"`
		StartedAt int64  `json:"startedAt"`
	}
	if json.Unmarshal(out, &raw) != nil {
		return nil
	}
	var agents []Agent
	for _, r := range raw {
		if r.Kind != "background" || r.ID == "" || !underFilter(r.CWD) {
			continue
		}
		agents = append(agents, Agent{
			ID: r.ID, SessionID: r.SessionID, CWD: r.CWD, Name: r.Name, State: r.State,
			UpdatedAt: time.UnixMilli(r.StartedAt),
		})
	}
	return agents
}

// jobState is the part of ~/.claude/jobs/<id>/state.json ccradar shows.
type jobState struct {
	Detail    string `json:"detail"`
	Needs     string `json:"needs"`
	Intent    string `json:"intent"`
	UpdatedAt string `json:"updatedAt"`
	Output    struct {
		Result string `json:"result"`
	} `json:"output"`
	Children []struct {
		ID   string `json:"id"`
		Href string `json:"href"`
		Kind string `json:"kind"`
	} `json:"children"`
}

var prHref = regexp.MustCompile(`^https://github\.com/([^/]+/[^/]+)/pull/(\d+)$`)

// enrichAgent fills in the job state (needs, detail, result, PRs) and the
// transcript (model, branch, last reply, recap, cost).
func enrichAgent(a *Agent) {
	if a.SessionID != "" {
		if matches, _ := filepath.Glob(filepath.Join(projectsDir(), "*", a.SessionID+".jsonl")); len(matches) > 0 {
			tm := scanTranscriptCached(matches[0])
			a.Model, a.Branch, a.LastReply = tm.model, tm.branch, tm.reply
			a.sessionDetails = tm.details
			a.Name = firstNonEmpty(a.Name, tm.details.Name, tm.title)
			if tm.last.After(a.UpdatedAt) {
				a.UpdatedAt = tm.last
			}
		}
	}
	b, err := os.ReadFile(filepath.Join(jobsDir(), a.ID, "state.json"))
	if err != nil {
		return
	}
	var js jobState
	if json.Unmarshal(b, &js) != nil {
		return
	}
	a.Needs, a.Detail, a.Result, a.Intent = js.Needs, js.Detail, js.Output.Result, js.Intent
	if t, err := time.Parse(time.RFC3339Nano, js.UpdatedAt); err == nil && t.After(a.UpdatedAt) {
		a.UpdatedAt = t
	}
	// The job's PR children are the authoritative list; keep any the
	// transcript linked too, with the job's last.
	for _, c := range js.Children {
		if m := prHref.FindStringSubmatch(c.Href); c.Kind == "pr" && m != nil {
			n, _ := strconv.Atoi(m[2])
			a.addPR(prLink{URL: c.Href, Repo: m[1], Number: n})
		}
	}
}

// addPR appends l unless the agent already has it.
func (a *Agent) addPR(l prLink) {
	for _, p := range a.PRs {
		if p.URL == l.URL {
			return
		}
	}
	a.PRs = append(append([]prLink(nil), a.PRs...), l) // copy: PRs may be shared with the scan cache
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// agentRank orders the tab: agents waiting on you, then working, then the rest.
func agentRank(state string) int {
	switch state {
	case "blocked":
		return 0
	case "working":
		return 1
	case "done":
		return 3
	}
	return 2
}

func sortAgents(as []Agent) {
	sort.SliceStable(as, func(i, j int) bool {
		if ri, rj := agentRank(as[i].State), agentRank(as[j].State); ri != rj {
			return ri < rj
		}
		return as[i].UpdatedAt.After(as[j].UpdatedAt)
	})
}

// agentGroup is the divider an agent's row sits under.
func agentGroup(state string) string {
	switch agentRank(state) {
	case 0:
		return "needs you"
	case 1:
		return "working"
	case 3:
		return "done"
	}
	return "other"
}

// agentStateText is the fixed-width (7-col) state label, like statusText.
func agentStateText(state string) (string, lipgloss.Style) {
	switch state {
	case "blocked":
		return "◆ needs", styBusy
	case "working":
		return "● work ", styIdle
	case "done":
		return "✓ done ", styDim
	}
	return statusText(state)
}

// attachCommand is the shell command that opens a background session.
func attachCommand(id string) string { return "claude attach " + shellQuote(id) }

// observeAgents returns a notification for each agent that newly needs you.
// The first load only records states, so starting ccradar doesn't replay them.
func (m *model) observeAgents(next []Agent) []tea.Cmd {
	var cmds []tea.Cmd
	cur := make(map[string]string, len(next))
	for _, a := range next {
		cur[a.ID] = a.State
		if prev, seen := m.prevAgentState[a.ID]; m.agentsLoaded && m.notify && a.needsYou() && (!seen || prev != a.State) {
			body := firstNonEmpty(a.Needs, a.Detail, "waiting for your reply")
			cmds = append(cmds, notifyCmd("Agent needs you · "+firstNonEmpty(a.Name, a.ID), body, ""))
		}
	}
	m.prevAgentState = cur
	return cmds
}
