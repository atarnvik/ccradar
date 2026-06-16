package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// HistEntry is a past (non-live) Claude Code session, reconstructed from its
// transcript file.
type HistEntry struct {
	SessionID  string
	CWD        string
	Title      string
	Model      string    // model of the last non-sidechain assistant turn
	Branch     string    // git branch from the transcript
	LastPrompt string    // most recent user prompt (for the preview pane)
	LastReply  string    // most recent assistant text (for the preview pane)
	ModAt      time.Time // transcript file mtime ≈ last activity
}

// histScanLimit caps how many recent transcripts we parse, so a big history
// directory stays snappy. When scoped to a directory we scan deeper (histScan
// LimitFiltered) since most transcripts won't match the filter.
const (
	histScanLimit         = 80
	histScanLimitFiltered = 500
)

// loadHistory returns recent past sessions (most-recent transcripts first),
// excluding any session id currently live.
func loadHistory(activeIDs map[string]bool) []HistEntry {
	if demoMode() {
		return demoHistory()
	}
	paths, _ := filepath.Glob(filepath.Join(projectsDir(), "*", "*.jsonl"))
	if len(paths) == 0 {
		return nil
	}

	type pathMod struct {
		path string
		mod  time.Time
	}
	pm := make([]pathMod, 0, len(paths))
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue
		}
		pm = append(pm, pathMod{p, fi.ModTime()})
	}
	sort.Slice(pm, func(i, j int) bool { return pm[i].mod.After(pm[j].mod) })

	// Read most-recent transcripts first, stopping once we have histScanLimit
	// matching results or have read the (filter-dependent) cap of files.
	readCap := histScanLimit
	if dirFilter != "" {
		readCap = histScanLimitFiltered
	}

	var out []HistEntry
	reads := 0
	for _, e := range pm {
		if reads >= readCap || len(out) >= histScanLimit {
			break
		}
		sid := filepath.Base(e.path)
		sid = sid[:len(sid)-len(".jsonl")]
		if activeIDs[sid] {
			continue
		}
		reads++
		tm := scanTranscript(e.path)
		if tm.cwd == "" || !underFilter(tm.cwd) {
			continue // need a directory, and it must be within the filter
		}
		out = append(out, HistEntry{
			SessionID: sid, CWD: tm.cwd, Title: tm.title, Model: tm.model,
			Branch: tm.branch, LastPrompt: tm.prompt, LastReply: tm.reply, ModAt: e.mod,
		})
	}

	// group-friendly: by directory, then most recent first within a directory
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].CWD != out[j].CWD {
			return out[i].CWD < out[j].CWD
		}
		return out[i].ModAt.After(out[j].ModAt)
	})
	return out
}

// transcriptMeta is everything we extract from one transcript in a single pass.
type transcriptMeta struct {
	cwd, title, model, branch, prompt, reply string
}

// scanTranscript reads a transcript once and pulls the first cwd, the last
// ai-title, the model and git branch, and the latest user prompt + assistant
// reply (for the preview pane).
func scanTranscript(path string) transcriptMeta {
	var tm transcriptMeta
	f, err := os.Open(path)
	if err != nil {
		return tm
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if tm.cwd == "" && bytes.Contains(line, []byte(`"cwd"`)) {
			var r struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &r) == nil && r.Cwd != "" {
				tm.cwd = r.Cwd
			}
		}
		if bytes.Contains(line, []byte(`"gitBranch"`)) {
			var r struct {
				Branch string `json:"gitBranch"`
			}
			if json.Unmarshal(line, &r) == nil && r.Branch != "" {
				tm.branch = r.Branch
			}
		}
		if t, ok := scanTitle(line); ok {
			tm.title = t
		}
		if mdl, ok := scanModel(line); ok {
			tm.model = mdl
		}
		if bytes.Contains(line, []byte(`"type":"last-prompt"`)) {
			var r struct {
				LastPrompt string `json:"lastPrompt"`
			}
			if json.Unmarshal(line, &r) == nil && r.LastPrompt != "" {
				tm.prompt = r.LastPrompt
			}
		}
		if bytes.Contains(line, []byte(`"type":"assistant"`)) {
			if txt := assistantText(line); txt != "" {
				tm.reply = txt
			}
		}
	}
	return tm
}

// assistantText returns the concatenated text blocks of a non-sidechain
// assistant message ("" if it's a sidechain or has no text, e.g. tool-only).
func assistantText(line []byte) string {
	var r struct {
		IsSidechain bool `json:"isSidechain"`
		Message     struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil || r.IsSidechain {
		return ""
	}
	var b strings.Builder
	for _, c := range r.Message.Content {
		if c.Type == "text" && c.Text != "" {
			if b.Len() > 0 {
				b.WriteByte(' ')
			}
			b.WriteString(c.Text)
		}
	}
	return strings.TrimSpace(b.String())
}

// activeSessionIDs returns the set of session ids backed by a live process.
func activeSessionIDs(sessions []Session) map[string]bool {
	m := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		m[s.SessionID] = true
	}
	return m
}
