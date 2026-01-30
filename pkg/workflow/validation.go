// This file provides validation functions for agentic workflow compilation.
//
// # Validation Architecture
//
// The validation system for agentic workflows is organized into focused,
// domain-specific files to maintain clarity and single responsibility:
//
//   - validation.go: This file - package documentation only
//   - strict_mode_validation.go: Security and strict mode validation
//   - repository_features_validation.go: Repository capability detection
//   - schema_validation.go: GitHub Actions schema validation
//   - runtime_validation.go: Runtime packages, containers, expressions
//   - agent_validation.go: Agent files and feature support
//   - pip_validation.go: Python package validation
//   - npm_validation.go: NPM package validation
//   - docker_validation.go: Docker image validation
//   - expression_safety.go: GitHub Actions expression security
//   - engine_validation.go: AI engine configuration validation
//   - mcp_config_validation.go: MCP server configuration validation
//   - template_validation.go: Template structure validation
//   - firewall_validation.go: Firewall log-level validation
//   - gateway_validation.go: Gateway port validation
//   - sandbox_validation.go: Sandbox and mounts validation
//   - bundler_safety_validation.go: JavaScript bundle safety (require/module checks)
//   - bundler_script_validation.go: JavaScript script content (execSync, GitHub globals)
//   - bundler_runtime_validation.go: JavaScript runtime mode compatibility
//
// # Pass-Through Field Validation
//
// Several workflow frontmatter fields are "pass-through" fields - they are extracted
// from frontmatter and passed directly to GitHub Actions without modification:
//   - concurrency: Workflow concurrency control
//   - container: Container configuration for jobs
//   - environment: GitHub environment configuration
//   - env: Environment variables
//   - runs-on: Runner selection
//   - services: Service containers
//
// These fields ARE validated during frontmatter parsing using JSON Schema validation
// (see pkg/parser/schemas/main_workflow_schema.json). The schema catches:
//   - Invalid data types (e.g., array when object expected)
//   - Missing required properties (e.g., container missing 'image')
//   - Invalid additional properties (e.g., unknown fields in concurrency)
//   - Structure violations (e.g., runs-on as number instead of string/array/object)
//
// Schema validation happens in pkg/parser/schema_validation.go during frontmatter
// parsing, so errors are caught at compile time rather than GitHub Actions runtime.
// See pkg/parser/schema_passthrough_validation_test.go for comprehensive test coverage.
//
// # When to Add New Validation
//
// Add validation to existing domain files when:
//   - It fits the domain (e.g., package validation → pip_validation.go)
//   - It extends existing functionality
//
// Create a new validation file when:
//   - It represents a distinct validation domain
//   - It has multiple related validation functions
//   - It requires its own caching or state management
//
// # Validation Patterns
//
// The validation system uses several patterns:
//   - Schema validation: JSON schema validation with caching
//   - External resource validation: Docker images, npm/pip packages
//   - Size limit validation: Expression sizes, file sizes
//   - Feature detection: Repository capabilities
//   - Security validation: Permission restrictions, expression safety

package workflow
