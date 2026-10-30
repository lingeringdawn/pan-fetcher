package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/deadblue/elevengo"
)

// ---------- ensureKeywordParam ----------

func TestEnsureKeywordParam(t *testing.T) {
	cases := []struct {
		name    string
		rawURL  string
		keyword string
		want    string
	}{
		{
			name:    "blank keyword leaves URL untouched",
			rawURL:  "http://127.0.0.1:8115/rss/search?q=foo",
			keyword: "",
			want:    "http://127.0.0.1:8115/rss/search?q=foo",
		},
		{
			name:    "whitespace-only keyword leaves URL untouched",
			rawURL:  "http://127.0.0.1:8115/rss/search?q=foo",
			keyword: "   ",
			want:    "http://127.0.0.1:8115/rss/search?q=foo",
		},
		{
			name:    "keyword appended when absent",
			rawURL:  "http://127.0.0.1:8115/rss/search?q=foo",
			keyword: "group:SubsPlease",
			want:    "http://127.0.0.1:8115/rss/search?keyword=group%3ASubsPlease&q=foo",
		},
		{
			name:    "existing keyword in URL wins over filter field",
			rawURL:  "http://127.0.0.1:8115/rss/search?q=foo&keyword=fromURL",
			keyword: "fromFilter",
			want:    "http://127.0.0.1:8115/rss/search?q=foo&keyword=fromURL",
		},
		{
			name:    "empty keyword param in URL is treated as absent",
			rawURL:  "http://127.0.0.1:8115/rss/search?q=foo&keyword=",
			keyword: "fromFilter",
			want:    "http://127.0.0.1:8115/rss/search?keyword=fromFilter&q=foo",
		},
		{
			name:    "multi-group keyword is escaped and preserved",
			rawURL:  "/rss/search?q=bar",
			keyword: "group:A|B res:1080p",
			want:    "/rss/search?keyword=group%3AA%7CB+res%3A1080p&q=bar",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ensureKeywordParam(tc.rawURL, tc.keyword)
			if got != tc.want {
				t.Fatalf("ensureKeywordParam(%q, %q)\n got: %q\nwant: %q",
					tc.rawURL, tc.keyword, got, tc.want)
			}
		})
	}
}

// TestEnsureKeywordParamRoundTrip guards against double-escaping: whatever the
// helper produces must read back as the exact keyword the caller supplied,
// because ProcessRSSFeed parses it out again with url.Parse.
func TestEnsureKeywordParamRoundTrip(t *testing.T) {
	keywords := []string{
		"TARI TARI",
		"group:喵萌奶茶屋|LoliHouse",
		"res:1080p|2160p",
		"[豌豆字幕组&LoliHouse]",
		"100%",
		"a&b=c",
	}
	for _, kw := range keywords {
		got := ensureKeywordParam("http://127.0.0.1:8115/rss/search?q=x", kw)
		u, err := url.Parse(got)
		if err != nil {
			t.Fatalf("parse %q: %v", got, err)
		}
		if back := u.Query().Get("keyword"); back != kw {
			t.Errorf("round trip mismatch: sent %q, read back %q (url=%s)", kw, back, got)
		}
	}
}

// ---------- handler-level regression tests ----------

type feedCall struct {
	url      string
	cid      string
	savepath string
	subKey   string
}

// recordingAgent wraps fakeServerAgent to capture ProcessRSSFeed invocations,
// which the real handler dispatches from a goroutine.
type recordingAgent struct {
	*fakeServerAgent
	calls chan feedCall
}

func (r *recordingAgent) ProcessRSSFeed(feedURL, cid, savepath, subKey string) ([]string, error) {
	r.calls <- feedCall{url: feedURL, cid: cid, savepath: savepath, subKey: subKey}
	return nil, nil
}

func newRecordingServer(t *testing.T) (*recordingAgent, *http.ServeMux) {
	t.Helper()
	fake := &fakeServerAgent{
		userInfo: elevengo.UserInfo{Id: 123, Name: "tester", IsVip: true},
	}
	agent := &recordingAgent{
		fakeServerAgent: fake,
		calls:           make(chan feedCall, 4),
	}
	srv := &Server{Agent: agent, Port: 8115, startTime: time.Now()}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return agent, mux
}

func waitForFeedCall(t *testing.T, agent *recordingAgent) feedCall {
	t.Helper()
	select {
	case c := <-agent.calls:
		return c
	case <-time.After(3 * time.Second):
		t.Fatal("ProcessRSSFeed was never called")
		return feedCall{}
	}
}

func postForm(mux *http.ServeMux, path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestSubsRunAppliesFilterField is the regression test for issue #1: the manual
// "run now" button used to send `filter` while the handler dropped it, so the
// keyword never reached ProcessRSSFeed.
func TestSubsRunAppliesFilterField(t *testing.T) {
	agent, mux := newRecordingServer(t)

	form := url.Values{
		"rss_url":  {"/rss/search?q=TARI+TARI&sort=date"},
		"cid":      {"cid-1"},
		"savepath": {"anime"},
		"filter":   {"group:LoliHouse"},
		"sub_name": {"TARI TARI"},
	}
	rec := postForm(mux, "/subs/run", form,
		map[string]string{"X-Requested-With": "XMLHttpRequest"})

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	call := waitForFeedCall(t, agent)

	u, err := url.Parse(call.url)
	if err != nil {
		t.Fatalf("parse feed url %q: %v", call.url, err)
	}
	if got := u.Query().Get("keyword"); got != "group:LoliHouse" {
		t.Errorf("keyword not applied: got %q, want %q (url=%s)", got, "group:LoliHouse", call.url)
	}
	// Relative URL must still be resolved to the local endpoint.
	if !strings.HasPrefix(call.url, "http://127.0.0.1:8115/rss/search") {
		t.Errorf("relative URL not resolved: %s", call.url)
	}
	// subKey must stay the subscription name so dedup cache keys remain stable.
	if call.subKey != "TARI TARI" {
		t.Errorf("subKey changed: got %q, want %q", call.subKey, "TARI TARI")
	}
	if call.cid != "cid-1" || call.savepath != "anime" {
		t.Errorf("cid/savepath lost: %q %q", call.cid, call.savepath)
	}
}

// TestSubsRunEmptyFilterLeavesURLUntouched ensures the fix does not inject a
// stray empty keyword parameter for subscriptions without a filter.
func TestSubsRunEmptyFilterLeavesURLUntouched(t *testing.T) {
	agent, mux := newRecordingServer(t)

	form := url.Values{
		"rss_url":  {"http://127.0.0.1:8115/rss/search?q=foo"},
		"cid":      {"cid-2"},
		"filter":   {""},
		"sub_name": {"no-filter"},
	}
	postForm(mux, "/subs/run", form, map[string]string{"X-Requested-With": "XMLHttpRequest"})

	call := waitForFeedCall(t, agent)
	if strings.Contains(call.url, "keyword=") {
		t.Errorf("empty filter must not add keyword param: %s", call.url)
	}
}

// TestSubsRunURLKeywordWinsOverFilter pins the precedence rule: a keyword
// already embedded in the subscription URL must never be overridden by the
// separate filter field, matching auto-run behaviour.
func TestSubsRunURLKeywordWinsOverFilter(t *testing.T) {
	agent, mux := newRecordingServer(t)

	form := url.Values{
		"rss_url":  {"http://127.0.0.1:8115/rss/search?q=foo&keyword=fromURL"},
		"cid":      {"cid-3"},
		"filter":   {"fromFilter"},
		"sub_name": {"both"},
	}
	postForm(mux, "/subs/run", form, map[string]string{"X-Requested-With": "XMLHttpRequest"})

	call := waitForFeedCall(t, agent)
	u, err := url.Parse(call.url)
	if err != nil {
		t.Fatalf("parse %q: %v", call.url, err)
	}
	if got := u.Query().Get("keyword"); got != "fromURL" {
		t.Errorf("URL keyword should win: got %q, want %q", got, "fromURL")
	}
}

// TestRSSFeedQuickGrabAppliesKeyword covers the second dropped-keyword path:
// the /rss/feed quick grab read the keyword field but only logged it.
func TestRSSFeedQuickGrabAppliesKeyword(t *testing.T) {
	agent, mux := newRecordingServer(t)

	form := url.Values{
		"rss_url": {"https://example.com/rss"},
		"keyword": {"res:1080p"},
		"cid":     {"123"},
	}
	postForm(mux, "/rss/feed", form, nil)

	call := waitForFeedCall(t, agent)
	u, err := url.Parse(call.url)
	if err != nil {
		t.Fatalf("parse %q: %v", call.url, err)
	}
	if got := u.Query().Get("keyword"); got != "res:1080p" {
		t.Errorf("keyword not applied: got %q, want %q (url=%s)", got, "res:1080p", call.url)
	}
	// subKey intentionally stays the bare URL to keep dedup keys stable.
	if call.subKey != "https://example.com/rss" {
		t.Errorf("subKey should be the bare URL: got %q", call.subKey)
	}
}

// TestRSSFeedQuickGrabEmptyKeyword keeps the no-keyword path clean.
func TestRSSFeedQuickGrabEmptyKeyword(t *testing.T) {
	agent, mux := newRecordingServer(t)

	form := url.Values{
		"rss_url": {"https://example.com/rss"},
		"keyword": {""},
		"cid":     {"123"},
	}
	postForm(mux, "/rss/feed", form, nil)

	call := waitForFeedCall(t, agent)
	if strings.Contains(call.url, "keyword=") {
		t.Errorf("empty keyword must not add param: %s", call.url)
	}
}

// ---------- subscription edit ----------

// withTempRssJSON points rssJsonPath at a temp file seeded with one subscription
// and returns the path. The original value is restored on cleanup.
func withTempRssJSON(t *testing.T, entry rssFeedEntry) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rss.json")
	feeds := map[string][]rssFeedEntry{"site": {entry}}
	data, err := json.MarshalIndent(feeds, "", "  ")
	if err != nil {
		t.Fatalf("marshal seed feeds: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write seed feeds: %v", err)
	}
	old := rssJsonPath
	rssJsonPath = path
	t.Cleanup(func() { rssJsonPath = old })
	return path
}

func readSingleSub(t *testing.T) rssFeedEntry {
	t.Helper()
	data, err := os.ReadFile(rssJsonPath)
	if err != nil {
		t.Fatalf("read feeds: %v", err)
	}
	var feeds map[string][]rssFeedEntry
	if err := json.Unmarshal(data, &feeds); err != nil {
		t.Fatalf("unmarshal feeds: %v", err)
	}
	entries := feeds["site"]
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 entry, got %d", len(entries))
	}
	return entries[0]
}

// TestEditSubCanClearFilter is the regression test for the edit form: guarding
// the assignment with `filter != ""` made a set filter impossible to remove.
func TestEditSubCanClearFilter(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "TARI TARI", URL: "/rss/search?q=TARI+TARI",
		Cid: "cid-1", SavePath: "anime", Filter: "group:LoliHouse", Enabled: true,
	})
	_, mux := newRecordingServer(t)

	// Submit with every field cleared.
	form := url.Values{
		"action":   {"edit"},
		"site":     {"site"},
		"name":     {"TARI TARI"},
		"cid":      {""},
		"savepath": {""},
		"filter":   {""},
	}
	rec := postForm(mux, "/subs", form, nil)
	if rec.Code != http.StatusSeeOther && rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}

	got := readSingleSub(t)
	if got.Filter != "" {
		t.Errorf("filter not cleared: got %q, want empty", got.Filter)
	}
	if got.SavePath != "" {
		t.Errorf("savepath not cleared: got %q, want empty", got.SavePath)
	}
	if got.Cid != "" {
		t.Errorf("cid not cleared: got %q, want empty", got.Cid)
	}
	// Identity fields must survive an edit.
	if got.Name != "TARI TARI" || got.URL != "/rss/search?q=TARI+TARI" {
		t.Errorf("identity fields changed: name=%q url=%q", got.Name, got.URL)
	}
	if !got.Enabled {
		t.Error("enabled flag must survive an edit")
	}
}

// TestEditSubCanSetFilter covers the ordinary write path and whitespace trimming.
func TestEditSubCanSetFilter(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "TARI TARI", URL: "/rss/search?q=TARI+TARI", Enabled: false,
	})
	_, mux := newRecordingServer(t)

	form := url.Values{
		"action":   {"edit"},
		"site":     {"site"},
		"name":     {"TARI TARI"},
		"cid":      {"  cid-9  "},
		"savepath": {"  sub dir  "},
		"filter":   {"  res:1080p  "},
	}
	postForm(mux, "/subs", form, nil)

	got := readSingleSub(t)
	if got.Filter != "res:1080p" {
		t.Errorf("filter not set/trimmed: got %q", got.Filter)
	}
	if got.Cid != "cid-9" {
		t.Errorf("cid not set/trimmed: got %q", got.Cid)
	}
	if got.SavePath != "sub dir" {
		t.Errorf("savepath not set/trimmed: got %q", got.SavePath)
	}
	if got.Enabled {
		t.Error("enabled flag must not be changed by an edit")
	}
}

// TestEditSubPartialClearOnlyTouchesSubmittedFields documents that the edit form
// is a complete-edit form: clearing one field clears only that field.
func TestEditSubPartialClearOnlyTouchesSubmittedFields(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "sub", URL: "/rss/search?q=x",
		Cid: "keep-cid", SavePath: "keep-path", Filter: "drop-me", Enabled: true,
	})
	_, mux := newRecordingServer(t)

	form := url.Values{
		"action":   {"edit"},
		"site":     {"site"},
		"name":     {"sub"},
		"cid":      {"keep-cid"},
		"savepath": {"keep-path"},
		"filter":   {""},
	}
	postForm(mux, "/subs", form, nil)

	got := readSingleSub(t)
	if got.Filter != "" {
		t.Errorf("filter not cleared: got %q", got.Filter)
	}
	if got.Cid != "keep-cid" || got.SavePath != "keep-path" {
		t.Errorf("untouched fields changed: cid=%q savepath=%q", got.Cid, got.SavePath)
	}
}

// TestEditSubUnknownNameIsNoop ensures editing a non-existent subscription does
// not corrupt or duplicate entries.
func TestEditSubUnknownNameIsNoop(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "sub", URL: "/rss/search?q=x", Filter: "keep-me", Enabled: true,
	})
	_, mux := newRecordingServer(t)

	form := url.Values{
		"action": {"edit"},
		"site":   {"site"},
		"name":   {"does-not-exist"},
		"filter": {""},
	}
	postForm(mux, "/subs", form, nil)

	got := readSingleSub(t)
	if got.Filter != "keep-me" {
		t.Errorf("unrelated subscription was modified: filter=%q", got.Filter)
	}
}

// TestEffectiveFilterPrefersURLKeyword is the regression test for the edit form
// showing an empty filter.
//
// The filter is stored in the URL's keyword parameter because that is where
// ProcessRSSFeed reads it. The form read only the legacy Filter field, which is
// empty for any subscription whose filter has been written into the URL — so the
// box always came up blank and saving it would have wiped the real filter.
func TestEffectiveFilterPrefersURLKeyword(t *testing.T) {
	cases := []struct {
		name   string
		url    string
		filter string
		want   string
	}{
		{
			name:   "keyword in URL wins over an empty field",
			url:    "/rss/search?q=x&keyword=group%3ANEST",
			filter: "",
			want:   "group:NEST",
		},
		{
			name:   "keyword in URL wins over a stale field",
			url:    "/rss/search?q=x&keyword=new",
			filter: "old",
			want:   "new",
		},
		{
			name:   "legacy field used when the URL has no keyword",
			url:    "/rss/search?q=x",
			filter: "legacy",
			want:   "legacy",
		},
		{
			name:   "both empty",
			url:    "/rss/search?q=x",
			filter: "",
			want:   "",
		},
		{
			name:   "unparsable URL falls back to the field",
			url:    "://bad",
			filter: "fallback",
			want:   "fallback",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := effectiveFilter(c.url, c.filter); got != c.want {
				t.Errorf("effectiveFilter(%q, %q) = %q, want %q", c.url, c.filter, got, c.want)
			}
		})
	}
}

// TestSubsPageShowsFilterFromURL is the end-to-end check: the rendered form must
// carry the filter that lives in the URL.
func TestSubsPageShowsFilterFromURL(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "侦探已死",
		URL:  "/rss/search?q=x&keyword=group%3ANEST+language%3A%E7%AE%80%E6%97%A5%E5%86%85%E5%B0%81",
		Cid:  "cid-1", Enabled: true,
	})
	s, mux := newRecordingServer(t)

	req := httptest.NewRequest("GET", "/subs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /subs = %d", rec.Code)
	}

	// The value is HTML-escaped in the attribute; look for the decoded form.
	body := rec.Body.String()
	if !strings.Contains(body, `name="filter"`) {
		t.Fatal("edit form has no filter input")
	}
	want := "group:NEST language:简日内封"
	if !strings.Contains(body, want) {
		t.Errorf("filter input does not carry %q", want)
	}
	_ = s
}

// TestEditSubWritesFilterIntoURL verifies that saving a filter also updates the
// URL, which is what actually drives the feed. Writing only the legacy field
// left the old keyword in the URL still winning, so the edit looked saved but
// had no effect.
func TestEditSubWritesFilterIntoURL(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "sub",
		URL:  "/rss/search?q=x&keyword=old",
		Cid:  "cid-1", Enabled: true,
	})
	_, mux := newRecordingServer(t)

	form := url.Values{
		"action": {"edit"},
		"site":   {"site"},
		"name":   {"sub"},
		"cid":    {"cid-1"},
		"filter": {"group:NEST"},
	}
	postForm(mux, "/subs", form, nil)

	got := readSingleSub(t)
	if got.Filter != "group:NEST" {
		t.Errorf("filter field = %q, want group:NEST", got.Filter)
	}
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatalf("stored URL unparsable: %v", err)
	}
	if k := u.Query().Get("keyword"); k != "group:NEST" {
		t.Errorf("URL keyword = %q, want group:NEST (url=%s)", k, got.URL)
	}
	// The rest of the URL must survive the rewrite.
	if q := u.Query().Get("q"); q != "x" {
		t.Errorf("query parameter lost: url=%s", got.URL)
	}
}

// TestEditSubClearingFilterRemovesURLKeyword covers the clear path: an emptied
// filter must remove the keyword, not leave the old one in place.
func TestEditSubClearingFilterRemovesURLKeyword(t *testing.T) {
	withTempRssJSON(t, rssFeedEntry{
		Name: "sub",
		URL:  "/rss/search?q=x&keyword=group%3ANEST",
		Cid:  "cid-1", Enabled: true,
	})
	_, mux := newRecordingServer(t)

	form := url.Values{
		"action": {"edit"},
		"site":   {"site"},
		"name":   {"sub"},
		"cid":    {"cid-1"},
		"filter": {""},
	}
	postForm(mux, "/subs", form, nil)

	got := readSingleSub(t)
	u, err := url.Parse(got.URL)
	if err != nil {
		t.Fatalf("stored URL unparsable: %v", err)
	}
	if k := u.Query().Get("keyword"); k != "" {
		t.Errorf("keyword should be removed, got %q (url=%s)", k, got.URL)
	}
	if q := u.Query().Get("q"); q != "x" {
		t.Errorf("query parameter lost: url=%s", got.URL)
	}
}

// TestSetURLKeyword covers the rewrite helper directly, including the case where
// the URL carries no query string at all.
//
// Expected values use url.Values ordering (alphabetical), which is what Encode
// produces; the point of the test is which parameters survive, not their order.
func TestSetURLKeyword(t *testing.T) {
	cases := []struct {
		name    string
		url     string
		keyword string
		want    string
	}{
		{"sets when absent", "/rss/search?q=x", "k", "/rss/search?keyword=k&q=x"},
		{"overwrites existing", "/rss/search?q=x&keyword=old", "new", "/rss/search?keyword=new&q=x"},
		{"removes when blank", "/rss/search?q=x&keyword=old", "", "/rss/search?q=x"},
		{"blank on URL without keyword", "/rss/search?q=x", "", "/rss/search?q=x"},
		{"unparsable URL unchanged", "://bad", "k", "://bad"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := setURLKeyword(c.url, c.keyword)
			if got != c.want {
				t.Errorf("setURLKeyword(%q, %q) = %q, want %q", c.url, c.keyword, got, c.want)
			}
		})
	}
}

