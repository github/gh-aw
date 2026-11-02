// Package ghaw contains compiler resources shared by native and WebAssembly builds.
package ghaw

import "embed"

// ConnectorResources embeds the authored declarations without generated copies.
//
//go:embed .github/aw/connectors/*.md
var ConnectorResources embed.FS
