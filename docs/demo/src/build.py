#!/usr/bin/env python3
"""Assemble the self-contained demo-day deck.

Reads deck.html, inlines fonts.css at {{FONTS}} and every img/<name>.png at
{{IMG:<name>}} as data URIs, and writes:

  ../archai-demo-day.html          full standalone document (open in a browser)
  ../archai-demo-day.artifact.html same content without the <html>/<head>/<body>
                                   skeleton, for hosts that wrap the page themselves

Run from anywhere: python3 docs/demo/src/build.py
"""
from __future__ import annotations

import base64
import re
from pathlib import Path

SRC = Path(__file__).resolve().parent
OUT = SRC.parent

tpl = (SRC / "deck.html").read_text()
fonts = (SRC / "fonts.css").read_text()


def img_uri(name: str) -> str:
    data = (SRC / "img" / f"{name}.png").read_bytes()
    return "data:image/png;base64," + base64.b64encode(data).decode()


html = tpl.replace("{{FONTS}}", fonts)
html = re.sub(r"\{\{IMG:([a-z0-9_-]+)\}\}", lambda m: img_uri(m.group(1)), html)
(OUT / "archai-demo-day.html").write_text(html)

# Artifact flavour: title first, then style + body content, no document skeleton.
title = re.search(r"<title>.*?</title>", html, re.S).group(0)
style = re.search(r"<style>.*?</style>", html, re.S).group(0)
body = re.search(r"<body>(.*)</body>", html, re.S).group(1)
(OUT / "archai-demo-day.artifact.html").write_text(f"{title}\n{style}\n{body}\n")

for p in (OUT / "archai-demo-day.html", OUT / "archai-demo-day.artifact.html"):
    print(f"{p.name}: {p.stat().st_size / 1e6:.2f} MB")
