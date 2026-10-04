from __future__ import annotations

import json
import os
import subprocess
import tempfile
import unittest
from html.parser import HTMLParser
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class InitialDOM(HTMLParser):
    """Inspect painted markup, excluding inert templates and no-script fallback."""
    def __init__(self):
        super().__init__()
        self.inert = 0
        self.elements = []

    def handle_starttag(self, tag, attrs):
        if tag in ('noscript', 'template'):
            self.inert += 1
        if not self.inert:
            self.elements.append((tag, dict(attrs)))

    def handle_endtag(self, tag):
        if tag in ('noscript', 'template'):
            self.inert -= 1


class ProductionBuildTest(unittest.TestCase):
    def test_every_entry_has_full_anonymous_first_paint_without_javascript(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / 'dist'
            subprocess.run(['python3', str(ROOT / 'build.py'), '--output', str(output)], check=True)
            for path in output.rglob('*.html'):
                source = path.read_text()
                dom = InitialDOM()
                dom.feed(source)
                elements = dom.elements
                for tag in ('header', 'aside', 'main', 'nav'):
                    self.assertTrue(any(name == tag for name, _ in elements), (path, tag))
                self.assertTrue(any(attrs.get('class') == 'page-loading' for _, attrs in elements), path)
                self.assertTrue(any('data-loading-recovery' in attrs for _, attrs in elements), path)
                self.assertTrue(any(attrs.get('href') == '/questions' for _, attrs in elements), path)
                self.assertFalse(any(name == 'article' or 'data-pulse-core' in attrs for name, attrs in elements), path)
                self.assertNotIn('/admin/dashboard', source)
                self.assertIn('id="metar-loading-template"', source)
                self.assertLess(source.index('/metar-assets/loading.js'), source.index('/metar-assets/app.js'))
                # Searchable prose remains available with JS disabled, not as the loading UI.
                if path.name == 'latest.html':
                    self.assertIn('<noscript><section class="page-inner seo-fallback"><h1>最新讨论</h1>', source)
            self.assertTrue((output / 'assets/loading.js').is_file())

    def test_private_deployment_umask_keeps_public_assets_readable(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "dist"
            subprocess.run(["python3", str(ROOT / "build.py"), "--output", str(output)],
                           check=True, preexec_fn=lambda: os.umask(0o077))
            for path in [output, *output.rglob("*")]:
                self.assertEqual(0o755 if path.is_dir() else 0o644, path.stat().st_mode & 0o777)

    def test_build_is_separate_from_prototype_and_contains_no_mock_markers(self):
        with tempfile.TemporaryDirectory() as temp:
            output = Path(temp) / "dist"
            subprocess.run(["python3", str(ROOT / "build.py"), "--output", str(output)], check=True)
            info = json.loads((output / "BUILD_INFO.json").read_text(encoding="utf-8"))
            self.assertEqual("production", info["kind"])
            self.assertFalse(info["mockData"])
            combined = "\n".join(path.read_text(encoding="utf-8") for path in output.rglob("*") if path.is_file())
            for marker in ("SEED_POSTS", "CANDIDATES", "演示身份", "未发送到线上", "X-Pulse-Signature", "New-Api-User"):
                self.assertNotIn(marker, combined)
            self.assertIn("/answer/api/v1", (output / "runtime-config.js").read_text(encoding="utf-8"))
            self.assertIn("/metar-assets/app.js", (output / "index.html").read_text(encoding="utf-8"))
            self.assertIn("/metar-assets/favicon.svg", (output / "index.html").read_text(encoding="utf-8"))
            self.assertTrue((output / "assets/favicon.svg").is_file())
            index = (output / "index.html").read_text(encoding="utf-8")
            self.assertIn("/metar-assets/avatars.js", index)
            self.assertLess(index.index("/metar-assets/avatars.js"), index.index("/metar-assets/app.js"))
            self.assertTrue((output / "assets/avatars.js").is_file())
            self.assertTrue((output / "assets/router.js").is_file())
            self.assertTrue((output / "assets/route-policy.js").is_file())
            self.assertTrue((output / "assets/theme.js").is_file())
            self.assertTrue((output / "assets/pulse-core.js").is_file())
            self.assertTrue((output / "assets/pulse-core.css").is_file())
            self.assertNotIn("/metar-assets/pulse-core.js", index)
            for feature in ("admin-periods", "admin-pulse", "growth-presentation", "growth"):
                self.assertTrue((output / f"assets/{feature}.js").is_file())
                self.assertNotIn(f"/metar-assets/{feature}.js", index)
            self.assertLess(index.index("/metar-assets/theme.js"), index.index("/metar-assets/app.css"))
            self.assertLess(index.index("/metar-assets/route-policy.js"), index.index("/metar-assets/router.js"))
            self.assertLess(index.index("/metar-assets/router.js"), index.index("/metar-assets/app.js"))
            self.assertTrue((output / "assets/i18n.js").is_file())
            self.assertLess(index.index("/metar-assets/i18n.js"), index.index("/metar-assets/adapters.js"))
            self.assertNotIn('style="', (output / "assets/app.js").read_text(encoding="utf-8"))
            self.assertIn('data-action="skip"', (output / "index.html").read_text(encoding="utf-8"))

    def test_javascript_syntax(self):
        subprocess.run(["node", "--check", str(ROOT / "src/loading.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/i18n.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/adapters.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/avatars.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/router.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/app.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/pulse-core.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/admin-pulse.js")], check=True)

    def test_ui_state_regressions(self):
        source = (ROOT / "src/app.js").read_text(encoding="utf-8")
        theme_line = next(line for line in source.splitlines() if "action === 'theme'" in line)
        self.assertIn("setTheme", theme_line)
        self.assertNotIn("navigate", theme_line)
        self.assertIn('data-action="retry-identity"', source)
        self.assertIn("currentUserState === 'unavailable'", source)
        self.assertIn("setAttribute('aria-expanded', 'false')", source)
        self.assertNotIn("behavior: 'instant'", source)

    def test_runtime_config_has_no_secret_fields(self):
        config = json.loads((ROOT / "config.production.json").read_text(encoding="utf-8"))
        forbidden_keys = {"secret", "password", "api_key", "cookie", "dsn", "signature"}
        normalized_keys = {key.lower() for key in config}
        self.assertTrue(forbidden_keys.isdisjoint(normalized_keys))
        serialized = json.dumps(config).lower()
        for marker in ("x-pulse-signature", "new-api-user", "begin private key"):
            self.assertNotIn(marker, serialized)
        self.assertEqual("/users/account-recovery", config["answerPasswordResetPath"])


if __name__ == "__main__":
    unittest.main()
