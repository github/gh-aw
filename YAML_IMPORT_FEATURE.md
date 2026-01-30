# YAML Workflow Import Support

## Summary

This PR adds support for importing existing classic GitHub Actions workflows (`.yml` files) into gh-aw markdown workflows. This allows users to:

1. **Reuse existing GitHub Actions workflows** - Import jobs from classic `.yml` workflows into new gh-aw markdown workflows
2. **Migrate incrementally** - Keep existing workflows and gradually adopt gh-aw by importing them
3. **Share common jobs** - Extract common CI/CD jobs into reusable YAML files that can be imported

## Features

### File Type Detection
- ✅ Detects `.yml` and `.yaml` files (case-insensitive)
- ✅ Rejects `.lock.yml` files with clear error message (these are compiled outputs)
- ✅ Distinguishes between `action.yml` (GitHub Action definitions) and workflow files
- ✅ Validates that imported files are valid GitHub Actions workflows

### Job and Service Extraction
- ✅ Extracts all jobs from imported YAML workflows
- ✅ Extracts services from job definitions (prefixed with job name to avoid conflicts)
- ✅ Preserves job dependencies (`needs` field)
- ✅ Preserves all job configuration (runs-on, steps, environment, etc.)

### Merge Behavior
- ✅ Main workflow jobs take precedence over imported jobs (override behavior)
- ✅ Imported jobs are added to the workflow if not already defined
- ✅ Services from YAML workflows are properly converted and merged

## Usage

### Basic Import

```markdown
---
name: My Workflow
on: issue_comment
imports:
  - ci-workflow.yml
engine: copilot
---

# My Workflow

This workflow imports a YAML workflow file.
```

### Example YAML Workflow

```yaml
name: CI Workflow
on:
  push:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v3
      - run: npm test
  
  build:
    runs-on: ubuntu-latest
    needs: test
    steps:
      - uses: actions/checkout@v3
      - run: npm run build
```

When compiled, the jobs `test` and `build` from the YAML workflow are merged into the final workflow.

## Implementation Details

### Parser Changes
- **`pkg/parser/yaml_import.go`** - New file with YAML workflow parsing logic
  - `isYAMLWorkflowFile()` - Detects YAML workflow files
  - `isActionDefinitionFile()` - Distinguishes actions from workflows
  - `processYAMLWorkflowImport()` - Extracts jobs and services from YAML

- **`pkg/parser/import_processor.go`** - Updated to handle YAML imports
  - Added YAML workflow detection in import processing
  - Early validation to reject `.lock.yml` files
  - Jobs and services extracted and stored in `ImportsResult`

### Compiler Changes
- **`pkg/workflow/compiler_orchestrator_workflow.go`** - Updated to merge imported jobs
  - `mergeJobsFromYAMLImports()` - Merges jobs from imported YAML workflows
  - Main workflow jobs override imported jobs (no conflicts)

### Test Coverage
- **Unit tests** - `pkg/parser/yaml_import_test.go`
  - File type detection
  - Action definition rejection
  - Invalid workflow rejection
  - Job and service extraction

- **End-to-end tests** - `pkg/parser/yaml_import_e2e_test.go`
  - Full import workflow with multiple jobs
  - Service extraction and merging
  - Job dependency preservation

- **Example workflow** - `.github/workflows/test-yaml-import.md`
  - Demonstrates YAML import feature
  - Imports `example-ci-workflow.yml`
  - Verified job merging in compiled output

## Error Handling

### Rejected Files
- **`.lock.yml` files**: Cannot be imported (these are compiled outputs)
  ```
  Error: cannot import .lock.yml files: 'workflow.lock.yml'. 
  Lock files are compiled outputs from gh-aw. Import the source .md file instead
  ```

- **`action.yml` files**: Cannot be imported (these are action definitions, not workflows)
  ```
  Error: cannot import action definition file (action.yml). 
  Only workflow files (.yml) can be imported
  ```

- **Invalid YAML**: Files that are not valid GitHub Actions workflows are rejected
  ```
  Error: not a valid GitHub Actions workflow: missing 'on' or 'jobs' field
  ```

## Benefits

1. **Backward Compatibility** - Existing YAML workflows can be imported without modification
2. **Incremental Migration** - Teams can adopt gh-aw gradually by importing existing workflows
3. **Code Reuse** - Common CI/CD jobs can be extracted into reusable YAML files
4. **Job Composition** - Combine jobs from multiple sources (markdown + YAML imports)

## Testing

All tests pass:
- ✅ `make fmt` - Code formatting
- ✅ `make lint` - Linter validation
- ✅ `make test-unit` - Unit tests
- ✅ Example workflow compilation successful

## Future Enhancements

Potential improvements for future iterations:
- Support for importing workflow-level configurations (env, defaults, etc.)
- Support for importing reusable workflows (workflow_call)
- Enhanced conflict resolution strategies
- Documentation generation from imported workflows
