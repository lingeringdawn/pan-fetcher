#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Check that what the README promises exists in the code.

A README is the first thing a user reads, and a stale one costs more trust than a
short one: a documented flag that does not exist, or a page route that 404s, is
reported as a bug against the project. This compares the claims that are easy to
verify mechanically — CLI subcommands, HTTP routes and the scripts it tells
people to run.
"""
import glob
import io
import re
import sys

README = "README.md"


def read(path):
    return io.open(path, encoding="utf-8").read()


def main():
    readme = read(README)
    problems = []

    # --- CLI subcommands the README documents ---
    #
    # The commands are spread across the repository root (main.go, fs_*.go) and
    # the server package, so every Go source is scanned rather than a guessed
    # file list — guessing is what made an earlier version of this check report
    # every documented verb as missing.
    sources = []
    for path in glob.glob("*.go") + glob.glob("server/*.go") + glob.glob("cmd/*.go"):
        if path.endswith("_test.go"):
            continue
        sources.append(read(path))
    all_src = "\n".join(sources)

    for m in re.finditer(r"pan-fetcher fs ([a-z-]+)", readme):
        verb = m.group(1)
        # Use strings carry their arguments ("ls [path]"), so match the verb as
        # the first word of a Use field rather than the whole string.
        if not re.search(r'Use:\s+"' + re.escape(verb) + r'(?=[\s"])', all_src):
            problems.append("README documents `fs %s`, which does not appear in the sources" % verb)

    # --- Routes the README lists ---
    routes = re.findall(r"\|\s*`(/[a-z-]*)`\s*\|", readme)
    server_go = read("server/server.go") if _exists("server/server.go") else ""
    for route in sorted(set(routes)):
        if route == "/":
            continue
        # handleXxx is registered as mux.HandleFunc("/route", ...)
        if ('"%s"' % route) not in server_go:
            problems.append("README lists route %s, which is not registered in server.go" % route)

    # --- Scripts the README tells people to curl ---
    for m in re.finditer(r"raw\.githubusercontent\.com/lingeringdawn/pan-fetcher/master/(scripts/[a-z-]+\.sh)", readme):
        path = m.group(1)
        if not _exists(path):
            problems.append("README tells users to run %s, which does not exist" % path)

    # --- Images: the README should not reference local screenshots ---
    for m in re.finditer(r'src="(\.github/[^"]+)"', readme):
        problems.append("README still references a local image: %s" % m.group(1))

    if problems:
        for p in problems:
            print("  " + p)
        print()
        print("problems:", len(problems))
        return 1

    print("README claims check out: CLI verbs, routes and scripts all exist")
    return 0


def _exists(path):
    import os
    return os.path.exists(path)


if __name__ == "__main__":
    sys.exit(main())
