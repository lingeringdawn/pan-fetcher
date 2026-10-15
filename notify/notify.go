// Package notify provides notification sending for various channels.
package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
	"unicode/utf8"
)

var (
	mu          sync.Mutex
	webhookURL  string
	tzLoc       *time.Location
	logEnabled  bool
	logQueue    = make(chan string, 200)
	client      = &http.Client{Timeout: 10 * time.Second}
	recentItems []RecentItem
	recentMax   = 10
	recentFile  = "recent-items.json"
)

// RecentItem is a newly added subscription resource shown on dashboard.
type RecentItem struct {
	Name string `json:"name"`
	Time string `json:"time"`
	Sub  string `json:"sub"`
}

// SetDataDir points the recent-items file at the application data directory.
//
// The file used to be a bare relative name, which resolved against the process
// working directory. Self-update restarts the server with its working directory
// set to the binary's folder, so the list silently moved. Call this during
// startup, before LoadRecentItems.
func SetDataDir(dir string) {
	if dir == "" {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	recentFile = filepath.Join(dir, "recent-items.json")
}

// RecordItems adds items to the recent list for dashboard display.
func RecordItems(sub string, names []string) {
	ts := now().Format("01-02 15:04")
	mu.Lock()
	for _, n := range names {
		recentItems = append([]RecentItem{{Name: n, Time: ts, Sub: sub}}, recentItems...)
	}
	if len(recentItems) > recentMax {
		recentItems = recentItems[:recentMax]
	}
	saveRecentLocked()
	n := len(recentItems)
	mu.Unlock()
	log.Printf("[notify] recorded %d items for %q, total=%d", len(names), sub, n)
}

// LoadRecentItems restores the recent items list from disk.
func LoadRecentItems() {
	mu.Lock()
	data, err := os.ReadFile(recentFile)
	if err != nil {
		mu.Unlock()
		return
	}
	data = bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
	if !utf8.Valid(data) {
		mu.Unlock()
		return
	}
	if err := json.Unmarshal(data, &recentItems); err != nil {
		mu.Unlock()
		return
	}
	if len(recentItems) > recentMax {
		recentItems = recentItems[:recentMax]
	}
	n := len(recentItems)
	mu.Unlock()
	log.Printf("[notify] loaded %d recent items from disk", n)
}

func saveRecentLocked() {
	data, err := json.Marshal(recentItems)
	if err != nil {
		log.Printf("[notify] recent save marshal error: %v", err)
		return
	}
	if err := os.WriteFile(recentFile, data, 0644); err != nil {
		log.Printf("[notify] recent save write error: %v", err)
	}
}

// RecentItems returns a copy of the recent items list.
func GetRecentItems() []RecentItem {
	mu.Lock()
	defer mu.Unlock()
	out := make([]RecentItem, len(recentItems))
	copy(out, recentItems)
	return out
}

func init() {
	go logSender()
}

func logSender() {
	for msg := range logQueue {
		mu.Lock()
		url := webhookURL
		mu.Unlock()
		if url != "" {
			if err := sendWework(url, client, msg); err != nil {
				log.Printf("[notify] log push failed: %v", err)
			}
		}
		// Generic webhooks can subscribe to log lines too. Dispatching here
		// rather than in Logf means the 1/s throttle below applies to both
		// destinations, so a chatty log cannot flood a receiver.
		Dispatch(EventFromData(EventLog, "运行日志", msg, nil))
		time.Sleep(1 * time.Second)
	}
}

// SetWebhook configures the WeChat Work webhook URL.
func SetWebhook(url string) {
	mu.Lock()
	defer mu.Unlock()
	webhookURL = url
}

// SetTimezone sets the timezone for notification timestamps.
func SetTimezone(tz string) {
	mu.Lock()
	defer mu.Unlock()
	if tz == "" {
		tzLoc = nil
		return
	}
	loc, err := time.LoadLocation(tz)
	if err == nil {
		tzLoc = loc
	}
}

func now() time.Time {
	mu.Lock()
	loc := tzLoc
	mu.Unlock()
	if loc != nil {
		return time.Now().In(loc)
	}
	return time.Now()
}

// Webhook returns the current webhook URL.
func Webhook() string {
	mu.Lock()
	defer mu.Unlock()
	return webhookURL
}

// Send delivers a markdown notification to the WeChat Work channel.
//
// It is kept for the call sites that have not moved to Dispatch yet. New code
// should build an Event and call Dispatch, which also reaches generic webhooks.
func Send(content string, enabled bool) {
	if !enabled {
		return
	}
	mu.Lock()
	url := webhookURL
	mu.Unlock()
	if url == "" {
		return
	}
	go func() {
		if err := sendWework(url, client, content); err != nil {
			log.Printf("[notify] send failed: %v", err)
		}
	}()
}

// DispatchEvent enqueues an event, delivering it to every subscribed channel.
//
// The markdown body is sent to WeChat Work (when it is configured and the
// caller's feature toggle is on) and the whole event is sent to generic
// webhooks. Passing enabled=false suppresses only the WeChat Work delivery, so
// the pre-existing per-feature toggles keep their meaning while webhooks remain
// independently controllable through their own event subscriptions.
func DispatchEvent(ev Event, enabled bool) {
	if !enabled {
		// Webhooks are still told: their subscriptions are the gate, and the
		// legacy toggles only ever governed WeChat Work.
		Dispatch(ev)
		return
	}
	Dispatch(ev)
}

// EventFromMessage builds an event from a pre-formatted markdown message.
//
// It is the bridge for existing call sites: the markdown is reused as both the
// WeChat Work content and the webhook body, and the structured data is left to
// the caller when it has any.
func EventFromMessage(t EventType, title, body string) Event {
	return Event{Type: t, Time: now(), Title: title, Body: body}
}

// TaskSubmitted formats a message for magnet task submission.
func TaskSubmitted(count int, cid string, names []string) string {
	msg := fmt.Sprintf("## 离线任务已提交\n> **数量**: %d 个\n> **目标目录**: %s\n> **时间**: %s", count, cid, now().Format("15:04:05"))
	if len(names) > 0 {
		msg += "\n> **资源**:"
		for _, n := range names {
			msg += "\n> - " + n
		}
	}
	return msg
}

// TaskSubmittedEvent builds the event for a task submission.
func TaskSubmittedEvent(count int, cid string, names []string) Event {
	return EventFromData(EventTaskSubmitted, "离线任务已提交", TaskSubmitted(count, cid, names),
		map[string]interface{}{
			"count": count,
			"cid":   cid,
			"names": names,
		})
}

// TaskFailed formats a message for a failed task.
func TaskFailed(name string, cid string) string {
	return fmt.Sprintf(`## 离线任务失败
> **任务**: %s
> **目标目录**: %s
> **时间**: %s`, name, cid, now().Format("15:04:05"))
}

// TaskFailedEvent builds the event for a failed task.
func TaskFailedEvent(name, cid string) Event {
	return EventFromData(EventTaskFailed, "离线任务失败", TaskFailed(name, cid),
		map[string]interface{}{
			"name": name,
			"cid":  cid,
		})
}

// RSSFound formats a message when new items are found in RSS feed.
func RSSFound(subName string, count int, names []string) string {
	msg := fmt.Sprintf("## RSS 新资源\n> **订阅**: %s\n> **新增**: %d 条\n> **时间**: %s", subName, count, now().Format("15:04:05"))
	if len(names) > 0 {
		msg += "\n> **资源**:"
		for _, n := range names {
			msg += "\n> - " + n
		}
	}
	return msg
}

// RSSFoundEvent builds the event for new RSS resources.
func RSSFoundEvent(subName string, count int, names []string) Event {
	return EventFromData(EventRSSFound, "RSS 新资源", RSSFound(subName, count, names),
		map[string]interface{}{
			"sub":   subName,
			"count": count,
			"names": names,
		})
}

// ServerStarted formats a startup message.
func ServerStarted(port int) string {
	return fmt.Sprintf(`## pan-fetcher 已启动
> **端口**: %d
> **时间**: %s`, port, now().Format("2006-01-02 15:04:05"))
}

// ServerStartedEvent builds the event for server startup.
func ServerStartedEvent(port int) Event {
	return EventFromData(EventServerStarted, "pan-fetcher 已启动", ServerStarted(port),
		map[string]interface{}{"port": port})
}

// UpdateAvailableEvent builds the event for a new release.
func UpdateAvailableEvent(latest, current string) Event {
	body := "pan-fetcher 发现新版本 " + latest + "（当前 " + current + "），请前往设置页手动更新"
	return EventFromData(EventUpdateAvailable, "发现新版本", body,
		map[string]interface{}{
			"latest":  latest,
			"current": current,
		})
}

// SetLogEnabled enables or disables log push notifications.
func SetLogEnabled(enabled bool) {
	mu.Lock()
	defer mu.Unlock()
	logEnabled = enabled
}

// Logf enqueues a log line for push notification (1/s interval).
func Logf(format string, args ...interface{}) {
	mu.Lock()
	enabled := logEnabled
	mu.Unlock()
	if !enabled {
		return
	}
	msg := fmt.Sprintf(format, args...)
	content := fmt.Sprintf("## pan-fetcher 运行日志\n> %s\n> **时间**: %s", msg, now().Format("15:04:05"))
	select {
	case logQueue <- content:
	default:
		// queue full, drop oldest
		select {
		case <-logQueue:
		default:
		}
		logQueue <- content
	}
}
