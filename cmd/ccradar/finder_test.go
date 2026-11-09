package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var findHistory = []HistEntry{
	{SessionID: "sid-old", CWD: "/w/app", Title: "Git worktrees disk usage", ModAt: time.Now().Add(-24 * time.Hour),
		Prompts: []string{"clean up docker"}},
	{SessionID: "sid-evals", CWD: "/w/yogi", Title: "Social topic tools in Ask Yogi evals", Branch: "main",
		ModAt: time.Now().Add(-30 * time.Minute), Prompts: []string{"Wire the social topic tools\ninto the evals", strings.Repeat("x", 500)}},
	{SessionID: "sid-box", CWD: "/workspaces/app", Title: "Container work", Source: "devbox",
		ModAt: time.Now().Add(-2 * time.Hour), LastPrompt: "only the latest"},
	{SessionID: "sid-empty", CWD: "/w/app", ModAt: time.Now()}, // nothing to match on
}

func TestFindCatalog(t *testing.T) {
	catalog, ids := findCatalog(findHistory)
	lines := strings.Split(strings.TrimSpace(catalog), "\n")
	if len(lines) != 3 || len(ids) != 3 {
		t.Fatalf("want 3 lines (empty session skipped), got %q", catalog)
	}
	// newest first, positional ids mapping back to sessions
	if !strings.HasPrefix(lines[0], "s1 | /w/yogi | 30m ago | main | Social topic tools in Ask Yogi evals | ") || ids["s1"] != "sid-evals" {
		t.Errorf("first line = %q, ids = %v", lines[0], ids)
	}
	if !strings.Contains(lines[0], `"Wire the social topic tools into the evals"`) {
		t.Errorf("prompt whitespace not collapsed: %q", lines[0])
	}
	if strings.Contains(lines[0], strings.Repeat("x", findPromptLen+1)) {
		t.Errorf("long prompt not truncated")
	}
	if !strings.Contains(lines[1], "devbox:/workspaces/app") || !strings.Contains(lines[1], `"only the latest"`) || ids["s2"] != "sid-box" {
		t.Errorf("container line = %q (want source prefix and LastPrompt fallback)", lines[1])
	}
}

func TestParseFindOutput(t *testing.T) {
	ids := map[string]string{"s1": "sid-a", "s2": "sid-b"}
	out := `{"type":"result","is_error":false,"result":"…","structured_output":{"matches":[
		{"id":"s2","why":" best "},{"id":"s9","why":"unknown id"},{"id":"s2","why":"dup"},{"id":"s1","why":"second"}]}}`
	hits, err := parseFindOutput([]byte(out), ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits["sid-b"] != (findHit{0, "best"}) || hits["sid-a"] != (findHit{1, "second"}) {
		t.Fatalf("hits = %+v", hits)
	}

	if _, err := parseFindOutput([]byte(`{"is_error":true,"result":"Not logged in\nrun /login"}`), ids); err == nil || err.Error() != "Not logged in" {
		t.Fatalf("is_error: err = %v", err)
	}
	if _, err := parseFindOutput([]byte("garbage"), ids); err == nil {
		t.Fatal("garbage parsed")
	}
}

// askClaude end to end against a stand-in claude that records its arguments
// and stdin, and replies like `claude -p --output-format json`.
func TestAskClaudeWithFakeBinary(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir) // findDir lives under the user cache dir
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache"))
	fake := filepath.Join(dir, "claude")
	script := `#!/bin/sh
printf '%s\n' "$@" > "` + dir + `/args"
cat > "` + dir + `/stdin"
pwd > "` + dir + `/pwd"
echo '{"type":"result","is_error":false,"structured_output":{"matches":[{"id":"s1","why":"evals for social tools"}]}}'
`
	if err := os.WriteFile(fake, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CCRADAR_CLAUDE", fake)
	t.Setenv("CCRADAR_FIND_MODEL", "test-model")

	hits, err := askClaude("the ask yogi evals one", findHistory)
	if err != nil {
		t.Fatal(err)
	}
	if hits["sid-evals"].why != "evals for social tools" || len(hits) != 1 {
		t.Fatalf("hits = %+v", hits)
	}
	args, _ := os.ReadFile(filepath.Join(dir, "args"))
	for _, want := range []string{"-p", "--no-session-persistence", "--strict-mcp-config", "--json-schema", "test-model"} {
		if !strings.Contains("\n"+string(args), "\n"+want+"\n") {
			t.Errorf("args missing %q:\n%s", want, args)
		}
	}
	stdin, _ := os.ReadFile(filepath.Join(dir, "stdin"))
	if !strings.Contains(string(stdin), "Social topic tools in Ask Yogi evals") ||
		!strings.Contains(string(stdin), "Description: the ask yogi evals one") {
		t.Errorf("stdin = %s", stdin)
	}
	pwd, _ := os.ReadFile(filepath.Join(dir, "pwd"))
	if !strings.HasSuffix(strings.TrimSpace(string(pwd)), filepath.Join("ccradar", "find")) {
		t.Errorf("ran in %s, want the fixed finder dir", pwd)
	}

	if _, err := askClaude("x", nil); err == nil {
		t.Error("want an error with no history")
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func press(t *testing.T, m model, keys ...string) (model, tea.Cmd) {
	t.Helper()
	var cmd tea.Cmd
	for _, k := range keys {
		var tm tea.Model
		tm, cmd = m.Update(key(k))
		m = tm.(model)
	}
	return m, cmd
}

func histIDs(m model) []string {
	var ids []string
	for _, r := range m.rows {
		if r.kind == rowHist {
			ids = append(ids, r.hist.SessionID)
		}
	}
	return ids
}

func TestFinderFlow(t *testing.T) {
	m := model{history: append([]HistEntry(nil), findHistory...), width: 120, height: 30}
	m.rebuild()

	// ? from Active jumps to History and opens the prompt; typing doesn't send.
	m, _ = press(t, m, "?")
	if m.view != viewHistory || !m.finding {
		t.Fatalf("view=%v finding=%v", m.view, m.finding)
	}
	m, cmd := press(t, m, "e", "v", "a", "l", "s")
	if m.findQuery != "evals" || m.findBusy || cmd != nil {
		t.Fatalf("typing: query=%q busy=%v", m.findQuery, m.findBusy)
	}
	m, cmd = press(t, m, "enter")
	if !m.findBusy || m.finding || cmd == nil {
		t.Fatalf("enter should send: busy=%v finding=%v cmd=%v", m.findBusy, m.finding, cmd != nil)
	}
	if !strings.Contains(m.View(), "asking Claude") {
		t.Error("no progress shown while asking")
	}

	// A stale reply is ignored; the current one filters History by rank.
	tm, _ := m.Update(findMsg{seq: m.findSeq - 1, hits: map[string]findHit{"sid-old": {0, "stale"}}})
	m = tm.(model)
	if m.findHits != nil {
		t.Fatal("stale reply applied")
	}
	tm, _ = m.Update(findMsg{seq: m.findSeq, hits: map[string]findHit{
		"sid-old": {1, "docker cleanup"}, "sid-evals": {0, "evals for social tools"},
	}})
	m = tm.(model)
	if got := strings.Join(histIDs(m), ","); got != "sid-evals,sid-old" {
		t.Fatalf("rows = %s, want ranked hits only", got)
	}
	if !strings.Contains(m.View(), "evals for social tools") { // preview off → shown on the ? line
		t.Error("selected match reason not shown")
	}

	// esc drops the results and restores the full list.
	m, _ = press(t, m, "esc")
	if m.findHits != nil || len(histIDs(m)) != len(findHistory) {
		t.Fatalf("esc: hits=%v rows=%v", m.findHits, histIDs(m))
	}

	// Cancelling while busy drops the reply when it lands.
	m, _ = press(t, m, "?", "enter")
	seq := m.findSeq
	m, _ = press(t, m, "esc")
	tm, _ = m.Update(findMsg{seq: seq, hits: map[string]findHit{"sid-old": {0, "late"}}})
	if tm.(model).findHits != nil {
		t.Fatal("reply after cancel applied")
	}
}

func TestLocalSessionsSkipsHeadless(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSIONS_DIR", dir)
	t.Setenv("CLAUDE_PROJECTS_DIR", t.TempDir())
	t.Setenv("CLAUDE_PROC_MATCH", procComm(os.Getpid())) // treat this test process as claude
	pid := strconv.Itoa(os.Getpid())
	for name, ep := range map[string]string{"cli": "cli", "sdk": "sdk-cli"} {
		reg := `{"pid":` + pid + `,"sessionId":"sid-` + name + `","cwd":"/w","status":"busy","updatedAt":1,"entrypoint":"` + ep + `"}`
		if err := os.WriteFile(filepath.Join(dir, name+".json"), []byte(reg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	ss := localSessions()
	if len(ss) != 1 || ss[0].SessionID != "sid-cli" {
		t.Fatalf("sessions = %+v, want only the interactive one", ss)
	}
}

func TestNotePromptSamplesFirstAndLatest(t *testing.T) {
	var tm transcriptMeta
	for _, p := range []string{"a", "a", "b", "c", "d", "e", "f", "g", "g"} {
		tm.notePrompt(p)
	}
	if got := strings.Join(tm.prompts, ","); got != "a,b,d,e,f,g" {
		t.Fatalf("prompts = %s, want first 2 + last 4, repeats collapsed", got)
	}
}
