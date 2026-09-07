#!/usr/bin/env python3
"""Build the standalone METAR prototype using only the Python standard library."""
from pathlib import Path
ROOT = Path(__file__).resolve().parent
html = (ROOT/'src/shell.html').read_text(encoding='utf-8')
css = (ROOT/'src/styles.css').read_text(encoding='utf-8')
js = (ROOT/'src/app.js').read_text(encoding='utf-8')
if '</script' in js.lower():
    raise ValueError('Unexpected closing script tag in JavaScript source')
html = html.replace('/* __STYLES__ */', css).replace('/* __APP__ */', js)
(ROOT/'index.html').write_text(html, encoding='utf-8')
(ROOT/'preview.html').write_text(html.replace('<body>', '<body data-preview="true">'), encoding='utf-8')
print(f'Built {ROOT / "index.html"} ({len(html.encode("utf-8")):,} bytes)')
