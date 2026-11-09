package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// sessionDetails is the transcript-derived context shared by live and past
// sessions, beyond the title/model/prompt/reply every row has.
type sessionDetails struct {
	Name    string    // a name the user gave the session; shown instead of the ai-title
	Recap   string    // Claude's latest "while you were away" summary
	RecapAt time.Time // when that recap was written
	Cost    costState // totals as of the session's last exit (zero if never recorded)
	PRs     []prLink  // pull requests the session opened or touched, most recent last
	Context int       // tokens in context as of the latest turn (0 = unknown)
}

// costState is the last cost-state record. Claude Code writes it when a session
// process exits, so it is only current for sessions that aren't running.
type costState struct {
	USD          float64 `json:"totalCostUSD"`
	LinesAdded   int     `json:"totalLinesAdded"`
	LinesRemoved int     `json:"totalLinesRemoved"`
}

func (c costState) known() bool { return c.USD > 0 || c.LinesAdded > 0 || c.LinesRemoved > 0 }

// recapSuffix is the settings hint Claude Code appends to every recap.
const recapSuffix = "(disable recaps in /config)"

// scanDetails updates d from one transcript line. Each record type is gated on
// a cheap substring check so the common lines skip JSON decoding entirely.
func (d *sessionDetails) scanDetails(line []byte) {
	switch {
	case bytes.Contains(line, []byte(`"subtype":"away_summary"`)):
		var r struct {
			Subtype   string `json:"subtype"`
			Content   string `json:"content"`
			Timestamp string `json:"timestamp"`
		}
		if json.Unmarshal(line, &r) == nil && r.Subtype == "away_summary" && r.Content != "" {
			d.Recap = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(r.Content), recapSuffix))
			d.RecapAt, _ = time.Parse(time.RFC3339Nano, r.Timestamp)
		}
	case bytes.Contains(line, []byte(`"type":"agent-name"`)):
		var r struct {
			Type string `json:"type"`
			Name string `json:"agentName"`
		}
		if json.Unmarshal(line, &r) == nil && r.Type == "agent-name" && r.Name != "" {
			d.Name = r.Name
		}
	case bytes.Contains(line, []byte(`"type":"pr-link"`)):
		d.scanPRLink(line)
	case bytes.Contains(line, []byte(`"type":"assistant"`)) && bytes.Contains(line, []byte(`"usage"`)):
		d.scanUsage(line)
	case bytes.Contains(line, []byte(`"subtype":"compact_boundary"`)):
		var r struct {
			Subtype string `json:"subtype"`
			Meta    struct {
				Post int `json:"postTokens"`
			} `json:"compactMetadata"`
		}
		if json.Unmarshal(line, &r) == nil && r.Subtype == "compact_boundary" && r.Meta.Post > 0 {
			d.Context = r.Meta.Post // until the next turn reports its own size
		}
	case bytes.Contains(line, []byte(`"type":"cost-state"`)):
		var r struct {
			Type string `json:"type"`
			costState
		}
		if json.Unmarshal(line, &r) == nil && r.Type == "cost-state" {
			d.Cost = r.costState
		}
	}
}

// scanUsage records the context size from a main-thread assistant turn: the
// prompt it was sent (uncached + cache-written + cache-read input tokens).
// Sub-agent turns and synthetic (error/interrupt) turns are skipped.
func (d *sessionDetails) scanUsage(line []byte) {
	var r struct {
		Type        string `json:"type"`
		IsSidechain bool   `json:"isSidechain"`
		Message     struct {
			Model string `json:"model"`
			Usage struct {
				Input      int `json:"input_tokens"`
				CacheWrite int `json:"cache_creation_input_tokens"`
				CacheRead  int `json:"cache_read_input_tokens"`
			} `json:"usage"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &r) != nil || r.Type != "assistant" || r.IsSidechain || r.Message.Model == "<synthetic>" {
		return
	}
	if n := r.Message.Usage.Input + r.Message.Usage.CacheWrite + r.Message.Usage.CacheRead; n > 0 {
		d.Context = n
	}
}

// fmtTokens renders a token count in at most 5 cells (950, 12k, 412k, 1.2M).
func fmtTokens(n int) string {
	switch {
	case n < 1000:
		return fmt.Sprint(n)
	case n < 999_500:
		return fmt.Sprintf("%dk", (n+500)/1000)
	default:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	}
}

// displayTitle is what a row shows: the user's name for the session when it
// has one, else the ai-title.
func displayTitle(d sessionDetails, title string) string {
	if d.Name != "" {
		return d.Name
	}
	return titleOr(title)
}

// fmtCost renders a dollar amount in at most 5 cells ($0.58, $7.91, $426, $1.2k).
func fmtCost(usd float64) string {
	switch {
	case usd < 10:
		return fmt.Sprintf("$%.2f", usd)
	case usd < 1000:
		return fmt.Sprintf("$%.0f", usd)
	default:
		return fmt.Sprintf("$%.1fk", usd/1000)
	}
}

// costLine is the preview's one-line cost summary ("" when nothing recorded).
func costLine(c costState) string {
	if !c.known() {
		return ""
	}
	return fmt.Sprintf("%s · +%d −%d lines", fmtCost(c.USD), c.LinesAdded, c.LinesRemoved)
}
