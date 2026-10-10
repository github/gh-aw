//go:build !js && !wasm

// This file provides Python package validation for agentic workflows.
//
// # Python Package Validation
//
// This file validates Python package availability on PyPI using pip and uv package managers.
// Validation ensures that Python packages specified in workflows exist and can be installed
// at runtime, preventing failures due to typos or non-existent packages.
//
// # Validation Functions
//
//   - validatePythonPackagesWithPip() - Generic pip validation helper
//   - validatePipPackages() - Validates pip packages from workflow configuration
//   - validateUvPackages() - Validates uv packages from workflow configuration
//   - validateUvPackagesWithPip() - Validates uv packages using pip index
//
// # Validation Pattern: Warning vs Error
//
// Python package validation uses a warning-based approach rather than hard errors:
//   - If pip validation fails, a warning is emitted but compilation continues
//   - This allows for experimental packages or packages not yet published
//   - Verbose mode provides detailed validation feedback
//
// # When to Add Validation Here
//
// Add validation to this file when:
//   - It validates Python/pip ecosystem packages
//   - It checks PyPI package existence
//   - It validates Python version compatibility
//   - It validates uv package manager packages
//
// For package extraction functions, see pip.go.
// For general validation, see validation.go.
// For detailed documentation, see scratchpad/validation-architecture.md

package workflow

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/logger"
)

var pipValidationLog = logger.New("workflow:pip_validation")

func validatePipCommandPackageArg(pkgName string) error {
	if strings.HasPrefix(pkgName, "-") {
		return errors.New("names must not start with '-'")
	}
	if strings.IndexFunc(pkgName, unicode.IsControl) >= 0 {
		return errors.New("names must not contain control characters")
	}
	return validatePipPackageName(pkgName)
}

// validatePythonPackagesWithPip is a generic helper that validates Python packages using pip index.
// It accepts a package list, package type name for error messaging, and a validated pip executable path.
func (c *Compiler) validatePythonPackagesWithPip(packages []string, packageType string, pipPath string) {
	pipValidationLog.Printf("Validating %d %s packages using %s", len(packages), packageType, pipPath)

	for _, pkg := range packages {
		// Extract package name without version specifier (pip-style "==version"
		// or uvx-style "@version", e.g. "ruff@0.1.0").
		pkgName := stripUvPackageVersion(pkg)

		// Validate the package name against PyPI naming rules (PEP 508).
		// pip does not universally honour '--', so we validate upfront.
		if err := validatePipCommandPackageArg(pkgName); err != nil {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s package name '%s' is invalid: %v", packageType, pkg, err)))
			continue
		}

		pipValidationLog.Printf("Validating %s package: %s", packageType, pkgName)

		// Use pip index to check if package exists on PyPI
		// Include --pre flag to check for pre-release versions (alpha, beta, rc)
		// #nosec G204 -- pipPath is resolved from the hardcoded executable names "pip" or "pip3"
		// via fileutil.ResolveExecutablePath; pkgName is
		// validated above by validatePipPackageName against the strict PyPI PEP 508 allowlist.
		cmd := exec.Command(pipPath, "index", "versions", pkgName, "--pre")
		output, err := cmd.CombinedOutput()

		if err != nil {
			outputStr := strings.TrimSpace(string(output))
			pipValidationLog.Printf("Package validation failed for %s: %v", pkg, err)
			// Treat all pip validation errors as warnings, not compilation failures
			// The package may be experimental, not yet published, or will be installed at runtime
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s package '%s' validation failed - skipping verification. Package may or may not exist on PyPI.", packageType, pkg)))
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage("  Details: "+outputStr))
			}
		} else {
			pipValidationLog.Printf("Package validated successfully: %s", pkg)
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("✓ %s package validated: %s", packageType, pkg)))
			}
		}
	}
}

// validatePipPackages validates that pip packages are available on PyPI
func (c *Compiler) validatePipPackages(workflowData *WorkflowData) error {
	packages := extractPipPackages(workflowData)
	if len(packages) == 0 {
		pipValidationLog.Print("No pip packages to validate")
		return nil
	}

	pipValidationLog.Printf("Starting pip package validation for %d packages", len(packages))

	// Check if pip is available
	pipPath, err := fileutil.ResolveExecutablePath("pip")
	if err != nil {
		// Try pip3 as fallback
		pipPath, err = fileutil.ResolveExecutablePath("pip3")
		if err != nil {
			pipValidationLog.Print("pip command not found, skipping validation")
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("pip command not found - skipping pip package validation. Install Python/pip for full validation"))
			return nil
		}
		pipValidationLog.Print("Using pip3 command for validation")
	}

	c.validatePythonPackagesWithPip(packages, "pip", pipPath)
	return nil
}

// stripUvPackageVersion extracts the bare package name from a uv package spec,
// stripping a trailing "==version" (pip-style) or "@version" (uvx-style, e.g.
// "ruff@0.1.0") specifier if present.
func stripUvPackageVersion(pkg string) string {
	if eqIndex := strings.Index(pkg, "=="); eqIndex > 0 {
		return pkg[:eqIndex]
	}
	if atIndex := strings.Index(pkg, "@"); atIndex > 0 {
		return pkg[:atIndex]
	}
	return pkg
}

// validateUvPackages validates that uv packages are available
func (c *Compiler) validateUvPackages(workflowData *WorkflowData) error {
	packages := extractUvPackages(workflowData)
	if len(packages) == 0 {
		pipValidationLog.Print("No uv packages to validate")
		return nil
	}

	pipValidationLog.Printf("Starting uv package validation for %d packages", len(packages))

	// Reject any package names starting with '-' before invoking uv or pip.
	// These would be interpreted as flags by the CLI tools (argument injection).
	if err := rejectHyphenPrefixPackages(packages, "uv"); err != nil {
		pipValidationLog.Printf("uv package name validation failed: %v", err)
		return err
	}

	// Validate package name syntax (PEP 508) upfront, before resolving or invoking
	// uv/pip and independent of whether those tools are installed. This ensures
	// argument-injection attempts (e.g. "pkg;whoami") are rejected even in
	// environments without uv or pip available.
	var invalidNameErrors []string
	for _, pkg := range packages {
		pkgName := stripUvPackageVersion(pkg)
		if err := validatePipCommandPackageArg(pkgName); err != nil {
			pipValidationLog.Printf("Invalid uv package name %s: %v", pkgName, err)
			invalidNameErrors = append(invalidNameErrors, fmt.Sprintf("uv package '%s' is invalid: %v", pkg, err))
		}
	}
	if len(invalidNameErrors) > 0 {
		return NewValidationError(
			"uv.packages",
			fmt.Sprintf("%d package name(s) invalid", len(invalidNameErrors)),
			"uv package name(s) do not conform to PyPI naming rules (PEP 508)",
			"Package names must start and end with a letter or digit, with hyphens, underscores, or dots allowed inside (e.g. \"requests\" or \"my-package\"). Optionally followed by a \"==version\" or \"@version\" specifier.\n\nValidation details:\n"+strings.Join(invalidNameErrors, "\n"),
		)
	}

	// Check if uv is available
	uvPath, err := fileutil.ResolveExecutablePath("uv")
	if err != nil {
		pipValidationLog.Print("uv command not found, falling back to pip validation")
		// uv not available, but we can still validate using pip index
		pipPath, pipErr := fileutil.ResolveExecutablePath("pip")
		if pipErr != nil {
			// Try pip3 as fallback
			var pip3Err error
			pipPath, pip3Err = fileutil.ResolveExecutablePath("pip3")
			if pip3Err != nil {
				pipValidationLog.Print("Neither uv nor pip commands found, cannot validate")
				combinedErr := errors.Join(
					fmt.Errorf("pip: %w", pipErr),
					fmt.Errorf("pip3: %w", pip3Err),
				)
				return NewOperationError(
					"validate",
					"uv packages",
					"",
					combinedErr,
					"Install uv or pip to enable package validation:\n\nInstall uv (recommended):\n$ curl -LsSf https://astral.sh/uv/install.sh | sh\n\nOr install pip:\n$ python -m ensurepip --upgrade\n\nAlternatively, disable validation by setting GH_AW_SKIP_UV_VALIDATION=true",
				)
			}
			pipValidationLog.Print("Using pip3 for validation")
		}

		return c.validateUvPackagesWithPip(packages, pipPath)
	}

	pipValidationLog.Print("Using uv command for validation")

	// Validate with uv
	// Package names were already validated against PyPI naming rules (PEP 508) above,
	// before this point. validatePipCommandPackageArg below is a final point-of-use
	// safety gate applied immediately before pkgName is passed to uv pip show.
	var errors []string
	for _, pkg := range packages {
		pkgName := stripUvPackageVersion(pkg)
		if err := validatePipCommandPackageArg(pkgName); err != nil {
			return NewValidationError(
				"uv.packages",
				fmt.Sprintf("invalid uv package name %q", pkg),
				"uv package name is unsafe to pass to the uv CLI",
				fmt.Sprintf("Rejected uv package %q before invoking uv pip show: %v", pkg, err),
			)
		}

		// Use uv pip show to check if package exists on PyPI
		// #nosec G204 -- uvPath is resolved from the hardcoded executable name "uv" via
		// fileutil.ResolveExecutablePath; pkgName is validated above by validatePipPackageName
		// against the strict PyPI PEP 508 allowlist.
		cmd := exec.Command(uvPath, "pip", "show", pkgName, "--no-cache")
		_, err := cmd.CombinedOutput()

		if err != nil {
			// Package not installed, try to check if it's available
			errors = append(errors, fmt.Sprintf("uv package '%s' validation requires network access or local cache", pkg))
		} else if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("✓ uv package validated: "+pkg))
		}
	}

	if len(errors) > 0 {
		return NewValidationError(
			"uv.packages",
			fmt.Sprintf("%d packages require validation", len(errors)),
			"uv package validation requires network access or local cache",
			"Ensure uv package validation can access package metadata.\n\nExample workflow configuration:\n\nnetwork:\n  allowed:\n    - pypi.org\n    - files.pythonhosted.org\n\ncustom-steps: |\n  uv pip install ruff\n\nValidation details:\n"+strings.Join(errors, "\n"),
		)
	}

	return nil
}

// validateUvPackagesWithPip validates uv packages using pip index
func (c *Compiler) validateUvPackagesWithPip(packages []string, pipPath string) error {
	c.validatePythonPackagesWithPip(packages, "uv", pipPath)
	return nil
}
