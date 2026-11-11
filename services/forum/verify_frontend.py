#!/usr/bin/env python3
"""Validate Answer plugin assets before vendoring, or a deployed HTML entry."""
from __future__ import annotations

import argparse
from html.parser import HTMLParser
import json
from pathlib import Path
import re
import shlex
from urllib.parse import urljoin, urlsplit
from urllib.request import Request, urlopen

ROOT = Path(__file__).resolve().parents[2]
IMPORT = re.compile(r"(?:\bfrom\s*|\bimport\s*(?:\(\s*)?|\brequire\s*\(\s*)['\"](\.[^'\"]+)['\"]")
CSS_IMPORT = re.compile(r"@import\s+(?:url\(\s*)?['\"](\.[^'\"]+)['\"]")


def embedded_files(directory: Path) -> set[str]:
    result = set()
    for source in directory.glob('*.go'):
        if source.name.endswith('_test.go'):
            continue
        for declaration in re.findall(r'^//go:embed (.+)$', source.read_text(encoding='utf-8'), re.M):
            for pattern in shlex.split(declaration):
                for entry in directory.glob(pattern.removeprefix('all:')):
                    entries = entry.rglob('*') if entry.is_dir() else [entry]
                    result.update(p.relative_to(directory).as_posix() for p in entries if p.is_file())
    return result


def check_plugin(directory: Path) -> list[str]:
    embedded = embedded_files(directory)
    errors = []
    if 'package.json' not in embedded:
        errors.append(f'{directory.name}: package.json is not embedded; Answer cannot discover this UI plugin')
    package = json.loads((directory / 'package.json').read_text(encoding='utf-8'))
    pending = [package.get('main', 'index.ts')]
    seen = set()
    while pending:
        name = pending.pop()
        if name in seen:
            continue
        seen.add(name)
        source = directory / name
        if not source.is_file():
            errors.append(f'{directory.name}: missing source {name}')
            continue
        if name not in embedded:
            errors.append(f'{directory.name}: {name} is imported but not embedded')
        if source.suffix not in ('.js', '.jsx', '.ts', '.tsx', '.css'):
            continue
        text = source.read_text(encoding='utf-8')
        refs = CSS_IMPORT.findall(text) if source.suffix == '.css' else IMPORT.findall(text)
        for ref in refs:
            base = source.parent / ref
            candidates = [base] + [Path(str(base) + suffix) for suffix in ('.js', '.ts', '.tsx', '.jsx', '.css', '/index.ts', '/index.js')]
            found = next((p.resolve() for p in candidates if p.is_file()), None)
            if found is None or not found.is_relative_to(directory.resolve()):
                errors.append(f'{directory.name}: unresolved local import {name} -> {ref}')
            else:
                pending.append(found.relative_to(directory.resolve()).as_posix())
    return errors


class EntryHTML(HTMLParser):
    def __init__(self):
        super().__init__()
        self.scripts = []
        self.styles = []

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'script' and 'src' in attrs:
            self.scripts.append(attrs['src'] or '')
        if tag == 'link' and attrs.get('rel') == 'stylesheet':
            self.styles.append(attrs.get('href') or '')


def check_html(html: str) -> list[str]:
    page = EntryHTML()
    page.feed(html)
    errors = []
    if not any(page.scripts):
        errors.append('missing frontend script src: loading overlay cannot be replaced')
    if not page.styles or any(not href.strip() for href in page.styles):
        errors.append('missing or empty stylesheet href')
    return errors


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--base-url', help='Optional deployed HTTPS site; read-only check of a native page and its assets')
    parser.add_argument('--path', default='/users/login', help='Same-origin page path for deployed validation')
    args = parser.parse_args()
    if args.base_url:
        base = urlsplit(args.base_url)
        if base.scheme != 'https' or not base.netloc or base.username or base.password:
            parser.error('--base-url must be an HTTPS origin without credentials')
        if not args.path.startswith('/') or args.path.startswith('//'):
            parser.error('--path must be a same-origin absolute path')
        url = urljoin(args.base_url, args.path)
        def get(target):
            request = Request(target, headers={'User-Agent': 'Mozilla/5.0 METAR-Frontend-Check', 'Cache-Control': 'no-cache'})
            with urlopen(request, timeout=15) as response:
                if urlsplit(response.url).netloc != base.netloc:
                    raise ValueError('unexpected cross-origin redirect')
                return response.read(), response.headers.get('Content-Type', '')
        try:
            body, _ = get(url)
            html = body.decode('utf-8')
            errors = check_html(html)
            page = EntryHTML(); page.feed(html)
            if not errors:
                for asset in page.scripts + page.styles:
                    target = urljoin(url, asset)
                    if urlsplit(target).netloc != base.netloc:
                        errors.append(f'external asset requires separate verification: {target}')
                        continue
                    content, content_type = get(target)
                    if not content or 'text/html' in content_type:
                        errors.append(f'asset is empty or returned HTML: {target}')
        except Exception as exc:
            errors = [f'deployed frontend check failed: {exc}']
    else:
        plugins = ROOT / 'services/forum-plugin'
        errors = [error for directory in plugins.iterdir() if (directory / 'package.json').is_file() for error in check_plugin(directory)]
    if errors:
        raise SystemExit('\n'.join(errors))
    print('Forum frontend assets verified')


if __name__ == '__main__':
    main()
