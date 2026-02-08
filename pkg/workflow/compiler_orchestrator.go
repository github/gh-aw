// Package workflow implements workflow compilation orchestration.
//
// The compiler orchestrator is split into 5 focused modules for maintainability:
//
//   - orchestrator.go: Shared logger and constants used across orchestrator modules
//   - orchestrator_engine.go: Engine detection, validation, and setup logic
//   - orchestrator_frontmatter.go: Frontmatter parsing and validation
//   - orchestrator_tools.go: Tool configuration and MCP server setup
//   - orchestrator_workflow.go: Main workflow orchestration and YAML generation
//
// The orchestrator follows a phased approach with typed result structures
// for clear data flow between compilation stages. Each module handles a specific
// concern in the compilation pipeline, making the codebase easier to understand
// and maintain.
package workflow

import (
	"github.com/github/gh-aw/pkg/logger"
)

// Shared logger used across compiler orchestrator modules
var detectionLog = logger.New("workflow:detection")
