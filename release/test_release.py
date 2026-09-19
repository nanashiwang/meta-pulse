import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import tarfile
import tempfile
import unittest

from manifest import assets, create

spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).parents[1] / "deploy/verify-release.py")
verify_release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify_release)


class ReleaseTests(unittest.TestCase):
    def packages(self, directory, entry="VERSION"):
        for name in assets("v0.1.0"):
            with tarfile.open(directory / name, "w:gz") as archive:
                member = tarfile.TarInfo(entry)
                member.size = 5
                archive.addfile(member, io.BytesIO(b"0.1.0"))

    def test_complete_inventory_and_checksums_bind_commit(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            self.packages(directory)
            create(directory, "v0.1.0", "a" * 40)
            document = json.loads((directory / "release-manifest.json").read_text())
            verify_release.verify(document, "v0.1.0", "a" * 40)
            for line in (directory / "SHA256SUMS").read_text().splitlines():
                digest, name = line.split("  ")
                self.assertEqual(digest, hashlib.sha256((directory / name).read_bytes()).hexdigest())
            for field, value in [("commit", "b" * 40), ("version", "v0.2.0"), ("assets", document["assets"][:-1])]:
                broken = copy.deepcopy(document)
                broken[field] = value
                with self.assertRaises(ValueError):
                    verify_release.verify(broken, "v0.1.0", "a" * 40)

    def test_secret_and_unsafe_paths_fail_before_manifest(self):
        for name in ["meta-pulse/.env", "meta-pulse/.env.production", "meta-pulse/.data/keys", "meta-pulse/runtime-keys/api.key", "meta-pulse/docker-compose.override.yml", "../outside"]:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                self.packages(directory, name)
                with self.assertRaises(ValueError):
                    create(directory, "v0.1.0", "a" * 40)
                self.assertFalse((directory / "release-manifest.json").exists())

    def test_missing_or_extra_asset_rejects_publication(self):
        for extra in [False, True]:
            with tempfile.TemporaryDirectory() as tmp:
                directory = Path(tmp)
                self.packages(directory)
                if extra:
                    (directory / "local-secret.txt").write_text("private fixture")
                else:
                    (directory / assets("v0.1.0")[0]).unlink()
                with self.assertRaises(ValueError):
                    create(directory, "v0.1.0", "a" * 40)


if __name__ == "__main__":
    unittest.main()
