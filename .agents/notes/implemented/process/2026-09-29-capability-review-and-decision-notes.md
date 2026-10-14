# Agent Note: Capability review and repository decision notes

Status: implemented

## Problem

Page-local changes can duplicate existing capabilities; chat-only reasoning does not travel with the repository. Changes need evidence from adjacent entry points and durable rationale without replacing existing project rules.

## Existing capabilities and impact

The repository already contains a capability map, 15 active decision notes, a pinned Skill and an independent verification job. Its validator sources match the new-api copy; no business notes need importing. Existing architecture and product documentation remain authoritative. The local Makefile and README now expose the existing workflow alongside native project checks.

## Decision

The repository now has a scoped capability map, a project-local pinned decision-note Skill, and an offline Bun verification entry point. Both `make verify-notes` and the independent CI job call the existing shell wrapper; the wrapper remains callable from outside the checkout. Preserve current build tools, release rules and deployment boundaries. Record decisions, not invented historical explanations. Continue within existing authorization instead of adding routine confirmation gates.

## Alternatives considered

- Chat reminders alone are cheap but cannot preserve decisions alongside source changes.
- Installing only the Skill preserves rationale but cannot discover overlooked product capabilities; require code and adjacent-entry review as well.
- A shared absolute path saves duplicated tooling but breaks standalone clones and CI; vendor a fixed copy into each repository.
- Adding note validation to `make test` would cover everything in one command, but would require Bun for Go/deployment checks; keep the documented note target and independent CI job.

## Verification

The shared `make verify-notes` entry passes for all 15 active notes. An isolated temporary copy with the Alternatives heading removed fails through Make, confirming error propagation. The standalone wrapper also passes from `/tmp`; README, map and workflow-guide local links resolve. `make -n test build` preserves native command selection, and release metadata preflight passes for the unchanged application version.

Local note checks pass from outside the repository, and project documentation links resolve. Original tracked AGENTS.md content is preserved as an unchanged prefix. Isolated temporary fixtures confirm rejection of missing alternatives, dead links, missing seals, modified archived text and modified text plus replacement seals against a Git baseline. Remote CI results are recorded by the actual workflow runs, not assumed from local success. Unrelated local changes are excluded from this task.

## Consequences

Maps can become stale and need source verification. Structural checks do not establish design quality or guarantee complete note coverage. Bun is a development-only prerequisite, not an application dependency.

The benefit is searchable design rationale and explicit adjacent-capability review. The maintenance cost is keeping maps and notes aligned with code. This workflow-only update does not change application versions, publish a release or deploy services.
