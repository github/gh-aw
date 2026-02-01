# Serena Language Server Tool

Serena is an **advanced static analysis tool** that provides deep code understanding through language server capabilities. It is designed for **advanced coding tasks** that require sophisticated code analysis.

## When to Use Serena

⚠️ **Important**: Serena should only be suggested when the user **explicitly requests advanced static analysis** for specific programming languages.

**Appropriate use cases:**
- Deep code refactoring requiring semantic understanding
- Complex code transformations across multiple files
- Advanced type checking and inference
- Cross-file dependency analysis
- Language-specific semantic analysis

**NOT recommended for:**
- Simple file editing or text manipulation
- Basic code generation
- General workflow automation
- Tasks that don't require deep code understanding

## Configuration

When a user explicitly asks for advanced static analysis capabilities:

```yaml
tools:
  serena: ["<language>"]  # Specify the programming language(s)
```

**Supported languages:**
- `go`
- `typescript`
- `python`
- `ruby`
- `rust`
- `java`
- `cpp`
- `csharp`
- And many more (see `.serena/project.yml` for full list)

## Detection and Usage

**To detect the repository's primary language:**
1. Check file extensions in the repository
2. Look for language-specific files:
   - `go.mod` → Go
   - `package.json` → TypeScript/JavaScript
   - `requirements.txt` or `pyproject.toml` → Python
   - `Cargo.toml` → Rust
   - `pom.xml` or `build.gradle` → Java
   - etc.

## Example Configuration

```yaml
tools:
  serena: ["go"]  # For Go repositories requiring advanced static analysis
```

## User Interaction Pattern

When a user describes a task:

1. **Analyze the request**: Does it require advanced static analysis?
2. **If NO**: Use standard tools (bash, edit, etc.)
3. **If YES**: Ask the user if they want to use Serena for advanced static analysis
4. **Only add Serena** if the user explicitly confirms

**Example conversation:**

User: "I need to refactor the authentication logic across multiple files with proper type safety"

Agent: "This task involves complex cross-file refactoring. Would you like me to use the Serena language server for advanced static analysis? This will provide deeper code understanding but adds complexity to the workflow."

User: "Yes, use Serena"

Agent: *Adds `serena: ["go"]` to the workflow configuration*

## Best Practices

- **Default to simpler tools** when possible
- **Only suggest Serena** for genuinely complex analysis tasks
- **Ask before adding** Serena to a workflow
- **Explain the benefits** when suggesting Serena
- **Consider the trade-offs** (added complexity vs. better analysis)
