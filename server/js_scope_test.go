package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestSharedHelpersDeclaredEarly guards against a whole class of "feature is
// deployed but does nothing" bugs.
//
// Each page has its own <script> block, but a function declared at the top level
// of one block is visible to the others only *after* that block has run. A page
// that calls a helper during its own execution, while the helper is declared
// further down the document, throws a ReferenceError: the page's script stops at
// that statement and everything after it never runs.
//
// That is exactly how the genre filter broke: loadTrendingGenres() ran before
// escHTML was declared, so the chip row was never filled in — the feature looked
// unimplemented although the API and the markup were both fine.
func TestSharedHelpersDeclaredEarly(t *testing.T) {
	src := readSource(t, templateSourceFile)

	// Helpers used by more than one page's script.
	helpers := []string{"ICO", "escHTML"}

	for _, name := range helpers {
		decl := regexp.MustCompile(`function\s+` + name + `\s*\(`).FindStringIndex(src)
		if decl == nil {
			t.Errorf("%s is never declared", name)
			continue
		}
		declLine := strings.Count(src[:decl[0]], "\n") + 1

		// Every call site has to come after the declaration.
		callRe := regexp.MustCompile(`\b` + name + `\s*\(`)
		for _, m := range callRe.FindAllStringIndex(src, -1) {
			// Skip the declaration itself.
			if m[0] == decl[0] {
				continue
			}
			line := strings.Count(src[:m[0]], "\n") + 1
			lineText := strings.TrimSpace(strings.Split(src, "\n")[line-1])
			// Skip comments: they mention the name without calling it.
			if strings.HasPrefix(lineText, "//") || strings.HasPrefix(lineText, "*") {
				continue
			}
			if line < declLine {
				t.Errorf("%s is called on line %d but declared on line %d; "+
					"the call runs before the function exists and throws a ReferenceError",
					name, line, declLine)
			}
		}
	}
}

// TestSharedHelpersInFirstScriptBlock verifies the shared helpers live in the
// first script block, which every page runs before its own code.
func TestSharedHelpersInFirstScriptBlock(t *testing.T) {
	src := readSource(t, templateSourceFile)

	firstEnd := strings.Index(src, "</script>")
	if firstEnd < 0 {
		t.Fatal("no script block found")
	}
	first := src[:firstEnd]

	for _, name := range []string{"ICO", "escHTML"} {
		if !strings.Contains(first, "function "+name+"(") {
			t.Errorf("%s should be declared in the first script block so every page can use it", name)
		}
	}
}

// TestEscHTMLIsDeclaredOnce verifies there is a single definition.
//
// Two copies would drift: the one a page actually calls depends on which block
// runs first, which is exactly the kind of ambiguity that produced this bug.
func TestEscHTMLIsDeclaredOnce(t *testing.T) {
	src := readSource(t, templateSourceFile)

	n := strings.Count(src, "function escHTML(")
	if n != 1 {
		t.Errorf("expected exactly 1 declaration of escHTML, found %d", n)
	}
}

// TestTrendingGenreWiring verifies the discover page's filters are wired end to
// end: the chip containers, the loaders, the click handlers and the request.
func TestTrendingGenreWiring(t *testing.T) {
	src := readSource(t, templateSourceFile)

	for _, needle := range []string{
		`id="trending-filters"`,    // container the genre chips render into
		`id="trending-sorts"`,      // sort chips
		`id="trending-years"`,      // year chips
		"loadTrendingGenres()",     // fills the genre row
		"renderTrendingYears()",    // fills the year row
		"selectTrendingGenre(",     // genre click handler
		"selectTrendingSort(",      // sort click handler
		"selectTrendingYear(",      // year click handler
		"trendingGenre",            // genre state
		"trendingSort",             // sort state
		"trendingYear",             // year state
		"'/api/tmdb/genres?type='", // genre list request, per media type
		"'&genre='+trendingGenre",  // genre applied to the request
		"'&year='+trendingYear",    // year applied to the request
		"trendingQuery(",           // single place that builds the request
	} {
		if !strings.Contains(src, needle) {
			t.Errorf("discover page is missing %q; that filter would not work", needle)
		}
	}

	// The loaders must actually be invoked, not just defined.
	for _, call := range []string{"loadTrendingGenres()", "renderTrendingYears()"} {
		if !regexp.MustCompile(`(?m)^\s*` + regexp.QuoteMeta(call) + `;`).MatchString(src) {
			t.Errorf("%s is defined but never called, so its chips never appear", call)
		}
	}
}

// TestGenreListFollowsMediaType verifies the genre list is reloaded when the tab
// changes.
//
// The two genre lists differ, so keeping the old chips after switching tabs would
// offer genres the new media type does not have — and selecting one returns an
// empty list that looks like "no results".
func TestGenreListFollowsMediaType(t *testing.T) {
	src := readSource(t, templateSourceFile)

	i := strings.Index(src, "function switchTrendingTab(")
	if i < 0 {
		t.Fatal("switchTrendingTab not found")
	}
	end := strings.Index(src[i:], "\n    }")
	if end < 0 {
		end = len(src) - i
	}
	body := src[i : i+end]
	if !strings.Contains(body, "loadTrendingGenres()") {
		t.Error("switchTrendingTab must reload the genre list, since it is per media type")
	}
}
