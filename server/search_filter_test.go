package server

import (
	"strings"
	"testing"

	"github.com/lingeringdawn/pan-fetcher/indexer"
)

// titles builds a result slice from bare titles, which is all the filter looks at.
func titles(ts ...string) []indexer.SearchResult {
	out := make([]indexer.SearchResult, 0, len(ts))
	for _, t := range ts {
		out = append(out, indexer.SearchResult{Title: t})
	}
	return out
}

// gotTitles extracts the titles from a result slice.
func gotTitles(rs []indexer.SearchResult) []string {
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.Title)
	}
	return out
}

// TestApplyTitleFilterDropsUnrelated is the regression test for the noise
// reported in issue #1: a short title such as "TARI TARI" made NYAA return a
// long tail of unrelated releases.
func TestApplyTitleFilterDropsUnrelated(t *testing.T) {
	in := titles(
		"[LoliHouse] TARI TARI - 01 [WebRip 1080p HEVC-10bit AAC].mkv",
		"TARI.TARI.2012.1080p.BluRay.x264",
		"TARI_TARI_BD_01",
		"[Group] Tari Tari - 02 (720p)",
		// Unrelated: shares no term with the query.
		"[Group] Some Other Anime - 07 [1080p]",
		"Completely Different Movie 2020 1080p",
		// Shares the word once, but this is a different show: "TARI TARI" is the
		// name, so a title that says it once is not it.
		"TARI - 01 [1080p]",
	)
	got := gotTitles(applyTitleFilter(in, "TARI TARI"))

	want := 4 // the four TARI TARI entries
	if len(got) != want {
		t.Fatalf("kept %d results, want %d: %v", len(got), want, got)
	}
	for _, g := range got {
		if strings.Count(normalizeForMatch(g), "tari") < 2 {
			t.Errorf("kept a title that does not repeat the name: %q", g)
		}
	}
}

// TestApplyTitleFilterRequiresRepeatedTerm pins the multiplicity rule.
//
// "TARI TARI" is the title of the show, not an emphasis on "TARI". Collapsing
// the repeated word (an earlier implementation did) let single-occurrence
// releases through, which is exactly the noise the filter exists to remove.
func TestApplyTitleFilterRequiresRepeatedTerm(t *testing.T) {
	if got := queryTerms("TARI TARI"); len(got) != 2 {
		t.Fatalf("repeated term must be kept as written, got %v", got)
	}

	in := titles(
		"TARI TARI - 01",   // correct: appears twice
		"TARITARI - 02",    // correct: concatenated, still twice
		"TARI.TARI.03",     // correct: punctuation is whitespace
		"TARI - 04",        // wrong: once only
		"TARITARI",         // correct: concatenated
		"TA RI TA RI",      // wrong: split across word boundaries
	)
	got := gotTitles(applyTitleFilter(in, "TARI TARI"))

	want := map[string]bool{
		"TARI TARI - 01": true,
		"TARITARI - 02":  true,
		"TARI.TARI.03":   true,
		"TARITARI":       true,
	}
	if len(got) != len(want) {
		t.Fatalf("kept %d, want %d: %v", len(got), len(want), got)
	}
	for _, g := range got {
		if !want[g] {
			t.Errorf("unexpected title kept: %q", g)
		}
	}
}

// TestApplyTitleFilterTermOrder verifies the terms must appear in the order
// given, which is how a title reads.
func TestApplyTitleFilterTermOrder(t *testing.T) {
	if got := gotTitles(applyTitleFilter(titles("BARI TARI - 01"), "TARI BARI")); len(got) != 0 {
		t.Errorf("reversed order should not match, got %v", got)
	}
	if got := gotTitles(applyTitleFilter(titles("TARI BARI - 01"), "TARI BARI")); len(got) != 1 {
		t.Errorf("matching order should match, got %v", got)
	}
}

// TestApplyTitleFilterNormalization covers the punctuation and spacing
// differences between a query and how releases name the same thing.
func TestApplyTitleFilterNormalization(t *testing.T) {
	in := titles(
		"TARI.TARI.2012.1080p",
		"TARI_TARI_2012",
		"TARI-TARI-2012",
		"TARI[TARI]2012",
		"tari tari 2012",
		"[Group] TARI  TARI  2012", // extra whitespace
	)
	got := applyTitleFilter(in, "TARI TARI")
	if len(got) != len(in) {
		t.Errorf("normalisation should accept all %d forms, kept %d: %v", len(in), len(got), gotTitles(got))
	}
}

// TestApplyTitleFilterCJK verifies Chinese titles are matched, since the
// normaliser must not strip non-ASCII runes as punctuation.
func TestApplyTitleFilterCJK(t *testing.T) {
	in := titles(
		"[Nekomoe kissaten] 侦探已死 - 01 [1080p]",
		"侦探已死 第01话",
		"完全无关的另一部动画 01",
	)
	got := gotTitles(applyTitleFilter(in, "侦探已死"))
	if len(got) != 2 {
		t.Fatalf("kept %d, want 2: %v", len(got), got)
	}
	for _, g := range got {
		if !strings.Contains(g, "侦探已死") {
			t.Errorf("unrelated title kept: %q", g)
		}
	}
}

// TestApplyTitleFilterTermsNotAdjacent pins the deliberate looseness: a title
// that separates the terms with other words still matches, as long as each
// occurrence is present and in order.
func TestApplyTitleFilterTermsNotAdjacent(t *testing.T) {
	in := titles("[Group] TARI the animation TARI")
	got := applyTitleFilter(in, "TARI TARI")
	if len(got) != 1 {
		t.Errorf("non-adjacent but ordered occurrences should match, got %v", gotTitles(got))
	}
}

// TestApplyTitleFilterEmptyQuery verifies an empty query disables the filter:
// with no terms there is nothing to judge against, and dropping everything
// would be the wrong default.
func TestApplyTitleFilterEmptyQuery(t *testing.T) {
	in := titles("a", "b", "c")
	for _, q := range []string{"", "   ", "\t\n"} {
		if got := applyTitleFilter(in, q); len(got) != len(in) {
			t.Errorf("query %q: kept %d, want all %d", q, len(got), len(in))
		}
	}
}

// TestApplyTitleFilterSingleRuneTerms ensures one-character terms are ignored
// rather than making the filter useless by matching nearly everything.
func TestApplyTitleFilterSingleRuneTerms(t *testing.T) {
	in := titles("anything at all", "another title")
	// "a b" are single characters; with no usable terms the list passes through.
	if got := applyTitleFilter(in, "a b"); len(got) != len(in) {
		t.Errorf("single-rune terms should be ignored, kept %d of %d", len(got), len(in))
	}
	// A single-char term alongside a real one: only the real one constrains.
	got := applyTitleFilter(titles("TARI TARI", "no match"), "TARI x")
	if len(got) != 1 {
		t.Errorf("expected only the real term to constrain, got %v", gotTitles(got))
	}
}

// TestQueryTermsStripsPunctuation covers operator-like punctuation a user may
// paste from a keyword expression.
func TestQueryTermsStripsPunctuation(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"TARI TARI", 2}, // repeated term is kept as written
		{"TARI BARI", 2},
		{"  TARI   TARI  ", 2},
		{`"TARI TARI"`, 2},
		{"group:NEST", 1},
		{"a b c", 0}, // all single characters
		{"", 0},
	}
	for _, c := range cases {
		if got := len(queryTerms(c.query)); got != c.want {
			t.Errorf("queryTerms(%q) returned %d terms, want %d: %v", c.query, got, c.want, queryTerms(c.query))
		}
	}
}

// TestNormalizeForMatch pins the normaliser, which is what makes dotted and
// underscored release names match a spaced query.
func TestNormalizeForMatch(t *testing.T) {
	cases := []struct{ in, want string }{
		{"TARI.TARI.2012", "tari tari 2012"},
		{"TARI_TARI", "tari tari"},
		{"TARI-TARI", "tari tari"},
		{"TARI[TARI]", "tari tari"},
		{"  TARI   TARI  ", "tari tari"},
		{"[Group] TARI TARI - 01", "group tari tari 01"},
		{"侦探已死", "侦探已死"},
		{"", ""},
		{"...", ""},
	}
	for _, c := range cases {
		if got := normalizeForMatch(c.in); got != c.want {
			t.Errorf("normalizeForMatch(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestFilterSearchTitleDefaultsOn is the important compatibility property: a
// config written before this setting existed must have the filter enabled, not
// silently disabled by the zero value of a bool.
func TestFilterSearchTitleDefaultsOn(t *testing.T) {
	var ws webSettings
	if !ws.filterSearchTitle() {
		t.Error("an unset FilterSearchTitle must default to enabled")
	}

	ws.FilterSearchTitle = boolPtr(true)
	if !ws.filterSearchTitle() {
		t.Error("explicit true must be enabled")
	}

	ws.FilterSearchTitle = boolPtr(false)
	if ws.filterSearchTitle() {
		t.Error("explicit false must be disabled")
	}
}

// TestTitleFilterPreservesOrderAndFields verifies the filter only removes
// entries and does not disturb the ones it keeps.
func TestTitleFilterPreservesOrderAndFields(t *testing.T) {
	in := []indexer.SearchResult{
		{Title: "TARI TARI 01", Seeders: 10, IndexerName: "nyaasi"},
		{Title: "unrelated", Seeders: 99, IndexerName: "other"},
		{Title: "TARI TARI 02", Seeders: 5, IndexerName: "dmhy"},
	}
	got := applyTitleFilter(in, "TARI TARI")
	if len(got) != 2 {
		t.Fatalf("kept %d, want 2", len(got))
	}
	if got[0].Title != "TARI TARI 01" || got[1].Title != "TARI TARI 02" {
		t.Errorf("order changed: %v", gotTitles(got))
	}
	if got[0].Seeders != 10 || got[0].IndexerName != "nyaasi" {
		t.Errorf("fields lost: %+v", got[0])
	}
}

// TestTitleFilterHandlesUnicodeCase verifies case folding works for non-ASCII
// input, which strings.ToLower handles but a naive byte comparison would not.
func TestTitleFilterHandlesUnicodeCase(t *testing.T) {
	in := titles("СТАЛКЕР 01", "сталкер 02", "другое")
	got := applyTitleFilter(in, "сталкер")
	if len(got) != 2 {
		t.Errorf("Cyrillic case folding failed, kept %v", gotTitles(got))
	}
}
