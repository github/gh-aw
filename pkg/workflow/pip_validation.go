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
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var pipValidationLog = logger.New("workflow:pip_validation")

// validatePythonPackagesWithPip is a generic helper that validates Python packages using pip index.
// It accepts a package list, package type name for error messaging, and pip command to use.
func (c *Compiler) validatePythonPackagesWithPip(packages []string, packageType string, pipCmd string) {
	pipValidationLog.Printf("Validating %d %s packages using %s", len(packages), packageType, pipCmd)

	for _, pkg := range packages {
		// Extract package name without version specifier
		pkgName := pkg
		if eqIndex := strings.Index(pkg, "=="); eqIndex > 0 {
			pkgName = pkg[:eqIndex]
		}

		pipValidationLog.Printf("Validating %s package: %s", packageType, pkgName)

		// Use pip index to check if package exists on PyPI
		// Include --pre flag to check for pre-release versions (alpha, beta, rc)
		cmd := exec.Command(pipCmd, "index", "versions", pkgName, "--pre")
		output, err := cmd.CombinedOutput()

		if err != nil {
			outputStr := strings.TrimSpace(string(output))
			pipValidationLog.Printf("Package validation failed for %s: %v", pkg, err)
			// Treat all pip validation errors as warnings, not compilation failures
			// The package may be experimental, not yet published, or will be installed at runtime
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s package '%s' validation failed - skipping verification. Package may or may not exist on PyPI.", packageType, pkg)))
			if c.verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("  Details: %s", outputStr)))
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
	pipCmd := "pip"
	_, err := exec.LookPath("pip")
	if err != nil {
		// Try pip3 as fallback
		_, err3 := exec.LookPath("pip3")
		if err3 != nil {
			pipValidationLog.Print("pip command not found, skipping validation")
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage("pip command not found - skipping pip package validation. Install Python/pip for full validation"))
			return nil
		}
		pipCmd = "pip3"
		pipValidationLog.Print("Using pip3 command for validation")
	}

	c.validatePythonPackagesWithPip(packages, "pip", pipCmd)
	return nil
}

// validateUvPackages validates that uv packages are available
func (c *Compiler) validateUvPackages(workflowData *WorkflowData) error {
	packages := extractUvPackages(workflowData)
	if len(packages) == 0 {
		pipValidationLog.Print("No uv packages to validate")
		return nil
	}

	pipValidationLog.Printf("Starting uv package validation for %d packages", len(packages))

	// Check if uv is available
	_, err := exec.LookPath("uv")
	if err != nil {
		pipValidationLog.Print("uv command not found, falling back to pip validation")
		// uv not available, but we can still validate using pip index
		pipCmd := "pip"
		_, pipErr := exec.LookPath("pip")
		if pipErr != nil {
			// Try pip3 as fallback
			_, pip3Err := exec.LookPath("pip3")
			if pip3Err != nil {
				pipValidationLog.Print("Neither uv nor pip commands found, cannot validate")
				return NewOperationError(
					"validate",
					"uv packages",
					"",
					pip3Err,
					"Install uv or pip to enable package validation:\n\nInstall uv (recommended):\n$ curl -LsSf https://astral.sh/uv/install.sh | sh\n\nOr install pip:\n$ python -m ensurepip --upgrade\n\nAlternatively, disable validation by setting GH_AW_SKIP_UV_VALIDATION=true",
				)
			}
			pipCmd = "pip3"
			pipValidationLog.Print("Using pip3 for validation")
		}

		return c.validateUvPackagesWithPip(packages, pipCmd)
	}

	pipValidationLog.Print("Using uv command for validation")

	// Validate with uv
	var errors []string
	for _, pkg := range packages {
		// Extract package name without version specifier
		pkgName := pkg
		if eqIndex := strings.Index(pkg, "=="); eqIndex > 0 {
			pkgName = pkg[:eqIndex]
		}

		// Use uv pip show to check if package exists on PyPI
		cmd := exec.Command("uv", "pip", "show", pkgName, "--no-cache")
		_, err := cmd.CombinedOutput()

		if err != nil {
			// Package not installed, try to check if it's available
			errors = append(errors, fmt.Sprintf("uv package '%s' validation requires network access or local cache", pkg))
		} else if c.verbose {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("✓ uv package validated: %s", pkg)))
		}
	}

	if len(errors) > 0 {
		return NewValidationError(
			"uv.packages",
			fmt.Sprintf("%d packages require validation", len(errors)),
			"uv package validation requires network access or local cache",
			fmt.Sprintf("Ensure network access or cache uv packages locally:\n\n%s\n\nCache packages:\n$ uv pip install <package-name> --no-cache\n\nOr connect to network for validation", strings.Join(errors, "\n")),
		)
	}

	return nil
}

// validateUvPackagesWithPip validates uv packages using pip index
func (c *Compiler) validateUvPackagesWithPip(packages []string, pipCmd string) error {
	c.validatePythonPackagesWithPip(packages, "uv", pipCmd)
	return nil
}
