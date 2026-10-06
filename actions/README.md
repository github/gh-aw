# GitHub Actions Directory

This directory contains custom GitHub Actions for the gh-aw project. These actions are used internally by compiled workflows to provide functionality such as MCP server file management.

## Directory Structure

Each action follows a standard structure:

```
actions/{action-name}/
├── action.yml          # Action metadata and configuration
├── index.js            # Bundled action code (generated, committed)
├── src/                # Source files
│   └── index.js       # Main action source
└── README.md           # Action-specific documentation
```

## Available Actions

### setup

Copies workflow script files to the agent environment. This action embeds all necessary JavaScript files used across all workflow jobs and copies them to a specified destination directory.

[Documentation](./setup/README.md)

### setup-cli

Installs the gh-aw CLI extension for a specific version. Supports both release tags and commit SHAs that resolve to releases.

[Documentation](./setup-cli/README.md)

### noop

Processes noop safe output - a fallback output type that logs messages for transparency without taking any GitHub API actions.

[Documentation](./noop/README.md)

### minimize_comment

Minimizes (hides) a comment using the GraphQL API.

[Documentation](./minimize_comment/README.md)

### close_issue

Closes a GitHub issue.

[Documentation](./close_issue/README.md)

### close_pull_request

Closes a GitHub pull request.

[Documentation](./close_pull_request/README.md)

### close_discussion

Closes a GitHub discussion.

[Documentation](./close_discussion/README.md)

## Building Actions

Actions are built using the Go-based build system that reuses the bundler and script registry infrastructure from the workflow compiler.

The build process:

1. Reads source files from `actions/{action-name}/src/`
2. Identifies and embeds required JavaScript dependencies from `pkg/workflow/js/`
3. Uses the same bundler infrastructure as workflow compilation
4. Bundles everything into `actions/{action-name}/index.js`
5. Validates `action.yml` files

### Build Commands

```bash
# Build all actions (generates index.js files)
make actions-build
# or
gh aw actions-build

# Validate action.yml files
make actions-validate
# or
gh aw actions-validate

# Clean generated files
# Clean generated files
make actions-clean
# or
gh aw actions-clean
```

The build system uses:
- **Bundler Infrastructure**: Same JavaScript bundler used for workflow compilation (`pkg/workflow/bundler.go`)
- **Script Registry**: Centralized registry for managing JavaScript sources (`pkg/workflow/script_registry.go`)
- **Embedded Sources**: All JavaScript files from `pkg/workflow/js/` via `GetJavaScriptSources()`

## Development Guidelines

### Generating Actions from JavaScript Modules

You can automatically generate action scaffolding from existing JavaScript modules in `pkg/workflow/js/`:

```bash
# Generate action.yml and README.md for selected JavaScript modules
make generate-action-metadata
# or
go run ./internal/tools/generate-action-metadata generate
```

This tool will:
1. Parse JavaScript files to extract inputs (from `core.getInput()`) and outputs (from `core.setOutput()`)
2. Extract descriptions from JSDoc comments
3. Generate `action.yml` with proper structure
4. Generate `README.md` with usage examples
5. Copy source files to `actions/{action-name}/src/index.js`

After generation, you need to:
1. Review and refine the generated `action.yml` and `README.md`
2. Update the dependency mapping in `pkg/cli/actions_build_command.go`
3. Run `make actions-build` to bundle dependencies

### Creating a New Action

1. **Create the directory structure:**
   ```bash
   mkdir -p actions/{action-name}/src
   ```

2. **Create action.yml:**
   Define the action metadata, inputs, outputs, and runtime configuration.
   ```yaml
   name: 'Action Name'
   description: 'Action description'
   author: 'GitHub Next'
   
   inputs:
     input-name:
       description: 'Input description'
       required: false
       default: 'default-value'
   
   outputs:
     output-name:
       description: 'Output description'
   
   runs:
     using: 'node24'
     main: 'index.js'
   ```

3. **Create source file (src/index.js):**
   Write the action logic using `@actions/core` and `@actions/github`:
   ```javascript
   const core = require('@actions/core');
   
   async function run() {
     try {
       const input = core.getInput('input-name');
       core.info(`Processing: ${input}`);
       
       // Action logic here
       
       core.setOutput('output-name', 'result');
     } catch (error) {
       core.setFailed(`Action failed: ${error.message}`);
     }
   }
   
   run();
   ```

4. **Update dependency mapping:**
   Add the action to the dependency map in `pkg/cli/actions_build_command.go` to specify which JavaScript files from `pkg/workflow/js/` should be embedded.

5. **Build and test:**
   ```bash
   make actions-build
   make actions-validate
   ```

6. **Create README.md:**
   Document the action's purpose, usage, inputs, outputs, and examples.

### Action Requirements

- **Runtime**: Actions must use Node.js 24 (`using: 'node24'`)
- **Dependencies**: Use `@actions/core` and `@actions/github` for GitHub Actions integration
- **Error Handling**: Always wrap main logic in try-catch and use `core.setFailed()` for errors
- **Logging**: Use `core.info()`, `core.warning()`, and `core.error()` for output
- **Outputs**: Set outputs using `core.setOutput(name, value)`

### Embedding Files

The build system supports embedding JavaScript files from `pkg/workflow/js/` into actions. To use this:

1. Define a `FILES` constant in your source file:
   ```javascript
   const FILES = {
     // This will be populated by the build script
   };
   ```

2. Add the files to the dependency map in `scripts/build-actions.js`

3. The build script will replace the empty `FILES` object with the actual file contents

4. Use the embedded files in your action:
   ```javascript
   for (const [filename, content] of Object.entries(FILES)) {
     fs.writeFileSync(path.join(destination, filename), content, 'utf8');
   }
   ```

## Action Types

This directory supports two types of actions:

### 1. Simple Actions (Single-file)

Actions with all logic in `src/index.js` without additional source files.

### 2. Complex Actions (Multi-file)

Actions that use multiple source files in the `src/` directory. The build system will bundle them together.

## Validation

The build system validates:

- ✅ `action.yml` exists and contains required fields
- ✅ `action.yml` uses `node24` runtime
- ✅ Source files exist in `src/` directory
- ✅ Required dependencies are available

## Git Management

### Files Committed to Git

- ✅ `action.yml` - Action metadata
- ✅ `index.js` - **Bundled action code (generated but committed)**
- ✅ `src/` - Source files
- ✅ `README.md` - Documentation

### Files Excluded from Git

- ❌ `node_modules/` - Dependencies (if any)
- ❌ `*.tmp` - Temporary files
- ❌ `.build/` - Build artifacts

**Note**: The bundled `index.js` files are committed to the repository because GitHub Actions requires the complete action code to be present when the action is used. This is standard practice for JavaScript actions.

## Testing

Test actions locally by:

1. Creating a test workflow in `.github/workflows/`
2. Using the action with a local path:
   ```yaml
   - uses: ./actions/setup
     with:
       destination: /tmp/test
   ```
3. Running the workflow on GitHub Actions

## Troubleshooting

### Build fails with "File not found"

Ensure the dependency files exist in `pkg/workflow/js/` and are listed correctly in the dependency map.

### Action fails at runtime

Check that:
- All required inputs are provided
- The bundled `index.js` is up to date (run `make actions-build`)
- The action has necessary permissions

### Validation fails

Ensure `action.yml` includes all required fields:
- `name`
- `description`
- `runs` with `using: 'node24'` and `main: 'index.js'`

## References

- [GitHub Actions - Creating JavaScript actions](https://docs.github.com/en/actions/creating-actions/creating-a-javascript-action)
- [GitHub Actions Toolkit](https://github.com/actions/toolkit)
- [@actions/core documentation](https://github.com/actions/toolkit/tree/main/packages/core)
