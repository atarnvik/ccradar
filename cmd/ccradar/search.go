package main

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// sessionHay / histHay are the searchable text for a row: directory, name,
// title, model and the session's PR numbers ("#28"), so a query can match any
// of them.
func sessionHay(s Session) string {
	return dirDisplay(s.CWD) + " " + s.Name + " " + titleOr(s.Title) + " " + modelShort(s.Model) + prTokens(s.PRs)
}
func histHay(h HistEntry) string {
	return dirDisplay(h.CWD) + " " + h.Name + " " + titleOr(h.Title) + " " + modelShort(h.Model) + prTokens(h.PRs)
}

// prTokens is " #12 #28" for a session's PRs ("" for none).
func prTokens(prs []prLink) string {
	var b strings.Builder
	for _, l := range prs {
		fmt.Fprintf(&b, " #%d", l.Number)
	}
	return b.String()
}

// prNumberToken matches a query token that names a PR ("#28").
var prNumberToken = regexp.MustCompile(`^#\d+$`)

// fuzzyMatch reports whether every whitespace-separated token of query is a
// case-insensitive subsequence of target (tokens AND together). A "#28" token
// must instead equal a whole word of target, so it finds PR #28 and not #128
// or #208. Empty query always matches.
func fuzzyMatch(query, target string) bool {
	t := strings.ToLower(target)
	var words []string
	for _, tok := range strings.Fields(strings.ToLower(query)) {
		if prNumberToken.MatchString(tok) {
			if words == nil {
				words = strings.FieldsFunc(t, func(r rune) bool { return r != '#' && !unicode.IsDigit(r) })
			}
			if !slices.Contains(words, tok) {
				return false
			}
			continue
		}
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
