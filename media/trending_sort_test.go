package media

import (
	"encoding/json"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// TestParseTrendingSort verifies an unknown value degrades to popularity.
//
// A bad query parameter should not empty the panel: the default list is a
// reasonable answer, an error page is not.
func TestParseTrendingSort(t *testing.T) {
	cases := map[string]TrendingSort{
		"date":       SortDate,
		"popularity": SortPopularity,
		"":           SortPopularity,
		"DATE":       SortPopularity, // case-sensitive on purpose: the client sends lowercase
		"nonsense":   SortPopularity,
	}
	for in, want := range cases {
		if got := ParseTrendingSort(in); got != want {
			t.Errorf("ParseTrendingSort(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDateSortUsesDiscover verifies date ordering switches to the discover
// endpoint, since /trending takes no sort parameter.
func TestDateSortUsesDiscover(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("tv", 0, 1, SortDate); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	u := urls[0]
	if u.Path != "/3/discover/tv" {
		t.Errorf("path = %q, want /3/discover/tv", u.Path)
	}
	// TV uses first_air_date, not primary_release_date.
	if got := u.Query().Get("sort_by"); got != "first_air_date.desc" {
		t.Errorf("sort_by = %q, want first_air_date.desc", got)
	}
}

// TestDateSortUsesFilmDateField verifies films sort by release date.
func TestDateSortUsesFilmDateField(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("movie", 0, 1, SortDate); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Query().Get("sort_by"); got != "primary_release_date.desc" {
		t.Errorf("sort_by = %q, want primary_release_date.desc", got)
	}
}

// TestDateSortExcludesFutureTitles verifies the upper date bound is set.
//
// A title that has not been released yet has nothing to search for, so listing
// it in a "new releases" view is noise.
func TestDateSortExcludesFutureTitles(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("tv", 0, 1, SortDate); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	got := urls[0].Query().Get("first_air_date.lte")
	if got == "" {
		t.Fatal("first_air_date.lte should be set to keep future shows out")
	}
	// It must be a real date, and not in the future.
	when, err := time.Parse("2006-01-02", got)
	if err != nil {
		t.Fatalf("first_air_date.lte = %q is not a date: %v", got, err)
	}
	if when.After(time.Now().Add(24 * time.Hour)) {
		t.Errorf("first_air_date.lte = %q is in the future", got)
	}
}

// TestDateSortKeepsUnratedTitles verifies the vote floor is dropped for the
// newest view.
//
// TMDB's default vote filter hides most of the current season, which is exactly
// what this view exists to show.
func TestDateSortKeepsUnratedTitles(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("tv", 0, 1, SortDate); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Query().Get("vote_count.gte"); got != "0" {
		t.Errorf("vote_count.gte = %q, want 0 so recent unrated shows still appear", got)
	}
}

// TestDateSortCombinesWithGenre verifies the new-anime preset sends both the
// genre and the date ordering in one request.
func TestDateSortCombinesWithGenre(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	// Animation (16), newest first, TV.
	if _, err := c.TrendingFiltered("tv", 16, 1, SortDate); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	q := urls[0].Query()
	if q.Get("with_genres") != "16" {
		t.Errorf("with_genres = %q, want 16", q.Get("with_genres"))
	}
	if q.Get("sort_by") != "first_air_date.desc" {
		t.Errorf("sort_by = %q, want first_air_date.desc", q.Get("sort_by"))
	}
}

// TestPopularitySortUnchanged verifies the default path still uses /trending.
//
// The plain view is what the panel shows on load; adding sort support must not
// quietly change it.
func TestPopularitySortUnchanged(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("movie", 0, 1, SortPopularity); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Path; got != "/3/trending/movie/week" {
		t.Errorf("path = %q, want /3/trending/movie/week", got)
	}
}
