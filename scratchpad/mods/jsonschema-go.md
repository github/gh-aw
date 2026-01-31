# Module: github.com/google/jsonschema-go

## Overview

**jsonschema-go** is Google's official Go library for working with JSON Schema, supporting schema creation, validation, and type inference per JSON Schema Draft 2020-12 specification. The library fully implements the JSON Schema Draft 2020-12 specification with no external dependencies beyond the Go standard library.

**Key Characteristics:**
- Zero external dependencies (only Go stdlib)
- Official Google project with MIT license
- Full JSON Schema Draft 2020-12 support
- Type-safe schema generation from Go structs
- Built-in validation engine
- Active maintenance and community support

**Maintainers:** Google (github.com/google)

## Version Used

Current version in `go.mod`: **v0.3.0**

## Usage in gh-aw

### Files Using This Module

- `pkg/cli/mcp_schema.go` - Core MCP tool schema generation
- `pkg/cli/mcp_schema_test.go` - Schema generation tests
- `pkg/cli/mcp_tool_schemas_test.go` - Integration tests with MCP tools

### Primary Use Case

The library is used for **automatic JSON schema generation for MCP (Model Context Protocol) tool outputs**. This enables type-safe integration between Go structs and JSON Schema requirements for AI agent tool interfaces.

### Key APIs Utilized

#### 1. `jsonschema.ForType()` - Schema Generation
```go
import "github.com/google/jsonschema-go/jsonschema"

schema, err := jsonschema.ForType(typ, &jsonschema.ForOptions{})
```text

**Purpose:** Generates JSON Schema from Go `reflect.Type`, automatically inferring schema properties from struct fields.

#### 2. `GenerateOutputSchema[T]()` - Type-Safe Helper
```go
func GenerateOutputSchema[T any]() (*jsonschema.Schema, error) {
    var zero T
    typ := reflect.TypeOf(zero)
    return jsonschema.ForType(typ, &jsonschema.ForOptions{})
}
```text

**Purpose:** Provides a generic wrapper for schema generation, leveraging Go 1.18+ generics for compile-time type safety.

### Usage Patterns Observed

1. **Struct Tag Integration:**
   - `json:` tags control field naming
   - `jsonschema:` tags provide descriptions and constraints
   - `omitempty` marks optional fields

2. **Type Safety:**
   - Generic function (`GenerateOutputSchema[T]`) ensures compile-time type checking
   - No runtime type assertions needed

3. **Schema Features Utilized:**
   - Object type schemas
   - Nested struct support
   - Optional field handling via pointers
   - Array/slice type mapping

### Example from Codebase

```go
type MyOutput struct {
    Name  string `json:"name" jsonschema:"description=Name of the item"`
    Count int    `json:"count,omitempty" jsonschema:"description=Number of items"`
}

schema, err := GenerateOutputSchema[MyOutput]()
tool := &mcp.Tool{
    Name:         "my-tool",
    Description:  "My tool description",
    OutputSchema: schema,
}
```text

## Research Summary

**Repository:** https://github.com/google/jsonschema-go

**Latest Version:** v0.3.0 (as of project usage)

### Key Features

1. **Automatic Schema Inference**
   - Generates schemas from Go types using reflection
   - Respects struct tags for customization
   - Supports complex type hierarchies

2. **Full JSON Schema Support**
   - Draft 2020-12 specification compliance
   - `$ref` resolution (local and remote)
   - Custom type mappings via `ForOptions.TypeSchemas`

3. **Validation Engine**
   - Direct validation of Go values against schemas
   - No intermediate JSON marshaling required for validation
   - Comprehensive error reporting

4. **Developer-Friendly API**
   - Concise function signatures following Go idioms
   - Good integration with Go idioms
   - Clear documentation and examples

### Recent Changes (v0.3.0 and Beyond)

Based on upstream activity and community discussions:

- **Enhanced Type Mapping:** Improved handling of pointer types for nullable fields
- **Better Error Messages:** More descriptive validation errors, especially for `additionalProperties` violations
- **Performance Improvements:** Optimizations in validation logic
- **TypeSchemas Key Update:** Changed from `any` to `reflect.Type` for better type safety
- **Struct Tag Support:** Comprehensive support for `json` and `jsonschema` struct tags

### Anticipated v0.4.0 Features

While not officially released, upstream development suggests:
- Deeper customization for schema inference
- Enhanced default value support
- More informative validation error messages
- Better meta-schema integration
- Improved handling of complex Go types

## Improvement Opportunities

### Quick Wins

1. **Consider Upgrade to Latest Version**
   - Monitor for v0.4.0 release with anticipated improvements
   - Current v0.3.0 is stable, but newer versions may offer performance gains

2. **Add Validation Usage**
   - Currently only using schema generation
   - Could add schema validation for MCP tool inputs/outputs
   - Would provide runtime type safety for AI agent interactions

3. **Leverage Schema Descriptions**
   - Expand `jsonschema:` tag usage for richer tool documentation
   - More detailed descriptions help AI agents understand tool outputs

### Feature Opportunities

1. **Custom Type Schemas**
   - Utilize `ForOptions.TypeSchemas` for domain-specific types
   - Map custom types (e.g., time.Time, url.URL) to specific JSON Schema formats

2. **Schema Validation in Tests**
   - Add validation tests for generated schemas
   - Ensure MCP tool outputs conform to generated schemas
   - Catch schema/implementation drift early

3. **Advanced Schema Features**
   - Add constraints: `minimum`, `maximum`, `pattern`, `enum`
   - Use `format` specifications: `date-time`, `email`, `uri`
   - Implement `default` values for optional fields

4. **Schema Caching**
   - Cache generated schemas to avoid repeated reflection
   - Especially beneficial for frequently called schema generation

### Best Practice Alignment

1. **Struct Tag Documentation**
   - ✅ Currently using `json:` and `jsonschema:` tags appropriately
   - ✅ Following Go conventions with `omitempty` for optional fields
   - 💡 Could add more detailed descriptions in `jsonschema:` tags

2. **Error Handling**
   - ✅ Proper error propagation in `GenerateOutputSchema`
   - ✅ Clear error messages for schema generation failures

3. **Type Safety**
   - ✅ Uses generics for compile-time type safety
   - ✅ No runtime type assertions needed
   - 💡 Could extend pattern to other schema operations

4. **Testing Coverage**
   - ✅ Comprehensive test coverage for schema generation
   - ✅ Tests for scalar types, nested objects, and array elements
   - 💡 Could add validation tests for schema correctness

### General Code Improvements

1. **Documentation Enhancement**
   - Add package-level documentation about MCP schema requirements
   - Document the relationship between struct tags and JSON Schema properties
   - Provide more examples in comments

2. **Expand Test Coverage**
   - Test edge cases: empty structs, deeply nested types
   - Test error conditions more thoroughly
   - Add benchmark tests for schema generation performance

3. **Schema Registry Pattern**
   - Consider implementing a registry for commonly used schemas
   - Reduces redundant schema generation
   - Provides centralized schema management

## API Patterns and Best Practices

### Recommended Usage Pattern

```go
// 1. Define struct with appropriate tags
type ToolOutput struct {
    Field1 string `json:"field1" jsonschema:"description=Field description"`
    Field2 int    `json:"field2,omitempty" jsonschema:"description=Optional field"`
}

// 2. Generate schema using generic helper
schema, err := GenerateOutputSchema[ToolOutput]()
if err != nil {
    return fmt.Errorf("failed to generate schema: %w", err)
}

// 3. Use schema in MCP tool definition
tool := &mcp.Tool{
    OutputSchema: schema,
}
```text

### Schema Generation Guidelines

1. **Use Descriptive Field Tags**
   - Always include `jsonschema:` descriptions
   - Descriptions help AI agents understand output structure
   - Be specific and actionable in descriptions

2. **Mark Optional Fields Properly**
   - Use pointer types for truly optional fields
   - Add `omitempty` to prevent null values in JSON
   - Consider `*string` vs `string` based on nullability needs

3. **Avoid Complex Types When Possible**
   - Use built-in Go types with direct JSON Schema mappings
   - Use structs for complex objects rather than maps
   - Document any custom type mappings

4. **Validate Generated Schemas**
   - Test that generated schemas match expectations
   - Verify schema properties align with struct fields
   - Check that required/optional semantics are correct

### Common Pitfalls to Avoid

1. **Missing Struct Tags**
   - Problem: Fields without tags may have unclear schema names
   - Solution: Always use `json:` tags for explicit field naming

2. **Incorrect Optional Field Handling**
   - Problem: Non-pointer fields with `omitempty` still required in schema
   - Solution: Use pointer types for truly optional fields

3. **Undocumented Fields**
   - Problem: Schema lacks descriptions for AI agent guidance
   - Solution: Add `jsonschema:"description=..."` to all fields

## Future Opportunities to Watch Upstream

1. **JSON Schema Draft 2020-12 Extensions**
   - Watch for new draft features and adoption
   - Enhanced validation capabilities
   - Better documentation generation support

2. **Performance Optimizations**
   - Schema generation caching mechanisms
   - Faster reflection-based type analysis
   - Reduced memory allocations

3. **Enhanced Type Support**
   - Better handling of Go's type system nuances
   - Support for more complex generic types
   - Custom scalar type mapping improvements

4. **Tooling Integration**
   - Code generation from schemas
   - Schema documentation generators
   - IDE integration and autocomplete

5. **Validation Features**
   - More detailed error reporting
   - Custom validator functions
   - Validation rule composition

## References

### Documentation
- **Package Documentation:** https://pkg.go.dev/github.com/google/jsonschema-go/jsonschema
- **Repository README:** https://github.com/google/jsonschema-go/blob/main/README.md
- **JSON Schema Specification:** https://json-schema.org/draft/2020-12/json-schema-core.html

### Repository
- **GitHub:** https://github.com/google/jsonschema-go
- **Issues:** https://github.com/google/jsonschema-go/issues
- **Releases:** https://github.com/google/jsonschema-go/releases

### Related Documentation
- **gh-aw MCP Schema Usage:** `pkg/cli/mcp_schema.go`
- **Test Examples:** `pkg/cli/mcp_schema_test.go`
- **MCP Tool Integration:** `pkg/cli/mcp_tool_schemas_test.go`

### Community Resources
- **JSON Schema Draft 2020-12:** https://json-schema.org/specification-links.html#2020-12
- **Go Package Context:** https://context7.com/google/jsonschema-go

---

**Last Reviewed:** 2025-12-18  
**Reviewer:** GitHub Copilot (automated via issue #6830)  
**Go Version:** 1.25.0  
**Module Version:** v0.3.0

---

*This summary was generated based on Go Fan analysis methodology. For the latest information, always check the upstream repository.*
