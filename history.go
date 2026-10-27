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
	ModAt      time.Time // last activity: newest record timestamp (file mtime as fallback)
	Source     string    // source label ("" = local; else a --docker container)
	Container  string    // resolved docker container id ("" = local)
}

// histScanLimit caps how many recent transcripts we parse, so a big history
// directory stays snappy. When scoped to a directory we scan deeper (histScan
// LimitFiltered) since most transcripts won't match the filter.
const (
	histScanLimit         = 80
	histScanLimitFiltered = 500
)

// loadHistory returns recent past sessions across all sources (most-recent
// first per source), excluding any session id currently live.
func loadHistory(activeIDs map[string]bool) []HistEntry {
	if demoMode() {
		return demoHistory()
	}
	var out []HistEntry
	for _, src := range allSources() {
		if src.kind == srcDocker {
			out = append(out, dockerHistory(src, activeIDs)...)
		} else {
			out = append(out, localHistory(activeIDs)...)
		}
	}
	return out
}

// localHistory returns recent past sessions from the host (most-recent first).
func localHistory(activeIDs map[string]bool) []HistEntry {
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
	// matching results or have read the (filter-dependent) cap of files. mtime
	// is only an upper bound on activity (Claude appends untimestamped records
	// like cost-state to old transcripts), so it orders the scan but the age
	// shown comes from the transcript's own timestamps.
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
		if tm.continued {
			continue // superseded by a forked continuation that carries its history
		}
		modAt := tm.last
		if modAt.IsZero() {
			modAt = e.mod
		}
		out = append(out, HistEntry{
			SessionID: sid, CWD: tm.cwd, Title: tm.title, Model: tm.model,
			Branch: tm.branch, LastPrompt: tm.prompt, LastReply: tm.reply, ModAt: modAt,
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
	last                                     time.Time // newest top-level record timestamp
	continued                                bool      // a continued-in record: this session was forked into another
}

// scanTranscript reads a transcript once and pulls the first cwd, the last
// ai-title, the model and git branch, the latest user prompt + assistant reply
// (for the preview pane), the newest record timestamp, and whether the session
// was continued in another one.
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
		// One decode for the envelope fields most message records carry. Other
		// timestamped records (api-request, file-history-snapshot, …) can be
		// large, so their timestamp is picked out without a full decode.
		if (tm.cwd == "" && bytes.Contains(line, []byte(`"cwd"`))) || bytes.Contains(line, []byte(`"gitBranch"`)) {
			var r struct {
				Cwd       string `json:"cwd"`
				Branch    string `json:"gitBranch"`
				Timestamp string `json:"timestamp"`
			}
			if json.Unmarshal(line, &r) == nil {
				if tm.cwd == "" && r.Cwd != "" {
					tm.cwd = r.Cwd
				}
				if r.Branch != "" {
					tm.branch = r.Branch
				}
				tm.noteTime(r.Timestamp)
			}
		} else {
			tm.noteTime(rawTimestamp(line))
		}
		if bytes.Contains(line, []byte(`"type":"continued-in"`)) {
			var r struct {
				Type        string `json:"type"`
				ContinuedIn string `json:"continuedInSessionId"`
			}
			if json.Unmarshal(line, &r) == nil && r.Type == "continued-in" && r.ContinuedIn != "" {
				tm.continued = true
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

// noteTime advances tm.last to ts (RFC 3339) when it is newer.
func (tm *transcriptMeta) noteTime(ts string) {
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && t.After(tm.last) {
		tm.last = t
	}
}

// rawTimestamp returns the first "timestamp" string value in a JSONL line
// without decoding it ("" if none).
func rawTimestamp(line []byte) string {
	key := []byte(`"timestamp":"`)
	i := bytes.Index(line, key)
	if i < 0 {
		return ""
	}
	v := line[i+len(key):]
	j := bytes.IndexByte(v, '"')
	if j < 0 {
		return ""
	}
	return string(v[:j])
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
