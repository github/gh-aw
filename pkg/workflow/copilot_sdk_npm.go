package workflow

import (
	"fmt"
)

const copilotSDKNpmCICommand = `npm ci --ignore-scripts --no-audit --no-fund`
const copilotSDKNpmResolverFile = "copilot_sdk_resolver.cjs"

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
		fmt.Sprintf("cp -- \"%s/%s\" resolve.cjs", SetupActionDestinationShell, copilotSDKNpmResolverFile),
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
