package pulse_user_center

import "embed"

// Answer's builder discovers this UI package after go mod vendor. Embedding
// keeps its source in vendor without forking or patching Answer's frontend.
//
//go:embed package.json index.ts language-switcher.js language-switcher.css route-policy.js navigation-bridge.js shared-theme.js theme-tokens.css native-shell.js home-navigation.js notification-navigation.js native-shell.css local-registration.js
var frontendAssets embed.FS
