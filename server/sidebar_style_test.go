package server

import (
	"os"
	"strings"
	"testing"
)

const templateSourceFile = "templates.go"
const i18nSourceFile = "i18n.go"

func readSource(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(name)
	if err != nil {
		t.Fatalf("read %s: %v", name, err)
	}
	return string(data)
}

// TestCollapseButtonDoesNotMove pins the requirement that the collapse control
// keeps the same position in both sidebar states, on both axes.
//
// Horizontally: the header centred its contents when collapsed, moving the
// button from the left padding to the middle of the rail.
// Vertically: the search box was still rendered in the rail, where its
// placeholder wrapped onto several lines and pushed the row taller.
func TestCollapseButtonDoesNotMove(t *testing.T) {
	src := readSource(t, templateSourceFile)

	expanded := cssBlock(t, src, `.sidebar-head {`)
	collapsed := cssBlock(t, src, `.sidebar.collapsed .sidebar-head {`)

	if expanded == "" || collapsed == "" {
		t.Fatalf("missing header rule (expanded=%q, collapsed=%q)", expanded, collapsed)
	}
	// Same horizontal padding in both states: the button's left edge is fixed.
	if !strings.Contains(expanded, "padding: 0 12px") {
		t.Errorf("expanded header padding changed: %s", expanded)
	}
	if !strings.Contains(collapsed, "padding: 0 12px") {
		t.Errorf("collapsed header must keep the same padding: %s", collapsed)
	}
	// A fixed height makes the row identical whether the language select is
	// present or not, so the button cannot shift vertically.
	if !strings.Contains(expanded, "height: 52px") {
		t.Errorf("header needs a fixed height so it does not change with the language control: %s", expanded)
	}
	if strings.Contains(collapsed, "justify-content: center") {
		t.Errorf("collapsed header must not centre its contents: %s", collapsed)
	}
}

// TestSearchHiddenInRail verifies the search box is removed when collapsed.
//
// It cannot fit in 64px; left in, its placeholder wraps and changes the height
// of everything below the header.
func TestSearchHiddenInRail(t *testing.T) {
	src := readSource(t, templateSourceFile)

	rule := cssBlock(t, src, `.sidebar.collapsed .sidebar-label,`)
	// The selector list spans several lines before the brace, so look at the
	// whole rule including what precedes the block.
	i := strings.Index(src, ".sidebar.collapsed .sidebar-label")
	if i < 0 {
		t.Fatal("rail hide rule not found")
	}
	seg := src[i : i+400]
	if !strings.Contains(seg, ".sidebar.collapsed .sidebar-search") {
		t.Errorf("the rail must hide .sidebar-search, otherwise it wraps and shifts the layout")
	}
	_ = rule
}

// TestCollapseButtonFitsRail verifies the button fits the 64px column.
func TestCollapseButtonFitsRail(t *testing.T) {
	src := readSource(t, templateSourceFile)

	const railW, btnW, padL, padR = 64, 28, 12, 12
	if padL+btnW+padR > railW {
		t.Errorf("button does not fit the rail: %d + %d + %d > %d", padL, btnW, padR, railW)
	}
	if !strings.Contains(src, "width: 64px") {
		t.Error("rail width rule missing")
	}
	if !strings.Contains(src, "width: 28px; height: 28px") {
		t.Error("collapse button size rule missing")
	}
}

// TestIconsAreMonochromeSVG verifies every icon is drawn with currentColor and
// no emoji, which is what makes the set monochrome and themeable.
func TestIconsAreMonochromeSVG(t *testing.T) {
	for key, body := range navIconSVG {
		if strings.Contains(body, `fill="#`) || strings.Contains(body, `stroke="#`) {
			t.Errorf("%s: hard-codes a colour instead of currentColor", key)
		}
		for _, r := range body {
			if r > 0x2000 {
				t.Errorf("%s: icon markup contains a non-ASCII glyph %q", key, r)
			}
		}
	}
	if len(navIconSVG) < 20 {
		t.Errorf("icon table looks too small: %d entries", len(navIconSVG))
	}
}

// TestNavIconKeysCoverNavEntries verifies the two tables stay in step.
func TestNavIconKeysCoverNavEntries(t *testing.T) {
	for _, k := range []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	} {
		icon, ok := navIconKeys[k]
		if !ok {
			t.Errorf("navIconKeys is missing %q", k)
			continue
		}
		if _, ok := navIconSVG[icon]; !ok {
			t.Errorf("navIconKeys[%q] = %q, which is not in navIconSVG", k, icon)
		}
	}
}

// TestNavIconsAreDistinct verifies no two navigation entries share an icon.
func TestNavIconsAreDistinct(t *testing.T) {
	seen := map[string]string{}
	for _, k := range []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	} {
		svg := string(iconSVG(navIconKeys[k]))
		if prev, ok := seen[svg]; ok {
			t.Errorf("%s and %s share the same icon", prev, k)
		}
		seen[svg] = k
	}
}

// TestIconSVGUnknownKey covers the fallback.
func TestIconSVGUnknownKey(t *testing.T) {
	if got := iconSVG("does-not-exist"); got != "" {
		t.Errorf("unknown key should render nothing, got %q", got)
	}
}

// TestNoColourEmojiInSources verifies neither the template nor the translations
// still contain a colour emoji.
//
// The requirement is a monochrome interface: an emoji anywhere renders in colour
// with platform-specific metrics, next to icons that do not.
func TestNoColourEmojiInSources(t *testing.T) {
	check := func(name, src string) {
		for ln, line := range strings.Split(src, "\n") {
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "//") || strings.HasPrefix(trimmed, "/*") {
				continue // documentation may quote an example
			}
			for _, r := range line {
				if isColourEmoji(r) {
					t.Errorf("%s:%d contains emoji %q: %s", name, ln+1, r, trimmed)
					break
				}
			}
		}
	}
	check(templateSourceFile, readSource(t, templateSourceFile))
	check(i18nSourceFile, readSource(t, i18nSourceFile))
}

func isColourEmoji(r rune) bool {
	switch {
	case r >= 0x1F000 && r <= 0x1FAFF: // pictographs
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
		return true
	case r == 0x23F3, r == 0x25B6, r == 0x270E, r == 0x2715: // hourglass, triangle, pencil, cross
		return true
	}
	return false
}

// TestHeaderOrderIsButtonTitleLanguage verifies the collapse button sits left of
// the title, which is the requested layout.
func TestHeaderOrderIsButtonTitleLanguage(t *testing.T) {
	src := readSource(t, templateSourceFile)
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
		t.Errorf("header order must be button < title < language, got %d %d %d", btn, title, lang)
	}
}

// TestTopbarHasNoCollapseButton verifies there is exactly one collapse control.
func TestTopbarHasNoCollapseButton(t *testing.T) {
	src := readSource(t, templateSourceFile)

	i := strings.Index(src, `<div class="topbar">`)
	if i < 0 {
		t.Fatal("topbar not found")
	}
	end := i + 400
	if end > len(src) {
		end = len(src)
	}
	if strings.Contains(src[i:end], "sidebar-toggle-btn") {
		t.Error("the topbar still renders a collapse button")
	}
	if n := strings.Count(src, `class="sidebar-toggle-btn"`); n != 1 {
		t.Errorf("expected exactly 1 collapse button, found %d", n)
	}
}

// TestNoEmojiSidebarStyles verifies the emoji-era rules are gone.
func TestNoEmojiSidebarStyles(t *testing.T) {
	src := readSource(t, templateSourceFile)
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
