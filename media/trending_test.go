package media

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// TestTrendingGenres pins the curated genre list.
//
// The ids are TMDB's own and must not change: they are sent as with_genres, and
// a wrong id silently returns unrelated titles — or, for TV, nothing at all.
func TestTrendingGenres(t *testing.T) {
	// film id -> (tv id, name)
	want := map[int]struct {
		tvID int
		name string
	}{
		16:    {16, "动画"},
		18:    {18, "剧情"},
		35:    {35, "喜剧"},
		28:    {10759, "动作"},
		99:    {99, "纪录"},
		878:   {10765, "科幻"},
		9648:  {9648, "悬疑"},
		10749: {10749, "爱情"},
	}
	got := TrendingGenres()
	if len(got) != len(want) {
		t.Fatalf("got %d genres, want %d", len(got), len(want))
	}
	for _, g := range got {
		w, ok := want[g.ID]
		if !ok {
			t.Errorf("unexpected genre id %d", g.ID)
			continue
		}
		if g.Name != w.name {
			t.Errorf("genre %d name = %q, want %q", g.ID, g.Name, w.name)
		}
		if g.TVID != w.tvID {
			t.Errorf("genre %d tv id = %d, want %d", g.ID, g.TVID, w.tvID)
		}
	}
	// Animation is the one this feature was asked for; make it first so it is
	// the easiest chip to reach.
	if got[0].ID != 16 {
		t.Errorf("first genre should be Animation (16), got %d", got[0].ID)
	}
}

// TestGenreIDForMediaType verifies film ids are remapped for TV where TMDB uses
// a different id.
//
// Sending the film id to /discover/tv does not error, it returns an empty list,
// which looks like "this genre has no series" rather than "wrong id". That is
// how Action and Sci-Fi showed 20 films and 0 series.
func TestGenreIDForMediaType(t *testing.T) {
	cases := []struct {
		genreID int
		media   string
		want    int
	}{
		{16, "movie", 16},
		{16, "tv", 16},
		{28, "movie", 28},
		{28, "tv", 10759}, // Action & Adventure
		{878, "movie", 878},
		{878, "tv", 10765}, // Sci-Fi & Fantasy
		{9648, "tv", 9648},
		// An id outside the curated table is passed through.
		{12345, "tv", 12345},
	}
	for _, c := range cases {
		if got := genreIDFor(c.genreID, c.media); got != c.want {
			t.Errorf("genreIDFor(%d, %q) = %d, want %d", c.genreID, c.media, got, c.want)
		}
	}
}

// TestTrendingFilteredSendsTVGenreID verifies the request for a TV genre uses the
// TV id, not the film id.
func TestTrendingFilteredSendsTVGenreID(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	// 878 (Sci-Fi for films) must become 10765 for TV.
	if _, err := c.TrendingFiltered("tv", 878, 1, SortPopularity); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Query().Get("with_genres"); got != "10765" {
		t.Errorf("with_genres = %q, want 10765 (the TV id for Sci-Fi)", got)
	}

	// The same genre for films keeps the film id.
	urls = nil
	if _, err := c.TrendingFiltered("movie", 878, 1, SortPopularity); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Query().Get("with_genres"); got != "878" {
		t.Errorf("with_genres = %q, want 878 (the film id for Sci-Fi)", got)
	}
}

// newTestClient points a TMDBClient at a test server and records requests.
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
	// Redirect every TMDB host to the test server.
	transport := &http.Transport{
		Proxy: func(*http.Request) (*url.URL, error) { return nil, nil },
	}
	_ = transport
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

// TestTrendingFilteredUsesTrendingEndpoint verifies that with no genre the
// trending endpoint is used, preserving TMDB's own ordering.
func TestTrendingFilteredUsesTrendingEndpoint(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{
				{"id": 1, "title": "A", "poster_path": "/a.jpg"},
			},
		})
	}, &urls)

	got, err := c.TrendingFiltered("movie", 0, 1, SortPopularity)
	if err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d results", len(got))
	}
	if got[0].MediaType != "movie" {
		t.Errorf("MediaType = %q, want movie", got[0].MediaType)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 request, got %d", len(urls))
	}
	if !strings.Contains(urls[0].Path, "/trending/movie/week") {
		t.Errorf("path = %q, want the trending endpoint", urls[0].Path)
	}
}

// TestTrendingFilteredUsesDiscoverWithGenre verifies a genre switches to the
// discover endpoint, which is the only one that accepts a genre filter.
func TestTrendingFilteredUsesDiscoverWithGenre(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"results": []map[string]interface{}{{"id": 2, "name": "B"}},
		})
	}, &urls)

	if _, err := c.TrendingFiltered("tv", 16, 2, SortPopularity); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if len(urls) != 1 {
		t.Fatalf("expected 1 request, got %d", len(urls))
	}
	u := urls[0]
	if !strings.Contains(u.Path, "/discover/tv") {
		t.Errorf("path = %q, want the discover endpoint", u.Path)
	}
	q := u.Query()
	if q.Get("with_genres") != "16" {
		t.Errorf("with_genres = %q, want 16", q.Get("with_genres"))
	}
	if q.Get("sort_by") != "popularity.desc" {
		t.Errorf("sort_by = %q, want popularity.desc", q.Get("sort_by"))
	}
	if q.Get("page") != "2" {
		t.Errorf("page = %q, want 2", q.Get("page"))
	}
	// A vote floor is set: without it TMDB hides most recent anime.
	if q.Get("vote_count.gte") == "" {
		t.Error("vote_count.gte should be set so recent titles are not filtered out")
	}
}

// TestTrendingFilteredRejectsUnknownMediaType verifies a typo cannot build a
// request to a nonsense path.
func TestTrendingFilteredRejectsUnknownMediaType(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {}, nil)
	if _, err := c.TrendingFiltered("anime", 16, 1, SortPopularity); err == nil {
		t.Fatal("expected an error for an unsupported media type")
	}
}

// TestTrendingFilteredClampsPage verifies a bad page number cannot be sent.
func TestTrendingFilteredClampsPage(t *testing.T) {
	var urls []*url.URL
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]interface{}{"results": []interface{}{}})
	}, &urls)

	if _, err := c.TrendingFiltered("movie", 0, 0, SortPopularity); err != nil {
		t.Fatalf("TrendingFiltered: %v", err)
	}
	if got := urls[0].Query().Get("page"); got != "1" {
		t.Errorf("page = %q, want 1", got)
	}
}

// TestTrendingFilteredReportsHTTPError verifies a non-200 is surfaced instead of
// being decoded into an empty list, which would look like "no results".
func TestTrendingFilteredReportsHTTPError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		w.Write([]byte(`{"status_message":"invalid api key"}`))
	}, nil)

	if _, err := c.TrendingFiltered("movie", 16, 1, SortPopularity); err == nil {
		t.Fatal("expected an error for HTTP 401")
	}
}

// TestTrendingFilteredRequiresKey verifies an unconfigured client is reported
// rather than issuing a request with an empty key.
func TestTrendingFilteredRequiresKey(t *testing.T) {
	c := &TMDBClient{}
	if _, err := c.TrendingFiltered("movie", 16, 1, SortPopularity); err == nil {
		t.Fatal("expected an error without an API key")
	}
}
