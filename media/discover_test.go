package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

// --- genre tables ---

// TestGenreTablesArePerMediaType pins the two genre lists.
//
// TMDB keeps them apart, and they overlap only partly: films have Horror and
// Thriller, series have Kids and Reality, and Action is 28 for films but 10759
// for series. Sending a film id to /discover/tv returns an empty list rather than
// an error, so the separation has to be real.
func TestGenreTablesArePerMediaType(t *testing.T) {
	movies := TrendingGenres("movie")
	tv := TrendingGenres("tv")

	if len(movies) < 15 {
		t.Errorf("film genre list looks incomplete: %d entries", len(movies))
	}
	if len(tv) < 12 {
		t.Errorf("TV genre list looks incomplete: %d entries", len(tv))
	}

	// Spot-check ids that must be right, since a wrong one silently returns
	// unrelated titles.
	wantMovie := map[int]string{28: "动作", 16: "动画", 27: "恐怖", 53: "惊悚"}
	for id, name := range wantMovie {
		if !hasGenre(movies, id, name) {
			t.Errorf("film genres missing %d=%q", id, name)
		}
	}
	wantTV := map[int]string{10759: "动作冒险", 16: "动画", 10765: "科幻奇幻", 10764: "真人秀"}
	for id, name := range wantTV {
		if !hasGenre(tv, id, name) {
			t.Errorf("TV genres missing %d=%q", id, name)
		}
	}

	// The two lists must not be the same: if they were, the per-type split would
	// be pointless.
	if len(movies) == len(tv) && movies[0].ID == tv[0].ID {
		t.Error("the film and TV genre lists look identical")
	}
}

// TestGenreValidForRejectsCrossType verifies a film-only genre is rejected for TV.
//
// This is what stops "Horror" from emptying the TV side while looking like "no
// results".
func TestGenreValidForRejectsCrossType(t *testing.T) {
	// Horror (27) exists for films only.
	if !genreValidFor("movie", 27) {
		t.Error("Horror should be a film genre")
	}
	if genreValidFor("tv", 27) {
		t.Error("Horror is not a TV genre and must be rejected")
	}
	// Reality (10764) exists for TV only.
	if genreValidFor("movie", 10764) {
		t.Error("Reality is not a film genre and must be rejected")
	}
	if !genreValidFor("tv", 10764) {
		t.Error("Reality should be a TV genre")
	}
	// Animation exists for both.
	if !genreValidFor("movie", 16) || !genreValidFor("tv", 16) {
		t.Error("Animation should exist for both media types")
	}
}

func hasGenre(list []TrendingGenre, id int, name string) bool {
	for _, g := range list {
		if g.ID == id {
			return g.Name == name
		}
	}
	return false
}

// --- sort parsing ---

// TestParseTrendingSort verifies an unknown value degrades to popularity.
func TestParseTrendingSort(t *testing.T) {
	cases := map[string]TrendingSort{
		"date":       SortDate,
		"popularity": SortPopularity,
		"":           SortPopularity,
		"DATE":       SortPopularity,
		"nonsense":   SortPopularity,
	}
	for in, want := range cases {
		if got := ParseTrendingSort(in); got != want {
			t.Errorf("ParseTrendingSort(%q) = %q, want %q", in, got, want)
		}
	}
}

// --- endpoint selection ---

// TestUnfilteredUsesTrending verifies the plain view keeps TMDB's own ordering.
func TestUnfilteredUsesTrending(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{MediaType: "movie"}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := urls[0].Path; got != "/3/trending/movie/week" {
		t.Errorf("path = %q, want /3/trending/movie/week", got)
	}
}

// TestEachFilterSwitchesToDiscover verifies genre, date sort and year each move
// the request to the discover endpoint, which is the only one that accepts them.
func TestEachFilterSwitchesToDiscover(t *testing.T) {
	cases := []struct {
		name string
		q    DiscoverQuery
	}{
		{"genre", DiscoverQuery{MediaType: "movie", GenreID: 16}},
		{"date", DiscoverQuery{MediaType: "movie", Sort: SortDate}},
		{"year", DiscoverQuery{MediaType: "movie", Year: 2024}},
	}
	for _, c := range cases {
		var urls []*url.URL
		client := newTestClient(t, emptyResults, &urls)
		if _, err := client.Discover(c.q); err != nil {
			t.Fatalf("%s: Discover: %v", c.name, err)
		}
		if got := urls[0].Path; got != "/3/discover/movie" {
			t.Errorf("%s: path = %q, want /3/discover/movie", c.name, got)
		}
	}
}

// --- date ordering ---

// TestDateSortUsesPerTypeField verifies the date field differs between media.
func TestDateSortUsesPerTypeField(t *testing.T) {
	cases := []struct {
		media string
		want  string
	}{
		{"movie", "primary_release_date.desc"},
		{"tv", "first_air_date.desc"},
	}
	for _, c := range cases {
		var urls []*url.URL
		client := newTestClient(t, emptyResults, &urls)
		if _, err := client.Discover(DiscoverQuery{MediaType: c.media, Sort: SortDate}); err != nil {
			t.Fatalf("%s: Discover: %v", c.media, err)
		}
		if got := urls[0].Query().Get("sort_by"); got != c.want {
			t.Errorf("%s: sort_by = %q, want %q", c.media, got, c.want)
		}
	}
}

// TestDateSortExcludesFutureTitles verifies the upper bound is today.
func TestDateSortExcludesFutureTitles(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{MediaType: "tv", Sort: SortDate}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	got := urls[0].Query().Get("first_air_date.lte")
	if got == "" {
		t.Fatal("first_air_date.lte should be set so future shows are excluded")
	}
	when, err := time.Parse("2006-01-02", got)
	if err != nil {
		t.Fatalf("first_air_date.lte = %q is not a date: %v", got, err)
	}
	if when.After(time.Now().Add(24 * time.Hour)) {
		t.Errorf("first_air_date.lte = %q is in the future", got)
	}
}

// TestDateSortKeepsUnratedTitles verifies the vote floor is dropped for newest.
func TestDateSortKeepsUnratedTitles(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{MediaType: "tv", Sort: SortDate}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := urls[0].Query().Get("vote_count.gte"); got != "0" {
		t.Errorf("vote_count.gte = %q, want 0 so recent unrated shows still appear", got)
	}
}

// --- year ---

// TestYearBecomesDateRange verifies the year is sent as a range.
//
// TMDB has no year parameter; a year has to be expressed as first/last day.
func TestYearBecomesDateRange(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{MediaType: "movie", Year: 2024}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	q := urls[0].Query()
	if got := q.Get("primary_release_date.gte"); got != "2024-01-01" {
		t.Errorf("primary_release_date.gte = %q, want 2024-01-01", got)
	}
	if got := q.Get("primary_release_date.lte"); got != "2024-12-31" {
		t.Errorf("primary_release_date.lte = %q, want 2024-12-31", got)
	}
}

// TestYearOverridesDateSortBound verifies a specific year wins over "today".
//
// Without this, asking for 2024 while sorted by newest would bound the range at
// both 2024-12-31 and today, and the narrower of the two would silently apply.
func TestYearOverridesDateSortBound(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{MediaType: "movie", Sort: SortDate, Year: 2020}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := urls[0].Query().Get("primary_release_date.lte"); got != "2020-12-31" {
		t.Errorf("lte = %q, want 2020-12-31 (the selected year, not today)", got)
	}
}

// --- combinations and validation ---

// TestFiltersCombine verifies genre, year and sort are sent together.
func TestFiltersCombine(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)

	if _, err := c.Discover(DiscoverQuery{
		MediaType: "tv", GenreID: 16, Sort: SortDate, Year: 2025,
	}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	q := urls[0].Query()
	if q.Get("with_genres") != "16" {
		t.Errorf("with_genres = %q, want 16", q.Get("with_genres"))
	}
	if q.Get("sort_by") != "first_air_date.desc" {
		t.Errorf("sort_by = %q", q.Get("sort_by"))
	}
	if q.Get("first_air_date.gte") != "2025-01-01" {
		t.Errorf("gte = %q", q.Get("first_air_date.gte"))
	}
}

// TestRejectsGenreFromOtherMediaType verifies a mismatched genre is an error, not
// an empty list.
func TestRejectsGenreFromOtherMediaType(t *testing.T) {
	c := newTestClient(t, emptyResults, nil)
	// Horror (27) is a film genre; asking for it as TV must fail loudly.
	if _, err := c.Discover(DiscoverQuery{MediaType: "tv", GenreID: 27}); err == nil {
		t.Fatal("expected an error for a film-only genre on TV")
	}
}

// TestRejectsUnknownMediaType verifies a typo cannot build a nonsense path.
func TestRejectsUnknownMediaType(t *testing.T) {
	c := newTestClient(t, emptyResults, nil)
	if _, err := c.Discover(DiscoverQuery{MediaType: "anime"}); err == nil {
		t.Fatal("expected an error for an unsupported media type")
	}
}

// TestClampsPage verifies a bad page number cannot be sent.
func TestClampsPage(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, emptyResults, &urls)
	if _, err := c.Discover(DiscoverQuery{MediaType: "movie", Page: 0}); err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if got := urls[0].Query().Get("page"); got != "1" {
		t.Errorf("page = %q, want 1", got)
	}
}

// TestReportsHTTPError verifies a non-200 is surfaced rather than decoded into an
// empty list, which would look like "no results".
func TestReportsHTTPError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status_message":"invalid api key"}`))
	}, nil)
	if _, err := c.Discover(DiscoverQuery{MediaType: "movie", GenreID: 16}); err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
}

// TestRequiresKey verifies an unconfigured client is reported.
func TestRequiresKey(t *testing.T) {
	c := &TMDBClient{}
	if _, err := c.Discover(DiscoverQuery{MediaType: "movie"}); err == nil {
		t.Fatal("expected an error without an API key")
	}
}

// emptyResults is the handler used by tests that only inspect the request.
func emptyResults(w http.ResponseWriter, r *http.Request) {
	json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
}

// newTestClient points a TMDBClient at a test server and records requests.
//
// The real host is redirected by a custom RoundTripper rather than by changing
// the client's URLs, so the code under test builds exactly the request it would
// send in production and only the transport is swapped.
func newTestClient(t *testing.T, handler http.HandlerFunc, captured *[]*url.URL) *TMDBClient {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if captured != nil {
			*captured = append(*captured, r.URL)
		}
		handler(w, r)
	}))
	t.Cleanup(srv.Close)

	c := NewTMDBClient("testkey")
	c.http = &http.Client{Transport: rewriteTransport{srv.URL, http.DefaultTransport}}
	return c
}

// rewriteTransport sends api.themoviedb.org requests to the test server.
type rewriteTransport struct {
	base string
	next http.RoundTripper
}

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.base)
	if err != nil {
		return nil, err
	}
	r2 := r.Clone(r.Context())
	r2.URL.Scheme = u.Scheme
	r2.URL.Host = u.Host
	return t.next.RoundTrip(r2)
}
