package server

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lingeringdawn/pan-fetcher/indexer"
)

// dedupKey returns a compact deduplication key.  Uses the most specific
// identifier available: InfoHash > MagnetURL > Title+Size.
// The leading char encodes the type: 'h'=InfoHash, 'm'=MagnetURL, 't'=Title+Size.
func dedupKey(r indexer.SearchResult) string {
	if r.InfoHash != "" {
		return "h" + r.InfoHash
	}
	if r.MagnetURL != "" {
		return "m" + r.MagnetURL
	}
	// Avoid fmt.Sprintf in hot path
	return "t" + r.Title + "\x00" + strconv.FormatInt(r.Size, 10)
}

// dedupSlice removes duplicates from a slice, optionally using and updating
// an existing seen map for cross-batch dedup.  Pass nil to create a fresh set.
// Keeps the first occurrence of each key (stable ordering).
func dedupSlice(results []indexer.SearchResult, seen map[string]bool) []indexer.SearchResult {
	if seen == nil {
		seen = make(map[string]bool, len(results))
	}
	out := results[:0]
	for i := range results {
		key := dedupKey(results[i])
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, results[i])
	}
	return out
}

// savedSearch represents a user-saved search query.
type savedSearch struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Query     string `json:"query"`
	Sort      string `json:"sort"`
	CreatedAt string `json:"created_at"`
}

func (s *Server) loadSavedSearches() []savedSearch {
	data, err := os.ReadFile(s.dataFile("saved-searches.json"))
	if err != nil {
		return nil
	}
	var searches []savedSearch
	json.Unmarshal(data, &searches)
	return searches
}

func (s *Server) saveSearches(searches []savedSearch) {
	data, _ := json.MarshalIndent(searches, "", "  ")
	os.WriteFile(s.dataFile("saved-searches.json"), data, 0644)
}

func (s *Server) handleSearchSubscribe(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	q := strings.TrimSpace(r.FormValue("query"))
	name := strings.TrimSpace(r.FormValue("name"))
	rssURL := strings.TrimSpace(r.FormValue("url"))
	filter := strings.TrimSpace(r.FormValue("filter"))
	cid := strings.TrimSpace(r.FormValue("cid"))
	savepath := strings.TrimSpace(r.FormValue("savepath"))

	if rssURL == "" && q != "" {
		// Auto-build local aggregated RSS URL
		rssURL = buildRssURL(s.Port, q, nil)
	}
	if rssURL == "" {
		data := s.pageData("", tr(s.langFromAgent(), "please_fill_rss"))
		data.SearchQuery = q
		dashboardTemplate.Execute(w, data)
		return
	}

	// Derive a name when the form left it blank.
	//
	// The name doubles as the deduplication key, so an empty one would make every
	// unnamed subscription share a cache bucket and silently suppress each
	// other's releases. Falling back to the feed's host keeps them distinct and
	// still recognisable.
	if name == "" {
		name = q
	}
	if name == "" {
		if u, err := url.Parse(rssURL); err == nil && u.Host != "" {
			name = u.Host
		} else {
			name = "RSS " + time.Now().Format("0102-1504")
		}
	}

	// Save to rss.json
	if err := addRSSFeed(rssURL, name, filter, cid, savepath); err != nil {
		data := s.pageData("", tr(s.langFromAgent(), "saving_failed")+err.Error())
		data.SearchQuery = q
		dashboardTemplate.Execute(w, data)
		return
	}
	log.Printf("[search] subscribed: name=%s url=%s", name, rssURL)
	http.Redirect(w, r, "/subs?subscribed=1", http.StatusSeeOther)
}

// effectiveFilter returns the filter that will actually be applied to a
// subscription.
//
// The keyword lives in the URL's keyword parameter, because that is where
// ProcessRSSFeed reads it from; the separate filter field only exists for
// subscriptions created before that was the case. Reading only the field left
// the edit form blank for every subscription whose filter had been written into
// the URL — which is all of them once the subscription has been run or edited
// at least once.
//
// The URL wins when it carries a keyword, matching ensureKeywordParam and the
// auto-run behaviour.
func effectiveFilter(rawURL, filter string) string {
	if u, err := url.Parse(rawURL); err == nil {
		if k := strings.TrimSpace(u.Query().Get("keyword")); k != "" {
			return k
		}
	}
	return strings.TrimSpace(filter)
}

// setURLKeyword returns rawURL with its keyword parameter set to keyword, or
// with the parameter removed when keyword is blank.
//
// Unlike ensureKeywordParam this always overwrites: it is used when the user
// explicitly edits the filter, where an existing value must be replaceable
// (including being cleared).
func setURLKeyword(rawURL, keyword string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		q.Del("keyword")
	} else {
		q.Set("keyword", keyword)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// ensureKeywordParam returns rawURL carrying the keyword query parameter.
//
// The keyword must live in the URL (not only in the filter field) because
// ProcessRSSFeed reads it from there, and the local /rss/search endpoint applies
// its own keyword filter based on the same parameter.
//
// An existing keyword parameter already present in the URL always wins, so a
// subscription whose URL was built with a filter is never silently overridden;
// the separate filter field only fills the gap for older subscriptions. This
// matches the long-standing auto-run behaviour.
//
// Returns rawURL unchanged when keyword is blank or the URL cannot be parsed.
func ensureKeywordParam(rawURL, keyword string) string {
	keyword = strings.TrimSpace(keyword)
	if keyword == "" {
		return rawURL
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	q := u.Query()
	if strings.TrimSpace(q.Get("keyword")) != "" {
		return rawURL
	}
	q.Set("keyword", keyword)
	u.RawQuery = q.Encode()
	return u.String()
}

// buildRssURL constructs a local aggregated RSS feed URL from search parameters.
func buildRssURL(port int, query string, indexers []string) string {
	v := url.Values{}
	v.Set("q", query)
	if len(indexers) > 0 {
		v.Set("indexers", strings.Join(indexers, ","))
	}
	v.Set("sort", "date")
	return fmt.Sprintf("http://127.0.0.1:%d/rss/search?%s", port, v.Encode())
}

// addRSSFeed appends an RSS feed entry to rss.json.
func addRSSFeed(rssURL, name, filter, cid, savepath string) error {
	// Determine site from URL
	site := "pan-fetcher"
	if u, err := url.Parse(rssURL); err == nil && u.Host != "" {
		site = u.Host
		if strings.Contains(site, "mikanani") || strings.Contains(site, "mikanime") {
			site = "mikanani.me"
		}
		if strings.Contains(site, "dmhy") {
			site = "share.dmhy.org"
		}
	}

	// Read existing rss.json
	feeds := make(map[string][]rssFeedEntry)
	if data, err := os.ReadFile(rssJsonPath); err == nil {
		json.Unmarshal(data, &feeds)
	}

	entry := rssFeedEntry{
		Name:     name,
		URL:      rssURL,
		Filter:   filter,
		Cid:      cid,
		SavePath: savepath,
		Enabled:  false, // disabled by default, user must enable in subs page
	}
	feeds[site] = append(feeds[site], entry)

	data, err := json.MarshalIndent(feeds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(rssJsonPath, data, 0644)
}

type rssFeedEntry struct {
	Name     string `json:"name"`
	URL      string `json:"url"`
	Filter   string `json:"filter,omitempty"`
	Cid      string `json:"cid,omitempty"`
	SavePath string `json:"savepath,omitempty"`
	Enabled  bool   `json:"enabled"`
}

// UnmarshalJSON treats a missing "enabled" field as enabled.
//
// The field used to be a *bool read through IsEnabled(), where nil meant true, so
// subscription files written before the flag existed kept running. Switching to a
// plain bool silently changed that: the zero value is false, so every legacy
// entry — and every entry copied from rss.example.json, which omits the flag in
// two of its three examples — became "disabled" after an upgrade and was skipped
// by the scheduler with no visible reason.
//
// An explicit false is still honoured; only an absent key defaults to true.
func (r *rssFeedEntry) UnmarshalJSON(data []byte) error {
	// The shadow type has the same fields but none of the methods, so decoding
	// into it cannot recurse back here.
	type shadow rssFeedEntry
	aux := struct {
		*shadow
		Enabled *bool `json:"enabled"`
	}{shadow: (*shadow)(r)}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	r.Enabled = aux.Enabled == nil || *aux.Enabled
	return nil
}

func (s *Server) handleSearchUnsubscribe(w http.ResponseWriter, r *http.Request) {
	id := r.FormValue("id")
	searches := s.loadSavedSearches()
	filtered := searches[:0]
	for _, ss := range searches {
		if ss.ID != id {
			filtered = append(filtered, ss)
		}
	}
	s.saveSearches(filtered)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
