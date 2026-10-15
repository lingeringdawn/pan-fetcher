package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
)

// weworkMsg is the WeChat Work markdown message payload.
type weworkMsg struct {
	MsgType  string   `json:"msgtype"`
	Markdown weworkMD `json:"markdown"`
}

type weworkMD struct {
	Content string `json:"content"`
}

// weworkChannel delivers markdown to a WeChat Work group-bot webhook.
//
// It is gated by the pre-existing per-feature toggles in web-settings.json
// (notify_task / notify_rss / notify_log) rather than by an event subscription,
// so an upgrade does not suddenly change what gets sent.
//
// Log events are deliberately excluded from its subscription list. They are
// delivered by the log pusher instead, which owns the per-second throttle and
// the notify_log toggle; subscribing here as well would send every log line
// twice.
type weworkChannel struct {
	url    string
	client *http.Client
}

func (w *weworkChannel) Name() string { return "企业微信" }

func (w *weworkChannel) Events() []EventType {
	out := make([]EventType, 0, len(AllEventTypes()))
	for _, t := range AllEventTypes() {
		if t == EventLog {
			continue
		}
		out = append(out, t)
	}
	return out
}

func (w *weworkChannel) Send(ev Event) error {
	if strings.TrimSpace(w.url) == "" {
		return nil // not configured; nothing to do
	}
	return sendWework(w.url, w.client, ev.Body)
}

// sendWework posts markdown content to a WeChat Work webhook.
func sendWework(url string, client *http.Client, content string) error {
	msg := weworkMsg{
		MsgType:  "markdown",
		Markdown: weworkMD{Content: content},
	}
	body, err := json.Marshal(&msg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return nil
}

// testWework verifies a WeChat Work webhook URL.
func testWework(url string, client *http.Client) error {
	thisURL := strings.TrimSpace(url)
	if thisURL == "" {
		return fmt.Errorf("企业微信 Webhook 为空")
	}
	content := fmt.Sprintf("## pan-fetcher 通知测试 ✅\n> 时间: %s\n> Webhook 配置成功", now().Format("15:04:05"))
	if err := sendWework(thisURL, client, content); err != nil {
		return err
	}
	log.Printf("[notify] wework test ok: %s", redactURL(thisURL))
	return nil
}
