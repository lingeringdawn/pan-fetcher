// Package indexer holds the shared search-result model used by the Web UI.
//
// It previously contained a full Cardigann YAML search engine for built-in
// indexers. That engine has been removed: all searching now goes through
// Jackett, which owns indexer definitions and their maintenance. What remains
// here are the display types the UI and the search cache are built around, so
// that results from Jackett have a single stable representation.
package indexer

import (
	"sort"
	"time"
)

// SearchResult is a unified search result shown in the UI and cached to disk.
type SearchResult struct {
	Title       string    `json:"title"`
	MagnetURL   string    `json:"magnet_url"`
	TorrentURL  string    `json:"torrent_url,omitempty"`
	Size        int64     `json:"size"`
	SizeFmt     string    `json:"-"` // pre-formatted size string for display
	DateFmt     string    `json:"-"` // pre-formatted date string for display
	Seeders     int       `json:"seeders"`
	Leechers    int       `json:"leechers"`
	PublishDate time.Time `json:"publish_date"`
	Category    string    `json:"category"`
	IndexerID   string    `json:"indexer_id"`
	IndexerName string    `json:"indexer_name"`
	InfoHash    string    `json:"info_hash,omitempty"`
	PageURL     string    `json:"page_url,omitempty"`
	Group       string    `json:"group,omitempty"` // fansub group from first [...] tag
}

// IndexerInfo is the runtime info for an activated indexer, as rendered by the
// indexer management page and the search filter list.
type IndexerInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Type      string `json:"type"`
	Language  string `json:"language"`
	SiteLink  string `json:"site_link"`
	Enabled   bool   `json:"enabled"`
	Healthy   bool   `json:"healthy"`
	HasLogin  bool   `json:"has_login"`
	LastError string `json:"last_error,omitempty"`
	LastTest  string `json:"last_test,omitempty"`
	Source    string `json:"source,omitempty"` // "local" or "jackett"
}

// SearchAllErrors bundles search results with any per-source error messages.
//
// With Jackett as the only backend there is normally at most one entry in
// Errors, but the shape is kept so the UI can keep rendering a list of failures
// without special-casing a single backend.
type SearchAllErrors struct {
	Results []SearchResult
	Errors  map[string]string // source name → error message
}

// SortResults orders results in place by the given key.
//
// Supported keys are "size", "date" and anything else (treated as "seeds").
// The seeders sort falls back to publish date so that equally-seeded results
// stay in a deterministic, newest-first order.
func SortResults(results []SearchResult, sortBy string) {
	sort.Slice(results, func(i, j int) bool {
		switch sortBy {
		case "size":
			return results[i].Size > results[j].Size
		case "date":
			return results[i].PublishDate.After(results[j].PublishDate)
		default: // "seeds"
			if results[i].Seeders != results[j].Seeders {
				return results[i].Seeders > results[j].Seeders
			}
			return results[i].PublishDate.After(results[j].PublishDate)
		}
	})
}
