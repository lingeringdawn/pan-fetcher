package notify

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Router fans events out to registered channels.
//
// Delivery is asynchronous: a notification must never delay the download it
// describes, and a receiver that hangs must not block a subscription run.
// Channel failures are logged and isolated, so one broken webhook cannot stop
// the others from being told.
type Router struct {
	mu       sync.RWMutex
	channels []Channel
	queue    chan Event
	file     string
}

var (
	routerMu sync.RWMutex
	router   *Router
)

// SetRouterDataDir points the channel store at the application data directory.
//
// The file is resolved against the data directory rather than the working
// directory for the same reason as every other runtime file: self-update
// restarts the process with a different working directory.
func SetRouterDataDir(dir string) {
	if dir == "" {
		return
	}
	r := getRouter()
	r.mu.Lock()
	r.file = filepath.Join(dir, "webhooks.json")
	r.mu.Unlock()
}

// getRouter returns the process-wide router, creating it on first use.
func getRouter() *Router {
	routerMu.Lock()
	defer routerMu.Unlock()
	if router == nil {
		router = &Router{
			// Sized to absorb a burst (a subscription run can report several
			// events at once) without letting an unbounded queue eat memory.
			queue: make(chan Event, 256),
		}
		go router.run()
	}
	return router
}

// run drains the queue, delivering each event to every interested channel.
func (r *Router) run() {
	for ev := range r.queue {
		r.mu.RLock()
		channels := append([]Channel(nil), r.channels...)
		r.mu.RUnlock()

		for _, c := range channels {
			if !wants(c, ev.Type) {
				continue
			}
			if err := c.Send(ev); err != nil {
				log.Printf("[notify] %s 发送失败 (%s): %v", c.Name(), ev.Type, err)
			}
		}
	}
}

// Dispatch enqueues an event for delivery.
//
// The event is enriched with the current time when it has none, so call sites
// do not each have to remember to stamp it.
func Dispatch(ev Event) {
	if !ev.Type.Valid() {
		log.Printf("[notify] 忽略未知事件类型: %q", ev.Type)
		return
	}
	if ev.Time.IsZero() {
		ev.Time = now()
	}
	r := getRouter()
	select {
	case r.queue <- ev:
	default:
		// The queue is full: drop rather than block the caller. Losing a
		// notification is better than stalling a download run, and the log
		// records it so the gap is visible.
		log.Printf("[notify] 通知队列已满，丢弃事件 %s", ev.Type)
	}
}

// SetChannels replaces the registered channels.
func SetChannels(list []Channel) {
	r := getRouter()
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channels = list
}

// ChannelCount returns how many channels are registered, for tests and logs.
func ChannelCount() int {
	r := getRouter()
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.channels)
}

// --- persistence -----------------------------------------------------------

// WebhookStore is the on-disk form of the webhook channel list.
type WebhookStore struct {
	Webhooks []WebhookConfig `json:"webhooks"`
}

// LoadWebhooks reads the stored webhook channels.
//
// A missing file is not an error: it is the state of every installation that
// has never configured one.
func LoadWebhooks(path string) ([]WebhookConfig, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var store WebhookStore
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, err
	}
	return store.Webhooks, nil
}

// SaveWebhooks writes the webhook channels to path.
//
// The write goes through a temporary file and a rename so a crash mid-write
// cannot leave a truncated file that would fail to parse on next start.
func SaveWebhooks(path string, hooks []WebhookConfig) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	data, err := json.MarshalIndent(WebhookStore{Webhooks: hooks}, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// NewWebhookID returns a short random identifier for a new webhook.
func NewWebhookID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fall back to the clock; a collision here only affects display.
		return "wh-" + time.Now().Format("20060102150405")
	}
	return "wh-" + hex.EncodeToString(b[:])
}

// BuildChannels turns stored configuration into the channel list the router
// uses.
//
// Every destination is a webhook entry, including WeChat Work: an older install
// that configured only the WeChat Work URL is migrated into the list by
// MigrateLegacyWework, so the settings page shows one uniform set of channels.
func BuildChannels(hooks []WebhookConfig) []Channel {
	out := make([]Channel, 0, len(hooks))
	// Stored order is preserved so the settings list is stable across restarts.
	for _, h := range hooks {
		out = append(out, NewWebhookChannel(h))
	}
	return out
}

// MigrateLegacyWework folds a pre-existing WeChat Work webhook URL into the
// channel list, returning the list to store and whether anything changed.
//
// Earlier versions kept this URL in a separate setting with three on/off
// switches. Folding it in means users keep the notification they already had,
// and afterwards there is a single place where destinations live.
//
// The legacy URL is only migrated when no WeChat Work entry exists yet, so
// running this repeatedly (at every startup) cannot create duplicates. The
// caller clears the old setting once migrated is true.
func MigrateLegacyWework(legacyURL string, hooks []WebhookConfig) (out []WebhookConfig, migrated bool) {
	legacyURL = strings.TrimSpace(legacyURL)
	if legacyURL == "" {
		return hooks, false
	}
	for _, h := range hooks {
		if NormalizeKind(h.Kind) == KindWework {
			// Already represented; the legacy setting is redundant.
			return hooks, true
		}
	}
	migratedEntry := WebhookConfig{
		ID:   NewWebhookID(),
		Name: "企业微信",
		Kind: KindWework,
		URL:  legacyURL,
		// The old switches defaulted to off; enabling the entry keeps the
		// channel visible in the list, and the per-event subscriptions below
		// decide what actually gets sent, exactly as the switches did.
		Enabled: true,
	}
	// Map the legacy switches onto event subscriptions so their meaning is
	// preserved: task and RSS on, log off. The log pusher has its own toggle
	// and throttle, so it stays out of the event list.
	var events []string
	events = append(events, string(EventTaskSubmitted), string(EventTaskFailed))
	if strings.TrimSpace(legacyURL) != "" {
		events = append(events, string(EventRSSFound), string(EventUpdateAvailable),
			string(EventServerStarted))
	}
	migratedEntry.Events = events

	out = append([]WebhookConfig{migratedEntry}, hooks...)
	return out, true
}

// EventFromData builds an event with structured data attached.
//
// It is the constructor call sites should use: it keeps the type, title, body
// and data consistent so a receiver can rely on the shape.
func EventFromData(t EventType, title, body string, data map[string]interface{}) Event {
	return Event{
		Type:  t,
		Time:  now(),
		Title: title,
		Body:  body,
		Data:  data,
	}
}
