#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""Validate the release workflow and summarise its jobs."""
import io
import sys

try:
    import yaml
except ImportError:
    print("pyyaml not available; skipping")
    sys.exit(0)

PATH = ".github/workflows/release.yml"

doc = yaml.safe_load(io.open(PATH, encoding="utf-8"))
print("YAML parsed OK")

triggers = doc.get(True) or doc.get("on")
print("triggers:", ", ".join(triggers.keys()))

for name, job in doc["jobs"].items():
    cond = job.get("if", "(always)")
    needs = job.get("needs", "-")
    print("  %-16s needs=%-14s if=%s" % (name, needs, cond))

print()
print("docker job pushes :latest:", ":latest" in yaml.safe_dump(doc["jobs"]["docker"]))
print("promote job moves :latest:", ":latest" in yaml.safe_dump(doc["jobs"]["promote"]))
print("create-release forces --prerelease:", "--prerelease\n" in doc["jobs"]["create-release"]["steps"][-1]["run"])
