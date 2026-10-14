// Package jackett provides a minimal Jackett/Prowlarr API client.
package jackett

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	client    = &http.Client{Timeout: 60 * time.Second}
	cookieMu  sync.Mutex
	cookieJar *cookiejar.Jar
)

// requestTimeout bounds a single Jackett API call. It matches the client
// timeout so the context is never the tighter of the two.
//
// 20s was too tight for the first request against a cold Jackett: enumerating
// hundreds of indexers took long enough to trip the deadline, which surfaced as
// "connection failed: context deadline exceeded" even though the instance was
// healthy and answering.
const requestTimeout = 60 * time.Second

func init() {
	cookieJar, _ = cookiejar.New(nil)
}

// SetProxy configures an HTTP proxy for the Jackett client.
func SetProxy(proxyURL string) {
	if proxyURL == "" {
		client.Transport = nil
		return
	}
	if u, err := url.Parse(proxyURL); err == nil {
		client.Transport = &http.Transport{
			Proxy: http.ProxyURL(u),
		}
	}
}

// Login authenticates with the Jackett admin UI and stores the session cookie.
// Uses cfg.AdminPassword if set, otherwise falls back to cfg.APIKey.
func Login(cfg Config) error {
	password := cfg.AdminPassword
	if password == "" {
		password = cfg.APIKey
	}
	loginURL := strings.TrimRight(cfg.URL, "/") + "/UI/Dashboard"
	form := url.Values{}
	form.Set("password", password)

	// Use a client that does NOT follow redirects so we can capture the auth cookie
	jarClient := &http.Client{
		Timeout: 30 * time.Second,
		Jar:     cookieJar,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse // don't follow redirects
		},
	}
	// Copy proxy settings from the main client
	if t, ok := client.Transport.(*http.Transport); ok {
		jarClient.Transport = t
	}

	req, err := http.NewRequest("POST", loginURL, strings.NewReader(form.Encode()))
	if err != nil {
		return fmt.Errorf("login request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := jarClient.Do(req)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}
	defer resp.Body.Close()

	// Jackett returns 302 to /UI/Dashboard on success. Failure also returns 302
	// to /UI/Dashboard but without setting the auth cookie.
	// Detect success by checking if we got a "Jackett" auth cookie.
	cookieMu.Lock()
	cookieJar.SetCookies(req.URL, resp.Cookies())
	baseURL, _ := url.Parse(strings.TrimRight(cfg.URL, "/"))
	cookieJar.SetCookies(baseURL, resp.Cookies())
	cookieMu.Unlock()

	// Verify we have an auth cookie
	baseURL2, _ := url.Parse(strings.TrimRight(cfg.URL, "/"))
	hasAuth := false
	for _, c := range cookieJar.Cookies(baseURL2) {
		if c.Name == "Jackett" {
			hasAuth = true
			break
		}
	}

	if !hasAuth {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("login failed — no auth cookie returned. Check admin password (HTTP %d: %s)", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	// Apply the authenticated jar to the main client
	client.Jar = cookieJar
	return nil
}

// hasAuthCookie reports whether the shared jar already holds a Jackett session
// cookie for base, so callers can skip a redundant login round trip.
func hasAuthCookie(base string) bool {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil {
		return false
	}
	cookieMu.Lock()
	defer cookieMu.Unlock()
	for _, c := range cookieJar.Cookies(u) {
		if c.Name == "Jackett" {
			return true
		}
	}
	return false
}

// Config holds Jackett connection settings.
type Config struct {
	URL           string // e.g. "http://localhost:9117"
	APIKey        string
	AdminPassword string // web UI admin password (falls back to APIKey if empty)
}

// Result is a single search result from Jackett.
type Result struct {
	Title       string
	Link        string
	MagnetURI   string
	Size        int64
	Seeders     int
	Peers       int
	Tracker     string
	TrackerName string
	Category    string
	PublishDate string
}

// IndexerInfo is a Jackett/Prowlarr configured indexer.
type IndexerInfo struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	SiteLink    string `json:"site_link"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Status      int    `json:"status"`
	Configured  bool   `json:"configured,omitempty"`
}

// rss is the Jackett/Prowlarr RSS XML structure.
type rss struct {
	XMLName xml.Name `xml:"rss"`
	Channel channel  `xml:"channel"`
}

type channel struct {
	Items []item `xml:"item"`
}

type item struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Comments    string    `xml:"comments"`
	GUID        string    `xml:"guid"`
	MagnetURI   string    `xml:"magnetURI"`
	Size        string    `xml:"size"`
	Seeders     string    `xml:"seeders"`
	Peers       string    `xml:"peers"`
	Tracker     trackerEl `xml:"jackettindexer"`
	Category    string    `xml:"category"`
	PubDate     string    `xml:"pubDate"`
	Enclosure   enclosure `xml:"enclosure"`
	TorznabAttrs []torznabAttr `xml:"http://torznab.com/schemas/2015/feed attr"`
}

type trackerEl struct {
	ID   string `xml:"id,attr"`
	Name string `xml:",chardata"`
}

type torznabAttr struct {
	Name  string `xml:"name,attr"`
	Value string `xml:"value,attr"`
}

func (it *item) attr(name string) string {
	for _, a := range it.TorznabAttrs {
		if a.Name == name {
			return a.Value
		}
	}
	return ""
}

type enclosure struct {
	URL  string `xml:"url,attr"`
	Type string `xml:"type,attr"`
}

// torznabError is the XML body Jackett returns for API-level failures.
//
// Jackett reports a bad API key as HTTP 200 with
//
//	<?xml version="1.0" encoding="UTF-8"?>
//	<error code="100" description="Invalid API Key" />
//
// and the same is true for other request errors. Checking the status code alone
// therefore cannot tell a valid key from an invalid one, so callers must inspect
// the body.
type torznabError struct {
	XMLName     xml.Name `xml:"error"`
	Code        int      `xml:"code,attr"`
	Description string   `xml:"description,attr"`
}

// checkTorznabBody reports an API-level error carried in a 200 response.
//
// Only the leading bytes are inspected: a successful response starts with
// <caps>, <indexers> or <rss>, whereas an error document is a single small
// element, so a truncated read is enough to classify it without buffering whole
// result feeds.
func checkTorznabBody(body []byte) error {
	trimmed := bytes.TrimSpace(body)
	// Strip the XML declaration if present.
	if bytes.HasPrefix(trimmed, []byte("<?xml")) {
		if i := bytes.Index(trimmed, []byte("?>")); i >= 0 {
			trimmed = bytes.TrimSpace(trimmed[i+2:])
		}
	}
	if !bytes.HasPrefix(trimmed, []byte("<error")) {
		return nil
	}
	var e torznabError
	if err := xml.Unmarshal(trimmed, &e); err != nil {
		return fmt.Errorf("jackett 返回错误响应")
	}
	desc := strings.TrimSpace(e.Description)
	if desc == "" {
		desc = "unknown error"
	}
	if e.Code == 100 || strings.Contains(strings.ToLower(desc), "api key") {
		return fmt.Errorf("API Key 无效：%s", desc)
	}
	return fmt.Errorf("Jackett 返回错误 (code %d)：%s", e.Code, desc)
}

// Test verifies the Jackett connection and that the API key is accepted.
//
// A reachable-but-unauthorised instance answers 200 just like a healthy one, so
// the response body is validated rather than only the status code.
func Test(cfg Config) error {
	u, err := url.Parse(strings.TrimRight(cfg.URL, "/") + "/api/v2.0/indexers/all/results/torznab/api")
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	q.Set("apikey", cfg.APIKey)
	q.Set("t", "caps")
	u.RawQuery = q.Encode()

	resp, err := client.Get(u.String())
	if err != nil {
		return fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return fmt.Errorf("API Key 无效 (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 8*1024))
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}
	if err := checkTorznabBody(body); err != nil {
		return err
	}
	// A valid caps document names the server; its absence means we are talking to
	// something that is not a Torznab implementation.
	if !bytes.Contains(body, []byte("<caps")) {
		return fmt.Errorf("响应不是 Torznab caps 文档，请确认地址指向 Jackett")
	}
	return nil
}

// indexerXML mirrors one <indexer> element of the Torznab t=indexers response.
type indexerXML struct {
	XMLName     xml.Name `xml:"indexer"`
	ID          string   `xml:"id,attr"`
	Configured  string   `xml:"configured,attr"`
	Title       string   `xml:"title"`
	Description string   `xml:"description"`
	Link        string   `xml:"link"`
	Language    string   `xml:"language"`
	Type        string   `xml:"type"`
}

// indexersXML is the document returned by t=indexers.
type indexersXML struct {
	XMLName  xml.Name     `xml:"indexers"`
	Indexers []indexerXML `xml:"indexer"`
}

// fetchIndexers implements the shared t=indexers request used by both the
// "configured only" and "all indexers" listings.
//
// An invalid API key is reported by Jackett as HTTP 200 carrying an <error>
// document, so the body is validated rather than the status code alone;
// otherwise a wrong key would silently look like "no indexers configured".
func fetchIndexers(cfg Config, configuredOnly bool) ([]IndexerInfo, error) {
	u, err := url.Parse(strings.TrimRight(cfg.URL, "/") + "/api/v2.0/indexers/all/results/torznab/api")
	if err != nil {
		return nil, fmt.Errorf("invalid URL: %w", err)
	}
	q := u.Query()
	q.Set("apikey", cfg.APIKey)
	q.Set("t", "indexers")
	if configuredOnly {
		q.Set("configured", "true")
	}
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if err := checkTorznabBody(body); err != nil {
		return nil, err
	}

	var data indexersXML
	if err := xml.Unmarshal(body, &data); err != nil {
		return nil, fmt.Errorf("xml decode: %w", err)
	}

	var indexers []IndexerInfo
	for _, idx := range data.Indexers {
		if configuredOnly && idx.Configured != "true" {
			continue
		}
		indexers = append(indexers, IndexerInfo{
			ID:          idx.ID,
			Name:        idx.Title,
			SiteLink:    idx.Link,
			Language:    idx.Language,
			Type:        idx.Type,
			Description: idx.Description,
			Status:      1,
			Configured:  idx.Configured == "true",
		})
	}
	return indexers, nil
}

// ListIndexers returns the configured indexers of a Jackett instance.
func ListIndexers(cfg Config) ([]IndexerInfo, error) {
	return fetchIndexers(cfg, true)
}

// ListAllIndexers returns every known indexer, configured or not.
func ListAllIndexers(cfg Config) ([]IndexerInfo, error) {
	return fetchIndexers(cfg, false)
}

// Search queries a Jackett/Prowlarr instance and returns parsed results.
// offset is the starting offset for pagination (0 = first page).
// SearchLimit is the number of results requested from Jackett per call.
//
// Without a limit Jackett queries every configured tracker in full and returns
// everything it finds: a broad query such as "TARI TARI" came back with 1185
// items after ~19.5s, which both sat right on the request deadline and buried
// the few relevant results. Capping the page finishes in ~2.5s.
//
// It also has to stay in step with Jackett's fixed Torznab paging, which the
// panel's "next page" control advances by this same amount.
const SearchLimit = 100

func Search(cfg Config, query string, categories []int, offset int) ([]Result, error) {
	u, err := url.Parse(strings.TrimRight(cfg.URL, "/") + "/api/v2.0/indexers/all/results/torznab/api")
	if err != nil {
		return nil, fmt.Errorf("jackett url: %w", err)
	}
	q := u.Query()
	q.Set("apikey", cfg.APIKey)
	q.Set("t", "search")
	q.Set("q", query)
	q.Set("limit", strconv.Itoa(SearchLimit))
	if offset > 0 {
		q.Set("offset", strconv.Itoa(offset))
	}
	if len(categories) > 0 {
		catStrs := make([]string, len(categories))
		for i, c := range categories {
			catStrs[i] = strconv.Itoa(c)
		}
		q.Set("cat", strings.Join(catStrs, ","))
	}
	u.RawQuery = q.Encode()

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jackett request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("jackett HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("jackett read: %w", err)
	}
	// Jackett answers an unauthorised search with HTTP 200 and an <error>
	// document. Without this check the unmarshal below would succeed with zero
	// items, silently presenting "no results" for a bad API key.
	if err := checkTorznabBody(data); err != nil {
		return nil, err
	}

	var rssData rss
	if err := xml.Unmarshal(data, &rssData); err != nil {
		return nil, fmt.Errorf("jackett xml: %w", err)
	}

	results := make([]Result, 0, len(rssData.Channel.Items))
	for _, it := range rssData.Channel.Items {
		// Use <comments> (site page) as Link if available, fall back to <guid>/<link>
		pageLink := it.Comments
		if pageLink == "" {
			pageLink = it.GUID
		}
		if pageLink == "" {
			pageLink = it.Link
		}
		// Use enclosure URL for magnet/torrent if available
		magnetLink := it.MagnetURI
		if magnetLink == "" && it.Enclosure.URL != "" && it.Enclosure.Type == "application/x-bittorrent" {
			magnetLink = it.Enclosure.URL
		}
		// Use GUID (direct .torrent) if no magnet
		if magnetLink == "" && it.GUID != "" {
			magnetLink = it.GUID
		}
		// Fall back to Link
		if magnetLink == "" {
			magnetLink = it.Link
		}
		// Parse seeders from torznab attrs (torznab:attr), fallback to direct element
		seedersStr := it.attr("seeders")
		if seedersStr == "" {
			seedersStr = it.Seeders
		}
		seeders, _ := strconv.Atoi(strings.TrimSpace(seedersStr))
		peersStr := it.attr("peers")
		if peersStr == "" {
			peersStr = it.Peers
		}
		peers, _ := strconv.Atoi(strings.TrimSpace(peersStr))

		r := Result{
			Title:       it.Title,
			Link:        pageLink,
			MagnetURI:   magnetLink,
			Tracker:     it.Tracker.ID,
			TrackerName: strings.TrimSpace(it.Tracker.Name),
			Category:    it.Category,
			PublishDate: it.PubDate,
			Seeders:     seeders,
			Peers:       peers,
		}
		if s, err := strconv.ParseInt(it.Size, 10, 64); err == nil {
			r.Size = s
		}
		results = append(results, r)
	}
	return results, nil
}

// AddIndexer configures an indexer in Jackett by its ID.
// Uses Jackett's admin API: POST /api/v2.0/indexers
// AddIndexer adds/activates an indexer in Jackett by fetching its default config
// and posting it to the indexer's config endpoint.
// Requires admin login cookie; calls Login automatically.
//
// Each HTTP step gets its own deadline. They used to share a single context, so
// on a slow host (Jackett reloads and re-tests every indexer after a config
// change) the earlier steps consumed the whole budget and the POST died with
// "context deadline exceeded" after ~60s, even though the same POST completes in
// about a second on its own.
func AddIndexer(cfg Config, id string) error {
	// Reuse an existing session when we already hold a valid cookie; logging in
	// on every call doubles the round trips exactly when Jackett is busiest.
	if !hasAuthCookie(cfg.URL) {
		if err := Login(cfg); err != nil {
			return fmt.Errorf("login: %w", err)
		}
	}

	baseURL := strings.TrimRight(cfg.URL, "/")

	// Step 1: GET default config
	cfgURL, err := url.Parse(baseURL + "/api/v2.0/indexers/" + id + "/config")
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", cfgURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create GET request: %w", err)
	}
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("get config: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d getting config: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	configJSON, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read config: %w", err)
	}

	// Check if this indexer requires credentials that we can't provide
	if needsManualConfig(configJSON) {
		return fmt.Errorf("此索引器需要登录凭据（用户名/密码/Cookie），请在 Jackett WebUI 中手动添加: %s/ui/indexers", baseURL)
	}

	// Step 2: POST config to add the indexer. A fresh deadline: this is the step
	// Jackett makes slow, and it must not inherit time already spent above.
	postURL, err := url.Parse(baseURL + "/api/v2.0/indexers/" + id + "/config")
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel2()

	postReq, err := http.NewRequestWithContext(ctx2, "POST", postURL.String(), strings.NewReader(string(configJSON)))
	if err != nil {
		return fmt.Errorf("create POST request: %w", err)
	}
	postReq.Header.Set("Content-Type", "application/json")
	postReq.Header.Set("Accept", "application/json")

	postResp, err := client.Do(postReq)
	if err != nil {
		return fmt.Errorf("add indexer: %w", err)
	}
	defer postResp.Body.Close()

	if postResp.StatusCode >= 400 {
		body, _ := io.ReadAll(postResp.Body)
		msg := parseJackettError(body)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		// Truncate for display
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return fmt.Errorf("HTTP %d: %s", postResp.StatusCode, msg)
	}
	return nil
}

// needsManualConfig checks whether the config JSON has required credential
// fields (username, password, apikey, cookie) that are null/empty,
// meaning the indexer can't be auto-configured via API.
func needsManualConfig(configJSON []byte) bool {
	var items []struct {
		ID    string      `json:"id"`
		Type  string      `json:"type"`
		Value interface{} `json:"value"`
	}
	if json.Unmarshal(configJSON, &items) != nil {
		return false // can't parse, try anyway
	}
	for _, it := range items {
		if it.Type == "hiddendata" || it.Type == "inputtags" || it.Type == "displayinfo" {
			continue
		}
		if it.Value == nil {
			return true
		}
		if s, ok := it.Value.(string); ok && s == "" && (it.ID == "username" || it.ID == "password" || it.ID == "apikey" || it.ID == "cookie") {
			return true
		}
	}
	return false
}

// parseJackettError extracts the error message from a Jackett JSON error response.
// Jackett returns: {"config": [...], "result": "error", "error": "message"}
func parseJackettError(body []byte) string {
	var errResp struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if json.Unmarshal(body, &errResp) == nil && errResp.Result == "error" {
		return errResp.Error
	}
	return ""
}

// DeleteIndexer removes an indexer from Jackett.
// Requires admin login cookie; calls Login automatically.
func DeleteIndexer(cfg Config, id string) error {
	if err := Login(cfg); err != nil {
		return fmt.Errorf("login: %w", err)
	}

	baseURL := strings.TrimRight(cfg.URL, "/")
	delURL, err := url.Parse(baseURL + "/api/v2.0/indexers/" + id)
	if err != nil {
		return fmt.Errorf("invalid URL: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), requestTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "DELETE", delURL.String(), nil)
	if err != nil {
		return fmt.Errorf("create DELETE request: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("delete indexer: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		msg := parseJackettError(body)
		if msg == "" {
			msg = strings.TrimSpace(string(body))
		}
		if len(msg) > 300 {
			msg = msg[:300] + "…"
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, msg)
	}
	return nil
}
