// Package notify delivers notifications about what pan-fetcher did.
//
// The package is built around two ideas:
//
//   - An Event is something that happened, identified by a type and carrying
//     structured data. Call sites report events; they never decide who is
//     told about them.
//   - A Channel is a delivery mechanism (WeChat Work, a generic webhook, the
//     log pusher). Channels declare which event types they want, and the
//     router fans each event out to the ones that match.
//
// Before this split, delivery was hard-coded to WeChat Work: Send took a
// pre-formatted markdown string and the webhook URL was a package-level
// variable, so adding a channel meant touching every call site.
package notify

import (
	"encoding/json"
	"strings"
	"time"
)

// EventType identifies what happened. The string values are part of the
// webhook payload contract, so they are stable and namespaced.
type EventType string

const (
	// EventTaskSubmitted fires when magnet/ed2k/http tasks are handed to 115.
	EventTaskSubmitted EventType = "task.submitted"
	// EventTaskFailed fires when a submitted task did not succeed.
	EventTaskFailed EventType = "task.failed"
	// EventRSSFound fires when a subscription run finds new resources.
	EventRSSFound EventType = "rss.found"
	// EventServerStarted fires once the web server is listening.
	EventServerStarted EventType = "server.started"
	// EventUpdateAvailable fires when a newer release is published.
	EventUpdateAvailable EventType = "update.available"
	// EventLog fires for pushed runtime log lines.
	EventLog EventType = "log"
	// EventTest is used by the settings page to verify a channel.
	EventTest EventType = "test"
)

// AllEventTypes lists every type a channel may subscribe to, in the order the
// settings page presents them.
func AllEventTypes() []EventType {
	return []EventType{
		EventTaskSubmitted,
		EventTaskFailed,
		EventRSSFound,
		EventUpdateAvailable,
		EventServerStarted,
		EventLog,
	}
}

// Valid reports whether t is a known event type.
//
// Unknown types are rejected rather than silently accepted: a typo in a
// subscription would otherwise look configured while never firing.
func (t EventType) Valid() bool {
	for _, k := range AllEventTypes() {
		if k == t {
			return true
		}
	}
	return t == EventTest
}

// Event is a single occurrence worth notifying about.
type Event struct {
	// Type identifies what happened.
	Type EventType `json:"type"`
	// Time is when it happened, in the configured timezone.
	Time time.Time `json:"time"`
	// Title is a short human-readable summary.
	Title string `json:"title"`
	// Body is the markdown message used by channels that render text.
	Body string `json:"body"`
	// Data carries the structured payload for generic webhooks.
	Data map[string]interface{} `json:"data,omitempty"`
}

// Payload renders the JSON body sent to a generic webhook.
//
// The shape is deliberately flat and self-describing so a receiver needs no
// knowledge of this project:
//
//	{
//	  "type": "rss.found",
//	  "time": "2026-10-15T16:04:05+08:00",
//	  "title": "RSS 新资源",
//	  "body": "## RSS 新资源\n...",
//	  "data": { "sub": "...", "count": 2, "names": [...] }
//	}
func (e Event) Payload() ([]byte, error) {
	return json.Marshal(struct {
		Type  EventType              `json:"type"`
		Time  time.Time              `json:"time"`
		Title string                 `json:"title"`
		Body  string                 `json:"body"`
		Data  map[string]interface{} `json:"data,omitempty"`
	}{
		Type:  e.Type,
		Time:  e.Time,
		Title: e.Title,
		Body:  e.Body,
		Data:  e.Data,
	})
}

// Channel delivers events to one destination.
type Channel interface {
	// Name identifies the channel for logs and error messages.
	Name() string
	// Events returns the event types this channel wants. A nil slice means
	// every type, which is how the WeChat Work channel keeps behaving as it
	// always has (it is gated by the existing per-feature toggles instead).
	Events() []EventType
	// Send delivers the event. The router runs channels sequentially and
	// isolates failures, so a slow or broken channel cannot block the others.
	Send(Event) error
}

// wants reports whether c subscribes to t.
func wants(c Channel, t EventType) bool {
	list := c.Events()
	if list == nil {
		return true
	}
	for _, k := range list {
		if k == t {
			return true
		}
	}
	return false
}

// parseEventTypes converts stored strings into event types, dropping unknown
// ones and preserving the canonical order.
//
// Dropping is right here rather than erroring: a subscription list can contain
// types written by an older or newer build, and refusing to load the whole
// channel over one stale entry would be worse than ignoring it.
func parseEventTypes(raw []string) []EventType {
	if len(raw) == 0 {
		return nil
	}
	set := make(map[EventType]bool, len(raw))
	for _, s := range raw {
		t := EventType(strings.TrimSpace(s))
		if t.Valid() {
			set[t] = true
		}
	}
	out := make([]EventType, 0, len(set))
	for _, t := range AllEventTypes() {
		if set[t] {
			out = append(out, t)
		}
	}
	return out
}

// eventTypeStrings renders event types for storage.
func eventTypeStrings(types []EventType) []string {
	out := make([]string, 0, len(types))
	for _, t := range types {
		out = append(out, string(t))
	}
	return out
}
