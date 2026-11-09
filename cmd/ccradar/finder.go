package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The finder (`?`) asks headless Claude Code which past sessions match a loose
// description. It sends a compact catalog of the loaded history — never the
// transcripts — and gets back ranked session ids with a short reason each.

func findBin() string   { return envOr("CCRADAR_CLAUDE", "claude") }
func findModel() string { return envOr("CCRADAR_FIND_MODEL", "haiku") }

const (
	findTimeout   = 90 * time.Second
	findMaxHits   = 5
	findPromptLen = 160 // runes kept per prompt in the catalog
)

const findSystemPrompt = `You help someone find one of their past Claude Code sessions from a loose description.
You get a catalog with one session per line: id | directory | last active | branch | title | sample user prompts.
Return the sessions that plausibly match the description, best match first, at most 5. Only include a session when its title, prompts, directory or branch give concrete evidence; return an empty list if none fit.
For each, "why" is a concrete reason under 12 words, e.g. "evals for social topic tools; ended on an API error".
Use only the catalog and copy ids exactly.`

const findSchema = `{"type":"object","properties":{"matches":{"type":"array","items":{"type":"object","properties":{"id":{"type":"string"},"why":{"type":"string"}},"required":["id","why"]}}},"required":["matches"]}`

// findHit is one ranked match: rank 0 is the best.
type findHit struct {
	rank int
	why  string
}

// findMsg carries a finder reply; seq ties it to the ask that sent it.
type findMsg struct {
	seq  int
	hits map[string]findHit // session id → hit
	err  error
}

func findCmd(seq int, query string, hs []HistEntry) tea.Cmd {
	return func() tea.Msg {
		hits, err := askClaude(query, hs)
		return findMsg{seq: seq, hits: hits, err: err}
	}
}

// findCatalog renders candidates newest first, one per line, with short
// positional ids (s1, s2, …) to keep the prompt small. ids maps them back to
// session ids. Sessions with neither a title nor a prompt are left out: there
// is nothing to match, and the model tends to pad its answer with them.
func findCatalog(hs []HistEntry) (catalog string, ids map[string]string) {
	hs = append([]HistEntry(nil), hs...)
	sort.SliceStable(hs, func(i, j int) bool { return hs[i].ModAt.After(hs[j].ModAt) })
	ids = make(map[string]string, len(hs))
	var b strings.Builder
	for _, h := range hs {
		prompts := h.Prompts
		if len(prompts) == 0 && h.LastPrompt != "" {
			prompts = []string{h.LastPrompt} // container history only carries the latest
		}
		if h.Title == "" && len(prompts) == 0 {
			continue
		}
		id := fmt.Sprintf("s%d", len(ids)+1)
		ids[id] = h.SessionID
		quoted := make([]string, len(prompts))
		for k, p := range prompts {
			quoted[k] = `"` + trunc(strings.Join(strings.Fields(p), " "), findPromptLen) + `"`
		}
		dir := dirDisplay(h.CWD)
		if h.Source != "" {
			dir = h.Source + ":" + dir
		}
		fmt.Fprintf(&b, "%s | %s | %s ago | %s | %s | %s\n", id, dir,
			fmtAge(time.Since(h.ModAt).Milliseconds()), h.Branch, titleOr(h.Title), strings.Join(quoted, " · "))
	}
	return b.String(), ids
}

// askClaude runs one headless, tool-less, unsaved Claude Code turn over the
// catalog and returns the ranked hits.
func askClaude(query string, hs []HistEntry) (map[string]findHit, error) {
	if len(hs) == 0 {
		return nil, errors.New("no past sessions to search")
	}
	catalog, ids := findCatalog(hs)
	ctx, cancel := context.WithTimeout(context.Background(), findTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, findBin(), "-p",
		"--model", findModel(),
		"--no-session-persistence", // don't add the lookup itself to history
		"--tools", "",
		"--strict-mcp-config", // skip MCP servers: faster, no side effects
		"--system-prompt", findSystemPrompt,
		"--output-format", "json",
		"--json-schema", findSchema)
	cmd.Dir = findDir()
	cmd.Stdin = strings.NewReader("Sessions:\n" + catalog + "\nDescription: " + query + "\n")
	out, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("claude took too long")
		}
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(out) == 0 {
			if msg := strings.TrimSpace(string(ee.Stderr)); msg != "" {
				return nil, errors.New(firstLine(msg))
			}
		}
		if len(out) == 0 {
			return nil, err
		}
	}
	return parseFindOutput(out, ids)
}

// parseFindOutput reads `claude -p --output-format json` output, mapping the
// catalog ids back to session ids. Unknown and repeated ids are dropped.
func parseFindOutput(out []byte, ids map[string]string) (map[string]findHit, error) {
	var res struct {
		IsError    bool   `json:"is_error"`
		Result     string `json:"result"`
		Structured struct {
			Matches []struct {
				ID  string `json:"id"`
				Why string `json:"why"`
			} `json:"matches"`
		} `json:"structured_output"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, errors.New("unexpected reply from claude")
	}
	if res.IsError {
		msg := firstLine(strings.TrimSpace(res.Result))
		if msg == "" {
			msg = "claude reported an error"
		}
		return nil, errors.New(msg)
	}
	hits := map[string]findHit{}
	for _, mt := range res.Structured.Matches {
		sid, ok := ids[strings.TrimSpace(mt.ID)]
		if !ok {
			continue
		}
		if _, dup := hits[sid]; dup {
			continue
		}
		hits[sid] = findHit{rank: len(hits), why: strings.TrimSpace(mt.Why)}
		if len(hits) == findMaxHits {
			break
		}
	}
	return hits, nil
}

// findDir is a fixed working directory for the lookup, so it never picks up a
// project's CLAUDE.md and leaves at most one (empty) entry under projects/.
func findDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		base = filepath.Join(homeDir(), ".cache")
	}
	d := filepath.Join(base, "ccradar", "find")
	_ = os.MkdirAll(d, 0o755)
	return d
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
