package media

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TMDBClient wraps the TMDB API v3.
type TMDBClient struct {
	apiKey string
	http   *http.Client
	cache  sync.Map
}

// TMDBResult is a search or detail result from TMDB.
type TMDBResult struct {
	TMDBID        int      `json:"id"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"original_title"`
	Name          string   `json:"name"`
	OriginalName  string   `json:"original_name"`
	MediaType     string   `json:"media_type"`
	Year       string   `json:"release_date"`
	FirstAir   string   `json:"first_air_date"`
	Overview   string   `json:"overview"`
	PosterPath string   `json:"poster_path"`
	Backdrop   string   `json:"backdrop_path"`
	VoteAvg    float64  `json:"vote_average"`
	VoteCount  int      `json:"vote_count"`
	Runtime    int      `json:"runtime"`
	NumSeasons int      `json:"number_of_seasons"`
	NumEps     int      `json:"number_of_episodes"`
	Status     string   `json:"status"`
	Tagline    string   `json:"tagline"`
	Homepage   string   `json:"homepage"`
	AltTitles  []string `json:"-"` // populated from alternative_titles
	Seasons    []TMDBSeason `json:"seasons,omitempty"`
	Genres     []TMDBGenre  `json:"genres,omitempty"`
}

// PosterURL returns the full poster image URL (w200 size).
func (r *TMDBResult) PosterURL() string {
	if r.PosterPath == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w200" + r.PosterPath
}

// DisplayName returns the best display name.
func (r *TMDBResult) DisplayName() string {
	if r.Title != "" {
		return r.Title
	}
	return r.Name
}

// YearStr extracts the year from the date string.
func (r *TMDBResult) YearStr() string {
	date := r.FirstAir
	if date == "" {
		date = r.Year
	}
	if len(date) >= 4 {
		return date[:4]
	}
	return ""
}

// SearchKeyword returns the best keyword for aggregator search (prefers original title).
func (r *TMDBResult) SearchKeyword() string {
	if r.OriginalTitle != "" && r.OriginalTitle != r.Title {
		return r.OriginalTitle
	}
	if r.OriginalName != "" && r.OriginalName != r.Name {
		return r.OriginalName
	}
	return r.DisplayName()
}

// EpisodeCount returns total episode count across all seasons.
func (r *TMDBResult) EpisodeCount() int {
	total := 0
	for _, s := range r.Seasons {
		if s.SeasonNumber > 0 {
			total += s.EpisodeCount
		}
	}
	return total
}

type TMDBSeason struct {
	SeasonNumber int `json:"season_number"`
	EpisodeCount int `json:"episode_count"`
}

type TMDBGenre struct {
	Name string `json:"name"`
}

type tmdbSearchResp struct {
	Results []TMDBResult `json:"results"`
}

// NewTMDBClient creates a new TMDB API client.
func NewTMDBClient(apiKey string) *TMDBClient {
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
	}
	if tmdbProxyURL != "" {
		if u, err := url.Parse(tmdbProxyURL); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	}
	return &TMDBClient{
		apiKey: apiKey,
		http:   &http.Client{Timeout: 10 * time.Second, Transport: transport},
	}
}

var tmdbProxyURL string

// SetTMDBProxy sets the HTTP proxy for TMDB API calls.
func SetTMDBProxy(proxyURL string) {
	tmdbProxyURL = proxyURL
	// Re-init default client if already created
	if DefaultTMDB != nil {
		DefaultTMDB = NewTMDBClient(DefaultTMDB.apiKey)
	}
}

// Search searches TMDB for a title.
func (c *TMDBClient) Search(query string, mediaType string) (*TMDBResult, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}

	cacheKey := mediaType + ":" + query
	if cached, ok := c.cache.Load(cacheKey); ok {
		return cached.(*TMDBResult), nil
	}

	var results []TMDBResult
	var err error

	switch mediaType {
	case "tv", "anime":
		results, err = c.searchTV(query)
	default:
		results, err = c.searchMulti(query)
	}
	if err != nil {
		return nil, err
	}
	if len(results) == 0 {
		return nil, fmt.Errorf("no results for %q", query)
	}

	best := &results[0]
	// Get details for best match
	c.fillDetails(best)

	c.cache.Store(cacheKey, best)
	return best, nil
}

func (c *TMDBClient) searchTV(query string) ([]TMDBResult, error) {
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("query", query)
	params.Set("language", "zh-CN")

	reqURL := "https://api.themoviedb.org/3/search/tv?" + params.Encode()
	resp, err := c.http.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r tmdbSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	for i := range r.Results {
		r.Results[i].MediaType = "tv"
	}
	return r.Results, nil
}

func (c *TMDBClient) searchMulti(query string) ([]TMDBResult, error) {
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("query", query)
	params.Set("language", "zh-CN")

	reqURL := "https://api.themoviedb.org/3/search/multi?" + params.Encode()
	resp, err := c.http.Get(reqURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var r tmdbSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return nil, err
	}
	return r.Results, nil
}

func (c *TMDBClient) fillDetails(result *TMDBResult) {
	if result == nil {
		return
	}
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("language", "zh-CN")

	var detailURL string
	if result.MediaType == "tv" {
		detailURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?%s", result.TMDBID, params.Encode())
	} else {
		detailURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?%s", result.TMDBID, params.Encode())
	}

	resp, err := c.http.Get(detailURL)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	var detail TMDBResult
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return
	}
	result.Seasons = detail.Seasons
	result.Genres = detail.Genres
	result.FirstAir = detail.FirstAir
	result.Overview = detail.Overview
}

// BackdropURL returns the full backdrop image URL (w780 size).
func (r *TMDBResult) BackdropURL() string {
	if r.Backdrop == "" {
		return ""
	}
	return "https://image.tmdb.org/t/p/w780" + r.Backdrop
}

// GetDetail fetches full details for a movie or TV show.
func (c *TMDBClient) GetDetail(tmdbID int, mediaType string) (*TMDBResult, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("language", "zh-CN")

	var detailURL string
	if mediaType == "tv" {
		detailURL = fmt.Sprintf("https://api.themoviedb.org/3/tv/%d?%s", tmdbID, params.Encode())
	} else {
		detailURL = fmt.Sprintf("https://api.themoviedb.org/3/movie/%d?%s", tmdbID, params.Encode())
	}

	resp, err := c.http.Get(detailURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var detail TMDBResult
	if err := json.NewDecoder(resp.Body).Decode(&detail); err != nil {
		return nil, err
	}
	detail.MediaType = mediaType
	detail.TMDBID = tmdbID
	return &detail, nil
}

// BestMatch searches TMDB and returns the best matching title and its TMDB ID.
// Used to enrich subscription matching.
// DefaultTMDB is the global TMDB client instance. Call InitTMDB to configure.
var DefaultTMDB *TMDBClient

// InitTMDB configures the global TMDB client.
func InitTMDB(apiKey string) {
	if apiKey != "" {
		DefaultTMDB = NewTMDBClient(apiKey)
	}
}

// SearchAll searches both movies and TV shows from TMDB, returning top results.
func (c *TMDBClient) SearchAll(query string) (movies, tvShows []TMDBResult, err error) {
	if c == nil || c.apiKey == "" {
		return nil, nil, fmt.Errorf("TMDB API key not configured")
	}
	// Search movies
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("query", query)
	params.Set("language", "zh-CN")

	movieURL := "https://api.themoviedb.org/3/search/movie?" + params.Encode()
	resp, err := c.http.Get(movieURL)
	if err != nil {
		return nil, nil, err
	}
	var mr tmdbSearchResp
	json.NewDecoder(resp.Body).Decode(&mr)
	resp.Body.Close()
	for i := range mr.Results {
		mr.Results[i].MediaType = "movie"
	}
	movies = mr.Results
	if len(movies) > 8 {
		movies = movies[:8]
	}

	// Search TV
	tvURL := "https://api.themoviedb.org/3/search/tv?" + params.Encode()
	resp, err = c.http.Get(tvURL)
	if err != nil {
		return movies, nil, nil // movies still ok
	}
	var tr tmdbSearchResp
	json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	for i := range tr.Results {
		tr.Results[i].MediaType = "tv"
	}
	tvShows = tr.Results
	if len(tvShows) > 8 {
		tvShows = tvShows[:8]
	}
	return movies, tvShows, nil
}

// TrendingGenre is a genre a caller can filter the discover lists by.
//
// TMDB keeps a separate genre list per media type, and the two overlap only
// partly: films have Horror, Thriller and War, series have Kids, Reality, Soap
// and Talk, and a few names that exist in both use different ids (Action is 28
// for films but 10759 for series; Sci-Fi is 878 versus 10765). Sending a film id
// to /discover/tv does not error — it returns nothing — so the two lists are kept
// apart rather than merged.
type TrendingGenre struct {
	ID   int
	Name string
}

// movieGenres is TMDB's film genre list, in a reading order rather than by id.
func movieGenres() []TrendingGenre {
	return []TrendingGenre{
		{28, "动作"},
		{12, "冒险"},
		{16, "动画"},
		{35, "喜剧"},
		{80, "犯罪"},
		{99, "纪录"},
		{18, "剧情"},
		{10751, "家庭"},
		{14, "奇幻"},
		{36, "历史"},
		{27, "恐怖"},
		{10402, "音乐"},
		{9648, "悬疑"},
		{10749, "爱情"},
		{878, "科幻"},
		{53, "惊悚"},
		{10752, "战争"},
		{37, "西部"},
	}
}

// tvGenres is TMDB's series genre list, in a reading order rather than by id.
func tvGenres() []TrendingGenre {
	return []TrendingGenre{
		{10759, "动作冒险"},
		{16, "动画"},
		{35, "喜剧"},
		{80, "犯罪"},
		{99, "纪录"},
		{18, "剧情"},
		{10751, "家庭"},
		{10762, "儿童"},
		{9648, "悬疑"},
		{10763, "新闻"},
		{10764, "真人秀"},
		{10765, "科幻奇幻"},
		{10766, "肥皂剧"},
		{10767, "脱口秀"},
		{10768, "战争政治"},
		{37, "西部"},
	}
}

// TrendingGenres returns the genre list for one media type.
//
// An unknown type gets the film list, since that is the larger of the two and
// the one a caller is most likely to mean.
func TrendingGenres(mediaType string) []TrendingGenre {
	if mediaType == "tv" {
		return tvGenres()
	}
	return movieGenres()
}

// genreValidFor reports whether id is a genre of the given media type.
//
// The check exists because the two lists are not interchangeable: filtering TV by
// a film-only id silently returns an empty list, which reads as "nothing here"
// rather than "wrong id".
func genreValidFor(mediaType string, id int) bool {
	for _, g := range TrendingGenres(mediaType) {
		if g.ID == id {
			return true
		}
	}
	return false
}

// TrendingSort selects how discover results are ordered.
type TrendingSort string

const (
	// SortPopularity orders by TMDB popularity. It is the default, and it is
	// what "trending" means for a genre.
	SortPopularity TrendingSort = "popularity"
	// SortDate orders by release / first-air date, newest first.
	SortDate TrendingSort = "date"
)

// ParseTrendingSort maps a query value onto a known sort.
//
// Anything unrecognised falls back to popularity rather than erroring: a bad
// value should degrade to the default list, not leave the panel empty.
func ParseTrendingSort(s string) TrendingSort {
	if s == string(SortDate) {
		return SortDate
	}
	return SortPopularity
}

// dateField is the discover parameter TMDB uses for a release date, which
// differs between films and series.
func dateField(mediaType string) string {
	if mediaType == "tv" {
		return "first_air_date"
	}
	return "primary_release_date"
}

// TrendingLanguage is a language a caller can filter the discover lists by.
//
// The code is TMDB's ISO 639-1 original_language value. Only the languages this
// project's audience actually looks for are offered; the full list is over a
// hundred entries and useless as a chip row.
type TrendingLanguage struct {
	Code string
	Name string
}

// TrendingLanguages returns the languages offered as filters, most useful first.
func TrendingLanguages() []TrendingLanguage {
	return []TrendingLanguage{
		{"ja", "日语"},
		{"zh", "中文"},
		{"en", "英语"},
		{"ko", "韩语"},
	}
}

// LanguageValid reports whether code is one of the offered languages.
//
// Exported because the HTTP layer validates the query parameter before deciding
// whether the request counts as filtered.
func LanguageValid(code string) bool {
	for _, l := range TrendingLanguages() {
		if l.Code == code {
			return true
		}
	}
	return false
}

// DiscoverQuery describes one request to the discover lists.
type DiscoverQuery struct {
	// MediaType is "movie" or "tv".
	MediaType string
	// GenreID restricts to one genre of that media type; 0 means any.
	GenreID int
	// Sort orders the result.
	Sort TrendingSort
	// Year restricts to one release / first-air year; 0 means any.
	Year int
	// Language restricts to one original language; "" means any.
	Language string
	Page     int
}

// hasFilters reports whether the query needs the discover endpoint.
//
// TMDB's /trending takes none of genre, sort, year or language, so any of them
// means the query has to go through /discover instead.
func (q DiscoverQuery) hasFilters() bool {
	return q.GenreID > 0 || q.Sort == SortDate || q.Year > 0 || q.Language != ""
}

// Discover returns one page of titles matching the query.
//
// An unfiltered query still uses /trending, because its ordering is TMDB's own
// popularity signal rather than something reconstructed from a discover sort.
func (c *TMDBClient) Discover(q DiscoverQuery) ([]TMDBResult, error) {
	if c == nil || c.apiKey == "" {
		return nil, fmt.Errorf("TMDB API key not configured")
	}
	if q.MediaType != "movie" && q.MediaType != "tv" {
		return nil, fmt.Errorf("unsupported media type: %s", q.MediaType)
	}
	// A genre from the other media type would return an empty list, which looks
	// like "no results" rather than "wrong filter", so it is rejected instead.
	if q.GenreID > 0 && !genreValidFor(q.MediaType, q.GenreID) {
		return nil, fmt.Errorf("genre %d is not a %s genre", q.GenreID, q.MediaType)
	}
	// Same reasoning for the language: an unknown code is a caller bug, not an
	// empty result.
	if q.Language != "" && !LanguageValid(q.Language) {
		return nil, fmt.Errorf("unsupported language: %s", q.Language)
	}
	page := q.Page
	if page < 1 {
		page = 1
	}

	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("language", "zh-CN")
	params.Set("page", strconv.Itoa(page))

	var endpoint string
	if q.hasFilters() {
		field := dateField(q.MediaType)
		switch q.Sort {
		case SortDate:
			params.Set("sort_by", field+".desc")
			// Never show future-dated titles: a release that has not aired has no
			// resources to search for, so it would only be noise in a "newest"
			// list.
			params.Set(field+".lte", time.Now().Format("2006-01-02"))
			// A vote floor would hide most of the current season, which is
			// precisely what this ordering is for.
			params.Set("vote_count.gte", "0")
		default:
			params.Set("sort_by", "popularity.desc")
			// Do not exclude titles that have not been rated yet — TMDB's default
			// vote filter hides most recent releases.
			params.Set("vote_count.gte", "10")
		}
		if q.GenreID > 0 {
			params.Set("with_genres", strconv.Itoa(q.GenreID))
		}
		if q.Year > 0 {
			// The year is expressed as a date range, because TMDB has no year
			// parameter: one field, first day to last day of that year.
			params.Set(field+".gte", fmt.Sprintf("%d-01-01", q.Year))
			params.Set(field+".lte", fmt.Sprintf("%d-12-31", q.Year))
		}
		if q.Language != "" {
			// original_language is the language a title was made in, not the
			// language of its metadata — the latter follows the `language`
			// parameter above.
			params.Set("with_original_language", q.Language)
		}
		endpoint = "https://api.themoviedb.org/3/discover/" + q.MediaType + "?" + params.Encode()
	} else {
		endpoint = "https://api.themoviedb.org/3/trending/" + q.MediaType + "/week?" + params.Encode()
	}

	resp, err := c.http.Get(endpoint)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("TMDB returned HTTP %d", resp.StatusCode)
	}

	var sr tmdbSearchResp
	if err := json.NewDecoder(resp.Body).Decode(&sr); err != nil {
		return nil, err
	}
	for i := range sr.Results {
		sr.Results[i].MediaType = q.MediaType
	}
	return sr.Results, nil
}

// Trending returns trending movies and TV shows for the week.
func (c *TMDBClient) Trending() (movies, tvShows []TMDBResult, err error) {
	return c.TrendingPage(1)
}

// TrendingPage returns trending movies and TV shows with pagination.
func (c *TMDBClient) TrendingPage(page int) (movies, tvShows []TMDBResult, err error) {
	if c == nil || c.apiKey == "" {
		return nil, nil, fmt.Errorf("TMDB API key not configured")
	}
	params := url.Values{}
	params.Set("api_key", c.apiKey)
	params.Set("language", "zh-CN")
	params.Set("page", fmt.Sprintf("%d", page))

	// Trending movies
	movieURL := "https://api.themoviedb.org/3/trending/movie/week?" + params.Encode()
	resp, err := c.http.Get(movieURL)
	if err != nil {
		return nil, nil, err
	}
	var mr tmdbSearchResp
	json.NewDecoder(resp.Body).Decode(&mr)
	resp.Body.Close()
	for i := range mr.Results {
		mr.Results[i].MediaType = "movie"
	}
	movies = mr.Results
	if len(movies) > 8 {
		movies = movies[:8]
	}

	// Trending TV
	tvURL := "https://api.themoviedb.org/3/trending/tv/week?" + params.Encode()
	resp, err = c.http.Get(tvURL)
	if err != nil {
		return movies, nil, nil
	}
	var tr tmdbSearchResp
	json.NewDecoder(resp.Body).Decode(&tr)
	resp.Body.Close()
	for i := range tr.Results {
		tr.Results[i].MediaType = "tv"
	}
	tvShows = tr.Results
	if len(tvShows) > 8 {
		tvShows = tvShows[:8]
	}
	return movies, tvShows, nil
}

func (c *TMDBClient) BestMatch(rawTitle string, mediaType string) *TMDBResult {
	if c == nil || c.apiKey == "" {
		return nil
	}
	// Clean the title for search
	clean := cleanForSearch(rawTitle)
	result, err := c.Search(clean, mediaType)
	if err != nil {
		return nil
	}
	// Verify a reasonable match
	if result != nil && titleSimilarity(rawTitle, result.DisplayName()) > 0.3 {
		return result
	}
	return nil
}

func cleanForSearch(s string) string {
	s = strings.TrimSpace(s)
	// Remove year in parentheses
	s = strings.ReplaceAll(s, "(", " ")
	s = strings.ReplaceAll(s, ")", " ")
	// Remove common tags
	for _, tag := range []string{"[", "]", "Season", "season"} {
		s = strings.ReplaceAll(s, tag, " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

func titleSimilarity(a, b string) float64 {
	a = strings.ToLower(strings.TrimSpace(a))
	b = strings.ToLower(strings.TrimSpace(b))
	if a == b {
		return 1.0
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return 0.85
	}
	ta := strings.Fields(a)
	tb := strings.Fields(b)
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	overlap := 0
	for _, x := range ta {
		if len(x) < 2 {
			continue
		}
		for _, y := range tb {
			if x == y {
				overlap++
				break
			}
		}
	}
	return float64(overlap) / float64(max(len(ta), len(tb)))
}
