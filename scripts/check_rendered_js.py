#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Validate the JavaScript in a rendered page.

No Node.js is available, so this walks each <script> block tracking string,
template-literal, regex and comment state, then checks that braces, brackets and
parentheses balance. That catches the damage a bad bulk edit creates: a lost
escape leaves a string unterminated and everything after it is misparsed.

Regex literals have to be recognised explicitly, otherwise their brackets and
slashes look like code and produce false positives.
"""
import io
import re
import sys


def regex_allowed(prev_sig):
    """A regex may start after these tokens; after an identifier it cannot."""
    if not prev_sig:
        return True
    return prev_sig in "(,=:[!&|?{};+-*%~^<>"


def scan(text, start_line=1):
    problems = []
    stack = []
    state = "code"
    i = 0
    line = start_line
    n = len(text)
    prev_sig = ""

    while i < n:
        c = text[i]
        nxt = text[i + 1] if i + 1 < n else ""

        if c == "\n":
            line += 1
            if state == "line_comment":
                state = "code"
            i += 1
            continue

        if state == "line_comment":
            i += 1
            continue

        if state == "block_comment":
            if c == "*" and nxt == "/":
                state = "code"
                i += 2
                continue
            i += 1
            continue

        if state == "regex":
            if c == "\\":
                i += 2
                continue
            if c == "[":
                # Character class: brackets inside are literal.
                j = i + 1
                while j < n:
                    if text[j] == "\\":
                        j += 2
                        continue
                    if text[j] == "]":
                        break
                    j += 1
                i = j + 1 if j < n else n
                continue
            if c == "/":
                state = "code"
                prev_sig = "/"
            i += 1
            continue

        if state == "code":
            if c == "/" and nxt == "/":
                state = "line_comment"
                i += 2
                continue
            if c == "/" and nxt == "*":
                state = "block_comment"
                i += 2
                continue
            if c == "/" and regex_allowed(prev_sig):
                state = "regex"
                i += 1
                continue
            if c == "'":
                state = "sq"
                i += 1
                continue
            if c == '"':
                state = "dq"
                i += 1
                continue
            if c == "`":
                state = "tpl"
                i += 1
                continue
            if c in "([{":
                stack.append((c, line))
                prev_sig = c
                i += 1
                continue
            if c in ")]}":
                want = {")": "(", "]": "[", "}": "{"}[c]
                if not stack:
                    problems.append((line, "unmatched closing %r" % c))
                elif stack[-1][0] != want:
                    problems.append(
                        (line, "closing %r does not match %r opened on line %d" % (c, stack[-1][0], stack[-1][1]))
                    )
                    stack.pop()
                else:
                    stack.pop()
                prev_sig = c
                i += 1
                continue
            if not c.isspace():
                prev_sig = c
            i += 1
            continue

        # inside a string
        if c == "\\":
            i += 2
            continue
        if (state == "sq" and c == "'") or (state == "dq" and c == '"') or (state == "tpl" and c == "`"):
            state = "code"
            prev_sig = c
        i += 1

    if state in ("sq", "dq", "tpl"):
        problems.append((line, "unterminated string"))
    for ch, ln in stack:
        problems.append((ln, "unclosed %r" % ch))
    return problems


def main():
    if len(sys.argv) < 2:
        print("usage: check_rendered_js.py <page.html>")
        return 2

    html = io.open(sys.argv[1], encoding="utf-8").read()
    blocks = list(re.finditer(r"<script>(.*?)</script>", html, re.S))
    if not blocks:
        print("no <script> blocks found")
        return 2

    total = 0
    lines = html.split("\n")
    for idx, m in enumerate(blocks, 1):
        start_line = html[: m.start()].count("\n") + 1
        problems = scan(m.group(1), start_line)
        if problems:
            total += len(problems)
            print("block %d (starts line %d):" % (idx, start_line))
            for ln, msg in problems[:10]:
                print("  line %d: %s" % (ln, msg))
                if 0 < ln <= len(lines):
                    print("    %s" % lines[ln - 1].strip()[:150])

    if total:
        print("problems: %d" % total)
        return 1
    print("ok: %d script block(s), strings and brackets balanced" % len(blocks))
    return 0


if __name__ == "__main__":
    sys.exit(main())
