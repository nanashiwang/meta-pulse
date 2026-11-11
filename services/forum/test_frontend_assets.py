import importlib.util
import json
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

HERE = Path(__file__).resolve().parent
spec = importlib.util.spec_from_file_location('verify_frontend', HERE / 'verify_frontend.py')
verify = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify)


class FrontendAssetsTest(unittest.TestCase):
    def fixture(self, root, embed):
        root.mkdir(exist_ok=True)
        (root / 'package.json').write_text(json.dumps({'main': 'index.ts'}))
        (root / 'frontend_assets.go').write_text('package fixture\nimport "embed"\n//go:embed ' + embed + '\nvar assets embed.FS\n')
        (root / 'index.ts').write_text("import { start } from './menu';\nimport './menu.css';\n")
        (root / 'menu.js').write_text("export { start } from './nested';")
        (root / 'nested.js').write_text('export const start = () => {};')
        (root / 'menu.css').write_text('.menu { display:flex; }')

    def test_recursive_imports_and_styles_must_be_embedded(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.fixture(root, 'package.json index.ts menu.js')
            errors = verify.check_plugin(root)
            self.assertTrue(any('nested.js' in e for e in errors))
            self.assertTrue(any('menu.css' in e for e in errors))
            self.fixture(root, 'package.json index.ts menu.js nested.js menu.css')
            self.assertEqual([], verify.check_plugin(root))

    def test_discovery_manifest_and_deleted_import_are_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            self.fixture(root, 'index.ts menu.js nested.js menu.css')
            self.assertTrue(any('package.json' in e for e in verify.check_plugin(root)))
            (root / 'nested.js').unlink()
            self.assertTrue(any('unresolved local import' in e for e in verify.check_plugin(root)))

    def test_spinner_html_without_bundle_is_rejected(self):
        self.assertEqual(2, len(verify.check_html('<link rel="stylesheet" href=""><div id="spin-container"></div>')))
        self.assertEqual([], verify.check_html('<link rel="stylesheet" href="/static/main.css"><script defer src="/static/main.js"></script>'))

    def test_final_binary_http_gate_rejects_missing_bundles(self):
        spec = importlib.util.spec_from_file_location('verify_binary', HERE / 'verify_binary.py')
        import sys
        sys.modules['verify_frontend'] = verify
        gate = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(gate)
        for broken in ('', 'index', 'asset'):
            with self.subTest(broken=broken), tempfile.TemporaryDirectory() as temp:
                fake = Path(temp) / 'answer'
                fake.write_text('#!' + sys.executable + '\n' + f"""
import os
from http.server import BaseHTTPRequestHandler, HTTPServer
class Handler(BaseHTTPRequestHandler):
    def do_GET(self):
        if self.path == '/install':
            body = b'<link rel="stylesheet" href="/static/main.css"><script src="/static/main.js"></script>'
            if {broken!r} == 'index': body = b'<div>loading</div>'
            kind = 'text/html'
        else:
            body = b'OK'
            kind = 'text/html' if {broken!r} == 'asset' and self.path.startswith('/static/') else 'text/plain'
        self.send_response(200)
        self.send_header('Content-Type', kind)
        self.end_headers()
        self.wfile.write(body)
HTTPServer(('127.0.0.1', int(os.environ['INSTALL_PORT'])), Handler).serve_forever()
""")
                fake.chmod(0o755)
                if broken:
                    with self.assertRaises(RuntimeError): gate.verify(fake)
                else:
                    gate.verify(fake)

    def test_real_plugins_are_complete(self):
        for directory in (verify.ROOT / 'services/forum-plugin').iterdir():
            if (directory / 'package.json').is_file():
                with self.subTest(plugin=directory.name):
                    self.assertEqual([], verify.check_plugin(directory))

    def test_go_vendor_preserves_frontend_files(self):
        go = os.environ.get('METAR_TEST_GO') or shutil.which('go')
        if not go:
            self.skipTest('Go required for actual vendor boundary check')
        for name in ('user-center-pulse', 'captcha-turnstile'):
            with self.subTest(plugin=name), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                source = verify.ROOT / 'services/forum-plugin' / name
                plugin = root / 'plugin'; plugin.mkdir()
                # Isolate the actual embed declaration from unrelated backend dependencies.
                shutil.copy(source / 'frontend_assets.go', plugin / 'frontend_assets.go')
                expected = verify.embedded_files(source)
                for asset in expected:
                    target = plugin / asset; target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copy(source / asset, target)
                (plugin / 'go.mod').write_text('module example.test/plugin\ngo 1.25\n')
                (root / 'go.mod').write_text('module example.test/consumer\ngo 1.25\nrequire example.test/plugin v0.0.0\nreplace example.test/plugin => ./plugin\n')
                (root / 'main.go').write_text('package main\nimport _ "example.test/plugin"\nfunc main() {}\n')
                env = dict(os.environ, GOWORK='off', GOTOOLCHAIN='local', GOPROXY='off')
                subprocess.run([go, 'mod', 'vendor'], cwd=root, env=env, check=True, capture_output=True)
                vendored = root / 'vendor/example.test/plugin'
                for asset in expected:
                    self.assertEqual((source / asset).read_bytes(), (vendored / asset).read_bytes(), asset)
                self.assertEqual([], verify.check_plugin(vendored))


if __name__ == '__main__':
    unittest.main()
