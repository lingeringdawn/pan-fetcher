package server

import (
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
)

// postSettings drives handleSettings directly.
//
// The route is wrapped in authCheck, and a test request has no session, so going
// through the mux would be redirected to the login page before the handler ever
// runs. These tests are about what the handler persists, so they call it
// directly and skip authentication.
func postSettings(t *testing.T, srv *Server, form url.Values) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/settings", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	srv.handleSettings(rec, req)
	return rec
}

// TestPartialPostKeepsPassword is the regression test for the panel losing its
// password.
//
// The settings handler read web_password with FormValue, which returns "" both
// for an empty field and for a field that was never submitted. A partial POST —
// the Jackett discovery flow posts only the fields it discovered — therefore
// looked like "clear the password", and the panel came back with authentication
// disabled. The log line was "[settings] updated: web_password=cleared".
func TestPartialPostKeepsPassword(t *testing.T) {
	saved := webPassword
	t.Cleanup(func() { webPassword = saved })

	webPassword = "original-secret"
	withTempWebSettings(t, `{"web_password":"original-secret"}`)

	postSettings(t, &Server{}, url.Values{"jackett_url": {"http://127.0.0.1:9117"}})

	if webPassword != "original-secret" {
		t.Errorf("a partial POST must not change the password: got %q", webPassword)
	}
	if got := (&Server{}).loadWebSettings().WebPassword; got != "original-secret" {
		t.Errorf("stored password = %q, want it unchanged", got)
	}
}

// TestExplicitEmptyPasswordClears verifies the deliberate clear path still works.
//
// Saving an empty password field is how the user disables authentication; the
// guard must not make that impossible.
func TestExplicitEmptyPasswordClears(t *testing.T) {
	saved := webPassword
	t.Cleanup(func() { webPassword = saved })

	webPassword = "original-secret"
	withTempWebSettings(t, `{"web_password":"original-secret"}`)

	postSettings(t, &Server{}, url.Values{"web_password": {""}})

	if webPassword != "" {
		t.Errorf("an explicitly empty password should clear it, got %q", webPassword)
	}
	if got := (&Server{}).loadWebSettings().WebPassword; got != "" {
		t.Errorf("stored password = %q, want cleared", got)
	}
}

// TestPartialPostKeepsTimezone covers the same class of bug for another
// destructive field: a timezone left out of a partial POST must not be reset.
func TestPartialPostKeepsTimezone(t *testing.T) {
	withTempWebSettings(t, `{"timezone":"Asia/Shanghai"}`)

	postSettings(t, &Server{}, url.Values{"jackett_apikey": {"abc"}})

	if got := (&Server{}).loadWebSettings().Timezone; got != "Asia/Shanghai" {
		t.Errorf("timezone was cleared by a partial POST: %q", got)
	}
}

// TestCheckboxesNeedFormMarker verifies the checkbox fields are only updated when
// the full settings form was submitted.
//
// An unchecked checkbox is absent from the POST body, so without the marker a
// partial POST would read "" and silently turn the option off.
func TestCheckboxesNeedFormMarker(t *testing.T) {
	withTempWebSettings(t, `{"notify_log":true,"auto_update":true}`)

	// Partial POST: no marker, so the checkboxes must keep their values.
	postSettings(t, &Server{}, url.Values{"jackett_url": {"http://127.0.0.1:9117"}})
	ws := (&Server{}).loadWebSettings()
	if !ws.NotifyLog || !ws.AutoUpdate {
		t.Errorf("partial POST turned the checkboxes off: notify_log=%v auto_update=%v", ws.NotifyLog, ws.AutoUpdate)
	}

	// Full form with the marker and the boxes unchecked: they turn off.
	postSettings(t, &Server{}, url.Values{"settings_submitted": {"1"}})
	ws = (&Server{}).loadWebSettings()
	if ws.NotifyLog || ws.AutoUpdate {
		t.Errorf("a full form with unchecked boxes should turn them off: notify_log=%v auto_update=%v", ws.NotifyLog, ws.AutoUpdate)
	}
}

// TestSettingsFormCarriesMarker verifies the template actually submits the
// marker, otherwise the checkbox guard would freeze those settings forever.
func TestSettingsFormCarriesMarker(t *testing.T) {
	src := readSource(t, templateSourceFile)

	i := strings.Index(src, `<form action="/settings" method="post">`)
	if i < 0 {
		t.Fatal("settings form not found")
	}
	end := i + 600
	if end > len(src) {
		end = len(src)
	}
	if !strings.Contains(src[i:end], `name="settings_submitted"`) {
		t.Error("the settings form must submit settings_submitted so unchecked boxes can be distinguished from absent fields")
	}
}

// TestDestructiveFieldsAreGuarded scans the settings handler for fields that are
// written straight from FormValue.
//
// Those fields treat an absent value as "clear it", so any partial POST wipes
// them. The check lists the ones that still do.
func TestDestructiveFieldsAreGuarded(t *testing.T) {
	src := readSource(t, "server.go")

	start := strings.Index(src, "has := func(name string) bool")
	if start < 0 {
		t.Fatal("settings handler not found")
	}
	end := strings.Index(src[start:], "// GET: load saved settings")
	if end < 0 {
		t.Fatal("could not find the end of the settings POST handler")
	}
	seg := src[start : start+end]

	destructive := []string{
		"web_password", "jackett_url", "jackett_apikey", "jackett_admin_password",
		"jackett_mode", "tmdb_apikey", "proxy_http", "timezone",
	}

	for _, name := range destructive {
		needle := `FormValue("` + name + `")`
		idx := strings.Index(seg, needle)
		if idx < 0 {
			continue
		}
		lineStart := strings.LastIndex(seg[:idx], "\n")
		prevStart := strings.LastIndex(seg[:lineStart], "\n")
		window := seg[prevStart+1 : idx+len(needle)]
		if !strings.Contains(window, `has("`+name+`")`) {
			t.Errorf("%s is read without a has() guard, so a partial POST clears it", name)
		}
	}
}

// TestWeworkWebhookIgnoresEmpty verifies the webhook field keeps its own guard.
//
// It uses "ignore an empty value" rather than has(), because the input box is
// gone from the page: a client that still posts the old form with a blank field
// must not wipe a configured destination.
func TestWeworkWebhookIgnoresEmpty(t *testing.T) {
	src := readSource(t, "server.go")

	i := strings.Index(src, `FormValue("wework_webhook")`)
	if i < 0 {
		t.Skip("the legacy webhook field is no longer read")
	}
	lineStart := strings.LastIndex(src[:i], "\n")
	line := src[lineStart : strings.Index(src[i:], "\n")+i]
	if !strings.Contains(line, `v != ""`) {
		t.Errorf("the webhook field must ignore an empty value, otherwise an old client wipes it: %s", strings.TrimSpace(line))
	}
}

// withTempWebSettings points the settings file at a temp directory seeded with
// body, so a handler run reads and writes an isolated file.
func withTempWebSettings(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/web-settings.json", []byte(body), 0o600); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
	savedDir := dataDir
	dataDir = dir
	t.Cleanup(func() { dataDir = savedDir })
}
