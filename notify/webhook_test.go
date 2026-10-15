package notify

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSignBodyFormat pins the signature format, which is the contract a
// receiver implements against.
func TestSignBodyFormat(t *testing.T) {
	body := []byte(`{"a":1}`)
	got := signBody("secret", body)

	if !strings.HasPrefix(got, SignatureAlgorithm+"=") {
		t.Fatalf("signature %q should start with %q=", got, SignatureAlgorithm)
	}
	// Recompute independently: the value must be the plain hex HMAC-SHA256.
	mac := hmac.New(sha256.New, []byte("secret"))
	mac.Write(body)
	want := SignatureAlgorithm + "=" + hex.EncodeToString(mac.Sum(nil))
	if got != want {
		t.Errorf("signBody = %q, want %q", got, want)
	}
}

// TestVerifySignature covers the receiver-side check, including tampering.
func TestVerifySignature(t *testing.T) {
	secret := "s3cret"
	body := []byte(`{"type":"test"}`)
	sig := signBody(secret, body)

	if !VerifySignature(secret, body, sig) {
		t.Error("valid signature rejected")
	}
	if VerifySignature(secret, []byte(`{"type":"evil"}`), sig) {
		t.Error("tampered body accepted")
	}
	if VerifySignature("wrong", body, sig) {
		t.Error("wrong secret accepted")
	}
	if VerifySignature(secret, body, "") {
		t.Error("empty signature accepted")
	}
}

// TestWebhookChannelSignsRequest verifies the headers a receiver relies on.
func TestWebhookChannelSignsRequest(t *testing.T) {
	var gotSig, gotEvent, gotAlgo, gotBody string
	var gotDelivery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		gotSig = r.Header.Get(HeaderSignature)
		gotEvent = r.Header.Get(HeaderEvent)
		gotAlgo = r.Header.Get(HeaderSignatureAlgo)
		gotDelivery = r.Header.Get(HeaderDelivery)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{
		ID: "wh-1", Name: "test", URL: srv.URL, Secret: "topsecret", Enabled: true,
	})
	ev := Event{
		Type:  EventRSSFound,
		Time:  time.Date(2026, 10, 15, 16, 0, 0, 0, time.UTC),
		Title: "RSS 新资源",
		Body:  "## RSS 新资源",
		Data:  map[string]interface{}{"sub": "侦探已死", "count": 2},
	}
	if err := ch.Send(ev); err != nil {
		t.Fatalf("Send: %v", err)
	}

	if gotEvent != string(EventRSSFound) {
		t.Errorf("event header = %q, want %q", gotEvent, EventRSSFound)
	}
	if gotAlgo != SignatureAlgorithm {
		t.Errorf("algorithm header = %q, want %q", gotAlgo, SignatureAlgorithm)
	}
	if gotDelivery == "" {
		t.Error("delivery header missing; retries cannot be deduplicated")
	}
	if !VerifySignature("topsecret", []byte(gotBody), gotSig) {
		t.Errorf("signature does not verify over the received body")
	}

	// The body must be the documented shape.
	var payload struct {
		Type  string                 `json:"type"`
		Time  time.Time              `json:"time"`
		Title string                 `json:"title"`
		Body  string                 `json:"body"`
		Data  map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal([]byte(gotBody), &payload); err != nil {
		t.Fatalf("payload is not valid JSON: %v (%s)", err, gotBody)
	}
	if payload.Type != string(EventRSSFound) || payload.Title != "RSS 新资源" {
		t.Errorf("payload fields wrong: %+v", payload)
	}
	if payload.Data["sub"] != "侦探已死" {
		t.Errorf("structured data missing or mangled: %+v", payload.Data)
	}
}

// TestWebhookChannelUnsignedSaysSo verifies the algorithm header marks an
// unsigned delivery, so a receiver requiring signatures can reject it
// deliberately instead of treating a missing header as a bug.
func TestWebhookChannelUnsignedSaysSo(t *testing.T) {
	var sig, algo string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sig = r.Header.Get(HeaderSignature)
		algo = r.Header.Get(HeaderSignatureAlgo)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{ID: "w", URL: srv.URL, Enabled: true})
	if err := ch.Send(Event{Type: EventTest, Title: "t"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if sig != "" {
		t.Errorf("no secret was set but a signature was sent: %q", sig)
	}
	if algo != "none" {
		t.Errorf("algorithm header = %q, want none", algo)
	}
}

// TestWebhookChannelRetriesOn5xx verifies a transient failure is retried and a
// permanent one is not.
func TestWebhookChannelRetriesOn5xx(t *testing.T) {
	var calls int32
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{ID: "w", URL: srv.URL, Enabled: true})
	if err := ch.Send(Event{Type: EventTest, Title: "t"}); err != nil {
		t.Fatalf("Send should succeed after a retry: %v", err)
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 2 {
		t.Errorf("expected 2 attempts (1 fail + 1 retry), got %d", n)
	}
}

// TestWebhookChannelNoRetryOn4xx verifies a client error is not repeated: the
// receiver will answer the same way, and repeating it only adds load.
func TestWebhookChannelNoRetryOn4xx(t *testing.T) {
	var calls int32
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		mu.Unlock()
		w.WriteHeader(http.StatusBadRequest)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{ID: "w", URL: srv.URL, Enabled: true})
	if err := ch.Send(Event{Type: EventTest, Title: "t"}); err == nil {
		t.Fatal("a 400 should be reported as an error")
	}
	mu.Lock()
	n := calls
	mu.Unlock()
	if n != 1 {
		t.Errorf("a 4xx must not be retried, got %d attempts", n)
	}
}

// TestWebhookChannelEventFiltering verifies a channel only receives the types
// it subscribed to.
func TestWebhookChannelEventFiltering(t *testing.T) {
	var mu sync.Mutex
	var seen []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Header.Get(HeaderEvent))
		mu.Unlock()
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{
		ID: "w", URL: srv.URL, Enabled: true,
		Events: []string{string(EventRSSFound)},
	})
	ch.Send(Event{Type: EventRSSFound, Title: "yes"})
	ch.Send(Event{Type: EventTaskSubmitted, Title: "no"})

	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 1 || seen[0] != string(EventRSSFound) {
		t.Errorf("subscription filter not applied, saw %v", seen)
	}
}

// TestWebhookChannelDisabledDoesNotSend verifies the enabled flag is honoured.
func TestWebhookChannelDisabledDoesNotSend(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{ID: "w", URL: srv.URL, Enabled: false})
	if err := ch.Send(Event{Type: EventTest}); err != nil {
		t.Fatalf("a disabled channel should be a no-op, got %v", err)
	}
	if calls != 0 {
		t.Errorf("disabled channel made %d requests", calls)
	}
}

// TestNoEventListMeansAll covers the default a user adding one webhook expects.
func TestNoEventListMeansAll(t *testing.T) {
	ch := NewWebhookChannel(WebhookConfig{ID: "w", URL: "http://example.invalid", Enabled: true})
	for _, ty := range AllEventTypes() {
		if !wants(ch, ty) {
			t.Errorf("a channel with no event list should want %s", ty)
		}
	}
}

// TestParseEventTypes verifies stored strings are validated and normalised.
func TestParseEventTypes(t *testing.T) {
	got := parseEventTypes([]string{"rss.found", "bogus", "task.submitted", "rss.found", "  "})
	want := []EventType{EventTaskSubmitted, EventRSSFound} // canonical order
	if len(got) != len(want) {
		t.Fatalf("parseEventTypes = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("parseEventTypes[%d] = %v, want %v", i, got[i], want[i])
		}
	}
	// An empty list means "all", so it must stay nil rather than becoming empty.
	if parseEventTypes(nil) != nil {
		t.Error("nil input should stay nil (meaning subscribe to all)")
	}
}

// TestEventTypeValid verifies unknown types are rejected.
func TestEventTypeValid(t *testing.T) {
	for _, ty := range AllEventTypes() {
		if !ty.Valid() {
			t.Errorf("%s should be valid", ty)
		}
	}
	if EventType("nope").Valid() {
		t.Error("an unknown type must not be valid")
	}
	if !EventTest.Valid() {
		t.Error("the test event must be valid")
	}
}

// TestRedactURL verifies secrets embedded in webhook paths are not logged.
func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", "https://qyapi.weixin.qq.com/***"},
		{"https://example.com/hook/secret", "https://example.com/***"},
		{"https://example.com", "https://example.com"},
		{"not a url", "***"},
	}
	for _, c := range cases {
		if got := redactURL(c.in); got != c.want {
			t.Errorf("redactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestDeliveryIDStable verifies a retry of the same event carries the same id,
// which is what lets a receiver drop the duplicate.
func TestDeliveryIDStable(t *testing.T) {
	ev := Event{Type: EventRSSFound, Time: time.Unix(1000, 0), Title: "x"}
	a := deliveryID(ev)
	b := deliveryID(ev)
	if a != b {
		t.Errorf("deliveryID is not stable: %q vs %q", a, b)
	}
	other := Event{Type: EventRSSFound, Time: time.Unix(1001, 0), Title: "x"}
	if deliveryID(other) == a {
		t.Error("different events should get different delivery ids")
	}
}

// TestStoreRoundTrip verifies the webhook list survives a save/load cycle,
// including the secret.
func TestStoreRoundTrip(t *testing.T) {
	path := t.TempDir() + "/webhooks.json"
	in := []WebhookConfig{
		{ID: "a", Name: "one", URL: "https://example.com/1", Secret: "s1", Enabled: true,
			Events: []string{"rss.found"}},
		{ID: "b", Name: "two", URL: "https://example.com/2", Enabled: false},
	}
	if err := SaveWebhooks(path, in); err != nil {
		t.Fatalf("SaveWebhooks: %v", err)
	}
	out, err := LoadWebhooks(path)
	if err != nil {
		t.Fatalf("LoadWebhooks: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("loaded %d hooks, want 2", len(out))
	}
	if out[0].Secret != "s1" || out[0].URL != "https://example.com/1" {
		t.Errorf("hook 0 mangled: %+v", out[0])
	}
	if len(out[0].Events) != 1 || out[0].Events[0] != "rss.found" {
		t.Errorf("events mangled: %+v", out[0].Events)
	}
	if out[1].Enabled {
		t.Error("enabled flag not preserved")
	}
}

// TestLoadWebhooksMissingFile verifies a fresh install is not an error.
func TestLoadWebhooksMissingFile(t *testing.T) {
	out, err := LoadWebhooks(t.TempDir() + "/nope.json")
	if err != nil {
		t.Errorf("a missing file should not error: %v", err)
	}
	if out != nil {
		t.Errorf("expected no hooks, got %v", out)
	}
}

// TestTestWebhookChannelUnreachable verifies the settings-page test reports a
// failure rather than pretending it worked.
func TestTestWebhookChannelUnreachable(t *testing.T) {
	err := TestWebhookChannel(WebhookConfig{URL: "http://127.0.0.1:1/hook"})
	if err == nil {
		t.Fatal("an unreachable URL should be reported as a failure")
	}
	if err := TestWebhookChannel(WebhookConfig{URL: "  "}); err == nil {
		t.Error("an empty URL should be reported as a failure")
	}
}

// TestTestWebhookChannelSucceeds verifies the happy path, including that the
// test event type is delivered even though it is not subscribable.
func TestTestWebhookChannelSucceeds(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(HeaderEvent)
	}))
	defer srv.Close()

	if err := TestWebhookChannel(WebhookConfig{URL: srv.URL, Secret: "k"}); err != nil {
		t.Fatalf("TestWebhookChannel: %v", err)
	}
	if got != string(EventTest) {
		t.Errorf("event header = %q, want %q", got, EventTest)
	}
}

// TestBuildChannelsIncludesWeworkOnlyWhenConfigured verifies the legacy channel
// appears exactly when a URL is set, which is what keeps existing installs
// behaving identically.
func TestBuildChannelsIncludesWeworkOnlyWhenConfigured(t *testing.T) {
	if got := BuildChannels("", nil); len(got) != 0 {
		t.Errorf("no configuration should yield no channels, got %d", len(got))
	}
	got := BuildChannels("https://example.com/wework", []WebhookConfig{
		{ID: "a", URL: "https://example.com/1", Enabled: true},
	})
	if len(got) != 2 {
		t.Fatalf("expected wework + 1 webhook, got %d", len(got))
	}
	if got[0].Name() != "企业微信" {
		t.Errorf("first channel = %q, want 企业微信", got[0].Name())
	}
}

// TestWeworkChannelDoesNotTakeLogEvents verifies log lines are not delivered
// twice.
//
// The WeChat Work URL is pushed to directly by the log throttler, which owns
// the notify_log toggle and the per-second rate limit. If this channel also
// subscribed to log events, every line would be sent twice.
func TestWeworkChannelDoesNotTakeLogEvents(t *testing.T) {
	ch := &weworkChannel{url: "https://example.com/hook", client: http.DefaultClient}
	if wants(ch, EventLog) {
		t.Error("the wework channel must not subscribe to log events")
	}
	// It must still take everything else.
	for _, ty := range AllEventTypes() {
		if ty == EventLog {
			continue
		}
		if !wants(ch, ty) {
			t.Errorf("the wework channel should receive %s", ty)
		}
	}
}

// TestDispatchIgnoresUnknownType verifies an invalid event is dropped rather
// than delivered to channels that cannot interpret it.
func TestDispatchIgnoresUnknownType(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
	}))
	defer srv.Close()

	SetChannels([]Channel{NewWebhookChannel(WebhookConfig{ID: "w", URL: srv.URL, Enabled: true})})
	Dispatch(Event{Type: EventType("bogus"), Title: "x"})

	// Dispatch is asynchronous; a short wait is enough to prove nothing was
	// queued, since a valid event would arrive well within it.
	time.Sleep(200 * time.Millisecond)
	if calls != 0 {
		t.Errorf("unknown event type was delivered (%d calls)", calls)
	}
}

// TestDispatchDeliversToSubscribedChannels is the end-to-end router check.
func TestDispatchDeliversToSubscribedChannels(t *testing.T) {
	var mu sync.Mutex
	var events []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		events = append(events, r.Header.Get(HeaderEvent))
		mu.Unlock()
	}))
	defer srv.Close()

	SetChannels([]Channel{
		NewWebhookChannel(WebhookConfig{ID: "a", URL: srv.URL, Enabled: true,
			Events: []string{string(EventRSSFound)}}),
		NewWebhookChannel(WebhookConfig{ID: "b", URL: srv.URL, Enabled: true,
			Events: []string{string(EventTaskSubmitted)}}),
	})

	Dispatch(Event{Type: EventRSSFound, Title: "x"})
	Dispatch(Event{Type: EventTaskSubmitted, Title: "y"})

	// Wait for the async deliveries.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(events)
		mu.Unlock()
		if n >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	if len(events) != 2 {
		t.Fatalf("expected 2 deliveries, got %v", events)
	}
}
