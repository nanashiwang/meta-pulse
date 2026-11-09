# Agent Note: Blog CI Must Honor Its npm Lockfile

Status: implemented

## Problem

The Blog workflow installed dependencies with `pnpm install --no-frozen-lockfile`, but `sites/blog` has no pnpm lockfile. This caused CI to resolve transitive dependencies afresh and fail when the registry could not provide `@algolia/ingestion@1.63.0`, even though the checked-in npm lockfile and local build use `1.57.0` successfully.

## Decision

Use the repository's existing `package-lock.json` as the dependency source of truth in CI: install with `npm ci --ignore-scripts` and build with `npm run build`. Do not add a second package-manager lockfile just for CI.

## Alternatives considered

- Generate and maintain a pnpm lockfile: this would add a second lockfile and require keeping it synchronized with the existing npm workflow without a product need.
- Pin the failing transitive Algolia package through overrides: this would patch around one resolution symptom while leaving CI free to drift from the committed dependency graph.

## Consequences

CI and local/release builds now install the same exact dependency graph. The workflow no longer depends on pnpm setup or uncached transitive registry versions. Dependency updates must continue to update `package-lock.json` deliberately.

## Testing

`npm ci --ignore-scripts` and `npm run build` succeeded locally. The Blog workflow's install/build steps and Release workflow are rerun for v0.2.102.
