package rsssite

import (
	neturl "net/url"
	"strings"
	"testing"
)

// jackettURL builds a download link shaped like the ones indexers return.
func jackettURL(title string) string {
	return "http://127.0.0.1:9117/dl/acgrip/?jackett_apikey=k&path=Q2ZESjhO&file=" +
		neturl.QueryEscape(title)
}

// TestTaskDisplayNamePrefersTitle verifies the caller's title wins.
func TestTaskDisplayNamePrefersTitle(t *testing.T) {
	const title = "[绿茶字幕组] 某番剧 [01-12修正合集]"
	got := TaskDisplayName("magnet:?xt=urn:btih:aaa&dn=other", title)
	if got != title {
		t.Errorf("got %q, want the supplied title", got)
	}
}

// TestTaskDisplayNameReadsDownloadLink is the regression test for the reported
// symptom: a submission shown as a Jackett URL instead of the release name.
//
// Indexers return a .torrent endpoint and put the name in file=, so a lookup that
// only understood magnet dn= fell through to the raw string.
func TestTaskDisplayNameReadsDownloadLink(t *testing.T) {
	const title = "[绿茶字幕组] 不动声色的柏田与喜形于色的太田 [01-12修正合集][WebRip][1080p][繁日内封]"
	link := jackettURL(title)

	got := TaskDisplayName(link, "")
	if got != title {
		t.Errorf("got %q, want the release name from file=", got)
	}
	if strings.HasPrefix(got, "http") {
		t.Error("a download link must not be used as the display name")
	}
}

// TestTaskDisplayNameFallsBackToMagnet verifies dn= is used when no title is
// supplied.
func TestTaskDisplayNameFallsBackToMagnet(t *testing.T) {
	const name = "某番剧 第01话"
	got := TaskDisplayName("magnet:?xt=urn:btih:aaa&dn="+neturl.QueryEscape(name), "")
	if got != name {
		t.Errorf("got %q, want the magnet dn=", got)
	}
}

// TestTaskDisplayNameLastResort verifies an unlabelled task still yields something
// rather than an empty string.
func TestTaskDisplayNameLastResort(t *testing.T) {
	const raw = "magnet:?xt=urn:btih:0123456789abcdef"
	if got := TaskDisplayName(raw, ""); got != raw {
		t.Errorf("got %q, want the raw task as a last resort", got)
	}
}

// TestTaskDisplayNameIgnoresBlankTitle verifies whitespace is not a title.
func TestTaskDisplayNameIgnoresBlankTitle(t *testing.T) {
	const name = "有名字"
	got := TaskDisplayName("magnet:?xt=urn:btih:aaa&dn="+neturl.QueryEscape(name), "   ")
	if got != name {
		t.Errorf("got %q, a blank title should be ignored", got)
	}
}

// TestExtractNameFromURLKeys verifies the parameter names accepted.
func TestExtractNameFromURLKeys(t *testing.T) {
	cases := map[string]string{
		"https://h/dl?file=" + neturl.QueryEscape("来自文件参数") + "&x=1":  "来自文件参数",
		"https://h/dl?title=" + neturl.QueryEscape("来自标题参数"):          "来自标题参数",
		"https://h/dl?name=" + neturl.QueryEscape("来自名称参数"):           "来自名称参数",
	}
	for raw, want := range cases {
		if got := ExtractNameFromURL(raw); got != want {
			t.Errorf("%s -> %q, want %q", raw, got, want)
		}
	}
}

// TestExtractNameFromURLRejectsUnusable verifies links without a name yield empty.
func TestExtractNameFromURLRejectsUnusable(t *testing.T) {
	for _, raw := range []string{
		"https://h/dl?other=1",
		"magnet:?xt=urn:btih:aaa",
		"://bad url",
		"",
	} {
		if got := ExtractNameFromURL(raw); got != "" {
			t.Errorf("%q should yield no name, got %q", raw, got)
		}
	}
}

// TestExtractNameFromMagnetDecodes verifies percent-encoded names are decoded.
func TestExtractNameFromMagnetDecodes(t *testing.T) {
	const name = "某番剧 [1080p] 简繁内封"
	got := ExtractNameFromMagnet("magnet:?xt=urn:btih:aaa&dn=" + neturl.QueryEscape(name) + "&tr=udp://x")
	if got != name {
		t.Errorf("got %q, want %q", got, name)
	}
}

// TestExtractNameFromMagnetAbsent verifies a magnet without dn= yields empty.
func TestExtractNameFromMagnetAbsent(t *testing.T) {
	if got := ExtractNameFromMagnet("magnet:?xt=urn:btih:aaa&tr=udp://x"); got != "" {
		t.Errorf("want empty, got %q", got)
	}
	if got := ExtractNameFromMagnet(""); got != "" {
		t.Errorf("want empty for an empty input, got %q", got)
	}
}
