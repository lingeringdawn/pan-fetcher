package server

import (
	"testing"
)

// Tests for the label/icon split and the navigation icon wiring.
//
// The layout and monochrome-SVG assertions live in sidebar_style_test.go.

// TestSplitNavLabel pins the split used when a label still carries an icon.
//
// The i18n strings no longer include an emoji, but the splitter is the single
// place that knows the format, and a hand-written or restored label may still
// start with one.
func TestSplitNavLabel(t *testing.T) {
	cases := []struct {
		label    string
		wantIcon string
		wantText string
	}{
		// The real labels after the emoji were removed.
		{"仪表盘", "", "仪表盘"},
		{"离线下载", "", "离线下载"},
		{"Dashboard", "", "Dashboard"},
		{"Settings", "", "Settings"},

		// A label that still starts with an icon is split.
		{"📊 仪表盘", "📊", "仪表盘"},
		{"⚙️ 设置", "⚙️", "设置"},
		{"ℹ️ 关于", "ℹ️", "关于"},
		{"📊 Dashboard", "📊", "Dashboard"},

		// Surrounding whitespace is trimmed.
		{"  仪表盘  ", "", "仪表盘"},

		// Empty input.
		{"", "", ""},
		{"   ", "", ""},
	}
	for _, c := range cases {
		icon, text := splitNavLabel(c.label)
		if icon != c.wantIcon || text != c.wantText {
			t.Errorf("splitNavLabel(%q) = (%q, %q), want (%q, %q)",
				c.label, icon, text, c.wantIcon, c.wantText)
		}
	}
}

// TestSplitNavLabelRejectsWordAsIcon verifies a leading word is not mistaken for
// an icon: "Log out" must not become icon="Log", text="out".
func TestSplitNavLabelRejectsWordAsIcon(t *testing.T) {
	if icon, text := splitNavLabel("Log out"); icon != "" || text != "Log out" {
		t.Errorf("splitNavLabel(\"Log out\") = (%q, %q)", icon, text)
	}
	if icon, text := splitNavLabel("115 网盘"); icon != "" || text != "115 网盘" {
		t.Errorf("splitNavLabel(\"115 网盘\") = (%q, %q)", icon, text)
	}
}

// TestNavLabelsTranslate verifies every navigation key has a non-empty label in
// both languages.
func TestNavLabelsTranslate(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	for _, lang := range []string{"zh", "en"} {
		for _, k := range keys {
			if tr(lang, k) == "" {
				t.Errorf("%s/%s: missing translation", lang, k)
			}
		}
	}
}
