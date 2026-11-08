package notify

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// longNames builds n names of the given rune length.
func longNames(n, runes int) []string {
	out := make([]string, n)
	body := strings.Repeat("测", runes)
	for i := range out {
		out[i] = body
	}
	return out
}

// TestTaskSubmittedStaysUnderLimit is the regression test for a rejected
// notification.
//
// WeChat Work returns 40058 when markdown content exceeds 4096 bytes. Listing
// every name of a large batch blew past that, so the whole delivery failed.
func TestTaskSubmittedStaysUnderLimit(t *testing.T) {
	cases := []struct {
		name  string
		names []string
	}{
		{"20 long names", longNames(20, 80)},
		{"200 long names", longNames(200, 80)},
		{"1 huge name", longNames(1, 5000)},
	}
	for _, c := range cases {
		got := TaskSubmitted(len(c.names), "123", c.names)
		if len(got) > weworkContentLimit {
			t.Errorf("%s: content is %d bytes, limit is %d", c.name, len(got), weworkContentLimit)
		}
		if !utf8.ValidString(got) {
			t.Errorf("%s: content is not valid UTF-8", c.name)
		}
	}
}

// TestTaskSubmittedReportsOmitted verifies the reader is told the list is
// partial, rather than silently believing it saw everything.
func TestTaskSubmittedReportsOmitted(t *testing.T) {
	names := longNames(200, 80)
	got := TaskSubmitted(len(names), "1", names)

	if !strings.Contains(got, "未列出") {
		t.Error("truncated list should say how many entries were omitted")
	}
	if !strings.Contains(got, "另有") {
		t.Error("truncated list should carry an 'and N more' line")
	}
	// The header still has to report the real total, not the listed count.
	if !strings.Contains(got, "200") {
		t.Error("the header should report the true count")
	}
}

// TestTaskSubmittedShortListIntact verifies small batches lose nothing.
func TestTaskSubmittedShortListIntact(t *testing.T) {
	names := []string{"第一集", "第二集", "第三集"}
	got := TaskSubmitted(3, "1", names)

	for _, n := range names {
		if !strings.Contains(got, n) {
			t.Errorf("name %q missing from a short message", n)
		}
	}
	if strings.Contains(got, "未列出") {
		t.Error("a short list should not claim anything was omitted")
	}
}

// TestRSSFoundStaysUnderLimit covers the same limit on the feed path.
//
// A subscription can deliver many new items at once, which fails the same way.
func TestRSSFoundStaysUnderLimit(t *testing.T) {
	names := longNames(150, 80)
	got := RSSFound("某订阅", len(names), names)

	if len(got) > weworkContentLimit {
		t.Errorf("content is %d bytes, limit is %d", len(got), weworkContentLimit)
	}
	if !utf8.ValidString(got) {
		t.Error("content is not valid UTF-8")
	}
	if !strings.Contains(got, "未列出") {
		t.Error("should report omitted entries")
	}
}

// TestNoNamesOmitsList verifies an empty list produces no dangling heading.
func TestNoNamesOmitsList(t *testing.T) {
	for _, got := range []string{
		TaskSubmitted(2, "1", nil),
		RSSFound("x", 2, nil),
	} {
		if strings.Contains(got, "**资源**") {
			t.Errorf("empty name list should not render a resource heading: %q", got)
		}
	}
}

// TestClipRunesKeepsValidUTF8 verifies truncation never splits a character.
//
// Slicing a string by bytes would cut a multi-byte rune in half and produce
// replacement characters in the notification.
func TestClipRunesKeepsValidUTF8(t *testing.T) {
	for _, n := range []int{1, 5, 50, 99} {
		got := clipRunes(strings.Repeat("测试", 100), n)
		if !utf8.ValidString(got) {
			t.Errorf("clip to %d runes produced invalid UTF-8: %q", n, got)
		}
		if utf8.RuneCountInString(got) > n+1 { // +1 for the ellipsis
			t.Errorf("clip to %d runes returned %d runes", n, utf8.RuneCountInString(got))
		}
	}
}

// TestClipRunesLeavesShortStrings verifies no ellipsis is added needlessly.
func TestClipRunesLeavesShortStrings(t *testing.T) {
	const s = "短"
	if got := clipRunes(s, 10); got != s {
		t.Errorf("short string was modified: %q", got)
	}
}
