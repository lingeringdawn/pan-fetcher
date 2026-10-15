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

// TestTestWebhookChannelKeepsKind is the regression test for the WeChat Work
// test button failing while real notifications worked.
//
// TestWebhookChannel builds the channel from the given config, so the kind has
// to be carried through. When the caller dropped it, a WeChat Work entry was
// tested as a generic JSON webhook — and that endpoint rejects anything that is
// not its own markdown envelope, so the test always failed while subscribed
// notifications (which did carry the kind) succeeded.
func TestTestWebhookChannelKeepsKind(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	cfg := WebhookConfig{ID: "w", Kind: KindWework, URL: srv.URL, Enabled: true}
	if err := TestWebhookChannel(cfg); err != nil {
		t.Fatalf("TestWebhookChannel: %v", err)
	}
	// The payload must be the markdown envelope, not the event JSON.
	var payload struct {
		MsgType string `json:"msgtype"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("payload is not the wework envelope: %v (%s)", err, body)
	}
	if payload.MsgType != "markdown" {
		t.Errorf("msgtype = %q; the kind was lost and JSON was sent instead", payload.MsgType)
	}
}

// TestSendWeworkReportsBusinessError covers the second half of the same bug.
//
// WeChat Work answers HTTP 200 even when it rejects the message, reporting the
// outcome in errcode. Reading only the status code made a rejected send look
// delivered, so the test button claimed success while nothing arrived.
func TestSendWeworkReportsBusinessError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"errcode":93000,"errmsg":"invalid webhook url"}`))
	}))
	defer srv.Close()

	err := sendWework(srv.URL, srv.Client(), "## hi")
	if err == nil {
		t.Fatal("a non-zero errcode must be reported as a failure")
	}
	if !strings.Contains(err.Error(), "93000") {
		t.Errorf("error should carry the code, got %q", err)
	}
	// The hint makes the message actionable in the panel.
	if !strings.Contains(err.Error(), "Webhook 地址无效") {
		t.Errorf("error should explain code 93000, got %q", err)
	}
}

// TestSendWeworkSucceedsOnZeroErrcode verifies a normal reply is not reported
// as an error.
func TestSendWeworkSucceedsOnZeroErrcode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"errcode":0,"errmsg":"ok"}`))
	}))
	defer srv.Close()

	if err := sendWework(srv.URL, srv.Client(), "## hi"); err != nil {
		t.Errorf("errcode 0 should succeed, got %v", err)
	}
}

// TestSendWeworkNonJSONBodyIsNotAnError verifies a 200 body that is not the
// expected JSON is treated as success rather than parsed into a phantom error.
//
// A reverse proxy in front of the webhook can return its own 200 page; failing
// on that would report a working setup as broken.
func TestSendWeworkNonJSONBodyIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	}))
	defer srv.Close()

	if err := sendWework(srv.URL, srv.Client(), "## hi"); err != nil {
		t.Errorf("a non-JSON 200 body should not be an error, got %v", err)
	}
}

// TestBuildChannels verifies every stored entry becomes a channel, in order.
func TestBuildChannels(t *testing.T) {
	if got := BuildChannels(nil); len(got) != 0 {
		t.Errorf("no configuration should yield no channels, got %d", len(got))
	}
	got := BuildChannels([]WebhookConfig{
		{ID: "a", Name: "one", URL: "https://example.com/1", Enabled: true},
		{ID: "b", Name: "two", Kind: KindWework, URL: "https://example.com/2", Enabled: true},
	})
	if len(got) != 2 {
		t.Fatalf("expected 2 channels, got %d", len(got))
	}
	if got[0].Name() != "one" || got[1].Name() != "two" {
		t.Errorf("order not preserved: %q, %q", got[0].Name(), got[1].Name())
	}
}

// TestWeworkKindUsesMarkdownPayload verifies a WeChat Work entry sends the
// envelope that destination requires, not the generic event JSON.
//
// WeChat Work rejects anything but its own msgtype/markdown shape, so a
// migrated entry that sent JSON would silently stop delivering.
func TestWeworkKindUsesMarkdownPayload(t *testing.T) {
	var body string
	var sig string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		sig = r.Header.Get(HeaderSignature)
	}))
	defer srv.Close()

	ch := NewWebhookChannel(WebhookConfig{
		ID: "w", Kind: KindWework, URL: srv.URL, Enabled: true,
		// A secret is deliberately set: WeChat Work authenticates by the token
		// in its URL, so the channel must not add a signature.
		Secret: "ignored",
	})
	if err := ch.Send(Event{Type: EventRSSFound, Title: "t", Body: "## 标题"}); err != nil {
		t.Fatalf("Send: %v", err)
	}
	var payload struct {
		MsgType  string `json:"msgtype"`
		Markdown struct {
			Content string `json:"content"`
		} `json:"markdown"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		t.Fatalf("wework payload is not the expected envelope: %v (%s)", err, body)
	}
	if payload.MsgType != "markdown" {
		t.Errorf("msgtype = %q, want markdown", payload.MsgType)
	}
	if payload.Markdown.Content != "## 标题" {
		t.Errorf("content = %q, want the event body", payload.Markdown.Content)
	}
	if sig != "" {
		t.Errorf("a wework entry must not be signed, got %q", sig)
	}
}

// TestNormalizeKind verifies an absent kind means generic, which is what every
// entry written before kinds existed was.
func TestNormalizeKind(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", KindGeneric},
		{"generic", KindGeneric},
		{"wework", KindWework},
		{"WeWork", KindWework},
		{"  wework  ", KindWework},
		{"bogus", KindGeneric},
	}
	for _, c := range cases {
		if got := NormalizeKind(c.in); got != c.want {
			t.Errorf("NormalizeKind(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestMigrateLegacyWework is the regression test for folding the old WeChat Work
// setting into the channel list.
func TestMigrateLegacyWework(t *testing.T) {
	const legacy = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc"

	// Nothing configured: no migration.
	out, changed := MigrateLegacyWework("", nil)
	if changed || len(out) != 0 {
		t.Errorf("empty legacy URL should not migrate, got %v (changed=%v)", out, changed)
	}

	// A legacy URL with no existing entries becomes one wework entry.
	out, changed = MigrateLegacyWework(legacy, nil)
	if !changed {
		t.Fatal("a legacy URL should migrate")
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(out))
	}
	if out[0].Kind != KindWework {
		t.Errorf("kind = %q, want %q", out[0].Kind, KindWework)
	}
	if out[0].URL != legacy {
		t.Errorf("URL = %q, want the legacy URL", out[0].URL)
	}
	if !out[0].Enabled {
		t.Error("the migrated entry should be enabled")
	}
	if out[0].ID == "" {
		t.Error("the migrated entry needs an id")
	}

	// Running it again must not duplicate: this runs at every startup.
	again, _ := MigrateLegacyWework(legacy, out)
	if len(again) != 1 {
		t.Errorf("migration is not idempotent: %d entries", len(again))
	}

	// An existing wework entry means the legacy setting is redundant: report
	// changed so the caller clears it, but do not add anything.
	existing := []WebhookConfig{{ID: "x", Kind: KindWework, URL: "https://example.com/other"}}
	out2, changed2 := MigrateLegacyWework(legacy, existing)
	if !changed2 {
		t.Error("an existing wework entry should still report changed (to clear the legacy value)")
	}
	if len(out2) != 1 {
		t.Errorf("no entry should be added, got %d", len(out2))
	}

	// Other entries are preserved, with the migrated one first.
	generic := []WebhookConfig{{ID: "g", URL: "https://example.com/g", Enabled: true}}
	out3, _ := MigrateLegacyWework(legacy, generic)
	if len(out3) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(out3))
	}
	if out3[0].Kind != KindWework {
		t.Error("the migrated entry should come first")
	}
	if out3[1].ID != "g" {
		t.Error("the existing entry was reordered")
	}
}

// TestMigratedWeworkSubscriptionsExcludeLog verifies the migration does not
// subscribe the new entry to log events, which the log pusher already handles.
func TestMigratedWeworkSubscriptionsExcludeLog(t *testing.T) {
	out, _ := MigrateLegacyWework("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", nil)
	if len(out) != 1 {
		t.Fatal("expected one entry")
	}
	for _, e := range out[0].Events {
		if e == string(EventLog) {
			t.Error("the migrated entry must not subscribe to log events (they would be sent twice)")
		}
	}
	if len(out[0].Events) == 0 {
		t.Error("the migrated entry should preserve the task/RSS notifications the old switches governed")
	}
}

// TestIsWeworkURL pins the heuristic used during migration.
func TestIsWeworkURL(t *testing.T) {
	cases := []struct {
		url  string
		want bool
	}{
		{"https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=abc", true},
		{"https://example.com/cgi-bin/webhook/send?key=abc", true},
		{"https://example.com/hook", false},
		{"", false},
	}
	for _, c := range cases {
		if got := isWeworkURL(c.url); got != c.want {
			t.Errorf("isWeworkURL(%q) = %v, want %v", c.url, got, c.want)
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
