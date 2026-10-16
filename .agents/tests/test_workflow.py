"""Exercise the installed CLI through the repository's public entry points."""
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest


ROOT = Path(__file__).resolve().parents[2]


class WorkflowIntegrationTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="meta-pulse workflow ")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name) / "project with spaces"
        self.root.mkdir()
        shutil.copytree(ROOT / ".agents", self.root / ".agents",
                        ignore=shutil.ignore_patterns("__pycache__", "tests"))
        for name in ("AGENTS.md", "Makefile"):
            shutil.copyfile(ROOT / name, self.root / name)
        self.env = os.environ.copy()
        self.env.pop("AGENT_NOTE_ARCHIVE_BASE_REF", None)
        self.env["AGENT_NOTE_ROOT"] = str(self.root / "wrong-note-root")

    def run_cli(self, *args, ok=True):
        result = subprocess.run(args, cwd=self.temp.name, env=self.env,
                                capture_output=True, text=True)
        if ok:
            self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        else:
            self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)
        return result.stdout + result.stderr

    def doctor(self, **kwargs):
        return self.run_cli(sys.executable, str(self.root / ".agents/workflow.py"),
                            "doctor", str(self.root), **kwargs)

    def wrapper(self, **kwargs):
        return self.run_cli("bash", str(self.root / ".agents/verify-notes.sh"), **kwargs)

    def test_existing_entries_work_outside_checkout_and_ignore_foreign_note_root(self):
        self.doctor()
        self.assertIn("note(s) verified", self.wrapper())
        self.run_cli("make", "-C", str(self.root), "verify-notes")

    def test_changed_managed_tool_fails_through_make(self):
        script = self.root / ".agents/skills/write-notes-like-deepseek/scripts/verify-agent-note-tree.ts"
        script.write_text(script.read_text() + "\n// local edit\n")
        output = self.run_cli("make", "-C", str(self.root), "verify-notes", ok=False)
        self.assertIn("verify-agent-note-tree.ts", output)

    def test_missing_managed_file_fails_doctor(self):
        (self.root / ".agents/skills/write-notes-like-deepseek/templates/proposed.md").unlink()
        self.assertIn("templates/proposed.md", self.doctor(ok=False))

    def test_modified_managed_rules_fail_doctor(self):
        agents = self.root / "AGENTS.md"
        agents.write_text(agents.read_text().replace("## 能力核对与决策笔记", "## Changed rules"))
        self.assertIn("AGENTS.md", self.doctor(ok=False))

    def test_invalid_project_note_is_not_hidden_by_foreign_root(self):
        note = self.root / ".agents/notes/implemented/process/2026-09-29-capability-review-and-decision-notes.md"
        note.write_text(note.read_text().replace("## Alternatives considered", "## Options"))
        self.assertIn("Alternatives", self.wrapper(ok=False))

    def test_project_rules_outside_managed_block_remain_editable(self):
        agents = self.root / "AGENTS.md"
        agents.write_text("Project-specific additional guidance.\n\n" + agents.read_text())
        self.doctor()


if __name__ == "__main__":
    unittest.main()
