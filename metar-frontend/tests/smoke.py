"""Render the exact standalone HTML without navigating to a network/file origin.
Uses native hash routing; no browser security policy is modified.
Set CHROMIUM_PATH to an installed Chromium; otherwise use Playwright's browser.
"""
from pathlib import Path
import json
import os
import shutil
from playwright.sync_api import sync_playwright

ROOT = Path(__file__).resolve().parents[1]
HTML = (ROOT / 'index.html').read_text(encoding='utf-8')
results = {
    'harness': 'Exact HTML via set_content; native hash routing; no policy changes',
    'viewports': [{'width': 1440, 'height': 1050}, {'width': 390, 'height': 844}],
    'console_errors': [], 'network_requests': [], 'route_checks': [],
    'viewport_checks': [], 'overflow': []
}
with sync_playwright() as p:
    executable = os.environ.get('CHROMIUM_PATH') or shutil.which('chromium')
    browser = p.chromium.launch(executable_path=executable, headless=True)
    page = browser.new_page(viewport=results['viewports'][0], device_scale_factor=1)
    page.on('pageerror', lambda e: results['console_errors'].append(str(e)))
    page.on('console', lambda m: results['console_errors'].append(m.text) if m.type == 'error' else None)
    page.on('request', lambda r: results['network_requests'].append(r.url))
    page.set_content(HTML, wait_until='load')
    paths = page.evaluate('SITEMAP.flatMap(x=>x[1].map(y=>y[1]))')

    def go(path):
        page.evaluate('(path)=>{closeModal();location.hash=path;render()}', path)
        page.wait_for_timeout(65)

    def screenshot(path, filename, role='member', theme='light', mobile=False):
        page.evaluate('([role,theme])=>{state.role=role;state.scenario="normal";state.theme=theme;save()}', [role,theme])
        page.set_viewport_size(results['viewports'][1 if mobile else 0])
        go(path)
        page.wait_for_timeout(120)
        page.screenshot(path=str(ROOT/'screenshots'/filename), full_page=False)

    for viewport in results['viewports']:
        page.set_viewport_size(viewport)
        for path in paths:
            page.evaluate("state.role='admin';state.scenario='normal';save()")
            go(path)
            title = page.locator('main h1').first.inner_text() if page.locator('main h1').count() else '(missing)'
            text = page.locator('main').inner_text()
            ok = '这份本地状态需要重新加载' not in text and title != '(missing)'
            check = {'path': path, 'title': title, 'ok': ok}
            results['viewport_checks'].append({**check, 'width': viewport['width']})
            if viewport['width'] == 1440:
                results['route_checks'].append(check)
            overflow = page.evaluate('document.documentElement.scrollWidth-innerWidth')
            if overflow > 1:
                results['overflow'].append({'path': path, 'width': viewport['width'], 'pixels': overflow})
    screenshot('/discover', '01-home-desktop.png')
    screenshot('/pulse', '02-pulse-desktop.png', role='bound')
    screenshot('/admin/entries', '03-entries-desktop.png', role='admin')
    screenshot('/discover', '04-home-dark.png', theme='dark')
    screenshot('/discover', '05-home-mobile.png', mobile=True)
    screenshot('/pulse', '06-pulse-mobile.png', role='bound', mobile=True)
    screenshot('/question/q1', '07-question-desktop.png')
    screenshot('/settings/binding', '08-binding-desktop.png')
    browser.close()

(ROOT/'tests/smoke-results.json').write_text(json.dumps(results,ensure_ascii=False,indent=2),encoding='utf-8')
summary = {
    'routes': len(results['route_checks']),
    'viewport_checks': len(results['viewport_checks']),
    'failed': [r for r in results['viewport_checks'] if not r['ok']],
    'console_errors': results['console_errors'],
    'network_requests': results['network_requests'],
    'overflow': results['overflow']
}
print(json.dumps(summary,ensure_ascii=False,indent=2))
raise SystemExit(1 if any(summary[k] for k in ('failed','console_errors','network_requests','overflow')) else 0)
