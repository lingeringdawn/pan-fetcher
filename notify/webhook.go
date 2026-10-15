package notify

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// Signature headers. A receiver verifies the request with the shared secret and
// can reject anything that does not match, so a leaked URL alone is not enough
// to forge a notification.
const (
	// HeaderSignature carries the hex HMAC-SHA256 of the raw body.
	HeaderSignature = "X-Pan-Fetcher-Signature"
	// HeaderSignatureAlgo names the algorithm, so a receiver can support more
	// than one without guessing.
	HeaderSignatureAlgo = "X-Pan-Fetcher-Signature-Algorithm"
	// HeaderEvent carries the event type, for receivers that route without
	// parsing the body.
	HeaderEvent = "X-Pan-Fetcher-Event"
	// HeaderDelivery identifies this delivery attempt, for dedup on the
	// receiver side when a retry follows a timeout.
	HeaderDelivery = "X-Pan-Fetcher-Delivery"
)

// SignatureAlgorithm is the value sent in HeaderSignatureAlgo and the prefix of
// the signature header value.
const SignatureAlgorithm = "hmac-sha256"

// signBody returns the value of the signature header for body under secret.
//
// The format is "<algorithm>=<hex digest>", which is what most webhook
// receivers expect (GitHub, Stripe and friends) and leaves room to change
// algorithm without ambiguity.
func signBody(secret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return SignatureAlgorithm + "=" + hex.EncodeToString(mac.Sum(nil))
}

// VerifySignature checks a signature produced by signBody.
//
// It exists so tests and downstream examples can share one implementation
// instead of re-deriving the format. The comparison is constant-time.
func VerifySignature(secret string, body []byte, header string) bool {
	want := signBody(secret, body)
	return hmac.Equal([]byte(want), []byte(header))
}

// Webhook kinds. The kind selects the request format, not the routing: both
// are ordinary channels in the same list with the same event subscriptions.
const (
	// KindGeneric posts the event as JSON and signs it with HMAC when a secret
	// is set. This is the default.
	KindGeneric = "generic"
	// KindWework posts WeChat Work's markdown payload. It exists so the WeChat
	// Work webhook configured by earlier versions keeps working after being
	// migrated into this list — the destination expects that shape and rejects
	// anything else.
	KindWework = "wework"
)

// WebhookChannel delivers events to one URL.
//
// The kind decides the body format; everything else (subscriptions, enabling,
// retry policy) is shared, so the settings page can present one list.
type WebhookChannel struct {
	id      string
	name    string
	kind    string
	url     string
	secret  string
	events  []EventType
	enabled bool
	client  *http.Client
}

// WebhookConfig is the stored form of a webhook channel.
type WebhookConfig struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Kind    string   `json:"kind,omitempty"`
	URL     string   `json:"url"`
	Secret  string   `json:"secret,omitempty"`
	Events  []string `json:"events,omitempty"`
	Enabled bool     `json:"enabled"`
}

// NormalizeKind maps an absent or unknown kind onto a known one.
//
// Empty means generic: that is what every entry written before kinds existed
// was, so an upgrade keeps them behaving as they did.
func NormalizeKind(k string) string {
	if strings.EqualFold(strings.TrimSpace(k), KindWework) {
		return KindWework
	}
	return KindGeneric
}

// NewWebhookChannel builds a channel from stored configuration.
//
// A channel with no explicit event list subscribes to everything: that is what
// a user adding one webhook expects, and it matches the behaviour of every
// other tool that takes a single callback URL.
func NewWebhookChannel(cfg WebhookConfig) *WebhookChannel {
	return &WebhookChannel{
		id:      cfg.ID,
		name:    cfg.Name,
		kind:    NormalizeKind(cfg.Kind),
		url:     strings.TrimSpace(cfg.URL),
		secret:  cfg.Secret,
		events:  parseEventTypes(cfg.Events),
		enabled: cfg.Enabled,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (w *WebhookChannel) Name() string {
	if w.name != "" {
		return w.name
	}
	return "webhook"
}

func (w *WebhookChannel) Events() []EventType { return w.events }

// Send delivers the event, retrying once on a transport error or a 5xx.
//
// Retrying is limited and never repeats a 4xx: a receiver that answered 400
// will answer 400 again, and repeating it would duplicate a rejected delivery
// while adding load.
func (w *WebhookChannel) Send(ev Event) error {
	if !w.enabled {
		return nil
	}
	if w.url == "" {
		return fmt.Errorf("webhook %s: URL 为空", w.Name())
	}
	if !wants(w, ev.Type) {
		return nil
	}

	// WeChat Work takes its own payload shape and is not signed; it
	// authenticates by the secret embedded in the URL.
	if w.kind == KindWework {
		return sendWework(w.url, w.client, ev.Body)
	}

	body, err := ev.Payload()
	if err != nil {
		return fmt.Errorf("webhook %s: 序列化失败: %w", w.Name(), err)
	}

	const attempts = 2
	var lastErr error
	for i := 0; i < attempts; i++ {
		if i > 0 {
			time.Sleep(time.Duration(i) * time.Second)
		}
		retry, err := w.post(ev, body)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			break
		}
	}
	return lastErr
}

// post performs one delivery, reporting whether a retry is worthwhile.
func (w *WebhookChannel) post(ev Event, body []byte) (retry bool, err error) {
	req, err := http.NewRequest(http.MethodPost, w.url, bytes.NewReader(body))
	if err != nil {
		return false, fmt.Errorf("webhook %s: 构造请求失败: %w", w.Name(), err)
	}
	req.Header.Set("Content-Type", "application/json; charset=utf-8")
	req.Header.Set("User-Agent", "pan-fetcher-webhook/1.0")
	req.Header.Set(HeaderEvent, string(ev.Type))
	req.Header.Set(HeaderDelivery, deliveryID(ev))
	if w.secret != "" {
		req.Header.Set(HeaderSignatureAlgo, SignatureAlgorithm)
		req.Header.Set(HeaderSignature, signBody(w.secret, body))
	} else {
		// Say so explicitly rather than leaving the header absent, which is
		// indistinguishable from a bug to a receiver that requires signing.
		req.Header.Set(HeaderSignatureAlgo, "none")
	}

	resp, err := w.client.Do(req)
	if err != nil {
		return true, fmt.Errorf("webhook %s: 请求失败: %w", w.Name(), err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return false, nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 256))
	if resp.StatusCode >= 500 {
		return true, fmt.Errorf("webhook %s: HTTP %d: %s", w.Name(), resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return false, fmt.Errorf("webhook %s: HTTP %d: %s", w.Name(), resp.StatusCode, strings.TrimSpace(string(snippet)))
}

// deliveryID is a stable identifier for one event delivery.
//
// It is derived from the event rather than random so a retry carries the same
// id and the receiver can drop the duplicate.
func deliveryID(ev Event) string {
	h := sha256.Sum256([]byte(string(ev.Type) + "|" + ev.Time.Format(time.RFC3339Nano) + "|" + ev.Title))
	return hex.EncodeToString(h[:8])
}

// TestWebhookChannel sends a test event through the given configuration without
// registering it, so the settings page can verify a URL before saving.
func TestWebhookChannel(cfg WebhookConfig) error {
	ch := NewWebhookChannel(cfg)
	ch.enabled = true // the caller is explicitly testing it
	ch.events = nil   // and wants delivery regardless of subscriptions
	if strings.TrimSpace(ch.url) == "" {
		return fmt.Errorf("Webhook URL 为空")
	}
	ev := Event{
		Type:  EventTest,
		Time:  now(),
		Title: "pan-fetcher 通知测试",
		Body:  "## pan-fetcher 通知测试 ✅\n> Webhook 配置成功",
		Data:  map[string]interface{}{"test": true},
	}
	if err := ch.Send(ev); err != nil {
		return err
	}
	log.Printf("[notify] webhook test ok: %s", redactURL(ch.url))
	return nil
}
// redactURL hides the path and query of a webhook URL for logging.
//
// Webhook URLs commonly embed a secret token in the path (that is how Slack,
// Discord and WeChat Work work), so logging one verbatim would leak it into the
// runtime log the panel displays.
func redactURL(raw string) string {
	i := strings.Index(raw, "://")
	if i < 0 {
		return "***"
	}
	rest := raw[i+3:]
	j := strings.IndexByte(rest, '/')
	if j < 0 {
		return raw // no path to hide
	}
	return raw[:i+3] + rest[:j] + "/***"
}
