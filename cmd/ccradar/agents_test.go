package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

const agentsJSON = `[
 {"id":"aaaa1111","cwd":"/w/app","kind":"background","startedAt":1790000000000,"sessionId":"sid-a","name":"Social topics page","state":"blocked"},
 {"pid":42,"cwd":"/w/app","kind":"interactive","startedAt":1790000000000,"sessionId":"sid-i","name":"app-12","status":"idle"},
 {"id":"bbbb2222","cwd":"/w/app","kind":"background","startedAt":1791000000000,"sessionId":"sid-b","name":"IAM audit","state":"done"},
 {"id":"cccc3333","cwd":"/elsewhere","kind":"background","startedAt":1792000000000,"sessionId":"sid-c","name":"Other","state":"working"}
]`

func TestParseAgentsKeepsBackgroundOnly(t *testing.T) {
	prev := dirFilter
	dirFilter = ""
	t.Cleanup(func() { dirFilter = prev })
	as := parseAgents([]byte(agentsJSON))
	if len(as) != 3 || as[0].ID != "aaaa1111" || as[0].State != "blocked" || as[0].SessionID != "sid-a" {
		t.Fatalf("agents = %+v", as)
	}
	setDirFilter("/w")
	if as := parseAgents([]byte(agentsJSON)); len(as) != 2 {
		t.Errorf("dir filter: %d agents, want 2", len(as))
	}
	if parseAgents([]byte("error: unknown command")) != nil {
		t.Error("non-JSON output should give no agents")
	}
}

func TestLoadAgentsEnrichesFromJobAndTranscript(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	prev := dirFilter
	dirFilter = ""
	t.Cleanup(func() { dirFilter = prev })
	t.Setenv("CCRADAR_DEMO", "")
	t.Setenv("CLAUDE_JOBS_DIR", filepath.Join(dir, "jobs"))
	t.Setenv("CLAUDE_PROJECTS_DIR", filepath.Join(dir, "projects"))

	fake := filepath.Join(dir, "claude")
	script := "#!/bin/sh\nprintf '%s ' \"$@\" > \"" + dir + "/args\"\ncat <<'JSON'\n" + agentsJSON + "\nJSON\n"
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCRADAR_CLAUDE", fake)

	job := `{"state":"blocked","detail":"PRs ready","needs":"Should I close #5709?","intent":"rebase #5705",
	 "updatedAt":"2026-10-02T19:02:23.329Z","output":{"result":"Restacked"},
	 "children":[{"id":"5652","href":"https://github.com/meetyogi/yogi/pull/5652","kind":"pr"},{"id":"x","href":"https://example.com/x","kind":"link"}]}`
	if err := os.MkdirAll(filepath.Join(dir, "jobs", "aaaa1111"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "jobs", "aaaa1111", "state.json"), []byte(job), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTranscript(t, filepath.Join(dir, "projects", "-w-app", "sid-a.jsonl"), time.Now(),
		`{"type":"user","cwd":"/w/app","gitBranch":"stack","timestamp":"2026-10-02T14:00:00.000Z","message":{"role":"user","content":"go"}}`,
		`{"type":"assistant","isSidechain":false,"timestamp":"2026-10-02T15:00:00.000Z","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"done"}]}}`,
		prLine(5652, "meetyogi/yogi"), prLine(5691, "meetyogi/yogi"))

	as := loadAgents()
	if len(as) != 3 {
		t.Fatalf("agents = %+v", as)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	if strings.TrimSpace(string(args)) != "agents --json --all" {
		t.Errorf("args = %q", args)
	}
	a := as[0] // blocked sorts first
	if a.ID != "aaaa1111" || a.Needs != "Should I close #5709?" || a.Detail != "PRs ready" || a.Result != "Restacked" || a.Intent != "rebase #5705" {
		t.Errorf("job state not applied: %+v", a)
	}
	if a.Model != "claude-opus-5-5" || a.Branch != "stack" || a.LastReply != "done" {
		t.Errorf("transcript not applied: %+v", a)
	}
	if want := time.Date(2026, 10, 2, 19, 2, 23, 329_000_000, time.UTC); !a.UpdatedAt.Equal(want) {
		t.Errorf("updatedAt = %v, want the job's %v", a.UpdatedAt, want)
	}
	if len(a.PRs) != 2 || a.PRs[0].Number != 5652 || a.PRs[1].Number != 5691 {
		t.Errorf("PRs = %+v, want transcript PRs with the job's PR deduped", a.PRs)
	}
	if as[1].State != "working" || as[2].State != "done" {
		t.Errorf("order = %s,%s,%s, want blocked, working, done", as[0].State, as[1].State, as[2].State)
	}
}

func TestAgentsTabFlow(t *testing.T) {
	m := model{width: 120, height: 30}
	tm, _ := m.Update(agentsMsg(demoAgents()))
	m = tm.(model)
	if !m.agentsLoaded || len(m.agents) != 3 {
		t.Fatalf("agents not stored: %+v", m.agents)
	}
	if !strings.Contains(m.tabBar(), "Agents (3, 1 needs you)") {
		t.Errorf("tab bar = %q", m.tabBar())
	}

	// tab cycles 1 → 2 → 3 → 1, shift+tab goes back
	for _, want := range []viewKind{viewHistory, viewAgents, viewActive} {
		tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
		m = tm.(model)
		if m.view != want {
			t.Fatalf("tab → view %d, want %d", m.view, want)
		}
	}
	tm, _ = m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	if m = tm.(model); m.view != viewAgents {
		t.Fatalf("shift+tab → view %d, want agents", m.view)
	}

	var kinds []string
	for _, r := range m.rows {
		switch r.kind {
		case rowDivider:
			kinds = append(kinds, "["+r.header+"]")
		case rowAgent:
			kinds = append(kinds, r.agent.State)
		}
	}
	if got := strings.Join(kinds, " "); got != "[needs you] blocked [working] working [done] done" {
		t.Errorf("rows = %s", got)
	}
	if r := m.rows[m.cursor]; r.kind != rowAgent || r.agent.State != "blocked" {
		t.Errorf("cursor should start on the agent that needs you")
	}
}

func TestHiddenFromHistoryIncludesAgents(t *testing.T) {
	m := model{sessions: []Session{{SessionID: "live"}}, agents: []Agent{{ID: "a", SessionID: "bg"}}}
	ids := m.hiddenFromHistory()
	if !ids["live"] || !ids["bg"] || len(ids) != 2 {
		t.Fatalf("hidden = %v", ids)
	}
}

func TestObserveAgentsNotifiesOnNewlyBlocked(t *testing.T) {
	m := &model{notify: true}
	if cmds := m.observeAgents([]Agent{{ID: "a", State: "blocked"}}); len(cmds) != 0 {
		t.Fatal("first load should not notify")
	}
	m.agentsLoaded = true
	if cmds := m.observeAgents([]Agent{{ID: "a", State: "blocked"}}); len(cmds) != 0 {
		t.Error("still blocked: no repeat notification")
	}
	m.observeAgents([]Agent{{ID: "a", State: "working"}})
	if cmds := m.observeAgents([]Agent{{ID: "a", State: "blocked", Needs: "merge?"}}); len(cmds) != 1 {
		t.Error("working → blocked should notify")
	}
	if cmds := m.observeAgents([]Agent{{ID: "a", State: "blocked"}, {ID: "new", State: "blocked"}}); len(cmds) != 1 {
		t.Error("a new agent that needs you should notify")
	}
	m.notify = false
	m.observeAgents([]Agent{{ID: "b", State: "working"}})
	if cmds := m.observeAgents([]Agent{{ID: "b", State: "blocked"}}); len(cmds) != 0 {
		t.Error("notifications off")
	}
}

func TestScanCache(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, p, time.Now().Add(-time.Hour), `{"type":"ai-title","aiTitle":"First"}`)
	if got := scanTranscriptCached(p).title; got != "First" {
		t.Fatalf("title = %q", got)
	}
	writeTranscript(t, p, time.Now(), `{"type":"ai-title","aiTitle":"Second, longer"}`)
	if got := scanTranscriptCached(p).title; got != "Second, longer" {
		t.Fatalf("changed file not rescanned: %q", got)
	}
}
