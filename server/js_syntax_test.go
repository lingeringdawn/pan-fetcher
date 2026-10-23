package server

import (
	"regexp"
	"strings"
	"testing"
)

// TestNoBrokenQuoteConcatenation guards against a specific edit accident.
//
// A bulk replacement once lost the backslash escapes inside a JS string:
//
//	onclick="browseDirs(\''+e.id+'\')"   (correct)
//	onclick="browseDirs(''+e.id+'')"     (what it became)
//
// The second form closes and reopens an empty string, which is a syntax error.
// An error anywhere in a <script> block stops the whole block from parsing, so
// unrelated features in the same block silently died — in that case the
// pagination bar stopped rendering.
//
// An empty string literal between two concatenation operators is never needed,
// so seeing one means an escape was eaten.
func TestNoBrokenQuoteConcatenation(t *testing.T) {
	src := readSource(t, templateSourceFile)

	// (' '+ / +' ') style fragments: a quote pair with nothing between them,
	// directly adjacent to a concatenation.
	pattern := regexp.MustCompile(`\(''\+|\+''\)|\(` + "`" + "`" + `\+|\+` + "`" + "`" + `\)`)

	for ln, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "//") {
			continue
		}
		if pattern.MatchString(line) {
			t.Errorf("templates.go:%d has an empty string between concatenations, "+
				"which usually means a lost escape: %s", ln+1, trimmed)
		}
	}
}

// TestScriptBlocksBalance verifies every <script> block is syntactically
// balanced.
//
// Without a JavaScript parser available in this environment, this tracks string,
// template-literal, regex and comment state and checks that brackets balance.
// It catches unterminated strings and stray brackets, the two failure modes a
// template edit tends to introduce.
func TestScriptBlocksBalance(t *testing.T) {
	src := readSource(t, templateSourceFile)

	// The template's script blocks are static HTML; template actions inside them
	// are replaced by a neutral token so the scanner sees valid JS.
	blocks := regexp.MustCompile(`(?s)<script>(.*?)</script>`).FindAllStringSubmatch(src, -1)
	if len(blocks) == 0 {
		t.Fatal("no <script> blocks found")
	}

	for i, b := range blocks {
		body := regexp.MustCompile(`\{\{[^}]*\}\}`).ReplaceAllString(b[1], "0")
		if msg, ok := balanced(body); !ok {
			t.Errorf("script block %d is unbalanced: %s", i+1, msg)
		}
	}
}

// balanced reports whether brackets in s balance, honouring strings, template
// literals, regex literals and comments.
func balanced(s string) (string, bool) {
	type open struct {
		ch   byte
		line int
	}
	var stack []open
	const (
		code = iota
		sq
		dq
		tpl
		regex
		lineComment
		blockComment
	)
	state := code
	line := 1
	var prev byte

	for i := 0; i < len(s); i++ {
		c := s[i]
		var next byte
		if i+1 < len(s) {
			next = s[i+1]
		}
		if c == '\n' {
			line++
			if state == lineComment {
				state = code
			}
			continue
		}
		switch state {
		case lineComment:
			continue
		case blockComment:
			if c == '*' && next == '/' {
				state = code
				i++
			}
			continue
		case regex:
			if c == '\\' {
				i++
				continue
			}
			if c == '[' {
				for j := i + 1; j < len(s); j++ {
					if s[j] == '\\' {
						j++
						continue
					}
					if s[j] == ']' {
						i = j
						break
					}
				}
				continue
			}
			if c == '/' {
				state = code
				prev = '/'
			}
			continue
		case sq, dq, tpl:
			if c == '\\' {
				i++
				continue
			}
			if (state == sq && c == '\'') || (state == dq && c == '"') || (state == tpl && c == '`') {
				state = code
				prev = c
			}
			continue
		}

		// code
		switch {
		case c == '/' && next == '/':
			state = lineComment
			i++
		case c == '/' && next == '*':
			state = blockComment
			i++
		case c == '/' && allowsRegex(prev):
			state = regex
		case c == '\'':
			state = sq
		case c == '"':
			state = dq
		case c == '`':
			state = tpl
		case c == '(' || c == '[' || c == '{':
			stack = append(stack, open{c, line})
			prev = c
		case c == ')' || c == ']' || c == '}':
			want := map[byte]byte{')': '(', ']': '[', '}': '{'}[c]
			if len(stack) == 0 {
				return "unmatched " + string(c) + " on line " + itoa(line), false
			}
			top := stack[len(stack)-1]
			if top.ch != want {
				return "closing " + string(c) + " on line " + itoa(line) +
					" does not match " + string(top.ch) + " opened on line " + itoa(top.line), false
			}
			stack = stack[:len(stack)-1]
			prev = c
		default:
			if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
				prev = c
			}
		}
	}

	if state == sq || state == dq || state == tpl {
		return "unterminated string", false
	}
	if len(stack) > 0 {
		top := stack[len(stack)-1]
		return "unclosed " + string(top.ch) + " opened on line " + itoa(top.line), false
	}
	return "", true
}

// allowsRegex reports whether a regex literal may begin after prev.
func allowsRegex(prev byte) bool {
	if prev == 0 {
		return true
	}
	return strings.ContainsRune("(,=:[!&|?{};+-*%~^<>", rune(prev))
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
