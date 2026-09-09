# parser Package

> Markdown frontmatter parsing, import resolution, GitHub URL handling, and schema validation for agentic workflow files.

## Overview

The `parser` package is responsible for extracting and processing YAML frontmatter from agentic workflow `.md` files. Frontmatter defines the workflow's entire configuration — triggers, permissions, tools, safe outputs, engine settings, network restrictions, and runtime overrides. The markdown body that follows the frontmatter serves as the AI agent's prompt text.

Beyond basic frontmatter extraction, the package provides a rich import system that resolves `@import` directives (local files, GitHub URLs, fragments), an include expander for `@include` directives in the markdown body, a schedule parser that converts natural-language schedules into cron expressions, MCP server configuration extraction, and JSON schema–backed validation with actionable error messages.

The package is designed for use both in the main CLI binary and in WebAssembly contexts (see `*_wasm.go` files). Build constraints separate platform-specific implementations for remote fetching and filesystem access.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `FrontmatterResult` | struct | Result of extracting frontmatter from markdown content |
| `ImportCache` | struct | Thread-safe cache of resolved imports to avoid redundant fetches |
| `ImportDirectiveMatch` | struct | Parsed `@import` or `@include` directive line |
| `ImportError` | struct | Structured error for import resolution failures |
| `ImportCycleError` | struct | Structured error for circular import chains |
| `FormattedParserError` | struct | Pre-formatted parser error with display-ready message |
| `ImportsResult` | struct | Result of `ProcessImportsFromFrontmatterWithSource` |
| `ImportInputDefinition` | struct | Input definition from an imported workflow fragment |
| `ImportSpec` | struct | Resolved import specification (path, ref, optional flag) |
| `GitHubURLType` | string alias | Classifies a GitHub URL (`tree`, `blob`, `raw`, `run`, `pr`, etc.) |
| `GitHubURLComponents` | struct | Parsed components of a GitHub URL (owner, repo, ref, path, etc.) |
| `JSONPathLocation` | struct | Line/column location of a JSON path in YAML content |
| `JSONPathInfo` | struct | JSON path with human-readable description |
| `NestedSection` | struct | Locates nested YAML sections for error reporting |
| `PathSegment` | struct | A single segment in a resolved JSON path |
| `RegistryMCPServerConfig` | struct | Parsed MCP server configuration (type, command, URL, env, etc.) |
| `MCPServerInfo` | struct | Metadata about an MCP server entry |
| `ScheduleParser` | struct | Converts natural-language schedules to cron expressions |
| `DeprecatedField` | struct | A deprecated frontmatter field with migration guidance |
| `FileReader` | func type | `func(filePath string) ([]byte, error)` — abstraction for file reading |
| `InlineSubAgent` | struct | A single inline sub-agent definition extracted via the `## agent: \`name\`` syntax |
| `InlineSkill` | struct | A single inline skill definition extracted via the `## skill: \`name\`` syntax |
| `BodyLevelImport` | struct | A `{{#runtime-import}}` directive found in the markdown body, with `Path` (workspace-root-relative) and `Optional` flag |
| `PromptImportEntry` | struct | A single import contribution to prompt assembly; either a runtime-import path or inlined markdown |

### Functions

#### Frontmatter Extraction

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractFrontmatterFromContent` | `func(content string) (*FrontmatterResult, error)` | Extracts YAML frontmatter between `---` delimiters from markdown |
| `ExtractFrontmatterFromBuiltinFile` | `func(path string, content []byte) (*FrontmatterResult, error)` | Extracts frontmatter from an embedded/built-in workflow file |
| `ExtractMarkdownContent` | `func(content string) (string, error)` | Returns the markdown body (everything after frontmatter) |
| `ExtractMarkdownSection` | `func(content, sectionName string) (string, error)` | Extracts a named `##` section from markdown |
| `ExtractWorkflowNameFromMarkdownBody` | `func(markdownBody, virtualPath string) (string, error)` | Derives the workflow name from the first `#` heading |
| `ExtractWorkflowNameFromContent` | `func(content, virtualPath string) (string, error)` | Combines frontmatter extraction and name derivation |

#### Import Processing

| Function | Signature | Description |
|----------|-----------|-------------|
| `ProcessImportsFromFrontmatterWithSource` | `func(frontmatter map[string]any, baseDir string, cache *ImportCache, ...) (*ImportsResult, error)` | Resolves all `@import` directives in frontmatter, merging imported configs |
| `ParseImportDirective` | `func(line string) *ImportDirectiveMatch` | Parses a single `@import` or `@include` line |
| `NewImportCache` | `func(repoRoot string) *ImportCache` | Creates a new import cache rooted at the repository |
| `ExpandIncludesWithManifest` | `func(content, baseDir string, extractTools bool) (string, []string, error)` | Expands `@include` directives in markdown body and returns included file paths |
| `ExpandIncludesForEngines` | `func(content, baseDir string) ([]string, error)` | Returns engine names referenced via `@include` |
| `ExpandIncludesForSafeOutputs` | `func(content, baseDir string) ([]string, error)` | Returns safe output types referenced via `@include` |
| `ExtractBodyLevelImportPaths` | `func(content, baseDir string) []BodyLevelImport` | Scans markdown body for `{{#runtime-import}}` directives and returns them as workspace-root-relative `BodyLevelImport` entries |

#### GitHub URL Parsing

| Function | Signature | Description |
|----------|-----------|-------------|
| `ParseGitHubURL` | `func(urlStr string) (*GitHubURLComponents, error)` | Parses any GitHub URL into structured components |
| `ParseRunURLExtended` | `func(input string) (*GitHubURLComponents, error)` | Parses a workflow run URL (extended formats) |
| `ParsePRURL` | `func(prURL string) (owner, repo string, prNumber int, err error)` | Parses a pull request URL |
| `ParseRepoFileURL` | `func(fileURL string) (owner, repo, ref, filePath string, err error)` | Parses a repository file URL |
| `IsValidGitHubIdentifier` | `func(s string) bool` | Validates a GitHub username/org identifier |
| `IsValidGitHubRepositoryName` | `func(s string) bool` | Validates a GitHub repository name |
| `GetGitHubHost` | `func() string` | Returns the GitHub host (supports GHES via `GH_HOST`) |
| `GetGitHubHostForRepo` | `func(owner, repo string) string` | Returns the GitHub host for a specific repo |
| `GetGitHubToken` | `func() (string, error)` | Returns the GitHub auth token from the environment |

#### Remote Fetching

| Function | Signature | Description |
|----------|-----------|-------------|
| `ResolveIncludePath` | `func(filePath, baseDir string, cache *ImportCache) (string, error)` | Resolves a relative or GitHub URL path to an absolute path or fetches remotely |
| `DownloadFileFromGitHub` | `func(ctx context.Context, owner, repo, path, ref string) ([]byte, error)` | Downloads a file from GitHub via the API |
| `DownloadFileFromGitHubForHost` | `func(ctx context.Context, owner, repo, path, ref, host string) ([]byte, error)` | Downloads a file from a specific GitHub host |
| `ResolveRefToSHAForHost` | `func(ctx context.Context, owner, repo, ref, host string) (string, error)` | Resolves a branch/tag ref to a commit SHA |
| `VerifyCommitExists` | `func(ctx context.Context, owner, repo, sha, host string) error` | Verifies that a specific commit SHA exists in the repository on the given host |
| `ListWorkflowFiles` | `func(ctx context.Context, owner, repo, ref, workflowPath string) ([]string, error)` | Lists workflow files in a remote repository |
| `ListWorkflowFilesForHost` | `func(ctx context.Context, owner, repo, ref, workflowPath, host string) ([]string, error)` | Lists workflow files in a remote repository on a specific GitHub host |
| `ListDirAllFilesForHost` | `func(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error)` | Lists all files (any extension) that are direct children of the given directory in a remote repository |
| `ListDirAllFilesRecursivelyForHost` | `func(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error)` | Lists all files under the given directory recursively in a remote repository |
| `ListDirSubdirsForHost` | `func(ctx context.Context, owner, repo, ref, dirPath, host string) ([]string, error)` | Lists subdirectory paths that are direct children of the given directory in a remote repository |
| `IsWorkflowSpec` | `func(path string) bool` | Returns whether a path is a workflow specification markdown file |

#### MCP Configuration

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractMCPConfigurations` | `func(frontmatter map[string]any, serverFilter string) ([]RegistryMCPServerConfig, error)` | Extracts all MCP server configurations from frontmatter |
| `ParseMCPConfig` | `func(toolName string, mcpSection any, toolConfig map[string]any) (RegistryMCPServerConfig, error)` | Parses a single MCP server entry |
| `IsMCPType` | `func(typeStr string) bool` | Validates an MCP transport type string |
| `IsSimpleSecretExpression` | `func(value string) bool` | Reports whether `value` is a direct `${{ secrets.NAME }}` GitHub Actions reference without additional expression operators |
| `ParseLinearToolsets` | `func(value any) ([]string, error)` | Validates and expands `tools.linear.toolsets` (a name or array of names) into the corresponding sorted, deduplicated MCP tool names; `"all"` expands to `["*"]` |
| `ValidateLinearAllowedForToolsets` | `func(allowed, toolsetTools []string) error` | Checks that every `tools.linear.allowed` glob pattern matches at least one tool in the configured toolsets (the `"*"` toolset accepts any pattern) |

#### Schedule Parsing

| Function | Signature | Description |
|----------|-----------|-------------|
| `ParseSchedule` | `func(input string) (cron, original string, err error)` | Parses natural-language or cron schedule to a cron expression |
| `ScatterSchedule` | `func(fuzzyCron, workflowIdentifier string) (string, error)` | Deterministically scatters a daily/hourly cron to reduce thundering herd |
| `IsDailyCron` | `func(cron string) bool` | Detects whether a cron expression runs daily |
| `IsHourlyCron` | `func(cron string) bool` | Detects whether a cron expression runs hourly |
| `IsWeeklyCron` | `func(cron string) bool` | Detects whether a cron expression runs weekly |
| `IsFuzzyCron` | `func(cron string) bool` | Detects whether a cron is a fuzzy wildcard |
| `IsCronExpression` | `func(input string) bool` | Detects whether a string is already a cron expression |

#### Schema Validation

| Function | Signature | Description |
|----------|-----------|-------------|
| `ValidateMainWorkflowFrontmatterWithSchemaAndLocation` | `func(frontmatter map[string]any, filePath string) error` | JSON-schema validates frontmatter and returns located errors |
| `ValidateIncludedFileFrontmatterWithSchemaAndLocation` | `func(frontmatter map[string]any, filePath string) error` | JSON-schema validates frontmatter of an included file fragment |
| `ValidateMCPConfigWithSchema` | `func(mcpConfig map[string]any) error` | JSON-schema validates an MCP server configuration map |
| `ValidateRepositoryPackageManifestWithSchemaAndLocation` | `func(manifest map[string]any, filePath string) error` | JSON-schema validates a repository package manifest |
| `GetCompiledRepoConfigSchema` | `func() (*jsonschema.Schema, error)` | Returns the compiled JSON schema for repo config |
| `GetSafeOutputTypeKeys` | `func() ([]string, error)` | Returns valid safe-output type keys from the schema |
| `GetMainWorkflowDeprecatedFields` | `func() ([]DeprecatedField, error)` | Returns deprecated frontmatter fields with migration notes |
| `FindDeprecatedFieldsInFrontmatter` | `func(map[string]any, []DeprecatedField) []DeprecatedField` | Finds deprecated fields present in a parsed frontmatter map |
| `GetMainWorkflowDeprecatedFieldsDeep` | `func() ([]DeprecatedField, error)` | Returns deprecated fields at any schema nesting level (e.g. `tools.grep`) with dot-separated paths |
| `FindDeprecatedFieldsInFrontmatterDeep` | `func(map[string]any, []DeprecatedField) []DeprecatedField` | Finds deprecated fields at any nesting depth in frontmatter using dot-separated paths |
| `FindClosestMatches` | `func(target string, candidates []string, maxResults int) []string` | Finds the closest string matches (for typo suggestions) |
| `CompileSchema` | `func(schemaJSON, schemaURL string) (*jsonschema.Schema, error)` | Compiles a JSON schema from a JSON string |

#### Frontmatter Hashing

| Function | Signature | Description |
|----------|-----------|-------------|
| `ComputeFrontmatterHashFromFile` | `func(filePath string, cache *ImportCache) (string, error)` | Computes a stable hash of a workflow's frontmatter (including imports) |
| `ComputeFrontmatterHashFromParsedContent` | `func(frontmatterText, markdownBody string, parsedFrontmatter map[string]any, baseDir string, cache *ImportCache, fileReader FileReader) (string, error)` | Computes hash from already-extracted frontmatter text and markdown body |
| `ComputeFrontmatterHashFromFileWithParsedFrontmatter` | `func(filePath string, parsedFrontmatter map[string]any, ...) (string, error)` | Computes hash from already-parsed frontmatter |
| `ComputeFrontmatterHashFromFileWithReader` | `func(filePath string, cache *ImportCache, fileReader FileReader) (string, error)` | Computes hash with a custom file reader |
| `ComputeBodyHashFromParsedContent` | `func(markdownBody, frontmatterText, baseDir string, fileReader FileReader) (string, error)` | Computes a stable hash of a workflow's markdown body, including the bodies of all transitively imported files |
| `ComputeBodyHashFromFile` | `func(filePath string) (string, error)` | Computes the body hash for a workflow file from disk |

#### Error Formatting

| Function | Signature | Description |
|----------|-----------|-------------|
| `FormatImportCycleError` | `func(*ImportCycleError) error` | Formats a cycle error with the import chain |
| `FormatImportError` | `func(*ImportError, yamlContent string) error` | Formats an import error with YAML context |
| `NewFormattedParserError` | `func(formatted string) *FormattedParserError` | Creates a pre-formatted parser error |
| `FormatYAMLError` | `func(err error, frontmatterLineOffset int, sourceYAML string) string` | Formats a YAML error with source code context, adjusting line numbers by the frontmatter offset |
| `TranslateYAMLMessage` | `func(message string) string` | Translates a cryptic YAML parser message to a user-friendly description |

#### JSON Path Location

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractJSONPathFromValidationError` | `func(err error) []JSONPathInfo` | Extracts JSON path info from a schema validation error |
| `LocateJSONPathInYAML` | `func(yamlContent, jsonPath string) JSONPathLocation` | Maps a JSON path to a line number in YAML text |
| `LocateJSONPathForPathInfo` | `func(yamlContent string, info JSONPathInfo) JSONPathLocation` | Maps a `JSONPathInfo` to a line/column in YAML text, handling additional-property errors |

#### Trigger Helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `IsLabelOnlyEvent` | `func(eventValue any) bool` | Detects whether a trigger only activates on label events |
| `IsNonConflictingCommandEvent` | `func(eventValue any) bool` | Detects whether a trigger is a non-conflicting slash command |

#### Inline Sub-Agent Processing

Inline sub-agents are secondary agent definitions embedded in the same markdown file as the primary workflow, delimited by `## agent: \`name\`` level-2 headings. Each sub-agent may carry its own frontmatter block (only `description` and `model` are valid fields) plus a prompt body.

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractInlineSubAgents` | `func(markdown string) (mainMarkdown string, agents []InlineSubAgent, err error)` | Splits markdown into the main workflow section and any inline sub-agent definitions |
| `ValidateInlineSubAgentsFrontmatter` | `func(markdown string) []string` | Validates inline sub-agent frontmatter in a full workflow file (strips top-level frontmatter first); returns advisory warning strings |
| `ValidateInlineSubAgentsInBody` | `func(body string) []string` | Validates inline sub-agent frontmatter in an already-stripped markdown body |
| `GetEngineSubAgentExt` | `func(engineID string) string` | Returns the file extension for sub-agent files for a given engine (`.md` for `claude`/`codex`/`gemini`, `.agent.md` otherwise) |

#### Inline Skill Processing

Inline skills are secondary skill definitions embedded in the same markdown file as the primary workflow, delimited by `## skill: \`name\`` level-2 headings. Each skill may carry its own frontmatter block (only `description` is a valid field) plus a content body.

| Function | Signature | Description |
|----------|-----------|-------------|
| `ExtractInlineSkills` | `func(markdown string) (mainMarkdown string, skills []InlineSkill, err error)` | Splits markdown into the main workflow section and any inline skill definitions |
| `ValidateInlineSkillsFrontmatter` | `func(markdown string) []string` | Validates inline skill frontmatter in a full workflow file (strips top-level frontmatter first); returns advisory warning strings |
| `ValidateInlineSkillsInBody` | `func(body string) []string` | Validates inline skill frontmatter in an already-stripped markdown body |

#### Virtual Filesystem and Workflow Update Helpers

| Function | Signature | Description |
|----------|-----------|-------------|
| `RegisterBuiltinVirtualFile` | `func(path string, content []byte)` | Registers embedded virtual file content under an `@builtin:` path |
| `BuiltinVirtualFileExists` | `func(path string) bool` | Returns whether a built-in virtual file path has been registered |
| `GetBuiltinFrontmatterCache` | `func(path string) (*FrontmatterResult, bool)` | Gets cached frontmatter parse results for built-in virtual files |
| `SetBuiltinFrontmatterCache` | `func(path string, result *FrontmatterResult) *FrontmatterResult` | Stores a frontmatter parse result in the built-in cache |
| `ReadFile` | `func(path string) ([]byte, error)` | Reads file content through parser virtual/builtin-aware file resolution |
| `SetVirtualFiles` | `func(files map[string][]byte)` | *(WASM only — `//go:build js \|\| wasm`)* Populates the in-memory virtual filesystem used by the WASM runtime; keys are workspace-relative file paths |
| `ClearVirtualFiles` | `func()` | *(WASM only)* Removes all entries from the in-memory virtual filesystem |
| `VirtualFileExists` | `func(path string) bool` | *(WASM only)* Returns whether a path is present in the in-memory virtual filesystem |
| `MergeTools` | `func(base, additional map[string]any) (map[string]any, error)` | Merges two tool configuration maps with MCP-aware conflict handling |
| `UpdateWorkflowFrontmatter` | `func(workflowPath string, updateFunc func(frontmatter map[string]any) error, verbose bool) error` | Reads, updates, and rewrites workflow frontmatter with a callback |
| `ReconstructWorkflowFile` | `func(frontmatterYAML, markdownContent string) (string, error)` | Reconstructs a complete workflow file string from frontmatter YAML and markdown content |
| `EnsureToolsSection` | `func(frontmatter map[string]any) map[string]any` | Ensures `tools` exists and is a map in frontmatter |
| `QuoteCronExpressions` | `func(yamlContent string) string` | Ensures schedule cron values in YAML are quoted |

### Constants / Variables

| Name | Type | Description |
|------|------|-------------|
| `BuiltinPathPrefix` | `string` | Path prefix `"@builtin:"` used to identify registered virtual built-in files |
| `ValidMCPTypes` | `[]string` | Valid MCP transport types: `"stdio"`, `"http"`, `"local"` |
| `IncludeDirectivePattern` | `*regexp.Regexp` | Matches `@import`, `@include`, and `{{#import ...}}` directives |
| `LegacyIncludeDirectivePattern` | `*regexp.Regexp` | Matches legacy `@import`/`@include` forms |
| `DefaultFileReader` | `FileReader` | Default file reader using `os.ReadFile` |
| `RepoConfigSchema` | `string` | Embedded JSON schema for repo-level configuration |

## Usage Examples

### Parse frontmatter from a workflow file

```go
content, _ := os.ReadFile("my-workflow.md")
result, err := parser.ExtractFrontmatterFromContent(string(content))
if err != nil {
    log.Fatal(err)
}
fmt.Println("Triggers:", result.Frontmatter["on"])
fmt.Println("Prompt:", result.Markdown)
```

### Resolve imports

```go
cache := parser.NewImportCache("/path/to/repo")
imports, err := parser.ProcessImportsFromFrontmatterWithSource(
    result.Frontmatter,
    filepath.Dir("my-workflow.md"),
    cache,
    "my-workflow.md",
    result.FrontmatterYAML,
)
```

### Parse a schedule

```go
cron, original, err := parser.ParseSchedule("every day at 9am")
// cron = "0 9 * * *"
```

### Validate frontmatter

```go
err := parser.ValidateMainWorkflowFrontmatterWithSchemaAndLocation(
    frontmatter, "my-workflow.md",
)
```

### Extract MCP server configurations

```go
servers, err := parser.ExtractMCPConfigurations(frontmatter, "")
for _, s := range servers {
    fmt.Printf("%s: type=%s\n", s.Name, s.Type)
}
```

## Architecture

The parsing pipeline for a workflow file proceeds as:

1. **Read** the raw markdown file content.
2. **Extract** the YAML frontmatter block between `---` delimiters (`ExtractFrontmatterFromContent`).
3. **Process imports**: resolve all `@import` directives recursively, merge imported YAML configurations, and deduplicate (`ProcessImportsFromFrontmatterWithSource`).
4. **Validate** the merged frontmatter against the JSON schema (`ValidateMainWorkflowFrontmatterWithSchemaAndLocation`).
5. **Expand includes** in the markdown body (`ExpandIncludesWithManifest`).
6. **Pass** the merged frontmatter and markdown body to `pkg/workflow` for compilation.

Import caching is crucial for performance and cycle detection. The `ImportCache` tracks visited paths within a single compilation run to prevent infinite recursion.

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/console` — parser-facing warning/error message formatting
- `github.com/github/gh-aw/pkg/constants` — shared parser constants and default values
- `github.com/github/gh-aw/pkg/envutil` — reads environment variables (e.g. `GITHUB_TOKEN`, `GH_TOKEN`) with consistent default handling
- `github.com/github/gh-aw/pkg/errorutil` — shared error classification helpers for remote fetch and import errors
- `github.com/github/gh-aw/pkg/fileutil` — file existence and path helper utilities
- `github.com/github/gh-aw/pkg/gitutil` — Git remote and host detection helpers
- `github.com/github/gh-aw/pkg/importinpututil` — input-value resolution and formatting utilities for `@import` directive substitution
- `github.com/github/gh-aw/pkg/jsonutil` — compact JSON marshaling for frontmatter hash computation
- `github.com/github/gh-aw/pkg/types` — `BaseMCPServerConfig`
- `github.com/github/gh-aw/pkg/typeutil` — safe type conversion helpers for dynamic frontmatter
- `github.com/github/gh-aw/pkg/logger` — debug logging
- `github.com/github/gh-aw/pkg/setutil` — set membership helpers used in import BFS traversal and cycle detection
- `github.com/github/gh-aw/pkg/sliceutil` — slice helper utilities for validation and merging
- `github.com/github/gh-aw/pkg/stringutil` — string normalization and ANSI/format helpers
- `github.com/github/gh-aw/pkg/syncutil` — thread-safe one-shot caching (used for lazy JSON schema compilation)

**Test-only**:
- `github.com/github/gh-aw/pkg/testutil` — shared test fixtures and assertion helpers used by parser package tests

**External**:
- `github.com/santhosh-tekuri/jsonschema/v6` — JSON schema validation
- `github.com/goccy/go-yaml` — YAML 1.1/1.2 compatible parsing (for GitHub Actions compatibility)
- `github.com/cli/go-gh/v2` — GitHub CLI API integration for remote file fetching
- `github.com/modelcontextprotocol/go-sdk/mcp` — MCP Go SDK for MCP server configuration

## Thread Safety

`ImportCache` is designed for use within a single goroutine per compilation run. Its internal map is not concurrency-safe. For concurrent compilations, create a separate `ImportCache` per compilation.

The `DefaultFileReader` variable is safe to read but MUST NOT be mutated after package initialization. Tests may replace it with a custom `FileReader` to inject virtual filesystem content.

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 24 |
| Constants | 10 |
| Variables | 5 |
| Functions and methods | 96 |
| Additional symbols documented in this appendix | 19 |

### Additional types

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `mcp.go` | `MCPRootInfo` | `type MCPRootInfo struct { URI string Name string }` | MCPRootInfo contains display metadata inferred from MCP server roots. |

### Additional constants and variables

| File | Kind | Symbol | Declaration | Description |
|------|------|--------|-------------|-------------|
| `remote_resolve_sha.go` | `var` | `ErrVerificationSkipped` | `var ErrVerificationSkipped = errors.New("commit verification skipped")` | ErrVerificationSkipped is returned when commit verification cannot be completed due to auth/permission constraints. |
| `schedule_parser.go` | `var` | `ErrUnsupportedSyntax` | `var ErrUnsupportedSyntax = errors.New("unsupported schedule syntax")` | ErrUnsupportedSyntax marks schedule inputs that are intentionally unsupported and should be rewritten to fuzzy or cron forms. |
| `github_urls.go` | `const` | `URLTypeBlob` | `const URLTypeBlob GitHubURLType = "blob"` | File blob view |
| `github_urls.go` | `const` | `URLTypeIssue` | `const URLTypeIssue GitHubURLType = "issue"` | Issue |
| `github_urls.go` | `const` | `URLTypePullRequest` | `const URLTypePullRequest GitHubURLType = "pull"` | Pull request |
| `github_urls.go` | `const` | `URLTypeRaw` | `const URLTypeRaw GitHubURLType = "raw"` | Raw file view |
| `github_urls.go` | `const` | `URLTypeRawContent` | `const URLTypeRawContent GitHubURLType = "rawcontent"` | raw. |
| `github_urls.go` | `const` | `URLTypeRun` | `const URLTypeRun GitHubURLType = "run"` | GitHub Actions run |
| `github_urls.go` | `const` | `URLTypeTree` | `const URLTypeTree GitHubURLType = "tree"` | Directory tree view |
| `github_urls.go` | `const` | `URLTypeUnknown` | `const URLTypeUnknown GitHubURLType = "unknown"` | Unknown type |
| `import_cache.go` | `const` | `ImportCacheDir` | `const ImportCacheDir = ".github/aw/imports"` | ImportCacheDir is the directory where cached imports are stored |

### Additional functions and methods

| File | Symbol | Declaration | Description |
|------|--------|-------------|-------------|
| `github.go` | `IsAnyGitHubHostEnvVarSet` | `func IsAnyGitHubHostEnvVarSet() bool` | IsAnyGitHubHostEnvVarSet returns true when any GitHub host override environment variable is set. |
| `github_urls.go` | `IsGitHubHost` | `func IsGitHubHost(host string) bool` | IsGitHubHost returns true for recognized GitHub and GHES hostnames. |
| `import_cache.go` | `(*ImportCache).Get` | `func (*ImportCache).Get(owner, repo, path, sha string) (string, bool)` | Get retrieves a cached file path if it exists sha parameter should be the resolved commit SHA |
| `import_cache.go` | `(*ImportCache).GetCacheDir` | `func (*ImportCache).GetCacheDir() string` | GetCacheDir returns the base cache directory path |
| `import_cache.go` | `(*ImportCache).Set` | `func (*ImportCache).Set(owner, repo, path, sha string, content []byte) (string, error)` | Set stores a new cache entry by saving the content to the cache directory sha parameter should be the resolved commit SHA |
| `import_error.go` | `(*FormattedParserError).Unwrap` | `func (*FormattedParserError).Unwrap() error` | Exported function or method declared in `import_error.go`. |
| `schema_validation.go` | `IsImportSafeSharedWorkflowOn` | `func IsImportSafeSharedWorkflowOn(onValue any) bool` | IsImportSafeSharedWorkflowOn validates whether an imported `on:` block is restricted to safe shared-workflow triggers. |
| `mcp.go` | `IsSimpleSecretExpression` | `func IsSimpleSecretExpression(value string) bool` | Reports whether value is a direct GitHub Actions secrets reference without additional expression operators. |
| `linear_toolsets.go` | `ParseLinearToolsets` | `func ParseLinearToolsets(value any) ([]string, error)` | Validates and expands Linear toolsets into MCP tool names. |
| `linear_toolsets.go` | `ValidateLinearAllowedForToolsets` | `func ValidateLinearAllowedForToolsets(allowed, toolsetTools []string) error` | Checks that every allowed pattern selects at least one tool from the configured Linear toolsets. |

<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

## Source Synchronization

Reviewed against recent source updates on 2026-07-24; no additional public-contract deltas were identified beyond the sections above. Re-verified on 2026-08-14; no public-contract changes since the last review (only internal schema-suggestions refactoring landed). Re-verified on 2026-08-29; no public-contract deltas since the last review. Re-verified on 2026-09-03; no public-contract deltas since the last review. Re-verified on 2026-09-08; added `ParseLinearToolsets`, `ValidateLinearAllowedForToolsets`, and `IsSimpleSecretExpression` (Linear toolset support and secret-expression validation) to the MCP Configuration public API table, which were previously undocumented.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
