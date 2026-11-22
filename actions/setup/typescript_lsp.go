package setup

import "embed"

// TypeScriptLSPProject is the locked npm project staged by setup.sh.
//
//go:embed typescript-lsp/package.json typescript-lsp/package-lock.json
var TypeScriptLSPProject embed.FS
