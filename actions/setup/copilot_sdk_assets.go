package setup

import _ "embed"

//go:embed copilot-sdk/node/package.json
var CopilotSDKNodeManifest string

//go:embed copilot-sdk/node/package-lock.json
var CopilotSDKNodeLock string

//go:embed copilot-sdk/typescript/package.json
var CopilotSDKTypeScriptManifest string

//go:embed copilot-sdk/typescript/package-lock.json
var CopilotSDKTypeScriptLock string
