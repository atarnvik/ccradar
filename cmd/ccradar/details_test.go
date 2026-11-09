package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var detailsTranscript = []string{
	`{"type":"user","cwd":"/w/app","timestamp":"2026-09-01T10:00:00.000Z","message":{"role":"user","content":"hi"}}`,
	`{"type":"system","subtype":"away_summary","content":"Old recap. (disable recaps in /config)","timestamp":"2026-09-01T10:05:00.000Z"}`,
	`{"type":"system","subtype":"away_summary","content":"PR #12 is open; next, bump the pin. (disable recaps in /config)","timestamp":"2026-09-01T11:00:00.000Z"}`,
	`{"type":"ai-title","aiTitle":"Fix it","sessionId":"s"}`,
	`{"type":"agent-name","agentName":"Pin bump","sessionId":"s"}`,
	`{"type":"cost-state","sessionId":"s","totalCostUSD":3.35,"totalLinesAdded":10,"totalLinesRemoved":2}`,
	`{"type":"cost-state","sessionId":"s","totalCostUSD":7.91,"totalLinesAdded":120,"totalLinesRemoved":8}`,
}

func TestScanDetails(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, p, time.Now(), detailsTranscript...)
	d := scanTranscript(p).details

	if d.Recap != "PR #12 is open; next, bump the pin." {
		t.Errorf("recap = %q, want the latest one without the settings hint", d.Recap)
	}
	if want := time.Date(2026, 9, 1, 11, 0, 0, 0, time.UTC); !d.RecapAt.Equal(want) {
		t.Errorf("recapAt = %v, want %v", d.RecapAt, want)
	}
	if d.Name != "Pin bump" {
		t.Errorf("name = %q", d.Name)
	}
	if d.Cost != (costState{USD: 7.91, LinesAdded: 120, LinesRemoved: 8}) {
		t.Errorf("cost = %+v, want the last cost-state", d.Cost)
	}
	if got := displayTitle(d, "Fix it"); got != "Pin bump" {
		t.Errorf("displayTitle = %q, want the user's name over the ai-title", got)
	}
	if got := displayTitle(sessionDetails{}, ""); got != "(no title)" {
		t.Errorf("displayTitle fallback = %q", got)
	}
}

func TestFmtCost(t *testing.T) {
	for usd, want := range map[float64]string{0.584: "$0.58", 7.91: "$7.91", 77.73: "$78", 426.24: "$426", 1234: "$1.2k"} {
		if got := fmtCost(usd); got != want {
			t.Errorf("fmtCost(%v) = %q, want %q", usd, got, want)
		}
	}
	if costLine(costState{}) != "" {
		t.Error("costLine should be empty without a cost-state")
	}
	if got := costLine(costState{USD: 7.91, LinesAdded: 120, LinesRemoved: 8}); got != "$7.91 · +120 −8 lines" {
		t.Errorf("costLine = %q", got)
	}
}

func TestRegistryNameOnlyWhenUserChosen(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_SESSIONS_DIR", dir)
	t.Setenv("CLAUDE_PROJECTS_DIR", t.TempDir())
	t.Setenv("CLAUDE_PROC_MATCH", procComm(os.Getpid()))
	pid := strconv.Itoa(os.Getpid())
	for sid, src := range map[string]string{"derived": "derived", "chosen": "user"} {
		reg := `{"pid":` + pid + `,"sessionId":"` + sid + `","cwd":"/w","status":"idle","updatedAt":1,"entrypoint":"cli","name":"name-` + sid + `","nameSource":"` + src + `"}`
		if err := os.WriteFile(filepath.Join(dir, sid+".json"), []byte(reg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names := map[string]string{}
	for _, s := range localSessions() {
		names[s.SessionID] = s.Name
	}
	if names["derived"] != "" || names["chosen"] != "name-chosen" {
		t.Fatalf("names = %v, want only the user-chosen one", names)
	}
}

func TestFindCatalogIncludesRecap(t *testing.T) {
	catalog, _ := findCatalog([]HistEntry{{
		SessionID: "a", CWD: "/w", Title: "Fix it", ModAt: time.Now(),
		sessionDetails: sessionDetails{Name: "Pin bump", Recap: "PR #12 is open"},
	}})
	if !strings.Contains(catalog, "| Pin bump |") || !strings.Contains(catalog, `| recap: "PR #12 is open"`) {
		t.Fatalf("catalog = %q", catalog)
	}
}

func TestFindCatalogIncludesPRs(t *testing.T) {
	catalog, _ := findCatalog([]HistEntry{{
		SessionID: "a", CWD: "/w", Title: "Fix it", ModAt: time.Now(),
		sessionDetails: sessionDetails{PRs: []prLink{{Repo: "o/r", Number: 12}, {Repo: "o/r", Number: 28}}},
	}})
	if !strings.Contains(catalog, "| PRs: #12 #28") {
		t.Fatalf("catalog = %q", catalog)
	}
}

func TestSearchFindsSessionByPRNumber(t *testing.T) {
	m := model{view: viewHistory, width: 120, height: 20, query: "#28"}
	m.history = []HistEntry{
		{SessionID: "a", CWD: "/w", Title: "Opened 28", ModAt: time.Now(),
			sessionDetails: sessionDetails{PRs: []prLink{{Repo: "o/r", Number: 28}}}},
		{SessionID: "b", CWD: "/w", Title: "Opened 128", ModAt: time.Now(),
			sessionDetails: sessionDetails{PRs: []prLink{{Repo: "o/r", Number: 128}}}},
	}
	m.rebuild()
	if v := m.View(); !strings.Contains(v, "Opened 28") || strings.Contains(v, "Opened 128") {
		t.Fatalf("view:\n%s", v)
	}
}

func TestCostHiddenUntilToggled(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	m := model{view: viewHistory, preview: true, width: 120, height: 20}
	m.history = []HistEntry{{SessionID: "a", CWD: "/w", Title: "Fix it", ModAt: time.Now(),
		sessionDetails: sessionDetails{Cost: costState{USD: 7.91, LinesAdded: 3}}}}
	m.rebuild()
	if strings.Contains(m.View(), "$7.91") {
		t.Fatal("cost shown by default")
	}
	tm, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("$")})
	m = tm.(model)
	if !m.showCost || !strings.Contains(m.View(), "$7.91") {
		t.Fatal("$ should show the cost")
	}
	if !loadConfig().Cost {
		t.Error("cost preference not saved")
	}
	if strings.Contains(m.View(), "$ ") || strings.Contains(m.View(), "cost:") {
		t.Error("the toggle should stay out of the help line")
	}
}

func TestContextSizeFromTranscript(t *testing.T) {
	var d sessionDetails
	for _, ln := range []string{
		`{"type":"assistant","isSidechain":false,"message":{"model":"claude-opus-5-5","usage":{"input_tokens":2,"cache_creation_input_tokens":2234,"cache_read_input_tokens":101314}}}`,
		`{"type":"assistant","isSidechain":true,"message":{"model":"claude-haiku-4-5","usage":{"input_tokens":9000}}}`, // sub-agent
		`{"type":"assistant","isSidechain":false,"message":{"model":"<synthetic>","usage":{"input_tokens":0}}}`,        // error turn
	} {
		d.scanDetails([]byte(ln))
	}
	if d.Context != 103550 {
		t.Fatalf("context = %d, want 103550 (sub-agent and synthetic turns skipped)", d.Context)
	}
	d.scanDetails([]byte(`{"type":"system","subtype":"compact_boundary","compactMetadata":{"trigger":"auto","preTokens":441380,"postTokens":31816}}`))
	if d.Context != 31816 {
		t.Fatalf("after compaction context = %d, want 31816", d.Context)
	}
	for n, want := range map[int]string{950: "950", 31816: "32k", 441380: "441k", 999_499: "999k", 999_500: "1.0M", 1_234_567: "1.2M"} {
		if got := fmtTokens(n); got != want {
			t.Errorf("fmtTokens(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestContextColumnOnLiveRows(t *testing.T) {
	now := nowMs()
	m := model{view: viewActive, width: 140, height: 20, sessionsReady: true, preview: true}
	m.sessions = []Session{{PID: 1, CWD: "/a", Status: "idle", UpdatedAt: now, Title: "Big one", SurfaceID: "s",
		sessionDetails: sessionDetails{Context: 412_000}}}
	m.rebuild()
	if v := m.View(); !strings.Contains(v, " 412k ") || !strings.Contains(v, "412k context") {
		t.Fatalf("want the context size in the row and the preview:\n%s", v)
	}
}
