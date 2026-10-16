# Agent Note: Capability review and repository decision notes

Status: implemented

## Problem

Page-local changes can duplicate existing capabilities; chat-only reasoning does not travel with the repository. Changes need evidence from adjacent entry points and durable rationale without replacing existing project rules.

## Existing capabilities and impact

The repository contains a capability map, 23 active decision notes, a pinned Skill, a Make/Shell entry and an independent verification job. The complete project-decision-kit 1.0.1 release bundle matches all existing Skill files except the project-specific UPSTREAM.md. This integration extends the existing owner decision; the deployment/backup notes are unrelated, and no business note is imported or replaced. Existing architecture and product documentation remain authoritative. The capability map now also links concrete permission boundaries, data sources and tests for the current code.

## Decision

The repository uses the original project-decision-kit 1.0.1 installer and manifest to manage the pinned tools and the marked AGENTS block. The complete prior project provenance is retained in docs/AGENT_WORKFLOW_PROVENANCE.md, while the managed UPSTREAM.md matches the release bundle. The package's installation history describes its own provenance, not this project's history. This separation retains local rationale without creating a patched distribution or weakening hash checks.

Both `make verify-notes` and the existing independent CI job call the existing shell wrapper. It delegates to `python3 .agents/workflow.py verify`, so all three validators run only after file and managed-rule integrity checks. The wrapper remains callable from outside the checkout, including paths with spaces; inherited AGENT_NOTE_ROOT cannot redirect validation. Project-owned guidance, notes and maps stay outside managed files. Keep the manifest and use a complete downloaded bundle for future installation previews; the installed manager alone is not a distributable bundle.

Preserve current build tools, release rules and deployment boundaries. Keep note validation separate from application tests, and reuse the existing CI rather than installing another workflow with --ci. Record decisions, not invented historical explanations. Continue within existing authorization instead of adding routine confirmation gates.

## Alternatives considered

- Chat reminders alone are cheap but cannot preserve decisions alongside source changes.
- Installing only the Skill preserves rationale but cannot discover overlooked product capabilities; require code and adjacent-entry review as well.
- A shared absolute path saves duplicated tooling but breaks standalone clones and CI; vendor a fixed copy into each repository.
- Adding note validation to `make test` would cover everything in one command, but would require Bun for Go/deployment checks; keep the documented note target and independent CI job.
- Retaining a locally merged managed UPSTREAM.md would keep provenance in one place, but introduces a project-specific package or manifest exception. Preserve the exact prior record in a project-owned document and adopt the original package file after reviewing the conflict.
- Keeping the old shell validator loop is compatible but would let Make and CI bypass installation integrity. Delegate to the original manager, preserving the public entry point and archive-base environment.

## Verification

The 1.0.1 ZIP checksum matches the release SHA256SUMS.txt. Initial preview refused the provenance conflict without writing; after the reviewed preservation/adoption, preview lists only AGENTS.md, workflow.py and workflow-install.json. Installation doctor and verify pass for 23 notes. Six temporary-checkout regressions cover Make/Shell invocation from another directory, paths with spaces, managed tool tampering, missing templates, modified rule blocks, invalid note alternatives, foreign note-root environment and editable project rules. No production system is accessed.

Current local validation also passes `make test`, `make vet`, `make build` (Pulse binaries, blog and production community), and all 10 release regression tests. All 164 local links in the map/guide/provenance resolve. Repeating installation preview has no planned writes; all 22 managed files match the original bundle/manifest. Original AGENTS content remains an unchanged prefix, the entire prior provenance is preserved, and the 22 unrelated notes plus pre-existing go.work.sum remain unchanged. Database-dependent tests that skip without DSNs are not counted as database acceptance; real MySQL and container checks belong to CI. Production account and actual Benefit receipt acceptance remain external.

Previous integration evidence (not a claim of new execution):

The shared `make verify-notes` entry passes for all 15 active notes. An isolated temporary copy with the Alternatives heading removed fails through Make, confirming error propagation. The standalone wrapper also passes from `/tmp`; README, map and workflow-guide local links resolve. `make -n test build` preserves native command selection, and release metadata preflight passes for the unchanged application version.

Local note checks pass from outside the repository, and project documentation links resolve. Original tracked AGENTS.md content is preserved as an unchanged prefix. Isolated temporary fixtures confirm rejection of missing alternatives, dead links, missing seals, modified archived text and modified text plus replacement seals against a Git baseline. Remote CI results are recorded by the actual workflow runs, not assumed from local success. Unrelated local changes are excluded from this task.

## Consequences

Maps can become stale and need source verification. Structural checks do not establish design quality or guarantee complete note coverage. Bun is a development-only prerequisite, not an application dependency.

The benefit is searchable design rationale and explicit adjacent-capability review. The maintenance cost is keeping maps and notes aligned with code. Tooling updates follow the project commit and release policy; a Release does not deploy services. Python 3.9+ is now also required for the note entry point, and preserving provenance in a separate project-owned file adds one document to maintain. No business data or application behavior changes.
