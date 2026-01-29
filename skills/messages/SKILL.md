---
name: messages
description: Instructions for adding new message types to the safe-output messages system
---


# Adding New Message Types Guide

This guide explains how to add a new message type to the GitHub Agentic Workflows safe-output messages system. Follow these steps to ensure the new message is available in frontmatter, parsed by the compiler, available in JavaScript, and properly bundled.

## Overview

The messages system allows workflow authors to customize messages displayed in safe-output operations. Messages flow through:

1. **Frontmatter** (YAML) → 2. **JSON Schema** → 3. **Go Compiler** → 4. **JavaScript Modules** → 5. **Bundler**

## Step 1: Update JSON Schema

Add the new message field to `pkg/parser/schemas/main_workflow_schema.json` in the `messages` object:

```json
{
  "messages": {
    "properties": {
      "my-new-message": {
        "type": "string",
        "description": "Description of when this message is used. Available placeholders: {placeholder1}, {placeholder2}.",
        "examples": [
          "Example message with {placeholder1}"
        ]
      }
    }
  }
}
```

**Key points:**
- Use `kebab-case` for the YAML field name (e.g., `my-new-message`)
- Document all available placeholders in the description
- Provide helpful examples
- Run `make build` after changes (schema is embedded in binary)

## Step 2: Update Go Struct

Add the new field to `SafeOutputMessagesConfig` in `pkg/workflow/compiler.go`:

```go
type SafeOutputMessagesConfig struct {
	// ... existing fields ...
	MyNewMessage string `yaml:"my-new-message,omitempty" json:"myNewMessage,omitempty"` // Description of the message
}
```

**Key points:**
- Use `CamelCase` for Go field name
- Use `kebab-case` for YAML tag (matches frontmatter)
- Use `camelCase` for JSON tag (used in JavaScript)
- Add `omitempty` to both tags

## Step 3: Update Go Parser

If needed, update the parser in `pkg/workflow/safe_outputs.go`:

```go
func parseMessagesConfig(messagesMap map[string]any) *SafeOutputMessagesConfig {
	config := &SafeOutputMessagesConfig{}
	// ... existing parsing ...
	
	if myNewMessage, ok := messagesMap["my-new-message"].(string); ok {
		config.MyNewMessage = myNewMessage
	}
	
	return config
}
```

**Note:** The parser uses reflection for most fields, so this step may not be needed for simple string fields.

## Step 4: Create JavaScript Message Module

Create a new file `pkg/workflow/js/messages_my_new.cjs`:

```javascript
// @ts-check
/// <reference types="@actions/github-script" />

/**
 * My New Message Module
 *
 * This module provides the my-new-message generation
 * for [describe when it's used].
 */

const { getMessages, renderTemplate, toSnakeCase } = require("./messages_core.cjs");

/**
 * @typedef {Object} MyNewMessageContext
 * @property {string} placeholder1 - Description of placeholder1
 * @property {string} placeholder2 - Description of placeholder2
 */

/**
 * Get the my-new-message, using custom template if configured.
 * @param {MyNewMessageContext} ctx - Context for message generation
 * @returns {string} The generated message
 */
function getMyNewMessage(ctx) {
  const messages = getMessages();

  // Create context with both camelCase and snake_case keys
  const templateContext = toSnakeCase(ctx);

  // Default message template
  const defaultMessage = "Default message with {placeholder1} and {placeholder2}";

  // Use custom message if configured
  return messages?.myNewMessage
    ? renderTemplate(messages.myNewMessage, templateContext)
    : renderTemplate(defaultMessage, templateContext);
}

module.exports = {
  getMyNewMessage,
};
```

**Key points:**
- File naming: `messages_<category>.cjs` (flat structure, not subfolder)
- Import from `./messages_core.cjs` for shared utilities
- Use JSDoc for type definitions
- Provide sensible default message
- Support both custom and default templates

## Step 5: Add Tests

Create `pkg/workflow/js/messages_my_new.test.cjs`:

```javascript
import { describe, it, expect, beforeEach, vi } from "vitest";

// Mock core global
const mockCore = {
  warning: vi.fn(),
};
global.core = mockCore;

describe("getMyNewMessage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
  });

  it("should return default message when no custom message configured", async () => {
    const { getMyNewMessage } = await import("./messages_my_new.cjs");

    const result = getMyNewMessage({
      placeholder1: "value1",
      placeholder2: "value2",
    });

    expect(result).toBe("Default message with value1 and value2");
  });

  it("should use custom message when configured", async () => {
    process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({
      myNewMessage: "Custom: {placeholder1}",
    });

    const { getMyNewMessage } = await import("./messages_my_new.cjs");

    const result = getMyNewMessage({
      placeholder1: "test",
      placeholder2: "ignored",
    });

    expect(result).toContain("Custom: test");
  });
});
```

Run tests with `make test-js`.

## Step 6: Update Core Module TypeDef

Add the new property to the `SafeOutputMessages` typedef in `pkg/workflow/js/messages_core.cjs`:

```javascript
/**
 * @typedef {Object} SafeOutputMessages
 * @property {string} [footer] - Custom footer message template
 * // ... existing properties ...
 * @property {string} [myNewMessage] - Custom my-new-message template
 */
```

Also update the `getMessages()` function return object:

```javascript
return {
  footer: rawMessages.footer,
  // ... existing fields ...
  myNewMessage: rawMessages.myNewMessage,
};
```

## Step 7: Update Barrel File

Add the re-export to `pkg/workflow/js/messages.cjs`:

```javascript
// Re-export my new messages
const { getMyNewMessage } = require("./messages_my_new.cjs");

module.exports = {
  // ... existing exports ...
  getMyNewMessage,
};
```

## Step 8: Register in Go Embeddings

Add to `pkg/workflow/js.go`:

```go
//go:embed js/messages_my_new.cjs
var messagesMyNewScript string
```

Add to `GetJavaScriptSources()`:

```go
func GetJavaScriptSources() map[string]string {
	return map[string]string{
		// ... existing entries ...
		"messages_my_new.cjs": messagesMyNewScript,
	}
}
```

## Step 9: Use in Consumer Scripts

Import directly from the specific module in scripts that need it:

```javascript
const { getMyNewMessage } = require("./messages_my_new.cjs");

// Use the message
const message = getMyNewMessage({
  placeholder1: actualValue1,
  placeholder2: actualValue2,
});
```

## Step 10: Update Documentation

Update `scratchpad/safe-output-messages.md`:
1. Add the new message to the "Message Categories" section
2. Document placeholders and usage
3. Add examples

Update the Message Module Architecture table:
```markdown
| Module | Purpose | Exported Functions |
|--------|---------|-------------------|
| `messages_my_new.cjs` | My new message description | `getMyNewMessage` |
```

## Verification Checklist

Before committing:

- [ ] JSON Schema updated in `pkg/parser/schemas/main_workflow_schema.json`
- [ ] Go struct updated in `pkg/workflow/compiler.go`
- [ ] Go parser handles new field (if needed) in `pkg/workflow/safe_outputs.go`
- [ ] JavaScript module created: `pkg/workflow/js/messages_my_new.cjs`
- [ ] Tests created: `pkg/workflow/js/messages_my_new.test.cjs`
- [ ] TypeDef updated in `messages_core.cjs`
- [ ] Barrel file updated: `messages.cjs`
- [ ] Go embed directive added in `js.go`
- [ ] Added to `GetJavaScriptSources()` map
- [ ] Consumer scripts updated to use minimal imports
- [ ] Documentation updated in `scratchpad/safe-output-messages.md`
- [ ] Tests pass: `make test-js`
- [ ] Build succeeds: `make build`
- [ ] Linting passes: `make lint`

## File Summary

| File | Purpose | Changes Needed |
|------|---------|----------------|
| `pkg/parser/schemas/main_workflow_schema.json` | JSON Schema | Add field definition |
| `pkg/workflow/compiler.go` | Go struct | Add struct field |
| `pkg/workflow/safe_outputs.go` | Parser | Add parsing logic (if needed) |
| `pkg/workflow/js/messages_my_new.cjs` | JavaScript module | Create new file |
| `pkg/workflow/js/messages_my_new.test.cjs` | Tests | Create new file |
| `pkg/workflow/js/messages_core.cjs` | Core utilities | Update typedef |
| `pkg/workflow/js/messages.cjs` | Barrel file | Add re-export |
| `pkg/workflow/js.go` | Go embeddings | Add embed directive |
| `scratchpad/safe-output-messages.md` | Documentation | Document new message |

## Example: Adding `close-older-discussion` Message

This message type was added following this process:

1. **Schema**: Added `close-older-discussion` field with placeholders `{new_discussion_number}`, `{new_discussion_url}`, `{workflow_name}`, `{run_url}`
2. **Go struct**: Added `CloseOlderDiscussion string` field
3. **JavaScript**: Created `messages_close_discussion.cjs` with `getCloseOlderDiscussionMessage()`
4. **Tests**: Added corresponding test file
5. **Bundler**: Registered in `GetJavaScriptSources()`
6. **Consumer**: Used in `close_older_discussions.cjs` via direct import

See these files for a working implementation example.
