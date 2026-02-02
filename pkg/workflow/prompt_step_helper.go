// This file provides helper functions for generating prompt workflow steps.
//
// This file contains utilities for building GitHub Actions workflow steps that
// append prompt text to prompt files used by AI engines. These helpers extract
// common patterns used across multiple prompt generators (XPIA, temp folder,
// playwright, edit tool, etc.) to reduce code duplication and ensure security.
//
// # Organization Rationale
//
// These prompt step helpers are grouped here because they:
//   - Provide common patterns for prompt text generation used by 5+ generators
//   - Handle GitHub Actions expression extraction for security
//   - Ensure consistent prompt step formatting across engines
//   - Centralize template injection prevention logic
//
// This follows the helper file conventions documented in the developer instructions.
// See skills/developer/SKILL.md#helper-file-conventions for details.
//
// # Key Functions
//
// Static Prompt Generation:
//   - generateStaticPromptStep() - Generate steps for static prompt text
//   - generateStaticPromptStepWithExpressions() - Generate steps with secure expression handling
//
// # Usage Patterns
//
// These helpers are used when generating workflow steps that append text to
// prompt files. They follow two patterns:
//
//  1. **Static Text** (no GitHub Actions expressions):
//     ```go
//     generateStaticPromptStep(yaml,
//     "Append XPIA security instructions to prompt",
//     xpiaPromptText,
//     data.SafetyPrompt)
//     ```
//
//  2. **Text with Expressions** (contains ${{ ... }}):
//     ```go
//     generateStaticPromptStepWithExpressions(yaml,
//     "Append dynamic context to prompt",
//     promptWithExpressions,
//     shouldInclude)
//     ```
//
// The expression-aware helper extracts GitHub Actions expressions into
// environment variables to prevent template injection vulnerabilities.
//
// # Security Considerations
//
// Always use generateStaticPromptStepWithExpressions() when prompt text
// contains GitHub Actions expressions (${{ ... }}). This ensures:
//   - Expressions are evaluated in controlled env: context
//   - No inline shell script interpolation (prevents injection)
//   - Safe placeholder substitution via JavaScript
//
// See scratchpad/template-injection-prevention.md for security details.
//
// # When to Use vs Alternatives
//
// Use these helpers when:
//   - Generating workflow steps that append text to prompt files
//   - Working with static or expression-containing prompt text
//   - Need consistent prompt step formatting across engines
//
// For other prompt-related functionality, see:
//   - *_engine.go files for engine-specific prompt generation
//   - engine_helpers.go for shared engine utilities

package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var promptStepHelperLog = logger.New("workflow:prompt_step_helper")

// generateStaticPromptStep is a helper function that generates a workflow step
// for appending static prompt text to the prompt file. It encapsulates the common
// pattern used across multiple prompt generators (XPIA, temp folder, playwright, edit tool, etc.)
// to reduce code duplication and ensure consistency.
//
// Parameters:
//   - yaml: The string builder to write the YAML to
//   - description: The name of the workflow step (e.g., "Append XPIA security instructions to prompt")
//   - promptText: The static text content to append to the prompt (used for backward compatibility)
//   - shouldInclude: Whether to generate the step (false means skip generation entirely)
//
// Example usage:
//
//	generateStaticPromptStep(yaml,
//	    "Append XPIA security instructions to prompt",
//	    xpiaPromptText,
//	    data.SafetyPrompt)
//
// Deprecated: This function is kept for backward compatibility with inline prompts.
// Use generateStaticPromptStepFromFile for new code.
func generateStaticPromptStep(yaml *strings.Builder, description string, promptText string, shouldInclude bool) {
	promptStepHelperLog.Printf("Generating static prompt step: description=%s, shouldInclude=%t", description, shouldInclude)
	// Skip generation if guard condition is false
	if !shouldInclude {
		return
	}

	// Use the existing appendPromptStep helper with a renderer that writes the prompt text
	appendPromptStep(yaml,
		description,
		func(y *strings.Builder, indent string) {
			WritePromptTextToYAML(y, promptText, indent)
		},
		"", // no condition
		"          ")
}

// TODO: generateStaticPromptStepFromFile and generateStaticPromptStepFromFileWithExpressions
// could be implemented in the future to generate workflow steps for appending prompt files.
// For now, we use the unified prompt step approach in unified_prompt_step.go.
// See commit history if this needs to be restored.
