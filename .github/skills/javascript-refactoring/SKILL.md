---
name: javascript-refactoring
description: Split large JavaScript files into maintainable modules safely.
---


# JavaScript Code Refactoring Guide

Use this guide to split JavaScript into maintainable CommonJS modules in gh-aw without drifting into dead embedding patterns.

## Overview

The current gh-aw architecture is action-centric:

- Shared JS modules live under `pkg/workflow/js/*.cjs` and `actions/setup/js/*.cjs`
- Action source files live under `actions/<action-name>/src/`
- Generated action bundles are committed under `actions/<action-name>/index.js`
- Shipping is driven by the action build pipeline (`make actions-build`, `gh aw actions-build`) and dependency maps such as `pkg/cli/actions_build_command.go`
- `pkg/workflow/js.go` is a stub; it no longer owns the runtime JavaScript shipping path for the main workflows

If you are refactoring a workflow utility, prefer the current action/module architecture over any older `//go:embed` pattern.

### Top-Level Script Pattern

Top-level `.cjs` scripts executed directly in workflows follow this pattern:

**✅ Correct Pattern - Export main, but don't call it:**
```javascript
async function main() {
  // Script logic here
  core.info("Running the script");
}

module.exports = { main };
```

**❌ Incorrect Pattern - Don't call main in the file:**
```javascript
async function main() {
  // Script logic here
  core.info("Running the script");
}

await main(); // ❌ Don't do this!

module.exports = { main };
```

**Why this pattern?**
- The workflow bundler or action build step can wrap the script with `await main()` at execution time
- The module stays importable for tests while still being executable in GitHub Actions
- It makes unit testing easier and preserves a clean module boundary

## Step 1: Put the code in the right source tree

Choose the correct location for the module before writing code:

- Shared workflow utilities: `pkg/workflow/js/`
- Action-specific JavaScript: `actions/<action-name>/src/` or `actions/setup/js/`
- Generated bundle output: `actions/<action-name>/index.js`

**File naming convention:**
- Use snake_case for filenames (for example `sanitize_content.cjs`, `load_agent_output.cjs`)
- Use `.cjs` for CommonJS modules
- Keep the name aligned with the responsibility of the module

**Example file structure:**
```javascript
// @ts-check
/// <reference types="@actions/github-script" />

/**
 * Brief description of what this module does
 */

/**
 * Function documentation
 * @param {string} input - Description of parameter
 * @returns {string} Description of return value
 */
function myFunction(input) {
  return input;
}

module.exports = {
  myFunction,
};
```

**Key points:**
- Include `// @ts-check` for TypeScript checking
- Include `/// <reference types="@actions/github-script" />` when the module is used with GitHub Actions scripts
- Use JSDoc comments for documentation
- Export functions via `module.exports = { ... }`
- Do not import `@actions/core` or `@actions/github` directly unless the module is running in an action context that explicitly expects it

## Step 2: Add tests next to the module

Create a matching test beside the module using the same base name plus `.test.cjs`:

**Example:** `pkg/workflow/js/my_module.test.cjs`
```javascript
import { describe, it, expect, beforeEach, vi } from "vitest";

const mockCore = {
  debug: vi.fn(),
  info: vi.fn(),
  warning: vi.fn(),
  error: vi.fn(),
  setFailed: vi.fn(),
  setOutput: vi.fn(),
};

global.core = mockCore;

describe("myFunction", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("handles a normal input", async () => {
    const { myFunction } = await import("./my_module.cjs");
    expect(myFunction("test input")).toBe("expected output");
  });

  it("handles empty input", async () => {
    const { myFunction } = await import("./my_module.cjs");
    expect(myFunction("")).toBe("");
  });
});
```

**Testing guidelines:**
- Use Vitest for test execution
- Mock `core` and `github` globals as needed
- Use dynamic imports (`await import()`) to allow module setup at test time
- Clear mocks in `beforeEach`
- Cover success, failure, and edge cases

**Run tests:**
```bash
make test-js
```

## Step 3: Wire the module into the actual build path

Do not add a new `//go:embed` mapping just to ship a new runtime script. The current repo ships JavaScript through the action-generation/build pipeline.

Use this checklist:

- Shared utility used by generated actions: update the relevant dependency mapping in `pkg/cli/actions_build_command.go`
- Action-specific source file: add the module under `actions/<action-name>/src/`
- Generated action bundle: rebuild with `make actions-build`
- Shared workflow source for runtime modules: keep it under `pkg/workflow/js/` and update the action or workflow definition that consumes it

**Example design:**
```javascript
const { myFunction } = require("./my_module.cjs");

async function main() {
  const result = myFunction("some input");
  core.info(`Result: ${result}`);
}

module.exports = { main };
```

## Step 4: Validate the refactor

Run the relevant checks for the area you changed:

```bash
make fmt-cjs
make lint-cjs
make test-js
make test-unit
make actions-build
```

## Verification Checklist

Before committing your refactor:

- [ ] New `.cjs` file created in the correct source directory
- [ ] Matching `.test.cjs` file created
- [ ] Tests pass with `make test-js` or the targeted Vitest suite
- [ ] The module is wired through the real action/workflow build path
- [ ] No stale embedding instructions were added for the current action-based JS build flow
- [ ] Local `require()` statements work correctly in other JS files
- [ ] Code formatted with `make fmt-cjs`
- [ ] Relevant validation passes with `make lint-cjs` or `make test-unit`

## Common Patterns

### Pattern 1: Shared Utility Module

Files like `sanitize_content.cjs` or `load_agent_output.cjs` are best kept under `pkg/workflow/js/` or `actions/setup/js/` and consumed by other JS modules via `require()`.

### Pattern 2: Action-specific file

When the JavaScript belongs to a single action, keep it under `actions/<action-name>/src/` and regenerate the output bundle with `make actions-build`.

### Pattern 3: Top-level workflow script

If the script is executed directly in a workflow, export `main` and omit the direct `await main()` call. The host build/runtime step handles execution.

## Troubleshooting

### Issue: changes are not showing up in generated actions

**Cause:** Action bundle was not rebuilt after editing the source file

**Solution:**
```bash
make actions-build
```

### Issue: tests fail with `core is not defined`

**Cause:** Missing global mocks

**Solution:**
```javascript
global.core = mockCore;
```

### Issue: the module is only used in one place

**Cause:** It was added to the wrong layer

**Solution:** Move it to the action-specific source tree instead of creating a broad workflow-level registry entry.

## References

- `actions/README.md` - current action-generation/build workflow
- `pkg/cli/actions_build_command.go` - action dependency mapping
- `pkg/workflow/js/*.cjs` - existing shared module patterns
- `actions/setup/js/*.cjs` - action runtime/source examples
