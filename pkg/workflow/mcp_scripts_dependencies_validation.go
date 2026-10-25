//go:build !js && !wasm

package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/semverutil"
)

var (
	goModuleNameRE               = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
	shellPackageDependencyNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9+.-]*([:=][A-Za-z0-9.+~:-]+)?$`)
)

var mcpScriptsDepsValidationLog = logger.New("workflow:mcp_scripts_dependencies_validation")

func (c *Compiler) validateMCPScriptDependencies(workflowData *WorkflowData) error {
	if workflowData == nil || workflowData.MCPScripts == nil {
		return nil
	}

	mcpScriptsDepsValidationLog.Printf("Validating MCP script dependencies for %d tool(s)", len(workflowData.MCPScripts.Tools))

	for toolName, tool := range workflowData.MCPScripts.Tools {
		if len(tool.Dependencies) == 0 {
			continue
		}

		manager := inferMCPScriptDependencyManager(tool)
		if manager == "" {
			mcpScriptsDepsValidationLog.Printf("Tool %q: no dependency manager inferred, skipping %d dependencies", toolName, len(tool.Dependencies))
			continue
		}

		mcpScriptsDepsValidationLog.Printf("Tool %q: validating %d %q dependencies", toolName, len(tool.Dependencies), manager)

		for _, dependency := range tool.Dependencies {
			dependency = strings.TrimSpace(dependency)
			if dependency == "" {
				continue
			}
			if err := validateMCPScriptDependencyName(toolName, manager, dependency); err != nil {
				return err
			}
		}
	}

	return nil
}

func inferMCPScriptDependencyManager(tool *MCPScriptToolConfig) string {
	switch {
	case tool.Script != "":
		return "npm"
	case tool.Py != "":
		return "pip"
	case tool.Go != "":
		return "go"
	case tool.Run != "":
		return "apt"
	default:
		return ""
	}
}

func validateMCPScriptDependencyName(toolName, manager, dependency string) error {
	switch manager {
	case "npm":
		return validateMCPScriptNpmDependency(toolName, dependency)
	case "pip":
		return validateMCPScriptPipDependency(toolName, dependency)
	case "go":
		return validateMCPScriptGoDependency(toolName, dependency)
	case "apt":
		if !shellPackageDependencyNameRE.MatchString(dependency) {
			return newInvalidDependencyNameError(toolName, dependency)
		}
		hasVersionPin := strings.Contains(dependency, "=") || strings.Contains(dependency, ":")
		if !hasVersionPin {
			return newUnpinnedDependencyError(toolName, dependency, "shell", "name=1.2.3", "jq=1.6-2.1")
		}
	}
	return nil
}

func validateMCPScriptNpmDependency(toolName, dependency string) error {
	name, version, err := splitNpmDependency(dependency)
	if err != nil {
		if nameErr := validateNpmPackageName(dependency); nameErr != nil {
			return newInvalidDependencyNameError(toolName, dependency)
		}
		return newUnpinnedDependencyError(toolName, dependency, "npm", "name@1.2.3", "@scope/name@1.2.3")
	}
	if err := validateNpmPackageName(name); err != nil {
		return newInvalidDependencyNameError(toolName, dependency)
	}
	if !semverutil.IsValid(version) {
		return newUnpinnedDependencyError(toolName, dependency, "npm", "name@1.2.3", "@scope/name@1.2.3")
	}
	return nil
}

func validateMCPScriptPipDependency(toolName, dependency string) error {
	name := dependency
	if idx := strings.IndexAny(name, "=<>!~"); idx > 0 {
		name = name[:idx]
	}
	name = strings.TrimSpace(name)
	if err := validatePipPackageName(name); err != nil {
		return newInvalidDependencyNameError(toolName, dependency)
	}
	_, version, found := strings.Cut(dependency, "==")
	if !found {
		return newUnpinnedDependencyError(toolName, dependency, "pip", "name==1.2.3", "requests==2.32.3")
	}
	version = strings.TrimSpace(version)
	if version == "" {
		return newUnpinnedDependencyError(toolName, dependency, "pip", "name==1.2.3", "requests==2.32.3")
	}
	if !semverutil.IsValid(version) {
		return newUnpinnedDependencyError(toolName, dependency, "pip", "name==1.2.3", "requests==2.32.3")
	}
	return nil
}

func validateMCPScriptGoDependency(toolName, dependency string) error {
	moduleName, version, found := strings.Cut(dependency, "@")
	if !found {
		if !goModuleNameRE.MatchString(strings.TrimSpace(dependency)) {
			return newInvalidDependencyNameError(toolName, dependency)
		}
		return newUnpinnedDependencyError(toolName, dependency, "go", "module@v1.2.3", "github.com/google/uuid@v1.6.0")
	}
	moduleName = strings.TrimSpace(moduleName)
	version = strings.TrimSpace(version)
	if moduleName == "" || version == "" {
		return newUnpinnedDependencyError(toolName, dependency, "go", "module@v1.2.3", "github.com/google/uuid@v1.6.0")
	}
	if !goModuleNameRE.MatchString(moduleName) {
		return newInvalidDependencyNameError(toolName, dependency)
	}
	if !strings.HasPrefix(version, "v") || !semverutil.IsValid(version) {
		return newUnpinnedDependencyError(toolName, dependency, "go", "module@v1.2.3", "github.com/google/uuid@v1.6.0")
	}
	return nil
}

func splitNpmDependency(dependency string) (string, string, error) {
	atIndex := strings.LastIndex(dependency, "@")
	if atIndex <= 0 || atIndex == len(dependency)-1 {
		return "", "", errors.New("missing npm package version")
	}
	name := strings.TrimSpace(dependency[:atIndex])
	version := strings.TrimSpace(dependency[atIndex+1:])
	if name == "" || version == "" {
		return "", "", errors.New("missing npm package version")
	}
	return name, version, nil
}

func newInvalidDependencyNameError(toolName, dependency string) error {
	return fmt.Errorf(
		"invalid dependency name %q for tool %q. Expected a valid package name for the inferred package manager. Example: dependencies: [requests]",
		dependency,
		toolName,
	)
}

func newUnpinnedDependencyError(toolName, dependency, manager, expected, example string) error {
	return fmt.Errorf(
		"dependency %q for tool %q is not pinned to a release tag. Expected %s dependency format %q with an exact version. Example: dependencies: [%q]",
		dependency,
		toolName,
		manager,
		expected,
		example,
	)
}
