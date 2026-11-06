//go:build !integration

// Tests for argument injection prevention: package and image names starting with '-'
// must be rejected before being passed to exec.Command calls (npm, pip, uv, docker).

package workflow

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRejectHyphenPrefixPackages tests the shared helper that guards against
// argument injection in all package validation paths.
func TestRejectHyphenPrefixPackages(t *testing.T) {
	tests := []struct {
		name        string
		packages    []string
		kind        string
		expectError bool
		errContains string
	}{
		{
			name:        "empty list is accepted",
			packages:    []string{},
			kind:        "npx",
			expectError: false,
		},
		{
			name:        "valid package names are accepted",
			packages:    []string{"express", "@scope/pkg", "requests==2.28.0"},
			kind:        "pip",
			expectError: false,
		},
		{
			name:        "single hyphen prefix is rejected",
			packages:    []string{"-exploit"},
			kind:        "npx",
			expectError: true,
			errContains: "must not start with '-'",
		},
		{
			name:        "double hyphen prefix is rejected",
			packages:    []string{"--privileged"},
			kind:        "uv",
			expectError: true,
			errContains: "must not start with '-'",
		},
		{
			name:        "mixed list with one invalid is rejected",
			packages:    []string{"valid-pkg", "-exploit"},
			kind:        "pip",
			expectError: true,
			errContains: "-exploit",
		},
		{
			name:        "package with version specifier starting with hyphen is rejected",
			packages:    []string{"-exploit==1.0"},
			kind:        "uv",
			expectError: true,
			errContains: "-exploit==1.0",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := rejectHyphenPrefixPackages(tt.packages, tt.kind)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for packages %v but got none", tt.packages)
					return
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for packages %v: %v", tt.packages, err)
				}
			}
		})
	}
}

// TestValidateDockerImage_RejectsHyphenPrefix tests that Docker image names starting
// with '-' are rejected without invoking docker. This is the primary injection path
// because image names are taken directly from frontmatter (not filtered by extractors).
func TestValidateDockerImage_RejectsHyphenPrefix(t *testing.T) {
	tests := []struct {
		name        string
		image       string
		errContains string
	}{
		{
			name:        "single hyphen prefix",
			image:       "-exploit",
			errContains: "must not start with '-'",
		},
		{
			name:        "double hyphen prefix",
			image:       "--privileged",
			errContains: "must not start with '-'",
		},
		{
			name:        "looks like a docker flag",
			image:       "-rm",
			errContains: "must not start with '-'",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateDockerImage(tt.image, false, false)
			if err == nil {
				t.Errorf("expected error for image %q but got none", tt.image)
				return
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
			}
		})
	}
}

// TestExtractNpxFromCommands_HyphenPrefixFiltered shows that the npx package
// extractor already filters out names starting with '-' (treating them as flags),
// so they never reach the validation guard.
func TestExtractNpxFromCommands_HyphenPrefixFiltered(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		wantLen  int
	}{
		{
			name:     "lone hyphen-prefix arg yields no packages",
			commands: "npx -exploit",
			wantLen:  0,
		},
		{
			name:     "double-hyphen arg yields no packages",
			commands: "npx --exploit",
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := extractNpxFromCommands(tt.commands)
			if len(packages) != tt.wantLen {
				t.Errorf("expected %d packages, got %d: %v", tt.wantLen, len(packages), packages)
			}
		})
	}
}

// TestExtractPipFromCommands_HyphenPrefixFiltered shows that the pip package
// extractor already filters out names starting with '-', so they never reach
// the validation guard.
func TestExtractPipFromCommands_HyphenPrefixFiltered(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		wantLen  int
	}{
		{
			name:     "hyphen-prefix pip package yields no packages",
			commands: "pip install -exploit",
			wantLen:  0,
		},
		{
			name:     "double-hyphen pip package yields no packages",
			commands: "pip install --exploit",
			wantLen:  0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			packages := extractPipFromCommands(tt.commands)
			if len(packages) != tt.wantLen {
				t.Errorf("expected %d packages, got %d: %v", tt.wantLen, len(packages), packages)
			}
		})
	}
}

// TestCollectPackagesFromWorkflow_HyphenPrefixInArgs verifies that the MCP tool
// args extractor also filters out names starting with '-', providing an additional
// layer of defense for the structured args format.
func TestCollectPackagesFromWorkflow_HyphenPrefixInArgs(t *testing.T) {
	workflowData := &WorkflowData{
		Tools: map[string]any{
			"test-tool": map[string]any{
				"command": "npx",
				"args":    []any{"-exploit", "safe-package"},
			},
		},
	}

	packages := collectPackagesFromWorkflow(workflowData, extractNpxFromCommands, "npx")

	for _, pkg := range packages {
		if strings.HasPrefix(pkg, "-") {
			t.Errorf("package starting with '-' should not be collected: %s", pkg)
		}
	}
}

// TestValidateNpmPackageName tests that npm package names are validated against
// the npm naming rules, preventing argument injection via names that aren't caught
// by the hyphen-prefix check alone.
func TestValidateNpmPackageName(t *testing.T) {
	tests := []struct {
		name        string
		pkg         string
		expectError bool
		errContains string
	}{
		{
			name:        "simple valid name",
			pkg:         "express",
			expectError: false,
		},
		{
			name:        "scoped valid name",
			pkg:         "@scope/pkg",
			expectError: false,
		},
		{
			name:        "name with hyphen",
			pkg:         "my-package",
			expectError: false,
		},
		{
			name:        "name with dot",
			pkg:         "my.package",
			expectError: false,
		},
		{
			name:        "single character name",
			pkg:         "x",
			expectError: false,
		},
		{
			name:        "scoped name with version suffix",
			pkg:         "@sentry/mcp-server@0.29.0",
			expectError: false,
		},
		{
			name:        "unscoped name with version suffix",
			pkg:         "express@4.18.2",
			expectError: false,
		},
		{
			name:        "name with semver caret range",
			pkg:         "express@^4.0.0",
			expectError: false,
		},
		{
			name:        "name with tag version",
			pkg:         "express@latest",
			expectError: false,
		},
		{
			name:        "name starting with hyphen is rejected",
			pkg:         "--registry=https://attacker.example",
			expectError: true,
			errContains: "npm names must be lowercase alphanumeric and may include hyphens, dots, and underscores (e.g. \"my-package\" or \"@scope/name\")",
		},
		{
			name:        "name with equals sign is rejected",
			pkg:         "pkg=https://attacker.example",
			expectError: true,
			errContains: "npm names must be lowercase alphanumeric and may include hyphens, dots, and underscores (e.g. \"my-package\" or \"@scope/name\")",
		},
		{
			name:        "name with spaces is rejected",
			pkg:         "my package",
			expectError: true,
			errContains: "npm names must be lowercase alphanumeric and may include hyphens, dots, and underscores (e.g. \"my-package\" or \"@scope/name\")",
		},
		{
			name:        "uppercase name is rejected",
			pkg:         "MyPackage",
			expectError: true,
			errContains: "npm names must be lowercase alphanumeric and may include hyphens, dots, and underscores (e.g. \"my-package\" or \"@scope/name\")",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNpmPackageName(tt.pkg)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for package %q but got none", tt.pkg)
					return
				}
				expectedPrefix := fmt.Sprintf("invalid npm package name: %q", tt.pkg)
				if !strings.Contains(err.Error(), expectedPrefix) {
					t.Errorf("expected error to contain %q, got: %v", expectedPrefix, err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for package %q: %v", tt.pkg, err)
				}
			}
		})
	}
}

// TestValidatePipPackageName tests that PyPI package names are validated against
// PEP 508 naming rules, preventing argument injection via names not caught by
// the hyphen-prefix check alone.
func TestValidatePipPackageName(t *testing.T) {
	tests := []struct {
		name        string
		pkg         string
		expectError bool
		errContains string
	}{
		{
			name:        "simple valid name",
			pkg:         "requests",
			expectError: false,
		},
		{
			name:        "name with hyphen",
			pkg:         "my-package",
			expectError: false,
		},
		{
			name:        "name with underscore",
			pkg:         "my_package",
			expectError: false,
		},
		{
			name:        "name with dot",
			pkg:         "my.package",
			expectError: false,
		},
		{
			name:        "mixed case is valid per PEP 508",
			pkg:         "MyPackage",
			expectError: false,
		},
		{
			name:        "single character name",
			pkg:         "z",
			expectError: false,
		},
		{
			name:        "name starting with hyphen is rejected",
			pkg:         "--index-url=https://attacker.example",
			expectError: true,
			errContains: "PyPI names must start and end with a letter or digit, with hyphens, underscores, or dots allowed inside (e.g. \"requests\" or \"my-package\")",
		},
		{
			name:        "name with equals sign is rejected",
			pkg:         "pkg=https://attacker.example",
			expectError: true,
			errContains: "PyPI names must start and end with a letter or digit, with hyphens, underscores, or dots allowed inside (e.g. \"requests\" or \"my-package\")",
		},
		{
			name:        "name with spaces is rejected",
			pkg:         "my package",
			expectError: true,
			errContains: "PyPI names must start and end with a letter or digit, with hyphens, underscores, or dots allowed inside (e.g. \"requests\" or \"my-package\")",
		},
		{
			name:        "shell separator is rejected",
			pkg:         "pkg;whoami",
			expectError: true,
		},
		{
			name:        "command substitution is rejected",
			pkg:         "pkg$(whoami)",
			expectError: true,
		},
		{
			name:        "newline option injection is rejected",
			pkg:         "pkg\n--index-url",
			expectError: true,
		},
		{
			name:        "empty name is rejected",
			pkg:         "",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipPackageName(tt.pkg)
			if tt.expectError {
				if err == nil {
					t.Errorf("expected error for package %q but got none", tt.pkg)
					return
				}
				expectedPrefix := fmt.Sprintf("invalid pip package name: %q", tt.pkg)
				if !strings.Contains(err.Error(), expectedPrefix) {
					t.Errorf("expected error to contain %q, got: %v", expectedPrefix, err)
				}
				if tt.errContains != "" && !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error to contain %q, got: %v", tt.errContains, err)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error for package %q: %v", tt.pkg, err)
				}
			}

		})
	}
}

func TestValidatePipCommandPackageArg(t *testing.T) {
	tests := []struct {
		name        string
		pkg         string
		expectError bool
	}{
		{name: "valid package", pkg: "requests"},
		{name: "rejects hyphen prefix", pkg: "--index-url", expectError: true},
		{name: "rejects short option", pkg: "-r", expectError: true},
		{name: "rejects option with value", pkg: "--index-url=https://example.com", expectError: true},
		{name: "rejects leading whitespace before option", pkg: " --index-url", expectError: true},
		{name: "rejects control characters", pkg: "pkg\nname", expectError: true},
		{name: "rejects whitespace", pkg: "pkg name", expectError: true},
		{name: "rejects shell separators", pkg: "pkg;whoami", expectError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validatePipCommandPackageArg(tt.pkg)
			if tt.expectError && err == nil {
				t.Fatalf("expected error for package %q", tt.pkg)
			}
			if !tt.expectError && err != nil {
				t.Fatalf("unexpected error for package %q: %v", tt.pkg, err)
			}
		})
	}
}

func TestValidatePythonPackagesWithPip_SkipsInvalidPackageBeforeExec(t *testing.T) {
	compiler := NewCompiler()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "pip-args.txt")
	fakePip := filepath.Join(tempDir, "pip")

	script := `#!/bin/sh
printf "%s\n" "$@" >> "$GH_AW_TEST_PIP_ARGS_FILE"
`
	if err := os.WriteFile(fakePip, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pip executable: %v", err)
	}
	t.Setenv("GH_AW_TEST_PIP_ARGS_FILE", argsFile)

	compiler.validatePythonPackagesWithPip([]string{"--index-url"}, "pip", fakePip)

	if _, err := os.Stat(argsFile); err == nil {
		t.Fatalf("expected invalid package to be rejected before exec; args file %q was created", argsFile)
	} else if !os.IsNotExist(err) {
		t.Fatalf("failed to stat recorded pip arguments file: %v", err)
	}
}

func TestValidatePythonPackagesWithPip_SkipsInvalidExecutablePath(t *testing.T) {
	compiler := NewCompiler()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "pip-args.txt")
	fakePip := filepath.Join(tempDir, "pip")
	script := `#!/bin/sh
printf "%s\n" "$@" >> "$GH_AW_TEST_PIP_ARGS_FILE"
`
	if err := os.WriteFile(fakePip, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pip executable: %v", err)
	}
	t.Setenv("PATH", tempDir)
	t.Setenv("GH_AW_TEST_PIP_ARGS_FILE", argsFile)

	compiler.validatePythonPackagesWithPip([]string{"requests"}, "pip", "pip")

	if _, err := os.Stat(argsFile); !os.IsNotExist(err) {
		t.Fatalf("expected invalid executable path to be rejected before exec; stat error = %v", err)
	}
}

func TestValidatePythonPackagesWithPip_ExecutesValidatedPackage(t *testing.T) {
	compiler := NewCompiler()
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "pip-args.txt")
	fakePip := filepath.Join(tempDir, "pip")

	script := `#!/bin/sh
printf "%s\n" "$@" >> "$GH_AW_TEST_PIP_ARGS_FILE"
`
	if err := os.WriteFile(fakePip, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake pip executable: %v", err)
	}
	t.Setenv("GH_AW_TEST_PIP_ARGS_FILE", argsFile)

	compiler.validatePythonPackagesWithPip([]string{"requests"}, "pip", fakePip)

	if _, err := os.Stat(argsFile); err != nil {
		if os.IsNotExist(err) {
			t.Fatalf("expected pip to be invoked for valid package; args file %q was not created", argsFile)
		}
		t.Fatalf("failed to stat recorded pip arguments file: %v", err)
	}

	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read recorded pip arguments: %v", err)
	}
	args := string(argsData)

	// validatePythonPackagesWithPip executes: pip index versions <package> --pre
	gotArgs := strings.Split(strings.TrimSpace(args), "\n")
	wantArgs := []string{"index", "versions", "requests", "--pre"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("expected one pip invocation with %d args, got %d args: %q", len(wantArgs), len(gotArgs), args)
	}
	for i, want := range wantArgs {
		if gotArgs[i] != want {
			t.Fatalf("unexpected pip args at position %d: got %q, want %q (full args: %q)", i, gotArgs[i], want, args)
		}
	}
}

func TestValidateUvPackages_ExecutesFixedArguments(t *testing.T) {
	tempDir := t.TempDir()
	argsFile := filepath.Join(tempDir, "uv-args.txt")
	uvPath := filepath.Join(tempDir, "uv")
	script := `#!/bin/sh
printf "%s\n" "$@" > "$GH_AW_TEST_UV_ARGS_FILE"
`
	if err := os.WriteFile(uvPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake uv executable: %v", err)
	}
	t.Setenv("PATH", tempDir)
	t.Setenv("GH_AW_TEST_UV_ARGS_FILE", argsFile)

	if err := NewCompiler().validateUvPackages(&WorkflowData{CustomSteps: "uvx requests"}); err != nil {
		t.Fatalf("validateUvPackages returned an error: %v", err)
	}

	argsData, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("failed to read recorded uv arguments: %v", err)
	}
	gotArgs := strings.Split(strings.TrimSpace(string(argsData)), "\n")
	wantArgs := []string{"pip", "show", "requests", "--no-cache"}
	if len(gotArgs) != len(wantArgs) {
		t.Fatalf("expected %d fixed uv arguments, got %d: %q", len(wantArgs), len(gotArgs), gotArgs)
	}
	for i, want := range wantArgs {
		if gotArgs[i] != want {
			t.Fatalf("unexpected uv args at position %d: got %q, want %q (all args: %q)", i, gotArgs[i], want, gotArgs)
		}
	}
}

// TestValidateUvPackages_RejectsInvalidPackageName verifies that uv package names
// which do not conform to the PyPI naming rules are rejected before being passed
// as arguments to the uv CLI. This validation happens before uv/pip is resolved
// or invoked, so the test is deterministic regardless of whether uv is installed
// on the host running the test.
func TestValidateUvPackages_RejectsInvalidPackageName(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateUvPackages(&WorkflowData{
		CustomSteps: "uvx pkg;whoami",
	})
	if err == nil {
		t.Fatal("expected error for invalid uv package name but got none")
	}
	if !strings.Contains(err.Error(), "invalid pip package name") {
		t.Errorf("expected error to mention invalid package name, got: %v", err)
	}
}

// TestValidateUvPackages_AcceptsVersionedUvxSpec verifies that versioned uvx
// package specs (e.g. "ruff@0.1.0") are not rejected by the PEP 508 name
// validation, since extractUvFromCommands explicitly supports this syntax.
func TestValidateUvPackages_AcceptsVersionedUvxSpec(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateUvPackages(&WorkflowData{
		CustomSteps: "uvx ruff@0.1.0",
	})
	if err != nil && strings.Contains(err.Error(), "invalid pip package name") {
		t.Errorf("expected versioned uvx spec to be accepted as a valid package name, got: %v", err)
	}
}
