from __future__ import annotations

import json
import subprocess
import tempfile
import unittest
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]


class ProductionBuildTest(unittest.TestCase):
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
            self.assertNotIn('style="', (output / "assets/app.js").read_text(encoding="utf-8"))
            self.assertIn('data-action="skip"', (output / "index.html").read_text(encoding="utf-8"))

    def test_javascript_syntax(self):
        subprocess.run(["node", "--check", str(ROOT / "src/adapters.js")], check=True)
        subprocess.run(["node", "--check", str(ROOT / "src/app.js")], check=True)

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
