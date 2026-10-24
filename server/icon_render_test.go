package server

import (
	"strings"
	"testing"
)

// TestNoSVGAssignedToTextContent guards a rendering bug that is easy to miss.
//
// textContent escapes its argument: assigning SVG markup to it shows the markup
// as literal text, so the panel displayed a raw
// <svg class="ico ...">...</svg> string in front of the message instead of an
// icon. The template helper renders real markup, so those assignments have to
// use innerHTML.
//
// This is the check that would have caught it: any statement that puts an icon
// into textContent is wrong by construction.
func TestNoSVGAssignedToTextContent(t *testing.T) {
	src := readSource(t, templateSourceFile)

	for ln, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
			continue
		}
		if !strings.Contains(line, "textContent") {
			continue
		}
		// An icon may be built by the template helper or by the JS ICO().
		if strings.Contains(line, "{{ico") || strings.Contains(line, "ICO(") || strings.Contains(line, "icoSpin") {
			t.Errorf("templates.go:%d assigns markup to textContent, which renders it literally: %s",
				ln+1, trimmed)
		}
	}
}

// TestIconAssignmentsUseInnerHTML verifies the specific update-status messages
// use innerHTML.
//
// These are the ones the user reported: the "already up to date" line showed a
// raw <svg> tag before the text.
func TestIconAssignmentsUseInnerHTML(t *testing.T) {
	src := readSource(t, templateSourceFile)

	for _, key := range []string{
		"update_already_latest",
		"update_fetch_failed",
		"net_error",
	} {
		found := false
		for _, line := range strings.Split(src, "\n") {
			if !strings.Contains(line, key) || !strings.Contains(line, "{{ico") {
				continue
			}
			found = true
			if !strings.Contains(line, "innerHTML") {
				t.Errorf("the message built for %q must use innerHTML, not textContent: %s",
					key, strings.TrimSpace(line))
			}
		}
		if !found {
			t.Errorf("no icon assignment found for %q; the check would silently pass", key)
		}
	}
}
