package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func bgCall(toolID, desc string) string {
	return `{"type":"assistant","isSidechain":false,"timestamp":"2026-09-01T10:00:00.000Z","message":{"model":"claude-opus-5-5","content":[{"type":"tool_use","id":"` + toolID + `","name":"Bash","input":{"command":"x","description":"` + desc + `","run_in_background":true}}]}}`
}

func bgStart(toolID, taskID, ts string) string {
	return `{"type":"user","timestamp":"` + ts + `","message":{"role":"user","content":[{"tool_use_id":"` + toolID + `","type":"tool_result","content":"Command running in background with ID: ` + taskID + `"}]},"toolUseResult":{"stdout":"","backgroundTaskId":"` + taskID + `"}}`
}

func bgNotify(status string, taskIDs ...string) string {
	ids := ""
	for _, id := range taskIDs {
		ids += `<task-id>` + id + `</task-id>\n`
	}
	return `{"type":"user","timestamp":"2026-09-01T12:00:00.000Z","message":{"role":"user","content":"<task-notification>\n` + ids + `<status>` + status + `</status>\n<summary>x</summary>\n</task-notification>"}}`
}

var shellTranscript = []string{
	`{"type":"user","cwd":"/w","timestamp":"2026-09-01T09:00:00.000Z","message":{"role":"user","content":"go"}}`,
	bgCall("t-old", "From an earlier run"), bgStart("t-old", "b-old", "2026-09-01T09:30:00.000Z"),
	bgCall("t-api", "Start the API on :8000"), bgStart("t-api", "b-api", "2026-09-01T10:00:01.000Z"),
	bgCall("t-web", "Start the client on :3000"), bgStart("t-web", "b-web", "2026-09-01T10:00:02.000Z"),
	bgCall("t-test", "Run the suite"), bgStart("t-test", "b-test", "2026-09-01T10:00:03.000Z"),
	bgNotify("running", "b-api"), // a progress update, not an end
	bgNotify("completed", "b-test"),
	`{"type":"queue-operation","operation":"enqueue","content":"<task-notification>\n<task-id>b-gone</task-id>\n<status>killed</status>\n</task-notification>"}`,
	bgCall("t-stop", "Stopped by TaskStop"), bgStart("t-stop", "b-stop", "2026-09-01T10:00:04.000Z"),
	`{"type":"user","timestamp":"2026-09-01T10:30:00.000Z","message":{"role":"user","content":[{"tool_use_id":"t-x","type":"tool_result","content":"{\"message\":\"Successfully stopped task: b-stop (x)\"}"}]},"toolUseResult":{"message":"Successfully stopped task: b-stop (x)","task_id":"b-stop","task_type":"local_bash"}}`,
}

func TestShellTracker(t *testing.T) {
	p := filepath.Join(t.TempDir(), "s.jsonl")
	writeTranscript(t, p, time.Now(), shellTranscript...)
	st := scanTranscript(p).shells

	since := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC) // the current process started at 10:00
	got := st.running(since)
	if len(got) != 2 || got[0].ID != "b-api" || got[1].ID != "b-web" {
		t.Fatalf("running = %+v, want b-api and b-web (b-test completed, b-stop stopped, b-old from a previous run)", got)
	}
	if got[0].Desc != "Start the API on :8000" {
		t.Errorf("desc = %q, want the Bash call's description", got[0].Desc)
	}
	if len(st.running(time.Time{})) != 3 {
		t.Errorf("without a start time the earlier run's shell should count too")
	}

	// One notification can end several shells at once (e.g. on shutdown).
	writeTranscript(t, p, time.Now(), append(shellTranscript, bgNotify("stopped", "b-api", "b-web"))...)
	if left := scanTranscript(p).shells.running(since); len(left) != 0 {
		t.Errorf("after a stopped notification: %+v", left)
	}
}

func TestLiveSessionShellsAndStatusTime(t *testing.T) {
	dir := t.TempDir()
	proj := t.TempDir()
	t.Setenv("CLAUDE_SESSIONS_DIR", dir)
	t.Setenv("CLAUDE_PROJECTS_DIR", proj)
	t.Setenv("CLAUDE_PROC_MATCH", procComm(os.Getpid()))
	writeTranscript(t, filepath.Join(proj, "-w", "sid.jsonl"), time.Now(), shellTranscript...)

	started := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC).UnixMilli()
	reg := `{"pid":` + strconv.Itoa(os.Getpid()) + `,"sessionId":"sid","cwd":"/w","status":"busy","entrypoint":"cli",` +
		`"startedAt":` + strconv.FormatInt(started, 10) + `,"updatedAt":999,"statusUpdatedAt":500}`
	if err := os.WriteFile(filepath.Join(dir, "1.json"), []byte(reg), 0o644); err != nil {
		t.Fatal(err)
	}
	ss := localSessions()
	if len(ss) != 1 {
		t.Fatalf("sessions = %+v", ss)
	}
	if len(ss[0].Shells) != 2 {
		t.Errorf("shells = %+v, want the 2 started by this process", ss[0].Shells)
	}
	if ss[0].UpdatedAt != 500 {
		t.Errorf("UpdatedAt = %d, want statusUpdatedAt (time in the current status)", ss[0].UpdatedAt)
	}
}
