// This file provides package and image name validation utilities for agentic workflows.
//
// # Name Validation
//
// Package and image names passed to external tools (npm, pip, uv, docker) must not
// start with '-'. A leading '-' would be interpreted as a command-line flag by the
// downstream tool, causing unintended argument injection.
//
// Note: exec.Command uses argv directly (not sh -c), so this is argument injection,
// not shell injection. The risk is low — compilation runs on the developer's local
// machine with the developer's own privileges.

package workflow

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var nameValidationLog = logger.New("workflow:name_validation")

// npmPackageNameRE matches valid npm package specifiers:
// - Scoped: @scope/name where scope and name are lowercase alphanumeric + hyphens + dots + underscores
// - Unscoped: lowercase alphanumeric + hyphens + dots + underscores
// - Optional version suffix: @version (e.g. @1.2.3, @^1.0.0, @latest)
// This rejects any name that could be interpreted as a CLI flag.
var npmPackageNameRE = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*(@[a-zA-Z0-9^~>=<.*|-]+)?$`)

// pypiPackageNameRE matches valid PyPI package names per PEP 508 / PEP 625:
// must start and end with alphanumeric; interior may include dots, hyphens, underscores.
var pypiPackageNameRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// validateNpmPackageName returns an error if the package name does not conform
// to the npm package naming rules. This prevents argument injection into the npm CLI.
func validateNpmPackageName(pkg string) error {
	nameValidationLog.Printf("Validating npm package name: %s", pkg)
	if !npmPackageNameRE.MatchString(pkg) {
		nameValidationLog.Printf("Invalid npm package name: %s", pkg)
		return fmt.Errorf("invalid npm package name: %q — npm names must be lowercase alphanumeric and may include hyphens, dots, and underscores (e.g. \"my-package\" or \"@scope/name\")", pkg)
	}
	return nil
}

// validatePipPackageName returns an error if the package name does not conform
// to the PyPI naming rules (PEP 508). This prevents argument injection into pip/uv.
func validatePipPackageName(pkgName string) error {
	nameValidationLog.Printf("Validating pip package name: %s", pkgName)
	if !pypiPackageNameRE.MatchString(pkgName) {
		nameValidationLog.Printf("Invalid pip package name: %s", pkgName)
		return fmt.Errorf("invalid pip package name: %q — PyPI names must start and end with a letter or digit, with hyphens, underscores, or dots allowed inside (e.g. \"requests\" or \"my-package\")", pkgName)
	}
	return nil
}

// rejectHyphenPrefixPackages returns a ValidationError if any of the provided
// names starts with '-'. The kind parameter (e.g. "npx", "pip", "uv") is used
// in the error messages.
//
// Names starting with '-' would be interpreted as flags by the downstream CLI
// tool, constituting argument injection into the exec.Command call.
func rejectHyphenPrefixPackages(names []string, kind string) error {
	nameValidationLog.Printf("Checking %d %s package names for hyphen prefix", len(names), kind)
	var invalid []string
	for _, name := range names {
		if strings.HasPrefix(name, "-") {
			invalid = append(invalid, fmt.Sprintf("%s package name '%s' is invalid: names must not start with '-'", kind, name))
		}
	}
	if len(invalid) == 0 {
		return nil
	}
	nameValidationLog.Printf("Found %d invalid %s package names with hyphen prefix", len(invalid), kind)
	return NewValidationError(
		kind+".packages",
		fmt.Sprintf("%d invalid package names", len(invalid)),
		kind+" package names must not start with '-'",
		"Fix invalid package names:\n\n"+strings.Join(invalid, "\n"),
	)
}
