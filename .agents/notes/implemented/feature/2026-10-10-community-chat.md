# Agent Note: Community private and group chat

Status: implemented

## Problem

The global publish button duplicates the feed's new-topic action. Members need private conversations and groups, while the current Answer 1.7.1 plugin has authenticated routing, live account checks and MySQL access but no chat store or APIs. Pulse must stay outside community communication.

## Decision

Replace the production header publish action with Chat. Extend the existing Answer plugin with a bounded MySQL-backed text messaging module, keeping account/status authority with Answer. Conversation membership, room-local message sequence, read cursors, invitations, blocks and reports live in plugin-owned tables in the Answer backup boundary. Writes use same-origin authenticated requests and stable actor/request keys; concurrent retries return one original result or conflict. All message reads recheck live account and membership, including after removal. No local persistent message/token copies or public SEO projection.

Private rooms are unique per pair. Groups have an owner, explicit invitations, accept/decline, member removal, ownership transfer, leaving and closing. Newly joined/rejoined members see only messages from their join boundary; removed users lose access immediately. Blocked pairs cannot initiate private messages or invitations. Reports disclose only the reported message to live administrators and keep a moderation audit. Finite message, membership, room-creation and send-rate limits bound abuse and storage growth.

Foreground chat uses short, incremental polling; hidden pages stop and transient failures back off. The header reads a lightweight unread summary. Text-only messages avoid new file storage or scanning dependencies. UI preserves drafts during updates and retries ambiguous sends with the same key.

## Alternatives considered

- An external chat server supplies mature realtime/media features, but introduces another identity/backup/deployment boundary and resource cost on this constrained server.
- WebSocket fanout offers lower latency, but adds long-lived connection lifecycle, multi-instance pubsub and gateway requirements. Indexed incremental polling is sufficient for the initial community scale and keeps the database authoritative.
- Reusing public posts or native notifications as messages is smaller but cannot express private membership, history and group governance safely.

## Existing-note audit

Active notes were searched for chat, messaging and identity. No chat owner note exists. Existing taxonomy and shared-navigation decisions remain in force; chat extends their route inventory and header without changing category behavior.

## Validation

Real MySQL tests passed for duplicate/concurrent sends, read/unread cursors, nonmember access, join/removal boundaries, current suspension, blocks, invitations, owner controls, rate limits and report-only moderation. Route, strict request, XSS and retry tests passed. Local compiled Answer and MySQL with three independently authenticated test accounts verified private/group messages, explicit invitation acceptance and history boundary, ordinary member controls, drafts, unread, snapshot-only moderation and a lost response after commit with exactly one persisted message. Desktop and 320/390px mobile dark/light and bilingual layouts were inspected. Release gates and assets are verified separately; release does not deploy production.

## Risks

Polling imposes a bounded query load and delivers messages within a few seconds rather than instant push. Text is access-controlled server-side, not end-to-end encrypted. Backups include private messages and must retain existing restricted access. Production rollout needs the forum plugin, web assets and generated gateway routes together.

## Consequences

Actor guards use exclusive upserts to avoid shared-to-exclusive upgrade deadlocks under duplicate fanout. Room message versions propagate old-message redactions as well as new messages. Inbox peers are joined in one query, and the client loads at most two bounded pages to keep all 100 allowed memberships visible. Message requests, history and report audit remain retained; rate limits bound growth speed, not lifetime storage. The mobile workspace measures available space through flex layout so wrapped English tools do not push Send behind the bottom navigation.

Account revocation clears the active transcript, conversation list, unread badge, dialogs and in-memory drafts on the next authenticated failure; session changes use the existing shell identity invalidation. Existing conversations and messages survived a restart of the local Answer process.
