package workflow

import (
	"fmt"
)

const copilotSDKNpmCICommand = `npm ci --ignore-scripts --no-audit --no-fund`

// Redirect only SDK-owned imports, retaining the driver's normal resolution for
// relative imports and repository dependencies. Older Node versions need a
// separate CommonJS resolver alongside the asynchronous ESM hook.
const copilotSDKNpmResolver = `const { register, registerHooks, createRequire } = require("node:module");` +
	`const { pathToFileURL } = require("node:url");` +
	`const sdkParentURL = pathToFileURL(__filename).href;` +
	`const packages = Object.keys(require("./package.json").dependencies);` +
	`function resolve(specifier, context, nextResolve) {` +
	`if (context.parentURL !== sdkParentURL && packages.some(pkg => specifier === pkg || specifier.startsWith(pkg + "/"))) {` +
	`try { return { url: pathToFileURL(createRequire(sdkParentURL).resolve(specifier)).href, shortCircuit: true }; }` +
	`catch (error) { if (error.code !== "MODULE_NOT_FOUND" && error.code !== "ERR_MODULE_NOT_FOUND") throw error; } }` +
	`return nextResolve(specifier, context); }` +
	`if (registerHooks) { registerHooks({ resolve }); } else {` +
	`const Module = require("node:module"); const originalResolve = Module._resolveFilename;` +
	`Module._resolveFilename = function(specifier, parent, ...args) {` +
	`if (parent?.filename !== __filename && packages.some(pkg => specifier === pkg || specifier.startsWith(pkg + "/"))) {` +
	`try { return createRequire(sdkParentURL).resolve(specifier); }` +
	`catch (error) { if (error.code !== "MODULE_NOT_FOUND") throw error; } }` +
	`return originalResolve.call(this, specifier, parent, ...args); };` +
	`register("data:text/javascript," + encodeURIComponent(` +
	`'import { createRequire } from "node:module"; import { pathToFileURL } from "node:url";' +` +
	`"const packages = " + JSON.stringify(packages) + ";" +` +
	`"const sdkParentURL = " + JSON.stringify(sdkParentURL) + "; export " + resolve.toString())); }`

func copilotSDKNpmInstallLines(runtimeID string) []string {
	profile := "node"
	if runtimeID == "typescript" {
		profile = "typescript"
	}
	lines := []string{
		`mkdir -p "${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk"`,
		`cd "${GITHUB_WORKSPACE}/.gh-aw/copilot-sdk"`,
	}
	for _, name := range []string{"package.json", "package-lock.json"} {
		lines = append(lines, fmt.Sprintf("cp -- \"%s/copilot-sdk/%s/%s\" %s", SetupActionDestinationShell, profile, name, name))
	}
	return append(lines,
		fmt.Sprintf("printf '%%s\\n' %s > resolve.cjs", shellEscapeArg(copilotSDKNpmResolver)),
		copilotSDKNpmCICommand,
	)
}

func useLockedCopilotSDKNpmInstall(spec copilotSDKInstallSpec) copilotSDKInstallSpec {
	if spec.runtimeID == "node" || spec.runtimeID == "typescript" || (spec.command == "" && len(spec.runLines) == 0) {
		spec.command = copilotSDKNpmCICommand
		spec.runLines = copilotSDKNpmInstallLines(spec.runtimeID)
	}
	return spec
}
