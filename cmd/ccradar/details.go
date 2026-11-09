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
