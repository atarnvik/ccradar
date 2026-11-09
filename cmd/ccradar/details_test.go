package main

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
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
