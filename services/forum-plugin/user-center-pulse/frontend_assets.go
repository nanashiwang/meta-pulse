package pulse_user_center

import "embed"

// Answer's builder discovers this UI package after go mod vendor. Embedding
// keeps its source in vendor without forking or patching Answer's frontend.
//
//go:embed package.json index.ts language-switcher.js language-switcher.css
var frontendAssets embed.FS
