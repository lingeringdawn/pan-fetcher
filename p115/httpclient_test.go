package p115

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newCountingServer records every request and the body each one delivered.
func newCountingServer(t *testing.T, status int) (*httptest.Server, *int, *[]string) {
	t.Helper()
	calls := 0
	bodies := []string{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		data, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(data))
		w.WriteHeader(status)
		w.Write([]byte("<html>gateway</html>"))
	}))
	t.Cleanup(srv.Close)
	return srv, &calls, &bodies
}

// TestAPIClientPassesErrorStatusThrough is the regression test for a crash.
//
// The SDK's Get/Post shadow the error from send, so a transport error becomes
// (nil body, nil error) and CallApi hands that nil body to spec.Parse, which
// panics in encoding/json. Turning a 502 into an error here made that path
// reachable and killed the process on startup.
//
// The client must therefore return the response untouched and let the caller deal
// with a body it cannot decrypt.
func TestAPIClientPassesErrorStatusThrough(t *testing.T) {
	for _, status := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		srv, _, _ := newCountingServer(t, status)
		c := newAPIClient(newTransportClient())

		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("data=x"))
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("status %d must not be converted to an error (it crashes the SDK): %v", status, err)
		}
		if resp == nil {
			t.Fatalf("status %d: response must not be nil", status)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != status {
			t.Errorf("status = %d, want %d", resp.StatusCode, status)
		}
	}
}

// TestAPIClientPassesThroughSuccess verifies a healthy response is untouched.
func TestAPIClientPassesThroughSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"state":true}`))
	}))
	defer srv.Close()

	c := newAPIClient(newTransportClient())
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("data=x"))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d", resp.StatusCode)
	}
}

// TestAPIClientReplaysBody verifies the body survives repeated attempts on the
// same request.
//
// The SDK retries a timed-out request by calling Do again on the same
// *http.Request. Its body is a one-shot reader, so without this the retry sends
// nothing and fails with "ContentLength=N with Body length 0" — hiding the
// timeout that prompted it.
func TestAPIClientReplaysBody(t *testing.T) {
	const payload = "data=encrypted-blob-content"
	srv, calls, bodies := newCountingServer(t, http.StatusOK)

	c := newAPIClient(newTransportClient())
	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader(payload))
	req.ContentLength = int64(len(payload))

	for i := 0; i < 3; i++ {
		resp, err := c.Do(req)
		if err != nil {
			t.Fatalf("attempt %d: %v", i+1, err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
	}

	if *calls != 3 {
		t.Fatalf("server saw %d requests, want 3", *calls)
	}
	for i, got := range *bodies {
		if got != payload {
			t.Errorf("attempt %d sent %q, want the original payload", i+1, got)
		}
	}
}

// TestAPIClientForgetsBodyAfterSuccess verifies the replay cache does not keep
// every finished request alive.
func TestAPIClientForgetsBodyAfterSuccess(t *testing.T) {
	srv, _, _ := newCountingServer(t, http.StatusOK)
	c := newAPIClient(newTransportClient())

	req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("data=x"))
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	c.mu.Lock()
	left := len(c.bodys)
	c.mu.Unlock()
	if left != 0 {
		t.Errorf("cache still holds %d entries after success", left)
	}
}

// TestAPIClientBodylessRequest verifies a request without a body is handled.
func TestAPIClientBodylessRequest(t *testing.T) {
	srv, _, _ := newCountingServer(t, http.StatusOK)
	c := newAPIClient(newTransportClient())

	req, _ := http.NewRequest("GET", srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp.Body.Close()
}
