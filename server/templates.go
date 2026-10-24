package server

import (
	"html/template"
	"strings"
	"unicode"
)


// splitNavLabel separates a label into a leading icon and the remaining text.
//
// The i18n strings no longer carry an emoji — the icon set is drawn by the
// templates — so in practice this now returns an empty icon and the whole label.
// It is kept because it is the single place that knows how a label is split, and
// because a label written by hand (or restored from an older build) may still
// start with one; the split keeps that from leaking into the monochrome UI.
func splitNavLabel(label string) (icon, text string) {
	label = strings.TrimSpace(label)
	if label == "" {
		return "", ""
	}
	i := strings.IndexAny(label, " \t")
	if i <= 0 {
		// No space: the whole label is text.
		return "", label
	}
	candidate := label[:i]
	if !isIconSequence(candidate) {
		return "", label
	}
	return candidate, strings.TrimSpace(label[i:])
}

// isIconSequence reports whether s looks like an icon rather than a word.
func isIconSequence(s string) bool {
	runes := []rune(s)
	for idx := 0; idx < len(runes); idx++ {
		r := runes[idx]
		if isIconRune(r) {
			continue
		}
		// A letter is normally a word, but some icons are letters followed by
		// VARIATION SELECTOR-16, which is how emoji presentation is spelled.
		// "ℹ️" is exactly that: U+2139 INFORMATION SOURCE (a letter) + U+FE0F.
		if idx+1 < len(runes) && runes[idx+1] == '\uFE0F' {
			continue
		}
		return false
	}
	return true
}

// isIconRune reports whether r belongs to an icon rather than a word.
//
// Letters are rejected so a label such as "Log out" keeps its leading word.
// The ranges below cover the symbols and pictographs the navigation uses;
// anything else falls through to a category test that keeps marks and symbols
// while rejecting letters, digits, punctuation and spaces.
func isIconRune(r rune) bool {
	switch {
	case r == '\uFE0F', r == '\uFE0E': // variation selectors 16 / 15
		return true
	case r == '\u200D': // zero-width joiner
		return true
	case r >= 0x1F000 && r <= 0x1FAFF: // emoji and pictographs
		return true
	case r >= 0x1F3FB && r <= 0x1F3FF: // skin-tone modifiers
		return true
	case r >= 0x2190 && r <= 0x2BFF: // arrows, misc symbols, dingbats
		return true
	case r >= 0x2600 && r <= 0x27BF: // misc symbols and dingbats
		return true
	}
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return false
	}
	return !unicode.IsPunct(r) && !unicode.IsSpace(r)
}

// navIconSVG holds the monochrome line icons used by the sidebar and the
// collapse button.
//
// They are inline SVG rather than emoji for two reasons: emoji render in colour
// and differ per platform, which fights a monochrome UI; and their metrics vary
// wildly, so an icon rail built from them never lines up. Strokes use
// currentColor so a single CSS rule themes them, and every path is drawn on a
// 24x24 grid with 1.75 stroke width for a consistent weight.
var navIconSVG = map[string]string{
	"dashboard": `<path d="M3 10.5 12 3l9 7.5"/><path d="M5 9.5V21h14V9.5"/><path d="M9.5 21v-6h5v6"/>`,
	"home":      `<path d="M4 17V7l8-4 8 4v10l-8 4-8-4Z"/><path d="M4 7l8 4 8-4"/><path d="M12 11v10"/>`,
	"search":    `<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>`,
	"indexers":  `<circle cx="12" cy="12" r="2"/><path d="M7.8 7.8a6 6 0 0 0 0 8.4"/><path d="M16.2 16.2a6 6 0 0 0 0-8.4"/><path d="M4.9 4.9a10 10 0 0 0 0 14.2"/><path d="M19.1 19.1a10 10 0 0 0 0-14.2"/>`,
	"files":     `<path d="M3 7.5A2 2 0 0 1 5 5.5h4l2 2.5h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>`,
	"subs":      `<rect x="3.5" y="5" width="17" height="14.5" rx="2.5"/><path d="M7.5 3.5v3"/><path d="M16.5 3.5v3"/><path d="M3.5 10h17"/>`,
	"log":       `<path d="M6 3.5h9l4 4V20a.5.5 0 0 1-.5.5h-12A.5.5 0 0 1 6 20V4a.5.5 0 0 1 .5-.5Z"/><path d="M15 3.5V8h4"/><path d="M9 12h6"/><path d="M9 16h4"/>`,
	"settings":  `<circle cx="12" cy="12" r="3"/><path d="M12 2.8v2.4M12 18.8v2.4M4.3 7.5l2 1.2M17.7 15.3l2 1.2M4.3 16.5l2-1.2M17.7 8.7l2-1.2"/>`,
	"logout":    `<path d="M15 4.5h3.5a1 1 0 0 1 1 1v13a1 1 0 0 1-1 1H15"/><path d="M10 8.5 6.5 12 10 15.5"/><path d="M6.5 12h8"/>`,
	"about":     `<circle cx="12" cy="12" r="8.5"/><path d="M12 11v6"/><circle cx="12" cy="8" r="0.9" fill="currentColor" stroke="none"/>`,
	"menu":      `<path d="M4 7h16"/><path d="M4 12h16"/><path d="M4 17h16"/>`,

	// Interface icons used outside the sidebar. Same 24x24 grid and stroke
	// weight as the navigation set, so the whole panel reads as one family.
	"tmdb":       `<rect x="3" y="6" width="18" height="13" rx="2"/><path d="M3 10h18"/><path d="M8 6v3"/><path d="M16 6v3"/><path d="M8 13h2"/><path d="M14 13h2"/><path d="M8 16h2"/><path d="M14 16h2"/>`,
	"tv":         `<rect x="3" y="7.5" width="18" height="12" rx="2"/><path d="m8 3.5 4 4 4-4"/>`,
	"download":   `<path d="M12 4v10"/><path d="m7.5 10 4.5 4.5L16.5 10"/><path d="M4 19h16"/>`,
	"play":       `<path d="M7.5 5.5v13l11-6.5-11-6.5Z"/>`,
	"edit":       `<path d="M4 20h4L20 8l-4-4L4 16v4Z"/><path d="m14.5 5.5 4 4"/>`,
	"copy":       `<rect x="8.5" y="8.5" width="11" height="11" rx="2"/><path d="M15.5 8.5v-3a2 2 0 0 0-2-2h-7a2 2 0 0 0-2 2v7a2 2 0 0 0 2 2h3"/>`,
	"close":      `<path d="m6 6 12 12"/><path d="m18 6-12 12"/>`,
	"warning":    `<path d="M12 4.5 21 19.5H3L12 4.5Z"/><path d="M12 10v4"/><circle cx="12" cy="16.6" r="0.9" fill="currentColor" stroke="none"/>`,
	"lock":       `<rect x="4.5" y="10" width="15" height="10" rx="2"/><path d="M8 10V7a4 4 0 0 1 8 0v3"/>`,
	"loading":    `<path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5"/>`,
	"link":       `<path d="M10 13.5a4 4 0 0 0 5.7 0l2.8-2.8a4 4 0 0 0-5.7-5.7L11.5 6"/><path d="M14 10.5a4 4 0 0 0-5.7 0l-2.8 2.8a4 4 0 0 0 5.7 5.7L12.5 18"/>`,
	"user":       `<circle cx="12" cy="8.5" r="3.5"/><path d="M4.5 20a7.5 7.5 0 0 1 15 0"/>`,
	"external":   `<path d="M14 4h6v6"/><path d="M20 4 11 13"/><path d="M18 14v5a1 1 0 0 1-1 1H5a1 1 0 0 1-1-1V7a1 1 0 0 1 1-1h5"/>`,
	"bell":       `<path d="M6 9a6 6 0 0 1 12 0c0 4 1.5 5.5 1.5 5.5h-15S6 13 6 9Z"/><path d="M10 18a2 2 0 0 0 4 0"/>`,
	"settings2":  `<path d="M4 7h10"/><circle cx="17" cy="7" r="2.5"/><path d="M20 17H10"/><circle cx="7" cy="17" r="2.5"/>`,
	"book":       `<path d="M4 5.5A1.5 1.5 0 0 1 5.5 4H11v16H5.5A1.5 1.5 0 0 1 4 18.5v-13Z"/><path d="M20 5.5A1.5 1.5 0 0 0 18.5 4H13v16h5.5a1.5 1.5 0 0 0 1.5-1.5v-13Z"/>`,
	"film":       `<rect x="3" y="4.5" width="18" height="15" rx="2"/><path d="M7.5 4.5v15"/><path d="M16.5 4.5v15"/><path d="M3 9.5h4.5M16.5 9.5H21"/><path d="M3 14.5h4.5M16.5 14.5H21"/>`,
	"plus":       `<path d="M12 5v14"/><path d="M5 12h14"/>`,
	"check":      `<path d="m5 12.5 4.5 4.5L19 6.5"/>`,
	"refresh":    `<path d="M20 11a8 8 0 1 0-.7 4.5"/><path d="M20 4.5V11h-6.5"/>`,
}

// iconSVG renders one of navIconSVG on the shared 24x24 grid.
//
// Unknown keys return an empty string: a missing icon should leave a gap, not
// take the page down. The markup is trusted (it is a constant table above), so
// it is returned as template.HTML rather than escaped.
func iconSVG(key string) template.HTML {
	body, ok := navIconSVG[key]
	if !ok {
		return ""
	}
	return template.HTML(`<svg class="ico" viewBox="0 0 24 24" fill="none" stroke="currentColor" ` +
		`stroke-width="1.75" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">` +
		body + `</svg>`)
}

// navIconKeys maps a translation key onto the icon table above.
//
// It is separate from the label because two nav entries reuse translation keys
// that are also page headings ("runtime_log", "indexer_search"), so the emoji
// cannot simply be read off the label — the heading would lose its wording.
var navIconKeys = map[string]string{
	"dashboard":      "dashboard",
	"home":           "home",
	"indexer_search": "search",
	"indexers":       "indexers",
	"files":          "files",
	"subs":           "subs",
	"runtime_log":    "log",
	"settings":       "settings",
	"logout":         "logout",
	"about":          "about",
}

var dashboardTemplate = template.Must(template.New("dashboard").Funcs(template.FuncMap{
	"hasPrefix": strings.HasPrefix,
	// navIcon renders the monochrome icon for a navigation entry.
	"navIcon": func(key string) template.HTML {
		return iconSVG(navIconKeys[key])
	},
	// navText renders the label with the leading emoji stripped.
	//
	// The i18n strings still carry an emoji because the same keys are used as
	// page headings; here only the words are wanted, since the icon is drawn by
	// navIcon.
	"navText": func(lang, key string) template.HTML {
		_, text := splitNavLabel(tr(lang, key))
		if text == "" {
			return ""
		}
		return template.HTML(`<span class="nav-text sidebar-label">` + template.HTMLEscapeString(text) + `</span>`)
	},
	// navTitle is the tooltip text: the same words navText renders, without the
	// leading emoji, so a hovered tooltip matches the monochrome icon instead of
	// reintroducing a colour glyph.
	"navTitle": func(lang, key string) string {
		_, text := splitNavLabel(tr(lang, key))
		if text == "" {
			return tr(lang, key)
		}
		return text
	},
	// svgIcon exposes the table to the collapse button and other chrome.
	"svgIcon": iconSVG,
	// icoSpin renders the loading icon, spinning.
	//
	// It replaces the hourglass glyph the templates used to scatter around, so
	// every loading state looks the same and matches the icon family.
	"icoSpin": func() template.HTML {
		return template.HTML(`<svg class="ico ico-inline ico-spin" viewBox="0 0 24 24" fill="none" ` +
			`stroke="currentColor" stroke-width="1.75" stroke-linecap="round" ` +
			`aria-hidden="true"><path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5"/></svg>`)
	},
	// ico renders an icon inline inside text, sized to the current font.
	//
	// It exists for the places that used to hold a single emoji in a sentence
	// (a legend, a status line, a button label). Passing "" is allowed and
	// renders nothing, so a template branch can skip the icon without a
	// separate condition.
	"ico": func(key string) template.HTML {
		if key == "" {
			return ""
		}
		body, ok := navIconSVG[key]
		if !ok {
			return ""
		}
		return template.HTML(`<svg class="ico ico-inline" viewBox="0 0 24 24" fill="none" ` +
			`stroke="currentColor" stroke-width="1.75" stroke-linecap="round" ` +
			`stroke-linejoin="round" aria-hidden="true">` + body + `</svg>`)
	},
	"typeColor": func(t string) string {
		switch strings.ToLower(t) {
		case "public":
			return "#22c55e"
		case "semi-private":
			return "#f59e0b"
		case "private":
			return "#ef4444"
		default:
			return "#9ca3af"
		}
	},
	"indexerChecked": func(selected []string, id string) bool {
		for _, s := range selected {
			if s == id {
				return true
			}
		}
		return false
	},
	"add": func(a, b int) int { return a + b },
}).Parse(`
<!doctype html>
<html lang="{{.Lang}}">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>{{index .T "title"}}</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f5f7fb;
      --card: #ffffff;
      --text: #132238;
      --muted: #5c6b7a;
      --line: #dce3ec;
      --accent: #1d4ed8;
      --accent-2: #0f766e;
      --danger: #b42318;
      --warn: #b45309;
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
      background: linear-gradient(180deg, #eef3ff 0%, var(--bg) 100%);
      color: var(--text);
    }
    .wrap { display: flex; min-height: 100vh; }
    /* sidebar */
    .sidebar {
      width: 230px; min-width: 230px; background: var(--card); border-right: 1px solid var(--line);
      display: flex; flex-direction: column; transition: width .2s, min-width .2s;
      position: fixed; top: 0; left: 0; bottom: 0; z-index: 50; overflow: hidden;
    }
    /* Collapsed: shrink to an icon rail rather than sliding off-screen, so the
       navigation stays one click away. */
    .sidebar.collapsed { width: 64px; min-width: 64px; }
    /* Everything except the icons is hidden in the rail. Each label is wrapped
       so there is a single selector to switch off.
       The search box goes too: at 64px it has no room, and leaving it in would
       wrap its placeholder onto several lines, push the nav down, and shift the
       header button vertically between states. */
    .sidebar.collapsed .sidebar-label,
    .sidebar.collapsed .sidebar-nav a .nav-text,
    .sidebar.collapsed .sidebar-search { display: none; }

    /* Header: button | title | language.
       Both states use identical padding, so the button keeps its exact position
       on both axes — collapsing hides the title and the language control, which
       must not change the row's height. A fixed height guarantees that: the
       button is 28px and the padding adds 24px above and below, so the row is
       the same whether the select (about 22px) or nothing sits beside it. */
    .sidebar-head {
      display: flex; align-items: center; gap: 8px;
      height: 52px; padding: 0 12px;
    }
    .sidebar.collapsed .sidebar-head { padding: 0 12px; gap: 0; }
    .sidebar-title { font-size: 17px; font-weight: 700; color: var(--accent); white-space: nowrap; }
    .sidebar-toggle-btn {
      flex: 0 0 auto; display: inline-flex; align-items: center; justify-content: center;
      width: 28px; height: 28px; padding: 0; margin: 0;
      background: none; border: 1px solid transparent; border-radius: 7px;
      color: var(--muted); cursor: pointer;
    }
    .sidebar-toggle-btn:hover { background: var(--bg); color: var(--text); border-color: var(--line); }
    /* Language: a select when wide. In the rail there is no room for it beside
       the collapse button — 12px padding + 28px button + 8px gap already fills
       most of the 64px column — so it is hidden there and reappears when the
       sidebar is expanded. */
    .lang-select {
      margin: 0 0 0 auto; width: auto; font-size: 11px; padding: 2px 4px;
      border: 1px solid var(--line); border-radius: 4px; background: var(--bg);
    }
    .lang-toggle { display: none; }
    .sidebar.collapsed .lang-select { display: none; }

    /* Monochrome line icons. One rule sizes and colours them, because they are
       all drawn on the same 24x24 grid with currentColor strokes. */
    .ico { width: 18px; height: 18px; flex: 0 0 auto; display: block; }
    .sidebar-toggle-btn .ico { width: 17px; height: 17px; }
    /* Inline variant: follows the surrounding text size and baseline, for icons
       that sit inside a sentence, a legend or a button label. */
    .ico-inline {
      width: 1.05em; height: 1.05em; display: inline-block;
      vertical-align: -0.16em; margin-right: .35em;
    }
    /* A spinning icon doubles as the loading state, so "loading" reads the same
       everywhere instead of relying on a text glyph. */
    .ico-spin { animation: ico-spin 1s linear infinite; }
    @keyframes ico-spin { to { transform: rotate(360deg); } }

    .sidebar-subtitle { padding: 0 16px 8px; font-size: 11px; color: var(--muted); }
    .sidebar-search { padding: 10px 14px; }
    .sidebar-search input {
      width: 100%; padding: 9px 12px 9px 34px; font-size: 13px; border-radius: 12px;
      border: 1.5px solid var(--line); background: var(--bg);
      cursor: pointer; caret-color: transparent;
      background-image: url("data:image/svg+xml,%3Csvg xmlns='http://www.w3.org/2000/svg' width='14' height='14' viewBox='0 0 24 24' fill='none' stroke='%239ca3af' stroke-width='2'%3E%3Ccircle cx='11' cy='11' r='8'/%3E%3Cline x1='21' y1='21' x2='16.65' y2='16.65'/%3E%3C/svg%3E");
      background-repeat: no-repeat; background-position: 10px center;
      transition: all .2s;
    }
    .sidebar-search input:hover { border-color: var(--accent); box-shadow: 0 0 0 3px rgba(59,130,246,.1); }
    .sidebar-search input:focus { outline: none; border-color: var(--accent); background: #fff; caret-color: auto; box-shadow: 0 0 0 3px rgba(59,130,246,.15); }
    .sidebar-search-hint { font-size: 10px; color: var(--muted); padding: 2px 14px 0; text-align: center; }
    .sidebar-nav { flex: 1; overflow-y: auto; padding: 4px 0; }
    .sidebar-nav a {
      display: flex; align-items: center; gap: 10px; padding: 9px 14px; margin: 1px 6px;
      text-decoration: none; color: var(--muted); font-size: 14px; font-weight: 500;
      transition: background .12s, color .12s; border-radius: 8px;
    }
    .sidebar-nav a:hover { background: var(--bg); color: var(--text); }
    .sidebar-nav a.active { background: #eef2ff; color: var(--text); font-weight: 600; }
    .sidebar-nav a.active .ico { color: var(--accent); }
    /* In the rail the icons centre themselves in the 64px column. */
    .sidebar.collapsed .sidebar-nav a { justify-content: center; padding: 10px 0; margin: 1px 8px; gap: 0; }
    .sidebar-footer {
      padding: 10px 8px; border-top: 1px solid var(--line);
      display: flex; align-items: center; justify-content: space-between; gap: 4px;
    }
    .sidebar.collapsed .sidebar-footer { flex-direction: column; gap: 2px; padding: 8px 0; }
    /* main area */
    .main { flex: 1; margin-left: 230px; padding: 24px 24px 40px; transition: margin-left .2s; min-width: 0; }
    /* The rail is 64px wide, so the content sits beside it rather than under it. */
    .main.expanded { margin-left: 64px; }
    .grid { display: grid; grid-template-columns: repeat(auto-fit, minmax(280px, 1fr)); gap: 14px; }
    .card {
      background: var(--card);
      border: 1px solid var(--line);
      border-radius: 16px;
      padding: 16px;
      box-shadow: 0 8px 20px rgba(15, 23, 42, 0.04);
      min-width: 0;
      overflow-x: auto;
    }
    .card h2 { margin: 0 0 8px; font-size: 17px; }
    .meta { display: flex; gap: 10px; flex-wrap: wrap; margin: 8px 0 0; color: var(--muted); font-size: 13px; }
    label { display: block; margin: 8px 0 4px; font-size: 13px; color: var(--muted); }
    input, textarea, select {
      width: 100%;
      max-width: 100%;
      border: 1px solid var(--line);
      border-radius: 10px;
      padding: 8px 10px;
      font: inherit;
      font-size: 14px;
      background: #fff;
    }
    textarea { min-height: 100px; resize: vertical; }
    button {
      margin-top: 10px;
      border: 0;
      border-radius: 10px;
      padding: 8px 12px;
      background: var(--accent);
      color: #fff;
      font: inherit;
      font-size: 14px;
      cursor: pointer;
    }
    button.secondary { background: var(--accent-2); }
    .hint { color: var(--muted); font-size: 12px; margin-top: 6px; line-height: 1.5; }
    .status { margin-bottom: 12px; padding: 10px 12px; border-radius: 10px; background: #f8fafc; border: 1px solid var(--line); font-size: 14px; }
    .ok { color: var(--accent-2); }
    .err { color: var(--danger); }
    code { background: #f1f5f9; padding: 1px 5px; border-radius: 5px; font-size: 13px; }
    .footer { margin-top: 14px; color: var(--muted); font-size: 12px; }
    a { color: var(--accent); }

    /* full-width panels */
    .panel { margin-top: 16px; }
    .panel h2 { font-size: 17px; margin: 0 0 6px; }

    /* task table */
    .tbl { width: 100%; border-collapse: collapse; font-size: 13px; }
    .tbl th, .tbl td { padding: 6px 8px; text-align: left; border-bottom: 1px solid var(--line); }
    .tbl th { color: var(--muted); font-weight: 600; }
    .tbl td { word-break: break-all; }
    .row-done { opacity: 0.65; }
    .row-failed { background: #fef2f2; }
    .row-running { background: #f0fdf4; }
    .badge {
      display: inline-block;
      padding: 2px 6px;
      border-radius: 6px;
      font-size: 12px;
      font-weight: 600;
    }
    .badge-done { background: #dcfce7; color: #166534; }
    .badge-failed { background: #fee2e2; color: #991b1b; }
    .badge-running { background: #dbeafe; color: #1e40af; }
    .badge-waiting { background: #f3f4f6; color: #374151; }

    /* log */
    .log-panel {
      background: #0f172a;
      color: #e2e8f0;
      border-radius: 14px;
      padding: 14px;
      font-family: "SF Mono", "Cascadia Code", "Fira Code", monospace;
      font-size: 12px;
      line-height: 1.6;
      max-height: 440px;
      overflow-y: auto;
      white-space: pre-wrap;
      word-break: break-all;
    }
    .log-panel::-webkit-scrollbar { width: 6px; }
    .log-panel::-webkit-scrollbar-thumb { background: #334155; border-radius: 3px; }
    .log-clear-marker {
      display: block; padding: 4px 0; color: #f59e0b; font-size: 11px;
      border-top: 1px dashed #f59e0b; border-bottom: 1px dashed #f59e0b;
      margin: 4px 0; text-align: center;
    }
    .breadcrumb { display: flex; flex-wrap: wrap; align-items: center; gap: 0; margin-bottom: 10px; font-size: 13px; }
    .crumb { color: var(--accent); text-decoration: none; }
    .crumb:hover { text-decoration: underline; }
    .crumb-sep { color: var(--muted); margin: 0 4px; }
    .fs-tbl td.muted { color: var(--muted); font-size: 12px; }
    .fs-tbl td.mono { font-family: monospace; font-size: 11px; }
    .topbar { display: flex; align-items: center; gap: 12px; margin-bottom: 18px; }
    .topbar-msg { flex: 1; font-size: 13px; color: var(--muted); padding: 6px 12px; background: #f8fafc; border: 1px solid var(--line); border-radius: 8px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; }
    .topbar-msg.ok { color: var(--accent-2); border-color: #bae6fd; background: #f0f9ff; }
    .topbar-msg.err { color: var(--danger); border-color: #fecaca; background: #fef2f2; }
    /* Footer links carry an icon plus a label, so they follow the same rules as
       the nav entries rather than the old text-only styling. */
    .logout-text {
      display: flex; align-items: center; gap: 8px; padding: 8px 10px; border-radius: 8px;
      color: var(--muted); text-decoration: none; font-size: 13px; white-space: nowrap;
      transition: background .12s, color .12s;
    }
    .logout-text:hover { background: var(--bg); color: var(--text); }
    .sidebar.collapsed .logout-text { justify-content: center; padding: 8px 0; gap: 0; }

    /* search modal */
    .smodal-overlay { display: none; position: fixed; inset: 0; background: rgba(0,0,0,.35); z-index: 100; justify-content: center; align-items: flex-start; padding-top: 40px; }
    .smodal-overlay.active { display: flex; }
    .smodal-card {
      background: var(--card); border-radius: 18px; padding: 24px;
      width: 92%; max-width: 900px; max-height: 85vh; overflow-y: auto;
      box-shadow: 0 20px 60px rgba(0,0,0,.18);
    }
    .smodal-card h2 { margin-top: 0; }
  </style>
  <script>
    // ICO returns the markup for an icon so JavaScript-built HTML uses the same
    // icon set as the templates.
    //
    // Rows and modals are assembled as strings at runtime, where a template
    // helper is unavailable; without this they would keep the emoji the rest of
    // the panel has moved away from.
    var _icoPaths={
      play:'<path d="M7.5 5.5v13l11-6.5-11-6.5Z"/>',
      files:'<path d="M3 7.5A2 2 0 0 1 5 5.5h4l2 2.5h8a2 2 0 0 1 2 2v8a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2Z"/>',
      warning:'<path d="M12 4.5 21 19.5H3L12 4.5Z"/><path d="M12 10v4"/><circle cx="12" cy="16.6" r="0.9" fill="currentColor" stroke="none"/>',
      film:'<rect x="3" y="4.5" width="18" height="15" rx="2"/><path d="M7.5 4.5v15"/><path d="M16.5 4.5v15"/><path d="M3 9.5h4.5M16.5 9.5H21"/><path d="M3 14.5h4.5M16.5 14.5H21"/>',
      tv:'<rect x="3" y="7.5" width="18" height="12" rx="2"/><path d="m8 3.5 4 4 4-4"/>',
      search:'<circle cx="11" cy="11" r="7"/><path d="m20 20-3.5-3.5"/>',
      loading:'<path d="M12 3.5a8.5 8.5 0 1 0 8.5 8.5"/>'
    };
    function ICO(name,spin){
      var p=_icoPaths[name];
      if(!p)return '';
      return '<svg class="ico'+(spin?' ico-inline ico-spin':'')+'" viewBox="0 0 24 24" fill="none" '+
        'stroke="currentColor" stroke-width="1.75" stroke-linecap="round" '+
        'stroke-linejoin="round" aria-hidden="true">'+p+'</svg>';
    }
    var modalCb=null;
    function showModal(title,body,buttons){
      document.getElementById('g-modal-title').textContent=title;
      document.getElementById('g-modal-body').innerHTML=body;
      var btns=document.getElementById('g-modal-btns');
      btns.innerHTML='';
      (buttons||[{text:'{{index .T "confirm_btn"}}',cls:'',cb:function(){closeModal()}}]).forEach(function(b){
        var btn=document.createElement('button');
        btn.textContent=b.text;btn.style.margin='0';btn.style.padding='6px 16px';
        if(b.cls)btn.style.background=b.cls;
        if(b.id)btn.id=b.id;
        btn.onclick=function(){if(b.cb)b.cb();};
        btns.appendChild(btn);
      });
      document.getElementById('g-modal').style.display='flex';
    }
    function closeModal(){document.getElementById('g-modal').style.display='none';modalCb=null;}
    function alertModal(msg){showModal('',msg,[{text:'OK',cls:'var(--accent)',cb:function(){closeModal()}}]);}
    async function confirmAsync(msg){return new Promise(function(resolve){showModal('',msg,[{text:'Cancel',cls:'var(--danger)',cb:function(){closeModal();resolve(false);}},{text:'OK',cls:'var(--accent)',cb:function(){closeModal();resolve(true);}}]);});}
    async function promptModal(title,label,defaultValue){return new Promise(function(resolve){var dv=(defaultValue||'').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');var body='<div style="margin-bottom:6px;font-size:13px;color:var(--muted);">'+label+'</div><input id="g-modal-input" style="width:100%;padding:8px;border:1px solid var(--line);border-radius:6px;font-size:14px;box-sizing:border-box;" value="'+dv+'" onkeydown="if(event.key===&quot;Enter&quot;)document.getElementById(&quot;g-modal-btn-ok&quot;).click()" autofocus>';showModal(title,body,[{text:'Cancel',cls:'var(--danger)',cb:function(){closeModal();resolve(null);}},{text:'OK',cls:'var(--accent)',id:'g-modal-btn-ok',cb:function(){var v=document.getElementById('g-modal-input').value.trim();closeModal();resolve(v);}}]);});}
    function openSearchModal(){
      var m=document.getElementById('search-modal');
      if(m)m.classList.add('active');
      var q=document.getElementById('search-q');
      if(q)q.focus();
    }
    function closeSearchModal(){
      var m=document.getElementById('search-modal');
      if(m)m.classList.remove('active');
    }
    // The sidebar starts collapsed as an icon rail. The choice is remembered so
    // navigating between pages does not keep re-expanding it.
    function applySidebarState(collapsed){
      var sb=document.getElementById('sidebar');
      var mn=document.getElementById('main');
      if(sb){ sb.classList.toggle('collapsed',collapsed); }
      if(mn){ mn.classList.toggle('expanded',collapsed); }
    }
    function toggleSidebar(){
      var sb=document.getElementById('sidebar');
      var isRailed=sb?sb.classList.contains('collapsed'):true;
      var next=!isRailed; // expand if railed, rail if expanded
      applySidebarState(next);
      try{ localStorage.setItem('sidebar-railed',next?'1':'0'); }catch(e){}
    }
    (function(){
      // This runs from <head>, before #sidebar exists, so the DOM has to be
      // ready first: calling getElementById here would return null and the saved
      // state would never be applied.
      function restore(){
        var railed=true;
        try{
          var v=localStorage.getItem('sidebar-railed');
          // Default to the rail: it keeps every destination visible without
          // taking 230px of a narrow screen.
          if(v==='0')railed=false;
        }catch(e){}
        applySidebarState(railed);
      }
      if(document.readyState==='loading'){
        document.addEventListener('DOMContentLoaded',restore);
      }else{
        restore();
      }
    })();
    async function restartServer(){
      if(!(await confirmAsync('{{index .T "confirm_restart"}}')))return;
      try{
        var r=await fetch('/settings/restart',{method:'POST'});
        var j=await r.json();
        if(j.ok){alertModal('{{index .T "restarting"}}');}
        else{alertModal('{{index .T "restart_failed"}}'+j.msg);}
      }catch(e){alertModal('{{index .T "restart_req_failed"}}'+e.message);}
    }
    function switchLang(lang){
      fetch('/api/lang',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:'lang='+lang}).then(function(){location.reload();});
    }
  </script>
</head>
<body>
  <!-- left sidebar -->
  <div class="sidebar collapsed" id="sidebar">
    <!-- Header: the collapse button sits left of the title, and stays in the
         same place in both states so the control does not move under the
         pointer. Only the title and the language control change. -->
    <div class="sidebar-head">
      <button class="sidebar-toggle-btn" type="button" onclick="toggleSidebar()"
        title="{{index .T "toggle_sidebar"}}" aria-label="{{index .T "toggle_sidebar"}}">{{svgIcon "menu"}}</button>
      <span class="sidebar-title sidebar-label">pan-fetcher</span>
      <select class="lang-select sidebar-label" onchange="switchLang(this.value)">
        <option value="zh"{{if eq .Lang "zh"}} selected{{end}}>CN</option>
        <option value="en"{{if eq .Lang "en"}} selected{{end}}>EN</option>
      </select>
    </div>
    <div class="sidebar-search">
      <input type="text" id="quick-search-input" placeholder="搜索电影、剧集..." value="" autocomplete="off" onfocus="location.href='/discover';this.blur()">
      <div class="sidebar-search-hint">{{ico "tmdb"}}TMDB 发现</div>
    </div>
    <div class="sidebar-nav">
      <a href="/"{{if or (eq .Page "home") (eq .Page "")}} class="active"{{end}} title="{{navTitle $.Lang "dashboard"}}">{{navIcon "dashboard"}}{{navText $.Lang "dashboard"}}</a>
      <a href="/tasks"{{if eq .Page "tasks"}} class="active"{{end}} title="{{navTitle $.Lang "home"}}">{{navIcon "home"}}{{navText $.Lang "home"}}</a>
      <a href="/search"{{if eq .Page "search"}} class="active"{{end}} title="{{navTitle $.Lang "indexer_search"}}">{{navIcon "indexer_search"}}{{navText $.Lang "indexer_search"}}</a>
      <a href="/indexers"{{if eq .Page "indexers"}} class="active"{{end}} title="{{navTitle $.Lang "indexers"}}">{{navIcon "indexers"}}{{navText $.Lang "indexers"}}</a>
      <a href="/fs"{{if eq .Page "fs"}} class="active"{{end}} title="{{navTitle $.Lang "files"}}">{{navIcon "files"}}{{navText $.Lang "files"}}</a>
      <a href="/subs"{{if eq .Page "subs"}} class="active"{{end}} title="{{navTitle $.Lang "subs"}}">{{navIcon "subs"}}{{navText $.Lang "subs"}}</a>
      <a href="/log"{{if eq .Page "log"}} class="active"{{end}} title="{{navTitle $.Lang "runtime_log"}}">{{navIcon "runtime_log"}}{{navText $.Lang "runtime_log"}}</a>
      <a href="/settings"{{if eq .Page "settings"}} class="active"{{end}} title="{{navTitle $.Lang "settings"}}">{{navIcon "settings"}}{{navText $.Lang "settings"}}</a>
    </div>
    <div class="sidebar-footer">
      <a href="/logout" class="logout-text" title="{{navTitle $.Lang "logout"}}">{{navIcon "logout"}}{{navText $.Lang "logout"}}</a>
      <a href="/about" class="logout-text" title="{{navTitle $.Lang "about"}}">{{navIcon "about"}}{{navText $.Lang "about"}}</a>
    </div>
  </div>

  <!-- main content -->
  <div class="main expanded" id="main">
    <div class="topbar">
      {{if .Error}}<div class="topbar-msg err">{{.Error}}</div>
      {{else if .Message}}<div class="topbar-msg ok">{{.Message}}</div>
      {{else}}<div class="topbar-msg">{{index .T "announcement"}}</div>
      {{end}}
    </div>

    {{if or (eq .Page "home") (eq .Page "")}}
    <!-- dashboard -->
    <div class="grid" style="grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px;">
      <div class="card" style="text-align:center;">
        <div style="font-size:32px;font-weight:700;color:var(--accent);">{{.DashStats.TotalTasks}}</div>
        <div style="font-size:13px;color:var(--muted);margin-top:4px;">{{index .T "push_count"}}</div>
      </div>
      <div class="card" style="text-align:center;">
        <div style="font-size:32px;font-weight:700;color:var(--muted);">{{.DashStats.RssSubsActive}}/{{.DashStats.RssSubsTotal}}</div>
        <div style="font-size:13px;color:var(--muted);margin-top:4px;">{{index .T "rss_subs_short"}}</div>
      </div>
      <div class="card" style="text-align:center;">
        <div style="font-size:32px;font-weight:700;color:var(--accent-2);">{{.DashStats.ActiveIndexers}}</div>
        <div style="font-size:13px;color:var(--muted);margin-top:4px;">{{index .T "active_indexers_short"}}</div>
      </div>
      <div class="card" style="text-align:center;">
        <div style="font-size:32px;font-weight:700;color:#a78bfa;">{{.DashStats.CacheEntries}}</div>
        <div style="font-size:13px;color:var(--muted);margin-top:4px;">{{index .T "cache_entries"}}</div>
      </div>
    </div>
    <div class="card panel" style="margin-top:16px;">
      <div style="display:flex;align-items:center;justify-content:space-between;flex-wrap:wrap;gap:12px;">
        <div>
          <span style="font-size:13px;color:var(--muted);">{{index .T "uptime_label"}}: </span>
          <strong>{{.DashStats.Uptime}}</strong>
          {{if .HasAgent}}<span style="margin-left:12px;font-size:13px;color:var(--accent-2);">{{ico "check"}}115 {{index .T "status_connected"}}</span>{{end}}
          <span style="margin-left:12px;font-size:13px;{{if .HasPassword}}color:var(--accent-2);{{else}}color:var(--warn);{{end}}">{{if .HasPassword}}{{ico "lock"}}{{index .T "pw_set"}}{{else}}{{ico "warning"}}{{index .T "pw_not_set"}}{{end}}</span>
        </div>
      </div>
    </div>
    {{if .DashStats.RecentItems}}
    <div class="card panel" style="margin-top:16px;">
      <h3 style="margin:0 0 10px;">{{ico "plus"}}最近新增资源</h3>
      <div style="max-height:300px;overflow-y:auto;">
        {{range .DashStats.RecentItems}}
        <div style="padding:6px 0;border-bottom:1px solid var(--line);">
          <div style="font-size:13px;word-break:break-all;line-height:1.5;" title="{{.Name}}">{{.Name}}</div>
          <div style="font-size:11px;color:var(--muted);margin-top:2px;">
            <span>{{.Time}}</span>
            <span style="margin-left:6px;">[{{.Sub}}]</span>
          </div>
        </div>
        {{end}}
      </div>
    </div>
    {{end}}
    {{end}}

    <!-- offline tasks page -->
    {{if eq .Page "tasks"}}
    <div class="grid">
      <div class="card">
        <h2>{{index .T "add_magnet"}}</h2>
        <form action="/add" method="post">
          <label for="tasks">{{index .T "task_url"}}</label>
          <textarea id="tasks" name="tasks" placeholder="{{index .T "task_url_ph"}}"></textarea>
          <label for="cid">{{index .T "cid"}}</label>
          <div style="display:flex;gap:4px;">
            <input id="cid" name="cid" placeholder="{{index .T "cid_ph"}}" style="flex:1;">
            <button type="button" onclick="browseDirsFor('cid')" style="margin:0;padding:4px 8px;font-size:12px;background:var(--accent-2);white-space:nowrap;">{{index .T "browse_btn"}}</button>
          </div>
          <label for="savepath">{{index .T "savepath"}}</label>
          <input id="savepath" name="savepath" placeholder="{{index .T "savepath_ph"}}">
          <button type="submit">{{index .T "submit_task"}}</button>
        </form>
        <div class="hint">{{index .T "json_api"}} <code>POST /add</code></div>
      </div>
    </div>
    {{end}}

    <!-- cloud filesystem browser -->
    {{if eq .Page "fs"}}
    <div class="card panel">
      <h2>{{index .T "cloud_files"}}</h2>
      <div class="breadcrumb">
        {{range .FSCrumbs}}<a class="crumb" href="/fs?dir={{.ID}}">{{if eq .ID "0"}}{{index $.T "root_dir"}}{{else}}{{.Name}}{{end}}</a><span class="crumb-sep">/</span>{{end}}
      </div>
      <div style="margin-bottom:10px;">
        <button onclick="fsNewFolder('{{.FSCurrentID}}')" style="padding:4px 12px;font-size:12px;">{{ico "files"}}{{index .T "new_folder"}}</button>
      </div>
      {{if .FSEntries}}
      <table class="tbl fs-tbl">
        <thead><tr><th></th><th>{{index .T "name"}}</th><th>{{index .T "size"}}</th><th>ID</th><th></th></tr></thead>
        <tbody>
        {{if ne .FSCurrentID "0"}}<tr><td>⬆</td><td><a href="/fs?dir={{.FSParentID}}">..</a></td><td></td><td></td><td></td></tr>{{end}}
        {{range .FSEntries}}<tr>
          <td>{{.Icon}}</td>
          <td>{{if .IsDir}}<a href="/fs?dir={{.ID}}">{{.Name}}</a>{{else}}{{.Name}}{{end}}</td>
          <td class="muted">{{.Size}}</td>
          <td class="muted mono">{{.ID}}</td>
          <td style="white-space:nowrap;">
            <button onclick="fsRename('{{.ID}}','{{.Name}}')" style="padding:2px 6px;font-size:11px;margin:0;" title="{{index $.T "rename"}}">{{ico "edit"}}</button>
            <button onclick="fsMove('{{.ID}}','{{.Name}}')" style="padding:2px 6px;font-size:11px;margin:0;" title="{{index $.T "move"}}">↗</button>
            <button onclick="fsCopy('{{.ID}}','{{.Name}}')" style="padding:2px 6px;font-size:11px;margin:0;" title="{{index $.T "copy"}}">{{ico "copy"}}</button>
            <button onclick="fsDelete('{{.ID}}','{{.Name}}')" style="background:var(--danger);padding:2px 6px;font-size:11px;margin:0;" title="{{index $.T "delete"}}">{{ico "close"}}</button>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
      {{else}}
      <div class="hint">{{index .T "empty_dir"}}</div>
      {{end}}
    </div>
    {{end}}

    <!-- subscription management -->
    {{if eq .Page "subs"}}
    <div class="card panel">
      <h2>{{index .T "subs_mgmt"}} ({{len .RssSubs}})
        {{if not .RssSubs}}<span style="font-weight:400;font-size:12px;color:var(--muted);margin-left:8px;">{{index .T "no_subs_hint"}}</span>{{end}}
      </h2>
      {{if .RssSubs}}
      <div style="display:flex;gap:8px;margin-bottom:10px;flex-wrap:wrap;">
        <button onclick="toggleAllSubs(true)" style="margin:0;padding:4px 12px;font-size:12px;background:var(--accent-2);">{{index .T "enable_all"}}</button>
        <button onclick="toggleAllSubs(false)" style="margin:0;padding:4px 12px;font-size:12px;background:var(--danger);">{{index .T "disable_all"}}</button>
        <button onclick="clearAllCache()" style="margin:0;padding:4px 12px;font-size:12px;background:#6b7280;">{{index .T "clear_all_cache"}}</button>
      </div>
      <table class="tbl">
        <thead><tr><th>{{index .T "name"}}</th><th>{{index .T "indexer_label"}}</th><th>{{index .T "sub_status"}}</th><th>{{index .T "cache"}}</th><th></th></tr></thead>
        <tbody>
        {{range .RssSubs}}<tr>
          <td>
            <strong>{{.Name}}</strong><br><small class="muted">{{.Site}}</small>
          </td>
          <td><small class="muted">{{.IndexerDisplay}}</small></td>
          <td>
            <form action="/subs" method="post" style="display:inline;">
              <input type="hidden" name="action" value="toggle">
              <input type="hidden" name="site" value="{{.Site}}">
              <input type="hidden" name="name" value="{{.Name}}">
              <button type="submit" style="padding:2px 8px;font-size:11px;margin:0;{{if .Enabled}}background:var(--accent-2);{{else}}background:var(--danger);{{end}}">{{if .Enabled}}{{index $.T "enabled"}}{{else}}{{index $.T "disabled"}}{{end}}</button>
            </form>
          </td>
          <td>
            {{if gt .CacheCount 0}}<span style="cursor:pointer;user-select:none;font-size:12px;" onclick="toggleSubCache('{{.Name}}',this)">{{ico "play"}}</span>{{end}}
            <span style="font-size:12px;color:var(--muted);">{{.CacheCount}} {{index $.T "items"}}</span>
            {{if gt .CacheCount 0}}
            <form action="/dedup/clear" method="post" style="display:inline;">
              <input type="hidden" name="sub" value="{{.Name}}">
              <button type="submit" style="padding:1px 6px;font-size:10px;margin:0;background:var(--danger);" onclick="submitConfirm(this.form,'{{index $.T "confirm_clear_cache_msg"}}')">{{index $.T "clear"}}</button>
            </form>
            {{end}}
          </td>
          <td style="white-space:nowrap;">
            <button onclick="runSub(this,'{{.URL}}','{{.Cid}}','{{.SavePath}}','{{.Filter}}','{{.Name}}')" style="padding:2px 6px;font-size:11px;margin:0;background:var(--accent);" title="立即执行">{{ico "play"}}</button>
            <button onclick="editSubInline(this)" style="padding:2px 6px;font-size:11px;margin:0;">{{ico "edit"}}</button>
            <form action="/subs" method="post" style="display:inline;">
              <input type="hidden" name="action" value="delete">
              <input type="hidden" name="site" value="{{.Site}}">
              <input type="hidden" name="name" value="{{.Name}}">
              <button type="submit" style="background:var(--danger);padding:2px 8px;font-size:11px;margin:0;" onclick="submitConfirm(this.form,'{{index $.T "confirm_delete_sub"}}')">{{ico "close"}}</button>
            </form>
          </td>
        </tr>
        <tr class="edit-row" style="display:none;">
          <td colspan="5" style="padding:0;">
            <div style="padding:14px;background:#f8fafc;border:1px solid var(--line);border-radius:10px;margin:8px 0;">
              <h3 style="margin:0 0 10px;">{{index $.T "edit_sub"}}</h3>
              <form action="/subs" method="post">
                <input type="hidden" name="action" value="edit">
                <input type="hidden" name="site" value="{{.Site}}">
                <input type="hidden" name="name" value="{{.Name}}">
                <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
                  <div><label>CID</label><input name="cid" value="{{.Cid}}" style="font-size:13px;width:120px;"></div>
                  <div><label>{{index $.T "subdir_label"}}</label><input name="savepath" value="{{.SavePath}}" style="font-size:13px;width:100px;"></div>
                  <div><label>{{index $.T "filter_label"}}</label><input name="filter" value="{{.Filter}}" style="font-size:13px;width:100px;"></div>
                  <button type="submit" style="margin-top:0;">{{index $.T "save"}}</button>
                  <button type="button" onclick="closeAllEditRows()" style="margin-top:0;background:var(--danger);">{{index $.T "cancel"}}</button>
                </div>
              </form>
            </div>
          </td>
        </tr>
        <tr id="cache-{{.Name}}" style="display:none;"><td colspan="5" style="padding:0;">
          <div style="padding:4px 8px;background:#f8fafc;max-height:300px;overflow-y:auto;" id="cache-list-{{.Name}}">{{index $.T "loading"}}</div>
        </td></tr>
        {{end}}
        </tbody>
      </table>
      {{else}}
      <div class="hint">{{index .T "no_subs_hint"}}</div>
      {{end}}
      <script>
        function editSubInline(btn){
          closeAllEditRows();
          var tr=btn.closest('tr');
          var next=tr.nextElementSibling;
          if(next&&next.classList.contains('edit-row')){
            next.style.display='';
          }
        }
        function closeAllEditRows(){
          document.querySelectorAll('.edit-row').forEach(function(r){r.style.display='none';});
        }
        async function runSub(btn,url,cid,savepath,filter,subName){
          btn.textContent='…'; btn.disabled=true;
          try{
            var r=await fetch('/subs/run',{method:'POST',
              headers:{'Content-Type':'application/x-www-form-urlencoded','X-Requested-With':'XMLHttpRequest'},
              body:new URLSearchParams({rss_url:url,cid:cid,savepath:savepath,filter:filter,sub_name:subName})});
            var j=await r.json();
            if(j.ok){
              // Show message in topbar
              var tb=document.querySelector('.topbar-msg');
              if(tb){tb.textContent=j.msg;tb.className='topbar-msg ok';}
              // Auto-reload after 3s to show updated cache counts
              setTimeout(function(){location.reload();},3000);
            }else{
              alertModal(j.msg||'Error');
            }
          }catch(e){alertModal(e.message);}
        btn.innerHTML=ICO('play'); btn.disabled=false;
        }
        var subCacheData={};
        async function toggleSubCache(subKey,el){
          var row=document.getElementById('cache-'+subKey);
          if(row.style.display==='none'){
            row.style.display='table-row';
            el.textContent='▼ ';
            if(subCacheData[subKey]){
              document.getElementById('cache-list-'+subKey).innerHTML=subCacheData[subKey];
              return;
            }
            try{
              var r=await fetch('/api/dedup/hashes?sub='+encodeURIComponent(subKey));
              var items=await r.json();
              if(!items||!Array.isArray(items))items=[];
              var cnt=0;
              var html='<table style="width:100%;font-size:11px;border-collapse:collapse;">';
              items.forEach(function(it){
                cnt++;
                var display=it.name||it.hash;
                html+='<tr style="border-bottom:1px solid #e8ecf1;">';
                html+='<td style="padding:3px 6px;max-width:400px;overflow:hidden;text-overflow:ellipsis;white-space:nowrap;font-size:12px;" title="'+(it.hash||'')+'">'+display+'</td>';
                html+='<td style="padding:3px 6px;text-align:right;">';
                html+='<button data-hash="'+it.hash+'" data-sub="'+subKey.replace(/"/g,'&quot;')+'" onclick="removeCacheHash(this)" style="padding:1px 6px;font-size:10px;margin:0;background:var(--danger);">{{ico "close"}}</button>';
                html+='</td></tr>';
              });
              html+='</table>';
              if(cnt===0)html='{{index $.T "no_records"}}';
              subCacheData[subKey]=html;
              document.getElementById('cache-list-'+subKey).innerHTML=html;
            }catch(e){
              document.getElementById('cache-list-'+subKey).innerHTML='{{index .T "load_failed"}}'+e.message;
            }
          }else{
            row.style.display='none';
            el.innerHTML=ICO('play');
          }
        }
        async function removeCacheHash(btn){
          var hash=btn.getAttribute('data-hash');
          var sub=btn.getAttribute('data-sub');
          if(!hash||!sub)return;
          if(!(await confirmAsync('{{index .T "confirm_delete"}} '+hash.substring(0,12)+'... ?')))return;
          btn.disabled=true;
          try{
            var form=new FormData();
            form.append('sub',sub);
            form.append('hash',hash);
            var r=await fetch('/api/dedup/remove-hash',{method:'POST',body:form});
            var j=await r.json();
            if(j.status==='ok'){
              var tr=btn.closest('tr');
              if(tr)tr.remove();
              delete subCacheData[sub];
            }else{
              alertModal(j.message||'Error');
            }
          }catch(e){
            alertModal(e.message);
          }
          btn.disabled=false;
        }
        async function toggleAllSubs(enabled){
          if(!(await confirmAsync(enabled?'{{index .T "enable_all_confirm"}}':'{{index .T "disable_all_confirm"}}')))return;
          try{
            var r=await fetch('/subs/toggle-all',{method:'POST',
              headers:{'Content-Type':'application/x-www-form-urlencoded','X-Requested-With':'XMLHttpRequest'},
              body:new URLSearchParams({enabled:enabled?'1':'0'})});
            var j=await r.json();
            if(j.ok)location.reload();
            else alertModal(j.msg);
          }catch(e){alertModal(e.message);}
        }
        async function clearAllCache(){
          if(!(await confirmAsync('{{index .T "clear_all_cache_confirm"}}')))return;
          try{
            var r=await fetch('/dedup/clear-all',{method:'POST',headers:{'X-Requested-With':'XMLHttpRequest'}});
            var j=await r.json();
            if(j.ok)location.reload();
            else alertModal(j.msg);
          }catch(e){alertModal(e.message);}
        }
      </script>
      {{if .HasAgent}}<div style="margin-top:10px;">
        <form action="/subs/run" method="post">
          <input name="rss_url" placeholder="{{index .T "sub_rss_ph"}}" style="width:auto;min-width:300px;display:inline;">
          <button type="submit" style="margin-top:0;">{{index .T "sub_run"}}</button>
        </form>
      </div>{{end}}
    </div>
    {{end}}

    <!-- offline task list -->
    {{if eq .Page "tasks"}}
    <div class="card panel" style="margin-top:16px;">
      <h2 id="task-heading">{{index .T "offline_tasks"}} (<span id="task-total">{{.TaskCount}}</span>)
        <span style="font-weight:400;font-size:12px;margin-left:8px;">
          <span id="tab-downloading" style="cursor:pointer;color:var(--accent);border-bottom:2px solid var(--accent);" onclick="switchTaskTab('downloading')">{{index .T "downloading"}} <span id="cnt-downloading"></span></span>
          <span style="margin:0 8px;color:var(--line);">|</span>
          <span id="tab-failed" style="cursor:pointer;color:var(--muted);" onclick="switchTaskTab('failed')">{{index .T "failed"}} <span id="cnt-failed"></span></span>
          <span style="margin:0 8px;color:var(--line);">|</span>
          <span id="tab-done" style="cursor:pointer;color:var(--muted);" onclick="switchTaskTab('done')">{{index .T "completed"}} <span id="cnt-done"></span></span>
        </span>
      </h2>
      <form action="/clear" method="post" style="margin-bottom:8px;">
        <select name="type" style="width:auto;display:inline;">
          <option value="1">{{index .T "clear_done"}}</option>
          <option value="4">{{index .T "clear_running"}}</option>
          <option value="3">{{index .T "clear_failed"}}</option>
          <option value="2">{{index .T "clear_all"}}</option>
        </select>
        <button type="submit" style="margin-top:0;padding:6px 12px;font-size:13px;">{{index .T "execute"}}</button>
      </form>
      <table class="tbl" id="task-table">
        <thead><tr><th>{{index .T "name"}}</th><th>{{index .T "size"}}</th><th style="width:60px;">%</th><th style="width:40px;"></th></tr></thead>
        <tbody>
        {{if .Tasks}}{{range .Tasks}}<tr class="{{.RowClass}}" data-status="{{.Status}}" data-url="{{.URL}}">
          <td title="{{.InfoHash}}">{{.Name}}</td>
          <td>{{.Size}}</td>
          <td>{{printf "%.0f" .Percent}}%</td>
          <td><button onclick="copyTaskURL('{{.URL}}')" style="padding:2px 6px;font-size:10px;margin:0;" title="{{index $.T "copy_link_title"}}">{{ico "copy"}}</button></td>
        </tr>{{end}}{{else}}<tr><td colspan="4" class="hint" id="task-empty-hint">{{index .T "no_tasks"}}</td></tr>{{end}}
        </tbody>
      </table>
    </div>
    <script>
      function switchTaskTab(tab){
        document.querySelectorAll('#tab-downloading,#tab-failed,#tab-done').forEach(function(el){
          el.style.color='var(--muted)';el.style.borderBottom='none';
        });
        document.getElementById('tab-'+tab).style.color='var(--accent)';
        document.getElementById('tab-'+tab).style.borderBottom='2px solid var(--accent)';
        var rows=document.querySelectorAll('#task-table tbody tr');
        var counts={downloading:0,failed:0,done:0};
        rows.forEach(function(r){
          var s=r.getAttribute('data-status');
          if(s===tab||tab==='all'){r.style.display='';}
          else{r.style.display='none';}
          counts[s]=(counts[s]||0)+1;
        });
        document.getElementById('cnt-downloading').textContent='('+counts.downloading+')';
        document.getElementById('cnt-failed').textContent='('+counts.failed+')';
        document.getElementById('cnt-done').textContent='('+counts.done+')';
      }
      function copyTaskURL(url){
        navigator.clipboard.writeText(url).then(function(){},function(){
          alertModal('URL: '+url);
        });
      }
      // init — load tasks immediately
      refreshTasks();
      // Auto-refresh tasks every 30s
      var taskRefreshTimer=setInterval(refreshTasks,30000);
      async function refreshTasks(){
        try{
          var r=await fetch('/api/tasks');
          var j=await r.json();
          var tbody=document.querySelector('#task-table tbody');
          if(!tbody)return;
          // Update total count in heading
          var totalEl=document.getElementById('task-total');
          if(totalEl&&j.count!==undefined)totalEl.textContent=j.count;
          if(!j.tasks||j.tasks.length===0){
            tbody.innerHTML='<tr><td colspan="4" class="hint">{{index .T "no_tasks"}}</td></tr>';
            ['downloading','failed','done'].forEach(function(s){document.getElementById('cnt-'+s).textContent='(0)';});
            return;
          }
          tbody.innerHTML=j.tasks.map(function(t){
            return '<tr class="'+t.row_class+'" data-status="'+t.status+'" data-url="'+t.url+'">'+
              '<td title="'+t.info_hash+'">'+t.name+'</td>'+
              '<td>'+t.size+'</td>'+
              '<td>'+t.percent.toFixed(0)+'%</td>'+
              '<td><button onclick="copyTaskURL(\''+t.url+'\')" style="padding:2px 6px;font-size:10px;margin:0;" title="'+'{{index .T "copy_link_title"}}'+'">'+ICO('copy')+'</button></td>'+
              '</tr>';
          }).join('');
          // Re-count
          var c={downloading:0,failed:0,done:0};
          j.tasks.forEach(function(t){c[t.status]=(c[t.status]||0)+1;});
          document.getElementById('cnt-downloading').textContent='('+c.downloading+')';
          document.getElementById('cnt-failed').textContent='('+c.failed+')';
          document.getElementById('cnt-done').textContent='('+c.done+')';
          // Re-apply current tab
          var active=document.querySelector('#tab-downloading[style*="accent"],#tab-failed[style*="accent"],#tab-done[style*="accent"]');
          if(active)switchTaskTab(active.id.replace('tab-',''));
        }catch(e){}
      }
    </script>
    {{end}}

    <!-- log panel -->
    {{if eq .Page "log"}}
    <div class="card panel">
      <h2>{{index .T "runtime_log"}}</h2>
      <div class="log-panel" id="log-panel" style="max-height:none;">{{range .Logs}}{{if hasPrefix . "--- ["}}<span class="log-clear-marker">{{.}}</span>{{else}}{{.}}{{end}}
{{end}}</div>
    </div>
    <script>
      var logLastLine='';
      var logTimer=setInterval(refreshLogs,5000);

      function logRender(lines){
        // Newest first: each line becomes a row at the top, in the order given,
        // so an incremental block lands above everything already shown without
        // being reversed.
        var panel=document.getElementById('log-panel');
        var frag=document.createDocumentFragment();
        for(var i=0;i<lines.length;i++){
          var line=lines[i];
          if(line.indexOf('--- [')===0){
            var m=document.createElement('span');
            m.className='log-clear-marker';
            m.textContent=line;
            frag.appendChild(m);
          }else{
            frag.appendChild(document.createTextNode(line+'\n'));
          }
        }
        panel.insertBefore(frag,panel.firstChild);
      }

      async function refreshLogs(){
        try{
          var url='/api/logs';
          if(logLastLine) url+='?since='+encodeURIComponent(logLastLine);
          var r=await fetch(url);
          var j=await r.json();
          if(!j.lines||j.lines.length===0)return;
          var panel=document.getElementById('log-panel');
          if(j.reset){
            // The cursor scrolled out of the server's ring buffer. Replace the
            // content instead of prepending, which would duplicate every line.
            panel.textContent='';
          }
          logRender(j.lines);
          logLastLine=j.lines[0];
        }catch(e){}
      }

      // Seed the cursor with the first rendered line: the panel is newest-first,
      // so that is the most recent line the client already has. Using the oldest
      // line instead made ?since= match near the end of the buffer, and every
      // poll re-added the whole history.
      (function(){
        var p=document.getElementById('log-panel');
        if(!p)return;
        var t=p.textContent.trim();
        if(!t)return;
        logLastLine=t.split('\n')[0].trim();
      })();
    </script>
    {{end}}

    <!-- aggregator search page -->
    {{if eq .Page "search"}}
    <div class="card panel">
      <div style="display:flex;align-items:center;justify-content:space-between;margin-bottom:16px;">
        <h2 style="margin:0;">{{index .T "indexer_search"}}</h2>
        <button type="button" onclick="clearSearch()" style="margin:0;padding:4px 12px;font-size:12px;background:var(--accent-2);" title="{{index .T "search_refresh"}}">{{index .T "search_refresh"}}</button>
      </div>
      <form action="/search" method="post" id="search-form" autocomplete="off">
        <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
          <input name="q" id="search-q" placeholder="{{index .T "search_ph"}}" value="{{.SearchQuery}}" style="flex:3;min-width:160px;" autofocus>
          <input type="hidden" name="keyword" id="search-keyword" value="{{.SearchKeyword}}">
          <select name="sort" style="flex:1;min-width:100px;">
            <option value="seeds"{{if eq .SearchSort "seeds"}} selected{{end}}>{{index .T "sort_seeds"}}</option>
            <option value="size"{{if eq .SearchSort "size"}} selected{{end}}>{{index .T "sort_size"}}</option>
            <option value="date"{{if eq .SearchSort "date"}} selected{{end}}>{{index .T "sort_date"}}</option>
          </select>
          <button type="submit" style="margin-top:0;white-space:nowrap;">{{index .T "search_btn"}}</button>
        </div>
        {{if .IndexerList}}
        <div style="margin-top:8px;display:flex;flex-wrap:wrap;gap:4px;align-items:center;">
          <span style="font-size:12px;color:var(--muted);white-space:nowrap;">{{index .T "search_sites"}}</span>
          {{range .IndexerList}}
          <label style="font-size:11px;display:flex;align-items:center;gap:2px;cursor:pointer;padding:2px 6px;background:var(--bg);border:1px solid var(--line);border-radius:6px;">
            <input type="checkbox" name="indexer" value="{{.ID}}" style="width:auto;margin:0;"{{if indexerChecked $.SearchIndexers .ID}} checked{{end}}>
            {{.Name}}
          </label>
          {{end}}
        </div>
        {{end}}
      </form>
      {{if .SearchResults}}
      <div style="margin-top:16px;" id="search-results-wrap">
        <table class="tbl" id="search-results"><thead><tr><th>#</th><th>{{index .T "name"}}</th><th>{{index .T "size"}}</th><th>↑</th><th>{{index .T "date_label"}}</th><th>{{index .T "indexer_label"}}</th><th></th></tr></thead><tbody>
        {{range $i, $r := .SearchResults}}<tr data-title="{{$r.Title}}" data-group="{{$r.Group}}">
          <td class="muted" style="font-size:11px;text-align:center;">{{add $i 1}}</td>
          <td>{{if $r.PageURL}}<a href="{{$r.PageURL}}" target="_blank">{{$r.Title}}</a>{{else}}{{$r.Title}}{{end}}</td>
          <td class="muted">{{$r.SizeFmt}}</td><td>{{$r.Seeders}}</td><td class="muted" style="font-size:11px;">{{$r.DateFmt}}</td><td class="muted">{{$r.IndexerName}}</td>
          <td>{{if $r.MagnetURL}}<button data-magnet="{{$r.MagnetURL}}" data-title="{{$r.Title}}" onclick="addTaskWithBrowse(this)" style="background:var(--accent-2);padding:2px 8px;font-size:11px;margin:0;">+</button>{{end}}</td>
        </tr>{{end}}</tbody></table>
      <div id="pagination-bar" style="display:flex;justify-content:center;align-items:center;gap:4px;margin-top:14px;flex-wrap:wrap;"></div>
      </div>
      {{else}}{{if .SearchQuery}}<div class="hint" style="margin-top:12px;">{{index .T "search_no_result"}}</div>{{end}}{{end}}
      {{if .SearchErrors}}{{range $id, $err := .SearchErrors}}<div style="padding:4px 8px;margin:2px 0;background:#fef2f2;color:#991b1b;border-radius:6px;word-break:break-all;">{{ico "warning"}}{{$err}}</div>{{end}}{{end}}
      <!-- saved searches -->
      {{if .SavedSearches}}<div style="margin-top:16px;padding-top:12px;border-top:1px solid var(--line);"><h3 style="margin:0 0 8px;">{{index .T "saved_searches_title"}}</h3>{{range .SavedSearches}}<div style="display:flex;align-items:center;gap:8px;padding:4px 0;font-size:13px;"><span style="flex:1;">{{ico "search"}}{{.Query}}</span><form action="/search" method="post" style="display:inline;"><input type="hidden" name="q" value="{{.Query}}"><input type="hidden" name="sort" value="{{.Sort}}"><button type="submit" style="padding:2px 8px;font-size:11px;margin:0;">{{index $.T "search_btn_sm"}}</button></form><form action="/search" method="post" style="display:inline;"><input type="hidden" name="action" value="unsubscribe"><input type="hidden" name="id" value="{{.ID}}"><button type="submit" style="padding:2px 8px;font-size:11px;margin:0;background:var(--danger);">{{index $.T "delete_btn"}}</button></form></div>{{end}}</div>{{end}}
    </div>
    <script>
    document.getElementById('search-form').addEventListener('submit',function(e){
      var kw=document.getElementById('search-keyword'),bar=document.getElementById('group-chip-bar');
      if(kw&&bar){var grp={};bar.querySelectorAll('span[data-cat]').forEach(function(c){if(c.style.background==='var(--accent)'||c.style.background==='rgb(59,130,246)'){var t=c.getAttribute('data-filter');var g=c.getAttribute('data-cat');if(t&&g){if(!grp[g])grp[g]=[];if(grp[g].indexOf(t)===-1)grp[g].push(t);}}});
      var parts=[];for(var g in grp){parts.push(g+':'+grp[g].join('|'));}if(parts.length)kw.value=parts.join(' ');else kw.value='';}
      // Compare non-keyword params: if unchanged, filter from cache instead of full search
      var form=document.getElementById('search-form');var fd=new FormData(form);fd.delete('keyword');
      var curParams=new URLSearchParams(fd).toString();
      if(curParams===lastSearchParams&&curParams!==''){e.preventDefault();filterByChips();}
      else{lastSearchParams=curParams;sessionStorage.setItem('pan-fetcher-last-params',curParams);}
    });
    var lastSearchParams=sessionStorage.getItem('pan-fetcher-last-params')||'';
    var searchTotal={{.SearchTotal}};
    var pageSize={{.PageSize}};
    var currentPage=1;
    var totalPages=1;
    if(searchTotal>0){totalPages=Math.ceil(searchTotal/pageSize);}
    else{var rows=document.querySelectorAll('#search-results tbody tr');if(rows.length>0){totalPages=Math.max(1,Math.ceil(rows.length/pageSize));}}
    (function(){
      var pgKey='pan-fetcher-page';
      {{if .SearchQuery}}
      var searchFormData=new URLSearchParams(new FormData(document.getElementById('search-form'))).toString();
      sessionStorage.setItem('pan-fetcher-query',searchFormData);
      sessionStorage.setItem(pgKey,JSON.stringify({currentPage:currentPage,totalPages:totalPages,searchTotal:searchTotal,pageSize:pageSize}));
      {{else}}
      var savedQuery=sessionStorage.getItem('pan-fetcher-query');
      var savedPage=sessionStorage.getItem(pgKey);
      if(savedPage){try{var ps=JSON.parse(savedPage);currentPage=ps.currentPage||1;totalPages=ps.totalPages||1;searchTotal=ps.searchTotal||0;pageSize=ps.pageSize||50;}catch(e){}}
      if(savedQuery&&!{{.SearchQuery}}){var fd=new URLSearchParams(savedQuery);if(fd.get('q')){document.getElementById('search-q').value=fd.get('q');document.getElementById('search-form').submit();}}
      {{end}}
    })();
    </script>
    {{end}}

    <script>
    // === Shared: tag extraction, AI classification, chip rendering ===
    var activeRSSFilters=[];

    function updateRSSFilterTags(tags){
      window.activeRSSFilters=tags;
    }
    function updateChipBarFromFilters(){
      var bar=document.getElementById('group-chip-bar');if(!bar)return;
      var set=activeRSSFilters.map(function(t){return t.toLowerCase();});
      bar.querySelectorAll('span[data-filter]:not([data-filter=""])').forEach(function(c){var a=set.indexOf(c.getAttribute('data-filter').toLowerCase())!==-1;c.style.background=a?'var(--accent)':'var(--bg)';c.style.color=a?'#fff':'';c.style.borderColor=a?'var(--accent)':'var(--line)';});
      var all=bar.querySelector('span[data-filter=""]');if(all){all.style.background=activeRSSFilters.length?'var(--bg)':'var(--accent)';all.style.color=activeRSSFilters.length?'':'#fff';}
      var kw=document.getElementById('search-keyword');if(kw)kw.value=buildGroupKeyword();
    }
    function buildGroupKeyword(){
      var bar=document.getElementById('group-chip-bar');if(!bar)return activeRSSFilters.join(' ');
      var grp={};bar.querySelectorAll('span[data-cat]').forEach(function(c){if(c.style.background==='var(--accent)'||c.style.background==='rgb(59,130,246)'){var t=c.getAttribute('data-filter');var g=c.getAttribute('data-cat');if(t&&g){if(!grp[g])grp[g]=[];if(grp[g].indexOf(t)===-1)grp[g].push(t);}}});
      var parts=[];for(var g in grp){parts.push(g+':'+grp[g].join('|'));}return parts.join(' ');
    }
    async function filterByChips(){
      var kw=buildGroupKeyword();
      var fd=new URLSearchParams();
      var form=document.getElementById('search-form');
      if(form){var fdx=new FormData(form);for(var k of new Set(fdx.keys())){if(k!=='keyword'){fdx.getAll(k).forEach(function(v){fd.append(k,v);});}}}
      fd.set('keyword',kw);fd.set('offset','0');
      try{
        var r=await fetch('/search/more',{method:'POST',body:fd,headers:{'X-Requested-With':'XMLHttpRequest'}});
        var j=await r.json();
        if(!j.results||j.results.length===0){return;}
        var tbody=document.querySelector('#search-results tbody');
        if(!tbody)return;
        tbody.innerHTML=j.results.map(function(item,i){return buildRowHTML(item,i,0);}).join('');
        searchTotal=j.total||0;
        currentPage=1;
        totalPages=searchTotal>0?Math.ceil(searchTotal/pageSize):1;
        renderPagination();
        // Rebuild chip bar with filtered tags
        if(j.all_tags)window._currentAllTags=j.all_tags;
        if(j.all_groups)window._currentAllGroups=j.all_groups;
        document.getElementById('search-keyword').value=kw;
        buildGroupChips(document.getElementById('search-results-wrap'),document.getElementById('search-results'),document.getElementById('search-q')?.value||'');
        document.getElementById('search-results').scrollIntoView({block:'start'});
      }catch(e){console.error(e);}
    }
    function extractSeason(text){
      var n=0,cn={一:1,二:2,三:3,四:4,五:5,六:6,七:7,八:8,九:9,十:10,十一:11,十二:12,十三:13,十四:14,十五:15,十六:16,十七:17,十八:18,十九:19,二十:20};
      var cm=text.match(/第([一二三四五六七八九十]+|\d+)\s*(季|期)/);if(cm)n=cn[cm[1]]||parseInt(cm[1])||0;
      if(!n){var sm=text.match(/Season\s*(\d+)/i);if(sm)n=parseInt(sm[1])||0;}
      if(!n){var sm2=text.match(/(\d+)(?:st|nd|rd|th)\s*Season/i);if(sm2)n=parseInt(sm2[1])||0;}
      return n>0&&n<=99?'S'+String(n).padStart(2,'0'):'';
    }
    function classifyTag(tag){
      var t=tag.toLowerCase();
      if(/^\d+\(\d+\)$/.test(tag)||/^\d{1,4}$/.test(tag)||/^(vol|volume|disc|cd|part|pt)[\s.]*\d+$/i.test(t))return null;
      if(/^s\d{2}$/.test(t))return{cat:'season',label:'季'};
      if(/^\d{3,4}[pi]$/.test(t)||/^4k$/i.test(t))return{cat:'resolution',label:'分辨率'};
      if(/^(x26[45]|hevc|avc|av1|vp\d|flac|aac|opus|ac3|ddp?|dts|truehd|pcm|alac)/i.test(t)||/^(hevc-10bit|avc\s*aac|flac\s*\d|aac\s*avc)/i.test(t))return{cat:'codec',label:'编码'};
      if(/^(web-dl|webrip|bdrip|bd|dvdrip|hdtv|tvrip|bluray|remux)/i.test(t)||/^(viutv|baha|iqiyi|b-global|cr|netflix|amazon|hulu|disney|bahamut|aniplus|at-x)/i.test(t))return{cat:'source',label:'来源'};
      if(/^(mp4|mkv|avi|ts|m2ts)$/i.test(t))return{cat:'container',label:'容器'};
      if(/^(cht|chs|jpn|eng|kor|繁|简|日|英|中|外挂|内封|内嵌|字幕)/i.test(t)||/^(简繁|繁简|繁体|简体|中文|日语|英语|韩语)/.test(tag))return{cat:'language',label:'语言'};
      // Group: only from server-validated data-group
      if(window._serverGroups&&window._serverGroups[tag])return{cat:'group',label:'字幕组'};
      return{cat:'other',label:'其他'};
    }
    function tagRowsWithGroup(table){
      if(!table)return[];var rows=table.querySelectorAll('tbody tr');if(!rows.length)return[];var groups=[],seen={};
      window._serverGroups=window._serverGroups||{};
      rows.forEach(function(tr){var g=tr.getAttribute('data-group');if(g)window._serverGroups[g]=true;});
      rows.forEach(function(tr){var g=tr.getAttribute('data-group');if(g)window._serverGroups[g]=true;
      var td=tr.querySelector('td:nth-child(2)');var text=td?td.textContent.trim():(tr.getAttribute('data-title')||'');var allTags=[];var re=/[\[【]([^\]】]{1,40})[\]】]/g;var m;while((m=re.exec(text))!==null){var tag=m[1].trim().replace(/^DBD制作组$/,'DBD-Raws').replace(/^桜都字幕組$/,'桜都字幕组');if(!tag||tag.length>40||/^\d+\(\d+\)$/.test(tag)||/^\d{1,4}$/.test(tag)||/^(vol|volume|disc|cd|part|pt|ep)[\s.]*\d+$/i.test(tag))continue;tag=tag.replace(/\s+/g,' ').trim();if(tag)allTags.push(tag);}
      if(!allTags.length){var fm=text.match(/^([A-Za-z0-9_-]{2,20})(?=\s*[\[/])/);if(fm)allTags.push(fm[1]);}
      var pt=text.replace(/\[[^\]]*\]/g,' ').replace(/\s+/g,' ').trim();var sTag=extractSeason(pt);if(sTag)allTags.push(sTag);
      var sm=text.match(/(?:^|\s)S(\d{1,3})\b(?![^\[\]]*\])/);if(sm&&parseInt(sm[1])<=99){var st='S'+String(parseInt(sm[1])).padStart(2,'0');if(allTags.indexOf(st)===-1)allTags.push(st);}
      tr.setAttribute('data-group',allTags.join(' '));
      allTags.forEach(function(g){var gk=g.toLowerCase();if(!seen[gk]){seen[gk]=true;groups.push(g);}});
      });return groups;
    }

    // Mutable tag lists: initially from server, updated by filterByChips
    window._currentAllTags={{if .AllTagsJSON}}{{.AllTagsJSON}}{{else}}[]{{end}};
    window._currentAllGroups={{if .AllGroupsJSON}}{{.AllGroupsJSON}}{{else}}[]{{end}};

    function buildGroupChips(container,table,rssQuery){
      var allGroups=window._currentAllGroups||[];
      window._serverGroups={};
      if(allGroups&&allGroups.length){
        allGroups.forEach(function(g){window._serverGroups[g]=true;});
      }
      var clientGroups=tagRowsWithGroup(table);
      var allTags=window._currentAllTags||[];
      var groups;
      if(allTags&&allTags.length){
        groups=allTags.slice(); // copy
        // Merge active filter tags into groups so selections persist across rebuilds
        var gf=activeRSSFilters||[];
        if(gf.length){
          var seen={};
          groups.forEach(function(g){seen[g.toLowerCase()]=true;});
          gf.forEach(function(f){
            var t=f.trim();
            if(t&&!seen[t.toLowerCase()]){groups.push(t);seen[t.toLowerCase()]=true;}
          });
        }
      }else{
        groups=clientGroups;
      }
      var old=container.querySelector('#group-chip-bar');if(old)old.remove();
      if(!table.querySelectorAll('tbody tr').length&&!allTags.length)return;
      renderChipBar(container,table,groups,null,rssQuery);
    }

    function renderChipBar(container,table,groups,catMap,rssQuery){
      var bar=document.createElement('div');bar.id='group-chip-bar';bar.style.cssText='display:flex;flex-direction:column;gap:5px;margin-bottom:10px;';
      var kwEl=document.getElementById('search-keyword');var hasKw=kwEl&&kwEl.value.trim()!=='';var akw=kwEl?kwEl.value.trim().toLowerCase():'';var akwSet=[];if(akw){var ps=akw.split(/\s+/);ps.forEach(function(p){var ci=p.indexOf(':');if(ci>0){p.substring(ci+1).split('|').forEach(function(t){akwSet.push(t);});}else{akwSet.push(p);}});}
      if(hasKw){activeRSSFilters=kwEl.value.trim().split(/[\s,]+/).filter(Boolean);updateRSSFilterTags(activeRSSFilters);}
      function mkChip(t,f,isA){var c=document.createElement('span');c.textContent=t;c.setAttribute('data-filter',f);c.style.cssText='padding:3px 10px;border-radius:12px;font-size:11px;white-space:nowrap;cursor:pointer;background:'+(isA?'var(--accent)':'var(--bg)')+';color:'+(isA?'#fff':'')+';border:1px solid '+(isA?'var(--accent)':'var(--line)');return c;}
      var top=document.createElement('div');top.style.cssText='display:flex;flex-wrap:wrap;gap:6px;align-items:center;';top.appendChild(mkChip('全部','',!hasKw));
      var rss=document.createElement('span');rss.textContent='+ RSS';rss.id='chip-rss-btn';rss.style.cssText='padding:4px 14px;border-radius:14px;background:var(--accent-2);color:#fff;cursor:pointer;font-size:12px;margin-left:auto;';
      rss.onclick=function(){var sf=document.getElementById('sub-form');if(sf){var q=rssQuery||'';document.getElementById('sub-query').value=q;document.getElementById('sub-name').value=q;var u='/rss/search?q='+encodeURIComponent(q);var form=document.getElementById('search-form');if(form){var fd=new FormData(form);var s=fd.get('sort');if(s)u+='&sort='+encodeURIComponent(s);var idx=fd.getAll('indexer');if(idx.length)u+='&indexers='+encodeURIComponent(idx.join(','));}var fl=buildGroupKeyword();if(fl)u+='&keyword='+encodeURIComponent(fl);document.getElementById('sub-url').value=u;sf.style.display='flex';}};
      top.appendChild(rss);bar.appendChild(top);
      var cats={},co=[],cl={group:'{{index .T "chip_group"}}',source:'{{index .T "chip_source"}}',codec:'{{index .T "chip_codec"}}',resolution:'{{index .T "chip_resolution"}}',language:'{{index .T "chip_language"}}',container:'{{index .T "chip_container"}}',season:'{{index .T "chip_season"}}',other:'{{index .T "chip_other"}}'};
      groups.forEach(function(g){var ck;if(catMap&&catMap[g])ck=catMap[g];else{var cl2=classifyTag(g);if(!cl2)return;ck=cl2.cat;}if(!cats[ck]){cats[ck]={label:cl[ck]||ck,tags:[],key:ck};co.push(ck);}cats[ck].tags.push(g);});
      co.forEach(function(ck){var cat=cats[ck];var row=document.createElement('div');row.style.cssText='display:flex;flex-wrap:wrap;gap:4px;align-items:center;';var lbl=document.createElement('span');lbl.textContent=cat.label;lbl.style.cssText='font-size:10px;color:var(--muted);margin-right:2px;white-space:nowrap;opacity:0.7;cursor:pointer;';row.appendChild(lbl);var isCollapsed=ck!=='group';var cwrap=null;if(isCollapsed){cwrap=document.createElement('span');cwrap.style.cssText='display:none;';row.appendChild(cwrap);lbl.textContent=cat.label+'('+cat.tags.length+') ▸';lbl.onclick=function(){var s=cwrap.style.display==='none';cwrap.style.display=s?'':'none';lbl.textContent=cat.label+'('+cat.tags.length+') '+(s?'▾':'▸');};}cat.tags.forEach(function(g){var ia=akwSet.indexOf(g.toLowerCase())!==-1;var c=mkChip(g,g,ia);c.setAttribute('data-cat',ck);(isCollapsed&&cwrap?cwrap:row).appendChild(c);});bar.appendChild(row);});
      bar.addEventListener('click',function(e){if(e.target.id==='chip-rss-btn')return;if(e.target.tagName!=='SPAN')return;var f=e.target.getAttribute('data-filter');if(f===undefined||f===null)return;if(f===''){var ha=false;bar.querySelectorAll('span:not(#chip-rss-btn):not([data-filter=""])').forEach(function(c){if(c.style.background==='var(--accent)'||c.style.background==='rgb(59,130,246)')ha=true;});bar.querySelectorAll('span:not(#chip-rss-btn)').forEach(function(c){c.style.background='var(--bg)';c.style.color='';c.style.borderColor='var(--line)';});e.target.style.background='var(--accent)';e.target.style.color='#fff';e.target.style.borderColor='var(--accent)';updateRSSFilterTags([]);var ke2=document.getElementById('search-keyword');if(ke2)ke2.value='';}else{var ia=e.target.style.background==='var(--accent)'||e.target.style.background==='rgb(59,130,246)';if(ia){e.target.style.background='var(--bg)';e.target.style.color='';e.target.style.borderColor='var(--line)';}else{e.target.style.background='var(--accent)';e.target.style.color='#fff';e.target.style.borderColor='var(--accent)';}var ac=bar.querySelector('span[data-filter=""]');if(ac){ac.style.background='var(--bg)';ac.style.color='';ac.style.borderColor='var(--line)';}var act=[];bar.querySelectorAll('span:not(#chip-rss-btn):not([data-filter=""])').forEach(function(c){if(c.style.background==='var(--accent)'||c.style.background==='rgb(59,130,246)')act.push(c.getAttribute('data-filter'));});if(!act.length&&ac){ac.style.background='var(--accent)';ac.style.color='#fff';ac.style.borderColor='var(--accent)';}updateRSSFilterTags(act);var ke=document.getElementById('search-keyword');if(ke)ke.value=buildGroupKeyword();}});
      container.insertBefore(bar,table);
    }

    // Initialize chip bar on page load if search results present
    (function(){
      var wrap=document.getElementById('search-results-wrap');
      var tbl=document.getElementById('search-results');
      if(wrap&&tbl&&tbl.querySelectorAll('tbody tr').length){
        buildGroupChips(wrap,tbl,document.getElementById('search-q')?.value||'');
      }
    })();

    </script>

    <!-- discover page (TMDB) -->
    {{if eq .Page "discover"}}
    <style>
      .discover-hero{text-align:center;padding:20px 0 10px;}
      .discover-hero h2{font-size:22px;margin:0 0 6px;}
      .discover-hero p{color:var(--muted);font-size:13px;margin:0;}
      .discover-search{display:flex;gap:8px;max-width:560px;margin:0 auto;}
      .discover-search input{flex:1;font-size:15px;padding:10px 14px;border-radius:10px;}
      .discover-search button{padding:10px 24px;font-size:14px;border-radius:10px;margin:0;}
      .tmdb-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(150px,1fr));gap:14px;margin-top:16px;}
      .tmdb-card{cursor:pointer;border-radius:12px;overflow:hidden;background:var(--card);border:1px solid var(--line);transition:all .2s;position:relative;}
      .tmdb-card:hover{transform:translateY(-4px);box-shadow:0 8px 24px rgba(0,0,0,.12);border-color:var(--accent);}
      .tmdb-card.selected{border:2px solid var(--accent);box-shadow:0 0 0 3px rgba(59,130,246,.25);}
      .tmdb-poster-wrap{position:relative;aspect-ratio:2/3;overflow:hidden;background:linear-gradient(135deg,#e5e7eb,#d1d5db);}
      .tmdb-poster{width:100%;height:100%;object-fit:cover;display:block;}
      .tmdb-poster-placeholder{width:100%;height:100%;display:flex;align-items:center;justify-content:center;font-size:36px;color:#9ca3af;}
      .tmdb-card-overlay{position:absolute;bottom:0;left:0;right:0;background:linear-gradient(transparent,rgba(0,0,0,.7));padding:24px 10px 10px;}
      .tmdb-card-title{font-size:13px;font-weight:600;color:#fff;line-height:1.3;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden;text-shadow:0 1px 2px rgba(0,0,0,.5);}
      .tmdb-card-year{font-size:11px;color:rgba(255,255,255,.75);margin-top:2px;}
      .tmdb-card-type{position:absolute;top:8px;right:8px;background:rgba(0,0,0,.55);color:#fff;font-size:10px;padding:2px 8px;border-radius:10px;}
      .search-tabs{display:flex;gap:0;margin:16px 0 0;border-bottom:2px solid var(--line);}
      .search-tab{padding:10px 24px;cursor:pointer;font-size:14px;font-weight:500;color:var(--muted);border-bottom:2px solid transparent;margin-bottom:-2px;transition:.2s;}
      .search-tab:hover{color:var(--text);}
      .search-tab.active{color:var(--accent);border-bottom-color:var(--accent);}
      .season-select{display:flex;gap:8px;flex-wrap:wrap;margin-top:10px;}
      .season-chip{padding:6px 16px;border-radius:22px;font-size:13px;cursor:pointer;border:1px solid var(--line);background:var(--bg);transition:.2s;user-select:none;}
      .season-chip:hover{border-color:var(--accent);color:var(--accent);}
      .season-chip.active{background:var(--accent);color:#fff;border-color:var(--accent);}
      .search-step{display:none;animation:fadeIn .25s ease;}
      .search-step.active{display:block;}
      @keyframes fadeIn{from{opacity:0;transform:translateY(8px);}to{opacity:1;transform:translateY(0);}}
      .confirm-bar{display:flex;align-items:center;gap:12px;padding:12px 16px;background:var(--bg);border-radius:12px;margin-top:14px;border:1px solid var(--line);}
      .confirm-bar .sel-title{font-weight:600;font-size:15px;flex:1;display:flex;align-items:center;gap:8px;}
      .confirm-bar .sel-poster{width:40px;height:56px;border-radius:6px;object-fit:cover;}
      .back-to-top{position:fixed;bottom:24px;right:24px;width:44px;height:44px;border-radius:50%;background:var(--accent);color:#fff;border:none;cursor:pointer;box-shadow:0 4px 16px rgba(0,0,0,.15);display:none;align-items:center;justify-content:center;font-size:20px;z-index:100;transition:all .2s;}
      .back-to-top:hover{transform:translateY(-2px);box-shadow:0 6px 20px rgba(0,0,0,.2);}
      .back-to-top.show{display:flex;}
      /* detail modal */
      .detail-overlay{position:fixed;inset:0;z-index:200;background:rgba(0,0,0,.75);overflow-y:auto;display:flex;align-items:flex-start;justify-content:center;padding:24px 0;animation:fadeIn .2s ease;scrollbar-width:none;-ms-overflow-style:none;}
      .detail-overlay::-webkit-scrollbar{display:none;}
      .detail-card{position:relative;width:min(780px,94vw);background:var(--card);border-radius:16px;overflow:hidden;display:flex;flex-direction:column;box-shadow:0 20px 60px rgba(0,0,0,.35);margin:0 auto;}
      .detail-close{position:absolute;top:12px;right:12px;z-index:10;width:36px;height:36px;border-radius:50%;background:rgba(0,0,0,.45);color:#fff;border:none;font-size:22px;cursor:pointer;display:flex;align-items:center;justify-content:center;transition:.2s;}
      .detail-close:hover{background:rgba(0,0,0,.7);}
      .detail-backdrop-wrap{position:relative;width:100%;height:200px;overflow:hidden;flex-shrink:0;background:linear-gradient(135deg,#1e293b,#0f172a);}
      .detail-backdrop-img{width:100%;height:100%;object-fit:cover;opacity:.55;}
      .detail-body{display:flex;gap:20px;padding:0 24px 24px;margin-top:-60px;position:relative;z-index:2;}
      .detail-poster-col{flex-shrink:0;}
      .detail-poster-img{width:130px;height:195px;border-radius:10px;object-fit:cover;box-shadow:0 8px 24px rgba(0,0,0,.3);border:3px solid var(--card);}
      .detail-poster-placeholder{width:130px;height:195px;border-radius:10px;display:flex;align-items:center;justify-content:center;font-size:48px;background:linear-gradient(135deg,#374151,#1f2937);border:3px solid var(--card);}
      /* Genre filter chips under the trending tabs. Kept visually lighter than
         the tabs above so the two levels stay distinguishable. */
      .trending-filters{display:flex;align-items:center;gap:6px;flex-wrap:wrap;margin:10px 0 12px;}
      .trending-filters-label{font-size:11px;color:var(--muted);margin-right:2px;}
      .genre-chip{
        padding:3px 12px;border-radius:14px;font-size:12px;cursor:pointer;
        background:var(--bg);border:1px solid var(--line);color:var(--muted);
        transition:background .12s,color .12s,border-color .12s;user-select:none;
      }
      .genre-chip:hover{border-color:var(--accent);color:var(--accent);}
      .genre-chip.active{background:var(--accent);border-color:var(--accent);color:#fff;}
      .detail-info-col{flex:1;min-width:0;padding-top:60px;}
      .detail-info-col h2{margin:0 0 4px;font-size:22px;line-height:1.3;}
      .detail-info-col .detail-orig{font-size:13px;color:var(--muted);margin-bottom:6px;}
      .detail-tagline{font-size:14px;color:var(--accent);font-style:italic;margin:0 0 10px;}
      .detail-meta{display:flex;gap:14px;flex-wrap:wrap;font-size:13px;color:var(--muted);margin-bottom:8px;}
      .detail-meta span{display:inline-flex;align-items:center;gap:4px;}
      .detail-genres{display:flex;gap:6px;flex-wrap:wrap;margin-bottom:10px;}
      .detail-genre-chip{padding:3px 12px;border-radius:14px;font-size:11px;background:var(--bg);border:1px solid var(--line);color:var(--muted);}
      .detail-overview{font-size:13.5px;line-height:1.65;color:var(--text);margin-bottom:12px;}
      .detail-season-wrap{margin:10px 0;}
      .detail-season-wrap label{font-size:13px;color:var(--muted);display:block;margin-bottom:6px;}
      .detail-actions{display:flex;gap:10px;flex-wrap:wrap;margin-top:4px;}
      .detail-actions button{padding:10px 22px;font-size:14px;border-radius:10px;}
      @media(max-width:600px){.detail-body{flex-direction:column;align-items:center;margin-top:-40px;padding:0 16px 16px;}.detail-info-col{padding-top:10px;}.detail-poster-img{width:100px;height:150px;}.detail-backdrop-wrap{height:140px;}}
    </style>
    <div class="discover-hero">
      <h2>{{ico "tmdb"}}发现</h2>
      <p>搜索电影和剧集，找到你想要的资源</p>
    </div>
    <div class="discover-search">
      <input type="text" id="tmdb-query" placeholder="输入电影或剧集名称..." autofocus>
      <button onclick="doTMDBSearch()" style="background:var(--accent);">搜索</button>
    </div>
    <div id="tmdb-status" style="text-align:center;margin-top:8px;font-size:12px;color:var(--muted);min-height:20px;"></div>

    <div id="step-tmdb" class="search-step active">
      <div class="search-tabs" id="tmdb-tabs" style="display:none;">
        <div class="search-tab active" data-tab="movies" onclick="switchTMDTab('movies')">{{ico "film"}}电影</div>
        <div class="search-tab" data-tab="tv" onclick="switchTMDTab('tv')">{{ico "tv"}}电视节目</div>
      </div>
      <div class="tmdb-grid" id="tmdb-grid"></div>
    </div>

    <!-- Trending section -->
    <div id="trending-section" style="margin-top:24px;">
      <div style="display:flex;align-items:center;gap:8px;margin-bottom:12px;">
        <h3 style="margin:0;font-size:16px;">{{ico "film"}}热门推荐</h3>
        <span style="font-size:11px;color:var(--muted);">本周流行</span>
      </div>
      <div class="search-tabs" style="margin-bottom:0;">
        <div class="search-tab active" data-trending="movies" onclick="switchTrendingTab('movies')">{{ico "film"}}热门电影</div>
        <div class="search-tab" data-trending="tv" onclick="switchTrendingTab('tv')">{{ico "tv"}}热门剧集</div>
      </div>
      <!-- Genre filter. TMDB's trending endpoints take no genre argument, so a
           selection switches the request to /discover sorted by popularity. -->
      <div class="trending-filters" id="trending-filters">
        <span class="trending-filters-label">{{index .T "trending_genre"}}</span>
        <!-- Filled by JS from /api/tmdb/genres, with "all" always first. -->
      </div>
      <div class="tmdb-grid" id="trending-grid" style="min-height:120px;">
        <div style="grid-column:1/-1;text-align:center;padding:30px;color:var(--muted);">{{icoSpin}}</div>
      </div>
      <div style="text-align:center;margin-top:14px;" id="trending-more-wrap">
        <button id="btn-trending-more" onclick="loadMoreTrending()" style="padding:8px 28px;font-size:13px;border-radius:20px;background:var(--bg);border:1px solid var(--line);cursor:pointer;display:none;color:var(--accent);">{{ico "download"}}显示更多</button>
      </div>
    </div>

    <div id="step-season" class="search-step">
      <div class="confirm-bar">
        <span class="sel-title" id="sel-title"></span>
        <button onclick="backToTMDB()" style="margin:0;padding:6px 14px;font-size:12px;background:#6b7280;border-radius:8px;">← 返回</button>
      </div>
      <div style="margin-top:10px;font-size:13px;color:var(--muted);">选择季（可选，不选则搜索全部季）：</div>
      <div class="season-select" id="season-list"></div>
      <button id="btn-confirm-tv" onclick="confirmTVSearch()" style="margin-top:14px;padding:10px 24px;font-size:14px;background:var(--accent);border-radius:10px;">{{ico "search"}}搜索此剧集</button>
    </div>

    <div id="step-confirm" class="search-step">
      <div class="confirm-bar">
        <span class="sel-title" id="confirm-title"></span>
        <button onclick="backToTMDB()" style="margin:0;padding:6px 14px;font-size:12px;background:#6b7280;border-radius:8px;">← 重新选择</button>
      </div>
      <div id="confirm-query" style="margin-top:10px;font-size:13px;color:var(--muted);"></div>
      <div style="display:flex;gap:10px;margin-top:14px;flex-wrap:wrap;">
        <button onclick="doAggregatedSearch()" style="padding:10px 24px;font-size:14px;background:var(--accent);border-radius:10px;">{{ico "download"}}聚合搜索</button>
        <button onclick="doAggregatedSearchRSS()" style="padding:10px 24px;font-size:14px;background:var(--accent-2);border-radius:10px;">{{ico "book"}}一键订阅</button>
      </div>
    </div>

    <!-- Detail Modal -->
    <div id="detail-modal" class="detail-overlay" style="display:none;" onclick="if(event.target===this)closeDetail()">
      <div class="detail-card">
        <button class="detail-close" onclick="closeDetail()">&times;</button>
        <div class="detail-backdrop-wrap">
          <img id="detail-backdrop" class="detail-backdrop-img" src="" alt="">
        </div>
        <div class="detail-body">
          <div class="detail-poster-col">
            <img id="detail-poster" class="detail-poster-img" src="" alt="" onerror="this.style.display='none';document.getElementById('detail-poster-ph').style.display='flex';">
        <div id="detail-poster-ph" class="detail-poster-placeholder" style="display:none;">{{ico "film"}}</div>
          </div>
          <div class="detail-info-col">
            <h2 id="detail-title"></h2>
            <div class="detail-orig" id="detail-orig"></div>
            <p class="detail-tagline" id="detail-tagline"></p>
            <div class="detail-meta" id="detail-meta"></div>
            <div class="detail-genres" id="detail-genres"></div>
            <p class="detail-overview" id="detail-overview"></p>
            <div class="detail-season-wrap" id="detail-season-wrap" style="display:none;">
              <label>{{ico "tv"}}选择季：</label>
              <div class="season-select" id="detail-season-list"></div>
            </div>
            <div class="detail-actions">
              <button onclick="detailGetResources()" style="background:var(--accent);color:#fff;">{{ico "download"}}获取资源</button>
            </div>
          </div>
        </div>
      </div>
    </div>

    <script>
    var tmdbMovies=[],tmdbTV=[];
    var detailData=null, detailSeason=0;

    document.getElementById('tmdb-query').addEventListener('keydown',function(e){if(e.key==='Enter')doTMDBSearch();});

    async function doTMDBSearch(){
      var q=document.getElementById('tmdb-query').value.trim();
      if(!q)return;
      document.getElementById('tmdb-status').innerHTML='<span style="display:inline-block;width:16px;height:16px;border:2px solid var(--line);border-top-color:var(--accent);border-radius:50%;animation:spin .6s linear infinite;vertical-align:middle;margin-right:6px;"></span>搜索中...';
      try{
        var r=await fetch('/api/tmdb/search?q='+encodeURIComponent(q));
        var j=await r.json();
        if(j.error){document.getElementById('tmdb-status').textContent='!'+j.error;return;}
        tmdbMovies=j.movies||[];tmdbTV=j.tv||[];
        var total=tmdbMovies.length+tmdbTV.length;
        document.getElementById('tmdb-status').textContent='找到 '+total+' 个结果（电影 '+tmdbMovies.length+'，电视 '+tmdbTV.length+'）';
        document.getElementById('tmdb-tabs').style.display=tmdbMovies.length&&tmdbTV.length?'flex':(total?'flex':'none');
        var startTab=tmdbMovies.length?'movies':'tv';
        document.querySelectorAll('.search-tab').forEach(function(t){t.classList.toggle('active',t.getAttribute('data-tab')===startTab);});
        renderTMDBCards(startTab);
        showStep('step-tmdb');
      }catch(e){document.getElementById('tmdb-status').textContent='!'+'请求失败: '+e.message;}
    }

    function switchTMDTab(tab){
      document.querySelectorAll('.search-tab').forEach(function(t){t.classList.toggle('active',t.getAttribute('data-tab')===tab);});
      renderTMDBCards(tab);
    }

    function renderTMDBCards(tab){
      var items=tab==='movies'?tmdbMovies:tmdbTV;
      var grid=document.getElementById('tmdb-grid');
      if(!items.length){grid.innerHTML='<div style="grid-column:1/-1;text-align:center;padding:40px;color:var(--muted);">无结果</div>';return;}
      grid.innerHTML=items.map(function(it){
        var posterHTML=it.poster
          ?'<img class="tmdb-poster" src="'+it.poster+'" alt="'+it.title+'" loading="lazy" onerror="this.parentElement.innerHTML=\'<div class=&#34;tmdb-poster-placeholder&#34;>\'+ICO("film")+\'</div>\'">'
          :'<div class="tmdb-poster-placeholder">'+(it.media_type==='movie'?'film':'tv')+'</div>';
        return '<div class="tmdb-card" onclick="selectTMDB(\''+it.media_type+'\','+it.id+')" data-type="'+it.media_type+'" data-id="'+it.id+'">'
          +'<div class="tmdb-poster-wrap">'+posterHTML+'<div class="tmdb-card-overlay"><div class="tmdb-card-title">'+it.title+'</div><div class="tmdb-card-year">'+it.year+'</div></div></div>'
          +'<div class="tmdb-card-type">'+(it.media_type==='movie'?'电影':'剧集')+'</div>'
          +'</div>';
      }).join('');
    }

    async function selectTMDB(mediaType,id){
      document.querySelectorAll('.tmdb-card').forEach(function(c){c.classList.toggle('selected',c.getAttribute('data-id')==String(id));});
      try{
        var r=await fetch('/api/tmdb/detail?type='+encodeURIComponent(mediaType)+'&id='+id);
        var d=await r.json();
        if(d.error){alert('获取详情失败: '+d.error);return;}
        detailData=d; detailSeason=0;
        if(mediaType==='movie'){
          showDetail(d,0);
        }else{
          showTVSeasonSelect(d);
        }
      }catch(e){alert('请求失败: '+e.message);}
    }

    function showTVSeasonSelect(d){
      detailData=d;
      var wrap=document.getElementById('detail-season-wrap');
      var list=document.getElementById('detail-season-list');
      wrap.style.display='block';
      var seasons=d.seasons||[];
      var html='<span class="season-chip active" onclick="pickSeason(0,this)">全部 ('+(d.num_episodes||'?')+'集)</span>';
      for(var i=0;i<seasons.length;i++){
        var s=seasons[i];
        html+='<span class="season-chip" onclick="pickSeason('+s.season_number+',this)">第'+s.season_number+'季 ('+s.episode_count+'集)</span>';
      }
      if(!seasons.length){
        for(var s=1;s<=(d.num_seasons||1);s++){html+='<span class="season-chip" onclick="pickSeason('+s+',this)">第'+s+'季</span>';}
      }
      list.innerHTML=html;
      detailSeason=0;
      fillDetailCard(d);
      document.querySelector('.detail-actions').style.display='none';
      document.getElementById('detail-modal').style.display='flex';
      document.body.style.overflow='hidden';
    }

    function pickSeason(season,el){
      detailSeason=season;
      document.querySelectorAll('#detail-season-list .season-chip').forEach(function(c){c.classList.remove('active');});
      el.classList.add('active');
      document.querySelector('.detail-actions').style.display='';
    }

    function showDetail(d,season){
      fillDetailCard(d);
      document.getElementById('detail-season-wrap').style.display='none';
      document.querySelector('.detail-actions').style.display='';
      detailSeason=season||0;
      document.getElementById('detail-modal').style.display='flex';
      document.body.style.overflow='hidden';
    }

    function fillDetailCard(d){
      document.getElementById('detail-backdrop').src=d.backdrop||'';
      var posterImg=document.getElementById('detail-poster');
      var posterPh=document.getElementById('detail-poster-ph');
      if(d.poster){posterImg.src=d.poster;posterImg.style.display='';posterPh.style.display='none';}
      else{posterImg.style.display='none';posterPh.style.display='flex';posterPh.textContent=d.media_type==='movie'?'film':'tv';}
      document.getElementById('detail-title').textContent=d.title+' ('+d.year+')';
      var orig=document.getElementById('detail-orig');
      orig.textContent=d.original_title&&d.original_title!==d.title?d.original_title:'';
      orig.style.display=orig.textContent?'':'none';
      var tl=document.getElementById('detail-tagline');
      tl.textContent=d.tagline||'';
      tl.style.display=d.tagline?'':'none';
      // Meta
      var meta=[];
      if(d.vote_average>0)meta.push('⭐ '+d.vote_average.toFixed(1)+' ('+d.vote_count+'票)');
      if(d.media_type==='movie'&&d.runtime>0)meta.push('⏱ '+d.runtime+'分钟');
      if(d.status)meta.push(d.status);
      if(d.media_type==='tv'){
        if(d.num_seasons>0)meta.push(d.num_seasons+'季');
        if(d.num_episodes>0)meta.push(d.num_episodes+'集');
      }
      document.getElementById('detail-meta').innerHTML=meta.map(function(m){return '<span>'+m+'</span>';}).join('');
      // Genres
      var genres=d.genres||[];
      document.getElementById('detail-genres').innerHTML=genres.map(function(g){return '<span class="detail-genre-chip">'+g+'</span>';}).join('');
      // Overview
      document.getElementById('detail-overview').textContent=d.overview||'(暂无简介)';
    }

    function closeDetail(){
      document.getElementById('detail-modal').style.display='none';
      document.body.style.overflow='';
      detailData=null;detailSeason=0;
      document.querySelectorAll('.tmdb-card').forEach(function(c){c.classList.remove('selected');});
    }

    function detailGetResources(){
      if(!detailData)return;
      var query=detailData.title;
      if(detailData.media_type==='tv'&&detailSeason>0)query+=' S'+String(detailSeason).padStart(2,'0');
      goSearch(query,false);
    }

    function goSearch(query,subscribe){
      var form=document.createElement('form');form.method='POST';form.action='/search';
      var inp=document.createElement('input');inp.type='hidden';inp.name='q';inp.value=query;form.appendChild(inp);
      if(subscribe){var s=document.createElement('input');s.type='hidden';s.name='subscribe';s.value='1';form.appendChild(s);}
      document.body.appendChild(form);form.submit();
    }

    // Legacy step-confirm flow (kept for keyboard-only / accessibility fallback)
    var selectedItem=null, selectedSeason=0;
    function showStep(id){document.querySelectorAll('.search-step').forEach(function(s){s.classList.remove('active');});var el=document.getElementById(id);if(el)el.classList.add('active');}
    function backToTMDB(){showStep('step-tmdb');selectedItem=null;selectedSeason=0;closeDetail();}
    function doAggregatedSearch(){
      var query=selectedItem?selectedItem.title:document.getElementById('tmdb-query').value.trim();
      if(selectedItem&&selectedItem.media_type==='tv'&&selectedSeason>0)query+=' S'+String(selectedSeason).padStart(2,'0');
      var cat=selectedItem?(selectedItem.media_type==='movie'?'movie':'tv'):'';
      goSearch(query,false);
    }
    function doAggregatedSearchRSS(){
      var query=selectedItem?selectedItem.title:document.getElementById('tmdb-query').value.trim();
      if(selectedItem&&selectedItem.media_type==='tv'&&selectedSeason>0)query+=' S'+String(selectedSeason).padStart(2,'0');
      goSearch(query,true);
    }

    // --- Trending ---
    var trendingMovies=[],trendingTV=[],trendingPage=1,trendingTab='movies';
    // Selected genre id: 0 means no filter (plain trending).
    var trendingGenre=0,trendingGenres=[];

    function switchTrendingTab(tab){
      if(trendingTab===tab&&trendingMovies.length)return;
      trendingTab=tab;
      document.querySelectorAll('[data-trending]').forEach(function(t){t.classList.toggle('active',t.getAttribute('data-trending')===tab);});
      renderTrendingCards(tab);
    }

    // loadTrendingGenres fills the chip row. The "all" chip is rendered first so
    // the unfiltered view is always one click away.
    async function loadTrendingGenres(){
      var box=document.getElementById('trending-filters');
      if(!box)return;
      try{
        var r=await fetch('/api/tmdb/genres');
        var j=await r.json();
        trendingGenres=j.genres||[];
      }catch(e){ trendingGenres=[]; }
      var h='<span class="trending-filters-label">{{index .T "trending_genre"}}</span>';
      h+='<span class="genre-chip'+(trendingGenre===0?' active':'')+'" data-genre="0" onclick="selectTrendingGenre(0)">{{index .T "trending_genre_all"}}</span>';
      trendingGenres.forEach(function(g){
        h+='<span class="genre-chip'+(trendingGenre===g.id?' active':'')+'" data-genre="'+g.id+'" onclick="selectTrendingGenre('+g.id+')">'+escHTML(g.name)+'</span>';
      });
      box.innerHTML=h;
    }

    function selectTrendingGenre(id){
      if(trendingGenre===id)return;
      trendingGenre=id;
      document.querySelectorAll('#trending-filters .genre-chip').forEach(function(c){
        c.classList.toggle('active',Number(c.getAttribute('data-genre'))===id);
      });
      // The cached lists belong to the previous filter.
      trendingMovies=[];trendingTV=[];trendingPage=1;
      var grid=document.getElementById('trending-grid');
      if(grid)grid.innerHTML='<div style="grid-column:1/-1;text-align:center;padding:30px;color:var(--muted);">'+ICO('loading',true)+'</div>';
      loadTrending();
    }

    function renderTrendingCards(tab){
      var items=tab==='movies'?trendingMovies:trendingTV;
      var grid=document.getElementById('trending-grid');
      var moreBtn=document.getElementById('btn-trending-more');
      if(!items.length){
        grid.innerHTML='<div style="grid-column:1/-1;text-align:center;padding:30px;color:var(--muted);">{{index .T "trending_empty"}}</div>';
        if(moreBtn)moreBtn.style.display='none';
        return;
      }
      grid.innerHTML=items.map(function(it){
        var posterHTML=it.poster
          ?'<img class="tmdb-poster" src="'+it.poster+'" alt="'+it.title+'" loading="lazy" onerror="this.parentElement.innerHTML=\'<div class=&#34;tmdb-poster-placeholder&#34;>\'+ICO("film")+\'</div>\'">'
          :'<div class="tmdb-poster-placeholder">'+ICO(it.media_type==='movie'?'film':'tv')+'</div>';
        return '<div class="tmdb-card" onclick="selectTMDB(\''+it.media_type+'\','+it.id+')" data-type="'+it.media_type+'" data-id="'+it.id+'">'
          +'<div class="tmdb-poster-wrap">'+posterHTML+'<div class="tmdb-card-overlay"><div class="tmdb-card-title">'+it.title+'</div><div class="tmdb-card-year">'+it.year+'</div></div></div>'
          +'<div class="tmdb-card-type">'+(it.media_type==='movie'?'{{index .T "media_movie"}}':'{{index .T "media_tv"}}')+'</div>'
          +'</div>';
      }).join('');
      if(moreBtn)moreBtn.style.display='';
    }

    // trendingQuery builds the request for the current tab and filter.
    function trendingQuery(page){
      var q='/api/tmdb/trending?page='+page;
      if(trendingGenre>0)q+='&genre='+trendingGenre;
      return q;
    }

    async function loadTrending(){
      var grid=document.getElementById('trending-grid');
      if(grid)grid.innerHTML='<div style="grid-column:1/-1;text-align:center;padding:30px;color:var(--muted);">'+ICO('loading',true)+'</div>';
      try{
        var r=await fetch(trendingQuery(1));
        var j=await r.json();
        if(j.error){
          if(grid)grid.innerHTML='<div style="grid-column:1/-1;text-align:center;padding:30px;color:var(--muted);">'+escHTML(j.error)+'</div>';
          return;
        }
        trendingMovies=j.movies||[];trendingTV=j.tv||[];trendingPage=1;
        renderTrendingCards(trendingTab);
      }catch(e){}
    }

    async function loadMoreTrending(){
      var btn=document.getElementById('btn-trending-more');
      if(!btn||btn.disabled)return;
      btn.innerHTML=ICO('loading',true);btn.disabled=true;
      var moreLabel='{{index .T "trending_more"}}';
      var next=trendingPage+1;
      try{
        var r=await fetch(trendingQuery(next));
        var j=await r.json();
        if(j.error){btn.innerHTML=moreLabel;btn.disabled=false;return;}
        var newMovies=j.movies||[],newTV=j.tv||[];
        if(!newMovies.length&&!newTV.length){
          btn.innerHTML=ICO('check')+'{{index .T "trending_all_loaded"}}';btn.disabled=true;return;
        }
        trendingPage=next;
        var grid=document.getElementById('trending-grid');
        var frag=document.createDocumentFragment();
        var items=trendingTab==='movies'?newMovies:newTV;
        items.forEach(function(it){
          var div=document.createElement('div');div.className='tmdb-card';
          div.setAttribute('onclick',"selectTMDB('"+it.media_type+"',"+it.id+")");
          div.setAttribute('data-type',it.media_type);div.setAttribute('data-id',it.id);
          var posterHTML=it.poster
            ?'<img class="tmdb-poster" src="'+it.poster+'" alt="'+it.title+'" loading="lazy" onerror="this.parentElement.innerHTML=\'<div class=&#34;tmdb-poster-placeholder&#34;>\'+ICO("film")+\'</div>\'">'
            :'<div class="tmdb-poster-placeholder">'+ICO(it.media_type==='movie'?'film':'tv')+'</div>';
          div.innerHTML='<div class="tmdb-poster-wrap">'+posterHTML+'<div class="tmdb-card-overlay"><div class="tmdb-card-title">'+it.title+'</div><div class="tmdb-card-year">'+it.year+'</div></div></div><div class="tmdb-card-type">'+(it.media_type==='movie'?'{{index .T "media_movie"}}':'{{index .T "media_tv"}}')+'</div>';
          frag.appendChild(div);
        });
        grid.appendChild(frag);
        trendingMovies=trendingMovies.concat(newMovies);
        trendingTV=trendingTV.concat(newTV);
        btn.innerHTML=moreLabel;btn.disabled=false;
      }catch(e){btn.innerHTML=moreLabel;btn.disabled=false;}
    }

    loadTrendingGenres();
    loadTrending();
    var trendingObserver=new IntersectionObserver(function(entries){
      if(entries[0].isIntersecting){loadMoreTrending();}
    },{rootMargin:'200px'});
    var observeBtn=function(){var b=document.getElementById('btn-trending-more');if(b&&b.style.display!=='none')trendingObserver.observe(b);};
    var origRenderTrending=renderTrendingCards;
    renderTrendingCards=function(tab){origRenderTrending(tab);setTimeout(observeBtn,100);};
    // Back to top
    var backBtn=document.createElement('button');
    backBtn.className='back-to-top';backBtn.innerHTML='↑';backBtn.title='回到顶部';
    backBtn.onclick=function(){window.scrollTo({top:0,behavior:'smooth'});};
    document.body.appendChild(backBtn);
    window.addEventListener('scroll',function(){backBtn.classList.toggle('show',window.scrollY>400);});
    </script>
    {{end}}

    <!-- about page -->
    {{if eq .Page "about"}}
    <div class="card panel">
      <h2>{{index .T "about_title"}}</h2>
      <p style="font-size:14px;line-height:1.8;">{{index .T "about_desc"}}</p>
      <div style="margin-top:16px;display:flex;flex-wrap:wrap;gap:12px;">
        <span class="badge badge-running">Go</span>
        <span class="badge badge-running">SQLite</span>
        <span class="badge badge-done">115 API</span>
        <span class="badge badge-done">Torznab</span>
        <span class="badge badge-done">Docker</span>
      </div>
      <div style="margin-top:16px;padding:12px;background:#f8fafc;border-radius:10px;border:1px solid var(--line);">
        <table style="width:100%;font-size:13px;border-collapse:collapse;">
          <tr><td style="padding:4px 0;color:var(--muted);width:80px;">{{index .T "about_version"}}</td><td><strong>{{if .AboutVersion}}pan-fetcher {{.AboutVersion}}{{else}}{{index .T "about_version_error"}}{{end}}</strong></td></tr>
          <tr><td style="padding:4px 0;color:var(--muted);">Go</td><td>1.25</td></tr>
          <tr><td style="padding:4px 0;color:var(--muted);">{{index .T "about_platform"}}</td><td>{{index .T "about_platform_value"}}</td></tr>
          <tr><td style="padding:4px 0;color:var(--muted);">{{index .T "about_search_backend"}}</td><td>{{index .T "about_search_backend_value"}}</td></tr>
        </table>
      </div>
    </div>

    <!-- author -->
    <div class="card panel" style="margin-top:12px;">
      <h3 style="margin:0 0 8px;">{{ico "user"}}{{index .T "about_author_title"}}</h3>
      <p style="font-size:13px;line-height:1.8;margin:0 0 10px;">{{index .T "about_author_desc"}}</p>
      <div style="display:flex;flex-wrap:wrap;gap:10px;align-items:center;font-size:13px;">
        <a href="https://www8ld.com/" target="_blank" rel="noopener"
           style="display:inline-flex;align-items:center;gap:6px;padding:6px 14px;border-radius:8px;background:var(--accent);color:#fff;text-decoration:none;font-weight:600;">
          {{ico "external"}}{{index .T "about_author_site"}}
        </a>
        <a href="https://github.com/lingeringdawn" target="_blank" rel="noopener">GitHub</a>
      </div>
      <div class="hint" style="margin-top:8px;font-size:12px;">www8ld.com</div>
    </div>

    <div class="card panel" style="margin-top:12px;">
      <h3 style="margin:0 0 8px;">{{ico "link"}}{{index .T "about_links"}}</h3>
      <div style="display:flex;flex-wrap:wrap;gap:12px;font-size:13px;">
        <a href="https://github.com/lingeringdawn/pan-fetcher" target="_blank" rel="noopener">GitHub</a>
        <span style="color:var(--line);">|</span>
        <a href="https://github.com/lingeringdawn/pan-fetcher/releases" target="_blank" rel="noopener">Releases</a>
        <span style="color:var(--line);">|</span>
        <a href="https://github.com/lingeringdawn/pan-fetcher/issues" target="_blank" rel="noopener">Issues</a>
        <span style="color:var(--line);">|</span>
        <a href="https://github.com/lingeringdawn/pan-fetcher/blob/master/CHANGELOG.md" target="_blank" rel="noopener">{{index .T "about_changelog"}}</a>
      </div>
    </div>
    <div class="card panel" style="margin-top:12px;">
      <p style="font-size:12px;color:var(--muted);margin:0;">
        {{index .T "about_based_on"}}
        <a href="https://github.com/zhifengle/rss2cloud" target="_blank" rel="noopener">rss2cloud</a>
        &nbsp;·&nbsp;
        <a href="https://github.com/Jackett/Jackett" target="_blank" rel="noopener">Jackett</a>
        &nbsp;·&nbsp;
        <a href="https://github.com/Nahuimi/elevengo" target="_blank" rel="noopener">elevengo</a>
      </p>
      <p class="hint" style="margin-top:8px;">© 2025-2026 pan-fetcher</p>
    </div>
    {{end}}

    <!-- shared utility functions -->
    <script>
      function refreshSearch(){
        sessionStorage.removeItem('pan-fetcher-page');
        sessionStorage.removeItem('pan-fetcher-query');
        location.href='/search';
      }
      function clearSearch(){
        sessionStorage.removeItem('pan-fetcher-page');
        sessionStorage.removeItem('pan-fetcher-query');
        // Also clear persisted search cache so reload doesn't restore old results
        fetch('/search/clear-cache',{method:'POST',headers:{'X-Requested-With':'XMLHttpRequest'}}).then(function(){location.href='/search';});
      }
      var pendingMagnet='';
      var pendingTitle='';

      // Note: searchTotal, pageSize, currentPage, totalPages, searchDone
      // are declared above in the search page template section.

      function buildRowHTML(item, idx, pageStart){
        var num=pageStart+idx+1;
        var title=item.page_url?'<a href="'+item.page_url+'" target="_blank">'+(item.title||'')+'</a>':(item.title||'');
        var magnetBtn=item.magnet_url?'<button data-magnet="'+item.magnet_url.replace(/&/g,'&amp;').replace(/"/g,'&quot;')+'" data-title="'+item.title.replace(/&/g,'&amp;').replace(/"/g,'&quot;')+'" onclick="addTaskWithBrowse(this)" style="background:var(--accent-2);padding:2px 8px;font-size:11px;margin:0;">+</button>':'';
        return '<tr data-title="'+(item.title||'')+'" data-group="'+(item.group||'')+'"><td class="muted" style="font-size:11px;text-align:center;">'+num+'</td><td>'+title+'</td><td class="muted">'+(item.size||'-')+'</td><td>'+(item.seeders||0)+'</td><td class="muted" style="font-size:11px;">'+(item.date||'')+'</td><td class="muted">'+(item.indexer||'')+'</td><td>'+magnetBtn+'</td></tr>';
      }

      function renderPagination(){
        var bar=document.getElementById('pagination-bar');
        if(!bar)return;
        var tbl=document.getElementById('search-results');
        if(!tbl||!tbl.querySelector('tbody tr')){bar.innerHTML='';return;}
        if(totalPages<=1){bar.innerHTML='<span style="font-size:11px;color:var(--muted);">{{index .T "page_total"}}</span>'.replace('%d',searchTotal);return;}
        var html='';
        html+='<button onclick="goToPage('+(currentPage-1)+')" '+(currentPage<=1?'disabled':'')+' style="padding:4px 10px;font-size:12px;margin:0;">{{index .T "page_prev"}}</button>';
        var start=Math.max(1,currentPage-2);
        var end=Math.min(totalPages,currentPage+2);
        if(start>1){html+='<button onclick="goToPage(1)" style="padding:4px 8px;font-size:12px;margin:0;">1</button>';if(start>2)html+='<span style="padding:0 2px;">…</span>';}
        for(var i=start;i<=end;i++){
          html+='<button onclick="goToPage('+i+')" '+(i===currentPage?'disabled style="padding:4px 8px;font-size:12px;margin:0;font-weight:bold;background:var(--accent);"':'style="padding:4px 8px;font-size:12px;margin:0;"')+'>'+i+'</button>';
        }
        if(end<totalPages){if(end<totalPages-1)html+='<span style="padding:0 2px;">…</span>';html+='<button onclick="goToPage('+totalPages+')" style="padding:4px 8px;font-size:12px;margin:0;">'+totalPages+'</button>';}
        html+='<button onclick="goToPage('+(currentPage+1)+')" '+(currentPage>=totalPages?'disabled':'')+' style="padding:4px 10px;font-size:12px;margin:0;">{{index .T "page_next"}}</button>';
        html+=' <span style="font-size:11px;color:var(--muted);margin-left:8px;">{{index .T "page_total"}}</span>'.replace('%d',searchTotal);
        bar.innerHTML=html;
      }

      async function goToPage(page){
        if(page<1||page>totalPages||page===currentPage)return;
        var bar=document.getElementById('pagination-bar');
        if(bar)bar.innerHTML='<span style="font-size:12px;color:var(--muted);">{{index .T "page_loading"}}</span>';
        var form=document.getElementById('search-form');
        var fd=new URLSearchParams(new FormData(form));
        if(!fd.get('q')){
          var savedQ=sessionStorage.getItem('pan-fetcher-query');
          if(savedQ) fd=new URLSearchParams(savedQ);
        }
        if(!fd.get('q')){bar.innerHTML='<span style="color:var(--danger);">无搜索参数</span>';return;}
        fd.set('offset',(page-1)*pageSize);
        fd.set('keyword',buildGroupKeyword());
        try{
          var r=await fetch('/search/more',{method:'POST',body:fd,headers:{'X-Requested-With':'XMLHttpRequest'}});
          var j=await r.json();
          if(!j.results||j.results.length===0){renderPagination();return;}
          var tbody=document.querySelector('#search-results tbody');
          if(!tbody){renderPagination();return;}
          var pageStart=(page-1)*pageSize;
          tbody.innerHTML=j.results.map(function(item,i){return buildRowHTML(item,i,pageStart);}).join('');
          currentPage=page;
          searchTotal=j.total||0;
          totalPages=searchTotal>0?Math.ceil(searchTotal/pageSize):1;
          buildGroupChips(document.getElementById('search-results-wrap'),document.getElementById('search-results'),fd.get('q')||'');
          renderPagination();
          sessionStorage.setItem('pan-fetcher-page',JSON.stringify({currentPage:currentPage,totalPages:totalPages,searchTotal:searchTotal,pageSize:pageSize}));
          var form2=document.getElementById('search-form');
          var fd2=new URLSearchParams(new FormData(form2));
          if(fd2.get('q')) sessionStorage.setItem('pan-fetcher-query',fd2.toString());
          document.getElementById('search-results').scrollIntoView({block:'start'});
        }catch(e){console.error(e);renderPagination();}
      }

      // Init pagination bar on page load
      renderPagination();
      async function addTaskWithBrowse(el){
        pendingMagnet=el.getAttribute('data-magnet');
        pendingTitle=el.getAttribute('data-title')||'';
        window._browseShowSubdir=true;
        browseCallback=function(id){var sp=document.getElementById('browse-subdir')?.value?.trim()||'';closeModal();doAddTask(id,sp);};
        browseDirs('0');
      }
      async function doAddTask(cid,savepath){
        closeModal();
        try{
          var body='tasks='+encodeURIComponent(pendingMagnet);
          if(cid&&cid!=='0')body+='&cid='+encodeURIComponent(cid);
          if(savepath)body+='&savepath='+encodeURIComponent(savepath);
          if(pendingTitle)body+='&title='+encodeURIComponent(pendingTitle);
          var r=await fetch('/add',{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:body});
          var j=await r.json();
          if(j.status==='ok'){alertModal('{{index .T "task_added"}}');}else{alertModal(j.message||'{{index .T "add_failed"}}');}
        }catch(e){alertModal(e.message);}
        pendingMagnet='';
      }
      var browseTargetId='sub-cid';
      var browseCallback=null;
      function browseDirsFor(targetId){browseTargetId=targetId;window._browseShowSubdir=false;browseCallback=function(id){document.getElementById(targetId).value=id;closeModal();};browseDirs('0');}
      async function browseDirs(pid){
        if(!pid)pid='0';
        try{
          let r=await fetch('/subs/dirs?pid='+pid);
          let j=await r.json();
          if(!j.ok){showModal('{{index .T "error_label"}}','<p>'+j.msg+'</p>');return;}
          if(!j.entries)j.entries=[];
          if(!Array.isArray(j.entries))j.entries=[];
          var html='<div style="max-height:300px;overflow-y:auto;">';
          if(pid!=='0'){
            html+='<div style="cursor:pointer;padding:6px 8px;color:var(--accent-2);border-radius:6px;" onclick="browseDirs(\''+j.parent+'\')">{{index .T "parent_dir_label"}}</div>';
          }
          if(j.entries.length===0)html+='<p style="color:var(--muted);">{{index .T "no_subfolders"}}</p>';
          j.entries.forEach(function(e){
            html+='<div style="cursor:pointer;padding:6px 8px;margin:2px 0;border-radius:6px;display:flex;align-items:center;gap:8px;" onmouseover="this.style.background=\'#f0f4ff\'" onmouseout="this.style.background=\'\'">';
            html+=ICO('files')+'<span style="flex:1;cursor:pointer;" onclick="browseDirs(\''+e.id+'\')">'+e.name+'</span>';
            html+='<code style="font-size:11px;color:var(--muted);cursor:pointer;" onclick="browseCallback(\''+e.id+'\')" title="选定此目录">'+e.id+'</code></div>';
          });
          html+='</div>';
          updateBrowseModal('{{index $.T "select_dir_title"}}'.replace('%s',pid),html,pid);
        }catch(e){showModal('{{index $.T "error_label"}}','<p>'+e.message+'</p>');}
      }
      function updateBrowseModal(title,body,pid){
        document.getElementById('g-modal-title').textContent=title;
        var subdirHTML='';
        if(window._browseShowSubdir){
          subdirHTML='<div style="margin-top:10px;"><label style="font-size:12px;color:var(--muted);display:block;margin-bottom:4px;">{{index $.T "subdir_opt"}}:</label><input id="browse-subdir" type="text" placeholder="{{index $.T "subdir_opt"}}" style="width:100%;box-sizing:border-box;font-size:13px;padding:6px 10px;"></div>';
        }
        document.getElementById('g-modal-body').innerHTML=body+subdirHTML;
        var btns=document.getElementById('g-modal-btns');
        btns.innerHTML='<button onclick="browseCallback(\''+pid+'\')" style="margin:0;padding:6px 16px;background:var(--accent-2);">{{index $.T "select_current_dir"}}</button><button onclick="closeModal()" style="margin:0;padding:6px 16px;background:var(--danger);">{{index $.T "close_btn"}}</button>';
        document.getElementById('g-modal').style.display='flex';
      }
    </script>

    <!-- indexer management -->
    {{if eq .Page "indexers"}}

    <!-- Jackett connection status. Environment checks, install, update and
         uninstall live on the settings page: they are configuration, not
         everyday indexer management. -->
    <div class="card panel" id="jk-setup-panel">
      <h2>{{ico "indexers"}}{{index .T "jk_setup_title"}}</h2>
      <div id="jk-setup-body"><span class="hint">{{icoSpin}}</span></div>
    </div>

    <!-- active indexers -->
    <div class="card panel" style="margin-top:16px;">
      <h2>{{index .T "indexer_list"}} ({{len .IndexerList}})</h2>
      {{if .IndexerList}}
      <table class="tbl" id="idx-active">
        <thead><tr><th>{{index .T "name"}}</th><th>{{index .T "sub_type"}}</th><th>{{index .T "jk_id"}}</th><th>{{index .T "idx_lang"}}</th><th></th></tr></thead>
        <tbody>
        {{range .IndexerList}}<tr id="row-{{.ID}}">
          <td>{{if .SiteLink}}<a href="{{.SiteLink}}" target="_blank">{{.Name}}</a>{{else}}<strong>{{.Name}}</strong>{{end}}</td>
          <td class="muted">{{.Type}}</td>
          <td class="muted" style="font-size:11px;">{{.ID}}</td>
          <td class="muted">{{.Language}}</td>
          <td style="white-space:nowrap;">
            <button onclick="jkDeactivate('{{.ID}}')" style="padding:2px 6px;font-size:11px;margin:0;background:var(--danger);" title="{{index $.T "remove"}}">−</button>
          </td>
        </tr>{{end}}
        </tbody>
      </table>
      {{else}}
      <div class="hint">{{index .T "idx_no_active"}}</div>
      {{end}}
    </div>

    <!-- jackett library (loaded async) -->
    <div class="card panel" style="margin-top:16px;">
      <h2>{{index .T "idx_lib_jackett"}} (<span id="jk-count">…</span>) 
        <span id="jk-batch-btn"></span>
        <button onclick="showJKAddModal()" id="jk-add-btn" style="margin:0 0 0 8px;padding:4px 12px;font-size:12px;background:var(--accent-2);">{{index .T "jk_add_lib"}}</button>
      </h2>
      <div id="jk-content"><span class="hint">{{icoSpin}}</span></div>
    </div>

    <script>
      async function apiPost(action, fields){
        let form=new URLSearchParams();
        form.set('action',action);
        for(let k in fields){
          let v=fields[k];
          if(Array.isArray(v)) v.forEach(x=>form.append(k,x));
          else form.set(k,v);
        }
        let r=await fetch('/indexers',{method:'POST',body:form,headers:{'X-Requested-With':'XMLHttpRequest'}});
        try{return await r.json();}catch(e){return {};}
      }

      // ---- Jackett setup panel (discovery / install) ----
      // A URL alone is not enough to search: the Torznab API needs the key. Both
      // must be present for the panel to report a usable connection, which is
      // also the condition the backend enforces.
      var jkSetupState={configured:{{if and .JackettURL .JackettAPIKey}}true{{else}}false{{end}}};

      function renderJKSetup(info){
        var el=document.getElementById('jk-setup-body');
        if(!el) return;
        var h='';
        if(jkSetupState.configured){
          h+='<div style="display:flex;align-items:center;gap:10px;flex-wrap:wrap;">';
          h+='<span style="font-size:13px;color:var(--accent-2);font-weight:600;">● {{index .T "jk_connected"}}</span>';
          h+='<code style="font-size:12px;background:var(--bg);padding:2px 8px;border-radius:6px;">'+ (info&&info.url ? info.url : '{{.JackettURL}}') +'</code>';
          h+='<button type="button" onclick="jkDiscover()" style="margin:0;padding:4px 12px;font-size:12px;background:var(--accent-2);">{{index .T "jk_redetect"}}</button>';
          h+='<a href="/settings" style="font-size:12px;color:var(--accent);">{{index .T "jk_edit_manual"}}</a>';
          h+='</div>';
        } else {
          h+='<div class="hint" style="margin-bottom:10px;">{{index .T "jk_not_configured_hint"}}</div>';
          h+='<div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;">';
          h+='<button type="button" id="jk-detect-btn" onclick="jkDiscover()" style="margin:0;padding:6px 14px;font-size:13px;background:var(--accent-2);">{{index .T "jk_detect_btn"}}</button>';
          h+='<button type="button" id="jk-install-btn" onclick="jkInstall()" style="margin:0;padding:6px 14px;font-size:13px;background:var(--accent);">{{index .T "jk_install_btn"}}</button>';
          h+='<a href="/settings" style="font-size:12px;color:var(--accent);">{{index .T "jk_manual_link"}}</a>';
          h+='</div>';
        }
        h+='<div id="jk-setup-result" style="margin-top:10px;font-size:12px;"></div>';
        el.innerHTML=h;
      }

      function jkSetupMsg(text,isErr){
        var el=document.getElementById('jk-setup-result');
        if(el) el.innerHTML='<span style="color:'+(isErr?'var(--danger)':'var(--accent-2)')+';">'+text+'</span>';
      }

      async function jkDiscover(){
        jkSetupMsg('{{index .T "jk_detecting"}}',false);
        try{
          var r=await fetch('/indexers/jackett/discover');
          var j=await r.json();
          if(!j.ok){jkSetupMsg(j.msg||'failed',true);return;}
          if(!j.found){
            jkSetupMsg('{{index .T "jk_not_found"}}',true);
            return;
          }
          // Persist only the values actually discovered, so an empty result can
          // never blank out a working configuration.
          var fd=new URLSearchParams();
          if(j.url) fd.set('jackett_url',j.url);
          if(j.apikey) fd.set('jackett_apikey',j.apikey);
          if(j.admin_password) fd.set('jackett_admin_password',j.admin_password);
          if([...fd.keys()].length){
            await fetch('/settings',{method:'POST',body:fd,headers:{'X-Requested-With':'XMLHttpRequest'}}).catch(function(){});
          }
          var msg='{{index .T "jk_found"}}: '+(j.url||'')+(j.running?'':' ({{index .T "jk_not_running"}})');
          jkSetupMsg(msg,false);
          // The page is reloaded so the panel re-renders from the server's view
          // of the stored configuration. Deriving "configured" from the probe
          // reply was wrong: discovery reads Jackett's config file, which may be
          // unreadable (for example HOME unset under systemd) even though the
          // saved URL and key are perfectly valid — and that marked a working
          // setup as unconfigured.
          setTimeout(function(){location.reload();},900);
        }catch(e){jkSetupMsg(e.message,true);}
      }

      // Load Jackett indexers async
      async function loadJackettLib(){
        var ct=document.getElementById('jk-content');
        var cnt=document.getElementById('jk-count');
        if(cnt) cnt.textContent='…';
        try{
          var r=await fetch('/indexers/jackett');
          var j=await r.json();
          if(!j.ok||!j.data||!j.data.length){
            ct.innerHTML='<div class="hint">{{index .T "jk_lib_empty"}}</div>';
            cnt.textContent='0';
            return;
          }
          // Use server-provided active list (always accurate, not stale DOM)
          var jkActiveIds=new Set(j.active||[]);
          cnt.textContent=j.data.length;
          var h='<table class="tbl"><thead><tr><th></th><th>{{index .T "name"}}</th><th>{{index .T "sub_type"}}</th><th>{{index .T "jk_id"}}</th><th>{{index .T "idx_lang"}}</th><th></th></tr></thead><tbody>';
          j.data.forEach(function(x){
            var isActive=jkActiveIds.has(x.id);
            h+='<tr id="jk-row-'+x.id+'"'+(isActive?' style="opacity:0.6"':'')+'><td>'+(isActive?'':'<input type="checkbox" name="jk_ids" value="'+x.id+'" style="width:auto;margin:0;">')+'</td>';
            h+='<td>'+(x.site_link?'<a href="'+x.site_link+'" target="_blank">'+x.name+'</a>':'<strong>'+x.name+'</strong>')+(x.description?'<br><small class="muted">'+x.description+'</small>':'')+'</td>';
            h+='<td class="muted">'+(x.type||'')+'</td>';
            h+='<td class="muted" style="font-size:11px;">'+x.id+'</td>';
            h+='<td class="muted">'+(x.language||'')+'</td>';
            h+='<td style="white-space:nowrap;">';
            if(isActive){
              h+='<span style="font-size:11px;color:var(--accent-2);" title="{{index .T "idx_jk_activated"}}">{{index .T "idx_activated"}}</span> ';
            }else{
              h+='<button onclick="jkActivate(\''+x.id+'\')" style="padding:2px 6px;font-size:11px;margin:0;background:var(--accent);" title="{{index .T "idx_activate_hint"}}">+</button> ';
            }
            h+='<button onclick="jkRemoveFromJackett(\''+x.id+'\',\''+x.name.replace(/'/g,"\\'")+'\')" style="padding:2px 6px;font-size:10px;margin:0 0 0 2px;background:var(--danger);" title="{{index .T "idx_jk_delete_hint"}}">{{ico "close"}}</button>';
            h+='</td></tr>';
          });
          h+='</tbody></table>';
          document.getElementById('jk-batch-btn').innerHTML='<button onclick="jkActivateSelected()" style="margin:0 0 0 12px;padding:4px 12px;font-size:12px;background:var(--accent);">{{index .T "idx_batch_add"}}</button>';
          ct.innerHTML=h;
        }catch(e){
          ct.innerHTML='<div class="hint" style="color:var(--danger);">'+ICO('close')+' '+e.message+'</div>';
          cnt.textContent='?';
        }
      }

      // Show modal to add indexers from Jackett's full catalog
      var jkAllData=[];
      async function showJKAddModal(){
        // Reset paging and filter state so reopening the modal starts fresh;
        // otherwise it would resume mid-list from the previous visit.
        jkAddRendered = 0;
        jkAddFilter = '';
        showModal('{{index .T "jk_add_lib"}}',
          '<div style="margin-bottom:10px;"><input id="jk-add-filter" placeholder="{{index .T "jk_search_ph"}}" style="width:100%;padding:8px;" oninput="filterJKAddList()" autofocus></div>'+ 
          '<div id="jk-add-list" style="max-height:50vh;overflow-y:auto;">{{icoSpin}}</div>',
          [{text:'{{index .T "close_btn"}}',cls:'var(--danger)',cb:function(){closeModal()}}]);
        try{
          var r=await fetch('/indexers/jackett/all');
          var j=await r.json();
          if(!j.ok||!j.data){
            document.getElementById('jk-add-list').innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+j.msg+'</span>';
            return;
          }
          jkAllData=j.data;
          renderJKAddList();
        }catch(e){
          document.getElementById('jk-add-list').innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
        }
      }

      // Rendering all ~650 indexers at once froze the modal on low-end hosts
      // (Jackett itself runs on the same box). Page them instead, and appending
      // on scroll keeps the first paint immediate.
      const JK_ADD_PAGE = 50;
      var jkAddRendered = 0;
      var jkAddFilter = '';

      function filterJKAddList(){
        jkAddFilter = (document.getElementById('jk-add-filter').value || '').toLowerCase();
        jkAddRendered = 0;
        renderJKAddList();
      }

      function jkAddMatches(x){
        if(!jkAddFilter) return true;
        return x.name.toLowerCase().indexOf(jkAddFilter) >= 0
            || x.id.toLowerCase().indexOf(jkAddFilter) >= 0;
      }

      // The table shell is built once and only rows are appended afterwards.
      //
      // The previous version rebuilt the whole table on every page and tried to
      // merge pages by stripping everything before "<tbody>". That also removed
      // the closing </tbody></table>, and the "load more" button was emitted
      // inside that markup — so after the first append the document had an
      // unterminated table plus a stray button, and the layout broke apart.
      function jkAddShell(){
        var box = document.getElementById('jk-add-list');
        if(!box) return null;
        if(document.getElementById('jk-add-tbody')) return box;
        box.innerHTML =
          '<table class="tbl"><thead><tr>'
          + '<th>{{index .T "name"}}</th><th>{{index .T "sub_type"}}</th>'
          + '<th>{{index .T "idx_lang"}}</th><th></th>'
          + '</tr></thead><tbody id="jk-add-tbody"></tbody></table>'
          + '<div id="jk-add-footer" style="text-align:center;padding:8px;"></div>';
        return box;
      }

      function jkAddRow(x){
        var h = '<tr><td>'
          + (x.site_link
              ? '<a href="'+x.site_link+'" target="_blank">'+x.name+'</a>'
              : '<strong>'+x.name+'</strong>')
          + '</td>';
        h += '<td class="muted">'+(x.type||'')+'</td>';
        h += '<td class="muted">'+(x.language||'')+'</td>';
        h += '<td style="white-space:nowrap;">';
        if(x.configured){
          h += '<span style="font-size:11px;color:var(--muted);">{{index .T "configured"}}</span>';
        }else{
          h += '<button data-jk-add="'+x.id+'" style="padding:2px 8px;font-size:11px;margin:0;background:var(--accent-2);">{{index .T "add"}}</button>';
        }
        h += '</td></tr>';
        return h;
      }

      function renderJKAddList(){
        var box = jkAddShell();
        if(!box) return;
        var tbody = document.getElementById('jk-add-tbody');
        var footer = document.getElementById('jk-add-footer');

        var visible = jkAllData.filter(jkAddMatches);
        var slice = visible.slice(jkAddRendered, jkAddRendered + JK_ADD_PAGE);

        if(jkAddRendered === 0){
          tbody.innerHTML = '';
          if(visible.length === 0){
            footer.innerHTML = '<div class="hint">{{index .T "jk_no_match"}}</div>';
            return;
          }
        }

        var h = '';
        slice.forEach(function(x){ h += jkAddRow(x); });
        // Append rows into the existing tbody: the table structure is never
        // rebuilt, so nothing can end up half-closed.
        tbody.insertAdjacentHTML('beforeend', h);
        jkAddRendered += slice.length;

        if(jkAddRendered < visible.length){
          footer.innerHTML = '<button id="jk-add-more" style="margin:0;padding:4px 14px;font-size:12px;'
            + 'background:var(--bg);color:var(--accent-2);border:1px solid var(--line);">'
            + '{{index .T "loading"}} (' + jkAddRendered + '/' + visible.length + ')</button>';
          var more = document.getElementById('jk-add-more');
          if(more){ more.onclick = function(){ more.disabled = true; more.textContent = '…'; renderJKAddList(); }; }
        }else{
          footer.innerHTML = '<span class="hint">' + jkAddRendered + '/' + visible.length + '</span>';
        }

        // Wire only the buttons that do not have a handler yet, so re-rendering
        // never re-binds (or double-fires) an existing row.
        tbody.querySelectorAll('button[data-jk-add]').forEach(function(b){
          if(b.dataset.bound) return;
          b.dataset.bound = '1';
          b.onclick = function(){ jkAddToJackett(b.getAttribute('data-jk-add'), b); };
        });
      }

      function jkAddToJackett(id, btn){
        if(btn){btn.disabled = true; btn.textContent = '…';}
        // Do not reload the page on success: the modal stays where the user is
        // and only this row changes. A full reload re-fetches every indexer.
        apiPost('jk_add_to_jackett',{id:id}).then(function(r){
          if(r.ok){
            if(btn){
              btn.disabled = false;
              btn.removeAttribute('data-jk-add');
              btn.onclick = null;
              btn.textContent = '{{index .T "configured"}}';
              btn.style.background = 'transparent';
              btn.style.color = 'var(--muted)';
            }
          }else{
            if(btn){btn.disabled = false; btn.textContent = '{{index .T "add"}}';}
            alertModal('添加失败: '+r.msg);
          }
        }).catch(function(){
          if(btn){btn.disabled = false; btn.textContent = '{{index .T "add"}}';}
        });
      }

      async function jkRemoveFromJackett(id,name){
        if(!(await confirmAsync('从 Jackett 删除 <b>'+(name||id)+'</b>？<br><small style="color:var(--danger);">这将移除此索引器在 Jackett 中的所有配置</small>'))) return;
        showModal('删除索引器','正在从 Jackett 移除 <b>'+id+'</b>…');
        apiPost('jk_remove_from_jackett',{id}).then(function(r){
          closeModal();
          if(r.ok){location.reload();}
          else{alertModal('移除失败: '+r.msg);}
        });
      }
      async function jkActivate(id){
        // Instant visual feedback
        var row=document.getElementById('jk-row-'+id);
        if(row){row.style.opacity='0.5';var td=row.querySelectorAll('td');if(td.length>=6)td[5].innerHTML='<span style="font-size:11px;color:var(--accent-2);">已激活</span>';if(td.length>=1)td[0].innerHTML='';}
        await apiPost('jk_activate',{id});
        location.reload();
      }
      async function jkDeactivate(id){
        var row=document.getElementById('jk-row-'+id);
        if(row){row.style.opacity='1';var td=row.querySelectorAll('td');if(td.length>=6)td[5].innerHTML='<button onclick="jkActivate(\''+id+'\')" style="padding:2px 6px;font-size:11px;margin:0;background:var(--accent);">+</button>';if(td.length>=1)td[0].innerHTML='<input type="checkbox" name="jk_ids" value="'+id+'" style="width:auto;margin:0;">';}
        await apiPost('jk_deactivate',{id});
        location.reload();
      }
      async function jkActivateSelected(){
        var checks=document.querySelectorAll('#jk-content input[type="checkbox"]:checked');
        if(checks.length===0) return;
        for(var c of checks){
          var id=c.value;
          var row=document.getElementById('jk-row-'+id);
          if(row){row.style.opacity='0.5';var td=row.querySelectorAll('td');if(td.length>=6)td[5].innerHTML='<span style="font-size:11px;color:var(--accent-2);">已激活</span>';if(td.length>=1)td[0].innerHTML='';}
          await apiPost('jk_activate',{id:c.value});
        }
        location.reload();
      }
      renderJKSetup(null);
      loadJackettLib();
    </script>
    {{end}}

    <!-- settings -->
    {{if eq .Page "settings"}}
    <div class="card panel">
      <h2>{{index .T "sys_settings"}}</h2>
      <!-- login -->
      <div style="margin-bottom:14px;padding:12px;background:#f8fafc;border-radius:10px;border:1px solid var(--line);">
        <h3 style="margin:0 0 8px;">{{index .T "login_115"}}
          <span style="font-weight:400;font-size:13px;margin-left:8px;">
            {{if .LoggedIn}}<span style="color:var(--accent-2);">{{index .T "connected"}}</span>{{else}}<span style="color:var(--danger);">{{index .T "disconnected"}}</span>{{end}}
          </span>
          <button id="test115-btn" style="margin:0 0 0 8px;padding:2px 10px;font-size:12px;background:var(--accent-2);" onclick="test115()">{{index .T "test_conn"}}</button>
          <span id="test115-result" style="font-size:12px;margin-left:6px;"></span>
        </h3>
        <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
          <div style="flex:3;min-width:240px;">
            <label>{{index .T "cookies_label"}}</label>
            <input name="cookies" form="frm-cookies" placeholder="{{index .T "cookies_ph"}}" value="{{.Cookies}}">
          </div>
          <button type="submit" form="frm-cookies" style="margin-top:0;">{{index .T "update_cookies"}}</button>
          <button type="button" id="qr-login-btn" onclick="startQRLogin()" style="margin-top:0;padding:8px 14px;background:var(--accent-2);color:#fff;border-radius:10px;font-size:14px;border:none;cursor:pointer;">{{index .T "qr_login"}}</button>
        </div>
        <form id="frm-cookies" action="/login/cookies" method="post" style="display:none;"></form>
        <div id="qr-box" style="margin-top:10px;text-align:center;display:none;">
          <p>{{index .T "qrcode_wait"}}</p>
          <img id="qr-img" src="" style="max-width:200px;display:none;">
          <p id="qr-status" style="color:var(--muted);">{{index .T "qrcode_scanning"}}</p>
        </div>
          <script>
            async function startQRLogin(){
              var box=document.getElementById('qr-box');
              box.style.display='block';
              var btn=document.getElementById('qr-login-btn');
              btn.disabled=true;btn.textContent='{{index .T "waiting"}}';
              try {
                let r=await fetch('/login/qrcode',{method:'POST'});
                let d=await r.json();
                if(d.qrcode){
                  document.getElementById('qr-img').src=d.qrcode;
                  document.getElementById('qr-img').style.display='inline';
                  let polls=0;
                  let poll=setInterval(async()=>{
                    polls++;
                    if(polls>150){clearInterval(poll);document.getElementById('qr-status').textContent='{{index .T "qrcode_timeout"}}';btn.disabled=false;btn.textContent='{{index .T "qr_login"}}';return;}
                    let s=await fetch('/login/qrcode?poll=1');
                    let j=await s.json();
                    if(j.status==='ok'){document.getElementById('qr-status').textContent='{{index .T "qrcode_ok"}}';clearInterval(poll);btn.disabled=false;btn.textContent='{{index .T "qr_login"}}';}
                    else if(j.status!=='waiting'){document.getElementById('qr-status').textContent=j.status;clearInterval(poll);btn.disabled=false;btn.textContent='{{index .T "qr_login"}}';}
                  },2000);
                }else{document.getElementById('qr-status').textContent='{{index .T "qrcode_error"}}';btn.disabled=false;btn.textContent='{{index .T "qr_login"}}';}
              }catch(e){document.getElementById('qr-status').textContent='{{index .T "net_error"}}';btn.disabled=false;btn.textContent='{{index .T "qr_login"}}';}
            }
          </script>
      </div>
      <script>
        async function test115(){
          var btn=document.getElementById('test115-btn');
          var sp=document.getElementById('test115-result');
          btn.disabled=true;sp.textContent='{{index .T "testing"}}';sp.style.color='var(--muted)';
          try{
            var r=await fetch('/settings/test115');
            var j=await r.json();
            if(j.ok){sp.innerHTML=ICO('check')+' '+j.msg;sp.style.color='var(--accent-2)';}
            else{sp.innerHTML=ICO('close')+' '+j.msg;sp.style.color='var(--danger)';}
          }catch(e){sp.innerHTML='{{ico "close"}}{{index .T "net_error"}}';sp.style.color='var(--danger)';}
          btn.disabled=false;
        }
        async function testJackett(){
          var btn=document.getElementById('jk-test-btn');
          var sp=document.getElementById('jk-test-result');
          var urlEl=document.querySelector('input[name="jackett_url"]');
          var keyEl=document.querySelector('input[name="jackett_apikey"]');
          btn.disabled=true;sp.textContent='{{index .T "testing"}}';sp.style.color='var(--muted)';
          try{
            var fd=new URLSearchParams();
            fd.append('url',urlEl.value.trim());
            fd.append('apikey',keyEl.value.trim());
            var r=await fetch('/settings/test-jackett',{method:'POST',body:fd});
            var j=await r.json();
            if(j.ok){sp.innerHTML=ICO('check')+' '+j.msg;sp.style.color='var(--accent-2)';}
            else{sp.innerHTML=ICO('close')+' '+j.msg;sp.style.color='var(--danger)';}
          }catch(e){sp.innerHTML='{{ico "close"}}{{index .T "net_error"}}';sp.style.color='var(--danger)';}
          btn.disabled=false;
        }

        // ---- generic webhooks ----------------------------------------------
        var webhookEvents=[];

        function webhookMsg(text,isErr){
          var el=document.getElementById('webhook-msg');
          if(!el)return;
          el.innerHTML='<span style="color:'+(isErr?'var(--danger)':'var(--accent-2)')+';">'+ICO(isErr?'close':'check')+' '+text+'</span>';
          if(!isErr) setTimeout(function(){el.innerHTML='';},2500);
        }

        async function loadWebhooks(){
          var box=document.getElementById('webhook-list');
          if(!box)return;
          try{
            var r=await fetch('/api/webhooks');
            var j=await r.json();
            if(!j.ok){box.innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+(j.msg||'failed')+'</span>';return;}
            webhookEvents=j.events||[];
            applyWebhooks(j.webhooks||[]);
          }catch(e){
            box.innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }

        function webhookEventCheckboxes(selected){
          selected=selected||[];
          var h='';
          webhookEvents.forEach(function(ev){
            var on=selected.indexOf(ev.value)>=0;
            h+='<label style="display:inline-flex;align-items:center;gap:4px;font-size:12px;margin:0 10px 4px 0;cursor:pointer;">'
              +'<input type="checkbox" name="events" value="'+ev.value+'" style="width:auto;margin:0;"'+(on?' checked':'')+'>'
              +ev.label+'</label>';
          });
          return h;
        }

        // The rows are rebuilt from the server's list after every mutation, so
        // the page never shows a state the server does not have.
        function renderWebhooks(list){
          var box=document.getElementById('webhook-list');
          if(!box)return;
          if(!list.length){
            box.innerHTML='<div class="hint">{{index .T "webhook_empty"}}</div>';
            return;
          }
          var labels={};
          webhookEvents.forEach(function(e){labels[e.value]=e.label;});
          var h='';
          list.forEach(function(w){
            var kind=w.kind||'generic';
            h+='<div style="border:1px solid var(--line);border-radius:8px;padding:10px;margin-bottom:8px;background:var(--bg);">';
            h+='<div style="display:flex;align-items:center;gap:8px;flex-wrap:wrap;">';
            h+='<strong style="font-size:13px;">'+escHTML(w.name||w.id)+'</strong>';
            h+='<span class="badge" style="font-size:10px;">'+(kind==='wework'?'{{index .T "webhook_kind_wework"}}':'JSON')+'</span>';
            h+=w.secret?'<span class="badge badge-done" style="font-size:10px;">HMAC</span>':'';
            h+=w.enabled?'<span class="badge badge-running" style="font-size:10px;">{{index .T "enabled"}}</span>'
                        :'<span class="badge" style="font-size:10px;opacity:.6;">{{index .T "disabled"}}</span>';
            h+='<span style="flex:1;"></span>';
            h+='<button type="button" onclick="webhookEdit(\''+w.id+'\')" style="margin:0;padding:3px 10px;font-size:11px;">{{index .T "webhook_edit"}}</button>';
            h+='<button type="button" onclick="webhookToggle(\''+w.id+'\','+(w.enabled?'false':'true')+')" style="margin:0 0 0 4px;padding:3px 10px;font-size:11px;">'+(w.enabled?'{{index .T "disable"}}':'{{index .T "enable"}}')+'</button>';
            h+='<button type="button" onclick="webhookTest(\''+w.id+'\')" style="margin:0 0 0 4px;padding:3px 10px;font-size:11px;background:var(--accent-2);">{{index .T "webhook_test"}}</button>';
            h+='<button type="button" onclick="webhookDelete(\''+w.id+'\')" style="margin:0 0 0 4px;padding:3px 10px;font-size:11px;background:var(--danger);">{{index .T "webhook_delete"}}</button>';
            h+='</div>';
            h+='<div style="font-size:11px;color:var(--muted);margin-top:4px;word-break:break-all;">'+escHTML(w.url)+'</div>';
            if(w.events&&w.events.length){
              h+='<div style="font-size:11px;color:var(--muted);margin-top:4px;">{{index .T "webhook_events"}}: '
                +w.events.map(function(e){return escHTML(labels[e]||e);}).join('、')+'</div>';
            }else{
              h+='<div style="font-size:11px;color:var(--muted);margin-top:4px;">{{index .T "webhook_events_all"}}</div>';
            }
            h+='</div>';
          });
          box.innerHTML=h;
        }

        // Keep the latest list for the edit form, then render it.
        function applyWebhooks(list){
          window.__webhooks=list||[];
          renderWebhooks(window.__webhooks);
        }

        // Minimal escaping: webhook values are user input echoed back into HTML.
        function escHTML(s){
          return String(s==null?'':s).replace(/[&<>"']/g,function(c){
            return {'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c];
          });
        }

        function webhookAdd(){
          webhookForm('add','',{
            name:'', url:'', kind:'generic', events:[]
          });
        }

        // Adding a WeChat Work entry pre-fills the kind and a recognizable name,
        // so the destination's payload format is right without the user having
        // to know that the two differ.
        function webhookAddWework(){
          webhookForm('add','',{
            name:'{{index .T "wework_label"}}',
            url:'https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=',
            kind:'wework',
            events:[]
          });
        }

        function webhookEdit(id){
          var w=(window.__webhooks||[]).filter(function(x){return x.id===id;})[0];
          if(!w)return;
          webhookForm('update',id,{
            name:w.name||'', url:w.url||'', kind:w.kind||'generic', events:w.events||[]
          });
        }

        function webhookForm(action,id,w){
          var kind=w.kind||'generic';
          var h='<div><label>{{index .T "webhook_kind"}}</label>'
            +'<select id="wh-kind" style="width:100%;" onchange="webhookKindChanged()">'
            +'<option value="generic"'+(kind==='generic'?' selected':'')+'>{{index .T "webhook_kind_generic"}}</option>'
            +'<option value="wework"'+(kind==='wework'?' selected':'')+'>{{index .T "webhook_kind_wework"}}</option>'
            +'</select><div class="hint" style="font-size:11px;" id="wh-kind-hint"></div></div>'
            +'<div style="margin-top:8px;"><label>{{index .T "webhook_name"}}</label><input id="wh-name" value="'+escHTML(w.name)+'" style="width:100%;"></div>'
            +'<div style="margin-top:8px;"><label>{{index .T "webhook_url"}}</label><input id="wh-url" value="'+escHTML(w.url)+'" style="width:100%;"></div>'
            +'<div style="margin-top:8px;" id="wh-secret-wrap"><label>{{index .T "webhook_secret"}}</label><input id="wh-secret" placeholder="{{index .T "webhook_secret_ph"}}" style="width:100%;"></div>'
            +'<div style="margin-top:10px;"><label>{{index .T "webhook_events"}}</label><div style="margin-top:4px;">'+webhookEventCheckboxes(w.events)+'</div>'
            +'<div class="hint" style="font-size:11px;">{{index .T "webhook_events_all"}}</div></div>';
          showModal(action==='add'?'{{index .T "webhook_add"}}':'{{index .T "webhook_edit"}}', h,
            [{text:'{{index .T "webhook_cancel"}}',cls:'var(--danger)',cb:function(){closeModal()}},
             {text:'{{index .T "webhook_save"}}',cls:'var(--accent-2)',cb:function(){webhookSubmit(action,id);}}]);
          webhookKindChanged();
        }

        // The secret field is meaningless for WeChat Work (it authenticates by
        // the token in the URL) and generic entries are not signed without it,
        // so the hint tracks the selection.
        function webhookKindChanged(){
          var sel=document.getElementById('wh-kind');
          var hint=document.getElementById('wh-kind-hint');
          var wrap=document.getElementById('wh-secret-wrap');
          if(!sel)return;
          var wework=sel.value==='wework';
          if(hint) hint.textContent=wework?'{{index .T "webhook_kind_wework_hint"}}':'{{index .T "webhook_kind_generic_hint"}}';
          if(wrap) wrap.style.display=wework?'none':'';
        }

        async function webhookSubmit(action,id){
          var fd=new URLSearchParams();
          fd.set('webhook_action',action);
          if(id) fd.set('id',id);
          var n=document.getElementById('wh-name'), u=document.getElementById('wh-url'), s=document.getElementById('wh-secret');
          var k=document.getElementById('wh-kind');
          if(n) fd.set('name',n.value.trim());
          if(u) fd.set('url',u.value.trim());
          if(s&&s.value.trim()) fd.set('secret',s.value.trim());
          if(k) fd.set('kind',k.value);
          // The list behind the modal also renders checkboxes with this name, so
          // scope the query to the modal to avoid collecting both sets.
          var modal=document.getElementById('g-modal-body')||document;
          modal.querySelectorAll('input[name="events"]:checked').forEach(function(c){fd.append('events',c.value);});
          fd.set('enabled','1');
          try{
            var r=await fetch('/api/webhooks',{method:'POST',body:fd});
            var j=await r.json();
            if(!j.ok){webhookMsg(j.msg||'failed',true);return;}
            closeModal();
            webhookMsg(j.msg||'ok',false);
            applyWebhooks(j.webhooks||[]);
          }catch(e){webhookMsg(e.message,true);}
        }

        async function webhookToggle(id,on){
          var fd=new URLSearchParams();
          fd.set('webhook_action','toggle');
          fd.set('id',id);
          fd.set('enabled',on?'1':'0');
          try{
            var r=await fetch('/api/webhooks',{method:'POST',body:fd});
            var j=await r.json();
            if(!j.ok){webhookMsg(j.msg||'failed',true);return;}
            applyWebhooks(j.webhooks||[]);
          }catch(e){webhookMsg(e.message,true);}
        }

        async function webhookDelete(id){
          if(!(await confirmAsync('{{index .T "webhook_confirm_delete"}}')))return;
          var fd=new URLSearchParams();
          fd.set('webhook_action','delete');
          fd.set('id',id);
          try{
            var r=await fetch('/api/webhooks',{method:'POST',body:fd});
            var j=await r.json();
            if(!j.ok){webhookMsg(j.msg||'failed',true);return;}
            webhookMsg(j.msg||'ok',false);
            applyWebhooks(j.webhooks||[]);
          }catch(e){webhookMsg(e.message,true);}
        }

        async function webhookTest(id){
          webhookMsg('{{index .T "testing"}}',false);
          var fd=new URLSearchParams();
          fd.set('webhook_action','test');
          fd.set('id',id);
          // Send the stored kind so the server can test with the right payload
          // format even if it cannot look the entry up.
          var w=(window.__webhooks||[]).filter(function(x){return x.id===id;})[0];
          if(w&&w.kind) fd.set('kind',w.kind);
          if(w&&w.url) fd.set('url',w.url);
          try{
            var r=await fetch('/api/webhooks',{method:'POST',body:fd});
            var j=await r.json();
            webhookMsg(j.msg||'failed',!j.ok);
          }catch(e){webhookMsg(e.message,true);}
        }

        // ---- Jackett mode --------------------------------------------------
        // The radio only decides which controls are shown; the value is stored
        // when the form is submitted, like every other setting.
        function jkModeChanged(){
          var managed=document.querySelector('input[name="jackett_mode"][value="managed"]');
          var on=managed&&managed.checked;
          var mp=document.getElementById('jk-managed-panel');
          var ep=document.getElementById('jk-external-panel');
          if(mp) mp.style.display=on?'':'none';
          if(ep) ep.style.display=on?'none':'';
          if(on){ jkCheckEnv(); jkCheckUpdate(); }
        }

        // ---- environment ---------------------------------------------------
        function jkEnvLine(label,value,ok){
          var color = ok===undefined ? 'var(--muted)' : (ok?'var(--accent-2)':'var(--danger)');
          return '<div style="display:flex;gap:8px;font-size:12px;padding:2px 0;">'
            + '<span style="color:var(--muted);min-width:96px;">'+label+'</span>'
            + '<span style="color:'+color+';word-break:break-all;">'+value+'</span></div>';
        }

        function renderJKEnv(env,usable){
          var el=document.getElementById('jk-env-body');
          if(!el) return;
          var h='';
          h+=jkEnvLine('{{index .T "jk_env_os"}}', (env.distro_name||env.os||'?')+' / '+env.arch);
          h+=jkEnvLine('{{index .T "jk_env_pm"}}', env.package_manager||'{{index .T "jk_env_pm_none"}}', !!env.package_manager);
          h+=jkEnvLine('ICU', env.has_icu?'{{index .T "jk_env_ok"}}':'{{index .T "jk_env_missing"}}', env.has_icu);
          if(env.glibc_version) h+=jkEnvLine('glibc', env.glibc_version);
          if(env.musl) h+=jkEnvLine('libc', 'musl');
          if(env.install_dir){
            h+=jkEnvLine('{{index .T "jk_env_dir"}}', env.install_dir, env.install_dir_writable);
          }
          if(env.disk_free_bytes){
            var freeMB=Math.round(env.disk_free_bytes/1048576);
            var needMB=Math.round(env.disk_required_bytes/1048576);
            h+=jkEnvLine('{{index .T "jk_env_disk"}}', freeMB+' MB / '+needMB+' MB', env.disk_free_bytes>=env.disk_required_bytes);
          }
          h+=jkEnvLine('{{index .T "jk_env_priv"}}', env.is_root?'root':'{{index .T "jk_env_nonroot"}}', env.is_root);

          if(env.problems&&env.problems.length){
            h+='<div style="margin-top:8px;padding:8px 10px;border-radius:8px;background:#fef2f2;border:1px solid #fecaca;font-size:12px;color:var(--danger);">';
            env.problems.forEach(function(p){ h+='<div>'+ICO('close')+' '+p+'</div>'; });
            h+='</div>';
          }
          if(env.warnings&&env.warnings.length){
            h+='<div style="margin-top:8px;font-size:12px;color:var(--warn);">';
            env.warnings.forEach(function(w){ h+='<div>'+ICO('warning')+w+'</div>'; });
            h+='</div>';
          }
          if(!usable&&env.can_install){
            h+='<button type="button" id="jk-deps-btn" onclick="jkInstallDeps()" style="margin:8px 0 0;padding:5px 12px;font-size:12px;background:var(--accent);">{{index .T "jk_env_install_deps"}}</button>';
          }else if(!usable){
            h+='<div class="hint" style="margin-top:8px;">{{index .T "jk_env_manual_hint"}}</div>';
          }
          h+='<div id="jk-env-result" style="margin-top:8px;font-size:12px;"></div>';
          el.innerHTML=h;

          // Block install when the host cannot run Jackett, so a 50 MB download
          // is not attempted for a binary that would exit at once.
          var b=document.getElementById('jk-install-btn');
          if(b){
            b.disabled=!usable;
            b.title=usable?'':'{{index .T "jk_env_blocked"}}';
            b.style.opacity=usable?'1':'0.5';
          }
        }

        async function jkCheckEnv(){
          var el=document.getElementById('jk-env-body');
          if(el) el.innerHTML='<span class="hint">{{icoSpin}}</span>';
          try{
            var r=await fetch('/indexers/jackett/env');
            var j=await r.json();
            if(!j.ok){ if(el) el.innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+(j.msg||'failed')+'</span>'; return; }
            renderJKEnv(j.env,j.usable);
          }catch(e){
            if(el) el.innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }

        async function jkInstallDeps(){
          var btn=document.getElementById('jk-deps-btn');
          var out=document.getElementById('jk-env-result');
          if(btn){btn.disabled=true;btn.textContent='{{index .T "jk_env_installing"}}';}
          if(out) out.innerHTML='<span class="hint">{{index .T "jk_env_installing"}}</span>';
          try{
            var r=await fetch('/indexers/jackett/install-deps',{method:'POST'});
            var j=await r.json();
            if(out){
              var log=(j.log&&j.log.length)?('<pre style="margin:6px 0 0;padding:8px;background:var(--bg);border-radius:6px;font-size:11px;max-height:160px;overflow:auto;white-space:pre-wrap;">'+j.log.join('\n')+'</pre>'):'';
              out.innerHTML='<span style="color:'+(j.ok?'var(--accent-2)':'var(--danger)')+';">'+ICO(j.ok?'check':'close')+' '+(j.msg||'')+'</span>'+log;
            }
            if(j.env) renderJKEnv(j.env,j.usable);
          }catch(e){
            if(out) out.innerHTML='<span style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }

        // ---- install / uninstall -------------------------------------------
        async function jkInstall(){
          if(!(await confirmAsync('{{index .T "jk_install_confirm"}}'))) return;
          var btn=document.getElementById('jk-install-btn');
          var out=document.getElementById('jk-env-result');
          if(btn){btn.disabled=true;btn.textContent='{{index .T "jk_installing"}}';}
          if(out) out.innerHTML='<span class="hint">{{index .T "jk_installing_hint"}}</span>';
          try{
            var r=await fetch('/indexers/jackett/install',{method:'POST',body:new URLSearchParams({start:'1'})});
            var j=await r.json();
            if(out){
              var log=(j.log&&j.log.length)?('<pre style="margin:6px 0 0;padding:8px;background:var(--bg);border-radius:6px;font-size:11px;max-height:200px;overflow:auto;white-space:pre-wrap;">'+j.log.join('\n')+'</pre>'):'';
              out.innerHTML='<span style="color:'+(j.ok?'var(--accent-2)':'var(--danger)')+';">'+ICO(j.ok?'check':'close')+' '+(j.msg||'')+'</span>'+log;
            }
            if(j.ok) setTimeout(function(){location.reload();},1500);
            else if(btn){btn.disabled=false;btn.textContent='{{index .T "jk_install_btn"}}';}
          }catch(e){
            if(out) out.innerHTML='<span style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
            if(btn){btn.disabled=false;btn.textContent='{{index .T "jk_install_btn"}}';}
          }
        }

        async function jkUninstall(){
          if(!(await confirmAsync('{{index .T "jk_uninstall_confirm"}}'))) return;
          var btn=document.getElementById('jk-uninstall-btn');
          var out=document.getElementById('jk-env-result');
          if(btn){btn.disabled=true;btn.textContent='{{index .T "jk_uninstalling"}}';}
          if(out) out.innerHTML='<span class="hint">{{index .T "jk_uninstalling"}}</span>';
          try{
            var r=await fetch('/indexers/jackett/uninstall',{method:'POST'});
            var j=await r.json();
            if(out){
              var log=(j.log&&j.log.length)?('<pre style="margin:6px 0 0;padding:8px;background:var(--bg);border-radius:6px;font-size:11px;max-height:200px;overflow:auto;white-space:pre-wrap;">'+j.log.join('\n')+'</pre>'):'';
              out.innerHTML='<span style="color:'+(j.ok?'var(--accent-2)':'var(--danger)')+';">'+ICO(j.ok?'check':'close')+' '+(j.msg||'')+'</span>'+log;
            }
            if(j.ok) setTimeout(function(){location.reload();},1500);
            else if(btn){btn.disabled=false;btn.textContent='{{index .T "jk_uninstall_btn"}}';}
          }catch(e){
            if(out) out.innerHTML='<span style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
            if(btn){btn.disabled=false;btn.textContent='{{index .T "jk_uninstall_btn"}}';}
          }
        }

        // ---- version / update ----------------------------------------------
        async function jkCheckUpdate(){
          var el=document.getElementById('jk-update-body');
          if(el) el.innerHTML='<span class="hint">{{icoSpin}}</span>';
          try{
            var r=await fetch('/indexers/jackett/check-update');
            var j=await r.json();
            var st=j.st||{};
            if(!el) return;
            if(st.error){
              el.innerHTML='<span class="hint" style="color:var(--warn);">'+ICO('warning')+st.error+'</span>';
              return;
            }
            var h=jkEnvLine('{{index .T "jk_installed_ver"}}', st.installed||'{{index .T "jk_ver_unknown"}}', !!st.installed);
            h+=jkEnvLine('{{index .T "jk_latest_ver"}}', st.latest||'?', true);
            if(st.has_update){
              h+='<div style="margin-top:8px;"><button type="button" id="jk-update-btn" onclick="jkDoUpdate()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--accent);">{{index .T "jk_update_btn"}}</button></div>';
            }else{
              h+='<div class="hint" style="margin-top:6px;">{{index .T "jk_up_to_date"}}</div>';
            }
            h+='<div id="jk-update-result" style="margin-top:8px;font-size:12px;"></div>';
            el.innerHTML=h;
          }catch(e){
            if(el) el.innerHTML='<span class="hint" style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }

        async function jkDoUpdate(){
          if(!(await confirmAsync('{{index .T "jk_update_confirm"}}'))) return;
          var btn=document.getElementById('jk-update-btn');
          var out=document.getElementById('jk-update-result');
          if(btn){btn.disabled=true;btn.textContent='{{index .T "jk_updating"}}';}
          if(out) out.innerHTML='<span class="hint">{{index .T "jk_updating"}}</span>';
          try{
            var r=await fetch('/indexers/jackett/update',{method:'POST'});
            var j=await r.json();
            if(out){
              var log=(j.log&&j.log.length)?('<pre style="margin:6px 0 0;padding:8px;background:var(--bg);border-radius:6px;font-size:11px;max-height:200px;overflow:auto;white-space:pre-wrap;">'+j.log.join('\n')+'</pre>'):'';
              out.innerHTML='<span style="color:'+(j.ok?'var(--accent-2)':'var(--danger)')+';">'+ICO(j.ok?'check':'close')+' '+(j.msg||'')+'</span>'+log;
            }
            // A stopped instance stays stopped: the user decides when to start it.
            if(j.ok&&j.was_running&&out){
              out.innerHTML+='<div style="margin-top:8px;"><button type="button" onclick="jkStart()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--accent-2);">{{index .T "jk_start_btn"}}</button></div>';
            }
          }catch(e){
            if(out) out.innerHTML='<span style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }

        async function jkStart(){
          var out=document.getElementById('jk-update-result');
          if(out) out.innerHTML='<span class="hint">{{index .T "loading"}}</span>';
          try{
            var r=await fetch('/indexers/jackett/start',{method:'POST'});
            var j=await r.json();
            if(out) out.innerHTML='<span style="color:'+(j.ok?'var(--accent-2)':'var(--danger)')+';">'+ICO(j.ok?'check':'close')+' '+(j.msg||'')+'</span>';
          }catch(e){
            if(out) out.innerHTML='<span style="color:var(--danger);">'+ICO('close')+' '+e.message+'</span>';
          }
        }
        async function checkUpdate(){
          var btn=document.getElementById('check-update-btn');
          var sp=document.getElementById('update-status');
          btn.disabled=true;sp.textContent='{{index .T "update_checking"}}';sp.style.color='var(--muted)';
          try{
            var r=await fetch('/settings/check-update');
            var j=await r.json();
            if(j.has_update){
              sp.innerHTML='{{index .T "update_new_found"}}'.replace('%s','<b>'+j.latest+'</b>').replace('%s','<b>'+j.current+'</b>')+' <button type="button" id="do-update-btn" onclick="doUpdate()" style="margin:0 0 0 8px;padding:4px 10px;font-size:12px;background:var(--accent);">{{index .T "update_do_btn"}}</button>';
              sp.style.color='var(--accent-2)';
            }else if(j.latest){
              sp.innerHTML='{{ico "check"}}{{index .T "update_already_latest"}} ('+j.latest+')';sp.style.color='var(--accent-2)';
            }else{
              sp.innerHTML='{{ico "close"}}{{index .T "update_fetch_failed"}}';sp.style.color='var(--danger)';
            }
          }catch(e){sp.innerHTML='{{ico "close"}}{{index .T "net_error"}}';sp.style.color='var(--danger)';}
          btn.disabled=false;
        }
        async function doUpdate(){
          if(!(await confirmAsync('{{index .T "confirm_restart"}}')))return;
          var sp=document.getElementById('update-status');
          sp.textContent='{{index .T "update_checking"}}';sp.style.color='var(--muted)';
          try{
            var r=await fetch('/settings/update',{method:'POST'});
            var j=await r.json();
            if(j.ok){sp.innerHTML=ICO('check')+' '+j.msg;sp.style.color='var(--accent-2)';setTimeout(function(){location.reload();},3000);}
            else{sp.innerHTML=ICO('close')+' '+j.msg;sp.style.color='var(--danger)';var b=document.getElementById('check-update-btn');if(b)b.disabled=false;}
          }catch(e){sp.innerHTML='{{ico "close"}}{{index .T "net_error"}}';sp.style.color='var(--danger)';var b=document.getElementById('check-update-btn');if(b)b.disabled=false;}
        }
      </script>
      <!-- settings form -->
      <form action="/settings" method="post">
        <!-- Group: System -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "settings2"}}{{index .T "system_label"}}</legend>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:2;min-width:200px;">
              <label>{{index .T "http_proxy_label"}}</label>
              <input name="proxy_http" placeholder="http://127.0.0.1:7890" value="{{.ProxyHTTP}}">
            </div>
            <div style="flex:1;min-width:120px;">
              <label>{{index .T "web_pw"}}</label>
              <input name="web_password" type="password" placeholder="{{index .T "web_pw_ph"}}" value="{{.Settings.WebPassword}}" maxlength="128">
            </div>
            <div style="flex:1;min-width:130px;">
              <label>{{index .T "timezone_label"}}</label>
              <select name="timezone" style="width:100%;font-size:13px;padding:8px 6px;margin:0;">
                {{range $val, $name := .TimezoneOptions}}<option value="{{$val}}"{{if eq $.Timezone $val}} selected{{end}}>{{$name}}</option>{{end}}
              </select>
            </div>
          </div>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:center;margin-top:10px;padding-top:10px;border-top:1px solid var(--line);">
            <button type="button" id="check-update-btn" onclick="checkUpdate()" style="margin:0;padding:6px 14px;font-size:13px;background:var(--accent-2);">{{index .T "update_check_btn"}}</button>
            <span id="update-status" style="font-size:12px;color:var(--muted);"></span>
            <label style="display:flex;align-items:center;gap:6px;cursor:pointer;font-size:13px;margin:0 0 0 auto;">
              <input type="checkbox" name="auto_update" value="1" style="width:auto;margin:0;"{{if .AutoUpdate}} checked{{end}}>{{index .T "update_auto_label"}}
            </label>
          </div>
        </fieldset>

        <!-- Group: Download -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "download"}}{{index .T "download_settings_label"}}</legend>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:1;min-width:90px;">
              <label>{{index .T "chunk_size"}}</label>
              <input name="chunk_size" type="number" value="{{.Settings.ChunkSize}}">
            </div>
            <div style="flex:1;min-width:90px;">
              <label>{{index .T "chunk_delay"}}</label>
              <input name="chunk_delay" type="number" value="{{.Settings.ChunkDelay}}">
            </div>
            <div style="flex:1;min-width:100px;">
              <label>{{index .T "cooldown_min"}}</label>
              <input name="cooldown_min" type="number" value="{{.Settings.CooldownMinMs}}">
            </div>
            <div style="flex:1;min-width:100px;">
              <label>{{index .T "cooldown_max"}}</label>
              <input name="cooldown_max" type="number" value="{{.Settings.CooldownMaxMs}}">
            </div>
          </div>
        </fieldset>

        <!-- Group: Search -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "search"}}{{index .T "search_label"}}</legend>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:1;min-width:100px;">
              <label>{{index .T "page_size_label"}}</label>
              <input name="page_size" type="number" value="{{.PageSize}}" min="10" max="500" placeholder="50">
              <div class="hint" style="font-size:11px;">10–500，默认 50</div>
            </div>
          </div>
          <div style="margin-top:10px;padding-top:10px;border-top:1px solid var(--line);">
            <!-- Marks that this fieldset was part of the submission, so an
                 unchecked box below is read as "off" rather than "not sent". -->
            <input type="hidden" name="search_settings_submitted" value="1">
            <label style="display:flex;align-items:center;gap:6px;cursor:pointer;font-size:13px;margin:0;">
              <input type="checkbox" name="filter_search_title" value="1" style="width:auto;margin:0;"{{if .FilterSearchTitle}} checked{{end}}>{{index .T "filter_search_title_label"}}
            </label>
            <div class="hint" style="font-size:11px;margin-top:4px;">{{index .T "filter_search_title_hint"}}</div>
          </div>
        </fieldset>

        <!-- Group: Subscription & Notifications -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "bell"}}{{index .T "subs_notify_label"}}</legend>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:0.8;min-width:100px;">
              <label>{{index .T "subs_interval_label"}}</label>
              <input name="subs_interval" type="number" placeholder="{{index .T "subs_interval_ph"}}" value="{{.Settings.SubsInterval}}" min="0" style="font-size:13px;">
            </div>
          </div>
        </fieldset>

        <!-- Group: notifications.
             Every destination lives in one list, WeChat Work included: the old
             single-input + on/off switches were folded in by migration, so what
             gets sent is now decided per entry by event subscription. -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "bell"}}{{index .T "webhook_section"}}</legend>
          <div class="hint" style="margin-bottom:10px;">{{index .T "webhook_hint"}}</div>
          <div id="webhook-list"><span class="hint">{{icoSpin}}</span></div>
          <div style="margin-top:10px;">
            <button type="button" onclick="webhookAdd()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--accent-2);">{{index .T "webhook_add"}}</button>
            <button type="button" onclick="webhookAddWework()" style="margin:0 0 0 6px;padding:5px 12px;font-size:12px;background:var(--bg);color:var(--accent-2);border:1px solid var(--line);">{{index .T "webhook_add_wework"}}</button>
          </div>
          <div class="hint" style="margin-top:10px;font-size:11px;">{{index .T "webhook_signature_doc"}}</div>
          <div id="webhook-msg" style="font-size:12px;margin-top:8px;"></div>

          <!-- Log push stays a toggle rather than an event subscription: it is
               a throttled stream (one line per second), not a discrete event. -->
          <div style="margin-top:12px;padding-top:10px;border-top:1px solid var(--line);">
            <label style="display:flex;align-items:center;gap:6px;cursor:pointer;font-size:13px;margin:0;">
              <input type="checkbox" name="notify_log" value="1" style="width:auto;margin:0;"{{if .NotifyLog}} checked{{end}}>{{index .T "notify_log"}}
            </label>
            <div class="hint" style="font-size:11px;margin-top:4px;">{{index .T "notify_log_hint"}}</div>
          </div>
        </fieldset>

        <!-- Group: Jackett -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "indexers"}}Jackett</legend>

          <!-- Mode switch: managed install vs. an instance you run yourself -->
          <div style="margin-bottom:12px;">
            <label style="margin-bottom:6px;">{{index .T "jk_mode_label"}}</label>
            <div style="display:flex;gap:16px;flex-wrap:wrap;">
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;font-size:13px;margin:0;">
                <input type="radio" name="jackett_mode" value="managed" style="width:auto;margin:0;"
                  {{if eq .JackettMode "managed"}}checked{{end}} onchange="jkModeChanged()">
                {{index .T "jk_mode_managed"}}
              </label>
              <label style="display:flex;align-items:center;gap:6px;cursor:pointer;font-size:13px;margin:0;">
                <input type="radio" name="jackett_mode" value="external" style="width:auto;margin:0;"
                  {{if ne .JackettMode "managed"}}checked{{end}} onchange="jkModeChanged()">
                {{index .T "jk_mode_external"}}
              </label>
            </div>
            <div class="hint" style="margin-top:4px;">
              {{if eq .JackettMode "managed"}}{{index .T "jk_mode_managed_hint"}}{{else}}{{index .T "jk_mode_external_hint"}}{{end}}
            </div>
          </div>

          <!-- Connection details: used by both modes, auto-filled for managed -->
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:2;min-width:200px;">
              <label>Jackett URL</label>
              <input name="jackett_url" placeholder="http://localhost:9117" value="{{.JackettURL}}">
            </div>
            <div style="flex:1.5;min-width:160px;">
              <label>Jackett API Key</label>
              <div style="display:flex;align-items:center;gap:0;">
                <input name="jackett_apikey" placeholder="API Key" value="{{.JackettAPIKey}}" style="flex:1;">
                <button type="button" id="jk-test-btn" style="margin:0 0 0 6px;padding:2px 10px;font-size:12px;background:var(--accent-2);" onclick="testJackett()">{{index .T "jk_test_btn"}}</button>
              </div>
              <span id="jk-test-result" style="font-size:12px;"></span>
            </div>
            <div style="flex:1;min-width:140px;">
              <label>Admin 密码 <small style="color:var(--muted);">(WebUI 登录密码)</small></label>
              <input name="jackett_admin_password" type="password" placeholder="与 API Key 相同则留空" value="{{.JackettAdminPassword}}">
            </div>
          </div>

          <!-- Managed-install controls -->
          <div id="jk-managed-panel" style="margin-top:14px;padding-top:12px;border-top:1px solid var(--line);{{if ne .JackettMode "managed"}}display:none;{{end}}">
            <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:center;margin-bottom:10px;">
              <button type="button" onclick="jkCheckEnv()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--bg);color:var(--accent-2);border:1px solid var(--line);">{{index .T "jk_env_refresh"}}</button>
              <button type="button" onclick="jkCheckUpdate()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--bg);color:var(--accent-2);border:1px solid var(--line);">{{index .T "jk_update_check_btn"}}</button>
              <button type="button" id="jk-install-btn" onclick="jkInstall()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--accent);">{{index .T "jk_install_btn"}}</button>
              <button type="button" id="jk-uninstall-btn" onclick="jkUninstall()" style="margin:0;padding:5px 12px;font-size:12px;background:var(--danger);">{{index .T "jk_uninstall_btn"}}</button>
            </div>

            <h3 style="font-size:13px;margin:12px 0 6px;">{{index .T "jk_env_title"}}</h3>
            <div class="hint" style="margin-bottom:6px;">{{index .T "jk_env_hint"}}</div>
            <div id="jk-env-body"><span class="hint">{{icoSpin}}</span></div>

            <h3 style="font-size:13px;margin:16px 0 6px;">{{index .T "jk_update_title"}}</h3>
            <div id="jk-update-body"><span class="hint">{{icoSpin}}</span></div>
          </div>

          <!-- External mode note -->
          <div id="jk-external-panel" class="hint" style="margin-top:12px;padding-top:12px;border-top:1px solid var(--line);{{if eq .JackettMode "managed"}}display:none;{{end}}">
            {{index .T "jk_external_note"}}
          </div>
        </fieldset>

        <!-- Group: TMDB -->
        <fieldset style="border:1px solid var(--line);border-radius:10px;padding:14px 16px;margin-bottom:14px;">
          <legend style="font-weight:600;font-size:14px;padding:0 8px;">{{ico "film"}}TMDB API</legend>
          <div style="display:flex;gap:10px;flex-wrap:wrap;align-items:flex-end;">
            <div style="flex:2;min-width:260px;">
              <label>TMDB API Key <small style="color:var(--muted);">(<a href="https://www.themoviedb.org/settings/api" target="_blank">获取 Key</a>)</small></label>
              <input name="tmdb_apikey" placeholder="输入 TMDB API Key 以启用海报搜索" value="{{.TMDBAPIKey}}">
            </div>
          </div>
        </fieldset>

        <div style="display:flex;justify-content:space-between;align-items:center;margin-top:14px;padding-top:12px;border-top:1px solid var(--line);">
          <button type="submit" style="margin-top:0;">{{index .T "save"}}</button>
          <button type="button" onclick="restartServer()" style="margin-top:0;background:var(--danger);">{{index .T "restart_service_btn"}}</button>
        </div>
        <div class="hint">{{index .T "db_path"}}: {{.Settings.DatabasePath}}</div>
      </form>
    </div>
    <script>
      // Populate the managed-Jackett panels on load. Both are cheap (a local
      // check and one GitHub request) and only rendered in managed mode.
      (function(){
        loadWebhooks();
        var managed=document.querySelector('input[name="jackett_mode"][value="managed"]');
        if(managed&&managed.checked){ jkCheckEnv(); jkCheckUpdate(); }
      })();
    </script>
    {{end}}

  </div><!-- /.main -->

  <!-- RSS subscription modal -->
  <div id="sub-form" style="display:none;position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,0.4);z-index:998;align-items:center;justify-content:center;" onclick="if(event.target===this)document.getElementById('sub-form').style.display='none'">
    <div style="background:#fff;border-radius:16px;padding:24px;width:92%;max-width:600px;max-height:85vh;overflow-y:auto;box-shadow:0 12px 40px rgba(0,0,0,.2);" onclick="event.stopPropagation()">
      <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:16px;">
        <h3 style="margin:0;">{{index .T "add_rss_sub_title"}}</h3>
        <button onclick="document.getElementById('sub-form').style.display='none'" style="background:none;border:none;font-size:20px;cursor:pointer;padding:0;color:var(--muted);">×</button>
      </div>
      <form action="/search/subscribe" method="post">
        <input type="hidden" name="query" id="sub-query">
        <div style="display:flex;gap:8px;flex-wrap:wrap;align-items:flex-end;">
          <div style="flex:2;min-width:120px;"><label style="font-size:12px;">{{index .T "sub_name_label"}}</label><input name="name" id="sub-name" style="font-size:13px;"></div>
          <div style="flex:2;min-width:160px;"><label style="font-size:12px;">{{index .T "rss_addr_label"}}</label><input name="url" id="sub-url" style="font-size:13px;"></div>
          <div style="flex:1;min-width:200px;"><label style="font-size:12px;">{{index .T "dir_id_opt"}} / {{index .T "subdir_opt"}}</label><div style="display:flex;gap:4px;"><input name="cid" id="sub-cid" placeholder="cid" style="font-size:13px;flex:1;"><input name="savepath" placeholder="{{index .T "subdir_opt"}}" style="font-size:13px;flex:1;"><button type="button" onclick="browseDirsFor('sub-cid')" title="{{index .T "browse_btn"}}" style="font-size:13px;padding:4px 8px;margin:0;background:var(--bg);border:1px solid var(--line);border-radius:6px;cursor:pointer;">{{index .T "browse_btn"}}</button></div></div>
        </div>
        <div style="margin-top:10px;text-align:right;">
          <button type="submit" style="margin-top:0;background:var(--accent-2);">{{index .T "add_sub_btn"}}</button>
        </div>
      </form>
    </div>
  </div>

  <!-- global modal -->
  <div id="g-modal" style="display:none;position:fixed;top:0;left:0;right:0;bottom:0;background:rgba(0,0,0,0.4);z-index:999;align-items:center;justify-content:center;" onclick="if(event.target===this)closeModal()">
    <div style="background:#fff;border-radius:12px;padding:20px;min-width:300px;max-width:500px;max-height:80vh;overflow-y:auto;box-shadow:0 4px 24px rgba(0,0,0,0.15);" onclick="event.stopPropagation()">
      <div id="g-modal-title" style="font-weight:600;margin-bottom:12px;"></div>
      <div id="g-modal-body"></div>
      <div id="g-modal-btns" style="margin-top:14px;display:flex;gap:8px;justify-content:flex-end;"></div>
    </div>
  </div>
  <script>
    var modalCb=null;
    function showModal(title,body,buttons){
      document.getElementById('g-modal-title').textContent=title;
      document.getElementById('g-modal-body').innerHTML=body;
      var btns=document.getElementById('g-modal-btns');
      btns.innerHTML='';
      (buttons||[{text:'{{index .T "confirm_btn"}}',cls:'',cb:function(){closeModal()}}]).forEach(function(b){
        var btn=document.createElement('button');
        btn.textContent=b.text;btn.style.margin='0';btn.style.padding='6px 16px';
        if(b.cls)btn.style.background=b.cls;
        if(b.id)btn.id=b.id;
        btn.onclick=function(){if(b.cb)b.cb();};
        btns.appendChild(btn);
      });
      document.getElementById('g-modal').style.display='flex';
    }
    function closeModal(){document.getElementById('g-modal').style.display='none';modalCb=null;}
    function alertModal(msg){showModal('',msg,[{text:'OK',cls:'var(--accent)',cb:function(){closeModal()}}]);}
    async function confirmAsync(msg){return new Promise(function(resolve){showModal('',msg,[{text:'Cancel',cls:'var(--danger)',cb:function(){closeModal();resolve(false);}},{text:'OK',cls:'var(--accent)',cb:function(){closeModal();resolve(true);}}]);});}
    async function promptModal(title,label,defaultValue){return new Promise(function(resolve){var dv=(defaultValue||'').replace(/&/g,'&amp;').replace(/"/g,'&quot;').replace(/</g,'&lt;').replace(/>/g,'&gt;');var body='<div style="margin-bottom:6px;font-size:13px;color:var(--muted);">'+label+'</div><input id="g-modal-input" style="width:100%;padding:8px;border:1px solid var(--line);border-radius:6px;font-size:14px;box-sizing:border-box;" value="'+dv+'" onkeydown="if(event.key===&quot;Enter&quot;)document.getElementById(&quot;g-modal-btn-ok&quot;).click()" autofocus>';showModal(title,body,[{text:'Cancel',cls:'var(--danger)',cb:function(){closeModal();resolve(null);}},{text:'OK',cls:'var(--accent)',id:'g-modal-btn-ok',cb:function(){var v=document.getElementById('g-modal-input').value.trim();closeModal();resolve(v);}}]);});}
    function submitConfirm(form,msg){if(event)event.preventDefault();confirmAsync(msg).then(function(ok){if(ok)form.submit();});}
    async function fsApi(endpoint,body){var r=await fetch(endpoint,{method:'POST',headers:{'Content-Type':'application/x-www-form-urlencoded'},body:new URLSearchParams(body)});var t=await r.text();try{return JSON.parse(t);}catch(e){return{status:'error',message:t}}}
    async function fsRename(id,name){var nn=await promptModal('{{index .T "rename"}}','{{index .T "new_name"}}:',name);if(!nn||nn===name)return;var r=await fsApi('/api/fs/rename',{id:id,name:nn});if(r.status==='ok')location.reload();else alertModal(r.message||'Error');}
    async function fsDelete(id,name){var ok=await confirmAsync('{{index .T "confirm_delete"}} '+name+' ?');if(!ok)return;var r=await fsApi('/api/fs/delete',{id:id});if(r.status==='ok')location.reload();else alertModal(r.message||'Error');}
    async function fsNewFolder(parentId){var name=await promptModal('{{index .T "new_folder"}}','{{index .T "folder_name"}}:','');if(!name)return;var r=await fsApi('/api/fs/mkdir',{parent_id:parentId,name:name});if(r.status==='ok')location.reload();else alertModal(r.message||'Error');}
    async function fsMove(id,name){var target=await promptModal('{{index .T "move"}}','{{index .T "target_dir_id"}} '+name+':','');if(!target)return;var r=await fsApi('/api/fs/move',{id:id,target_dir:target});if(r.status==='ok')location.reload();else alertModal(r.message||'Error');}
    async function fsCopy(id,name){var target=await promptModal('{{index .T "copy"}}','{{index .T "target_dir_id"}} '+name+':','');if(!target)return;var r=await fsApi('/api/fs/copy',{id:id,target_dir:target});if(r.status==='ok')location.reload();else alertModal(r.message||'Error');}
  </script>
</body>
</html>`))
