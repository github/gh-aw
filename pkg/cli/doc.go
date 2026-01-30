// Package cli provides the command-line interface for gh-aw (GitHub Agentic Workflows).
//
// This package implements the `gh aw` CLI extension using the Cobra command framework.
// It provides commands for compiling workflows, running audits, and inspecting MCP
// servers. Each command follows consistent patterns for error handling, output
// formatting, and user interaction.
//
// # Available Commands
//
// compile - Convert markdown workflow files to GitHub Actions YAML
//
// add - Interactively add workflows to repositories
//
// audit - Analyze workflow runs for issues and generate reports
//
// logs - Download and inspect GitHub Actions workflow logs
//
// mcp - Manage and inspect MCP (Model Context Protocol) servers
//
// actions - Build and manage custom GitHub Actions
//
// # Basic Usage
//
//	// Compile a single workflow
//	err := cli.RunCompile(cli.CompileConfig{
//		WorkflowFiles: []string{"workflow.md"},
//		Verbose:      false,
//	})
//
//	// Audit a workflow run
//	err := cli.RunAuditWorkflowRun(cli.AuditConfig{
//		Owner:  "owner",
//		Repo:   "repo",
//		RunID:  123456,
//	})
//
// # Command Structure
//
// Each command follows a consistent pattern:
//  1. Command definition in *_command.go files
//  2. Runnable function (RunX) for testability
//  3. Input validation with helpful error messages
//  4. Console-formatted output using pkg/console
//  5. Comprehensive help text with examples
//
// Commands use standard flags:
//
//	--verbose/-v    Enable detailed output
//	--output/-o     Specify output directory
//	--json/-j       Output results in JSON format
//	--engine/-e     Override AI engine selection
//
// # Output Formatting
//
// All CLI output uses the console package for consistent formatting:
//   - Success messages in green
//   - Errors in red with actionable suggestions
//   - Warnings in yellow
//   - Info messages in blue
//   - Progress indicators for long-running operations
//
// # Error Handling
//
// Commands follow these error handling conventions:
//   - Early validation of inputs with clear error messages
//   - Wrapped errors with context using fmt.Errorf
//   - Console-formatted error output to stderr
//   - Non-zero exit codes for failures
//
// # Related Packages
//
// pkg/workflow - Core compilation logic called by compile command
//
// pkg/console - Output formatting utilities
//
// pkg/logger - Debug logging controlled by DEBUG environment variable
package cli
