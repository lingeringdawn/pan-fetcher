package jackett

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProbeJackettFingerprints locks in how a live instance is recognised.
//
// The original probe required the response body of /UI/Login to contain
// "jackett", but Jackett answers that route with 302 and a zero-length body, so
// a perfectly healthy instance was never detected and Start always timed out.
// These cases pin the fingerprints that replaced it.
func TestProbeJackettFingerprints(t *testing.T) {
	cases := []struct {
		name    string
		handler http.HandlerFunc
		want    bool
	}{
		{
			// The real behaviour of a freshly started Jackett.
			name: "testcookie redirect is recognised",
			handler: func(w http.ResponseWriter, r *http.Request) {
				http.SetCookie(w, &http.Cookie{Name: "TestCookie", Value: "1", Path: "/"})
				w.Header().Set("Location", "TestCookie")
				w.Header().Set("Server", "Kestrel")
				w.WriteHeader(http.StatusFound)
			},
			want: true,
		},
		{
			name: "rendered login page is recognised",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<html><head><title>Jackett</title></head></html>`))
			},
			want: true,
		},
		{
			name: "redirect into /UI is recognised",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Location", "/UI/Dashboard")
				w.WriteHeader(http.StatusFound)
			},
			want: true,
		},
		{
			name: "unrelated service on the port is rejected",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(`<html><title>nginx</title></html>`))
			},
			want: false,
		},
		{
			name: "empty 200 response is rejected",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			},
			want: false,
		},
		{
			name: "unrelated error page is rejected",
			handler: func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusInternalServerError)
				w.Write([]byte("Internal Server Error"))
			},
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			defer srv.Close()

			if got := probeJackett(srv.URL); got != tc.want {
				t.Errorf("probeJackett(%s) = %v, want %v", srv.URL, got, tc.want)
			}
		})
	}
}

// TestProbeJackettRootRedirect covers the secondary marker: a bare root request
// that redirects under /UI/.
func TestProbeJackettRootRedirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			w.Header().Set("Location", "/UI/Dashboard")
			w.WriteHeader(http.StatusMovedPermanently)
			return
		}
		// /UI/Login answers only the root redirect marker.
		w.Header().Set("Location", "/UI/Dashboard")
		w.WriteHeader(http.StatusMovedPermanently)
	}))
	defer srv.Close()

	if !probeJackett(srv.URL) {
		t.Error("root redirect under /UI/ should be recognised as Jackett")
	}
}

// TestProbeJackettUnreachable ensures a dead address is reported as absent
// rather than hanging.
func TestProbeJackettUnreachable(t *testing.T) {
	if probeJackett("http://127.0.0.1:1") {
		t.Error("nothing is listening on port 1; probe must report false")
	}
	if probeJackett("") {
		t.Error("empty URL must report false")
	}
}

// TestHealthCheckRejectsNonJackett verifies the pre-flight check used before an
// API key is known does not accept an arbitrary web server.
func TestHealthCheckRejectsNonJackett(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from some other service"))
	}))
	defer srv.Close()

	if err := HealthCheck(srv.URL); err == nil {
		t.Error("HealthCheck accepted a non-Jackett service")
	}
}
