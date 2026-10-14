# Agent Note: Desktop sidebar toggle

Status: implemented

## Problem

The existing menu button is mobile-only. Desktop members need to reclaim sidebar space without losing access to its entries.

## Decision

Reuse the same menu button for desktop collapse and mobile drawer opening. Persist only the desktop preference in browser storage; apply it before first paint and synchronize the initial shell, runtime chrome, accessibility state and viewport transitions. A closed sidebar is inert. Content and the taxonomy drawer use the existing sidebar-width variable.

## Alternatives considered

CSS-only hiding cannot express a saved preference, keyboard state or viewport transitions. Saving mobile drawer state would reopen it unexpectedly after navigation. Keeping the existing control and a small presentation controller avoids another navigation implementation.

## Existing-note audit

Community loading-shell owns first-paint consistency; community navigation simplification and member-sidebar retain their link inventories and privacy boundaries. This toggle changes layout only.

## Validation

Controller tests cover blocked storage and viewport transitions. Production-shell browser acceptance verifies desktop collapse/reopen, zero remaining sidebar margin, refresh, initial shell with app.js blocked, focus returning from hidden links, category drawer alignment, 390px navigation and Escape closure, and dark/light rendering. Existing community tests and production build pass.

## Consequences

Browser storage can be disabled; the control still works for the current page. Native Answer pages retain their own navigation controls.
