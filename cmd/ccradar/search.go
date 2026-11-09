package main

import (
	"strings"
)

// sessionHay / histHay are the searchable text for a row: directory + title +
// model, so a query can match the folder, the session name, or the model.
func sessionHay(s Session) string {
	return dirDisplay(s.CWD) + " " + s.Name + " " + titleOr(s.Title) + " " + modelShort(s.Model)
}
func histHay(h HistEntry) string {
	return dirDisplay(h.CWD) + " " + h.Name + " " + titleOr(h.Title) + " " + modelShort(h.Model)
}

// fuzzyMatch reports whether every whitespace-separated token of query is a
// case-insensitive subsequence of target (tokens AND together). Empty query
// always matches.
func fuzzyMatch(query, target string) bool {
	t := strings.ToLower(target)
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		if !subsequence(tok, t) {
			return false
		}
	}
	return true
}

// subsequence reports whether all runes of q appear in t in order.
func subsequence(q, t string) bool {
	qr := []rune(q)
	if len(qr) == 0 {
		return true
	}
	qi := 0
	for _, tr := range t {
		if tr == qr[qi] {
			if qi++; qi == len(qr) {
				return true
			}
		}
	}
	return false
}

// matchCount is the number of selectable (session/hist) rows currently shown.
func (m model) matchCount() int {
	n := 0
	for _, r := range m.rows {
		if selectable(r.kind) {
			n++
		}
	}
	return n
}
