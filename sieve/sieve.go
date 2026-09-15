// Package sieve fuzzy-filters a table's rows by a text query, the way
// the jira-today fzf picker does: every character of the query must
// appear in order somewhere in the row's text (a subsequence match,
// case-insensitive), so "cnp" matches a row containing "canopy" and
// "wt/dk" matches a path containing "worktrees/dashkit". A subsequence
// (not substring) is what makes typing a filter feel like fzf: the
// query doesn't have to appear verbatim anywhere, it just has to rhyme
// with the row in the right order.
//
// The match runs against a row's plain cell strings (whatever subset of
// columns the caller considers searchable), never the rendered,
// colorized view: by the time a row is on screen it carries zero-width
// cursor sentinels (loam.Tag) and ANSI styling that have no business
// being matchable text.
//
// Both dashboards own their own filter-input state (the textinput, the
// modal key routing); this package is only the shared "does this row
// match this query" answer, so the two apps cannot drift apart on
// matching semantics.
package sieve

import "strings"

// Match reports whether query fuzzy-matches the row made of cells: an
// empty query matches everything (no filter applied); otherwise every
// rune of the query must appear, in order, in the space-joined cells,
// case-insensitively. Extra characters in the row are always fine
// (skipping is what makes it a subsequence match), so cell boundaries
// and padding never get in the way.
func Match(query string, cells ...string) bool {
	if query == "" {
		return true
	}
	runes := []rune(strings.ToLower(query))
	target := strings.ToLower(strings.Join(cells, " "))
	i := 0
	for _, r := range target {
		if r == runes[i] {
			i++
			if i == len(runes) {
				return true
			}
		}
	}
	return false
}
