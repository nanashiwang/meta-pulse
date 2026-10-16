# Meta Pulse: decision-note workflow

The [pinned project-local Skill](../.agents/skills/write-notes-like-deepseek/UPSTREAM.md) records rationale alongside code. It is managed by project-decision-kit 1.0.1; [project provenance and adaptation](AGENT_WORKFLOW_PROVENANCE.md) preserve the original Meta Pulse source record. It does not replace existing project instructions or architecture documents. The repository is self-contained: no absolute path to another checkout or global Codex memory is required.

## Working sequence

1. State the business outcome. Read [the capability map](FEATURE_MAP.md), verify current code and permissions, and inspect related UI/API/data paths before choosing a solution.
2. Search active proposed, implemented and rejected notes. Reuse an existing note when only its facts change. Record new non-trivial decisions with the [proposal template](../.agents/skills/write-notes-like-deepseek/templates/proposed.md), including evidence of existing capabilities, affected entry points, real alternatives and acceptance criteria.
3. Proceed within existing user authorization. Imported confirmation defaults do not introduce repeated approval requirements. Missing material decisions still need user input; proposed notes do not themselves authorize deployment.
4. Once implementation and relevant checks are complete, rewrite Proposal as Decision and acceptance/risks as actual validation/consequences; move to implemented in the same commit as code. Decision reversals get new notes with links. Archive only eligible superseded implemented notes, not abandoned proposals.
5. Run `make verify-notes` and the existing tests/build required for the changed area. Update affected map entries. Mechanical edits do not require new notes; do not retroactively invent historical decisions.

## Layout and commands

Notes live under `.agents/notes/{proposed,implemented,rejected,archived}/{class}/yyyy-mm-dd-topic.md`. Classes are feature, bug-fix, simplification, architecture, process and testing. Create directories as needed. No central note index is required; the capability map indexes code, not individual notes.

Python 3.9+ runs the installation manager; Bun executes the pinned TypeScript tooling (CI: 1.3.14). Both are development/CI dependencies, not application runtime dependencies. From any directory, invoke the shell script by its path; auxiliary commands below run from the repository root:

```sh
make verify-notes
python3 .agents/workflow.py doctor # Managed-file hashes, AGENTS block and Bun availability
python3 .agents/workflow.py verify # Same validation as make verify-notes
python3 -m unittest discover -s .agents/tests -v # Integration and rejection regressions
# Equivalent standalone entry, also usable by absolute path from another directory:
bash .agents/verify-notes.sh
bun .agents/skills/write-notes-like-deepseek/scripts/archive-agent-note.ts <old-note> --superseded-by <new-note>
bun .agents/skills/write-notes-like-deepseek/scripts/build-board.ts --bundle .agents/notes /tmp/decision-board.html 'Project decisions'
```

The archive command mutates files; inspect the diff and inbound links. The optional board bundles note content, so keep private notes and credentials out of published artifacts. Do not commit generated boards or real customer data, passwords, tokens or private keys.

## Verification boundaries

Local Make and CI both delegate to the existing shell entry, which invokes the installed manager: it verifies managed-file hashes and the AGENTS block before running all three pinned validators. The manager removes inherited `AGENT_NOTE_ROOT` so validation cannot accidentally target another project. Note checks remain separate from `make test` and application builds, so Go-only workflows do not gain Python/Bun prerequisites from this integration.

The independent Verify Agent Notes CI job runs on main pushes and pull requests, with full Git history. Archive comparison uses the pre-push commit or PR base, not the new HEAD. CI failure does not automatically configure branch protection; no branch protection changes are included here.

Checks enforce directory/format/link and archive-seal integrity, not completeness of product research, truth of alternatives, or optimality of a design. Existing business tests, builds, release checks and production acceptance remain separate. Keep README, architecture, community and implementation-plan reading requirements. The root is a Go workspace, not a module; use existing explicit module test targets.

## Installation and upgrades

Clone the repository and install Python 3.9+ and Bun, then run `doctor` and `verify` above. No global Codex Skill is required. All managed tooling, templates, installation state and project notes belong in Git; generated boards do not.

For upgrades, download the complete [release bundle](https://github.com/nanashiwang/new-api/releases/tag/project-decision-kit-v1.0.1), verify its SHA256SUMS.txt, and extract it outside this checkout. From the project root:

```sh
python3 /path/to/project-decision-kit/workflow.py install "$PWD"
# Review the preview and resolve conflicts without losing project content first.
python3 /path/to/project-decision-kit/workflow.py install "$PWD" --apply
python3 .agents/workflow.py doctor
make verify-notes
```

Use the complete downloaded bundle for `install`; the installed `.agents/workflow.py` is the project `doctor`/`verify` entry, not a standalone distribution containing VERSION/templates/payload. Omit `--ci` here because the existing Verify Agent Notes job is already integrated. Keep `.agents/workflow-install.json`; do not regenerate hashes to hide local changes. Project-specific guidance belongs outside managed files and outside the marked AGENTS block. Existing AGENTS rules, project notes, this guide and the capability map remain project-owned. The source record explains the reviewed v1.0.1 provenance conflict resolution.
