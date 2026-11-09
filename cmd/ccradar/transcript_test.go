package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// A transcript as Claude Code writes it today: the last real activity is the
// synthetic error turn at 10:00:07.5; the ai-title, last-prompt and cost-state
// records after it carry no timestamp.
var activeTranscript = []string{
	`{"type":"user","cwd":"/w/app","gitBranch":"main","timestamp":"2026-09-01T10:00:00.000Z","message":{"role":"user","content":"hi"}}`,
	`{"type":"assistant","isSidechain":false,"timestamp":"2026-09-01T10:00:05.000Z","message":{"model":"claude-opus-5-5","content":[{"type":"text","text":"hello"}]}}`,
	`{"type":"assistant","isSidechain":true,"timestamp":"2026-09-01T10:00:06.000Z","message":{"model":"claude-haiku-5-5","content":[{"type":"text","text":"sub"}]}}`,
	`{"type":"assistant","isSidechain":false,"timestamp":"2026-09-01T10:00:07.500Z","message":{"model":"<synthetic>","content":[{"type":"text","text":"API Error"}]}}`,
	`{"type":"ai-title","aiTitle":"Fix it","sessionId":"old"}`,
	`{"type":"last-prompt","lastPrompt":"hi","sessionId":"old"}`,
	`{"type":"cost-state","sessionId":"old","totalCostUSD":1.5}`,
}

var activeLast = time.Date(2026, 9, 1, 10, 0, 7, 500_000_000, time.UTC)

// A session that was forked into a continuation carrying its history.
var continuedTranscript = []string{
	`{"type":"user","cwd":"/w/app","timestamp":"2026-09-02T09:00:00.000Z","message":{"role":"user","content":"go"}}`,
	`{"type":"ai-title","aiTitle":"Fix it","sessionId":"parent"}`,
	`{"type":"continued-in","timestamp":"2026-09-02T09:30:00.000Z","sessionId":"parent","continuedInSessionId":"old"}`,
}

// writeTranscript writes lines to path and stamps it with mtime, the way a
// late bookkeeping append would.
func writeTranscript(t *testing.T, path string, mtime time.Time, lines ...string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func TestScanTranscript(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "old.jsonl")
	writeTranscript(t, p, time.Now(), activeTranscript...)

	tm := scanTranscript(p)
	if !tm.last.Equal(activeLast) {
		t.Errorf("last = %v, want %v", tm.last, activeLast)
	}
	if tm.model != "claude-opus-5-5" {
		t.Errorf("model = %q, want the real model (not sidechain or <synthetic>)", tm.model)
	}
	if tm.cwd != "/w/app" || tm.branch != "main" || tm.title != "Fix it" || tm.prompt != "hi" {
		t.Errorf("wrong meta: %+v", tm)
	}
	if tm.continued {
		t.Error("continued set without a continued-in record")
	}

	c := filepath.Join(dir, "parent.jsonl")
	writeTranscript(t, c, time.Now(), continuedTranscript...)
	if !scanTranscript(c).continued {
		t.Error("continued-in record not detected")
	}
}

func TestLocalHistoryActivityAndContinuations(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("CLAUDE_PROJECTS_DIR", dir)
	prev := dirFilter
	dirFilter = ""
	t.Cleanup(func() { dirFilter = prev })

	now := time.Now()
	proj := filepath.Join(dir, "-w-app")
	fresh := now.Add(-time.Hour).UTC().Format(time.RFC3339Nano)
	bareMod := now.Add(-48 * time.Hour).Truncate(time.Second)
	writeTranscript(t, filepath.Join(proj, "old.jsonl"), now, activeTranscript...) // mtime bumped today
	writeTranscript(t, filepath.Join(proj, "parent.jsonl"), now, continuedTranscript...)
	writeTranscript(t, filepath.Join(proj, "fresh.jsonl"), now,
		`{"type":"user","cwd":"/w/app","timestamp":"`+fresh+`","message":{"role":"user","content":"x"}}`)
	writeTranscript(t, filepath.Join(proj, "bare.jsonl"), bareMod, `{"type":"user","cwd":"/w/app"}`)

	hs := localHistory(nil)
	var ids []string
	for _, h := range hs {
		ids = append(ids, h.SessionID)
	}
	if got := strings.Join(ids, ","); got != "fresh,bare,old" {
		t.Fatalf("history = %s, want fresh,bare,old (continued parent hidden, ordered by activity)", got)
	}
	if !hs[2].ModAt.Equal(activeLast) {
		t.Errorf("old ModAt = %v, want last record timestamp %v (not mtime)", hs[2].ModAt, activeLast)
	}
	if !hs[1].ModAt.Equal(bareMod) {
		t.Errorf("bare ModAt = %v, want mtime fallback %v", hs[1].ModAt, bareMod)
	}
}

// The in-container scripts only need POSIX sh + grep/sed, so run them against
// the same fixtures on the host.
func TestDockerScriptsOnFixture(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("no sh")
	}
	dir := t.TempDir()
	writeTranscript(t, filepath.Join(dir, "projects", "-w-app", "old.jsonl"), time.Now(), activeTranscript...)
	writeTranscript(t, filepath.Join(dir, "projects", "-w-app", "parent.jsonl"), time.Now(), continuedTranscript...)
	// A live registry entry: our own pid, so the script's kill -0 sees it alive.
	reg := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"sessionId":"old","cwd":"/w/app","status":"idle","updatedAt":1000}`
	writeTranscript(t, filepath.Join(dir, "sessions", "1.json"), time.Now(), reg)

	out, err := exec.Command("sh", "-c", dockerScript(dir)).Output()
	if err != nil {
		t.Fatal(err)
	}
	ss := parseDockerSessions(string(out), "box", "cid")
	if len(ss) != 1 || ss[0].Model != "claude-opus-5-5" || ss[0].Title != "Fix it" {
		t.Fatalf("sessions = %+v, want one with the real model", ss)
	}

	out, err = exec.Command("sh", "-c", dockerHistoryScript(dir)).Output()
	if err != nil {
		t.Fatal(err)
	}
	hs := parseDockerHistory(string(out), "box", "cid", nil)
	if len(hs) != 1 || hs[0].SessionID != "old" {
		t.Fatalf("history = %+v, want only old (continued parent skipped)", hs)
	}
	if hs[0].Model != "claude-opus-5-5" || !hs[0].ModAt.Equal(activeLast) {
		t.Fatalf("history entry = %+v, want real model and last record timestamp", hs[0])
	}
}
