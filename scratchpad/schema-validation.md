# JSON Schema Validation

## Overview

Both JSON schema files in this repository enforce strict validation by having `"additionalProperties": false` at the root level, which prevents typos and undefined fields from silently passing validation.

## Schema Files

### 1. Main Workflow Schema
- **File**: `pkg/parser/schemas/main_workflow_schema.json`
- **Root property**: `"additionalProperties": false` (line 3002)
- **Purpose**: Validates agentic workflow frontmatter in `.github/workflows/*.md` files

### 2. MCP Config Schema
- **File**: `pkg/parser/schemas/mcp_config_schema.json`
- **Root property**: `"additionalProperties": false` (line 99)
- **Purpose**: Validates MCP (Model Context Protocol) server configuration

## How It Works

When `"additionalProperties": false` is set at the root level of a JSON schema, the validator will reject any properties that are not explicitly defined in the schema's `properties` section. This catches common typos like:

- `permisions` instead of `permissions`
- `engnie` instead of `engine`
- `toolz` instead of `tools`
- `timeout_minute` instead of `timeout-minutes`
- `runs_on` instead of `runs-on`
- `safe_outputs` instead of `safe-outputs`
- `mcp_servers` instead of `mcp-servers`

## Testing

Test coverage is provided in:
- **File**: `pkg/parser/schema_additional_properties_test.go`
- **Test cases**: Tests for common typos and validation
  - Tests for common typos in main workflow schema
  - Tests for typos in MCP config schema
  - Tests to verify valid properties are still accepted

Run tests with:
```bash
make test-unit
# or
go test -v -run TestAdditionalPropertiesFalse ./pkg/parser/
```text

## Example Validation Error

When a workflow contains a typo, the compiler provides a clear error message:

```bash
$ ./gh-aw compile workflow-with-typo.md
✗ error: Unknown properties: toolz, engnie, permisions. Valid fields are: tools, engine, permissions, ...
```text

## Validation Process

1. The compiler reads the workflow frontmatter (YAML between `---` markers)
2. Parses it into a map structure
3. Validates against the appropriate JSON schema using the `jsonschema` library
4. If validation fails, provides a detailed error message with:
   - The invalid field name(s)
   - File location (line and column)
   - List of valid field names

## Adding New Fields

When adding new fields to any of the two schemas:

1. **Update the schema JSON file** with the new property definition
2. **Rebuild the binary** with `make build` (schemas are embedded using `//go:embed`)
3. **Add test cases** to verify the new field works correctly
4. **Update documentation** if the field is user-facing

⚠️ **Important**: Schema files are embedded in the binary at compile time, so changes require rebuilding.

## Schema Embedded in Binary

The schemas are embedded in the Go binary using `//go:embed` directives in `pkg/parser/schema.go`:

```go
//go:embed schemas/main_workflow_schema.json
var mainWorkflowSchema string

//go:embed schemas/mcp_config_schema.json
var mcpConfigSchema string
```text

This means:
- Schema changes require running `make build` to take effect
- The schemas are validated at runtime, not at build time
- No external JSON files need to be distributed with the binary

## Related Files

- `pkg/parser/schema.go` - Schema validation logic
- `pkg/parser/schema_test.go` - General schema validation tests
- `pkg/parser/schema_additional_properties_test.go` - Tests for additionalProperties validation
- `pkg/parser/frontmatter.go` - Frontmatter parsing logic

