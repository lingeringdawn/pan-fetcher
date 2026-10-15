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

// weworkResp is WeChat Work's reply.
//
// The endpoint answers HTTP 200 even when it rejects the message, putting the
// outcome in errcode. Treating the status code alone as success made a rejected
// send look like a delivered notification — which is exactly what the settings
// page test button reports, so the button said "sent" while nothing arrived.
type weworkResp struct {
	ErrCode int    `json:"errcode"`
	ErrMsg  string `json:"errmsg"`
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

	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	// A 200 with a non-zero errcode is still a failure.
	var r weworkResp
	if err := json.Unmarshal(raw, &r); err == nil && r.ErrCode != 0 {
		return fmt.Errorf("企业微信返回错误 %d: %s%s", r.ErrCode, r.ErrMsg, weworkErrHint(r.ErrCode))
	}
	return nil
}

// weworkErrHint explains the codes users actually hit, so the panel reports
// something actionable instead of a bare number.
func weworkErrHint(code int) string {
	switch code {
	case 93000:
		return "（Webhook 地址无效或已失效，请在群机器人设置中重新获取）"
	case 45009:
		return "（触发频率限制，请稍后重试）"
	case 40001:
		return "（令牌无效，请检查地址中的 key）"
	case 40058:
		return "（请求参数错误）"
	}
	return ""
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
