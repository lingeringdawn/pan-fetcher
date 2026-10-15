package notify

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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

// sendWework posts markdown content to a WeChat Work webhook.
//
// WeChat Work is not a generic webhook: it expects this fixed envelope and
// rejects anything else, which is why the channel carries a kind rather than
// always sending the event JSON.
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

// isWeworkURL reports whether a URL looks like a WeChat Work group-bot webhook.
//
// It is used only by migration, to decide whether a legacy URL should be given
// the WeChat Work kind. Matching on the host keeps a custom reverse proxy on
// the same domain working, and a URL that does not match simply becomes a
// generic webhook — where the user can change the kind if that is wrong.
func isWeworkURL(raw string) bool {
	u := strings.ToLower(strings.TrimSpace(raw))
	return strings.Contains(u, "qyapi.weixin.qq.com") || strings.Contains(u, "/cgi-bin/webhook/send")
}
