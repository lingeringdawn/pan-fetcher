package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestJackettErrorJSONIsEscaped is the regression test for the add-library
// dialog failing with:
//
//	Expected ',' or '}' after property value in JSON at position 43
//
// The handler built its response with fmt.Fprintf and interpolated err.Error()
// straight into the JSON string. Jackett errors routinely contain double quotes
// (e.g. `Get "http://127.0.0.1:9117/...": context deadline exceeded`), which
// terminated the JSON string early and made the body unparsable. The UI then
// reported a JSON syntax error instead of the real failure.
func TestJackettErrorJSONIsEscaped(t *testing.T) {
	s := &Server{Port: 8115}
	mux := http.NewServeMux()
	s.registerRoutes(mux)

	// Point Jackett at an address that cannot answer, so ListAllIndexers fails
	// with an error that contains quotes — exactly the shape that broke the
	// response before.
	s.JackettURL = "http://127.0.0.1:1"
	s.JackettAPIKey = "irrelevant"

	req := httptest.NewRequest("GET", "/indexers/jackett/all", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if body == "" {
		t.Fatal("empty response body")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
	if ok, _ := parsed["ok"].(bool); ok {
		t.Errorf("expected ok=false for an unreachable Jackett, body: %s", body)
	}
	msg, _ := parsed["msg"].(string)
	if msg == "" {
		t.Errorf("expected a non-empty msg, body: %s", body)
	}
	// The message must survive intact, quotes included.
	if !strings.Contains(msg, "http") {
		t.Errorf("error detail looks truncated: %q", msg)
	}
}

// TestJackettAddErrorJSONIsEscaped covers the same defect on the POST path that
// actually performs the add.
func TestJackettAddErrorJSONIsEscaped(t *testing.T) {
	s := &Server{Port: 8115}
	mux := http.NewServeMux()
	s.registerRoutes(mux)
	s.JackettURL = "http://127.0.0.1:1"
	s.JackettAPIKey = "irrelevant"

	req := httptest.NewRequest("POST", "/indexers", strings.NewReader("action=jk_add_to_jackett&id=dmhy"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("response is not valid JSON: %v\nbody: %s", err, body)
	}
	if ok, _ := parsed["ok"].(bool); ok {
		t.Errorf("expected ok=false, body: %s", body)
	}
}

// TestJSONHelpersEscapeQuotes documents the shared behaviour every JSON
// response now relies on: encoding, not string interpolation.
func TestJSONHelpersEscapeQuotes(t *testing.T) {
	rec := httptest.NewRecorder()
	writeJSON(rec, false, `Get "http://x": failed`)

	var parsed map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("writeJSON produced invalid JSON: %v", err)
	}
	if got, _ := parsed["msg"].(string); got != `Get "http://x": failed` {
		t.Errorf("message round-trip failed: %q", got)
	}
}
