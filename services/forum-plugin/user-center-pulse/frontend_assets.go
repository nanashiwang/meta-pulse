package pulse_user_center

import "embed"

// Answer's builder discovers this UI package after go mod vendor. Embedding
// keeps its source in vendor without forking or patching Answer's frontend.
//
//go:embed account-switch.js native-favicon.js analytics-client.js image-import.js image-import-editor.tsx drafts-store.js package.json index.ts language-switcher.js language-switcher.css route-policy.js navigation-bridge.js shared-theme.js theme-tokens.css native-shell.js composer-layout.js growth-presentation.js home-navigation.js notification-navigation.js native-shell.css local-registration.js avatar-cropper.js avatar-cropper.css
var frontendAssets embed.FS
