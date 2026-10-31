package rsssite

import (
	"testing"

	"github.com/mmcdole/gofeed"
)

// TestEnclosureRejectsUnconvertedURL is the regression test for duplicate
// submissions.
//
// When a torrent endpoint cannot be downloaded, NormalizeTaskURL hands back the
// original URL. Returning that as if it were a usable task URL made 115 create a
// task from an arbitrary endpoint — duplicating an episode that had already been
// submitted from its magnet — and because the URL has no info hash, dedup could
// not connect the two either.
func TestEnclosureRejectsUnconvertedURL(t *testing.T) {
	// Port 1 is closed, so the download fails and the URL comes back unchanged.
	const dead = "http://127.0.0.1:1/dl/mikan/?path=abc"

	item := &gofeed.Item{
		Title: "某番剧 S01E01",
		Enclosures: []*gofeed.Enclosure{
			{Type: "application/x-bittorrent", URL: dead},
		},
	}

	if got := GetMagnetByEnclosure(item); got != "" {
		t.Errorf("an unconvertible enclosure should yield no task URL, got %q", got)
	}
}

// TestEnclosureAcceptsMagnet verifies a magnet passes through untouched.
func TestEnclosureAcceptsMagnet(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"
	item := &gofeed.Item{
		Title:      "某番剧 S01E01",
		Enclosures: []*gofeed.Enclosure{{Type: "application/x-bittorrent", URL: magnet}},
	}
	if got := GetMagnetByEnclosure(item); got != magnet {
		t.Errorf("magnet should be returned as-is, got %q", got)
	}
}

// TestEnclosureFallsThroughToNext verifies a bad enclosure does not hide a good
// one behind it.
func TestEnclosureFallsThroughToNext(t *testing.T) {
	const magnet = "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567&dn=x"
	item := &gofeed.Item{
		Title: "某番剧 S01E01",
		Enclosures: []*gofeed.Enclosure{
			{Type: "application/x-bittorrent", URL: "http://127.0.0.1:1/broken"},
			{Type: "application/x-bittorrent", URL: magnet},
		},
	}
	if got := GetMagnetByEnclosure(item); got != magnet {
		t.Errorf("a broken enclosure must not mask a later usable one, got %q", got)
	}
}

// TestEnclosureIgnoresUnrelatedTypes verifies a non-torrent enclosure is skipped
// rather than treated as a task URL.
func TestEnclosureIgnoresUnrelatedTypes(t *testing.T) {
	item := &gofeed.Item{
		Title: "某番剧 S01E01",
		Enclosures: []*gofeed.Enclosure{
			{Type: "image/jpeg", URL: "https://example.com/cover.jpg"},
		},
	}
	if got := GetMagnetByEnclosure(item); got != "" {
		t.Errorf("an image enclosure is not a task, got %q", got)
	}
}

// TestEnclosureEmpty verifies an item with no enclosure yields nothing.
func TestEnclosureEmpty(t *testing.T) {
	if got := GetMagnetByEnclosure(&gofeed.Item{Title: "x"}); got != "" {
		t.Errorf("want empty, got %q", got)
	}
}

// TestHasTorrentEnclosure verifies the check that separates "nothing to fetch"
// from "fetch failed".
//
// ProcessRSSFeed returns an error only for the second case, so that a transient
// indexer failure is retried instead of dropping an episode for the whole cycle.
func TestHasTorrentEnclosure(t *testing.T) {
	cases := []struct {
		name string
		item *gofeed.Item
		want bool
	}{
		{
			name: "bittorrent enclosure",
			item: &gofeed.Item{Enclosures: []*gofeed.Enclosure{
				{Type: "application/x-bittorrent", URL: "http://127.0.0.1:1/x"},
			}},
			want: true,
		},
		{
			name: "magnet enclosure",
			item: &gofeed.Item{Enclosures: []*gofeed.Enclosure{
				{URL: "magnet:?xt=urn:btih:0123456789abcdef0123456789abcdef01234567"},
			}},
			want: true,
		},
		{
			name: "text-only item",
			item: &gofeed.Item{Title: "news", Description: "no links here"},
			want: false,
		},
		{
			name: "image enclosure only",
			item: &gofeed.Item{Enclosures: []*gofeed.Enclosure{
				{Type: "image/jpeg", URL: "https://example.com/a.jpg"},
			}},
			want: false,
		},
	}
	for _, c := range cases {
		if got := HasTorrentEnclosure(c.item); got != c.want {
			t.Errorf("%s: HasTorrentEnclosure = %v, want %v", c.name, got, c.want)
		}
	}
}
