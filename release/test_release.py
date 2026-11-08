import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

from manifest import assets, create

spec = importlib.util.spec_from_file_location("verify_release", Path(__file__).parents[1] / "deploy/verify-release.py")
verify_release = importlib.util.module_from_spec(spec)
spec.loader.exec_module(verify_release)

published_spec = importlib.util.spec_from_file_location("published", Path(__file__).with_name("published.py"))
published = importlib.util.module_from_spec(published_spec)
assert published_spec.loader is not None
published_spec.loader.exec_module(published)

publish_spec = importlib.util.spec_from_file_location("publish", Path(__file__).with_name("publish.py"))
publish = importlib.util.module_from_spec(publish_spec)
assert publish_spec.loader is not None
publish_spec.loader.exec_module(publish)


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

    def published_fixture(self, directory, commit="a" * 40):
        self.packages(directory)
        create(directory, "v0.1.0", commit)
        assets = []
        for path in sorted(directory.iterdir()):
            assets.append({"name": path.name, "size": path.stat().st_size, "state": "uploaded",
                           "digest": f"sha256:{hashlib.sha256(path.read_bytes()).hexdigest()}"})
        return {"tag_name": "v0.1.0", "draft": False, "assets": assets}

    def test_published_release_replay_is_read_only(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            release = self.published_fixture(directory)
            published.verify(directory, "v0.1.0", "a" * 40, "a" * 40, release)
            published.verify(directory, "v0.1.0", "a" * 40, "a" * 40, release)

    def test_published_release_skips_build_and_all_mutations(self):
        release = {"tag_name": "v0.1.0", "draft": False, "assets": []}
        with patch.dict(os.environ, {"GH_REPO": "owner/repo"}), \
                patch.object(publish, "output", return_value="a" * 40), \
                patch.object(publish, "tag_commit", return_value="a" * 40), \
                patch.object(publish, "release_state", return_value=release), \
                patch.object(publish, "verify_remote") as verify_remote, \
                patch.object(publish.subprocess, "run") as run:
            publish.publish("v0.1.0", build_if_needed=True)
        verify_remote.assert_called_once_with("owner/repo", "v0.1.0", "a" * 40, release)
        run.assert_not_called()

    def test_published_release_missing_asset_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            release = self.published_fixture(directory)
            (directory / "SHA256SUMS").unlink()
            with self.assertRaisesRegex(ValueError, "missing"):
                published.verify(directory, "v0.1.0", "a" * 40, "a" * 40, release)

    def test_published_release_source_conflict_fails(self):
        with tempfile.TemporaryDirectory() as tmp:
            directory = Path(tmp)
            release = self.published_fixture(directory)
            with self.assertRaisesRegex(ValueError, "different commit"):
                published.verify(directory, "v0.1.0", "a" * 40, "b" * 40, release)


if __name__ == "__main__":
    unittest.main()
