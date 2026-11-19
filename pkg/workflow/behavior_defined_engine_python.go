package workflow

import (
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const behaviorPackageManagerPip = "pip"

var (
	pythonRequirementName      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	pythonRequirementVersion   = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+!_-]*$`)
	pythonRequirementSHA256    = regexp.MustCompile(`^[0-9a-f]{64}$`)
	pythonPackageNameSeparator = regexp.MustCompile(`[-_.]+`)
	pythonPackagesEngineID     = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)
)

// normalizePythonPackageName applies the PEP 503 name normalization.
func normalizePythonPackageName(name string) string {
	return strings.ToLower(pythonPackageNameSeparator.ReplaceAllString(name, "-"))
}

// pythonPackagesRoot is mounted into the agent sandbox and is not uploaded as an artifact.
func pythonPackagesRoot(engineID string) string {
	return constants.TmpGhAwAgentDir + engineID + "-python"
}

func pythonPackagesSitePath(engineID string) string {
	return path.Join(pythonPackagesRoot(engineID), "site-packages")
}

func (e *BehaviorDefinedEngine) pipInstallation() *EngineInstallationDefinition {
	behavior := e.behavior()
	if behavior == nil || behavior.Installation == nil || behavior.Installation.PackageManager != behaviorPackageManagerPip ||
		len(behavior.Installation.Requirements) == 0 {
		return nil
	}
	return behavior.Installation
}

func pipInstallationVersion(def *EngineDefinition, install *EngineInstallationDefinition) string {
	if install.Version != "" {
		return install.Version
	}
	return def.Version
}

// validatePipInstallation requires complete, exactly pinned, hash-locked
// requirements that include the engine package at the engine version.
// A pip installation without requirements remains metadata for engines that
// install themselves in custom steps.
func validatePipInstallation(def *EngineDefinition) error {
	install := def.Behaviors.Installation
	if install == nil || len(install.Requirements) == 0 {
		return nil
	}
	if install.PackageManager != behaviorPackageManagerPip {
		return errors.New("'requirements' requires package-manager 'pip'")
	}
	if !pythonPackagesEngineID.MatchString(def.ID) {
		return fmt.Errorf("engine id %q must contain only lowercase letters, digits, and hyphens to use pip installation", def.ID)
	}
	if install.PackageName == "" {
		return errors.New("pip installation requires 'package-name'")
	}
	version := pipInstallationVersion(def, install)
	if version == "" {
		return errors.New("pip installation requires 'version' or an engine version")
	}
	packageName := normalizePythonPackageName(install.PackageName)
	seen := make(map[string]struct{})
	foundPackage := false
	for index, requirement := range install.Requirements {
		switch {
		case !pythonRequirementName.MatchString(requirement.Name):
			return fmt.Errorf("pip requirements[%d].name %q is not a valid distribution name", index, requirement.Name)
		case !pythonRequirementVersion.MatchString(requirement.Version):
			return fmt.Errorf("pip requirements[%d] (%s).version %q must be an exact version", index, requirement.Name, requirement.Version)
		case !pythonRequirementSHA256.MatchString(requirement.SHA256):
			return fmt.Errorf("pip requirements[%d] (%s).sha256 must be 64 lowercase hex characters", index, requirement.Name)
		}
		name := normalizePythonPackageName(requirement.Name)
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("pip requirements[%d] duplicates package %q", index, requirement.Name)
		}
		seen[name] = struct{}{}
		if name == packageName {
			if requirement.Version != version {
				return fmt.Errorf("pip requirements pin %s==%s, but the engine version is %s", requirement.Name, requirement.Version, version)
			}
			foundPackage = true
		}
	}
	if !foundPackage {
		return fmt.Errorf("pip requirements must pin package %q at version %s", install.PackageName, version)
	}
	return nil
}

// pipRequirementsLines renders the requirements in pip's hash-checking format.
func pipRequirementsLines(requirements []EnginePythonRequirement) []string {
	lines := make([]string, 0, len(requirements))
	for _, requirement := range requirements {
		lines = append(lines, requirement.Name+"=="+requirement.Version+" --hash=sha256:"+requirement.SHA256)
	}
	return lines
}

const pipInstalledVersionCheck = `import os
import re
import sys
from importlib.metadata import distributions

normalize = lambda name: re.sub(r"[-_.]+", "-", name).lower()
name = os.environ["GH_AW_PYTHON_PACKAGE"]
expected = os.environ["GH_AW_PYTHON_PACKAGE_VERSION"]
found = [d.version for d in distributions(path=[os.environ["GH_AW_PYTHON_PACKAGES"]]) if normalize(d.metadata["Name"]) == normalize(name)]
if found != [expected]:
    sys.exit(f"Installed {name} versions {found} do not match the required version {expected}; update the hash-locked requirements with engine.version")
print(f"Verified {name}=={expected}")`

// buildPipInstallationSteps installs the hash-locked requirements before the
// agent sandbox starts. pip --require-hashes rejects unpinned or unhashed
// transitive dependencies, and binary-only installation prevents build hooks.
func (e *BehaviorDefinedEngine) buildPipInstallationSteps(install *EngineInstallationDefinition, workflowData *WorkflowData) []GitHubActionStep {
	version := pipInstallationVersion(e.definition, install)
	if workflowData != nil && workflowData.EngineConfig != nil && workflowData.EngineConfig.Version != "" {
		version = workflowData.EngineConfig.Version
	}
	stepName := install.StepName
	if stepName == "" {
		stepName = "Install " + e.definition.DisplayName + " Python packages"
	}
	root := pythonPackagesRoot(e.definition.ID)
	site := pythonPackagesSitePath(e.definition.ID)
	requirementsFile := path.Join(root, "requirements.txt")
	step := GitHubActionStep{
		"      - name: " + stepName,
		"        env:",
		"          GH_AW_PYTHON_PACKAGES: " + site,
		"          GH_AW_PYTHON_PACKAGE: " + install.PackageName,
		"          GH_AW_PYTHON_PACKAGE_VERSION: " + fmt.Sprintf("%q", version),
		"          GH_AW_PYTHON_REQUIREMENTS: |",
	}
	for _, line := range pipRequirementsLines(install.Requirements) {
		step = append(step, "            "+line)
	}
	step = append(step, "          GH_AW_PYTHON_VERSION_CHECK: |")
	for line := range strings.SplitSeq(pipInstalledVersionCheck, "\n") {
		step = append(step, strings.TrimRight("            "+line, " "))
	}
	step = append(step,
		"        run: |",
		"          set -euo pipefail",
		"          rm -rf "+root,
		"          mkdir -p "+site,
		`          printf '%s' "$GH_AW_PYTHON_REQUIREMENTS" > `+requirementsFile,
		"          python3 -I -m pip install --quiet --isolated --disable-pip-version-check --no-input --no-cache-dir \\",
		"            --only-binary=:all: --require-hashes \\",
		`            --target "$GH_AW_PYTHON_PACKAGES" --requirement `+requirementsFile,
		`          python3 -I -c "$GH_AW_PYTHON_VERSION_CHECK"`,
	)
	steps := []GitHubActionStep{step}
	if install.VerifyCommand != "" {
		verifyName := install.VerifyStepName
		if verifyName == "" {
			verifyName = "Verify " + e.definition.DisplayName + " installation"
		}
		steps = append(steps, GitHubActionStep{
			"      - name: " + verifyName,
			"        env:",
			"          GH_AW_PYTHON_PACKAGES: " + site,
			"        run: " + install.VerifyCommand,
		})
	}
	return steps
}
