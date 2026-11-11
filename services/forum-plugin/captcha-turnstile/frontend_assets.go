package turnstile

import "embed"

// Answer discovers frontend plugins from the vendored package.json. Keep the
// entry point and its local dependencies when go mod vendor copies this package.
//
//go:embed package.json index.ts controller.js widget.js
var frontendAssets embed.FS
