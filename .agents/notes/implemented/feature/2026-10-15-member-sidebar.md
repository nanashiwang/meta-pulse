# Agent Note: Member sidebar, browser drafts and community events

Status: implemented

## Problem

Members need visible draft/message entries and upcoming community events with registration. Existing chat and native notifications already own messages; Answer stores only one local question/answer draft and has no event registration service.

## Decision

Reuse chat and notification pages. Add an account-scoped local draft library integrated with Answer's storage adapter, preserving native editing and publish cleanup; disclose current-browser persistence and require explicit ownership confirmation to import legacy unscoped drafts. Add independent plugin tables in Answer MySQL for administrator-published events and active-member registrations; serialize capacity changes and retain command receipts. No Pulse/new-api dependency, payments or automatic rewards.

## Alternatives considered

A second chat implementation duplicates membership and unread contracts. Linking drafts only to the composer does not provide a list or multiple saved questions. Cloud draft synchronization requires a separate conflict/offline policy and is deferred. Using public replies as registrations avoids tables but exposes participants and cannot enforce capacity atomically; use private member rows and administrator-only roster.

## Existing-note audit

Community chat owns message delivery, membership and unread state; reuse it. Community navigation simplification remains in force, including native notification navigation. Taxonomy remains a separate content classification system. This feature does not reinterpret reward activity or grant eligibility.

## Validation

Draft ownership, multiple drafts, resume/publish/delete, malformed storage and legacy import; authenticated and anonymous sidebar, message unread and notifications; real MySQL concurrent last-slot registration, duplicates, withdrawal, cancellation, administrator revocation and roster privacy; browser desktop/mobile bilingual and dark/light flows. Release/deploy verified independently.

## Consequences

Local drafts are not cross-device backups. Native storage integration is version-specific and tested against Answer 1.7.1. Events are informational and free; user-authored public announcements must be explicit administrator actions. Empty production lists stay empty until an administrator publishes.

## Implementation findings

Real MySQL stress exposed insert-gap deadlocks when rejected full registrations rolled back a newly created actor lock row. The actor row is initialized independently; the command transaction then locks it before the event, with bounded database deadlock retries. Native Answer prefills need a fresh draft slot, and browser ownership uses stable user ID rather than mutable username.

Actual validation: local MySQL 8 concurrent 100-request last-slot/replay tests, withdrawal/cancellation and revoked-admin roster checks passed. Production-shell browser acceptance used the real event store and synthetic local identities for create/join/full/roster/cancel and response-loss/reload/retry. A separately initialized real Answer instance verified native question autosave, shell draft listing, resume with unchanged text, and successful publication removing only the current draft. Existing chat is reused; mobile toolbar includes native notifications without changing composer sizing. Go tests/vet, frontend tests, plugin image build and Nginx SEO/deep-link checks passed. No real event was created in production during development.
