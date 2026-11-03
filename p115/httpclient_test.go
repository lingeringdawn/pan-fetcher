package p115

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// countingServer records how many requests it saw and whether each body arrived
// intact.
func countingServer(t *testing.T, status int, wantBody string) (*httptest.Server, *int, *[]string) {
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

// TestAPIClientReportsOutage is the regression test for misreported failures.
//
// A failing gateway answers with an HTML page. Left alone the SDK tries to
// decrypt it and reports "invalid ec data", which sends the user looking at their
// login while 115 is simply down.
func TestAPIClientReportsOutage(t *testing.T) {
	for _, status := range []int{
		http.StatusBadGateway,
		http.StatusServiceUnavailable,
		http.StatusGatewayTimeout,
	} {
		srv, _, _ := countingServer(t, status, "")
		c := newAPIClient(newTransportClient())

		req, _ := http.NewRequest("POST", srv.URL, strings.NewReader("data=x"))
		_, err := c.Do(req)
		if err == nil {
			t.Fatalf("status %d should be reported as an outage", status)
		}
		if !isOutage(err) {
			t.Errorf("status %d: want an outage error, got %v", status, err)
		}
		if !strings.Contains(err.Error(), "115") {
			t.Errorf("status %d: message should name 115, got %q", status, err.Error())
		}
		if strings.Contains(err.Error(), "invalid ec data") {
			t.Errorf("status %d: decryption wording must not leak through", status)
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
// timeout.
func TestAPIClientReplaysBody(t *testing.T) {
	const payload = "data=encrypted-blob-content"
	srv, calls, bodies := countingServer(t, http.StatusOK, payload)

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

// TestRetryP115SkipsOutage verifies an outage is not retried.
//
// Retrying a 502 only doubles the wait before the same answer.
func TestRetryP115SkipsOutage(t *testing.T) {
	calls := 0
	err := retryP115("test", func() error {
		calls++
		return &UpstreamError{Status: http.StatusBadGateway}
	})
	if err == nil {
		t.Fatal("an outage must be reported")
	}
	if calls != 1 {
		t.Errorf("an outage should be attempted once, got %d attempts", calls)
	}
	if !isOutage(err) {
		t.Errorf("the outage should surface unchanged, got %v", err)
	}
}

// TestExplainP115ErrorKeepsOutageMessage verifies the outage wording is not
// replaced by the generic decryption hint.
func TestExplainP115ErrorKeepsOutageMessage(t *testing.T) {
	got := explainP115Error(&UpstreamError{Status: http.StatusBadGateway})
	if got == nil || !strings.Contains(got.Error(), "不可用") {
		t.Errorf("want the outage message, got %v", got)
	}
	if strings.Contains(got.Error(), "解密") {
		t.Errorf("an outage must not be described as a decryption failure: %q", got.Error())
	}
}

// TestIsOutageOnPlainError verifies ordinary errors are not mistaken for outages.
func TestIsOutageOnPlainError(t *testing.T) {
	if isOutage(errors.New("invalid ec data")) {
		t.Error("a decryption error is not an outage")
	}
	if isOutage(nil) {
		t.Error("nil is not an outage")
	}
}
