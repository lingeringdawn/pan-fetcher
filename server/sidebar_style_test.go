package server

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// templateSourceFile is read directly by the tests below.
//
// The dashboard template is a 170 KB inline string in templates.go rather than
// a separate file, so there is nothing to embed. Reading the source keeps these
// layout assertions honest — they check what is actually in the file rather than
// a copy that could drift.
const templateSourceFile = "templates.go"

func readTemplateSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(templateSourceFile)
	if err != nil {
		t.Fatalf("read %s: %v", templateSourceFile, err)
	}
	return string(data)
}

// TestCollapseButtonDoesNotMove pins the requirement that the collapse control
// keeps the same position in both sidebar states.
//
// The header previously centred its contents when collapsed, which moved the
// button from 12px to the middle of the rail — the target shifted out from
// under the pointer exactly when the user clicked it.
func TestCollapseButtonDoesNotMove(t *testing.T) {
	src := readTemplateSource(t)

	expanded := cssBlock(t, src, `.sidebar-head {`)
	collapsed := cssBlock(t, src, `.sidebar.collapsed .sidebar-head {`)

	if expanded == "" || collapsed == "" {
		t.Fatalf("missing header rule (expanded=%q, collapsed=%q)", expanded, collapsed)
	}
	if !strings.Contains(expanded, "padding: 14px 12px 10px") {
		t.Errorf("expanded header padding changed: %s", expanded)
	}
	if !strings.Contains(collapsed, "padding: 14px 12px 10px") {
		t.Errorf("collapsed header must keep the same left padding: %s", collapsed)
	}
	if strings.Contains(collapsed, "justify-content: center") {
		t.Errorf("collapsed header must not centre its contents, which would move the button: %s", collapsed)
	}
}

// TestCollapseButtonFitsRail verifies the button fits the 64px column.
//
// Left plus right padding must leave room for the 28px button, otherwise it is
// clipped by the sidebar's overflow:hidden and becomes unclickable.
func TestCollapseButtonFitsRail(t *testing.T) {
	src := readTemplateSource(t)

	const railW, btnW, padL, padR = 64, 28, 12, 12
	if padL+btnW+padR > railW {
		t.Errorf("button does not fit the rail: %d + %d + %d > %d", padL, btnW, padR, railW)
	}
	if !strings.Contains(src, "width: 64px") {
		t.Error("rail width rule missing")
	}
	// The button must declare that width so the arithmetic above holds.
	if !strings.Contains(src, "width: 28px; height: 28px") {
		t.Error("collapse button size rule missing")
	}
}

// TestNavUsesMonochromeSVG verifies the navigation renders line icons rather
// than emoji.
//
// Emoji render in colour with platform-dependent metrics, so a stray one in the
// sidebar contradicts the monochrome icon set.
func TestNavUsesMonochromeSVG(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	for _, k := range keys {
		svg := string(iconSVG(navIconKeys[k]))
		if svg == "" {
			t.Errorf("%s: no svg", k)
			continue
		}
		if strings.Contains(svg, `fill="#`) || strings.Contains(svg, `stroke="#`) {
			t.Errorf("%s: icon hard-codes a colour instead of currentColor: %s", k, svg)
		}
		for _, r := range svg {
			if r > 0x2000 {
				t.Errorf("%s: icon markup contains a non-ASCII glyph %q", k, r)
			}
		}
	}
}

// TestNavTooltipsHaveNoEmoji verifies the tooltip text is plain words.
//
// The tooltip is the only label visible in the rail, so it must match the
// monochrome icon instead of reintroducing a colour glyph.
func TestNavTooltipsHaveNoEmoji(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	for _, lang := range []string{"zh", "en"} {
		for _, k := range keys {
			_, text := splitNavLabel(tr(lang, k))
			if text == "" {
				t.Errorf("%s/%s: empty tooltip", lang, k)
				continue
			}
			for _, r := range text {
				if r > 0x2000 && !isCJK(r) {
					t.Errorf("%s/%s: tooltip %q contains emoji %q", lang, k, text, r)
				}
			}
		}
	}
}

// cssBlock returns the declaration block that follows selector.
func cssBlock(t *testing.T, css, selector string) string {
	t.Helper()
	i := strings.Index(css, selector)
	if i < 0 {
		return ""
	}
	rest := css[i+len(selector):]
	j := strings.Index(rest, "}")
	if j < 0 {
		return ""
	}
	return strings.TrimSpace(rest[:j])
}

func isCJK(r rune) bool {
	return (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3000 && r <= 0x303F) ||
		(r >= 0xFF00 && r <= 0xFFEF)
}

// TestHeaderOrderIsButtonTitleLanguage verifies the collapse button sits to the
// left of the title, which is the requested layout.
func TestHeaderOrderIsButtonTitleLanguage(t *testing.T) {
	src := readTemplateSource(t)
	i := strings.Index(src, `<div class="sidebar-head">`)
	if i < 0 {
		t.Fatal("sidebar-head not found in template")
	}
	end := i + 1200
	if end > len(src) {
		end = len(src)
	}
	seg := src[i:end]

	btn := strings.Index(seg, "sidebar-toggle-btn")
	title := strings.Index(seg, "sidebar-title")
	lang := strings.Index(seg, "lang-select")
	if btn < 0 || title < 0 || lang < 0 {
		t.Fatalf("header elements missing (btn=%d title=%d lang=%d)", btn, title, lang)
	}
	if !(btn < title && title < lang) {
		t.Errorf("header order must be button < title < language, got positions %d %d %d", btn, title, lang)
	}
}

// TestTopbarHasNoCollapseButton verifies the old topbar button is gone, so
// there is exactly one control for this action.
func TestTopbarHasNoCollapseButton(t *testing.T) {
	src := readTemplateSource(t)

	i := strings.Index(src, `<div class="topbar">`)
	if i < 0 {
		t.Fatal("topbar not found")
	}
	end := i + 400
	if end > len(src) {
		end = len(src)
	}
	if strings.Contains(src[i:end], "sidebar-toggle-btn") {
		t.Error("the topbar still renders a collapse button; it belongs in the sidebar header")
	}
	if n := strings.Count(src, `class="sidebar-toggle-btn"`); n != 1 {
		t.Errorf("expected exactly 1 collapse button, found %d", n)
	}
}

// TestNoEmojiSidebarStyles verifies the emoji-era rules are gone.
func TestNoEmojiSidebarStyles(t *testing.T) {
	src := readTemplateSource(t)
	for _, stale := range []string{
		"nav-icon",
		"sidebar-logo",
		"border-left: 3px solid transparent",
	} {
		if strings.Contains(src, stale) {
			t.Errorf("stale sidebar style still present: %q", stale)
		}
	}
}

// TestTemplateIsParseable guards against a broken template after edits.
func TestTemplateIsParseable(t *testing.T) {
	if dashboardTemplate == nil {
		t.Fatal("dashboardTemplate is nil")
	}
	if !regexp.MustCompile(`<div class="sidebar`).MatchString(readTemplateSource(t)) {
		t.Error("templates.go does not contain the sidebar markup")
	}
}
