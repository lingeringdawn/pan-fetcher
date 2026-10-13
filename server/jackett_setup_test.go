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

// newJackettTestServer builds a Server whose web-settings.json is isolated in a
// temp directory, so tests never touch the real configuration.
func newJackettTestServer(t *testing.T, settings webSettings) (*Server, *http.ServeMux, string) {
	t.Helper()

	dir := t.TempDir()
	settingsPath := filepath.Join(dir, "web-settings.json")
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		t.Fatalf("marshal settings: %v", err)
	}
	if err := os.WriteFile(settingsPath, data, 0o644); err != nil {
		t.Fatalf("write settings: %v", err)
	}

	// loadWebSettings/saveWebSettings use a relative filename, so run from the
	// temp directory for the duration of the test.
	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldWD) })

	fake := &fakeServerAgent{
		userInfo: elevengo.UserInfo{Id: 1, Name: "tester"},
	}
	srv := &Server{
		Agent:     fake,
		Port:      8115,
		startTime: time.Now(),
		JackettURL:           settings.JackettURL,
		JackettAPIKey:        settings.JackettAPIKey,
		JackettAdminPassword: settings.JackettAdminPassword,
	}
	mux := http.NewServeMux()
	srv.registerRoutes(mux)
	return srv, mux, settingsPath
}

func readSettingsFile(t *testing.T, path string) webSettings {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	var ws webSettings
	if err := json.Unmarshal(data, &ws); err != nil {
		t.Fatalf("unmarshal settings: %v", err)
	}
	return ws
}

// TestJackettConfigured covers the gate that every search entry point relies on.
func TestJackettConfigured(t *testing.T) {
	cases := []struct {
		url, key string
		want     bool
	}{
		{"http://127.0.0.1:9117", "key", true},
		{"http://127.0.0.1:9117", "", false},
		{"", "key", false},
		{"", "", false},
		{"   ", "key", false},   // whitespace is not a valid URL
		{"http://x", "  ", false},
	}
	for _, tc := range cases {
		s := &Server{JackettURL: tc.url, JackettAPIKey: tc.key}
		if got := s.jackettConfigured(); got != tc.want {
			t.Errorf("jackettConfigured(url=%q, key=%q) = %v, want %v", tc.url, tc.key, got, tc.want)
		}
	}
}

// TestSearchRequiresJackett verifies a search POST without Jackett configured
// reports an error instead of silently returning nothing.
func TestSearchRequiresJackett(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	form := url.Values{"q": {"TARI TARI"}}
	req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	// The page must carry the "not configured" notice rather than a bare form.
	if !strings.Contains(rec.Body.String(), "Jackett") {
		t.Errorf("expected a Jackett configuration hint in the response")
	}
}

// TestRssSearchRequiresJackett mirrors the above for the RSS endpoint.
func TestRssSearchRequiresJackett(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	req := httptest.NewRequest(http.MethodGet, "/rss/search?q=foo", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 without Jackett, got %d", rec.Code)
	}
}

// TestSettingsPartialPostPreservesFields is the regression guard for the Jackett
// discovery flow: it POSTs only the fields it discovered, so everything else
// must survive untouched.
func TestSettingsPartialPostPreservesFields(t *testing.T) {
	srv, mux, settingsPath := newJackettTestServer(t, webSettings{
		Lang:                 "zh",
		JackettURL:           "http://old.example:9117",
		JackettAPIKey:        "oldkey",
		JackettAdminPassword: "oldpass",
		TMDBAPIKey:           "tmdbkey",
		PageSize:             42,
		SubsInterval:         77,
	})

	// Only jackett_url is submitted, mirroring a partial discovery result.
	form := url.Values{"jackett_url": {"http://new.example:9117"}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	got := readSettingsFile(t, settingsPath)
	if got.JackettURL != "http://new.example:9117" {
		t.Errorf("submitted field not applied: %q", got.JackettURL)
	}
	if got.JackettAPIKey != "oldkey" {
		t.Errorf("API key was clobbered by a partial POST: %q", got.JackettAPIKey)
	}
	if got.JackettAdminPassword != "oldpass" {
		t.Errorf("admin password was clobbered: %q", got.JackettAdminPassword)
	}
	if got.TMDBAPIKey != "tmdbkey" {
		t.Errorf("TMDB key was clobbered: %q", got.TMDBAPIKey)
	}
	if got.PageSize != 42 {
		t.Errorf("page size was clobbered: %d", got.PageSize)
	}
	if got.SubsInterval != 77 {
		t.Errorf("subs interval was clobbered: %d", got.SubsInterval)
	}
	if srv.JackettURL != "http://new.example:9117" {
		t.Errorf("in-memory URL not updated: %q", srv.JackettURL)
	}
}

// TestSettingsFullPostClearsFields confirms an explicit empty value still clears
// a setting, so the "absent vs empty" distinction does not break manual edits.
func TestSettingsFullPostClearsFields(t *testing.T) {
	_, mux, settingsPath := newJackettTestServer(t, webSettings{
		Lang:          "zh",
		JackettURL:    "http://old.example:9117",
		JackettAPIKey: "oldkey",
	})

	form := url.Values{"jackett_url": {""}, "jackett_apikey": {""}}
	req := httptest.NewRequest(http.MethodPost, "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	got := readSettingsFile(t, settingsPath)
	if got.JackettURL != "" {
		t.Errorf("URL should be cleared, got %q", got.JackettURL)
	}
	if got.JackettAPIKey != "" {
		t.Errorf("API key should be cleared, got %q", got.JackettAPIKey)
	}
}

// TestJackettDiscoverEndpoint verifies the discovery endpoint always answers
// with a well-formed JSON payload, even with no Jackett present.
func TestJackettDiscoverEndpoint(t *testing.T) {
	// Neutralise any real installation on this machine.
	t.Setenv("JACKETT_URL", "")
	t.Setenv("JACKETT_APIKEY", "")
	t.Setenv("ProgramData", t.TempDir())
	t.Setenv("LOCALAPPDATA", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	req := httptest.NewRequest(http.MethodGet, "/indexers/jackett/discover", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v (body=%s)", err, rec.Body.String())
	}
	if ok, _ := resp["ok"].(bool); !ok {
		t.Errorf("expected ok=true, got %v", resp["ok"])
	}
	for _, key := range []string{"found", "running", "url", "apikey", "install_dir"} {
		if _, present := resp[key]; !present {
			t.Errorf("response missing key %q", key)
		}
	}
}

// TestJackettInstallRejectsGET keeps the destructive install endpoint POST-only.
func TestJackettInstallRejectsGET(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	req := httptest.NewRequest(http.MethodGet, "/indexers/jackett/install", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if ok, _ := resp["ok"].(bool); ok {
		t.Error("GET must not trigger an install")
	}
}

// TestJackettStartWithoutInstall covers the error path when Jackett has never
// been installed into the managed directory.
func TestJackettStartWithoutInstall(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	req := httptest.NewRequest(http.MethodPost, "/indexers/jackett/start", nil)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	var resp map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("response is not valid JSON: %v", err)
	}
	if ok, _ := resp["ok"].(bool); ok {
		t.Error("start must fail when Jackett is not installed")
	}
}

// TestAutoDiscoverKeepsExistingConfig ensures startup discovery never overwrites
// a working configuration.
func TestAutoDiscoverKeepsExistingConfig(t *testing.T) {
	srv, _, settingsPath := newJackettTestServer(t, webSettings{
		Lang:          "zh",
		JackettURL:    "http://configured.example:9117",
		JackettAPIKey: "configuredkey",
	})

	srv.AutoDiscoverJackett()

	got := readSettingsFile(t, settingsPath)
	if got.JackettURL != "http://configured.example:9117" {
		t.Errorf("existing URL overwritten: %q", got.JackettURL)
	}
	if got.JackettAPIKey != "configuredkey" {
		t.Errorf("existing API key overwritten: %q", got.JackettAPIKey)
	}
}

// TestIndexersPageRenders checks the reworked indexers page still renders with
// Jackett unconfigured, showing the setup panel instead of a broken table.
func TestIndexersPageRenders(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	req := httptest.NewRequest(http.MethodGet, "/indexers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("unexpected status: %d", rec.Code)
	}
	body := rec.Body.String()
	// The setup panel and its actions must be present.
	for _, want := range []string{"jk-setup-body", "jkDiscover()", "jkInstall()"} {
		if !strings.Contains(body, want) {
			t.Errorf("indexers page missing %q", want)
		}
	}
	// The removed local-indexer UI must be gone.
	for _, gone := range []string{"idx-library", "newIdx()", "activateSelected()", "editIdx("} {
		if strings.Contains(body, gone) {
			t.Errorf("indexers page still references removed local indexer UI: %q", gone)
		}
	}
}

// TestLocalIndexerRoutesRemoved pins the removal of the Cardigann-based routes.
func TestLocalIndexerRoutesRemoved(t *testing.T) {
	_, mux, _ := newJackettTestServer(t, webSettings{Lang: "zh"})

	for _, path := range []string{"/indexers/test", "/indexers/testall", "/indexers/edit", "/indexers/delete", "/indexers/login"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, req)
		// An unregistered path falls through to the dashboard handler ("/"),
		// which returns 200; what matters is that it is not the old JSON API.
		if rec.Code == http.StatusOK && strings.Contains(rec.Header().Get("Content-Type"), "application/json") {
			t.Errorf("%s still served by a JSON handler", path)
		}
	}
}
