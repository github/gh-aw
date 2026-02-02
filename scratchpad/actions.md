# Custom GitHub Actions Build System

> Specification documenting the custom actions directory structure, build tooling, and architectural decisions.
> Last updated: 2025-12-09

## Overview

This document describes the custom GitHub Actions build system implemented to support migrating from inline JavaScript (using `actions/github-script`) to standalone custom actions. The system provides a foundation for creating, building, and managing custom GitHub Actions that can be referenced in compiled workflows.

**Key Implementation Detail**: The build system is **entirely implemented in Go** (in `pkg/cli/actions_build_command.go`). There are no JavaScript build scripts. The system reuses the workflow compiler's bundler infrastructure and is invoked via Makefile targets during development.

## Table of Contents

- [Motivation](#motivation)
- [Architecture](#architecture)
- [Directory Structure](#directory-structure)
- [Build System](#build-system)
- [Architectural Decisions](#architectural-decisions)
- [Usage Guide](#usage-guide)
- [CI Integration](#ci-integration)
- [Development Guide](#development-guide)
- [Future Work](#future-work)
- [Compiler Integration: Dev Action Mode](#compiler-integration-dev-action-mode)

## Motivation

### Problem Statement

The workflow compiler generates inline JavaScript code embedded in YAML files using `actions/github-script`. This approach has several limitations:

1. **No Version Control**: JavaScript code is embedded in compiled `.lock.yml` files without semantic versioning
2. **Limited Reusability**: Same JavaScript logic is duplicated across multiple workflows
3. **Testing Challenges**: Inline scripts are harder to test independently
4. **Maintenance Burden**: Changes require recompiling all affected workflows
5. **Distribution Issues**: Cannot easily share actions across repositories

### Solution

Create a custom actions system that:
- Stores actions in a dedicated `actions/` directory
- Provides versioning through Git tags/releases
- Enables reuse via `uses: ./actions/{action-name}`
- Supports independent testing and validation
- Leverages existing bundler infrastructure from workflow compilation

## Architecture

### High-Level Design

```text
┌─────────────────────────────────────────────────────────┐
│                    Makefile Interface                    │
│  ┌────────────────────────────────────────────────────┐ │
│  │  make actions-build  │  make actions-validate     │ │
│  │  make actions-clean                                │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼ (go run internal/tools/actions-build)
┌─────────────────────────────────────────────────────────┐
│         internal/tools/actions-build/main.go             │
│              (Internal Development Tool)                 │
│  ┌────────────────────────────────────────────────────┐ │
│  │  CLI dispatcher for:                               │ │
│  │  • build command                                   │ │
│  │  • validate command                                │ │
│  │  • clean command                                   │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│            pkg/cli/actions_build_command.go              │
│              (Pure Go Implementation)                    │
│  ┌────────────────────────────────────────────────────┐ │
│  │  • ActionsBuildCommand()                           │ │
│  │  • ActionsValidateCommand()                        │ │
│  │  • ActionsCleanCommand()                           │ │
│  │  • getActionDependencies() - Manual mapping        │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│         Reused Workflow Infrastructure                   │
│  ┌────────────────────────────────────────────────────┐ │
│  │  workflow.GetJavaScriptSources()                   │ │
│  │    - Returns empty map (no embedded files)         │ │
│  │    - JavaScript files used directly from           │ │
│  │      actions/setup/js/ at runtime                  │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
                          │
                          ▼
┌─────────────────────────────────────────────────────────┐
│                    actions/ Directory                    │
│  ┌────────────────────────────────────────────────────┐ │
│  │  setup/                                            │ │
│  │  ├── action.yml                                    │ │
│  │  ├── setup.sh                                      │ │
│  │  ├── js/                                           │ │
│  │  │   └── *.cjs (copied from pkg/workflow/js/)    │ │
│  │  ├── sh/                                           │ │
│  │  │   └── *.sh (source of truth)                   │ │
│  │  └── README.md                                     │ │
│  └────────────────────────────────────────────────────┘ │
└─────────────────────────────────────────────────────────┘
```text

### Component Responsibilities

#### 1. Makefile Interface (`Makefile`)
- Primary entry point for building actions: `make actions-build`, `make actions-validate`, `make actions-clean`
- Invokes internal tool via `go run ./internal/tools/actions-build <command>`
- Project-specific development commands (not end-user CLI)

#### 2. Internal Tool (`internal/tools/actions-build/main.go`)
- Lightweight CLI dispatcher for development-only commands
- Not part of the main `gh aw` CLI (which is for end users)
- Routes commands to appropriate functions in `pkg/cli`

#### 3. Build System Implementation (`pkg/cli/actions_build_command.go`)
- **Pure Go implementation** - No JavaScript build scripts
- **ActionsBuildCommand()**: Builds all actions by bundling dependencies
- **ActionsValidateCommand()**: Validates action.yml files
- **ActionsCleanCommand()**: Removes generated index.js files
- **getActionDependencies()**: Maps action names to required JavaScript files

#### 4. JavaScript Sources (`pkg/workflow/js.go`)
- `GetJavaScriptSources()`: Returns empty map (embedded scripts removed)
- JavaScript files are NOT embedded in the binary
- Files are used directly from `actions/setup/js/` at runtime

#### 5. Actions Directory (`actions/`)
- Contains custom action subdirectories
- Each action follows GitHub Actions standard structure
- Source files in `src/`, compiled output in root

## Directory Structure

### Repository Layout

```text
gh-aw/
├── actions/                          # Custom GitHub Actions
│   ├── README.md                     # Actions documentation
│   ├── setup/                        # Setup action with runtime file copying
│   │   ├── action.yml               # Action metadata
│   │   ├── setup.sh                 # Main setup script (copies files at runtime)
│   │   ├── js/                      # JavaScript files (SOURCE OF TRUTH)
│   │   │   └── *.cjs                # Manually edited, committed to git (~252 files)
│   │   ├── sh/                      # Shell scripts (SOURCE OF TRUTH)
│   │   │   └── *.sh                 # Manually edited, committed to git (~6 files)
│   │   └── README.md                # Action-specific docs
├── pkg/
│   ├── cli/
│   │   └── actions_build_command.go # Build system implementation
│   └── workflow/
│       ├── js.go                    # JavaScript sources (returns empty - no embedding)
│       ├── sh.go                    # Shell script sources
│       ├── js/                      # Contains only safe_outputs_tools.json
│       │   └── safe_outputs_tools.json  # NOT synced .cjs files
│       └── sh/                      # May contain generated files (not used at runtime)
├── cmd/gh-aw/
│   └── main.go                      # CLI entry point with commands
├── Makefile                         # Build targets (NO sync-shell-scripts or sync-js-scripts)
└── .github/workflows/
    └── ci.yml                       # CI pipeline
```text

**Runtime File Copy Flow (Current Architecture):**

JavaScript and shell script files are NOT embedded in the binary. Instead, they are copied at runtime:

1. **Source of Truth**: `actions/setup/js/*.cjs` and `actions/setup/sh/*.sh` (manually edited, committed to git)
2. **Runtime Copy**: The `actions/setup` action runs setup.sh which copies files from `actions/setup/js/` and `actions/setup/sh/` to `/tmp/gh-aw/actions`
3. **Usage**: Workflow jobs access files directly from `/tmp/gh-aw/actions` via `require()` for JavaScript or direct execution for shell scripts
4. **No Embedding**: Files are NOT embedded via `//go:embed` - the `pkg/workflow/js.go` file explicitly states "Embedded scripts have been removed"

**Key Directories:**
- `actions/setup/js/*.cjs` - Source of truth (manually edited, committed, ~252 files)
- `actions/setup/sh/*.sh` - Source of truth (manually edited, committed, ~6 files)
- `pkg/workflow/js/` - Contains only `safe_outputs_tools.json` (NOT synced .cjs files)
- `pkg/workflow/sh/` - NOT used for runtime shell scripts
- `/tmp/gh-aw/actions` - Runtime destination where files are copied for workflow execution

**Note:** The Makefile targets `make sync-js-scripts` and `make sync-shell-scripts` do NOT exist and are not needed in the current architecture.

### Action Structure

Each action follows this template:

```text
actions/{action-name}/
├── action.yml          # Metadata: name, description, inputs, outputs, runs
├── index.js            # Bundled JavaScript (generated, committed)
├── src/                # Source files
│   └── index.js        # Main entry point with FILES placeholder
└── README.md           # Action documentation
```text

### action.yml Format

```yaml
name: 'Action Name'
description: 'Action description'
inputs:
  destination:
    description: 'Destination directory path'
    required: true
    default: '/tmp/action-files'
runs:
  using: 'node20'
  main: 'index.js'
```text

### Source File Pattern

Source files use a `FILES` constant that gets replaced during build:

```javascript
const fs = require('fs');
const path = require('path');

// This object is populated during build with embedded file contents
const FILES = {};

// Main action code that uses FILES
const destinationDir = process.env.INPUT_DESTINATION || '/tmp/action-files';

// Create directory and write files
for (const [filename, content] of Object.entries(FILES)) {
  const filepath = path.join(destinationDir, filename);
  fs.mkdirSync(path.dirname(filepath), { recursive: true });
  fs.writeFileSync(filepath, content, 'utf8');
}
```text

## Build System

### Build Process

The build system is implemented entirely in Go and follows these steps:

1. **Shell Script Sync** (NEW): Copies shell scripts from `actions/setup/sh/` to `pkg/workflow/sh/`
2. **Discovery**: Scans `actions/` directory for action subdirectories
3. **Validation**: Validates each `action.yml` file structure
4. **Dependency Resolution**: Maps action name to required JavaScript files (manual mapping in Go)
5. **File Reading**: Retrieves file contents from `workflow.GetJavaScriptSources()`
6. **Bundling**: Creates JSON object with all dependencies
7. **Code Generation**: Replaces `FILES` placeholder in source with bundled content
8. **Output**: Writes bundled `index.js` to action directory

**Key Point**: The build system is pure Go code in `pkg/cli/actions_build_command.go`. There are no JavaScript build scripts - everything uses the workflow compiler's existing infrastructure.

### Shell Script Sync Process

**Important**: JavaScript and shell scripts are NOT embedded in the binary. They are used directly from `actions/setup/` at runtime.

**Current Architecture** (NO embedding):
```text
actions/setup/js/*.cjs  (SOURCE OF TRUTH)  →  Runtime copy to /tmp/gh-aw/actions
actions/setup/sh/*.sh   (SOURCE OF TRUTH)  →  Runtime copy to /tmp/gh-aw/actions
```text

**Why this pattern?**
- JavaScript and shell scripts live in `actions/setup/js/` and `actions/setup/sh/` as source of truth
- They are committed to git and used directly at runtime (no embedding in binary)
- The `actions/setup` action copies these files to `/tmp/gh-aw/actions` at runtime
- Workflows access the files via `require()` or direct execution from `/tmp/gh-aw/actions`
- Test files (`*.test.cjs`) remain in `actions/setup/js/` alongside production files

### Build Commands

Use Makefile targets for building actions:

```bash
# Build all actions
make actions-build

# Validate action.yml files
make actions-validate

# Clean generated files
make actions-clean
```text

### Implementation Details

#### Dependency Mapping

Currently uses manual mapping in `getActionDependencies()`:

```go
func getActionDependencies(actionName string) []string {
    // For setup, use the dynamic script discovery
    // This ensures all .cjs files are included automatically
    if actionName == "setup" {
        return workflow.GetAllScriptFilenames()
    }

    return []string{}
}
```text

#### File Embedding

Files are embedded at build time using regex replacement:

```go
// Replace the FILES placeholder in source
filesRegex := regexp.MustCompile(`(?s)const FILES = \{[^}]*\};`)
outputContent := filesRegex.ReplaceAllString(
    string(sourceContent), 
    fmt.Sprintf("const FILES = %s;", strings.TrimSpace(indentedJSON))
)
```text

## Architectural Decisions

### Decision 1: Reuse Workflow Bundler Infrastructure

**Decision**: Leverage existing `workflow.GetJavaScriptSources()` instead of creating separate bundling system.

**Rationale**:
- Eliminates code duplication
- Single source of truth for JavaScript files
- Maintains consistency with workflow compilation
- Reduces maintenance burden

**Implications**:
- Actions and workflows share same JavaScript dependencies
- Changes to embedded files affect both systems
- Build system must stay in sync with workflow compiler

### Decision 2: Manual Dependency Mapping

**Decision**: Use explicit map of action names to required files rather than automatic dependency resolution.

**Rationale**:
- Simpler implementation for initial version
- Explicit dependencies are easier to understand
- Fewer moving parts reduces complexity
- Can migrate to automatic resolution later

**Trade-offs**:
- Must manually update when dependencies change
- Risk of forgetting to update mapping
- More maintenance overhead

**Future**: Implement automatic dependency resolution using `FindJavaScriptDependencies()` from bundler.

### Decision 3: Commit Bundled Files

**Decision**: Commit generated `index.js` files to Git, marked as `linguist-generated`.

**Rationale**:
- GitHub Actions requires files to be in repository
- No runtime build step needed in workflows
- Easier for consumers to use actions
- Git diff shows what changed

**Implications**:
- Repository size increases with bundled files
- Must rebuild and commit after changes
- Generated files appear in diffs (marked as generated)

### Decision 4: Use `go run` for Development Commands

**Decision**: Makefile targets run action commands via `go run ./cmd/gh-aw` instead of building binary first.

**Rationale**:
- Faster iteration during development
- No stale binary issues
- Simpler developer workflow
- Commands are project-specific (not end-user facing)
- Eliminates need to rebuild binary for every change

**Trade-offs**:
- Slightly slower execution (compilation overhead)
- Requires Go toolchain

**Implementation**: Makefile uses `@go run ./cmd/gh-aw actions-build` pattern.

### Decision 5: Node.js 20 Runtime

**Decision**: Use `node24` as the runtime for all actions.

**Rationale**:
- Latest stable Node.js version supported by GitHub Actions (v8.0.0+ of github-script)
- Modern JavaScript features available
- Consistent with workflow compilation environment

## Usage Guide

### Using Actions in Workflows

Actions can be referenced using relative paths:

```yaml
jobs:
  my-job:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v5
      
      - name: Setup Workflow Scripts
        uses: ./actions/setup
        with:
          destination: /tmp/scripts
```text

### Creating a New Action

1. **Create directory structure**:
   ```bash
   mkdir -p actions/my-action/src
   ```

2. **Create action.yml**:
   ```yaml
   name: 'My Action'
   description: 'Description of my action'
   inputs:
     destination:
       description: 'Destination directory'
       required: true
       default: '/tmp/my-action'
   runs:
     using: 'node24'
     main: 'index.js'
   ```

3. **Create src/index.js**:
   ```javascript
   const fs = require('fs');
   const path = require('path');
   
   const FILES = {};
   
   const destinationDir = process.env.INPUT_DESTINATION || '/tmp/my-action';
   
   for (const [filename, content] of Object.entries(FILES)) {
     const filepath = path.join(destinationDir, filename);
     fs.mkdirSync(path.dirname(filepath), { recursive: true });
     fs.writeFileSync(filepath, content, 'utf8');
   }
   
   console.log(`Files copied to ${destinationDir}`);
   ```

4. **Update dependency mapping** in `pkg/cli/actions_build_command.go`:
   ```go
   func getActionDependencies(actionName string) []string {
       dependencyMap := map[string][]string{
           // ... existing mappings ...
           "my-action": {
               "required_file1.cjs",
               "required_file2.cjs",
           },
       }
       // ...
   }
   ```

5. **Build and test**:
   ```bash
   make actions-build
   make actions-validate
   ```

6. **Create README.md** documenting the action

### Modifying Existing Actions

1. **Edit source files** in `actions/{action-name}/src/`
2. **Update dependencies** if needed in `pkg/cli/actions_build_command.go`
3. **Rebuild**: `make actions-build`
4. **Validate**: `make actions-validate`
5. **Test**: Use action in a workflow and verify behavior
6. **Commit**: Include both source and generated `index.js` changes

## CI Integration

### Actions Build Job

The CI pipeline includes an `actions-build` job that validates actions on every pull request:

```yaml
actions-build:
  needs: [lint]
  runs-on: ubuntu-latest
  steps:
    - uses: actions/checkout@v5
    - uses: actions/setup-go@v6
      with:
        go-version-file: go.mod
        cache: true
    - run: go mod verify
    - run: make actions-build
    - run: make actions-validate
```text

### Trigger Conditions

The CI runs when:
- Any `.go` file changes
- Any file in `actions/**` changes
- `.github/workflows/ci.yml` changes
- Workflow markdown files change

### What Gets Validated

1. **Go code compilation**: Ensures build system compiles
2. **Action building**: All actions must build successfully
3. **Action validation**: All action.yml files must be valid
4. **Dependency resolution**: All referenced files must exist

## Development Guide

### For Future Agents

#### Quick Start

1. **Understand the structure**:
   ```bash
   tree actions/
   ```

2. **Explore build system**:
   - Read `pkg/cli/actions_build_command.go`
   - Check `getActionDependencies()` for mapping

3. **Test locally**:
   ```bash
   make actions-build
   make actions-validate
   ```

4. **Check CI**:
   - Look at `.github/workflows/ci.yml`
   - Find `actions-build` job

#### Common Tasks

**Modify a shell script**:
1. Edit the file in `actions/setup/sh/` (source of truth)
2. Run `make build` (syncs to pkg/workflow/sh/ and rebuilds binary)
3. Run `make actions-build` (builds actions including setup)
4. Test in a workflow to verify behavior
5. Commit both `actions/setup/sh/*.sh` and generated `pkg/workflow/sh/*.sh`

**Add a new shell script**:
1. Create the file in `actions/setup/sh/`
2. Update setup action to use the new script (if needed)
3. Commit the file to git
4. The file will be copied to `/tmp/gh-aw/actions` at runtime by the setup action
5. No embedding or build step required

**Add a new action**:
1. Create directory structure in `actions/`
2. Write `action.yml`, `src/index.js`, `README.md`
3. Update `getActionDependencies()` in `actions_build_command.go`
4. Run `make actions-build`
5. Test in a workflow

**Update dependencies**:
1. Modify `getActionDependencies()` mapping
2. Rebuild: `make actions-build`
3. Verify: Check generated `index.js` has new files

**Add new JavaScript source**:
1. Add file to `actions/setup/js/`
2. Format and lint: `make fmt-cjs && make lint-cjs`
3. Commit the file to git
4. The file will be copied to `/tmp/gh-aw/actions` at runtime by the setup action
5. No embedding or build step required

#### Key Files to Know

- `pkg/cli/actions_build_command.go` - **Pure Go build system** (no JavaScript)
- `internal/tools/actions-build/main.go` - Internal CLI tool dispatcher (development only)
- `pkg/workflow/js.go` - JavaScript source map (returns empty - no embedded files)
- `pkg/workflow/sh.go` - Shell script source map
- `actions/setup/sh/` - **Source of truth for shell scripts** (manually edited, committed)
- `actions/setup/js/` - **Source of truth for JavaScript files** (manually edited, committed)
- `actions/setup/setup.sh` - Copies files from actions/setup/ to /tmp/gh-aw/actions at runtime
- `Makefile` - Primary interface for building actions (`make actions-build`)
- `.github/workflows/ci.yml` - CI validation
- `actions/README.md` - Actions documentation

**Important**: Build process is 100% Go code. No `scripts/build-actions.js` or similar JavaScript build scripts exist. Commands are invoked via Makefile, which runs the internal tool at `internal/tools/actions-build`.

#### Debugging Tips

**Action won't build**:
- Check if all dependencies exist in `GetJavaScriptSources()`
- Verify `getActionDependencies()` mapping is correct
- Look for typos in filenames

**Generated file looks wrong**:
- Check regex pattern in `buildAction()`
- Verify source file has `const FILES = {};` placeholder
- Ensure JSON indentation is correct

**CI failing**:
- Run `make actions-build` locally first
- Check Go syntax errors
- Verify action.yml is valid YAML

## Future Work

### Planned Improvements

1. **Automatic Dependency Resolution**
   - Use `FindJavaScriptDependencies()` from bundler
   - Eliminate manual mapping
   - Parse `require()` statements automatically

2. **Action Versioning**
   - Git tags for action versions
   - Semantic versioning support
   - Version pinning in workflows

3. **Testing Infrastructure**
   - Unit tests for actions
   - Integration tests with workflows
   - Mock GitHub Actions environment

4. **Enhanced Validation**
   - Lint JavaScript code
   - Validate against Actions schema
   - Check for common mistakes

5. **Distribution**
   - Publish actions to GitHub Marketplace
   - Support external repository references
   - Create action templates

6. **Developer Experience**
   - Interactive action creation wizard
   - Auto-generate action.yml from source
   - Hot reload during development

### Migration Path

**Phase 1**: Current State (Complete)
- Directory structure established
- Build system working
- Two initial actions created
- CI integration complete

**Phase 2**: Enhanced Tooling (Next)
- Automatic dependency resolution
- Better error messages
- Validation improvements

**Phase 3**: Workflow Migration (Future)
- Identify inline scripts to migrate
- Create actions from inline code
- Update workflows to use actions

**Phase 4**: Distribution (Future)
- Version and publish actions
- External repository support
- Marketplace presence

## Summary

The custom GitHub Actions build system provides a foundation for migrating from inline JavaScript to versioned, reusable actions. Key achievements:

✅ **Structured directory layout** following GitHub Actions conventions
✅ **Go-based build system** reusing workflow bundler infrastructure  
✅ **Makefile integration** for action management
✅ **CI validation** ensuring actions stay buildable
✅ **Setup action** for workflow script management
✅ **Complete documentation** for future development

The system is production-ready and extensible, with clear paths for enhancement and migration of existing inline scripts.

## Compiler Integration: Action Modes

> This section documents the action mode features that control how the workflow compiler generates custom action references in compiled workflows.

### Overview

The action mode system enables the workflow compiler to generate different types of references to custom actions. Three modes are supported:

1. **Dev mode** (`ActionModeDev`): References custom actions using local paths (e.g., `uses: ./actions/setup`)
2. **Release mode** (`ActionModeRelease`): References custom actions using SHA-pinned remote paths (e.g., `uses: github/gh-aw/actions/setup@sha`)
3. **Script mode** (`ActionModeScript`): Generates direct shell script calls instead of using GitHub Actions `uses:` syntax

This creates a complete development workflow:

1. Create and build custom actions (using build system described above)
2. Compile workflows with action mode enabled
3. Generated workflows reference actions according to the selected mode

### Action Mode Selection

Action modes can be configured in multiple ways with the following precedence (highest to lowest):

1. **CLI flag**: `gh aw compile --action-mode script`
2. **Feature flag**: `features.action-mode: "script"` in workflow frontmatter
3. **Environment variable**: `GH_AW_ACTION_MODE=script`
4. **Auto-detection**: Based on build flags and GitHub Actions context

### Implementation Details

#### 1. Action Mode Type (`pkg/workflow/action_mode.go`)

Defines the `ActionMode` enum type with three modes:
- **`ActionModeDev`**: References custom actions using local paths (default for development)
- **`ActionModeRelease`**: References custom actions using SHA-pinned remote paths (for production)
- **`ActionModeScript`**: Generates direct shell script calls instead of using `uses:` syntax

Includes validation methods `IsValid()`, `IsDev()`, `IsRelease()`, `IsScript()`, and `String()`.

#### 2. Compiler Support (`pkg/workflow/compiler_types.go`)

- Added `actionMode` field to `Compiler` struct
- Default mode is `ActionModeInline` for backward compatibility

#### 3. Script Registry Extensions (`pkg/workflow/script_registry.go`)

- Extended `scriptEntry` to include optional `actionPath` field
- Added `RegisterWithAction()` method to register scripts with custom action paths
- Added `GetActionPath()` method to retrieve action paths
- Maintained backward compatibility with existing `Register()` and `RegisterWithMode()` methods

#### 4. Custom Action Step Generation (`pkg/workflow/safe_outputs.go`)

- Added `buildCustomActionStep()` method to generate steps using custom action references
- Added token mapping helpers:
  - `addCustomActionGitHubToken()`
  - `addCustomActionCopilotGitHubToken()`
  - `addCustomActionAgentGitHubToken()`
- Updated `buildSafeOutputJob()` to choose between inline and action modes based on compiler settings
- Falls back to inline mode if action path is not registered

#### 5. Script Mode Implementation (`pkg/workflow/compiler_yaml_helpers.go`)

Script mode implements direct shell script execution instead of using GitHub Actions `uses:` syntax:

**Checkout Step** (`generateCheckoutActionsFolder`):
```yaml
- name: Checkout actions folder
  uses: actions/checkout@v5
  with:
    repository: github/gh-aw
    sparse-checkout: |
      actions
    path: /tmp/gh-aw/actions-source
    depth: 1
    persist-credentials: false
```

**Setup Step** (`generateSetupStep`):
```yaml
- name: Setup Scripts
  run: |
    bash /tmp/gh-aw/actions-source/actions/setup/setup.sh
  env:
    INPUT_DESTINATION: /opt/gh-aw/actions
```

**Key differences from dev/release modes:**
- Checks out the `github/gh-aw` repository instead of the workflow repository
- Uses sparse checkout to only fetch the `actions` folder
- Runs setup.sh script directly instead of using `uses: ./actions/setup` or `uses: github/gh-aw/actions/setup@sha`
- Shallow clone (`depth: 1`) for efficiency
- Environment variable `INPUT_DESTINATION` passed to setup script

**When script mode is active:**
- The compiler detects `features.action-mode: "script"` in workflow frontmatter
- All jobs that need custom actions get the checkout + script execution steps
- The setup.sh script copies JavaScript and shell scripts from `/tmp/gh-aw/actions-source/actions/setup/` to the destination

#### 6. Safe Output Job Configuration

- Extended `SafeOutputJobConfig` struct with `ScriptName` field
- Script name enables lookup of custom action path from registry
- Updated `create_issue.go` to pass script name as example implementation

### Usage Example

#### Step 1: Register Script with Action Path

```go
// Register script with action path
workflow.DefaultScriptRegistry.RegisterWithAction(
    "create_issue",
    createIssueScriptSource,
    workflow.RuntimeModeGitHubScript,
    "./actions/create-issue", // Must match action directory name
)
```

#### Step 2: Compile with Action Mode

**Dev mode** (local action references):
```go
compiler := workflow.NewCompilerWithVersion("1.0.0")
compiler.SetActionMode(workflow.ActionModeDev)
compiler.CompileWorkflow("workflow.md")
```

**Release mode** (SHA-pinned remote references):
```go
compiler := workflow.NewCompilerWithVersion("1.0.0")
compiler.SetActionMode(workflow.ActionModeRelease)
compiler.CompileWorkflow("workflow.md")
```

**Script mode** (direct shell script execution):
```go
compiler := workflow.NewCompilerWithVersion("1.0.0")
compiler.SetActionMode(workflow.ActionModeScript)
compiler.CompileWorkflow("workflow.md")
```

Or via frontmatter feature flag:
```yaml
---
name: Test Workflow
on: workflow_dispatch
features:
  action-mode: "script"
permissions:
  contents: read
---

Test workflow using script mode.
```

#### Step 3: Output Comparison

**With Dev Mode** (local action reference):
```yaml
jobs:
  create_issue:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout actions folder
        uses: actions/checkout@v5
        with:
          sparse-checkout: |
            actions
          path: /tmp/gh-aw/actions
      - name: Setup Scripts
        uses: ./actions/setup
        with:
          destination: /opt/gh-aw/actions
      - name: Create Output Issue
        id: create_issue
        uses: ./actions/create-issue
        env:
          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
```

**With Script Mode** (direct script execution):
```yaml
jobs:
  create_issue:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout actions folder
        uses: actions/checkout@v5
        with:
          repository: github/gh-aw
          sparse-checkout: |
            actions
          path: /tmp/gh-aw/actions-source
          depth: 1
          persist-credentials: false
      - name: Setup Scripts
        run: |
          bash /tmp/gh-aw/actions-source/actions/setup/setup.sh
        env:
          INPUT_DESTINATION: /opt/gh-aw/actions
      - name: Create Output Issue
        id: create_issue
        uses: ./actions/create-issue
        env:
          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
```

**With Inline Mode (default)** (embeds JavaScript):
```yaml
jobs:
  create_issue:
    runs-on: ubuntu-latest
    steps:
      - name: Create Output Issue
        id: create_issue
        uses: actions/github-script@SHA
        env:
          GH_AW_AGENT_OUTPUT: ${{ env.GH_AW_AGENT_OUTPUT }}
        with:
          github-token: ${{ secrets.GITHUB_TOKEN }}
          script: |
            // JavaScript code here
```text

### Design Decisions

1. **Registry-based approach**: Scripts are registered once with optional action paths, avoiding duplicate configuration
2. **Fallback strategy**: If action path not found, automatically falls back to inline mode
3. **Backward compatibility**: Default mode is inline, no breaking changes to existing workflows
4. **Token mapping**: Custom actions use `token` input instead of `github-token` parameter
5. **Reuse existing infrastructure**: Leverages the same script registry and bundler used for inline mode

### Integration Points

**With Build System**:
- Action paths registered in script registry match directories in `actions/`
- The action must exist and be built using `make actions-build`
- Example: `RegisterWithAction("create_issue", script, mode, "./actions/create-issue")`

**With Compiler**:
- `SetActionMode(ActionModeDev)` switches from inline to custom action references
- `buildSafeOutputJob()` checks mode and calls appropriate step builder
- Falls back gracefully if action path not registered

**With Safe Outputs**:
- `ScriptName` field in `SafeOutputJobConfig` enables action path lookup
- Each safe output type can specify its corresponding action name
- Token parameters are mapped to action inputs automatically

### Complete Workflow Example

This example demonstrates the full integration between the build system and dev action mode:

#### 1. Create a Custom Action

```bash
# Create action directory
mkdir -p actions/create-issue/src

# Create action.yml
cat > actions/create-issue/action.yml << 'EOF'
name: 'Create Issue'
description: 'Creates a GitHub issue from agent output'
inputs:
  token:
    description: 'GitHub token for API access'
    required: true
  agent-output:
    description: 'Path to agent output JSON file'
    required: true
runs:
  using: 'node24'
  main: 'index.js'
EOF

# Create src/index.js with FILES placeholder
# (See "Creating a New Action" section above for details)

# Update dependency mapping in pkg/cli/actions_build_command.go
# Build the action
make actions-build
```text

#### 2. Register and Compile

```go
// Register script with action path
workflow.DefaultScriptRegistry.RegisterWithAction(
    "create_issue",
    createIssueScriptSource,
    workflow.RuntimeModeGitHubScript,
    "./actions/create-issue",
)

// Compile with dev action mode
compiler := workflow.NewCompilerWithVersion("1.0.0")
compiler.SetActionMode(workflow.ActionModeDev)
compiler.CompileWorkflow("workflow.md")
```text

#### 3. Result

The compiled workflow will reference your custom action:

```yaml
jobs:
  create_issue:
    runs-on: ubuntu-latest
    steps:
      - uses: ./actions/create-issue
        with:
          token: ${{ secrets.GITHUB_TOKEN }}
          agent-output: /tmp/agent-output.json
```text

### Current Status

**✅ Completed**:
- Core infrastructure for action mode switching
- Script registry extension for action path mapping
- Custom action step generation logic
- Token input mapping for custom actions
- Backward compatibility (all existing tests pass)
- Unit tests with coverage metrics

**⚠️ Known Issues**:
- Custom action compilation tests show mode triggers correctly
- Action paths are registered and found successfully
- However, generated lock files still contain `actions/github-script` references
- Further investigation needed in YAML generation pipeline

**🔄 Next Steps**:
1. Debug step generation in lock file output
2. Add `--action-mode` CLI flag to compile command
3. Extend `ScriptName` to other safe output types (add_comment, create_pull_request, etc.)
4. Create corresponding actions in `actions/` directory for all safe outputs
5. Implement release mode with SHA-pinned references
6. Add end-to-end integration tests

### Future Enhancements

1. **Input parameter mapping**: Map environment variables to action inputs for better type safety
2. **Action output handling**: Support custom action outputs in addition to standard outputs
3. **Validation**: Add compile-time validation of action paths (check if action exists in `actions/` directory)
4. **Cache support**: Cache compiled custom actions for faster subsequent compilations
5. **Automatic action creation**: Generate action scaffold from script registry entries
6. **Release mode**: Support versioned action references like `github/gh-aw/.github/actions/create-issue@v1.0.0`
7. **CLI integration**: Add `--action-mode=dev|inline` flag to compile command

### Testing

Tests are located in `pkg/workflow/compiler_custom_actions_test.go`:
- ActionMode type validation
- Compiler action mode default and setter methods
- Script registry action path registration
- Custom action mode compilation
- Inline action mode compilation (default)
- Fallback behavior when action path not found

All existing tests pass, ensuring backward compatibility.
