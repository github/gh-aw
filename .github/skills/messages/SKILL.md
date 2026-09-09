---
name: messages
description: Add new safe-output message types and wire validation/rendering.
---


# Adding New Message Types Guide

Use this guide to add a new safe-output message type so it works in the current gh-aw pipeline: frontmatter → schema → Go compiler → JavaScript modules → action/workflow build output.

## Overview

The messages system lets workflow authors customize safe-output messages. The current architecture does not rely on the old `pkg/workflow/js.go` embedding registry for runtime shipping.

Current flow:

1. **Frontmatter** (YAML)
2. **JSON Schema**
3. **Go Compiler**
4. **JavaScript module** under `pkg/workflow/js/` or `actions/setup/js/`
5. **Action/workflow bundle generation** via `make actions-build` or the relevant workflow build path

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
- Use `kebab-case` for the YAML field name (for example `my-new-message`)
- Document placeholders in the description
- Provide helpful examples
- Rebuild the schema-backed binary or run the relevant compile checks after changes

## Step 2: Update Go Struct

Add the field to `SafeOutputMessagesConfig` in `pkg/workflow/compiler.go`:

```go
type SafeOutputMessagesConfig struct {
	// ... existing fields ...
	MyNewMessage string `yaml:"my-new-message,omitempty" json:"myNewMessage,omitempty"`
}
```

**Key points:**
- Use `CamelCase` for Go field names
- Use `kebab-case` for YAML tags
- Use `camelCase` for JSON tags
- Add `omitempty` to both tags

## Step 3: Update the parser if needed

If the message needs custom parsing logic, update the workflow parser in `pkg/workflow/safe_outputs.go` or the relevant config block. Most simple string fields will be wired automatically by the existing reflection-based parser.

## Step 4: Create the JavaScript message module

Create the new module in the current shared JS location, typically `pkg/workflow/js/`:

```javascript
// @ts-check
/// <reference types="@actions/github-script" />

const { getMessages, renderTemplate, toSnakeCase } = require("./messages_core.cjs");

/**
 * @typedef {Object} MyNewMessageContext
 * @property {string} placeholder1 - Description of placeholder1
 * @property {string} placeholder2 - Description of placeholder2
 */

function getMyNewMessage(ctx) {
	const messages = getMessages();
	const templateContext = toSnakeCase(ctx);
	const defaultMessage = "Default message with {placeholder1} and {placeholder2}";

	return messages?.myNewMessage
		? renderTemplate(messages.myNewMessage, templateContext)
		: renderTemplate(defaultMessage, templateContext);
}

module.exports = {
	getMyNewMessage,
};
```

**Key points:**
- File naming: `messages_<category>.cjs`
- Reuse `./messages_core.cjs` for shared helpers
- Use JSDoc for types and default behavior
- Keep the default message sensible and deterministic

## Step 5: Add tests

Create a matching test file, for example `pkg/workflow/js/messages_my_new.test.cjs`:

```javascript
import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = { warning: vi.fn() };
global.core = mockCore;

describe("getMyNewMessage", () => {
	beforeEach(() => {
		vi.clearAllMocks();
		delete process.env.GH_AW_SAFE_OUTPUT_MESSAGES;
	});

	it("returns the default message when no custom template is configured", async () => {
		const { getMyNewMessage } = await import("./messages_my_new.cjs");
		const result = getMyNewMessage({ placeholder1: "value1", placeholder2: "value2" });
		expect(result).toBe("Default message with value1 and value2");
	});

	it("uses the custom template when configured", async () => {
		process.env.GH_AW_SAFE_OUTPUT_MESSAGES = JSON.stringify({ myNewMessage: "Custom: {placeholder1}" });
		const { getMyNewMessage } = await import("./messages_my_new.cjs");
		const result = getMyNewMessage({ placeholder1: "test", placeholder2: "ignored" });
		expect(result).toContain("Custom: test");
	});
});
```

Run the relevant tests with `make test-js` or the targeted Vitest file.

## Step 6: Update the core JS type metadata and exports

Update the `SafeOutputMessages` typedef and the return object in `pkg/workflow/js/messages_core.cjs`, and re-export the message helper from `pkg/workflow/js/messages.cjs`.

## Step 7: Wire it into the real build path

Do not add any new `//go:embed` entries to `pkg/workflow/js.go` for a normal message module. The current system packages JavaScript through the action-generation/build path.

Instead:

- keep the JS module in `pkg/workflow/js/` or the relevant action folder,
- update the action dependency map or action source if needed,
- rebuild the action bundle with `make actions-build`.

## Step 8: Use the message in consumer scripts

```javascript
const { getMyNewMessage } = require("./messages_my_new.cjs");

const message = getMyNewMessage({
	placeholder1: actualValue1,
	placeholder2: actualValue2,
});
```

## Step 9: Update documentation

Document the new message in the repo’s relevant safe-output docs, and keep the examples aligned with the current action-based JavaScript build flow.

## Verification Checklist

Before committing a message change:

- [ ] Frontmatter and schema updated
- [ ] Go config/struct updated if needed
- [ ] JS module created under the correct source tree
- [ ] Tests added and passing
- [ ] `messages_core.cjs` and `messages.cjs` updated if relevant
- [ ] Generated action/build output refreshed when required
- [ ] No stale embedding instructions are introduced for the current action-based JS build flow

## References

- `actions/README.md` - current action-generation/build workflow
- `pkg/workflow/js/messages_core.cjs` - shared safe-output message helpers
- `pkg/workflow/js/messages.cjs` - message exports
- `pkg/parser/schemas/main_workflow_schema.json` - schema source of truth

Update the Message Module Architecture table:
```markdown
| Module | Purpose | Exported Functions |
|--------|---------|-------------------|
| `messages_my_new.cjs` | My new message description | `getMyNewMessage` |
```

## Notes

For current gh-aw work, keep message modules aligned with the action-generation flow instead of the historical Go-embed pattern. If you need an example, review the existing safe-output modules under `pkg/workflow/js/` and the generated action files under `actions/`.
