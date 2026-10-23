package server

import (
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

// TestNavLabelsAllHaveIcons verifies every navigation label the sidebar renders
// carries an icon, which the rail depends on.
//
// A label without one would leave an empty button in the collapsed sidebar —
// visible only by hovering, which is effectively a dead link.
func TestNavLabelsAllHaveIcons(t *testing.T) {
	keys := []string{
		"dashboard", "home", "indexer_search", "indexers",
		"files", "subs", "runtime_log", "settings", "logout", "about",
	}
	for _, lang := range []string{"zh", "en"} {
		for _, k := range keys {
			label := tr(lang, k)
			if label == "" {
				t.Errorf("%s/%s: missing translation", lang, k)
				continue
			}
			icon, text := splitNavLabel(label)
			if icon == "" {
				t.Errorf("%s/%s: label %q has no icon, so the rail would be blank", lang, k, label)
			}
			if text == "" {
				t.Errorf("%s/%s: label %q has no text, so the expanded sidebar would be blank", lang, k, label)
			}
		}
	}
}
