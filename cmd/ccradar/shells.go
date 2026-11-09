package main

import (
	"bytes"
	"encoding/json"
	"regexp"
	"time"
)

// bgShell is a background shell a session started (Bash with
// run_in_background).
type bgShell struct {
	ID      string
	Desc    string
	Started time.Time
	ended   bool
}

// shellTracker follows background shells through a transcript: started by a
// tool result carrying backgroundTaskId, ended by a <task-notification> with a
// final status (completed, failed, killed, stopped) or a TaskStop result.
type shellTracker struct {
	descs  map[string]string // tool_use id → description, from the Bash call
	shells []bgShell
}

var (
	notifTaskID = regexp.MustCompile(`<task-id>([^<]+)</task-id>`)
	notifStatus = regexp.MustCompile(`<status>([a-z_]+)</status>`)
)

// scan updates the tracker from one transcript line.
func (st *shellTracker) scan(line []byte) {
	switch {
	case bytes.Contains(line, []byte(`"run_in_background":true`)) && bytes.Contains(line, []byte(`"type":"assistant"`)):
		st.noteCall(line)
	case bytes.Contains(line, []byte(`"backgroundTaskId"`)):
		st.noteStart(line)
	case bytes.Contains(line, []byte(`Successfully stopped task`)):
		st.noteStopped(line)
	}
	if bytes.Contains(line, []byte("<task-notification>")) {
		st.noteEnd(line)
	}
}

// noteCall remembers the description of each background Bash call.
func (st *shellTracker) noteCall(line []byte) {
	var r struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string `json:"type"`
				ID    string `json:"id"`
				Name  string `json:"name"`
				Input struct {
					Description string `json:"description"`
					Background  bool   `json:"run_in_background"`
				} `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil || r.Type != "assistant" {
		return
	}
	for _, c := range r.Message.Content {
		if c.Type == "tool_use" && c.Name == "Bash" && c.Input.Background {
			if st.descs == nil {
				st.descs = map[string]string{}
			}
			st.descs[c.ID] = c.Input.Description
		}
	}
}

// noteStart records a shell from the tool result that launched it.
func (st *shellTracker) noteStart(line []byte) {
	var r struct {
		Type      string `json:"type"`
		Timestamp string `json:"timestamp"`
		Result    struct {
			TaskID string `json:"backgroundTaskId"`
		} `json:"toolUseResult"`
		Message struct {
			Content []struct {
				ToolUseID string `json:"tool_use_id"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil || r.Type != "user" || r.Result.TaskID == "" {
		return
	}
	sh := bgShell{ID: r.Result.TaskID}
	sh.Started, _ = time.Parse(time.RFC3339Nano, r.Timestamp)
	if len(r.Message.Content) > 0 {
		sh.Desc = st.descs[r.Message.Content[0].ToolUseID]
	}
	st.shells = append(st.shells, sh)
}

// noteStopped ends the shell a TaskStop call stopped (no notification follows).
func (st *shellTracker) noteStopped(line []byte) {
	var r struct {
		Type   string `json:"type"`
		Result struct {
			TaskID string `json:"task_id"`
		} `json:"toolUseResult"`
	}
	if json.Unmarshal(line, &r) == nil && r.Type == "user" && r.Result.TaskID != "" {
		st.end(r.Result.TaskID)
	}
}

func (st *shellTracker) end(id string) {
	for i := range st.shells {
		if st.shells[i].ID == id {
			st.shells[i].ended = true
		}
	}
}

// noteEnd marks every shell a notification names as ended, unless the
// notification only reports that it is still running.
func (st *shellTracker) noteEnd(line []byte) {
	for _, block := range bytes.Split(line, []byte("<task-notification>"))[1:] {
		status := notifStatus.FindSubmatch(block)
		if status == nil || string(status[1]) == "running" {
			continue
		}
		for _, id := range notifTaskID.FindAllSubmatch(block, -1) {
			st.end(string(id[1]))
		}
	}
}

// running lists shells still going that were started since the given time.
// A session process's shells die with it, so pass the current process's start
// to drop those from earlier runs of a resumed session.
func (st shellTracker) running(since time.Time) []bgShell {
	var out []bgShell
	for _, sh := range st.shells {
		if !sh.ended && !sh.Started.Before(since) {
			out = append(out, sh)
		}
	}
	return out
}

// shellColumnWidth is the "⚙2" indicator column on live rows.
const shellColumnWidth = 3
