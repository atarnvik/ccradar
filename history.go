package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// HistEntry is a past (non-live) Claude Code session, reconstructed from its
// transcript file.
type HistEntry struct {
	SessionID string
	CWD       string
	Title     string
	Model     string    // model of the last non-sidechain assistant turn
	ModAt     time.Time // transcript file mtime ≈ last activity
}

// histScanLimit caps how many recent transcripts we parse, so a big history
// directory stays snappy.
const histScanLimit = 80

// loadHistory returns recent past sessions (most-recent transcripts first),
// excluding any session id currently live.
func loadHistory(activeIDs map[string]bool) []HistEntry {
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
	if len(pm) > histScanLimit {
		pm = pm[:histScanLimit]
	}

	var out []HistEntry
	for _, e := range pm {
		sid := filepath.Base(e.path)
		sid = sid[:len(sid)-len(".jsonl")]
		if activeIDs[sid] {
			continue
		}
		cwd, title, model := readTranscript(e.path)
		if cwd == "" {
			continue // can't resume without a directory
		}
		out = append(out, HistEntry{SessionID: sid, CWD: cwd, Title: title, Model: model, ModAt: e.mod})
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

// readTranscript does a single pass to grab the first cwd, the last ai-title,
// and the model of the last non-sidechain assistant turn.
func readTranscript(path string) (cwd, title, model string) {
	f, err := os.Open(path)
	if err != nil {
		return "", "", ""
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1024*1024), 32*1024*1024)
	for sc.Scan() {
		line := sc.Bytes()
		if cwd == "" && bytes.Contains(line, []byte(`"cwd"`)) {
			var r struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(line, &r) == nil && r.Cwd != "" {
				cwd = r.Cwd
			}
		}
		if t, ok := scanTitle(line); ok {
			title = t
		}
		if mdl, ok := scanModel(line); ok {
			model = mdl
		}
	}
	return cwd, title, model
}

// activeSessionIDs returns the set of session ids backed by a live process.
func activeSessionIDs(sessions []Session) map[string]bool {
	m := make(map[string]bool, len(sessions))
	for _, s := range sessions {
		m[s.SessionID] = true
	}
	return m
}
