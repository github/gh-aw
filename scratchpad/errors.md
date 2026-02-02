# Error Handling and Validation

This document explains how to use the error handling and validation utilities in GitHub Agentic Workflows (gh-aw). These utilities provide structured error types, retry logic, and validation helpers for both Go and JavaScript code.

## Table of Contents

1. [Overview](#overview)
2. [Go Error Utilities](#go-error-utilities)
3. [JavaScript Error Recovery](#javascript-error-recovery)
4. [Best Practices](#best-practices)
5. [Examples](#examples)

---

## Overview

The error handling system provides:

- **Structured error types** with timestamps, context, and actionable suggestions
- **Automatic retry logic** with exponential backoff for transient failures
- **Validation helpers** for common input validation patterns
- **Error enhancement** to add context while preserving error chains

### Key Files

**Go:**
- `pkg/workflow/error_helpers.go` - Error types and validation utilities
- `pkg/workflow/error_helpers_test.go` - Test suite

**JavaScript:**
- `actions/setup/js/error_recovery.cjs` - Retry logic and error utilities
- `actions/setup/js/error_recovery.test.cjs` - Test suite

---

## Go Error Utilities

### Error Types

#### ValidationError

Use for input validation failures with field-level context.

```go
import "github.com/github/gh-aw/pkg/workflow"

// Create a validation error
err := workflow.NewValidationError(
    "title",                                    // field name
    titleValue,                                 // invalid value
    "cannot be empty",                          // reason
    "Provide a non-empty value for 'title'",   // suggestion
)

// Output:
// [2026-01-14T13:00:00Z] Validation failed for field 'title'
//
// Value: 
// Reason: cannot be empty
// Suggestion: Provide a non-empty value for 'title'
```

**Features:**
- Automatically includes ISO 8601 timestamp
- Truncates long values (>100 chars) for readability
- Optional suggestion field for guidance

#### OperationError

Use for operation failures with entity context.

```go
// Create an operation error
cause := errors.New("API rate limit exceeded")
err := workflow.NewOperationError(
    "update",              // operation name
    "issue",               // entity type
    "123",                 // entity ID
    cause,                 // underlying error
    "Wait and retry",      // custom suggestion (optional)
)

// Output:
// [2026-01-14T13:00:00Z] Failed to update issue #123
//
// Underlying error: API rate limit exceeded
// Suggestion: Wait and retry
```

**Features:**
- Includes timestamp and entity details
- Wraps underlying error (accessible via `errors.Unwrap()`)
- Auto-generates suggestions if not provided
- Entity ID is optional

#### ConfigurationError

Use for configuration validation failures.

```go
// Create a configuration error
err := workflow.NewConfigurationError(
    "safe-outputs.max",                         // config key
    "abc",                                      // invalid value
    "must be an integer",                       // reason
    "Use a numeric value like 'max: 3'",       // suggestion (optional)
)

// Output:
// [2026-01-14T13:00:00Z] Configuration error in 'safe-outputs.max'
//
// Value: abc
// Reason: must be an integer
// Suggestion: Use a numeric value like 'max: 3'
```

**Features:**
- Structured for configuration-specific errors
- Auto-generates helpful suggestions if not provided
- Truncates long configuration values

### Error Enhancement Functions

#### EnhanceError

Add context and suggestions to existing errors.

```go
originalErr := errors.New("file not found")
enhanced := workflow.EnhanceError(
    originalErr,
    "loading workflow configuration",  // context
    "Check the file path",              // suggestion
)

// Output:
// [2026-01-14T13:00:00Z] loading workflow configuration
//
// Original error: file not found
// Suggestion: Check the file path
```

**Use when:**
- Adding context to errors from external libraries
- Providing user-friendly explanations
- Adding suggestions without changing error structure

#### WrapErrorWithContext

Wrap errors while preserving the error chain.

```go
originalErr := errors.New("connection failed")
wrapped := workflow.WrapErrorWithContext(
    originalErr,
    "connecting to API",  // context
    "Check network",      // suggestion (optional)
)

// Can still unwrap
if errors.Is(wrapped, originalErr) {
    // Handle specific error type
}
```

**Use when:**
- You need `errors.Is()` or `errors.As()` to work
- Preserving error chain is important
- Building error context layers

### Validation Helpers

Common validation patterns with consistent error messages.

#### ValidateRequired

```go
err := workflow.ValidateRequired("title", titleValue)
// Returns ValidationError if value is empty or whitespace-only
```

#### ValidateMaxLength

```go
err := workflow.ValidateMaxLength("body", bodyValue, 1000)
// Returns ValidationError if value exceeds 1000 characters
```

#### ValidateMinLength

```go
err := workflow.ValidateMinLength("password", password, 8)
// Returns ValidationError if value is less than 8 characters
```

#### ValidateInList

```go
allowedStates := []string{"open", "closed", "draft"}
err := workflow.ValidateInList("state", stateValue, allowedStates)
// Returns ValidationError if value is not in the allowed list
```

#### ValidatePositiveInt

```go
err := workflow.ValidatePositiveInt("timeout", timeoutValue)
// Returns ValidationError if value is <= 0
```

#### ValidateNonNegativeInt

```go
err := workflow.ValidateNonNegativeInt("retry-count", retryCount)
// Returns ValidationError if value is < 0
```

### Usage Example

```go
func ValidateWorkflowConfig(config *WorkflowConfig) error {
    // Validate required fields
    if err := workflow.ValidateRequired("name", config.Name); err != nil {
        return err
    }
    
    // Validate field length
    if err := workflow.ValidateMaxLength("description", config.Description, 500); err != nil {
        return err
    }
    
    // Validate enum values
    validStates := []string{"enabled", "disabled"}
    if err := workflow.ValidateInList("state", config.State, validStates); err != nil {
        return err
    }
    
    // Validate positive integers
    if err := workflow.ValidatePositiveInt("timeout", config.Timeout); err != nil {
        return err
    }
    
    return nil
}
```

---

## JavaScript Error Recovery

### Retry Logic with Exponential Backoff

#### withRetry

Automatically retry operations that fail with transient errors.

```javascript
const { withRetry } = require('./error_recovery.cjs');

// Basic usage with defaults
const result = await withRetry(
  async () => {
    return await github.rest.issues.create({
      owner: 'org',
      repo: 'repo',
      title: 'Issue Title',
      body: 'Issue Body'
    });
  },
  {},                  // config (optional)
  'create issue'       // operation name for logging
);

// Custom configuration
const result = await withRetry(
  operation,
  {
    maxRetries: 5,           // Retry up to 5 times (default: 3)
    initialDelayMs: 2000,    // Start with 2s delay (default: 1000)
    maxDelayMs: 30000,       // Cap at 30s delay (default: 10000)
    backoffMultiplier: 2,    // Double delay each time (default: 2)
    shouldRetry: (error) => {
      // Custom retry logic (default: isTransientError)
      return error.status === 503;
    }
  },
  'custom operation'
);
```

**Features:**
- Automatic detection of transient errors (network, timeouts, rate limits)
- Exponential backoff: 1s → 2s → 4s → 8s (capped at maxDelayMs)
- Logs retry attempts with timing information
- Configurable retry behavior

**Detected transient errors:**
- Network errors (ECONNRESET, ETIMEDOUT, etc.)
- HTTP errors (502, 503, 504)
- GitHub rate limiting and abuse detection
- Timeouts and socket hangups

#### isTransientError

Check if an error should be retried.

```javascript
const { isTransientError } = require('./error_recovery.cjs');

try {
  await apiCall();
} catch (error) {
  if (isTransientError(error)) {
    core.warning('Transient error detected, will retry...');
    // Handle transient error
  } else {
    core.error('Non-transient error, cannot retry');
    // Handle permanent error
  }
}
```

### Error Enhancement

#### enhanceError

Add context and suggestions to errors.

```javascript
const { enhanceError } = require('./error_recovery.cjs');

try {
  await operation();
} catch (error) {
  const enhanced = enhanceError(
    error,
    {
      operation: 'update issue',
      attempt: 1,
      retryable: true,
      suggestion: 'Check repository permissions'
    }
  );
  throw enhanced;
}

// Output:
// [2026-01-14T13:00:00.000Z] update issue failed (attempt 1)
//
// Original error: Resource not found
// Retryable: true
// Suggestion: Check repository permissions
```

#### createValidationError

Create structured validation errors.

```javascript
const { createValidationError } = require('./error_recovery.cjs');

if (!title || title.trim() === '') {
  throw createValidationError(
    'title',                              // field name
    title,                                // invalid value
    'cannot be empty',                    // reason
    'Provide a non-empty title'          // suggestion (optional)
  );
}

// Output:
// [2026-01-14T13:00:00.000Z] Validation failed for field 'title'
//
// Value: 
// Reason: cannot be empty
// Suggestion: Provide a non-empty title
```

#### createOperationError

Create operation-specific errors.

```javascript
const { createOperationError } = require('./error_recovery.cjs');

try {
  await github.rest.issues.update({ issue_number: 123, ... });
} catch (error) {
  throw createOperationError(
    'update',                    // operation
    'issue',                     // entity type
    error,                       // cause
    123,                         // entity ID (optional)
    'Check permissions'          // suggestion (optional)
  );
}

// Output:
// [2026-01-14T13:00:00.000Z] Failed to update issue #123
//
// Underlying error: Not Found
// Suggestion: Check permissions
```

### Usage Example

```javascript
const { withRetry, createOperationError } = require('./error_recovery.cjs');

async function createIssueWithRetry(github, owner, repo, title, body) {
  try {
    // Automatically retry transient failures
    const result = await withRetry(
      async () => {
        return await github.rest.issues.create({
          owner,
          repo,
          title,
          body
        });
      },
      {
        maxRetries: 3,
        initialDelayMs: 1000
      },
      'create issue'
    );
    
    core.info(`✓ Created issue #${result.data.number}`);
    return result.data;
    
  } catch (error) {
    // Enhance error with context
    throw createOperationError(
      'create',
      'issue',
      error,
      undefined,
      'Check that the repository exists and you have write access'
    );
  }
}
```

---

## Best Practices

### When to Use Each Error Type

**ValidationError** - Use for:
- ✅ Input validation failures
- ✅ Field-level constraint violations
- ✅ Schema validation errors
- ✅ Type checking failures

**OperationError** - Use for:
- ✅ API operation failures
- ✅ Database operation errors
- ✅ File system operation failures
- ✅ External service errors

**ConfigurationError** - Use for:
- ✅ Workflow configuration errors
- ✅ Setting validation failures
- ✅ Schema validation in config files
- ✅ Invalid configuration values

### Error Message Guidelines

**DO:**
- ✅ Include timestamps for debugging
- ✅ Provide specific field names and values
- ✅ Suggest concrete actions to fix the problem
- ✅ Include relevant IDs (issue numbers, PR numbers, etc.)
- ✅ Use structured error types with consistent formatting

**DON'T:**
- ❌ Use generic error messages like "Invalid input"
- ❌ Expose internal implementation details
- ❌ Include sensitive information (tokens, passwords)
- ❌ Create deeply nested error chains
- ❌ Silence errors without logging

### Retry Guidelines

**DO retry for:**
- ✅ Network timeouts and connection errors
- ✅ HTTP 502, 503, 504 errors
- ✅ GitHub API rate limiting
- ✅ Transient infrastructure failures
- ✅ Temporary service unavailability

**DON'T retry for:**
- ❌ Validation errors (400 Bad Request)
- ❌ Authentication failures (401, 403)
- ❌ Resource not found (404)
- ❌ Permanent failures (410 Gone)
- ❌ Client errors that won't resolve with retry

### Error Context Best Practices

**Add context at boundaries:**
```go
// ✅ GOOD - Add context when crossing module boundaries
func ProcessWorkflow(path string) error {
    data, err := readWorkflowFile(path)
    if err != nil {
        return workflow.WrapErrorWithContext(
            err,
            "reading workflow file",
            "Check that the file exists and is readable",
        )
    }
    return processWorkflowData(data)
}

// ❌ BAD - Too many context layers
func ProcessWorkflow(path string) error {
    data, err := readWorkflowFile(path)
    if err != nil {
        wrapped := workflow.WrapErrorWithContext(err, "step 1", "")
        wrapped = workflow.WrapErrorWithContext(wrapped, "step 2", "")
        wrapped = workflow.WrapErrorWithContext(wrapped, "step 3", "")
        return wrapped
    }
    return processWorkflowData(data)
}
```

---

## Examples

### Complete Go Example

```go
package main

import (
    "fmt"
    "os"
    
    "github.com/github/gh-aw/pkg/console"
    "github.com/github/gh-aw/pkg/workflow"
)

type IssueConfig struct {
    Title  string
    Body   string
    Labels []string
    State  string
}

func ValidateAndCreateIssue(config *IssueConfig) error {
    // Validate inputs
    if err := validateIssueConfig(config); err != nil {
        fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
        return err
    }
    
    // Attempt to create issue
    if err := createIssueWithRetry(config); err != nil {
        fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
        return err
    }
    
    fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("Issue created successfully"))
    return nil
}

func validateIssueConfig(config *IssueConfig) error {
    // Validate title
    if err := workflow.ValidateRequired("title", config.Title); err != nil {
        return err
    }
    if err := workflow.ValidateMaxLength("title", config.Title, 256); err != nil {
        return err
    }
    
    // Validate body
    if err := workflow.ValidateMaxLength("body", config.Body, 65536); err != nil {
        return err
    }
    
    // Validate state
    validStates := []string{"open", "closed"}
    if err := workflow.ValidateInList("state", config.State, validStates); err != nil {
        return err
    }
    
    return nil
}

func createIssueWithRetry(config *IssueConfig) error {
    // Simulate API call with potential failure
    err := callGitHubAPI(config)
    if err != nil {
        return workflow.NewOperationError(
            "create",
            "issue",
            "",
            err,
            "Check repository access and rate limits",
        )
    }
    return nil
}

func callGitHubAPI(config *IssueConfig) error {
    // Actual GitHub API call would go here
    return nil
}
```

### Complete JavaScript Example

```javascript
const { withRetry, createValidationError, createOperationError } = require('./error_recovery.cjs');

/**
 * Validate and create a GitHub issue with automatic retry
 */
async function createIssueWithValidation(github, config) {
  // Validate inputs
  validateIssueConfig(config);
  
  try {
    // Create issue with automatic retry on transient failures
    const result = await withRetry(
      async () => {
        return await github.rest.issues.create({
          owner: config.owner,
          repo: config.repo,
          title: config.title,
          body: config.body,
          labels: config.labels || []
        });
      },
      {
        maxRetries: 3,
        initialDelayMs: 1000,
        maxDelayMs: 10000
      },
      'create issue'
    );
    
    core.info(`✓ Created issue #${result.data.number}`);
    return result.data;
    
  } catch (error) {
    // Enhance error with context
    throw createOperationError(
      'create',
      'issue',
      error,
      undefined,
      'Check repository access, rate limits, and permissions'
    );
  }
}

/**
 * Validate issue configuration
 */
function validateIssueConfig(config) {
  // Validate title
  if (!config.title || config.title.trim() === '') {
    throw createValidationError(
      'title',
      config.title,
      'cannot be empty',
      'Provide a non-empty issue title'
    );
  }
  
  if (config.title.length > 256) {
    throw createValidationError(
      'title',
      config.title,
      `exceeds maximum length of 256 characters (actual: ${config.title.length})`,
      'Shorten the issue title to 256 characters or less'
    );
  }
  
  // Validate labels
  if (config.labels && config.labels.length > 100) {
    throw createValidationError(
      'labels',
      config.labels.join(', '),
      `too many labels (maximum: 100, actual: ${config.labels.length})`,
      'Reduce the number of labels to 100 or fewer'
    );
  }
}

// Usage
async function main() {
  try {
    const issue = await createIssueWithValidation(github, {
      owner: 'org',
      repo: 'repo',
      title: 'Bug Report',
      body: 'Description of the bug',
      labels: ['bug', 'high-priority']
    });
    
    core.info(`Issue created: ${issue.html_url}`);
    
  } catch (error) {
    core.setFailed(error.message);
  }
}
```

---

## Related Documentation

- [Error Recovery Patterns](./error-recovery-patterns.md) - General error handling patterns and console formatting
- [Validation Architecture](./validation-architecture.md) - Validation system organization
- [Testing](./testing.md) - Testing guidelines for error handling code

---

## Summary

The error handling utilities provide:

1. **Structured error types** with timestamps, context, and suggestions
2. **Automatic retry logic** for transient failures
3. **Validation helpers** for common input validation patterns
4. **Error enhancement** to add context while preserving error chains

**Key principles:**
- Add context at module boundaries
- Provide actionable error messages with suggestions
- Use retry logic for transient failures only
- Validate inputs early with clear error messages
- Include timestamps and relevant IDs for debugging

For more examples, see the test files:
- `pkg/workflow/error_helpers_test.go`
- `actions/setup/js/error_recovery.test.cjs`
