package server

import (
	"strings"
	"testing"
)

// TestSplitNavLabel pins the split used to build the collapsed icon rail.
//
// Labels are stored in i18n as "<emoji> text", and the rail shows only the
// emoji. Getting this wrong makes either the icon or the whole label vanish,
// so the cases below cover the shapes the real labels actually have.
func TestSplitNavLabel(t *testing.T) {
	cases := []struct {
		label    string
		wantIcon string
		wantText string
	}{
		// The real labels, both languages.
		{"📊 仪表盘", "📊", "仪表盘"},
		{"📥 离线下载", "📥", "离线下载"},
		{"🔍 聚合搜索", "🔍", "聚合搜索"},
		{"📡 索引器", "📡", "索引器"},
		{"📂 文件管理", "📂", "文件管理"},
		{"📋 订阅管理", "📋", "订阅管理"},
		{"📜 运行日志", "📜", "运行日志"},
		{"⚙️ 设置", "⚙️", "设置"},
		{"🚪 退出", "🚪", "退出"},
		{"ℹ️ 关于", "ℹ️", "关于"},
		{"📊 Dashboard", "📊", "Dashboard"},
		{"⚙️ Settings", "⚙️", "Settings"},

		// A label with no icon must keep all of its text, otherwise the nav
		// entry would render blank in the rail.
		{"设置", "", "设置"},
		{"Settings", "", "Settings"},
		{"Log out", "", "Log out"},

		// Surrounding whitespace is trimmed both sides.
		{"  📊 仪表盘  ", "📊", "仪表盘"},

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
// an icon.
//
// Without the letter check, "Log out" would yield icon="Log" and text="out" —
// the rail would then show a fragment of a word instead of the label.
func TestSplitNavLabelRejectsWordAsIcon(t *testing.T) {
	icon, text := splitNavLabel("Log out")
	if icon != "" {
		t.Errorf("icon = %q, want empty for a word-prefixed label", icon)
	}
	if text != "Log out" {
		t.Errorf("text = %q, want the whole label", text)
	}

	// A digit-leading label is likewise not an icon.
	icon, text = splitNavLabel("115 网盘")
	if icon != "" || text != "115 网盘" {
		t.Errorf("splitNavLabel(\"115 网盘\") = (%q, %q)", icon, text)
	}
}

// TestNavLabelsAllHaveIcons verifies every navigation entry resolves to an icon
// and a label.
//
// The rail shows only the icon, so a missing one leaves a blank button that is
// findable only by hovering — effectively a dead link. A missing label leaves
// the expanded sidebar blank. Both are checked across both languages.
func TestNavLabelsAllHaveIcons(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	for _, k := range keys {
		svg := string(iconSVG(navIconKeys[k]))
		if svg == "" {
			t.Errorf("%s: no icon mapped, the rail would be blank", k)
			continue
		}
		// A well-formed inline icon: an <svg> with a viewBox and a path/shape.
		if !strings.Contains(svg, "<svg") || !strings.Contains(svg, "viewBox=\"0 0 24 24\"") {
			t.Errorf("%s: icon is not a 24x24 svg: %s", k, svg)
		}
		if !strings.Contains(svg, "stroke=\"currentColor\"") {
			t.Errorf("%s: icon does not inherit colour, so it cannot be themed", k)
		}
	}

	for _, lang := range []string{"zh", "en"} {
		for _, k := range keys {
			label := tr(lang, k)
			if label == "" {
				t.Errorf("%s/%s: missing translation", lang, k)
				continue
			}
			_, text := splitNavLabel(label)
			if text == "" {
				t.Errorf("%s/%s: label %q has no text, so the expanded sidebar would be blank", lang, k, label)
			}
		}
	}
}

// TestNavIconsAreDistinct verifies no two navigation entries share an icon.
//
// Two identical icons in a rail with no visible labels would be
// indistinguishable, which defeats the purpose of showing icons at all.
func TestNavIconsAreDistinct(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	seen := map[string]string{}
	for _, k := range keys {
		svg := string(iconSVG(navIconKeys[k]))
		if prev, ok := seen[svg]; ok {
			t.Errorf("%s and %s share the same icon", prev, k)
		}
		seen[svg] = k
	}
}

// TestIconSVGUnknownKey covers the fallback: a missing icon must not panic or
// emit broken markup.
func TestIconSVGUnknownKey(t *testing.T) {
	if got := iconSVG("does-not-exist"); got != "" {
		t.Errorf("unknown key should render nothing, got %q", got)
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
