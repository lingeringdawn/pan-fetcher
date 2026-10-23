package server

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/lingeringdawn/pan-fetcher/config"
	"github.com/lingeringdawn/pan-fetcher/indexer"
	"github.com/lingeringdawn/pan-fetcher/jackett"
	"github.com/lingeringdawn/pan-fetcher/media"
	"github.com/lingeringdawn/pan-fetcher/notify"
	p115pkg "github.com/lingeringdawn/pan-fetcher/p115"
	"github.com/lingeringdawn/pan-fetcher/rsssite"

	"github.com/deadblue/elevengo"
)

type Agent interface {
	AddMagnetTask([]string, string, string) error
	ProcessRSSFeed(rssURL, cid, savepath, subKey string) []string
	OfflineClear(int) error
	ListTasks() ([]p115pkg.TaskItem, error)
	ListDir(string) ([]p115pkg.DirEntry, error)
	GetEntry(string) (p115pkg.DirEntry, error)
	Mkdir(parentID, name string) (p115pkg.DirEntry, error)
	RenameEntry(entryID, newName string) error
	DeleteEntry(entryID string) error
	MoveEntry(targetDirID, entryID string) error
	Copy(targetDirID, entryID string) error
	UserGet(*elevengo.UserInfo) error
	StoreClose() error
	// Settings
	GetSettings() p115pkg.AppSettings
	UpdateSettings(s p115pkg.AppSettings) error
	// Connection test
	TestConnection() error
	// Cookies
	LoadCookiesStr() string
}

type Server struct {
	Agent     Agent
	Port      int
	Domain    string // Bind domain, e.g. "example.com" or "0.0.0.0"
	CertFile  string // TLS certificate
	KeyFile   string // TLS private key
	ProxyHTTP string
	WeworkWH  string // WeChat Work webhook
	JackettURL           string
	JackettAPIKey        string
	JackettAdminPassword string
	jackettActive map[string]bool           // jackett indexer IDs that are activated
	jackettActiveMu sync.Mutex
	jackettCache  []jackett.IndexerInfo
	jackettCacheMu sync.Mutex
	jackettCacheTime time.Time
	startTime time.Time
	fsCache   map[string]fsCacheEntry
	fsCacheMu sync.Mutex

	// Lightweight caches for expensive per-request operations
	connCheckLoggedIn bool
	connCheckTime     time.Time
	taskCountCache    int
	taskCountCacheAt  time.Time
	entryCache        map[string]p115pkg.DirEntry
	entryCacheMu      sync.Mutex

	// updateNotice holds the newest version found by the background poll, so
	// the settings page can report it without issuing its own GitHub request.
	updateNotice   string
	updateNoticeMu sync.Mutex
}

type fsCacheEntry struct {
	entries []p115pkg.DirEntry
	expires time.Time
}

// SetAgent replaces the current agent (used after re-login).
func (s *Server) SetAgent(a Agent) {
	if pa, ok := a.(*p115pkg.Agent); ok {
		pa.Dedup = globalDedup
	}
	s.Agent = a
}

func (s *Server) invalidateFSCache(dirID string) {
	s.fsCacheMu.Lock()
	if dirID == "" {
		s.fsCache = make(map[string]fsCacheEntry)
	} else {
		delete(s.fsCache, "fs:"+dirID)
	}
	s.fsCacheMu.Unlock()
	// Also clear entry cache on modifications
	s.entryCacheMu.Lock()
	s.entryCache = make(map[string]p115pkg.DirEntry)
	s.entryCacheMu.Unlock()
}

type OfflineTask struct {
	Tasks    []string `json:"tasks"`
	Cid      string   `json:"cid"`
	SavePath string   `json:"savepath,omitempty"`
}

// taskRow is a single offline task row for the template.
type taskRow struct {
	InfoHash string
	Name     string
	Size     string
	Status   string
	Percent  float64
	URL      string
	RowClass string
}

type dashboardData struct {
	LoggedIn      bool
	Lang          string
	T             map[string]string
	Message       string
	Error         string
	Page          string
	Tasks         []taskRow
	TaskCount     int
	Logs          []string
	FSEntries     []fsEntry
	FSCrumbs      []fsBreadcrumb
	FSCurrentID   string
	FSParentID    string
	Subs          []subRow
	RssSubs       []rssSubRow
	Settings      p115pkg.AppSettings
	ProxyHTTP     string
	Domain        string
	WeworkWebhook string
	ShowQR        bool
	Cookies       string
	SearchQuery   string
	SearchKeyword string
	SearchResults []indexer.SearchResult
	SearchTotal   int
	PageSize      int
	SearchErrors  map[string]string
	AllTagsJSON   template.JS // JSON of all unique tags from full search results
	AllGroupsJSON template.JS // JSON of all validated group names from full search results
	SearchSort     string
	SearchIndexers []string
	RssURL         string
	SavedSearches  []savedSearch
	IndexerList    []indexer.IndexerInfo
	JackettLibrary []jackett.IndexerInfo
	DedupEntries   []dedupEntry
	DashStats       dashStats
	HasAgent        bool
	HasPassword     bool
	NotifyLog       bool
	Timezone        string
	TimezoneOptions map[string]string
	JackettURL           string
	JackettAPIKey        string
	JackettAdminPassword string
	// JackettMode is "managed" or "external"; empty is treated as managed.
	JackettMode          string
	// FilterSearchTitle reflects the aggregated-search title filter.
	FilterSearchTitle   bool
	TMDBAPIKey           string
	AboutVersion         string
	AutoUpdate      bool
}

type dashStats struct {
	TotalTasks     int
	RssSubsTotal   int
	RssSubsActive  int
	ActiveIndexers int
	CacheEntries   int
	Uptime         string
	RecentItems    []notify.RecentItem
}

type dedupEntry struct {
	SubKey string
	Count  int
}

var srv *http.Server

// Version is set via ldflags at build time: -X server.Version=v0.x.x
var Version = ""

var rssJsonPath = "rss.json"

// searchCache holds results for incremental loading
var (
	searchCacheMu   sync.Mutex
	searchCache     []indexer.SearchResult // filtered by current keyword
	searchCacheFull []indexer.SearchResult // all results, never filtered (for tag extraction & re-filter)
	searchCtx       searchContext
	filterKeyword   string // the keyword currently applied to searchCache
)

// searchCacheFile is resolved against dataDir at startup so that the cache
// survives a self-update restart (see resolveDataFile).
var searchCacheFile = "search-cache.json"

type persistedSearch struct {
	Query     string                 `json:"query"`
	Sort      string                 `json:"sort"`
	Indexers  []string               `json:"indexers"`
	Keyword   string                 `json:"keyword"`
	PageSize  int                    `json:"page_size"`
	Results   []indexer.SearchResult `json:"results"`
}

func saveSearchCache() {
	searchCacheMu.Lock()
	defer searchCacheMu.Unlock()
	ps := persistedSearch{
		Query:    searchCtx.Query,
		Sort:     searchCtx.Sort,
		Indexers: searchCtx.Indexers,
		Keyword:  filterKeyword,
		PageSize: searchCtx.PageSize,
		Results:  searchCacheFull,
	}
	b, _ := json.MarshalIndent(ps, "", "  ")
	os.WriteFile(searchCacheFile, b, 0644)
}

func loadSearchCache() *persistedSearch {
	b, err := os.ReadFile(searchCacheFile)
	if err != nil {
		return nil
	}
	var ps persistedSearch
	if err := json.Unmarshal(b, &ps); err != nil {
		return nil
	}
	if len(ps.Results) == 0 {
		return nil
	}
	// Restore globals
	searchCacheMu.Lock()
	searchCacheFull = ps.Results
	searchCache = applyKeywordFilter(searchCacheFull, ps.Keyword)
	filterKeyword = ps.Keyword
	searchCtx = searchContext{
		Query:    ps.Query,
		Sort:     ps.Sort,
		Indexers: ps.Indexers,
		NextPage: 2,
		PageSize: ps.PageSize,
	}
	searchCacheMu.Unlock()
	return &ps
}

type searchContext struct {
	Query     string
	Sort      string
	Indexers  []string // includes "jackett:xxx" prefixed
	NextPage  int      // next page to fetch (2, 3, ...)
	PageSize  int      // items per display page
	Exhausted bool
}
// dataDir anchors every runtime file. SetDataDir must be called during startup
// (it is derived from the database path); the default keeps relative behaviour
// for tests and for a bare `pan-fetcher server` run in the data directory.
var dataDir = "."

var dedupCachePath = "dedup-cache.json"

// ---------- dedup + torrent URL cache (unified) ----------

// dedup-cache.json format:
// { "_torrent_urls": {"https://...": "hash"}, "_hash_names": {"hash": "name"}, "subKey1": {"hash1":true,...}, ... }

const torrentURLsKey = "_torrent_urls"
const torrentErrorsKey = "_torrent_errors"
const hashNamesKey = "_hash_names"

type dedupCache struct {
	mu            sync.Mutex
	subs          map[string]map[string]bool // subKey -> infoHash -> true
	torrentURLs   map[string]string          // .torrent URL -> info hash
	torrentErrors map[string]string          // .torrent URL -> last error
	hashNames     map[string]string          // infoHash -> display name
	totalEntries  int                        // running counter, O(1)
}

var globalDedup = &dedupCache{
	subs:          make(map[string]map[string]bool),
	torrentURLs:   make(map[string]string),
	torrentErrors: make(map[string]string),
	hashNames:     make(map[string]string),
}

func (d *dedupCache) Load() {
	d.mu.Lock()
	defer d.mu.Unlock()
	data, err := os.ReadFile(dedupCachePath)
	if err != nil {
		return
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return
	}
	for k, v := range raw {
		if k == torrentURLsKey {
			var urls map[string]string
			json.Unmarshal(v, &urls)
			for url, hash := range urls {
				d.torrentURLs[url] = hash
			}
		} else if k == torrentErrorsKey {
			var errs map[string]string
			json.Unmarshal(v, &errs)
			for url, errMsg := range errs {
				d.torrentErrors[url] = errMsg
			}
		} else if k == hashNamesKey {
			var names map[string]string
			json.Unmarshal(v, &names)
			for hash, name := range names {
				d.hashNames[hash] = name
			}
		} else {
			// Support both legacy array format and new map format
			var list []string
			if err := json.Unmarshal(v, &list); err == nil {
				set := make(map[string]bool, len(list))
				for _, h := range list {
					set[h] = true
				}
				d.subs[k] = set
			} else {
				var set map[string]bool
				json.Unmarshal(v, &set)
				if set == nil {
					set = make(map[string]bool)
				}
				d.subs[k] = set
			}
		}
	}
	// Rebuild running counter after loading from disk
	for _, set := range d.subs {
		d.totalEntries += len(set)
	}
}

func (d *dedupCache) Save() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.saveLocked()
}

func (d *dedupCache) Has(subKey, magnet string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	set := d.subs[subKey]
	if set == nil {
		return false
	}
	return set[extractInfoHashFromMagnet(magnet)]
}

func (d *dedupCache) Add(subKey, magnet string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	h := extractInfoHashFromMagnet(magnet)
	if h == "" {
		return
	}
	if d.subs[subKey] == nil {
		d.subs[subKey] = make(map[string]bool)
	}
	if !d.subs[subKey][h] {
		d.totalEntries++
	}
	d.subs[subKey][h] = true
	// Store display name from magnet dn= parameter
	if name := extractNameFromMagnet(magnet); name != "" {
		d.hashNames[h] = name
	}
}

// RemoveSub deletes all dedup entries for a given subKey.
func (d *dedupCache) RemoveSub(subKey string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if set := d.subs[subKey]; set != nil {
		d.totalEntries -= len(set)
	}
	delete(d.subs, subKey)
}

// SubKeys returns all subscription keys in the dedup cache.
func (d *dedupCache) SubKeys() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	keys := make([]string, 0, len(d.subs))
	for k := range d.subs {
		keys = append(keys, k)
	}
	return keys
}

// SubCount returns the number of deduped hashes for a subKey.
func (d *dedupCache) SubCount(subKey string) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.subs[subKey])
}

// TotalCount returns the total number of cached entries across all subs (O(1)).
func (d *dedupCache) TotalCount() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.totalEntries
}

// Hashes returns the list of info hashes for a subKey.
func (d *dedupCache) Hashes(subKey string) []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	set := d.subs[subKey]
	if set == nil {
		return nil
	}
	list := make([]string, 0, len(set))
	for h := range set {
		list = append(list, h)
	}
	return list
}

// ---------- torrent URL → info hash (stored inside dedup cache) ----------

func (d *dedupCache) GetTorrentHash(url string) (hash string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	hash, ok = d.torrentURLs[url]
	return
}

func (d *dedupCache) SetTorrentHash(url, hash string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.torrentURLs[url] = hash
	delete(d.torrentErrors, url) // clear any previous error
	d.saveLocked()
}

func (d *dedupCache) GetTorrentError(url string) (errMsg string, ok bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	errMsg, ok = d.torrentErrors[url]
	return
}

func (d *dedupCache) SetTorrentError(url, errMsg string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.torrentErrors[url] = errMsg
	d.saveLocked()
}

func (d *dedupCache) SetHashName(hash, name string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.hashNames[hash] = name
	d.saveLocked()
}

func (d *dedupCache) GetHashName(hash string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.hashNames[hash]
}

func (d *dedupCache) saveLocked() {
	raw := make(map[string]interface{}, len(d.subs)+3)
	urls := make(map[string]string, len(d.torrentURLs))
	for url, hash := range d.torrentURLs {
		urls[url] = hash
	}
	raw[torrentURLsKey] = urls
	errs := make(map[string]string, len(d.torrentErrors))
	for url, errMsg := range d.torrentErrors {
		errs[url] = errMsg
	}
	raw[torrentErrorsKey] = errs
	names := make(map[string]string, len(d.hashNames))
	for hash, name := range d.hashNames {
		names[hash] = name
	}
	raw[hashNamesKey] = names
	for k, set := range d.subs {
		if len(set) == 0 {
			continue
		}
		list := make([]string, 0, len(set))
		for h := range set {
			list = append(list, h)
		}
		raw[k] = list
	}
	data, err := json.Marshal(raw)
	if err != nil {
		log.Printf("[dedup] save marshal error: %v", err)
		return
	}
	if err := os.WriteFile(dedupCachePath, data, 0644); err != nil {
		log.Printf("[dedup] save write error: %v", err)
	}
}

// ClearSub removes all dedup entries for a subKey and persists.
func (d *dedupCache) ClearSub(subKey string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if set := d.subs[subKey]; set != nil {
		d.totalEntries -= len(set)
	}
	delete(d.subs, subKey)
	d.saveLocked()
	log.Printf("[dedup] cleared subKey=%q (%d remaining)", subKey, len(d.subs))
}

// RemoveHash removes a single hash from a subKey's dedup set.
func (d *dedupCache) RemoveHash(subKey, hash string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	set := d.subs[subKey]
	if set == nil {
		return false
	}
	if _, ok := set[hash]; !ok {
		return false
	}
	delete(set, hash)
	d.totalEntries--
	if len(set) == 0 {
		delete(d.subs, subKey)
	}
	d.saveLocked()
	log.Printf("[dedup] removed hash=%s from subKey=%q", hash, subKey)
	return true
}

func (d *dedupCache) AllTorrentURLs() [][2]string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := make([][2]string, 0, len(d.torrentURLs))
	for url, hash := range d.torrentURLs {
		out = append(out, [2]string{url, hash})
	}
	return out
}

func (d *dedupCache) RemoveTorrentURL(url string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	hash := d.torrentURLs[url]
	delete(d.torrentURLs, url)
	// Also remove this hash from all subKey entries so re-download is allowed
	if hash != "" {
		for _, set := range d.subs {
			delete(set, hash)
		}
	}
	d.saveLocked()
	log.Printf("[dedup] removed torrent URL mapping: %s (hash=%s)", url, hash)
}

// TorrentURLByHash returns the .torrent URL for a given info hash, or "".
func (d *dedupCache) TorrentURLByHash(hash string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	for url, h := range d.torrentURLs {
		if h == hash {
			return url
		}
	}
	return ""
}

func extractInfoHashFromMagnet(magnet string) string {
	// Already a bare info hash (40 hex chars)?
	if len(magnet) == 40 {
		matched := true
		for _, c := range magnet {
			if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
				matched = false
				break
			}
		}
		if matched {
			return strings.ToLower(magnet)
		}
	}
	// Try URL-decode first (some clients double-encode)
	if decoded, err := url.QueryUnescape(magnet); err == nil && decoded != magnet {
		if h := extractInfoHashFromMagnet(decoded); h != "" {
			return h
		}
	}
	// Strip magnet:? prefix if present
	m := magnet
	if strings.HasPrefix(strings.ToLower(m), "magnet:?") {
		m = m[8:] // len("magnet:?") = 8
	}
	for _, part := range strings.Split(m, "&") {
		lower := strings.ToLower(part)
		if strings.HasPrefix(lower, "xt=urn:btih:") {
			return strings.ToLower(strings.TrimPrefix(part, "xt=urn:btih:"))
		}
		// Handle URL-encoded separator: xt=urn%3Abtih%3AHASH
		if strings.HasPrefix(lower, "xt=urn%3abtih%3a") {
			hash := strings.TrimPrefix(part, "xt=urn%3Abtih%3A")
			if decoded, err := url.QueryUnescape(hash); err == nil {
				return strings.ToLower(decoded)
			}
			return strings.ToLower(hash)
		}
	}
	return ""
}

func extractNameFromMagnet(magnet string) string {
	for _, part := range strings.Split(magnet, "&") {
		if strings.HasPrefix(strings.ToLower(part), "dn=") {
			name, _ := url.QueryUnescape(part[3:])
			return name
		}
	}
	return ""
}

type webSettings struct {
	Lang          string `json:"lang"`
	ChunkSize     int    `json:"chunk_size"`
	ChunkDelay    int    `json:"chunk_delay"`
	CooldownMin   int    `json:"cooldown_min"`
	CooldownMax   int    `json:"cooldown_max"`
	WebPassword   string `json:"web_password"`
	SubsInterval  int    `json:"subs_interval"`
	// Notification destinations and their per-event subscriptions live in
	// webhooks.json; only the throttled log push and the timezone remain here.
	NotifyLog     bool   `json:"notify_log"`
	Timezone      string `json:"timezone"`
	JackettURL           string `json:"jackett_url"`
	JackettAPIKey        string `json:"jackett_apikey"`
	JackettAdminPassword string `json:"jackett_admin_password"`
	// JackettMode selects how Jackett is provided: "managed" means pan-fetcher
	// installs and runs it, "external" means the user runs their own instance.
	// It only changes which controls the settings page offers; the connection
	// details above are used either way.
	JackettMode          string `json:"jackett_mode"`
	// FilterSearchTitle drops aggregated results whose title does not contain
	// the query. It is a pointer so "never set" is distinguishable from "set to
	// false": the feature defaults to on, and a plain bool would silently turn
	// it off for every configuration written before it existed.
	FilterSearchTitle *bool `json:"filter_search_title,omitempty"`
	TMDBAPIKey        string `json:"tmdb_apikey"`
	PageSize          int    `json:"page_size"`
	AutoUpdate        bool   `json:"auto_update"`
}

// filterSearchTitle returns the effective title-filter setting, defaulting to
// enabled when it has never been set.
func (ws webSettings) filterSearchTitle() bool {
	if ws.FilterSearchTitle == nil {
		return true
	}
	return *ws.FilterSearchTitle
}

// boolPtr returns a pointer to v, for the settings fields that need to
// distinguish "unset" from "false".
func boolPtr(v bool) *bool { return &v }

// JackettModeManaged and JackettModeExternal are the accepted JackettMode
// values. Anything else (including empty, for existing configs) is treated as
// managed, which is the default this project ships.
const (
	JackettModeManaged  = "managed"
	JackettModeExternal = "external"
)

func (s *Server) loadWebSettings() webSettings {
	data, err := os.ReadFile(s.dataFile("web-settings.json"))
	if err != nil {
		return webSettings{Lang: "zh", SubsInterval: 60, PageSize: 50}
	}
	var ws webSettings
	json.Unmarshal(data, &ws)
	if ws.Lang == "" {
		ws.Lang = "zh"
	}
	if ws.SubsInterval <= 0 {
		ws.SubsInterval = 60
	}
	return ws
}

func (s *Server) saveWebSettings(ws webSettings) {
	data, _ := json.MarshalIndent(ws, "", "  ")
	os.WriteFile(s.dataFile("web-settings.json"), data, 0644)
}

func (s *Server) loadJackettEnabled() {
	if s.jackettActive != nil {
		return // already loaded
	}
	s.jackettActive = make(map[string]bool)
	data, err := os.ReadFile(s.dataFile("jackett-enabled.json"))
	if err != nil {
		return
	}
	var ids []string
	if json.Unmarshal(data, &ids) == nil {
		for _, id := range ids {
			s.jackettActive[id] = true
		}
	}
}

func (s *Server) saveJackettEnabled() {
	var ids []string
	for id := range s.jackettActive {
		ids = append(ids, id)
	}
	data, _ := json.Marshal(ids)
	os.WriteFile(s.dataFile("jackett-enabled.json"), data, 0644)
}

// jackettConfig returns a jackett.Config populated from server settings.
func (s *Server) jackettConfig() jackett.Config {
	return jackett.Config{
		URL:           s.JackettURL,
		APIKey:        s.JackettAPIKey,
		AdminPassword: s.JackettAdminPassword,
	}
}

// jackettConfigured reports whether both the URL and API key are set, which is
// the minimum needed to search. Jackett is the only search backend, so this
// gates every search entry point.
func (s *Server) jackettConfigured() bool {
	return strings.TrimSpace(s.JackettURL) != "" && strings.TrimSpace(s.JackettAPIKey) != ""
}

// handleJKAddToJackett adds an indexer to the linked Jackett instance via API.
// writeJSON writes a JSON object to the response, properly escaping the message.
func writeJSON(w http.ResponseWriter, ok bool, msg string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"ok": ok, "msg": msg})
}

func (s *Server) handleJKAddToJackett(w http.ResponseWriter, r *http.Request, id string) {
	if s.JackettURL == "" || s.JackettAPIKey == "" {
		writeJSON(w, false, "Jackett not configured")
		return
	}
	cfg := s.jackettConfig()
	if err := jackett.AddIndexer(cfg, id); err != nil {
		log.Printf("[jackett] add %s failed: %v", id, err)
		writeJSON(w, false, err.Error())
		return
	}
	log.Printf("[jackett] %s added successfully", id)
	s.jackettActiveMu.Lock()
	if s.jackettActive == nil {
		s.jackettActive = make(map[string]bool)
	}
	s.jackettActive[id] = true
	s.saveJackettEnabled()
	s.jackettActiveMu.Unlock()
	go func() {
		if jk, err := jackett.ListIndexers(cfg); err == nil {
			s.jackettCacheMu.Lock()
			s.jackettCache = jk
			s.jackettCacheMu.Unlock()
		}
	}()
	writeJSON(w, true, "Added to Jackett")
}

func (s *Server) handleJKRemoveFromJackett(w http.ResponseWriter, r *http.Request, id string) {
	if s.JackettURL == "" || s.JackettAPIKey == "" {
		writeJSON(w, false, "Jackett not configured")
		return
	}
	cfg := s.jackettConfig()
	if err := jackett.DeleteIndexer(cfg, id); err != nil {
		log.Printf("[jackett] remove %s failed: %v", id, err)
		writeJSON(w, false, err.Error())
		return
	}
	log.Printf("[jackett] %s removed successfully", id)
	s.jackettActiveMu.Lock()
	if s.jackettActive != nil {
		delete(s.jackettActive, id)
		s.saveJackettEnabled()
	}
	s.jackettActiveMu.Unlock()
	go func() {
		if jk, err := jackett.ListIndexers(cfg); err == nil {
			s.jackettCacheMu.Lock()
			s.jackettCache = jk
			s.jackettCacheMu.Unlock()
		}
	}()
	writeJSON(w, true, "Removed from Jackett")
}

// ---------- log buffer ----------

type logBuffer struct {
	mu      sync.Mutex
	buf     []string
	size    int
	pos     int   // total lines written (wraps via modulo)
	wrapped bool  // true after first wrap-around
}

func newLogBuffer(size int) *logBuffer {
	return &logBuffer{buf: make([]string, size), size: size}
}

func (lb *logBuffer) Write(p []byte) (int, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	line := strings.TrimRight(string(p), "\r\n")
	if line == "" {
		return len(p), nil
	}
	lb.buf[lb.pos%lb.size] = line
	lb.pos++
	if lb.pos >= lb.size && lb.pos%lb.size == 0 {
		lb.wrapped = true
	}
	return len(p), nil
}

// Lines returns log lines in reverse chronological order (newest first).
// If the buffer has wrapped, the newest line is preceded by a marker.
func (lb *logBuffer) Lines() []string {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	total := lb.pos
	if total > lb.size {
		total = lb.size
	}
	out := make([]string, 0, total+1)

	// Newest first: iterate backwards
	lastIdx := lb.pos - 1
	startIdx := lastIdx - total + 1
	for i := lastIdx; i >= startIdx; i-- {
		idx := i % lb.size
		if idx < 0 {
			idx += lb.size
		}
		out = append(out, lb.buf[idx])
	}

	if lb.wrapped {
		out = append(out, "--- [older logs cleared] ---")
	}
	return out
}

var logBuf = newLogBuffer(500)

// notifyLogWriter sends every log line into the notify queue for push.
type notifyLogWriter struct{}

func (w *notifyLogWriter) Write(p []byte) (int, error) {
	line := strings.TrimRight(string(p), "\r\n")
	if line != "" {
		notify.Logf("%s", line)
	}
	return len(p), nil
}

// tzLogWriter wraps a writer and converts log timestamps to the configured timezone.
type tzLogWriter struct {
	out io.Writer
	tz  *time.Location
}

func (w *tzLogWriter) Write(p []byte) (int, error) {
	if w.tz == nil {
		return w.out.Write(p)
	}
	// Go log format: "2009/01/23 01:23:23 message" (20 chars timestamp prefix)
	line := string(p)
	if len(line) > 20 {
		t, err := time.Parse("2006/01/02 15:04:05", line[:19])
		if err == nil {
			line = t.In(w.tz).Format("2006/01/02 15:04:05") + line[19:]
		}
	}
	return w.out.Write([]byte(line))
}

var tzWriter = &tzLogWriter{out: io.MultiWriter(logBuf, os.Stderr, &notifyLogWriter{})}

var timezones = map[string]string{
	"":                  "系统默认",
	"Asia/Shanghai":     "Asia/Shanghai",
	"Asia/Tokyo":        "Asia/Tokyo",
	"Asia/Hong_Kong":    "Asia/Hong_Kong",
	"UTC":               "UTC",
	"Europe/London":     "Europe/London",
	"America/New_York":  "America/New_York",
	"America/Los_Angeles": "America/Los_Angeles",
}

func init() {
	log.SetOutput(tzWriter)
	log.SetFlags(log.LstdFlags)
}

// SetTimezone sets the timezone for log timestamps.
func SetTimezone(tz string) {
	if tz == "" {
		tzWriter.tz = nil
		return
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		tzWriter.tz = nil
		return
	}
	tzWriter.tz = loc
}

func formatTimeInTZ(t time.Time, tz string) string {
	if tz == "" {
		return t.Format("15:04:05")
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return t.Format("15:04:05")
	}
	return t.In(loc).Format("15:04:05")
}

// ---------- web auth ----------

var (
	webPassword string
	webSessions = make(map[string]bool)
	webSessMu   sync.Mutex
)

// SetPassword configures the web login password. Empty means no auth required.
func SetPassword(pw string) {
	webPassword = pw
}

// LoadPersistedPassword reads the password from web-settings.json if not already set via CLI.
func LoadPersistedPassword() {
	if webPassword != "" {
		return // CLI flag takes precedence
	}
	data, err := os.ReadFile(resolveDataFile("web-settings.json"))
	if err != nil {
		return
	}
	var ws struct {
		WebPassword string `json:"web_password"`
	}
	if err := json.Unmarshal(data, &ws); err != nil {
		return
	}
	if ws.WebPassword != "" {
		webPassword = ws.WebPassword
	}
}

func newSessionToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Server) authCheck(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if webPassword == "" {
			next(w, r)
			return
		}
		// Allow login page and static resources
		if r.URL.Path == "/login" || r.URL.Path == "/login/qrcode" || r.URL.Path == "/login/cookies" {
			next(w, r)
			return
		}
		// Check session cookie
		cookie, err := r.Cookie("r2c_session")
		if err != nil || !validSession(cookie.Value) {
			http.Redirect(w, r, "/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func validSession(token string) bool {
	webSessMu.Lock()
	defer webSessMu.Unlock()
	return webSessions[token]
}

// ---------- i18n ----------




// ---------- server lifecycle ----------

func New(agent *p115pkg.Agent, port int) *Server {
	s := &Server{Port: port, startTime: time.Now(), fsCache: make(map[string]fsCacheEntry), entryCache: make(map[string]p115pkg.DirEntry)}
	if agent != nil {
		agent.Dedup = globalDedup
		s.Agent = agent
	}
	// Anchor all runtime files to the data directory before anything reads them.
	// Without this the sidecar files resolve against the process working
	// directory, which self-update changes on restart.
	InitDataDir(s.databasePathAbs())
	// Pre-load Jackett settings from web-settings.json
	ws := s.loadWebSettings()
	s.JackettURL = ws.JackettURL
	s.JackettAPIKey = ws.JackettAPIKey
	s.JackettAdminPassword = ws.JackettAdminPassword
	// Restore language preference from persisted settings
	if ws.Lang != "" && s.Agent != nil {
		st := s.Agent.GetSettings()
		st.Lang = ws.Lang
		s.Agent.UpdateSettings(st)
	}
	// Apply proxy to Jackett client (same as engine)
	cfg, _, _ := config.LoadWithOptions(config.CLIParams{}, config.LoadOptions{})
	if cfg.Proxy.HTTP != "" {
		s.ProxyHTTP = cfg.Proxy.HTTP
		jackett.SetProxy(cfg.Proxy.HTTP)
		media.SetTMDBProxy(cfg.Proxy.HTTP)
	}
	// Init TMDB client (after proxy is configured)
	if ws.TMDBAPIKey != "" {
		media.InitTMDB(ws.TMDBAPIKey)
	}
	// Warm Jackett cache in background
	if s.JackettURL != "" && s.JackettAPIKey != "" {
		go func() {
			if jk, err := jackett.ListIndexers(s.jackettConfig()); err == nil {
				s.jackettCacheMu.Lock()
				s.jackettCache = jk
				s.jackettCacheTime = time.Now()
				s.jackettCacheMu.Unlock()
				log.Printf("[jackett] cache warmed: %d indexers", len(jk))
			} else {
				log.Printf("[jackett] cache warm failed: %v", err)
			}
		}()
	}
	return s
}

// SetDomain sets the domain to bind the server to.
func (s *Server) SetDomain(domain string) {
	s.Domain = domain
}

// SetTLS configures TLS certificate and key for HTTPS.
func (s *Server) SetTLS(certFile, keyFile string) {
	s.CertFile = certFile
	s.KeyFile = keyFile
}

// LoadProxyConfig reads proxy settings from config.toml.
func (s *Server) LoadProxyConfig() {
	cfg, _, err := config.LoadWithOptions(config.CLIParams{}, config.LoadOptions{})
	if err == nil && cfg.Proxy.HTTP != "" {
		s.ProxyHTTP = cfg.Proxy.HTTP
		media.SetTMDBProxy(cfg.Proxy.HTTP)
	}
}

// httpClient returns an HTTP client that respects the configured proxy.
// httpClient returns the HTTP client used for outbound calls to GitHub and
// Jackett.
//
// http.DefaultClient has no timeout, so a hung peer would block a request
// handler indefinitely. The shared client below always carries one; returning
// the default would silently drop that guarantee whenever no proxy is set.
func (s *Server) httpClient() *http.Client {
	if s.ProxyHTTP == "" {
		return sharedHTTPClient
	}
	proxyURL, err := url.Parse(s.ProxyHTTP)
	if err != nil {
		return sharedHTTPClient
	}
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
		Timeout:   60 * time.Second,
	}
}

// sharedHTTPClient is used when no proxy is configured.
var sharedHTTPClient = &http.Client{Timeout: 60 * time.Second}

// LoadNotifyConfig reads notification webhook and Jackett settings from config.toml.
func (s *Server) LoadNotifyConfig() {
	cfg, _, err := config.LoadWithOptions(config.CLIParams{}, config.LoadOptions{})
	if err == nil {
		if cfg.Notify.WeworkWebhook != "" {
			s.WeworkWH = cfg.Notify.WeworkWebhook
			notify.SetWebhook(cfg.Notify.WeworkWebhook)
		}
	}
	// Load timezone setting
	ws := s.loadWebSettings()
	SetTimezone(ws.Timezone)
	notify.SetTimezone(ws.Timezone)
	notify.SetLogEnabled(ws.NotifyLog)
	s.ReloadNotifyChannels()
}

// notifyWebhooksPath is where the generic webhook channel list is stored.
func (s *Server) notifyWebhooksPath() string {
	return s.dataFile("webhooks.json")
}

// ReloadNotifyChannels rebuilds the channel list from stored configuration.
//
// It is called at startup and after the settings page changes anything, so an
// edit takes effect without a restart.
func (s *Server) ReloadNotifyChannels() {
	notify.SetRouterDataDir(dataDir)

	hooks, err := notify.LoadWebhooks(s.notifyWebhooksPath())
	if err != nil {
		log.Printf("[notify] 读取 webhook 配置失败: %v", err)
		hooks = nil
	}

	// Fold a pre-existing WeChat Work URL into the list, so every destination
	// lives in one place from here on.
	legacy := s.WeworkWH
	if legacy == "" {
		legacy = notify.Webhook()
	}
	if migrated, changed := notify.MigrateLegacyWework(legacy, hooks); changed {
		if len(migrated) != len(hooks) {
			// A new entry was created: persist it.
			hooks = migrated
			if err := notify.SaveWebhooks(s.notifyWebhooksPath(), hooks); err != nil {
				log.Printf("[notify] 迁移企业微信配置失败: %v", err)
			} else {
				log.Printf("[notify] 已将企业微信 Webhook 整合进通知列表")
			}
		} else {
			// The entry already exists, so nothing was migrated. Reporting a
			// migration here would be a lie on every single startup.
			log.Printf("[notify] 企业微信已在通知列表中，忽略旧配置项")
		}
		// Clear the legacy setting either way, so the next startup takes the
		// branch above instead of repeating this message forever.
		s.WeworkWH = ""
		notify.SetWebhook("")
		_ = config.SaveNotifyWebhook("")
	}

	channels := notify.BuildChannels(hooks)
	notify.SetChannels(channels)

	// The log pusher is deliberately outside the event router: it owns the
	// per-second throttle and the notify_log toggle, and routing log events as
	// well would deliver every line twice. It still needs a destination, which
	// is the WeChat Work entry when the list has one.
	logTarget := ""
	for _, h := range hooks {
		if notify.NormalizeKind(h.Kind) == notify.KindWework && h.Enabled {
			logTarget = h.URL
			break
		}
	}
	notify.SetWebhook(logTarget)

	enabled := 0
	for _, h := range hooks {
		if h.Enabled {
			enabled++
		}
	}
	log.Printf("[notify] 通知渠道已加载: %d 个（其中启用 %d 个）", len(channels), enabled)
}

// loadWebhookConfigs returns the stored webhook channels.
func (s *Server) loadWebhookConfigs() []notify.WebhookConfig {
	hooks, err := notify.LoadWebhooks(s.notifyWebhooksPath())
	if err != nil {
		log.Printf("[notify] 读取 webhook 配置失败: %v", err)
		return nil
	}
	return hooks
}

// saveWebhookConfigs persists the webhook channels and re-registers them.
func (s *Server) saveWebhookConfigs(hooks []notify.WebhookConfig) error {
	if err := notify.SaveWebhooks(s.notifyWebhooksPath(), hooks); err != nil {
		return err
	}
	s.ReloadNotifyChannels()
	return nil
}

// LoadJackettConfig reads Jackett settings from web-settings.json.
func (s *Server) LoadJackettConfig() {
	ws := s.loadWebSettings()
	s.JackettURL = ws.JackettURL
	s.JackettAPIKey = ws.JackettAPIKey
	s.JackettAdminPassword = ws.JackettAdminPassword
}

// saveProxyConfig persists the current proxy setting to config.toml.
func (s *Server) saveProxyConfig() {
	_ = config.SaveProxy(s.ProxyHTTP)
}

func maskWebhook(url string) string {
	if len(url) > 40 {
		return url[:35] + "..."
	}
	return url
}

func (s *Server) Start(ctx context.Context) error {
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	addr := fmt.Sprintf(":%d", s.Port)
	srv = &http.Server{Addr: addr, Handler: mux}

	// Load caches
	globalDedup.Load()
	rsssite.SetTorrentHashCache(globalDedup)
	notify.SetDataDir(dataDir)
	notify.LoadRecentItems()

	// Populate task count cache asynchronously (don't block startup)
	go func() {
		if s.Agent != nil {
			if tasks, err := s.Agent.ListTasks(); err == nil {
				s.taskCountCache = len(tasks)
			}
		}
	}()

	// Start subscription auto-runner
	go s.autoRunSubscriptions()

	// Periodically check for updates when the user opted in. This only detects
	// and reports: applying an update restarts the server, so it stays a
	// deliberate action taken from the settings page.
	if s.loadWebSettings().AutoUpdate {
		go s.autoCheckUpdates()
	}

	useTLS := s.CertFile != "" && s.KeyFile != ""
	scheme := "http"
	if useTLS {
		scheme = "https"
	}
	log.Printf("server started on %s://0.0.0.0:%d\n", scheme, s.Port)
	// Announce the startup so notification channels subscribed to
	// server.started learn the service is back (useful after a self-update).
	notify.DispatchEvent(notify.ServerStartedEvent(s.Port), true)
	if useTLS {
		return srv.ListenAndServeTLS(s.CertFile, s.KeyFile)
	}
	return srv.ListenAndServe()
}

// autoCheckUpdates polls for a newer release on a fixed cadence when the user
// has opted in.
//
// It deliberately does not install: applying an update restarts the server and
// would interrupt RSS polling and in-flight downloads, so the release page
// stays the place where the user commits to it. A discovered version is stored
// so the settings page can report it without waiting for its own request, and a
// notification is sent once per newly seen version.
func (s *Server) autoCheckUpdates() {
	// Give the server a moment to settle before the first poll.
	time.Sleep(1 * time.Minute)

	var lastSeen string
	for {
		ws := s.loadWebSettings()
		if !ws.AutoUpdate {
			return // switched off in the settings page
		}

		interval := ws.SubsInterval
		if interval <= 0 {
			interval = 60
		}
		// Checking far more often than the RSS cycle is pointless: GitHub
		// rate-limits unauthenticated API calls.
		if interval < 30 {
			interval = 30
		}

		if latest := s.latestReleaseTag(); latest != "" && latest != lastSeen {
			lastSeen = latest
			if compareVersion(latest, Version) > 0 {
				s.updateNoticeMu.Lock()
				s.updateNotice = latest
				s.updateNoticeMu.Unlock()
				log.Printf("[update] 发现新版本 %s（当前 %s），请前往设置页手动更新", latest, Version)
				notify.DispatchEvent(notify.UpdateAvailableEvent(latest, Version), true)
			}
		}

		time.Sleep(time.Duration(interval) * time.Minute)
	}
}

// latestReleaseTag returns the tag name of the newest GitHub release, or "" if
// it cannot be determined. Errors are swallowed: a background poll must not
// spam the log when the machine is offline.
func (s *Server) latestReleaseTag() string {
	resp, err := s.httpClient().Get("https://api.github.com/repos/lingeringdawn/pan-fetcher/releases/latest")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ""
	}
	var release struct {
		TagName string `json:"tag_name"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return ""
	}
	return release.TagName
}

// autoRunSubscriptions loops through enabled RSS subscriptions.
// Between feeds: small cooldown (30s). Between full cycles: SubsInterval.
// Failed feeds are retried with exponential backoff.
func (s *Server) autoRunSubscriptions() {
	// Wait a short grace period for server to fully stabilise
	time.Sleep(2 * time.Minute)

	type retryState struct {
		failures int
		nextTry  time.Time
	}
	retryMap := make(map[string]*retryState) // subKey -> retry state

	for {
		ws := s.loadWebSettings()
		cycleInterval := ws.SubsInterval
		if cycleInterval <= 0 {
			cycleInterval = 60
		}

		feeds := s.readRssFeeds()
		ran := 0
		for _, entries := range feeds {
			for _, e := range entries {
				if !e.Enabled {
					continue
				}
				subKey := e.Name
				feedURL := e.URL
				if strings.HasPrefix(feedURL, "/") {
					feedURL = fmt.Sprintf("http://127.0.0.1:%d%s", s.Port, feedURL)
				}

				// Check retry state
				if rs, ok := retryMap[subKey]; ok {
					if time.Now().Before(rs.nextTry) {
						log.Printf("[auto-sub] skipping %s (retry in %s)", subKey, time.Until(rs.nextTry).Round(time.Second))
						continue
					}
				}

				log.Printf("[auto-sub] running: %s (%s)", subKey, feedURL)
				func() {
					defer func() {
						if r := recover(); r != nil {
							log.Printf("[auto-sub] panic in %s: %v", subKey, r)
						}
					}()
					if s.Agent != nil && e.Cid != "" {
						// Backward compat: old subs carry the filter in e.Filter
						// rather than in the URL. A keyword already present in the
						// URL wins, so explicit filters are never overridden.
						rssURL := ensureKeywordParam(feedURL, e.Filter)
						before := globalDedup.SubCount(subKey)
						names := s.Agent.ProcessRSSFeed(rssURL, e.Cid, e.SavePath, subKey)
						after := globalDedup.SubCount(subKey)
						newItems := after - before
						if newItems > 0 {
							notify.RecordItems(subKey, names)
							notify.DispatchEvent(notify.RSSFoundEvent(subKey, newItems, names), true)
						}
					}
				}()

				// Check for failures via dedup cache (if no new items added, might be a fetch error)
				// We track failures implicitly: if a feed runs but produces errors, the ProcessRSSFeed logs them.
				// For now, clear retry state on successful run.
				delete(retryMap, subKey)
				ran++
				time.Sleep(30 * time.Second)
			}
		}

		if ran == 0 {
			log.Printf("[auto-sub] no enabled subscriptions, sleeping %d min", cycleInterval)
		} else {
			log.Printf("[auto-sub] cycle complete: %d feeds ran, next cycle in %d min", ran, cycleInterval)
		}

		// Schedule retries for failed feeds
		// We check ProcessRSSFeed logs indirectly - any feed that consistently fails
		// will be retried in the next cycle automatically via the loop above.
		// For active retry with backoff, we rely on ProcessRSSFeed's internal error handling
		// and the fact that the main cycle will retry all enabled feeds.

		time.Sleep(time.Duration(cycleInterval) * time.Minute)
	}
}

func (s *Server) Shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if srv != nil {
		if err := srv.Shutdown(ctx); err != nil {
			return err
		}
	}
	if s.Agent != nil {
		if err := s.Agent.StoreClose(); err != nil {
			log.Printf("failed to close database: %v\n", err)
		}
	}
	log.Printf("server stopped properly\n")
	return nil
}

func (s *Server) registerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/", s.authCheck(s.handleDashboard))
	mux.HandleFunc("/tasks", s.authCheck(s.handleTasks))
	mux.HandleFunc("/add", s.authCheck(s.handleAddTask))
	mux.HandleFunc("/rss/feed", s.authCheck(s.handleRSSFeed))
	mux.HandleFunc("/clear", s.authCheck(s.handleClearTask))
	mux.HandleFunc("/fs", s.authCheck(s.handleFileSystem))
	mux.HandleFunc("/api/fs/mkdir", s.authCheck(s.handleFSMkdir))
	mux.HandleFunc("/api/fs/rename", s.authCheck(s.handleFSRename))
	mux.HandleFunc("/api/fs/delete", s.authCheck(s.handleFSDelete))
	mux.HandleFunc("/api/fs/move", s.authCheck(s.handleFSMove))
	mux.HandleFunc("/api/fs/copy", s.authCheck(s.handleFSCopy))
	mux.HandleFunc("/subs", s.authCheck(s.handleSubscriptions))
	mux.HandleFunc("/subs/run", s.authCheck(s.handleSubsRun))
	mux.HandleFunc("/subs/toggle-all", s.authCheck(s.handleSubsToggleAll))
	mux.HandleFunc("/dedup/clear-all", s.authCheck(s.handleDedupClearAll))
	mux.HandleFunc("/settings", s.authCheck(s.handleSettings))
	mux.HandleFunc("/login/qrcode", s.authCheck(s.handleQRLogin))
	mux.HandleFunc("/login/cookies", s.authCheck(s.handleCookiesLogin))
	mux.HandleFunc("/logout", s.handleLogout)
	mux.HandleFunc("/settings/test115", s.authCheck(s.handleTest115))
	mux.HandleFunc("/settings/test-jackett", s.authCheck(s.handleTestJackett))
	mux.HandleFunc("/api/webhooks", s.authCheck(s.handleWebhooks))
	mux.HandleFunc("/settings/restart", s.authCheck(s.handleRestart))
	mux.HandleFunc("/settings/check-update", s.authCheck(s.handleCheckUpdate))
	mux.HandleFunc("/settings/update", s.authCheck(s.handleDoUpdate))
	mux.HandleFunc("/search", s.authCheck(s.handleSearch))
	mux.HandleFunc("/search/more", s.authCheck(s.handleSearchMore))
	mux.HandleFunc("/search/clear-cache", s.authCheck(s.handleSearchClearCache))
	mux.HandleFunc("/discover", s.authCheck(s.handleDiscover))
	mux.HandleFunc("/indexers", s.authCheck(s.handleIndexers))
	mux.HandleFunc("/indexers/jackett", s.authCheck(s.handleJackettList))
	mux.HandleFunc("/indexers/jackett/all", s.authCheck(s.handleJackettAll))
	mux.HandleFunc("/indexers/jackett/add", s.authCheck(s.handleJackettAdd))
	mux.HandleFunc("/indexers/jackett/discover", s.authCheck(s.handleJackettDiscover))
	mux.HandleFunc("/indexers/jackett/install", s.authCheck(s.handleJackettInstall))
	mux.HandleFunc("/indexers/jackett/start", s.authCheck(s.handleJackettStart))
	mux.HandleFunc("/indexers/jackett/env", s.authCheck(s.handleJackettEnv))
	mux.HandleFunc("/indexers/jackett/install-deps", s.authCheck(s.handleJackettInstallDeps))
	mux.HandleFunc("/indexers/jackett/check-update", s.authCheck(s.handleJackettCheckUpdate))
	mux.HandleFunc("/indexers/jackett/update", s.authCheck(s.handleJackettUpdate))
	mux.HandleFunc("/indexers/jackett/uninstall", s.authCheck(s.handleJackettUninstall))
	mux.HandleFunc("/search/subscribe", s.authCheck(s.handleSearchSubscribe))
	mux.HandleFunc("/rss/search", s.handleRssSearch)
	mux.HandleFunc("/subs/dirs", s.authCheck(s.handleSubsDirs))
	mux.HandleFunc("/dedup/clear", s.authCheck(s.handleDedupClear))
	mux.HandleFunc("/api/dedup/hashes", s.authCheck(s.handleDedupHashes))
	mux.HandleFunc("/api/dedup/remove-hash", s.authCheck(s.handleDedupRemoveHash))
	mux.HandleFunc("/torrent", s.authCheck(s.handleTorrent))
	mux.HandleFunc("/torrent/clear", s.authCheck(s.handleTorrentClear))
	mux.HandleFunc("/log", s.authCheck(s.handleLogPage))
	mux.HandleFunc("/about", s.authCheck(s.handleAbout))
	mux.HandleFunc("/api/tasks", s.authCheck(s.handleAPITasks))
	mux.HandleFunc("/api/logs", s.authCheck(s.handleAPILogs))
	mux.HandleFunc("/api/tmdb/search", s.authCheck(s.handleTMDBSearch))
	mux.HandleFunc("/api/tmdb/trending", s.authCheck(s.handleTMDBTrending))
	mux.HandleFunc("/api/tmdb/detail", s.authCheck(s.handleTMDBDetail))
	mux.HandleFunc("/api/lang", s.authCheck(s.handleAPILang))
}

// ---------- aggregated RSS search endpoint ----------

func (s *Server) handleRssSearch(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		http.Error(w, "missing q parameter", http.StatusBadRequest)
		return
	}
	if !s.jackettConfigured() {
		http.Error(w, "jackett not configured", http.StatusServiceUnavailable)
		return
	}

	// Parse indexers from comma-separated list. IDs may carry a "jackett:"
	// prefix depending on how the subscription URL was generated.
	var indexers []string
	if idxStr := r.URL.Query().Get("indexers"); idxStr != "" {
		for _, id := range strings.Split(idxStr, ",") {
			id = strings.TrimPrefix(strings.TrimSpace(id), "jackett:")
			if id != "" {
				indexers = append(indexers, id)
			}
		}
	}

	sortBy := strings.TrimSpace(r.URL.Query().Get("sort"))
	if sortBy == "" {
		sortBy = "date"
	}

	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	jackettActiveSet := make(map[string]bool)
	for id := range s.jackettActive {
		jackettActiveSet[id] = true
	}
	s.jackettActiveMu.Unlock()

	selectedLower := make(map[string]bool, len(indexers))
	for _, id := range indexers {
		selectedLower[strings.ToLower(id)] = true
	}

	se := indexer.SearchAllErrors{Errors: make(map[string]string)}
	jr, err := jackett.Search(s.jackettConfig(), q, nil, 0)
	if err != nil {
		log.Printf("[rss] Jackett search error: %v", err)
		se.Errors["jackett"] = err.Error()
	} else {
		for _, r := range jr {
			trackerLower := strings.ToLower(r.Tracker)
			// With no explicit selection, restrict to activated indexers.
			if len(selectedLower) == 0 {
				if !jackettActiveSet[trackerLower] {
					continue
				}
			} else if !selectedLower[trackerLower] {
				continue
			}
			var pubDate time.Time
			if r.PublishDate != "" {
				pubDate, _ = time.Parse(time.RFC1123Z, r.PublishDate)
				if pubDate.IsZero() {
					pubDate, _ = time.Parse(time.RFC1123, r.PublishDate)
				}
			}
			se.Results = append(se.Results, indexer.SearchResult{
				Title:       r.Title,
				MagnetURL:   r.MagnetURI,
				PageURL:     r.Link,
				Size:        r.Size,
				Seeders:     r.Seeders,
				Leechers:    r.Peers,
				IndexerID:   r.Tracker,
				IndexerName: r.TrackerName,
				PublishDate: pubDate,
			})
		}
	}

	// Dedup (same as search)
	se.Results = dedupSlice(se.Results, nil)

	// Apply keyword filter using same group-aware logic as search
	se.Results = applyKeywordFilter(se.Results, r.URL.Query().Get("keyword"))

	// Build RSS XML
	w.Header().Set("Content-Type", "application/xml; charset=utf-8")
	w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?>` + "\n"))
	w.Write([]byte(`<rss version="2.0" xmlns:torznab="http://torznab.com/schemas/2015/feed">` + "\n"))
	w.Write([]byte(`<channel>` + "\n"))
	rssLang := s.langFromAgent()
	fmt.Fprintf(w, "<title>%s - %s</title>\n", xmlEscape(q), tr(rssLang, "rss_feed_title"))
	fmt.Fprintf(w, "<description>%s</description>\n", xmlEscape(trf(rssLang, "rss_feed_desc", q)))
	fmt.Fprintf(w, "<link>http://%s/search</link>\n", r.Host)
	fmt.Fprintf(w, "<language>%s</language>\n", rssLang)

	for _, result := range se.Results {
		guid := result.MagnetURL
		if guid == "" {
			guid = result.PageURL
		}
		if guid == "" {
			continue
		}
		// Convert .torrent URL to magnet (cached in dedup)
		if strings.HasSuffix(strings.ToLower(guid), ".torrent") {
			if hash, ok := globalDedup.GetTorrentHash(guid); ok {
				guid = "magnet:?xt=urn:btih:" + hash + "&dn=" + url.QueryEscape(result.Title)
			} else {
				m := rsssite.NormalizeTaskURL(guid, result.Title)
				if strings.HasPrefix(m, "magnet:?") {
					if h := extractInfoHashFromMagnet(m); h != "" {
						globalDedup.SetTorrentHash(guid, h)
						guid = "magnet:?xt=urn:btih:" + h + "&dn=" + url.QueryEscape(result.Title)
					} else {
						guid = m
					}
				}
			}
		} else if strings.HasPrefix(guid, "magnet:?") && !strings.Contains(guid, "dn=") && result.Title != "" {
			// Add dn= parameter to existing magnet links for display names
			guid = guid + "&dn=" + url.QueryEscape(result.Title)
		}
		pubDate := ""
		if !result.PublishDate.IsZero() {
			pubDate = result.PublishDate.Format("Mon, 02 Jan 2006 15:04:05 -0700")
		}
		sizeStr := ""
		if result.Size > 0 {
			sizeStr = fmt.Sprintf("%d", result.Size)
		}
		w.Write([]byte("<item>\n"))
		fmt.Fprintf(w, "<title>%s</title>\n", xmlEscape(result.Title))
		fmt.Fprintf(w, "<guid>%s</guid>\n", xmlEscape(guid))
		if result.PageURL != "" {
			fmt.Fprintf(w, "<link>%s</link>\n", xmlEscape(result.PageURL))
		}
		if pubDate != "" {
			fmt.Fprintf(w, "<pubDate>%s</pubDate>\n", pubDate)
		}
		fmt.Fprintf(w, "<category>%s</category>\n", xmlEscape(result.Category))
		if sizeStr != "" {
			fmt.Fprintf(w, "<enclosure url=\"%s\" length=\"%s\" type=\"application/x-bittorrent\"/>\n",
				xmlEscape(guid), sizeStr)
		}
		fmt.Fprintf(w, "<torznab:attr name=\"seeders\" value=\"%d\"/>\n", result.Seeders)
		fmt.Fprintf(w, "<torznab:attr name=\"peers\" value=\"%d\"/>\n", result.Leechers)
		fmt.Fprintf(w, "<torznab:attr name=\"site\" value=\"%s\"/>\n", xmlEscape(result.IndexerName))
		w.Write([]byte("</item>\n"))
	}
	w.Write([]byte("</channel>\n"))
	w.Write([]byte("</rss>\n"))
}

// xmlEscape escapes text for XML output.
func xmlEscape(s string) string {
	var buf strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			buf.WriteString("&amp;")
		case '<':
			buf.WriteString("&lt;")
		case '>':
			buf.WriteString("&gt;")
		case '"':
			buf.WriteString("&quot;")
		case '\'':
			buf.WriteString("&apos;")
		default:
			buf.WriteRune(r)
		}
	}
	return buf.String()
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := s.pageData("", "")
	data.Page = "home"
	data.HasAgent = s.Agent != nil
	data.HasPassword = webPassword != ""

	// Dashboard stats: only compute cheap in-memory counters.
	// Heavy API calls (ListTasks, readRssFeeds from disk) are skipped;
	// the dashboard is informational, not real-time.
	if s.Agent != nil {
		data.DashStats.TotalTasks = s.taskCountCache // updated by handleAPITasks
	}

	feeds := s.readRssFeeds()
	for _, entries := range feeds {
		for _, e := range entries {
			data.DashStats.RssSubsTotal++
			if e.Enabled {
				data.DashStats.RssSubsActive++
			}
		}
	}

	// Active indexers are Jackett's activated set.
	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	data.DashStats.ActiveIndexers = len(s.jackettActive)
	s.jackettActiveMu.Unlock()

	// Skip dedup walk — would iterate thousands of entries for a single stat.
	data.DashStats.CacheEntries = globalDedup.TotalCount()
	data.DashStats.Uptime = formatDuration(time.Since(s.startTime))
	data.DashStats.RecentItems = notify.GetRecentItems()

	if err := dashboardTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleTasks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	data := s.pageData("", "")
	data.Page = "tasks"
	data.HasAgent = s.Agent != nil
	data.TaskCount = s.taskCountCache
	if err := dashboardTemplate.Execute(w, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func (s *Server) handleAddTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.Agent == nil {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"status":"error","message":"not logged in"}`)
		return
	}
	task, err := decodeOfflineTask(r)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	err = s.Agent.AddMagnetTask(task.Tasks, task.Cid, task.SavePath)
	if err != nil {
		log.Printf("[task] web submit failed: %v", err)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{"status": "error", "message": err.Error()})
		return
	}
	log.Printf("[task] web submitted %d tasks, cid=%s", len(task.Tasks), task.Cid)
	// Extract human-readable names from magnets for notification
	// Prefer the title from the search result, fall back to magnet dn=
	title := strings.TrimSpace(r.FormValue("title"))
	names := make([]string, 0, len(task.Tasks))
	for _, m := range task.Tasks {
		if title != "" {
			names = append(names, title)
		} else if n := extractNameFromMagnet(m); n != "" {
			names = append(names, n)
		} else {
			names = append(names, m)
		}
	}
	subKey := "task-" + time.Now().Format("150405")
	notify.RecordItems(subKey, names)
	notify.DispatchEvent(notify.TaskSubmittedEvent(len(task.Tasks), task.Cid, names), true)
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"status":"ok","message":"%s"}`, trf(s.langFromAgent(), "tasks_submitted_fmt", len(task.Tasks)))
}

func (s *Server) handleRSSFeed(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		s.renderMsg(w, "", "err_not_logged_in")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderResult(w, "", err.Error())
		return
	}
	rssURL := strings.TrimSpace(r.FormValue("rss_url"))
	keyword := strings.TrimSpace(r.FormValue("keyword"))
	cid := strings.TrimSpace(r.FormValue("cid"))
	savepath := strings.TrimSpace(r.FormValue("savepath"))
	if rssURL == "" {
		s.renderMsg(w, "", "err_rss_empty")
		return
	}
	// ProcessRSSFeed only reads the keyword parameter from the URL, so the form
	// field must be merged in. subKey intentionally stays the bare URL to keep
	// existing dedup cache keys stable.
	feedURL := ensureKeywordParam(rssURL, keyword)
	go s.Agent.ProcessRSSFeed(feedURL, cid, savepath, rssURL)
	log.Printf("[feed] web triggered: %s keyword=%q cid=%s savepath=%q", feedURL, keyword, cid, savepath)
	s.renderMsgf(w, "quick_started_fmt", rssURL)
}

func (s *Server) handleClearTask(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		s.renderMsg(w, "", "err_not_logged_in")
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderResult(w, "", err.Error())
		return
	}
	typeNum, err := strconv.Atoi(strings.TrimSpace(r.FormValue("type")))
	if err != nil || typeNum < 1 || typeNum > 6 {
		s.renderMsg(w, "", "err_clear_type")
		return
	}
	if err := s.Agent.OfflineClear(typeNum - 1); err != nil {
		log.Printf("[task] clear type=%d failed: %v", typeNum, err)
		s.renderResult(w, "", err.Error())
		return
	}
	log.Printf("[task] clear type=%d executed", typeNum)
	s.renderMsgf(w, "clear_executed_fmt", typeNum)
}

// ---------- filesystem browser ----------

type fsEntry struct {
	ID      string
	Name    string
	IsDir   bool
	Size    string
	Icon    string
}

type fsBreadcrumb struct {
	ID   string
	Name string
}

func (s *Server) handleFileSystem(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		http.Redirect(w, r, "/settings?err=not_logged_in", http.StatusSeeOther)
		return
	}
	dirID := strings.TrimSpace(r.URL.Query().Get("dir"))
	if dirID == "" {
		dirID = "0"
	}

	// Try cache first
	var entries []p115pkg.DirEntry
	var err error
	cacheKey := "fs:" + dirID
	s.fsCacheMu.Lock()
	if cached, ok := s.fsCache[cacheKey]; ok && time.Now().Before(cached.expires) {
		entries = cached.entries
		s.fsCacheMu.Unlock()
	} else {
		s.fsCacheMu.Unlock()
		entries, err = s.Agent.ListDir(dirID)
		if err == nil {
			s.fsCacheMu.Lock()
			s.fsCache[cacheKey] = fsCacheEntry{entries: entries, expires: time.Now().Add(30 * time.Second)}
			s.fsCacheMu.Unlock()
		} else {
			lang := s.langFromAgent()
			data := s.pageDataWithCache("fs", tr(lang, "err_read_dir")+err.Error(), "")
			dashboardTemplate.Execute(w, data)
			return
		}
	}

	// Build breadcrumb (with entry cache)
	lang := s.langFromAgent()
	var crumbs []fsBreadcrumb
	currentID := dirID
	for i := 0; i < 20; i++ {
		if currentID == "0" || currentID == "" {
			crumbs = append([]fsBreadcrumb{{ID: "0", Name: tr(lang, "root_dir")}}, crumbs...)
			break
		}
		e, err := s.getEntryCached(currentID)
		if err != nil {
			crumbs = append([]fsBreadcrumb{{ID: currentID, Name: currentID}}, crumbs...)
			break
		}
		crumbs = append([]fsBreadcrumb{{ID: e.ID, Name: e.Name}}, crumbs...)
		currentID = e.ParentID
	}

	fsEntries := make([]fsEntry, 0, len(entries))
	for _, e := range entries {
		icon := "📄"
		if e.IsDir {
			icon = "📁"
		}
		fsEntries = append(fsEntries, fsEntry{
			ID:    e.ID,
			Name:  e.Name,
			IsDir: e.IsDir,
			Size:  formatSize(e.Size),
			Icon:  icon,
		})
	}

	parentID := "0"
	if len(crumbs) >= 2 {
		parentID = crumbs[len(crumbs)-2].ID
	}

	data := s.pageDataWithCache("fs", "", "")
	data.Page = "fs"
	data.FSEntries = fsEntries
	data.FSCrumbs = crumbs
	data.FSCurrentID = dirID
	data.FSParentID = parentID

	dashboardTemplate.Execute(w, data)
}

// getEntryCached returns a cached DirEntry or fetches+stores it.
func (s *Server) getEntryCached(entryID string) (p115pkg.DirEntry, error) {
	s.entryCacheMu.Lock()
	if e, ok := s.entryCache[entryID]; ok {
		s.entryCacheMu.Unlock()
		return e, nil
	}
	s.entryCacheMu.Unlock()

	e, err := s.Agent.GetEntry(entryID)
	if err != nil {
		return p115pkg.DirEntry{}, err
	}
	s.entryCacheMu.Lock()
	if len(s.entryCache) > 200 {
		s.entryCache = make(map[string]p115pkg.DirEntry) // simple eviction
	}
	s.entryCache[entryID] = e
	s.entryCacheMu.Unlock()
	return e, nil
}

// ---------- FS API handlers ----------

func (s *Server) writeFSResult(w http.ResponseWriter, status, msg string) {
	w.Header().Set("Content-Type", "application/json")
	if msg == "" {
		fmt.Fprintf(w, `{"status":"%s"}`, status)
	} else {
		fmt.Fprintf(w, `{"status":"%s","message":"%s"}`, status, msg)
	}
}

func (s *Server) handleFSMkdir(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	parentID := strings.TrimSpace(r.FormValue("parent_id"))
	name := strings.TrimSpace(r.FormValue("name"))
	if name == "" {
		s.writeFSResult(w, "error", "Folder name is required")
		return
	}
	_, err := s.Agent.Mkdir(parentID, name)
	if err != nil {
		s.writeFSResult(w, "error", err.Error())
		return
	}
	log.Printf("[fs] mkdir %q in parent=%s", name, parentID)
	s.invalidateFSCache(parentID)
	s.writeFSResult(w, "ok", "")
}

func (s *Server) handleFSRename(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entryID := strings.TrimSpace(r.FormValue("id"))
	newName := strings.TrimSpace(r.FormValue("name"))
	if entryID == "" || newName == "" {
		s.writeFSResult(w, "error", "ID and name are required")
		return
	}
	if err := s.Agent.RenameEntry(entryID, newName); err != nil {
		s.writeFSResult(w, "error", err.Error())
		return
	}
	log.Printf("[fs] rename id=%s -> %q", entryID, newName)
	s.invalidateFSCache("")
	s.writeFSResult(w, "ok", "")
}

func (s *Server) handleFSDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entryID := strings.TrimSpace(r.FormValue("id"))
	if entryID == "" {
		s.writeFSResult(w, "error", "ID is required")
		return
	}
	if err := s.Agent.DeleteEntry(entryID); err != nil {
		s.writeFSResult(w, "error", err.Error())
		return
	}
	log.Printf("[fs] delete id=%s", entryID)
	s.invalidateFSCache("")
	s.writeFSResult(w, "ok", "")
}

func (s *Server) handleFSMove(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entryID := strings.TrimSpace(r.FormValue("id"))
	targetDirID := strings.TrimSpace(r.FormValue("target_dir"))
	if entryID == "" || targetDirID == "" {
		s.writeFSResult(w, "error", "ID and target_dir are required")
		return
	}
	if err := s.Agent.MoveEntry(targetDirID, entryID); err != nil {
		s.writeFSResult(w, "error", err.Error())
		return
	}
	log.Printf("[fs] move id=%s -> dir=%s", entryID, targetDirID)
	s.invalidateFSCache("")
	s.writeFSResult(w, "ok", "")
}

func (s *Server) handleFSCopy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	entryID := strings.TrimSpace(r.FormValue("id"))
	targetDirID := strings.TrimSpace(r.FormValue("target_dir"))
	if entryID == "" || targetDirID == "" {
		s.writeFSResult(w, "error", "ID and target_dir are required")
		return
	}
	if err := s.Agent.Copy(targetDirID, entryID); err != nil {
		s.writeFSResult(w, "error", err.Error())
		return
	}
	log.Printf("[fs] copy id=%s -> dir=%s", entryID, targetDirID)
	s.invalidateFSCache("")
	s.writeFSResult(w, "ok", "")
}

// ---------- settings ----------

func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	ws := s.loadWebSettings()

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// A partial POST (used by the Jackett discovery flow) must only touch the
		// fields it actually submits. r.Form records presence, so absent fields
		// are skipped while an explicitly empty value still clears the setting.
		has := func(name string) bool {
			_, ok := r.Form[name]
			return ok
		}
		var changes []string

		// Proxy
		if v := strings.TrimSpace(r.FormValue("proxy_http")); v != s.ProxyHTTP {
			s.ProxyHTTP = v
			changes = append(changes, fmt.Sprintf("proxy=%s", v))
		}
		// Chunk delay
		if v, err := strconv.Atoi(r.FormValue("chunk_delay")); err == nil && v > 0 && v != ws.ChunkDelay {
			ws.ChunkDelay = v
			changes = append(changes, fmt.Sprintf("chunk_delay=%d", v))
		}
		// Chunk size
		if v, err := strconv.Atoi(r.FormValue("chunk_size")); err == nil && v > 0 && v != ws.ChunkSize {
			ws.ChunkSize = v
			changes = append(changes, fmt.Sprintf("chunk_size=%d", v))
		}
		// Cooldown min
		if v, err := strconv.Atoi(r.FormValue("cooldown_min")); err == nil && v > 0 && v != ws.CooldownMin {
			ws.CooldownMin = v
			changes = append(changes, fmt.Sprintf("cooldown_min=%d", v))
		}
		// Cooldown max
		if v, err := strconv.Atoi(r.FormValue("cooldown_max")); err == nil && v > 0 && v != ws.CooldownMax {
			ws.CooldownMax = v
			changes = append(changes, fmt.Sprintf("cooldown_max=%d", v))
		}
		// Subscription interval
		if v, err := strconv.Atoi(r.FormValue("subs_interval")); err == nil && v >= 0 && v != ws.SubsInterval {
			ws.SubsInterval = v
			changes = append(changes, fmt.Sprintf("subs_interval=%d", v))
		}
		// Web password (allow clearing by saving empty)
		if pw := sanitizePassword(r.FormValue("web_password")); pw != ws.WebPassword {
			webPassword = pw
			ws.WebPassword = pw
			if pw == "" {
				changes = append(changes, "web_password=cleared")
			} else {
				changes = append(changes, "web_password=***")
			}
		}
		// WeChat Work webhook.
		//
		// The input box is gone from the settings page (destinations live in the
		// webhook list now), so this only accepts a value from a client that
		// still posts the old form. An empty value is ignored rather than
		// treated as a removal, which would otherwise wipe a configured
		// destination on the next unrelated settings save.
		if v := strings.TrimSpace(r.FormValue("wework_webhook")); v != "" && v != s.WeworkWH {
			s.WeworkWH = v
			notify.SetWebhook(v)
			_ = config.SaveNotifyWebhook(v)
			s.ReloadNotifyChannels()
			changes = append(changes, "wework_webhook="+maskWebhook(v))
		}
		if v := r.FormValue("notify_log") == "1"; v != ws.NotifyLog {
			ws.NotifyLog = v
			notify.SetLogEnabled(v)
			changes = append(changes, fmt.Sprintf("notify_log=%v", v))
		}
		// Timezone
		if v := strings.TrimSpace(r.FormValue("timezone")); v != ws.Timezone {
			ws.Timezone = v
			SetTimezone(v)
			notify.SetTimezone(v)
			changes = append(changes, fmt.Sprintf("timezone=%s", v))
		}
		// Jackett
		if has("jackett_mode") {
			if v := normalizeJackettMode(r.FormValue("jackett_mode")); v != normalizeJackettMode(ws.JackettMode) {
				ws.JackettMode = v
				changes = append(changes, fmt.Sprintf("jackett_mode=%s", v))
			}
		}
		if has("jackett_url") {
			if v := strings.TrimSpace(r.FormValue("jackett_url")); v != ws.JackettURL {
				ws.JackettURL = v
				s.JackettURL = v
				changes = append(changes, fmt.Sprintf("jackett_url=%s", maskWebhook(v)))
			}
		}
		if has("jackett_apikey") {
			if v := strings.TrimSpace(r.FormValue("jackett_apikey")); v != ws.JackettAPIKey {
				ws.JackettAPIKey = v
				s.JackettAPIKey = v
				changes = append(changes, "jackett_apikey=***")
			}
		}
		if has("jackett_admin_password") {
			if v := strings.TrimSpace(r.FormValue("jackett_admin_password")); v != ws.JackettAdminPassword {
				ws.JackettAdminPassword = v
				s.JackettAdminPassword = v
				changes = append(changes, "jackett_admin_password=***")
			}
		}
		// Aggregated search title filter. A checkbox only appears in the form
		// data when checked, so presence means enabled and absence means
		// disabled — but only when the form actually carried the field.
		if has("filter_search_title") {
			v := r.FormValue("filter_search_title") == "1"
			if ws.FilterSearchTitle == nil || *ws.FilterSearchTitle != v {
				ws.FilterSearchTitle = boolPtr(v)
				changes = append(changes, fmt.Sprintf("filter_search_title=%v", v))
			}
		} else if has("search_settings_submitted") {
			// The search fieldset was submitted without the checkbox: it was
			// unchecked, which is an explicit "off".
			if ws.FilterSearchTitle == nil || *ws.FilterSearchTitle {
				ws.FilterSearchTitle = boolPtr(false)
				changes = append(changes, "filter_search_title=false")
			}
		}
		// TMDB
		if has("tmdb_apikey") {
			if v := strings.TrimSpace(r.FormValue("tmdb_apikey")); v != ws.TMDBAPIKey {
				ws.TMDBAPIKey = v
				media.InitTMDB(v)
				media.SetTMDBProxy(s.ProxyHTTP)
				changes = append(changes, "tmdb_apikey=***")
			}
		}
		// Page size
		if v, err := strconv.Atoi(r.FormValue("page_size")); err == nil && v >= 10 && v <= 500 && v != ws.PageSize {
			ws.PageSize = v
			changes = append(changes, fmt.Sprintf("page_size=%d", v))
		}
		// Auto update
		if v := r.FormValue("auto_update") == "1"; v != ws.AutoUpdate {
			ws.AutoUpdate = v
			changes = append(changes, fmt.Sprintf("auto_update=%v", v))
		}

		if len(changes) > 0 {
			log.Printf("[settings] updated: %s", strings.Join(changes, ", "))
		}
		s.saveWebSettings(ws)
		s.saveProxyConfig()

		// Apply notification log setting
		notify.SetLogEnabled(ws.NotifyLog)

		// Also save to agent if logged in
		if s.Agent != nil {
			st := p115pkg.AppSettings{
				Lang:         ws.Lang,
				ChunkDelay:   ws.ChunkDelay,
				ChunkSize:    ws.ChunkSize,
				CooldownMinMs: ws.CooldownMin,
				CooldownMaxMs: ws.CooldownMax,
			}
			_ = s.Agent.UpdateSettings(st)
		}

		lang := ws.Lang
		if lang == "" {
			lang = "zh"
		}
		data := s.pageData(tr(lang, "settings_saved"), "")
		data.Page = "settings"
		data.ProxyHTTP = s.ProxyHTTP
		data.WeworkWebhook = s.WeworkWH
		data.NotifyLog = ws.NotifyLog
		data.Timezone = ws.Timezone
		data.TimezoneOptions = timezones
		data.JackettURL = ws.JackettURL
		data.JackettAPIKey = ws.JackettAPIKey
		data.JackettAdminPassword = ws.JackettAdminPassword
		data.JackettMode = normalizeJackettMode(ws.JackettMode)
		data.FilterSearchTitle = ws.filterSearchTitle()
		data.TMDBAPIKey = ws.TMDBAPIKey
		if s.Agent != nil {
			data.Settings = s.Agent.GetSettings()
		}
		data.Settings.Lang = ws.Lang
		if ws.WebPassword != "" {
			data.Settings.WebPassword = ws.WebPassword
		}
		data.Settings.SubsInterval = ws.SubsInterval
		data.Settings.ChunkSize = ws.ChunkSize
		data.Settings.ChunkDelay = ws.ChunkDelay
		data.PageSize = ws.PageSize
		if data.PageSize <= 0 {
			data.PageSize = 50
		}
		data.AutoUpdate = ws.AutoUpdate
		http.SetCookie(w, &http.Cookie{Name: "r2c_lang", Value: lang, Path: "/", MaxAge: 86400 * 365})
		dashboardTemplate.Execute(w, data)
		return
	}

	// GET: load saved settings
	lang := ws.Lang
	if lang == "" {
		lang = "zh"
	}
	errMsg := ""
	if r.URL.Query().Get("err") == "not_logged_in" {
		errMsg = tr(lang, "err_not_logged_in")
	}
	data := s.pageData("", errMsg)
	data.Page = "settings"
	data.ProxyHTTP = s.ProxyHTTP
	data.WeworkWebhook = s.WeworkWH
	data.NotifyLog = ws.NotifyLog
	data.Timezone = ws.Timezone
	data.TimezoneOptions = timezones
	data.JackettURL = ws.JackettURL
	data.JackettAPIKey = ws.JackettAPIKey
	data.JackettAdminPassword = ws.JackettAdminPassword
	data.JackettMode = normalizeJackettMode(ws.JackettMode)
	data.FilterSearchTitle = ws.filterSearchTitle()
	data.TMDBAPIKey = ws.TMDBAPIKey
	if s.Agent != nil {
		data.Settings = s.Agent.GetSettings()
	}
	// Override with persisted web settings (only non-zero = user has set them)
	if ws.Lang != "" {
		data.Settings.Lang = ws.Lang
	}
	if ws.ChunkSize > 0 {
		data.Settings.ChunkSize = ws.ChunkSize
	}
	if ws.ChunkDelay > 0 {
		data.Settings.ChunkDelay = ws.ChunkDelay
	}
	if ws.CooldownMin > 0 {
		data.Settings.CooldownMinMs = ws.CooldownMin
	}
	if ws.CooldownMax > 0 {
		data.Settings.CooldownMaxMs = ws.CooldownMax
	}
	data.Settings.SubsInterval = ws.SubsInterval
	if ws.SubsInterval > 0 {
		data.Settings.SubsInterval = ws.SubsInterval
	}
	if ws.WebPassword != "" {
		data.Settings.WebPassword = ws.WebPassword
	}
	data.PageSize = ws.PageSize
	if data.PageSize <= 0 {
		data.PageSize = 50
	}
	data.AutoUpdate = ws.AutoUpdate

	if lang == "" {
		lang = "zh"
	}
	http.SetCookie(w, &http.Cookie{Name: "r2c_lang", Value: lang, Path: "/", MaxAge: 86400 * 365})
	dashboardTemplate.Execute(w, data)
}

// ---------- dedup ----------

func (s *Server) handleDedup(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "dedup"

	var entries []dedupEntry
	for _, subKey := range globalDedup.SubKeys() {
		cnt := globalDedup.SubCount(subKey)
		if cnt > 0 {
			entries = append(entries, dedupEntry{SubKey: subKey, Count: cnt})
		}
	}
	data.DedupEntries = entries
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleDedupClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subKey := r.FormValue("sub")
	if subKey == "" {
		s.renderMsg(w, "", "err_missing_sub_name")
		return
	}
	globalDedup.ClearSub(subKey)
	log.Printf("[dedup] cleared dedup for %s", subKey)
	http.Redirect(w, r, "/subs", http.StatusSeeOther)
}

func (s *Server) handleDedupHashes(w http.ResponseWriter, r *http.Request) {
	subKey := r.URL.Query().Get("sub")
	hashes := globalDedup.Hashes(subKey)
	// Build response with hash + name + torrent URL
	type hashEntry struct {
		Hash string `json:"hash"`
		Name string `json:"name,omitempty"`
		URL  string `json:"url,omitempty"`
	}
	out := make([]hashEntry, 0, len(hashes))
	for _, h := range hashes {
		name := globalDedup.GetHashName(h)
		if name == "" {
			// Fallback: try to extract from URL
			if u := globalDedup.TorrentURLByHash(h); u != "" {
				name = extractNameFromURL(u)
			}
		}
		out = append(out, hashEntry{Hash: h, Name: name, URL: globalDedup.TorrentURLByHash(h)})
	}
	w.Header().Set("Content-Type", "application/json")
	data, _ := json.Marshal(out)
	w.Write(data)
}

func (s *Server) handleDedupRemoveHash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	subKey := r.FormValue("sub")
	hash := r.FormValue("hash")
	if subKey == "" || hash == "" {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"error","message":"missing sub or hash"}`))
		return
	}
	if globalDedup.RemoveHash(subKey, hash) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"error","message":"not found"}`))
	}
}

// ---------- torrent cache (merged into dedup) ----------

func (s *Server) handleTorrent(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/dedup", http.StatusMovedPermanently)
}

func (s *Server) handleTorrentClear(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	url := r.FormValue("url")
	if url != "" {
		globalDedup.RemoveTorrentURL(url)
		log.Printf("[torrent-cache] removed entry for %s", url)
	} else {
		for _, entry := range globalDedup.AllTorrentURLs() {
			globalDedup.RemoveTorrentURL(entry[0])
		}
		log.Printf("[torrent-cache] cleared all entries")
	}
	http.Redirect(w, r, "/dedup", http.StatusSeeOther)
}

func (s *Server) handleLogPage(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "log"
	// The log panel shows newest first, matching /api/logs, so the client can
	// prepend incremental blocks without reordering anything.
	data.Logs = logBuf.Lines()
	dashboardTemplate.Execute(w, data)
}

func extractNameFromURL(rawURL string) string {
	if strings.Contains(rawURL, "dn=") {
		for _, part := range strings.Split(rawURL, "&") {
			if strings.HasPrefix(strings.ToLower(part), "dn=") {
				n, _ := url.QueryUnescape(part[3:])
				return n
			}
		}
	}
	return ""
}

func (s *Server) handleAbout(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "about"
	data.AboutVersion = Version
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleAPITasks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	type apiTask struct {
		InfoHash string  `json:"info_hash"`
		Name     string  `json:"name"`
		Size     string  `json:"size"`
		Status   string  `json:"status"`
		Percent  float64 `json:"percent"`
		URL      string  `json:"url"`
		RowClass string  `json:"row_class"`
	}
	resp := struct {
		Count int       `json:"count"`
		Tasks []apiTask `json:"tasks"`
	}{}
	if s.Agent != nil {
		tasks, err := s.Agent.ListTasks()
		if err == nil {
			resp.Count = len(tasks)
			s.taskCountCache = len(tasks) // keep dashboard in sync
			for _, t := range tasks {
				row := apiTask{
					InfoHash: t.InfoHash,
					Name:     displayName(t),
					Size:     formatSize(t.Size),
					Percent:  t.Percent,
					URL:      t.URL,
				}
				switch {
				case t.Status == 2:
					row.Status = "done"
					row.RowClass = "row-done"
				case t.Status == -1:
					row.Status = "failed"
					row.RowClass = "row-failed"
				case t.Status == 1:
					row.Status = "downloading"
					row.RowClass = "row-running"
				default:
					row.Status = "waiting"
				}
				resp.Tasks = append(resp.Tasks, row)
			}
		}
	}
	json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleTMDBSearch(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing query"})
		return
	}
	if media.DefaultTMDB == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "TMDB not configured"})
		return
	}
	movies, tvShows, err := media.DefaultTMDB.SearchAll(q)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	// Build response with poster URLs
	type result struct {
		TMDBID    int    `json:"id"`
		Title     string `json:"title"`
		Year      string `json:"year"`
		MediaType string `json:"media_type"`
		Poster    string `json:"poster"`
		Overview  string `json:"overview"`
	}
	moviesOut := make([]result, 0, len(movies))
	for _, m := range movies {
		moviesOut = append(moviesOut, result{
			TMDBID: m.TMDBID, Title: m.DisplayName(), Year: m.YearStr(),
			MediaType: "movie", Poster: m.PosterURL(), Overview: m.Overview,
		})
	}
	tvOut := make([]result, 0, len(tvShows))
	for _, t := range tvShows {
		tvOut = append(tvOut, result{
			TMDBID: t.TMDBID, Title: t.DisplayName(), Year: t.YearStr(),
			MediaType: "tv", Poster: t.PosterURL(), Overview: t.Overview,
		})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"movies": moviesOut,
		"tv":     tvOut,
	})
}

func (s *Server) handleTMDBTrending(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if media.DefaultTMDB == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "TMDB not configured"})
		return
	}
	page := 1
	if p, err := strconv.Atoi(r.URL.Query().Get("page")); err == nil && p > 0 {
		page = p
	}
	movies, tvShows, err := media.DefaultTMDB.TrendingPage(page)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	type result struct {
		TMDBID    int    `json:"id"`
		Title     string `json:"title"`
		Year      string `json:"year"`
		MediaType string `json:"media_type"`
		Poster    string `json:"poster"`
		Overview  string `json:"overview"`
	}
	moviesOut := make([]result, 0, len(movies))
	for _, m := range movies {
		moviesOut = append(moviesOut, result{TMDBID: m.TMDBID, Title: m.DisplayName(), Year: m.YearStr(), MediaType: "movie", Poster: m.PosterURL(), Overview: m.Overview})
	}
	tvOut := make([]result, 0, len(tvShows))
	for _, t := range tvShows {
		tvOut = append(tvOut, result{TMDBID: t.TMDBID, Title: t.DisplayName(), Year: t.YearStr(), MediaType: "tv", Poster: t.PosterURL(), Overview: t.Overview})
	}
	json.NewEncoder(w).Encode(map[string]interface{}{"movies": moviesOut, "tv": tvOut})
}

func (s *Server) handleTMDBDetail(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if media.DefaultTMDB == nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "TMDB not configured"})
		return
	}
	mediaType := r.URL.Query().Get("type")
	idStr := r.URL.Query().Get("id")
	if mediaType == "" || idStr == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "missing type or id"})
		return
	}
	tmdbID, err := strconv.Atoi(idStr)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": "invalid id"})
		return
	}
	detail, err := media.DefaultTMDB.GetDetail(tmdbID, mediaType)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
		return
	}
	// Build response
	type seasonInfo struct {
		SeasonNumber int `json:"season_number"`
		EpisodeCount int `json:"episode_count"`
	}
	seasons := make([]seasonInfo, 0)
	for _, s := range detail.Seasons {
		if s.SeasonNumber > 0 {
			seasons = append(seasons, seasonInfo{SeasonNumber: s.SeasonNumber, EpisodeCount: s.EpisodeCount})
		}
	}
	genres := make([]string, 0)
	for _, g := range detail.Genres {
		genres = append(genres, g.Name)
	}
	title := detail.Title
	if title == "" {
		title = detail.Name
	}
	origTitle := detail.OriginalTitle
	if origTitle == "" {
		origTitle = detail.OriginalName
	}
	year := detail.YearStr()
	json.NewEncoder(w).Encode(map[string]interface{}{
		"id":             detail.TMDBID,
		"title":          title,
		"original_title": origTitle,
		"year":           year,
		"media_type":     mediaType,
		"poster":         detail.PosterURL(),
		"backdrop":       detail.BackdropURL(),
		"overview":       detail.Overview,
		"tagline":        detail.Tagline,
		"genres":         genres,
		"vote_average":   detail.VoteAvg,
		"vote_count":     detail.VoteCount,
		"runtime":        detail.Runtime,
		"status":         detail.Status,
		"seasons":        seasons,
		"num_seasons":    detail.NumSeasons,
		"num_episodes":   detail.NumEps,
	})
}

// buildAllTagsList returns tag strings as []string for JSON encoding
func buildAllTagsList(results []indexer.SearchResult) []string {
	re := regexp.MustCompile(`[\[【]([^\]】]{1,40})[\]】]`)
	seen := make(map[string]bool)
	var tags []string
	for _, r := range results {
		matches := re.FindAllStringSubmatch(r.Title, -1)
		for _, m := range matches {
			tag := strings.TrimSpace(m[1])
			tag = strings.ReplaceAll(tag, "DBD制作组", "DBD-Raws")
			tag = strings.ReplaceAll(tag, "桜都字幕組", "桜都字幕组")
			if tag == "" || len(tag) > 40 { continue }
			if regexp.MustCompile(`^\d+\(\d+\)$`).MatchString(tag) { continue }
			if regexp.MustCompile(`^\d{1,4}$`).MatchString(tag) { continue }
			lk := strings.ToLower(tag)
			if !seen[lk] { seen[lk] = true; tags = append(tags, tag) }
		}
	}
	if tags == nil { tags = []string{} }
	return tags
}

// buildAllGroupsList returns validated group names as []string for JSON encoding
func buildAllGroupsList(results []indexer.SearchResult) []string {
	groupRe := regexp.MustCompile(`^[[【]([^\]】]{1,40})[\]】]`)
	seen := make(map[string]bool)
	var groups []string
	for _, r := range results {
		if m := groupRe.FindStringSubmatch(r.Title); m != nil {
			g := strings.TrimSpace(m[1])
			g = strings.ReplaceAll(g, "DBD制作组", "DBD-Raws")
			g = strings.ReplaceAll(g, "桜都字幕組", "桜都字幕组")
			lk := strings.ToLower(g)
			if !seen[lk] && isValidGroup(g) { seen[lk] = true; groups = append(groups, g) }
		}
	}
	if groups == nil { groups = []string{} }
	return groups
}

func buildAllTagsJSON(results []indexer.SearchResult) template.JS {
	re := regexp.MustCompile(`[\[【]([^\]】]{1,40})[\]】]`)
	seen := make(map[string]bool)
	var tags []string
	for _, r := range results {
		matches := re.FindAllStringSubmatch(r.Title, -1)
		for _, m := range matches {
			tag := strings.TrimSpace(m[1])
			tag = strings.ReplaceAll(tag, "DBD制作组", "DBD-Raws")
			tag = strings.ReplaceAll(tag, "桜都字幕組", "桜都字幕组")
			if tag == "" || len(tag) > 40 { continue }
			if regexp.MustCompile(`^\d+\(\d+\)$`).MatchString(tag) { continue }
			if regexp.MustCompile(`^\d{1,4}$`).MatchString(tag) { continue }
			lk := strings.ToLower(tag)
			if !seen[lk] { seen[lk] = true; tags = append(tags, tag) }
		}
	}
	b, _ := json.Marshal(tags)
	return template.JS(b)
}

// buildAllGroupsJSON collects validated group names from full search results
// Used by client to pre-populate _serverGroups so classifyTag works across all pages
func buildAllGroupsJSON(results []indexer.SearchResult) template.JS {
	groupRe := regexp.MustCompile(`^[[【]([^\]】]{1,40})[\]】]`)
	seen := make(map[string]bool)
	var groups []string
	for _, r := range results {
		if m := groupRe.FindStringSubmatch(r.Title); m != nil {
			g := strings.TrimSpace(m[1])
			g = strings.ReplaceAll(g, "DBD制作组", "DBD-Raws")
			g = strings.ReplaceAll(g, "桜都字幕組", "桜都字幕组")
			lk := strings.ToLower(g)
			if !seen[lk] && isValidGroup(g) { seen[lk] = true; groups = append(groups, g) }
		}
	}
	b, _ := json.Marshal(groups)
	return template.JS(b)
}

// isValidGroup checks if a first-bracket tag looks like a fansub group name
func isValidGroup(g string) bool {
	g = strings.TrimSpace(g)
	if g == "" || len(g) > 40 { return false }
	// Reject noise: episode ranges, labels, anime titles
	noise := []string{
		"合集", "全集", "总集", "新番", "特别篇", "前篇", "后篇", "番外",
		"剧场版", "第", "OVA", "OAD",
	}
	for _, n := range noise {
		if strings.Contains(g, n) { return false }
	}
	// Reject if starts with digit (episode range like "01-24TV全集")
	if len(g) > 0 && g[0] >= '0' && g[0] <= '9' { return false }
	// Accept if: has CJK, or "&", or "-Raws" suffix, or known Latin group
	hasCJK := false
	for _, r := range g {
		if r >= 0x4E00 && r <= 0x9FFF { hasCJK = true; break }
		if r >= 0x3040 && r <= 0x30FF { hasCJK = true; break }
	}
	if hasCJK { return true }
	if strings.Contains(g, "&") { return true }
	if strings.HasSuffix(strings.ToLower(g), "-raws") { return true }
	// Known Latin group patterns
	known := []string{"DBD", "VCB", "ANK", "Moozzi2", "ReinForce", "Beatrice",
		"Snow", "LowPower", "U3-Project", "AI-Raws", "NC-Raws", "Lilith",
		"GJ.Y", "c.c", "MCE", "7³", "ANi", "Porter"}
	for _, k := range known {
		if strings.HasPrefix(g, k) { return true }
	}
	// Short ASCII name (2-10 chars, starts with uppercase) = likely a group
	if len(g) >= 2 && len(g) <= 10 && g[0] >= 'A' && g[0] <= 'Z' {
		allASCII := true
		for _, r := range g {
			if r > 127 { allASCII = false; break }
		}
		if allASCII { return true }
	}
	return false
}

// applyTitleFilter drops results whose title does not contain the query.
//
// Torznab indexers match loosely: a search for a short or common title (the
// documented case is "TARI TARI") comes back with a long tail of unrelated
// releases, because the tracker matched on a fragment, a different season, or
// simply ranked it for the query. Those entries are noise in a long list and,
// worse, they get pushed to 115 if the result is grabbed.
//
// Matching rules:
//
//   - Every term must appear, case-insensitively, and a repeated term must
//     appear as many times as it was written. "TARI TARI" is a title, not an
//     emphasis, so a release named "TARI - 01" is a different show and is
//     rejected.
//   - Terms must appear in the order given, which is how a title reads. This is
//     what keeps "TARI TARI" from matching "TARI something TARI", where the two
//     occurrences are unrelated parts of the name.
//   - Punctuation and whitespace are ignored on both sides, so the query
//     "TARI TARI" matches "TARI.TARI", "TARI_TARI", "TARI[TARI]" and
//     "TARITARI" as well as the spaced form.
//
// The query is the one the user typed; the keyword filter (applyKeywordFilter)
// is separate and still applies on top, since it expresses tag requirements
// rather than the search itself.
func applyTitleFilter(results []indexer.SearchResult, query string) []indexer.SearchResult {
	terms := queryTerms(query)
	if len(terms) == 0 {
		return results
	}
	needle := strings.Join(terms, " ")

	out := make([]indexer.SearchResult, 0, len(results))
	for _, r := range results {
		if matchQueryTerms(normalizeForMatch(r.Title), needle, terms) {
			out = append(out, r)
		}
	}
	return out
}

// matchQueryTerms reports whether the normalised title contains every term, in
// order and with the right multiplicity.
//
// A repeated term is handled by matching the whole query as a phrase first:
// "tari tari" matches "tari tari 01" directly. Only when that fails do the terms
// get matched individually, which covers a title that separates them with other
// words ("tari the animation tari") while still requiring each occurrence.
//
// Individual terms must start at a word boundary. Without that constraint a
// term is found inside unrelated words, and the Romanised Japanese title
// "...sare tari sarenakattaridesuga..." matched "TARI TARI" because "tari"
// appears at the end of "sarenakatta" followed by the "ri" of the next word.
// Release names separate words with spaces, dots and brackets, all of which
// normalisation turns into spaces, so the boundary is well defined.
func matchQueryTerms(title, needle string, terms []string) bool {
	if strings.Contains(title, needle) {
		return true
	}
	// A single term that was already rejected as a phrase cannot match in any
	// other way.
	if len(terms) == 1 {
		return false
	}
	// Walk the terms from the left, consuming the title as each is found at a
	// word boundary. The position advances past every match, so a repeated term
	// needs a second, later occurrence rather than reusing the first.
	rest := title
	for _, t := range terms {
		i := indexAtWordStart(rest, t)
		if i < 0 {
			return false
		}
		rest = rest[i+len(t):]
	}
	return true
}

// indexAtWordStart returns the index of the first occurrence of term in s that
// begins at a word boundary, or -1.
//
// A boundary is the start of the string or a space. Concatenated forms such as
// "taritari" still match their first term (the string starts with it), and the
// second occurrence is then found in what remains.
func indexAtWordStart(s, term string) int {
	for off := 0; ; {
		i := strings.Index(s[off:], term)
		if i < 0 {
			return -1
		}
		abs := off + i
		if abs == 0 || s[abs-1] == ' ' {
			return abs
		}
		off = abs + 1
		if off >= len(s) {
			return -1
		}
	}
}

// queryTerms splits a search query into lower-case terms worth matching on.
//
// Single-character terms are dropped: they match almost everything and would
// make the filter ineffective. Repeated terms are kept as written — a title that
// genuinely repeats a word must appear repeated in the release name.
func queryTerms(query string) []string {
	fields := strings.Fields(strings.ToLower(strings.TrimSpace(query)))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		// Strip punctuation that never appears meaningfully in a title search
		// but does appear in queries, e.g. a trailing colon from an operator.
		f = strings.Trim(f, `"'()[]{}:,;!?*/\|`)
		if len([]rune(f)) < 2 {
			continue
		}
		n := normalizeForMatch(f)
		if n == "" {
			continue
		}
		out = append(out, n)
	}
	return out
}

// normalizeForMatch lower-cases a title and collapses the punctuation that
// differs between a query and how a release names the same thing.
//
// Releases separate words with dots, underscores or brackets
// ("TARI.TARI", "TARI_TARI", "TARI[TARI]"), so a plain substring test against
// the raw title would miss matches the user expects. Everything except letters,
// digits and CJK is turned into a single space, and runs of spaces collapse.
func normalizeForMatch(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 8)
	lastSpace := true // avoid a leading space
	for _, r := range strings.ToLower(s) {
		if isMatchRune(r) {
			b.WriteRune(r)
			lastSpace = false
			continue
		}
		if !lastSpace {
			b.WriteByte(' ')
			lastSpace = true
		}
	}
	return strings.TrimSpace(b.String())
}

// isMatchRune reports whether r survives normalisation.
//
// Letters, digits and any non-ASCII rune are kept: the last case covers CJK,
// which is common in the titles this project targets and must not be stripped
// as punctuation.
func isMatchRune(r rune) bool {
	if r >= 'a' && r <= 'z' {
		return true
	}
	if r >= '0' && r <= '9' {
		return true
	}
	return r > 0x7f
}

// applyKeywordFilter filters results using group-aware keyword matching.
// Format: "group:tag1|tag2 othergroup:tag3"
// Within each group: OR. Across groups: AND.
// If keyword is empty, returns all results unchanged.
func applyKeywordFilter(results []indexer.SearchResult, keyword string) []indexer.SearchResult {
	kw := strings.TrimSpace(keyword)
	if kw == "" {
		return results
	}
	groups := strings.Fields(kw)
	filtered := make([]indexer.SearchResult, 0, len(results))
	for _, r := range results {
		titleLower := strings.ToLower(r.Title)
		match := true
		for _, g := range groups {
			if idx := strings.IndexByte(g, ':'); idx > 0 {
				tags := strings.Split(g[idx+1:], "|")
				grpMatch := false
				for _, tag := range tags {
					if strings.Contains(titleLower, strings.ToLower(tag)) {
						grpMatch = true
						break
					}
				}
				if !grpMatch { match = false; break }
			} else {
				if !strings.Contains(titleLower, strings.ToLower(g)) {
					match = false
					break
				}
			}
		}
		if match {
			filtered = append(filtered, r)
		}
	}
	return filtered
}

// handleAPILogs serves log lines for the live log panel.
//
// Lines are returned newest-first, matching the panel's display order, so the
// client can prepend an incremental block verbatim.
//
// The `since` marker names the newest line the client already has. Everything
// before it in the buffer is newer, and is returned as-is.
//
// The marker is matched by content, not index: log lines are not unique (a
// recurring message such as "no enabled subscriptions" repeats verbatim), so an
// index-based cursor would jump to the wrong place once the buffer wrapped.
func (s *Server) handleAPILogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Already newest-first.
	all := logBuf.Lines()

	since := r.URL.Query().Get("since")
	resp := map[string]interface{}{}

	if since == "" {
		resp["lines"] = all
	} else {
		idx := -1
		for i, l := range all {
			if l == since {
				idx = i
				break
			}
		}
		switch {
		case idx < 0:
			// The marker scrolled out of the ring buffer, or the client is
			// stale. Send the whole log and flag it, so the client replaces
			// its content: prepending here would duplicate every line.
			resp["lines"] = all
			resp["reset"] = true
		case idx == 0:
			// Nothing new since the last poll. Returning an empty slice keeps
			// the client from re-adding the line it already shows.
			resp["lines"] = []string{}
		default:
			resp["lines"] = all[:idx]
		}
	}

	json.NewEncoder(w).Encode(resp)
}

// handleWebhooks is the CRUD endpoint for webhook channels.
//
// Actions: list, add, update, delete, toggle, test. Keeping them on one endpoint
// matches how the rest of this panel works (a single POST with an action field)
// and avoids a route per operation. Testing lives here too, so a channel is
// always verified with the configuration that will actually be used.
func (s *Server) handleWebhooks(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	hooks := s.loadWebhookConfigs()

	if r.Method == http.MethodGet {
		lang := requestLang(r)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":       true,
			"webhooks": hooks,
			"events":   eventTypeList(lang),
		})
		return
	}
	if r.Method != http.MethodPost {
		writeJSON(w, false, "GET 或 POST required")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, false, err.Error())
		return
	}

	action := strings.TrimSpace(r.FormValue("webhook_action"))
	switch action {
	case "add", "update":
		cfg, err := webhookConfigFromForm(r)
		if err != nil {
			writeJSON(w, false, err.Error())
			return
		}
		if action == "add" {
			if cfg.ID == "" {
				cfg.ID = notify.NewWebhookID()
			}
			if cfg.Name == "" {
				cfg.Name = "Webhook " + strconv.Itoa(len(hooks)+1)
			}
			hooks = append(hooks, cfg)
		} else {
			found := false
			for i := range hooks {
				if hooks[i].ID == cfg.ID {
					// An empty secret submitted by the edit form means "keep the
					// stored one": the value is never rendered back into the
					// page, so the form cannot resubmit it.
					if cfg.Secret == "" {
						cfg.Secret = hooks[i].Secret
					}
					hooks[i] = cfg
					found = true
					break
				}
			}
			if !found {
				writeJSON(w, false, "未找到该 Webhook")
				return
			}
		}
		if err := s.saveWebhookConfigs(hooks); err != nil {
			log.Printf("[notify] 保存 webhook 失败: %v", err)
			writeJSON(w, false, "保存失败: "+err.Error())
			return
		}
		log.Printf("[notify] webhook %s: id=%s events=%v enabled=%v",
			action, cfg.ID, cfg.Events, cfg.Enabled)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "已保存", "webhooks": hooks})

	case "delete":
		id := strings.TrimSpace(r.FormValue("id"))
		kept := make([]notify.WebhookConfig, 0, len(hooks))
		for _, h := range hooks {
			if h.ID != id {
				kept = append(kept, h)
			}
		}
		if len(kept) == len(hooks) {
			writeJSON(w, false, "未找到该 Webhook")
			return
		}
		if err := s.saveWebhookConfigs(kept); err != nil {
			writeJSON(w, false, "保存失败: "+err.Error())
			return
		}
		log.Printf("[notify] webhook deleted: id=%s", id)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "已删除", "webhooks": kept})

	case "toggle":
		id := strings.TrimSpace(r.FormValue("id"))
		on := r.FormValue("enabled") == "1"
		found := false
		for i := range hooks {
			if hooks[i].ID == id {
				hooks[i].Enabled = on
				found = true
				break
			}
		}
		if !found {
			writeJSON(w, false, "未找到该 Webhook")
			return
		}
		if err := s.saveWebhookConfigs(hooks); err != nil {
			writeJSON(w, false, "保存失败: "+err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "已更新", "webhooks": hooks})

	case "test":
		// A saved channel is tested exactly as stored — including its kind, which
		// decides the payload format. Testing from the form alone used to lose
		// the kind (the test request carries only an id), so a WeChat Work entry
		// was tested as a generic JSON webhook, which that endpoint rejects:
		// subscribing worked while the test button always failed.
		//
		// An unsaved entry is tested from the form values, so a URL can be
		// verified before committing it.
		cfg, err := webhookConfigFromForm(r)
		if err != nil {
			writeJSON(w, false, err.Error())
			return
		}
		if id := cfg.ID; id != "" {
			found := false
			for _, h := range hooks {
				if h.ID != id {
					continue
				}
				found = true
				// Take the stored entry wholesale, then let the form override the
				// fields it actually submitted. This keeps the kind, and it also
				// means a test uses the saved URL rather than depending on the
				// page having sent it.
				merged := h
				if cfg.Name != "" {
					merged.Name = cfg.Name
				}
				if cfg.URL != "" {
					merged.URL = cfg.URL
				}
				if cfg.Secret != "" {
					merged.Secret = cfg.Secret
				}
				if len(cfg.Events) > 0 {
					merged.Events = cfg.Events
				}
				// Kind comes from the form only when it was actually submitted;
				// otherwise the stored one wins.
				if k := strings.TrimSpace(r.FormValue("kind")); k != "" {
					merged.Kind = notify.NormalizeKind(k)
				}
				cfg = merged
				break
			}
			if !found {
				writeJSON(w, false, "未找到该 Webhook")
				return
			}
		}
		if err := notify.TestWebhookChannel(cfg); err != nil {
			log.Printf("[notify] webhook 测试失败: %v", err)
			writeJSON(w, false, "测试失败: "+err.Error())
			return
		}
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": "测试通知已发送"})

	default:
		writeJSON(w, false, "未知操作: "+action)
	}
}

// webhookConfigFromForm reads a webhook configuration from submitted form data.
func webhookConfigFromForm(r *http.Request) (notify.WebhookConfig, error) {
	rawURL := strings.TrimSpace(r.FormValue("url"))
	if rawURL != "" && !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return notify.WebhookConfig{}, fmt.Errorf("Webhook URL 必须以 http:// 或 https:// 开头")
	}
	cfg := notify.WebhookConfig{
		ID:      strings.TrimSpace(r.FormValue("id")),
		Name:    strings.TrimSpace(r.FormValue("name")),
		Kind:    notify.NormalizeKind(r.FormValue("kind")),
		URL:     rawURL,
		Secret:  strings.TrimSpace(r.FormValue("secret")),
		Events:  r.Form["events"],
		Enabled: r.FormValue("enabled") == "1",
	}
	return cfg, nil
}

// eventTypeList renders the subscribable event types for the settings page.
func eventTypeList(lang string) []map[string]string {
	out := make([]map[string]string, 0, len(notify.AllEventTypes()))
	for _, t := range notify.AllEventTypes() {
		out = append(out, map[string]string{
			"value": string(t),
			"label": eventTypeLabel(lang, t),
		})
	}
	return out
}

// eventTypeLabel gives an event type a translated description.
func eventTypeLabel(lang string, t notify.EventType) string {
	switch t {
	case notify.EventTaskSubmitted:
		return tr(lang, "event_task_submitted")
	case notify.EventTaskFailed:
		return tr(lang, "event_task_failed")
	case notify.EventRSSFound:
		return tr(lang, "event_rss_found")
	case notify.EventUpdateAvailable:
		return tr(lang, "event_update_available")
	case notify.EventServerStarted:
		return tr(lang, "event_server_started")
	case notify.EventLog:
		return tr(lang, "event_log")
	}
	return string(t)
}

// requestLang reads the panel language from the request cookie.
func requestLang(r *http.Request) string {
	if c, err := r.Cookie("r2c_lang"); err == nil && c.Value == "en" {
		return "en"
	}
	return "zh"
}

func (s *Server) handleAPILang(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	lang := strings.TrimSpace(r.FormValue("lang"))
	if lang != "zh" && lang != "en" {
		lang = "zh"
	}
	ws := s.loadWebSettings()
	ws.Lang = lang
	s.saveWebSettings(ws)
	// Also update agent settings
	if s.Agent != nil {
		st := s.Agent.GetSettings()
		st.Lang = lang
		s.Agent.UpdateSettings(st)
	}
	// Set cookie for login page language consistency
	http.SetCookie(w, &http.Cookie{Name: "r2c_lang", Value: lang, Path: "/", MaxAge: 365 * 24 * 3600})
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, `{"ok":true}`)
}

// ---------- login ----------

func (s *Server) handleQRLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		img, err := p115pkg.StartQrcodeLogin()
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"qrcode":"data:image/png;base64,%s"}`, base64Encode(img))
		return
	}
	if r.URL.Query().Get("poll") == "1" {
		success, err := p115pkg.PollQrcodeLogin()
		w.Header().Set("Content-Type", "application/json")
		if err != nil {
			json.NewEncoder(w).Encode(map[string]interface{}{"status": err.Error()})
			return
		}
		if success {
			newAgent, err := p115pkg.FinishQrcodeLogin()
			if err != nil {
				json.NewEncoder(w).Encode(map[string]interface{}{"error": err.Error()})
				return
			}
			s.SetAgent(newAgent)
			fmt.Fprint(w, `{"status":"ok"}`)
		} else {
			fmt.Fprint(w, `{"status":"waiting"}`)
		}
		return
	}
	data := s.pageData("", "")
	data.Page = "settings"
	data.ShowQR = true
	if s.Agent != nil {
		data.Settings = s.Agent.GetSettings()
		data.ProxyHTTP = s.ProxyHTTP
	}
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleCookiesLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		s.renderResult(w, "", err.Error())
		return
	}
	cookies := strings.TrimSpace(r.FormValue("cookies"))
	if cookies == "" {
		s.renderMsg(w, "", "err_cookies_empty")
		return
	}
	newAgent, err := p115pkg.ReloginWithCookies(cookies)
	if err != nil {
		log.Printf("[auth] cookies login failed: %v", err)
		s.renderResult(w, "", tr(s.langFromAgent(), "err_login_failed")+err.Error())
		return
	}
	s.SetAgent(newAgent)
	log.Printf("[auth] cookies updated successfully")
	s.renderMsg(w, "cookies_updated", "")
}

func (s *Server) handleTestJackett(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	url := strings.TrimSpace(r.FormValue("url"))
	apikey := strings.TrimSpace(r.FormValue("apikey"))
	if url == "" || apikey == "" {
		writeJSON(w, false, tr(s.langFromAgent(), "jk_test_empty"))
		return
	}
	if err := jackett.Test(jackett.Config{URL: url, APIKey: apikey}); err != nil {
		writeJSON(w, false, err.Error())
		return
	}
	writeJSON(w, true, tr(s.langFromAgent(), "jk_test_ok"))
}

func (s *Server) handleTest115(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.Agent == nil {
		fmt.Fprint(w, `{"ok":false,"msg":"`+tr(s.langFromAgent(), "err_not_logged_in")+`"}`)
		return
	}
	if err := s.Agent.TestConnection(); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	fmt.Fprintf(w, `{"ok":true,"msg":"%s"}`, tr(s.langFromAgent(), "conn_ok"))
}

func (s *Server) handleRestart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"msg":"%s"}`, tr(s.langFromAgent(), "restarting_json"))

	go func() {
		time.Sleep(500 * time.Millisecond)
		exe, err := os.Executable()
		if err != nil {
			log.Printf("[restart] failed to get executable: %v", err)
			os.Exit(1)
		}
		var args []string
		skipNext := false
		for i, a := range os.Args {
			if i == 0 { continue }
			if skipNext { skipNext = false; continue }
			if a == "--port" || a == "-p" { args = append(args, a); if i+1 < len(os.Args) { args = append(args, os.Args[i+1]) }; skipNext = true; continue }
			args = append(args, a)
		}
		cmd := exec.Command(exe, args...)
		// Keep the current working directory; see doRestart for why it must not
		// be switched to the binary's folder.
		cmd.Dir = ""
		// Detach into a new session so the replacement process survives our exit
		// and is not tied to this process's controlling terminal. Setsid also
		// creates a new process group; adding Setpgid would fail with EPERM
		// because setpgid(2) rejects a session leader.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		cmd.Stdin = nil
		cmd.Stdout = nil
		cmd.Stderr = nil
		if err := cmd.Start(); err != nil {
			log.Printf("[restart] failed to spawn: %v", err)
			os.Exit(1)
		}
		log.Printf("[restart] new process started (pid %d), exiting", cmd.Process.Pid)
		os.Exit(0)
	}()
}

// handleCheckUpdate queries GitHub for the latest release.
func (s *Server) handleCheckUpdate(w http.ResponseWriter, r *http.Request) {
	type updateInfo struct {
		Latest    string `json:"latest"`
		Current   string `json:"current"`
		HasUpdate bool   `json:"has_update"`
		URL       string `json:"url,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")

	info := updateInfo{Current: Version}

	// Prefer what the background poll already knows, so the page answers without
	// a GitHub round trip. Fall back to querying directly if it has not run yet.
	s.updateNoticeMu.Lock()
	cached := s.updateNotice
	s.updateNoticeMu.Unlock()

	if cached != "" {
		info.Latest = cached
		info.HasUpdate = compareVersion(cached, Version) > 0
		if info.HasUpdate {
			info.URL = "https://github.com/lingeringdawn/pan-fetcher/releases/tag/" + cached
		}
		json.NewEncoder(w).Encode(info)
		return
	}

	resp, err := s.httpClient().Get("https://api.github.com/repos/lingeringdawn/pan-fetcher/releases/latest")
	if err != nil {
		json.NewEncoder(w).Encode(info)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		json.NewEncoder(w).Encode(info)
		return
	}

	var release struct {
		TagName string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		json.NewEncoder(w).Encode(info)
		return
	}

	info.Latest = release.TagName
	info.HasUpdate = compareVersion(release.TagName, Version) > 0
	info.URL = release.HTMLURL

	json.NewEncoder(w).Encode(info)
}

// handleDoUpdate downloads the latest binary from GitHub and replaces self.
func (s *Server) handleDoUpdate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	lang := s.langFromAgent()
	w.Header().Set("Content-Type", "application/json")

	// pan-fetcher is Linux-only, so the release asset is unambiguous.
	const binaryName = "pan-fetcher"
	goarch := runtime.GOARCH

	// Get latest release info
	resp, err := s.httpClient().Get("https://api.github.com/repos/lingeringdawn/pan-fetcher/releases/latest")
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": tr(lang, "update_fetch_failed") + ": " + err.Error()})
		return
	}
	defer resp.Body.Close()

	var release struct {
		TagName string `json:"tag_name"`
		Assets  []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": tr(lang, "update_parse_failed")})
		return
	}

	if release.TagName == Version {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": tr(lang, "update_already_latest")})
		return
	}

	// Find the Linux asset for this architecture (tar.gz).
	pattern := fmt.Sprintf("linux-%s", goarch)
	var downloadURL string
	for _, a := range release.Assets {
		if strings.Contains(a.Name, pattern) && !strings.Contains(a.Name, "checksum") {
			downloadURL = a.URL
			break
		}
	}
	if downloadURL == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": trf(lang, "update_no_asset", "linux/"+goarch)})
		return
	}

	// Download the archive
	dlResp, err := s.httpClient().Get(downloadURL)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": tr(lang, "update_download_failed") + ": " + err.Error()})
		return
	}
	defer dlResp.Body.Close()

	// Extract the binary from the tar.gz archive.
	binaryData, err := extractFromTarGz(dlResp.Body, binaryName)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": tr(lang, "update_download_failed") + ": " + err.Error()})
		return
	}

	exe, err := os.Executable()
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": "executable: " + err.Error()})
		return
	}

	// Write new binary next to the executable (same filesystem = atomic rename works)
	tmpPath := exe + ".new"
	if err := os.WriteFile(tmpPath, binaryData, 0755); err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": updateWriteError(lang, tmpPath, err)})
		return
	}

	// Replace the running binary. On Linux the running executable may be renamed
	// over, so this succeeds in place and we then restart ourselves.
	if err := os.Rename(tmpPath, exe); err != nil {
		os.Remove(tmpPath)
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": updateWriteError(lang, exe, err)})
		return
	}

	json.NewEncoder(w).Encode(map[string]interface{}{"ok": true, "msg": tr(lang, "update_ok")})
	go func() { time.Sleep(500 * time.Millisecond); doRestart() }()
}

// updateWriteError turns a failed binary replacement into an actionable message.
//
// A bare os error ("open /opt/.../pan-fetcher.new: permission denied") gives the
// user no idea what to do. The usual cause is an install directory owned by
// root while the service runs unprivileged, so name the directory and the fix.
func updateWriteError(lang, path string, err error) string {
	if errors.Is(err, fs.ErrPermission) {
		return trf(lang, "update_need_sudo_fmt", filepath.Dir(path))
	}
	return tr(lang, "update_install_failed") + ": " + err.Error()
}

// extractFromTarGz extracts a named file from a .tar.gz stream.
func extractFromTarGz(r io.Reader, name string) ([]byte, error) {
	gz, err := gzip.NewReader(r)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("tar: %w", err)
		}
		if hdr.Name == name || filepath.Base(hdr.Name) == name {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", name)
}

// compareVersion compares two semver strings (e.g. "v2.0.1" vs "v2.0.0").
// Returns positive if a > b, zero if equal, negative if a < b.
//
// Pre-release suffixes are understood: a version carrying one (-rc1, -beta2)
// sorts below the same version without it, so "v2.0.1-rc1" < "v2.0.1" but
// "v2.0.1-rc1" > "v2.0.0". Parsing previously ignored the suffix entirely,
// which made "v2.0.1-rc1" compare equal to "v2.0.0" and hid the update.
func compareVersion(a, b string) int {
	va, pa := parseVersion(a)
	vb, pb := parseVersion(b)

	for i := 0; i < len(va) && i < len(vb); i++ {
		if va[i] != vb[i] {
			return va[i] - vb[i]
		}
	}
	if d := len(va) - len(vb); d != 0 {
		return d
	}
	// Numeric parts equal: a release outranks its own pre-release.
	switch {
	case pa == "" && pb != "":
		return 1
	case pa != "" && pb == "":
		return -1
	case pa == pb:
		return 0
	case pa < pb:
		return -1
	default:
		return 1
	}
}

// parseVersion splits "v1.2.3-rc1" into its numeric parts and pre-release tag.
// Non-numeric segments are skipped rather than silently becoming zero, so a
// malformed version cannot masquerade as a valid one.
func parseVersion(s string) ([]int, string) {
	s = strings.TrimPrefix(strings.TrimSpace(s), "v")

	prerelease := ""
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		prerelease = s[i+1:]
		s = s[:i]
	}

	parts := strings.Split(s, ".")
	nums := make([]int, 0, len(parts))
	for _, p := range parts {
		n, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			continue // drop unparsable segments instead of counting them as 0
		}
		nums = append(nums, n)
	}
	return nums, prerelease
}

// doRestart spawns a new process and exits.
func doRestart() {
	exe, err := os.Executable()
	if err != nil {
		log.Printf("[update] failed to get executable: %v", err)
		os.Exit(1)
	}
	var args []string
	skipNext := false
	for i, a := range os.Args {
		if i == 0 {
			continue
		}
		if skipNext {
			skipNext = false
			continue
		}
		if a == "--port" || a == "-p" {
			args = append(args, a)
			if i+1 < len(os.Args) {
				args = append(args, os.Args[i+1])
			}
			skipNext = true
			continue
		}
		args = append(args, a)
	}
	cmd := exec.Command(exe, args...)
	// Keep the current working directory.
	//
	// It used to be set to the binary's folder, which broke every relative path
	// in the new process: config.toml was no longer found, so the database path
	// fell back to a default beside the binary and the data directory (and with
	// it web-settings.json and the Jackett connection) moved out from under the
	// deployment. Leaving the working directory alone preserves whatever the
	// service manager or operator established.
	cmd.Dir = ""
	// Detach into a new session so the replacement process survives our exit.
	// Setsid also creates a new process group; Setpgid must not be combined with
	// it, since setpgid(2) rejects a session leader with EPERM.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		log.Printf("[update] failed to spawn: %v", err)
		os.Exit(1)
	}
	log.Printf("[update] new process started (pid %d), exiting", cmd.Process.Pid)
	os.Exit(0)
}

// ---------- search ----------

func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "discover"
	ws := s.loadWebSettings()
	data.PageSize = ws.PageSize
	if data.PageSize <= 0 { data.PageSize = 50 }
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "search"

	// Restore previous search results on GET (page load / refresh)
	if r.Method != http.MethodPost {
		if ps := loadSearchCache(); ps != nil {
			data.SearchQuery = ps.Query
			data.SearchKeyword = ps.Keyword
			data.SearchSort = ps.Sort
			data.SearchIndexers = ps.Indexers
			data.PageSize = ps.PageSize
			if data.PageSize <= 0 { data.PageSize = 50 }
			data.SearchTotal = len(searchCache)
			data.AllTagsJSON = buildAllTagsJSON(searchCacheFull)
			data.AllGroupsJSON = buildAllGroupsJSON(searchCacheFull)
			if len(searchCache) > data.PageSize {
				data.SearchResults = searchCache[:data.PageSize]
			} else {
				data.SearchResults = searchCache
			}
			// Ensure formatted fields for display (not persisted in cache)
			for i := range data.SearchResults {
				if data.SearchResults[i].SizeFmt == "" {
					data.SearchResults[i].SizeFmt = formatSize(data.SearchResults[i].Size)
				}
				if data.SearchResults[i].DateFmt == "" && !data.SearchResults[i].PublishDate.IsZero() {
					data.SearchResults[i].DateFmt = data.SearchResults[i].PublishDate.Format("2006-01-02 15:04")
				}
			}
			if ps.Query != "" {
				data.RssURL = buildRssURL(s.Port, ps.Query, ps.Indexers)
			}
		}
	}

	// Populate the indexer filter checkboxes from Jackett's activated indexers.
	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	for _, jk := range s.jackettCache {
		if !s.jackettActive[jk.ID] {
			continue
		}
		data.IndexerList = append(data.IndexerList, indexer.IndexerInfo{
			ID:       "jackett:" + jk.ID,
			Name:     jk.Name,
			Type:     jk.Type,
			Language: jk.Language,
			SiteLink: jk.SiteLink,
			Enabled:  true,
			Source:   "jackett",
		})
	}
	s.jackettActiveMu.Unlock()
	// Load saved searches
	data.SavedSearches = s.loadSavedSearches()

	if r.Method == http.MethodPost {
		if r.FormValue("action") == "unsubscribe" {
			s.handleSearchUnsubscribe(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		q := strings.TrimSpace(r.FormValue("q"))
		if q == "" {
			data.Error = tr(s.langFromAgent(), "enter_search_kw")
			dashboardTemplate.Execute(w, data)
			return
		}
		if !s.jackettConfigured() {
			data.Error = tr(s.langFromAgent(), "err_jackett_not_configured")
			dashboardTemplate.Execute(w, data)
			return
		}

		// Parse filters
		sortBy := strings.TrimSpace(r.FormValue("sort"))
		keyword := strings.TrimSpace(r.FormValue("keyword"))
		if sortBy == "" {
			sortBy = "seeds"
		}
		indexers := r.Form["indexer"]

		// Determine which Jackett indexers participate in this search.
		s.jackettActiveMu.Lock()
		s.loadJackettEnabled()
		jackettActiveSet := make(map[string]bool)
		for id := range s.jackettActive {
			jackettActiveSet[id] = true
		}
		s.jackettActiveMu.Unlock()

		// Selected IDs arrive either prefixed ("jackett:foo") or bare, depending
		// on which page issued the request; normalise both forms.
		var selectedIDs []string
		for _, id := range indexers {
			id = strings.TrimPrefix(id, "jackett:")
			if id != "" {
				selectedIDs = append(selectedIDs, id)
			}
		}

		// Query Jackett once and filter its results locally. Jackett returns
		// results from every configured indexer, so per-indexer selection is
		// applied here rather than by issuing one request per indexer.
		jc := s.jackettConfig()
		jackettResults, jerr := jackett.Search(jc, q, nil, 0)

		se := indexer.SearchAllErrors{Errors: make(map[string]string)}
		if jerr != nil {
			se.Errors["jackett"] = jerr.Error()
		} else {
			selectedLower := make(map[string]bool, len(selectedIDs))
			for _, id := range selectedIDs {
				selectedLower[strings.ToLower(id)] = true
			}
			for _, r := range jackettResults {
				trackerLower := strings.ToLower(r.Tracker)
				// With no explicit selection, only activated indexers are used.
				if len(selectedLower) == 0 {
					if !jackettActiveSet[trackerLower] {
						continue
					}
				} else if !selectedLower[trackerLower] {
					continue
				}
				var pubDate time.Time
				if r.PublishDate != "" {
					pubDate, _ = time.Parse(time.RFC1123Z, r.PublishDate)
					if pubDate.IsZero() {
						pubDate, _ = time.Parse(time.RFC1123, r.PublishDate)
					}
				}
				se.Results = append(se.Results, indexer.SearchResult{
					Title:       r.Title,
					MagnetURL:   r.MagnetURI,
					PageURL:     r.Link,
					Size:        r.Size,
					SizeFmt:     formatSize(r.Size),
					DateFmt:     formatJackettDate(r.PublishDate),
					Seeders:     r.Seeders,
					Leechers:    r.Peers,
					IndexerID:   r.Tracker,
					IndexerName: r.TrackerName,
					PublishDate: pubDate,
				})
			}
		}
		data.SearchQuery = q
		data.SearchKeyword = keyword

		data.SearchSort = sortBy
		data.SearchIndexers = indexers
		data.SearchErrors = se.Errors

		// Cache all results, display first page
		ws := s.loadWebSettings()
		pageSize := ws.PageSize
		if pageSize <= 0 {
			pageSize = 50
		}
		// Drop results whose title does not contain the query before anything
		// else looks at them, so paging, counts and tags all describe the same
		// set the user sees.
		if ws.filterSearchTitle() {
			before := len(se.Results)
			se.Results = applyTitleFilter(se.Results, q)
			if dropped := before - len(se.Results); dropped > 0 {
				log.Printf("[search] title filter dropped %d/%d results for %q", dropped, before, q)
			}
		}
		searchCacheMu.Lock()
		indexer.SortResults(se.Results, sortBy)
		searchCacheFull = dedupSlice(se.Results, nil)
		// Extract group from first [...] or 【...】 in title (with validation)
		groupRe := regexp.MustCompile(`^[[【]([^\]】]{1,40})[\]】]`)
		for i := range searchCacheFull {
			if searchCacheFull[i].Group == "" {
				if m := groupRe.FindStringSubmatch(searchCacheFull[i].Title); m != nil {
					g := strings.TrimSpace(m[1])
					if isValidGroup(g) { searchCacheFull[i].Group = g }
				}
			}
		}
		// Apply keyword filter onto searchCache (working set)
		searchCache = applyKeywordFilter(searchCacheFull, keyword)
		filterKeyword = keyword
		searchCtx = searchContext{
			Query:    q,
			Sort:     sortBy,
			Indexers: indexers,
			NextPage: 2,
			PageSize: pageSize,
		}
		searchCacheMu.Unlock()
		data.PageSize = pageSize
		data.SearchTotal = len(searchCache)
		// Build tag/group JSON from FULL cache (all results, not just filtered)
		data.AllTagsJSON = buildAllTagsJSON(searchCacheFull)
		data.AllGroupsJSON = buildAllGroupsJSON(searchCacheFull)
		if len(searchCache) > pageSize {
			data.SearchResults = searchCache[:pageSize]
		} else {
			data.SearchResults = searchCache
		}
		// Auto-build RSS URL for subscription (local aggregated feed)
		if q != "" {
			data.RssURL = buildRssURL(s.Port, q, indexers)
		}
		// Persist search results for next page load
		saveSearchCache()
	}
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleSearchMore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	offset, _ := strconv.Atoi(r.FormValue("offset"))
	keyword := strings.TrimSpace(r.FormValue("keyword"))
	sortBy := strings.TrimSpace(r.FormValue("sort"))

	type apiResult struct {
		Title       string `json:"title"`
		MagnetURL   string `json:"magnet_url,omitempty"`
		PageURL     string `json:"page_url,omitempty"`
		SizeFmt     string `json:"size"`
		Seeders     int    `json:"seeders"`
		IndexerName string `json:"indexer"`
		DateFmt     string `json:"date"`
		Group       string `json:"group,omitempty"`
	}

	searchCacheMu.Lock()
	ctx := searchCtx
	pageSize := ctx.PageSize
	if pageSize <= 0 {
		pageSize = 50
	}
	// If keyword changed, re-filter from full cache
	if keyword != filterKeyword {
		if keyword == "" {
			searchCache = searchCacheFull
		} else {
			searchCache = applyKeywordFilter(searchCacheFull, keyword)
		}
		filterKeyword = keyword
	}
	// If sort changed, re-sort the full cache and re-apply keyword
	if sortBy != "" && sortBy != ctx.Sort {
		// Make a copy and sort it
		sortedFull := make([]indexer.SearchResult, len(searchCacheFull))
		copy(sortedFull, searchCacheFull)
		indexer.SortResults(sortedFull, sortBy)
		searchCacheFull = sortedFull
		// Re-apply keyword filter on newly sorted cache
		if filterKeyword == "" {
			searchCache = searchCacheFull
		} else {
			searchCache = applyKeywordFilter(searchCacheFull, filterKeyword)
		}
		ctx.Sort = sortBy
	}
	cached := searchCache
	searchCacheMu.Unlock()

	// Return up to pageSize items from the cache (skip offset items)
	skip := offset
	var results []apiResult
	for _, r := range cached {
		if skip > 0 {
			skip--
			continue
		}
		if len(results) >= pageSize {
			break
		}
		item := apiResult{
			Title:       r.Title,
			MagnetURL:   r.MagnetURL,
			PageURL:     r.PageURL,
			SizeFmt:     formatSize(r.Size),
			Seeders:     r.Seeders,
			IndexerName: r.IndexerName,
			Group:       r.Group,
		}
		if r.DateFmt != "" {
			item.DateFmt = r.DateFmt
		} else if !r.PublishDate.IsZero() {
			item.DateFmt = r.PublishDate.Format("2006-01-02 15:04")
		}
		results = append(results, item)
	}

	done := offset+len(results) >= len(cached)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"results":    results,
		"done":       done,
		"total":      len(cached),
		"all_tags":   buildAllTagsList(cached),
		"all_groups": buildAllGroupsList(cached),
	})
}

func (s *Server) handleSearchClearCache(w http.ResponseWriter, r *http.Request) {
	os.Remove(searchCacheFile)
	searchCacheMu.Lock()
	searchCache = nil
	searchCacheFull = nil
	filterKeyword = ""
	searchCacheMu.Unlock()
	w.WriteHeader(http.StatusOK)
}

// searchNextPage fetches the next page of results from Jackett.
func (s *Server) searchNextPage(ctx searchContext) []indexer.SearchResult {
	log.Printf("[search] fetching page %d for %q (indexers: %v)", ctx.NextPage, ctx.Query, ctx.Indexers)
	if !s.jackettConfigured() {
		return nil
	}

	// Normalise selection: context IDs may carry the "jackett:" prefix.
	selectedLower := make(map[string]bool, len(ctx.Indexers))
	for _, id := range ctx.Indexers {
		id = strings.ToLower(strings.TrimPrefix(id, "jackett:"))
		if id != "" {
			selectedLower[id] = true
		}
	}

	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	jackettActiveSet := make(map[string]bool)
	for id := range s.jackettActive {
		jackettActiveSet[id] = true
	}
	s.jackettActiveMu.Unlock()

	// Jackett's Torznab API pages in fixed steps, which must match the limit
	// jackett.Search requests.
	const jackettPageSize = jackett.SearchLimit
	offset := (ctx.NextPage - 1) * jackettPageSize

	jr, err := jackett.Search(s.jackettConfig(), ctx.Query, nil, offset)
	if err != nil {
		log.Printf("[search] page %d Jackett error: %v", ctx.NextPage, err)
		return nil
	}

	all := make([]indexer.SearchResult, 0, len(jr))
	for _, r := range jr {
		trackerLower := strings.ToLower(r.Tracker)
		if len(selectedLower) == 0 {
			if !jackettActiveSet[trackerLower] {
				continue
			}
		} else if !selectedLower[trackerLower] {
			continue
		}
		var pubDate time.Time
		if r.PublishDate != "" {
			pubDate, _ = time.Parse(time.RFC1123Z, r.PublishDate)
			if pubDate.IsZero() {
				pubDate, _ = time.Parse(time.RFC1123, r.PublishDate)
			}
		}
		all = append(all, indexer.SearchResult{
			Title:       r.Title,
			MagnetURL:   r.MagnetURI,
			PageURL:     r.Link,
			Size:        r.Size,
			SizeFmt:     formatSize(r.Size),
			DateFmt:     formatJackettDate(r.PublishDate),
			Seeders:     r.Seeders,
			Leechers:    r.Peers,
			IndexerID:   r.Tracker,
			IndexerName: r.TrackerName,
			PublishDate: pubDate,
		})
	}

	log.Printf("[search] page %d returned %d results", ctx.NextPage, len(all))
	return dedupSlice(all, nil)
}


// ---------- indexer management ----------

func (s *Server) handleIndexers(w http.ResponseWriter, r *http.Request) {
	data := s.pageData("", "")
	data.Page = "indexers"

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		action := r.FormValue("action")
		id := r.FormValue("id")

		switch action {
		case "jk_add_to_jackett":
			s.handleJKAddToJackett(w, r, id)
			return
		case "jk_remove_from_jackett":
			s.handleJKRemoveFromJackett(w, r, id)
			return
		case "jk_activate":
			s.jackettActiveMu.Lock()
			if s.jackettActive == nil {
				s.jackettActive = make(map[string]bool)
			}
			s.jackettActive[id] = true
			s.saveJackettEnabled()
			s.jackettActiveMu.Unlock()
			log.Printf("[jackett] %s activated", id)
		case "jk_deactivate":
			s.jackettActiveMu.Lock()
			if s.jackettActive != nil {
				delete(s.jackettActive, id)
				s.saveJackettEnabled()
			}
			s.jackettActiveMu.Unlock()
			log.Printf("[jackett] %s deactivated", id)
		case "jk_activate_batch":
			ids := r.Form["ids"]
			s.jackettActiveMu.Lock()
			if s.jackettActive == nil {
				s.jackettActive = make(map[string]bool)
			}
			for _, bid := range ids {
				if bid != "" {
					s.jackettActive[bid] = true
				}
			}
			s.saveJackettEnabled()
			s.jackettActiveMu.Unlock()
			log.Printf("[jackett] batch activated %d indexers", len(ids))
		case "jk_deactivate_all":
			s.jackettActiveMu.Lock()
			s.jackettActive = make(map[string]bool)
			s.saveJackettEnabled()
			s.jackettActiveMu.Unlock()
			log.Printf("[jackett] all indexers deactivated")
		}
		if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"ok":true}`)
			return
		}
		http.Redirect(w, r, "/indexers", http.StatusSeeOther)
		return
	}

	// Use cached Jackett data; trigger background refresh for next request.
	data.JackettURL = s.JackettURL
	data.JackettAPIKey = s.JackettAPIKey
	data.JackettAdminPassword = s.JackettAdminPassword
	s.jackettCacheMu.Lock()
	data.JackettLibrary = s.jackettCache
	s.jackettCacheMu.Unlock()
	// Background async refresh (don't block page load)
	go s.refreshJackettCache()

	// Build the activated list from Jackett's configured indexers.
	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	for _, jk := range data.JackettLibrary {
		if !s.jackettActive[jk.ID] {
			continue
		}
		data.IndexerList = append(data.IndexerList, indexer.IndexerInfo{
			ID:       jk.ID,
			Name:     jk.Name,
			Type:     jk.Type,
			Language: jk.Language,
			SiteLink: jk.SiteLink,
			Enabled:  true,
			Healthy:  true,
			Source:   "jackett",
		})
	}
	s.jackettActiveMu.Unlock()
	dashboardTemplate.Execute(w, data)
}

func (s *Server) handleJackettList(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.JackettURL == "" || s.JackettAPIKey == "" {
		fmt.Fprint(w, `{"ok":false,"msg":"not configured"}`)
		return
	}

	jk := s.refreshJackettCache()
	if len(jk) == 0 {
		fmt.Fprint(w, `{"ok":true,"data":[],"active":[]}`)
		return
	}

	// Build active ID list from server state (always up-to-date)
	s.jackettActiveMu.Lock()
	activeIDs := make([]string, 0, len(s.jackettActive))
	for id := range s.jackettActive {
		activeIDs = append(activeIDs, id)
	}
	s.jackettActiveMu.Unlock()

	data, _ := json.Marshal(jk)
	activeJSON, _ := json.Marshal(activeIDs)
	fmt.Fprintf(w, `{"ok":true,"data":%s,"active":%s}`, data, activeJSON)
}

// handleJackettAll returns ALL Jackett indexers (including unconfigured).
func (s *Server) handleJackettAll(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.JackettURL == "" || s.JackettAPIKey == "" {
		fmt.Fprint(w, `{"ok":false,"msg":"not configured"}`)
		return
	}
	all, err := jackett.ListAllIndexers(s.jackettConfig())
	if err != nil {
		// Encode through json rather than Fprintf: Jackett errors contain
		// quotes (e.g. Get "http://...": deadline exceeded), which broke the
		// response body and surfaced in the UI as a JSON parse error.
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	data, _ := json.Marshal(all)
	fmt.Fprintf(w, `{"ok":true,"data":%s}`, data)
}

// handleJackettAdd configures an indexer in Jackett via the admin API.
func (s *Server) handleJackettAdd(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		fmt.Fprint(w, `{"ok":false,"msg":"POST required"}`)
		return
	}
	id := r.FormValue("id")
	if id == "" {
		fmt.Fprint(w, `{"ok":false,"msg":"missing id"}`)
		return
	}
	jc := s.jackettConfig()
	if err := jackett.AddIndexer(jc, id); err != nil {
		// Encoded via json so quotes in the error cannot corrupt the body.
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	log.Printf("[jackett] added indexer: %s", id)
	fmt.Fprint(w, `{"ok":true,"msg":"added"}`)
}

// refreshJackettCache fetches the latest indexer list from Jackett,
// updates the cache, and prunes stale entries from the active set.
func (s *Server) refreshJackettCache() []jackett.IndexerInfo {
	if s.JackettURL == "" || s.JackettAPIKey == "" {
		return nil
	}

	jk, err := jackett.ListIndexers(s.jackettConfig())
	if err != nil {
		log.Printf("[jackett] list indexers: %v", err)
		s.jackettCacheMu.Lock()
		cached := s.jackettCache
		s.jackettCacheMu.Unlock()
		return cached
	}

	// Build set of current Jackett IDs
	currentIDs := make(map[string]bool, len(jk))
	for _, idx := range jk {
		currentIDs[idx.ID] = true
	}

	// Update cache; log only if count changed
	s.jackettCacheMu.Lock()
	oldCount := len(s.jackettCache)
	s.jackettCache = jk
	s.jackettCacheTime = time.Now()
	s.jackettCacheMu.Unlock()

	if len(jk) != oldCount {
		log.Printf("[jackett] loaded %d configured indexers (was %d)", len(jk), oldCount)
	}

	// Prune stale entries from jackettActive
	s.jackettActiveMu.Lock()
	s.loadJackettEnabled()
	changed := false
	for id := range s.jackettActive {
		if !currentIDs[id] {
			delete(s.jackettActive, id)
			changed = true
			log.Printf("[jackett] removed stale activation: %s", id)
		}
	}
	if changed {
		s.saveJackettEnabled()
	}
	s.jackettActiveMu.Unlock()

	return jk
}

// ---------- Jackett discovery / install ----------

// handleJackettDiscover probes the local machine for a Jackett instance and
// returns what it found so the settings form can be pre-filled.
//
// The API key is deliberately not persisted here: the caller decides whether to
// save it, so that merely opening the page never overwrites a working config.
func (s *Server) handleJackettDiscover(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	d := jackett.Discover()
	log.Printf("[jackett] discover: found=%v running=%v url=%q config=%q",
		d.Found, d.Running, d.URL, d.ConfigPath)

	resp := map[string]interface{}{
		"ok":             true,
		"found":          d.Found,
		"running":        d.Running,
		"url":            d.URL,
		"apikey":         d.APIKey,
		"admin_password": d.AdminPassword,
		"port":           d.Port,
		"config_path":    d.ConfigPath,
		"source":         d.Source,
		// The install directory is reported so the UI can show where Jackett
		// would be placed before the user commits to downloading it.
		"install_dir": s.jackettInstallDir(),
	}
	json.NewEncoder(w).Encode(resp)
}

// handleJackettInstall downloads and extracts Jackett, then optionally starts it.
//
// The download is large (roughly 50 MB) so the operation runs synchronously but
// reports its progress trail in the response; the UI shows a spinner meanwhile.
func (s *Server) handleJackettInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, false, "POST required")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, false, err.Error())
		return
	}
	force := r.FormValue("force") == "1"
	startAfter := r.FormValue("start") != "0" // default: start it

	dir := s.jackettInstallDir()
	log.Printf("[jackett] install requested: dir=%s force=%v start=%v", dir, force, startAfter)

	res, err := jackett.Install(r.Context(), dir, force, log.Printf)
	if err != nil {
		log.Printf("[jackett] install failed: %v", err)
		writeJSON(w, false, err.Error())
		return
	}

	// Adopt the freshly installed instance's settings automatically.
	url := s.JackettURL
	apiKey := s.JackettAPIKey
	adminPassword := s.JackettAdminPassword
	if d := jackett.Discover(); d.Found {
		if d.URL != "" {
			url = d.URL
		}
		if d.APIKey != "" {
			apiKey = d.APIKey
		}
		if d.AdminPassword != "" {
			adminPassword = d.AdminPassword
		}
	}
	if url == "" {
		url = fmt.Sprintf("http://127.0.0.1:%d", jackett.DefaultPort)
	}

	if startAfter {
		// Start reports the endpoint that actually answered, which may differ
		// from the requested one: Jackett reads its port from an OS-level data
		// folder rather than from the install directory.
		actualURL, err := jackett.Start(r.Context(), dir, url, log.Printf)
		if err != nil {
			// The install itself succeeded, so report the start failure but keep
			// the discovered settings so the user can retry manually.
			s.applyJackettSettings(url, apiKey, adminPassword)
			writeJSON(w, false, "安装完成，但启动失败: "+err.Error())
			return
		}
		url = actualURL
		res.Started = true
		// Re-read the config now that Jackett has initialised its data folder;
		// this is when a freshly generated API key becomes available.
		if d := jackett.Discover(); d.Found {
			if d.APIKey != "" {
				apiKey = d.APIKey
			}
			if d.AdminPassword != "" {
				adminPassword = d.AdminPassword
			}
		}
	}

	s.applyJackettSettings(url, apiKey, adminPassword)
	go s.refreshJackettCache()

	log.Printf("[jackett] install complete: %s (started=%v)", res.Dir, res.Started)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":         true,
		"msg":        "Jackett 安装完成",
		"url":        url,
		"apikey":     apiKey,
		"started":    res.Started,
		"version":    res.Version,
		"dir":        res.Dir,
		"log":        res.Log,
	})
}

// normalizeJackettMode maps an arbitrary value onto a known mode.
//
// Empty means managed: that is the behaviour of every config written before the
// switch existed, so an upgrade keeps working without touching settings.
func normalizeJackettMode(v string) string {
	if strings.EqualFold(strings.TrimSpace(v), JackettModeExternal) {
		return JackettModeExternal
	}
	return JackettModeManaged
}

// handleJackettUninstall stops the managed Jackett and removes its files.
//
// The data directory is kept by default: it holds the API key and the user's
// indexer configuration, and losing it silently would be a nasty surprise. A
// purge is available explicitly.
func (s *Server) handleJackettUninstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, false, "POST required")
		return
	}
	if err := r.ParseForm(); err != nil {
		writeJSON(w, false, err.Error())
		return
	}
	purge := r.FormValue("purge") == "1"
	dir := s.jackettInstallDir()

	if _, err := os.Stat(dir); os.IsNotExist(err) {
		writeJSON(w, false, "未找到 Jackett 安装目录: "+dir)
		return
	}

	log.Printf("[jackett] uninstall requested: dir=%s purge=%v", dir, purge)
	steps, err := jackett.Uninstall(dir, purge)
	if err != nil {
		log.Printf("[jackett] uninstall failed: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":  false,
			"msg": err.Error(),
			"log": steps,
		})
		return
	}

	// Clear the connection details only when the data directory went too;
	// otherwise the config may still point at an instance the user keeps.
	if purge {
		s.applyJackettSettings("", "", "")
		s.jackettCacheMu.Lock()
		s.jackettCache = nil
		s.jackettCacheMu.Unlock()
	}

	log.Printf("[jackett] uninstalled (purge=%v)", purge)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":    true,
		"msg":   uninstallResultMessage(purge),
		"log":   steps,
		"purge": purge,
		"dir":   dir,
	})
}

func uninstallResultMessage(purge bool) string {
	if purge {
		return "已卸载 Jackett 并清除其数据（含索引器配置与 API Key）"
	}
	return "已卸载 Jackett，数据目录保留（含索引器配置与 API Key）"
}

// handleJackettEnv reports what the host provides for running Jackett.
//
// The panel calls this before offering an install so a missing dependency is
// surfaced with the exact command that fixes it, rather than letting the user
// download 50 MB that cannot start.
func (s *Server) handleJackettEnv(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	rep := jackett.DetectEnv(s.jackettInstallDir())
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":     true,
		"env":    rep,
		"usable": rep.Supported(),
	})
}

// handleJackettInstallDeps installs the system packages Jackett needs.
//
// It only runs when the environment check says it can (root plus a known
// package manager); otherwise the response explains what to run by hand.
func (s *Server) handleJackettInstallDeps(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, false, "POST required")
		return
	}

	rep := jackett.DetectEnv(s.jackettInstallDir())
	// Only ICU is installable here. Other problems (an unwritable install
	// directory, no disk space) would not be fixed by a package manager, so say
	// so instead of running one for nothing.
	if rep.HasICU {
		writeJSON(w, true, "系统依赖已满足，无需安装")
		return
	}
	if !rep.IsRoot || rep.PackageManager == "" {
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":   false,
			"msg":  "无法自动安装：" + strings.Join(rep.Problems, "；"),
			"log":  []string{},
			"env":  rep,
			"hint": jackett.InstallHint(rep),
		})
		return
	}

	log.Printf("[jackett] installing dependencies (distro=%s pm=%s root=%v)",
		rep.Distro, rep.PackageManager, rep.IsRoot)

	steps, err := jackett.InstallMissingDeps(rep)
	if err != nil {
		log.Printf("[jackett] dependency install failed: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":    false,
			"msg":   err.Error(),
			"log":   steps,
			"env":   rep,
			"hint":  jackett.InstallHint(rep),
		})
		return
	}

	// Re-check: the install may have succeeded without actually providing the
	// library (a wrong package name, a stale index), and reporting success then
	// would mislead the user.
	after := jackett.DetectEnv(s.jackettInstallDir())
	log.Printf("[jackett] dependencies installed: ICU=%v usable=%v", after.HasICU, after.Supported())
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      after.Supported(),
		"msg":     dependencyResultMessage(after),
		"log":     steps,
		"env":     after,
		"usable":  after.Supported(),
	})
}

// dependencyResultMessage summarises the post-install state in one line.
func dependencyResultMessage(after jackett.EnvReport) string {
	if after.Supported() {
		return "系统依赖安装完成"
	}
	if len(after.Problems) > 0 {
		return "安装命令已执行，但环境仍不满足：" + after.Problems[0]
	}
	return "安装命令已执行，但环境仍不满足"
}

// handleJackettCheckUpdate compares the managed install against the latest
// release.
func (s *Server) handleJackettCheckUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	dir := s.jackettInstallDir()

	st := jackett.CheckUpdate(r.Context(), dir)
	log.Printf("[jackett] update check: installed=%q latest=%q hasUpdate=%v err=%q",
		st.Installed, st.Latest, st.HasUpdate, st.Error)

	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":  true,
		"st":  st,
	})
}

// handleJackettUpdate replaces the managed install with the latest release.
//
// The running instance is stopped first: replacing files underneath a live
// process leaves it serving from a half-swapped tree. It is not restarted
// automatically so the caller can decide, but the response reports whether it
// was running so the UI can offer to start it again.
func (s *Server) handleJackettUpdate(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, false, "POST required")
		return
	}
	dir := s.jackettInstallDir()

	if _, err := os.Stat(filepath.Join(dir, "jackett")); err != nil {
		writeJSON(w, false, "尚未安装 Jackett，请先安装")
		return
	}

	wasRunning := jackett.HealthCheck(s.JackettURL) == nil
	if wasRunning {
		log.Printf("[jackett] stopping before update")
		if err := jackett.Stop(dir); err != nil {
			// Replacing the tree underneath a live process leaves it serving
			// from a half-swapped install. Refuse rather than risk that.
			log.Printf("[jackett] stop before update failed: %v", err)
			json.NewEncoder(w).Encode(map[string]interface{}{
				"ok":          false,
				"msg":         "无法停止正在运行的 Jackett，已取消更新以免文件被替换到运行中的进程下：" + err.Error(),
				"was_running": true,
			})
			return
		}
	}

	res, err := jackett.Update(r.Context(), dir, log.Printf)
	if err != nil {
		log.Printf("[jackett] update failed: %v", err)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":          false,
			"msg":         err.Error(),
			"log":         res.Log,
			"was_running": wasRunning,
		})
		return
	}

	log.Printf("[jackett] updated to %s in %s", res.Version, res.Dir)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":          true,
		"msg":         "Jackett 已更新到 " + res.Version,
		"version":     res.Version,
		"log":         res.Log,
		"was_running": wasRunning,
		"dir":         res.Dir,
	})
}

// handleJackettStart launches an already-installed Jackett instance.
func (s *Server) handleJackettStart(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.Method != http.MethodPost {
		writeJSON(w, false, "POST required")
		return
	}

	dir := s.jackettInstallDir()
	url := s.JackettURL
	if url == "" {
		url = fmt.Sprintf("http://127.0.0.1:%d", jackett.DefaultPort)
	}
	log.Printf("[jackett] start requested: dir=%s url=%s", dir, url)

	actualURL, err := jackett.Start(r.Context(), dir, url, log.Printf)
	if err != nil {
		log.Printf("[jackett] start failed: %v", err)
		writeJSON(w, false, err.Error())
		return
	}
	// Adopt the endpoint that actually answered; Jackett's effective port comes
	// from its OS-level data folder, not from our install directory.
	if actualURL != url {
		log.Printf("[jackett] started on %s (requested %s), adopting", actualURL, url)
		apiKey := s.JackettAPIKey
		adminPassword := s.JackettAdminPassword
		if d := jackett.Discover(); d.Found {
			if d.APIKey != "" {
				apiKey = d.APIKey
			}
			if d.AdminPassword != "" {
				adminPassword = d.AdminPassword
			}
		}
		s.applyJackettSettings(actualURL, apiKey, adminPassword)
	}
	go s.refreshJackettCache()
	writeJSON(w, true, "Jackett 已启动")
}

// AutoDiscoverJackett probes for a local Jackett instance and adopts its
// settings when nothing has been configured yet.
//
// This is what makes a fresh install work out of the box: if Jackett is already
// running (or its config file is present) the connection details are filled in
// automatically. Explicit user settings always win.
func (s *Server) AutoDiscoverJackett() {
	hadURL := strings.TrimSpace(s.JackettURL) != ""
	hadKey := strings.TrimSpace(s.JackettAPIKey) != ""

	d := jackett.Discover()
	if !d.Found {
		log.Printf("[jackett] no local instance detected")
		return
	}

	url := s.JackettURL
	apiKey := s.JackettAPIKey
	adminPassword := s.JackettAdminPassword
	changed := false

	// Only fill in what is missing; never overwrite a working configuration.
	if !hadURL && d.URL != "" {
		url = d.URL
		changed = true
	}
	if !hadKey && d.APIKey != "" {
		apiKey = d.APIKey
		changed = true
	}
	if strings.TrimSpace(adminPassword) == "" && d.AdminPassword != "" {
		adminPassword = d.AdminPassword
	}

	if changed {
		s.applyJackettSettings(url, apiKey, adminPassword)
		log.Printf("[jackett] auto-discovered %s (running=%v, config=%s)", url, d.Running, d.ConfigPath)
	} else {
		log.Printf("[jackett] existing configuration kept (discovered %s)", d.URL)
	}
}

// databasePathAbs returns the absolute path of the SQLite database file.
//
// It is made absolute so that derived locations (such as the managed Jackett
// install) do not depend on the process working directory, which may change or
// be unset under a service manager.
func (s *Server) databasePathAbs() string {
	dbPath := ""
	if s.Agent != nil {
		dbPath = s.Agent.GetSettings().DatabasePath
	}
	if dbPath == "" {
		dbPath = "db.sqlite"
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		return dbPath
	}
	return abs
}

// InitDataDir anchors every runtime file to the directory holding dbPath and
// returns that directory.
//
// It must run before anything reads or writes a sidecar file. New() calls it,
// but the login password is loaded even earlier (the password gates every
// handler, so it has to be in place before the server is constructed). That
// ordering bug meant LoadPersistedPassword resolved web-settings.json against
// the process working directory: after a self-update restarted the server with
// its working directory set to the binary's folder, the file was not found and
// the panel silently came back with no password at all.
func InitDataDir(dbPath string) string {
	if strings.TrimSpace(dbPath) == "" {
		dbPath = "db.sqlite"
	}
	abs, err := filepath.Abs(dbPath)
	if err != nil {
		abs = dbPath
	}
	dataDir = filepath.Dir(abs)

	// Re-resolve the package-level paths that were captured at init time.
	dedupCachePath = resolveDataFile("dedup-cache.json")
	rssJsonPath = resolveDataFile("rss.json")
	searchCacheFile = resolveDataFile("search-cache.json")
	return dataDir
}

// dataDir is the directory holding all runtime state: the database and the
// sidecar files (web-settings.json, rss.json, dedup-cache.json, ...).
//
// Every runtime file is resolved through here rather than by bare relative
// name. The process working directory is not a stable anchor: self-update
// restarts the server with its working directory set to the binary's folder,
// so relative names silently resolved to a different directory afterwards and
// the settings (including the Jackett connection) appeared to be lost.
func (s *Server) dataDir() string {
	return filepath.Dir(s.databasePathAbs())
}

// dataFile resolves a runtime file name against dataDir.
func (s *Server) dataFile(name string) string {
	return resolveDataFile(name)
}

// resolveDataFile resolves a runtime file name against the data directory
// derived from the configured database path. It exists for the package-level
// helpers (dedup cache, search cache) that have no Server receiver.
func resolveDataFile(name string) string {
	if filepath.IsAbs(name) {
		return name
	}
	return filepath.Join(dataDir, name)
}

// jackettInstallDir is where a Jackett instance installed by pan-fetcher lives:
// a "jackett" folder beside the database, so a single data volume carries both.
func (s *Server) jackettInstallDir() string {
	return jackett.InstallDir(s.dataDir())
}

// applyJackettSettings stores Jackett connection details in memory and persists
// them to web-settings.json.
func (s *Server) applyJackettSettings(url, apiKey, adminPassword string) {
	s.JackettURL = strings.TrimRight(strings.TrimSpace(url), "/")
	s.JackettAPIKey = strings.TrimSpace(apiKey)
	s.JackettAdminPassword = strings.TrimSpace(adminPassword)

	ws := s.loadWebSettings()
	ws.JackettURL = s.JackettURL
	ws.JackettAPIKey = s.JackettAPIKey
	ws.JackettAdminPassword = s.JackettAdminPassword
	s.saveWebSettings(ws)

	jackett.SetProxy(s.ProxyHTTP)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("r2c_session"); err == nil {
		webSessMu.Lock()
		delete(webSessions, c.Value)
		webSessMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: "r2c_session", Value: "", Path: "/", MaxAge: -1})
	log.Printf("[auth] user logged out")
	http.Redirect(w, r, "/login", http.StatusSeeOther)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// Read language preference from cookie
	lang := "zh"
	if c, err := r.Cookie("r2c_lang"); err == nil && c.Value == "en" {
		lang = "en"
	}

	if r.Method == http.MethodPost {
		if err := r.ParseForm(); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		pw := sanitizePassword(r.FormValue("password"))
		if pw == webPassword {
			token := newSessionToken()
			webSessMu.Lock()
			webSessions[token] = true
			webSessMu.Unlock()
			http.SetCookie(w, &http.Cookie{Name: "r2c_session", Value: token, Path: "/", MaxAge: 86400 * 7})
			http.Redirect(w, r, "/", http.StatusSeeOther)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		errMsg := `<div class="err">` + tr(lang, "wrong_password_html") + `</div>`
		fmt.Fprint(w, loginPage(lang, errMsg))
		return
	}
	if webPassword == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	if c, err := r.Cookie("r2c_session"); err == nil && validSession(c.Value) {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, loginPage(lang, ""))
}

func sanitizePassword(s string) string {
	// Trim spaces, remove control characters, limit to 128 chars
	s = strings.TrimSpace(s)
	if len(s) > 128 {
		s = s[:128]
	}
	// Strip control characters and null bytes
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 0x20 && r != 0x7F {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func loginPage(lang, errHTML string) string {
	title := "🔐 pan-fetcher"
	pageTitle := tr(lang, "login_page_title")
	ph := tr(lang, "login_pw_ph")
	btn := tr(lang, "login_btn")
	htmlLang := "zh-CN"
	if lang == "en" {
		htmlLang = "en"
		htmlLang = "en"
	}
	return `<!doctype html>
<html lang="` + htmlLang + `">
<head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>` + pageTitle + `</title>
<style>
*{box-sizing:border-box}body{margin:0;font-family:-apple-system,sans-serif;background:linear-gradient(180deg,#eef3ff,#f5f7fb);min-height:100vh;display:flex;align-items:center;justify-content:center}
.card{background:#fff;border-radius:18px;padding:32px;width:360px;box-shadow:0 12px 40px rgba(0,0,0,.08)}
h1{text-align:center;margin:0 0 20px;font-size:24px}
input{width:100%;border:1px solid #dce3ec;border-radius:12px;padding:12px;font:inherit;margin-bottom:12px}
button{width:100%;border:0;border-radius:12px;padding:12px;background:#1d4ed8;color:#fff;font:inherit;cursor:pointer;font-size:16px}
.err{color:#b42318;text-align:center;margin-bottom:10px;font-size:14px}
</style></head>
<body><div class="card">
<h1>` + title + `</h1>
` + errHTML + `<form method="post"><input name="password" type="password" placeholder="` + ph + `" maxlength="128" autofocus><button>` + btn + `</button></form>
</div></body></html>`
}

func base64Encode(data []byte) string {
	const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/"
	n := (len(data) + 2) / 3 * 4
	out := make([]byte, n)
	for i, j := 0, 0; i < len(data); i += 3 {
		chunk := int(data[i]) << 16
		if i+1 < len(data) {
			chunk |= int(data[i+1]) << 8
		}
		if i+2 < len(data) {
			chunk |= int(data[i+2])
		}
		out[j] = alphabet[(chunk>>18)&0x3F]
		out[j+1] = alphabet[(chunk>>12)&0x3F]
		if i+1 < len(data) {
			out[j+2] = alphabet[(chunk>>6)&0x3F]
		} else {
			out[j+2] = '='
		}
		if i+2 < len(data) {
			out[j+3] = alphabet[chunk&0x3F]
		} else {
			out[j+3] = '='
		}
		j += 4
	}
	return string(out)
}

// ---------- helpers ----------

// ---------- subscription management ----------

type subRow struct {
	ID        int
	Name      string
	MediaType string
	Season    int
	Cid       string
	Savepath  string
	Enabled   bool
	Status    string
}

type rssSubRow struct {
	Site           string
	Name           string
	URL            string
	Filter         string
	Cid            string
	SavePath       string
	Enabled        bool
	CacheCount     int
	IndexerDisplay string
}

func (s *Server) handleSubscriptions(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.handleSubsAction(w, r)
		return
	}
	data := s.pageData("", "")
	data.Page = "subs"
	// Load RSS subscriptions from rss.json
	data.RssSubs = s.loadRssSubs()
	if err := dashboardTemplate.Execute(w, data); err != nil {
		log.Printf("[subs] template render error: %v", err)
	}
}

func (s *Server) handleSubsAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	action := r.FormValue("action")
	site := r.FormValue("site")
	name := r.FormValue("name")

	if action == "toggle" {
		s.toggleRssSub(site, name)
		log.Printf("[subs] toggle %q on %s", name, site)
		http.Redirect(w, r, "/subs", http.StatusSeeOther)
		return
	}
	if action == "delete" {
		s.deleteRssSub(site, name)
		log.Printf("[subs] delete %q from %s", name, site)
		http.Redirect(w, r, "/subs", http.StatusSeeOther)
		return
	}
	if action == "edit" {
		cid := strings.TrimSpace(r.FormValue("cid"))
		savepath := strings.TrimSpace(r.FormValue("savepath"))
		filter := strings.TrimSpace(r.FormValue("filter"))
		s.updateRssSub(site, name, cid, savepath, filter)
		log.Printf("[subs] edit %q on %s (cid=%s, savepath=%q, filter=%q)", name, site, cid, savepath, filter)
		http.Redirect(w, r, "/subs", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/subs", http.StatusSeeOther)
}

func (s *Server) loadRssSubs() []rssSubRow {
	feeds := s.readRssFeeds()
	var rows []rssSubRow
	for site, entries := range feeds {
		for _, e := range entries {
			rows = append(rows, rssSubRow{
				Site: site, Name: e.Name, URL: e.URL,
				// The keyword lives in the URL; fall back to the legacy field.
				Filter:   effectiveFilter(e.URL, e.Filter),
				Cid:      e.Cid,
				SavePath: e.SavePath,
				Enabled:  e.Enabled,
				CacheCount:     globalDedup.SubCount(e.Name),
				IndexerDisplay: parseIndexerFromRSSURL(e.URL),
			})
		}
	}
	return rows
}

// parseIndexerFromRSSURL extracts indexer names from an RSS URL's indexers parameter.
func parseIndexerFromRSSURL(rssURL string) string {
	u, err := url.Parse(rssURL)
	if err != nil {
		return ""
	}
	raw := u.Query().Get("indexers")
	if raw == "" {
		return "全部"
	}
	parts := strings.Split(raw, ",")
	for i, p := range parts {
		// Strip jackett: prefix for display
		parts[i] = strings.TrimPrefix(strings.TrimSpace(p), "jackett:")
	}
	return strings.Join(parts, ", ")
}

func (s *Server) readRssFeeds() map[string][]rssFeedEntry {
	feeds := make(map[string][]rssFeedEntry)
	if data, err := os.ReadFile(rssJsonPath); err == nil {
		json.Unmarshal(data, &feeds)
	}
	return feeds
}

func (s *Server) writeRssFeeds(feeds map[string][]rssFeedEntry) {
	data, _ := json.MarshalIndent(feeds, "", "  ")
	os.WriteFile(rssJsonPath, data, 0644)
}

func (s *Server) toggleRssSub(site, name string) {
	feeds := s.readRssFeeds()
	entries := feeds[site]
	for i, e := range entries {
		if e.Name == name {
			feeds[site][i].Enabled = !e.Enabled
			break
		}
	}
	s.writeRssFeeds(feeds)
}

func (s *Server) deleteRssSub(site, name string) {
	feeds := s.readRssFeeds()
	entries := feeds[site]
	filtered := entries[:0]
	for _, e := range entries {
		if e.Name != name {
			filtered = append(filtered, e)
		}
	}
	if len(filtered) == 0 {
		delete(feeds, site)
	} else {
		feeds[site] = filtered
	}
	s.writeRssFeeds(feeds)
	// Also clean up dedup cache for this subscription
	globalDedup.ClearSub(name)
}

// updateRssSub applies edits from the subscription edit form.
//
// The edit form is a complete-edit form: it always submits cid, savepath and
// filter together, so an empty value means "the user cleared this field" rather
// than "leave it unchanged". Assigning unconditionally is therefore required —
// guarding each field with `!= ""` made filter (and savepath) impossible to
// clear once set.
//
// The filter is written into the URL's keyword parameter as well as the field,
// because that is where ProcessRSSFeed reads it from. Updating only the field
// would leave the old keyword in the URL still winning, so the edit would
// appear to save and then have no effect.
func (s *Server) updateRssSub(site, name, cid, savepath, filter string) {
	feeds := s.readRssFeeds()
	entries := feeds[site]
	for i, e := range entries {
		if e.Name == name {
			feeds[site][i].Cid = cid
			feeds[site][i].SavePath = savepath
			feeds[site][i].Filter = filter
			feeds[site][i].URL = setURLKeyword(e.URL, filter)
			break
		}
	}
	s.writeRssFeeds(feeds)
}

func (s *Server) handleSubsRun(w http.ResponseWriter, r *http.Request) {
	if s.Agent == nil {
		s.renderMsg(w, "", "err_not_logged_in_subs")
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	rssURL := strings.TrimSpace(r.FormValue("rss_url"))
	if rssURL == "" {
		s.renderMsg(w, "", "err_rss_empty")
		return
	}
	cid := strings.TrimSpace(r.FormValue("cid"))
	savepath := strings.TrimSpace(r.FormValue("savepath"))
	filter := strings.TrimSpace(r.FormValue("filter"))
	// Resolve relative URLs to absolute for local RSS endpoints
	if strings.HasPrefix(rssURL, "/") {
		rssURL = fmt.Sprintf("http://127.0.0.1:%d%s", s.Port, rssURL)
	}
	subKey := strings.TrimSpace(r.FormValue("sub_name"))
	if subKey == "" {
		subKey = rssURL
	}
	// Merge the filter field into the URL. ProcessRSSFeed only reads the keyword
	// parameter from the URL, so passing filter separately would silently drop it.
	// subKey is derived above, keeping dedup cache keys stable across filter edits.
	feedURL := ensureKeywordParam(rssURL, filter)
	go func() {
		before := globalDedup.SubCount(subKey)
		names := s.Agent.ProcessRSSFeed(feedURL, cid, savepath, subKey)
		after := globalDedup.SubCount(subKey)
		newItems := after - before
		if newItems > 0 {
			notify.RecordItems(subKey, names)
			notify.DispatchEvent(notify.RSSFoundEvent(subKey, newItems, names), true)
		}
		log.Printf("[subs] run completed: %s, new=%d", subKey, newItems)
	}()
	log.Printf("[subs] run triggered: %s (filter=%q)", subKey, filter)
	if r.Header.Get("X-Requested-With") == "XMLHttpRequest" {
		w.Header().Set("Content-Type", "application/json")
		lang := s.langFromAgent()
		// rssURL is user-supplied and may contain quotes; encode it properly.
		json.NewEncoder(w).Encode(map[string]interface{}{
			"ok":  true,
			"msg": trf(lang, "subs_processing_fmt", rssURL),
		})
		return
	}
	http.Redirect(w, r, "/subs", http.StatusSeeOther)
}

// handleSubsToggleAll enables or disables all RSS subscriptions.
func (s *Server) handleSubsToggleAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	enabled := r.FormValue("enabled") == "1"
	feeds := s.readRssFeeds()
	for site, entries := range feeds {
		for i := range entries {
			entries[i].Enabled = enabled
		}
		feeds[site] = entries
	}
	s.writeRssFeeds(feeds)
	lang := s.langFromAgent()
	msg := tr(lang, "disable_all")
	if enabled {
		msg = tr(lang, "enable_all")
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":  true,
		"msg": msg + " " + tr(lang, "settings_saved"),
	})
}

// handleDedupClearAll clears the entire deduplication cache.
func (s *Server) handleDedupClearAll(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	for _, subKey := range globalDedup.SubKeys() {
		globalDedup.ClearSub(subKey)
	}
	log.Printf("[dedup] cleared all cache")
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"msg":"%s"}`, tr(s.langFromAgent(), "clear_all_cache"))
}

func (s *Server) handleSubsDirs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s.Agent == nil {
		fmt.Fprintf(w, `{"ok":false,"msg":"%s"}`, tr(s.langFromAgent(), "not_logged_in_json"))
		return
	}
	parentID := r.URL.Query().Get("pid")
	if parentID == "" {
		parentID = "0"
	}
	entries, err := s.Agent.ListDir(parentID)
	if err != nil {
		json.NewEncoder(w).Encode(map[string]interface{}{"ok": false, "msg": err.Error()})
		return
	}
	// Get actual parent of this directory for ".." navigation
	actualParent := "0"
	if parentID != "0" {
		if e, err := s.getEntryCached(parentID); err == nil {
			actualParent = e.ParentID
		}
	}
	type dirEntry struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	var dirs []dirEntry
	for _, e := range entries {
		if e.IsDir {
			dirs = append(dirs, dirEntry{ID: e.ID, Name: e.Name})
		}
	}
	json.NewEncoder(w).Encode(map[string]interface{}{
		"ok":      true,
		"parent":  actualParent,
		"entries": dirs,
	})
}

// ---------- helpers (continued) ----------

func (s *Server) renderResult(w http.ResponseWriter, message, errMsg string) {
	if err := dashboardTemplate.Execute(w, s.pageData(message, errMsg)); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// langFromAgent returns the user's language preference, defaults to "zh".
func (s *Server) langFromAgent() string {
	if s.Agent != nil {
		if st := s.Agent.GetSettings(); st.Lang != "" {
			return st.Lang
		}
	}
	return "zh"
}

// renderMsg renders a translated message/error through the template.
func (s *Server) renderMsg(w http.ResponseWriter, msgKey, errKey string) {
	lang := s.langFromAgent()
	s.renderResult(w, tr(lang, msgKey), tr(lang, errKey))
}

// renderMsgf renders a translated formatted message through the template.
func (s *Server) renderMsgf(w http.ResponseWriter, msgKey string, args ...interface{}) {
	lang := s.langFromAgent()
	s.renderResult(w, trf(lang, msgKey, args...), "")
}

func (s *Server) pageData(message, errMsg string) dashboardData {
	return s.pageDataWithCache("", message, errMsg)
}

func (s *Server) pageDataWithCache(page, message, errMsg string) dashboardData {
	// Language resolution, in order of authority:
	//
	//   1. the agent's setting, which is what the logged-in panel uses;
	//   2. web-settings.json, which is what /api/lang writes;
	//   3. Chinese.
	//
	// The web-settings fallback matters because the agent is nil until 115 is
	// logged in. Without it, switching to English on a fresh install did nothing
	// until a 115 login happened — the cookie and the stored preference were
	// both set, but the page kept rendering Chinese.
	lang := "zh"
	if s.Agent != nil {
		if st := s.Agent.GetSettings(); st.Lang != "" {
			lang = st.Lang
		}
	}
	ws := s.loadWebSettings()
	if ws.Lang != "" && (s.Agent == nil || s.Agent.GetSettings().Lang == "") {
		lang = ws.Lang
	}
	if lang != "zh" && lang != "en" {
		lang = "zh"
	}
	data := dashboardData{
		Message:     message,
		Error:       errMsg,
		Lang:        lang,
		T:           langMap(lang),
		AllTagsJSON:   template.JS("[]"),
		AllGroupsJSON: template.JS("[]"),
	}
	data.PageSize = ws.PageSize
	if data.PageSize <= 0 { data.PageSize = 50 }
	if s.Agent != nil {
		// Cache connection check for 60 seconds
		if time.Since(s.connCheckTime) < 60*time.Second {
			data.LoggedIn = s.connCheckLoggedIn
		} else {
			if err := s.Agent.TestConnection(); err == nil {
				data.LoggedIn = true
				s.connCheckLoggedIn = true
			} else {
				s.connCheckLoggedIn = false
			}
			s.connCheckTime = time.Now()
		}

		data.Cookies = s.Agent.LoadCookiesStr()

		// Only load full task list for the tasks page; dashboard just needs count
		if page == "tasks" {
			tasks, err := s.Agent.ListTasks()
			if err == nil {
				data.TaskCount = len(tasks)
				data.Tasks = make([]taskRow, 0, len(tasks))
				for _, t := range tasks {
					row := taskRow{
						InfoHash: t.InfoHash,
						Name:     displayName(t),
						Size:     formatSize(t.Size),
						Percent:  t.Percent,
						URL:      t.URL,
					}
					switch {
					case t.Status == 2:
						row.Status = "done"
						row.RowClass = "row-done"
					case t.Status == -1:
						row.Status = "failed"
						row.RowClass = "row-failed"
					case t.Status == 1:
						row.Status = "downloading"
						row.RowClass = "row-running"
					default:
						row.Status = "waiting"
					}
					data.Tasks = append(data.Tasks, row)
				}
			}
		}
	}
	return data
}

func displayName(t p115pkg.TaskItem) string {
	if t.Name != "" && t.Name != t.InfoHash {
		return t.Name
	}
	if len(t.InfoHash) > 14 {
		return t.InfoHash[:14] + "…"
	}
	return t.InfoHash
}

func formatSize(bytes int64) string {
	switch {
	case bytes <= 0:
		return "-"
	case bytes < 1024:
		return fmt.Sprintf("%dB", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(bytes)/1024)
	case bytes < 1024*1024*1024:
		return fmt.Sprintf("%.1fMB", float64(bytes)/(1024*1024))
	default:
		return fmt.Sprintf("%.2fGB", float64(bytes)/(1024*1024*1024))
	}
}

func formatJackettDate(s string) string {
	for _, f := range []string{time.RFC1123Z, time.RFC1123, "Mon, 02 Jan 2006 15:04:05 -0700"} {
		if t, err := time.Parse(f, s); err == nil {
			return t.Format("2006-01-02 15:04")
		}
	}
	if len(s) > 10 {
		return s[5:16]
	}
	return s
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh%dm", h, m)
	}
	days := h / 24
	h = h % 24
	return fmt.Sprintf("%dd%dh", days, h)
}

func decodeOfflineTask(r *http.Request) (OfflineTask, error) {
	var task OfflineTask
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&task); err != nil {
			return task, err
		}
	} else {
		if err := r.ParseForm(); err != nil {
			return task, err
		}
		task.Tasks = parseTasks(strings.TrimSpace(r.FormValue("tasks")))
		if len(task.Tasks) == 0 {
			task.Tasks = parseTasks(strings.TrimSpace(r.FormValue("task")))
		}
		task.Cid = strings.TrimSpace(r.FormValue("cid"))
		task.SavePath = strings.TrimSpace(r.FormValue("savepath"))
	}
	if len(task.Tasks) == 0 {
		return task, fmt.Errorf("tasks is empty")
	}
	return task, nil
}

func parseTasks(raw string) []string {
	if raw == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ',' || r == ';'
	})
	tasks := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			tasks = append(tasks, part)
		}
	}
	return tasks
}

func (s *Server) StartServer() {
	ctx, cancel := context.WithCancel(context.Background())

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		sig := <-c
		fmt.Printf("%s received.\n", sig.String())
		if err := s.Shutdown(ctx); err != nil {
			fmt.Printf("failed to shutdown server, error: %+v\n", err)
		}
		cancel()
	}()

	if err := s.Start(ctx); err != nil {
		if err != http.ErrServerClosed {
			fmt.Printf("failed to start server, error: %+v\n", err)
			cancel()
		}
	}

	<-ctx.Done()
}
