package server

import (
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// postSubscribe drives handleSearchSubscribe directly.
//
// The route is wrapped in authCheck, and a test request has no session, so going
// through the mux would be redirected before the handler runs.
func postSubscribe(t *testing.T, srv *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/search/subscribe", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleSearchSubscribe(rec, req)
	return rec
}

// withTempSubs points the subscription file at a temp path.
func withTempSubs(t *testing.T) {
	t.Helper()
	saved := rssJsonPath
	rssJsonPath = t.TempDir() + "/rss.json"
	t.Cleanup(func() { rssJsonPath = saved })
}

// readSubs returns the saved subscriptions flattened.
func readSubs(t *testing.T) []rssFeedEntry {
	t.Helper()
	data, err := os.ReadFile(rssJsonPath)
	if err != nil {
		t.Fatalf("read subs: %v", err)
	}
	var feeds map[string][]rssFeedEntry
	if err := json.Unmarshal(data, &feeds); err != nil {
		t.Fatalf("parse subs: %v", err)
	}
	var out []rssFeedEntry
	for _, entries := range feeds {
		out = append(out, entries...)
	}
	return out
}

// TestAddCustomRSSAccepted is the test for the feature this exists to provide:
// an arbitrary feed URL can be added from the subscriptions page.
//
// The address is not restricted to the bundled source sites. Whatever the feed
// is — a source site, a self-hosted RSSHub, anything else — it is stored as
// given.
func TestAddCustomRSSAccepted(t *testing.T) {
	withTempSubs(t)
	srv := &Server{Port: 8115}

	postSubscribe(t, srv, url.Values{
		"name": {"我的番剧"},
		"url":  {"https://rsshub.example.com/mikan/bangumi/3407"},
		"cid":  {"123456"},
	})

	subs := readSubs(t)
	if len(subs) != 1 {
		t.Fatalf("want 1 subscription, got %d", len(subs))
	}
	got := subs[0]
	if got.URL != "https://rsshub.example.com/mikan/bangumi/3407" {
		t.Errorf("URL = %q, want it stored verbatim", got.URL)
	}
	if got.Name != "我的番剧" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Cid != "123456" {
		t.Errorf("Cid = %q", got.Cid)
	}
	// New subscriptions start disabled so an unverified feed cannot start
	// submitting on its own.
	if got.Enabled {
		t.Error("a new subscription should start disabled")
	}
}

// TestNameFallsBackToHost verifies an unnamed subscription still gets a usable
// name.
//
// The name is also the deduplication key. An empty one would put every unnamed
// subscription in the same cache bucket, and they would suppress each other's
// releases without any visible error.
func TestNameFallsBackToHost(t *testing.T) {
	withTempSubs(t)
	srv := &Server{Port: 8115}

	postSubscribe(t, srv, url.Values{
		"url": {"https://share.dmhy.org/topics/rss/rss.xml"},
	})

	subs := readSubs(t)
	if len(subs) != 1 {
		t.Fatalf("want 1 subscription, got %d", len(subs))
	}
	if subs[0].Name != "share.dmhy.org" {
		t.Errorf("Name = %q, want the host as fallback", subs[0].Name)
	}
}

// TestTwoUnnamedSubsStayDistinct verifies the fallback does not collapse two
// different feeds into one name.
func TestTwoUnnamedSubsStayDistinct(t *testing.T) {
	withTempSubs(t)
	srv := &Server{Port: 8115}

	postSubscribe(t, srv, url.Values{"url": {"https://a.example.com/feed"}})
	postSubscribe(t, srv, url.Values{"url": {"https://b.example.com/feed"}})

	subs := readSubs(t)
	if len(subs) != 2 {
		t.Fatalf("want 2 subscriptions, got %d", len(subs))
	}
	if subs[0].Name == subs[1].Name {
		t.Errorf("two different feeds share the name %q", subs[0].Name)
	}
}

// TestEmptyURLRejected verifies a blank address is refused rather than saved.
func TestEmptyURLRejected(t *testing.T) {
	withTempSubs(t)
	srv := &Server{Port: 8115}

	postSubscribe(t, srv, url.Values{"name": {"x"}})

	if data, err := os.ReadFile(rssJsonPath); err == nil {
		t.Errorf("nothing should be written when the URL is empty, got %s", data)
	}
}

// TestSearchPageStillPrefills verifies the search-page entry keeps its prefilled
// internal URL.
//
// The two entry points share one dialog, so loosening it for the blank case must
// not break the "subscribe this search" case.
func TestSearchPageStillPrefills(t *testing.T) {
	src := readSource(t, templateSourceFile)

	i := strings.Index(src, "rss.onclick=function()")
	if i < 0 {
		t.Fatal("the search page RSS chip handler was not found")
	}
	end := i + 800
	if end > len(src) {
		end = len(src)
	}
	body := src[i:end]

	if !strings.Contains(body, "openSubForm(u,q)") {
		t.Error("the RSS chip should open the shared dialog with the built URL")
	}
	if !strings.Contains(body, "'/rss/search?q='") {
		t.Error("the RSS chip should still build the internal aggregated URL")
	}
}

// TestSubsPageHasAddButton verifies the subscriptions page exposes the entry
// point.
//
// Before this, the only way to open the dialog was the "+ RSS" chip on the search
// page, which is rendered only when a search has results — so an empty install
// had no way to add a feed at all.
func TestSubsPageHasAddButton(t *testing.T) {
	src := readSource(t, templateSourceFile)

	btn := strings.Index(src, `openSubForm('','')`)
	if btn < 0 {
		t.Fatal("no add button that starts with empty fields")
	}
	// It has to sit inside the subscriptions page block, not somewhere else that
	// happens to call the helper with empty values.
	block := strings.LastIndex(src[:btn], `{{if eq .Page "subs"}}`)
	if block < 0 {
		t.Fatal("the button is not inside the subscriptions page block")
	}
	if btn-block > 4000 {
		t.Errorf("the button is %d bytes past the start of the subscriptions block", btn-block)
	}
}

// TestOpenSubFormDeclaredEarly verifies the dialog helper is defined before any
// page script uses it.
//
// A function declared in a later script block is not callable when an earlier
// page script runs — that is how the genre chips broke once already.
func TestOpenSubFormDeclaredEarly(t *testing.T) {
	src := readSource(t, templateSourceFile)

	decl := strings.Index(src, "function openSubForm(")
	if decl < 0 {
		t.Fatal("openSubForm is not declared")
	}
	firstScriptEnd := strings.Index(src, "</script>")
	if firstScriptEnd < 0 {
		t.Fatal("no script block found")
	}
	if decl > firstScriptEnd {
		t.Error("openSubForm should be declared in the first script block so every page can call it")
	}

	// Every call site has to come after the declaration.
	for _, m := range findAllIndex(src, "openSubForm(") {
		if m < decl {
			t.Errorf("openSubForm is called at offset %d, before its declaration at %d", m, decl)
		}
	}
}

// findAllIndex returns the offset of every occurrence of needle.
func findAllIndex(s, needle string) []int {
	var out []int
	for i := 0; ; {
		j := strings.Index(s[i:], needle)
		if j < 0 {
			break
		}
		out = append(out, i+j)
		i += j + len(needle)
	}
	return out
}
