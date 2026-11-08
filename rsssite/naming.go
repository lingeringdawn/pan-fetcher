package rsssite

import (
	neturl "net/url"
	"strings"
)

// ExtractNameFromMagnet returns the display name a magnet carries in dn=.
//
// Magnets built by indexers usually include the release name there, which makes it
// the best label available when the caller did not pass a title of its own.
func ExtractNameFromMagnet(magnet string) string {
	for _, part := range strings.Split(magnet, "&") {
		if strings.HasPrefix(strings.ToLower(part), "dn=") {
			name, err := neturl.QueryUnescape(part[3:])
			if err != nil {
				return ""
			}
			return name
		}
	}
	return ""
}

// ExtractNameFromURL returns the release name carried by a download link.
//
// Indexers — and the Jackett proxy in front of them — hand back a .torrent URL and
// put the release name in a query parameter, because no magnet exists at that
// point. Without reading it, a submission has no label to show and displays the
// whole URL instead.
func ExtractNameFromURL(raw string) string {
	u, err := neturl.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, key := range []string{"file", "title", "name"} {
		if v := strings.TrimSpace(q.Get(key)); v != "" {
			return v
		}
	}
	return ""
}

// TaskDisplayName picks the most readable label available for a submitted task.
//
// Order: the caller's title, the magnet's dn=, the download link's file=. The raw
// task string is the last resort — showing it is what made a batch submission
// notify with an encrypted-looking URL instead of the release name.
func TaskDisplayName(task, title string) string {
	if title = strings.TrimSpace(title); title != "" {
		return title
	}
	if n := ExtractNameFromMagnet(task); n != "" {
		return n
	}
	if n := ExtractNameFromURL(task); n != "" {
		return n
	}
	return task
}
