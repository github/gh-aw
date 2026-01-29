# Repo-Memory Feature Specification

**Status**: Active  
**Last Updated**: 2025-01-02  
**Owners**: GitHub Agentic Workflows Team

## Overview

Repo-memory provides persistent, git-backed storage for AI agents across workflow runs. This feature allows agents to maintain state, notes, and artifacts in dedicated git branches, with automatic synchronization between workflow execution and git storage.

## Architecture

### System Components

```
┌─────────────────────────────────────────────────────────────┐
│                    Workflow Execution                        │
│                                                              │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Agent Job                                            │  │
│  │  - Clone memory branch to /tmp/gh-aw/repo-memory/{id}│  │
│  │  - Agent reads/writes files                          │  │
│  │  - Upload as artifact: repo-memory-{id}              │  │
│  └──────────────────────────────────────────────────────┘  │
│                          ↓                                   │
│  ┌──────────────────────────────────────────────────────┐  │
│  │  Push Repo Memory Job                                 │  │
│  │  - Download artifact: repo-memory-{id}               │  │
│  │  - Validate files (size, count, patterns)            │  │
│  │  - Commit and push to memory/{id} branch             │  │
│  └──────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                          ↓
         ┌────────────────────────────────┐
         │   Git Repository Storage        │
         │   Branch: memory/{id}           │
         └────────────────────────────────┘
```

### Data Flow

1. **Clone Phase** (Agent Job Start)
   - Shell script: `clone_repo_memory_branch.sh`
   - Clones `memory/{id}` branch to `/tmp/gh-aw/repo-memory/{id}`
   - Creates orphan branch if it doesn't exist (when `create-orphan: true`)

2. **Execution Phase** (Agent Job)
   - Agent reads/writes files in `/tmp/gh-aw/repo-memory/{id}/`
   - Prompt informs agent about available memory locations
   - Files persist in memory throughout job execution

3. **Upload Phase** (Agent Job End)
   - Uploads directory as GitHub Actions artifact: `repo-memory-{id}`
   - Runs even if job fails (`if: always()`)

4. **Download Phase** (Push Job Start)
   - Downloads artifact to `/tmp/gh-aw/repo-memory/{id}`
   - Validates files against constraints (size, count, patterns)

5. **Push Phase** (Push Job End)
   - JavaScript: `push_repo_memory.cjs`
   - Commits files to `memory/{id}` branch
   - Pushes to repository with merge strategy (`-X ours`)

## File Path Conventions

All paths in the repo-memory system follow strict naming conventions to ensure consistency across layers.

### Primary Path Patterns

| Pattern | Format | Example | Layer | Purpose |
|---------|--------|---------|-------|---------|
| **Memory Directory** | `/tmp/gh-aw/repo-memory/{memory-id}` | `/tmp/gh-aw/repo-memory/default` | All | Runtime directory where agent reads/writes |
| **Artifact Name** | `repo-memory-{memory-id}` | `repo-memory-default` | Go, YAML | GitHub Actions artifact identifier |
| **Branch Name** | `memory/{memory-id}` | `memory/default` | All | Git branch for persistent storage |
| **Prompt Path** | `/tmp/gh-aw/repo-memory/{memory-id}/` | `/tmp/gh-aw/repo-memory/default/` | Go (Prompt) | Path shown to agent (with trailing slash) |

### Path Consistency Rules

1. **Memory ID Format**
   - Must be alphanumeric with hyphens: `[a-zA-Z0-9-]+`
   - Default memory uses ID: `default`
   - Campaign memory uses ID: `campaigns`

2. **Directory Path Construction**
   ```go
   memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)
   ```
   - Used in: `repo_memory.go` (lines 351, 384, 467)
   - Used in: `repo_memory_prompt.go` (lines 44, 93)

3. **Artifact Name Construction**
   ```go
   fmt.Fprintf(builder, "name: repo-memory-%s\n", memory.ID)
   ```
   - Upload artifact: `repo_memory.go` line 358
   - Download artifact: `repo_memory.go` line 527

4. **Branch Name Construction**
   ```go
   func generateDefaultBranchName(memoryID string) string {
       if memoryID == "default" {
           return "memory/default"
       }
       return fmt.Sprintf("memory/%s", memoryID)
   }
   ```
   - Used in: `repo_memory.go` lines 52-58

5. **Prompt Display Path**
   - Always includes trailing slash: `/tmp/gh-aw/repo-memory/{memory-id}/`
   - Purpose: Indicates directory for agent file operations
   - Used in: `repo_memory_prompt.go` lines 44, 93

## Implementation Layers

### 1. Go Layer (Configuration & Compilation)

**Files**:
- `pkg/workflow/repo_memory.go` - Core configuration and validation
- `pkg/workflow/repo_memory_prompt.go` - Prompt generation
- `pkg/workflow/tools_types.go` - Type definitions

**Key Functions**:

```go
// Configuration extraction from frontmatter
func (c *Compiler) extractRepoMemoryConfig(toolsConfig *ToolsConfig) (*RepoMemoryConfig, error)

// Generate clone steps for agent job
func generateRepoMemorySteps(builder *strings.Builder, data *WorkflowData)

// Generate artifact upload steps for agent job
func generateRepoMemoryArtifactUpload(builder *strings.Builder, data *WorkflowData)

// Generate prompt section informing agent about memory
func (c *Compiler) generateRepoMemoryPromptStep(yaml *strings.Builder, config *RepoMemoryConfig)

// Build push job that downloads artifacts and commits to git
func (c *Compiler) buildPushRepoMemoryJob(data *WorkflowData, threatDetectionEnabled bool) (*Job, error)
```

**Path Usage**:
```go
// Memory directory (no trailing slash for operations)
memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s", memory.ID)

// Prompt path (trailing slash for display)
memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s/", memory.ID)

// Artifact name
fmt.Fprintf(builder, "name: repo-memory-%s\n", memory.ID)
```

### 2. Shell Script Layer (Git Operations)

**File**: `actions/setup/sh/clone_repo_memory_branch.sh`

**Purpose**: Clone or create memory branch during agent job setup

**Environment Variables**:
- `GH_TOKEN` - GitHub authentication token
- `BRANCH_NAME` - Branch to clone (e.g., `memory/default`)
- `TARGET_REPO` - Repository (e.g., `owner/repo`)
- `MEMORY_DIR` - Local directory path (e.g., `/tmp/gh-aw/repo-memory/default`)
- `CREATE_ORPHAN` - Whether to create branch if missing (`true`/`false`)

**Path Usage**:
```bash
# Clone branch to memory directory
git clone --depth 1 --single-branch --branch "$BRANCH_NAME" \
  "https://x-access-token:${GH_TOKEN}@github.com/${TARGET_REPO}.git" \
  "$MEMORY_DIR"

# Or create orphan branch
mkdir -p "$MEMORY_DIR"
cd "$MEMORY_DIR"
git init
git checkout --orphan "$BRANCH_NAME"
```

### 3. JavaScript Layer (Artifact Push)

**File**: `actions/setup/js/push_repo_memory.cjs`

**Purpose**: Download artifact, validate files, commit, and push to git branch

**Environment Variables**:
- `ARTIFACT_DIR` - Downloaded artifact directory (e.g., `/tmp/gh-aw/repo-memory/default`)
- `MEMORY_ID` - Memory identifier (e.g., `default`)
- `TARGET_REPO` - Target repository (e.g., `owner/repo`)
- `BRANCH_NAME` - Branch name (e.g., `memory/default`)
- `MAX_FILE_SIZE` - Maximum bytes per file (default: `10240`)
- `MAX_FILE_COUNT` - Maximum files per commit (default: `100`)
- `FILE_GLOB_FILTER` - Space-separated glob patterns (e.g., `*.md metrics/**`)
- `GH_AW_CAMPAIGN_ID` - Campaign ID for campaign mode validation
- `GH_TOKEN` - GitHub authentication token
- `GITHUB_RUN_ID` - Workflow run ID for commit messages

**Path Usage**:
```javascript
// Artifact directory is the source memory path
const sourceMemoryPath = artifactDir;  // e.g., /tmp/gh-aw/repo-memory/default

// Destination is the checked-out branch root
const destMemoryPath = workspaceDir;   // Git workspace root

// File paths are relative to artifact directory
const relativeFilePath = "history.jsonl"  // NOT "memory/default/history.jsonl"
```

**Key Behavior**:
- Artifact directory IS the memory directory (no nested structure)
- File glob patterns match against relative paths from artifact root
- Branch name used for git operations, NOT for pattern matching
- Campaign mode enforces schema validation for cursor and metrics files

## Configuration Options

### Frontmatter Schema

```yaml
tools:
  repo-memory:
    # Boolean - Enable with defaults
    # true | false
    
    # Object - Single memory with custom config
    # {
    #   target-repo: string (default: current repo)
    #   branch-name: string (default: memory/default)
    #   file-glob: string[] (default: all files)
    #   max-file-size: int (default: 10240, max: 104857600)
    #   max-file-count: int (default: 100, max: 1000)
    #   description: string (optional)
    #   create-orphan: boolean (default: true)
    #   campaign-id: string (optional)
    # }
    
    # Array - Multiple memories
    # [{
    #   id: string (required)
    #   target-repo: string (default: current repo)
    #   branch-name: string (default: memory/{id})
    #   file-glob: string[] (default: all files)
    #   max-file-size: int (default: 10240, max: 104857600)
    #   max-file-count: int (default: 100, max: 1000)
    #   description: string (optional)
    #   create-orphan: boolean (default: true)
    #   campaign-id: string (optional)
    # }]
```

### Configuration Examples

#### Example 1: Basic Enable (Boolean)

```yaml
tools:
  repo-memory: true
```

**Result**:
- Memory ID: `default`
- Branch: `memory/default`
- Path: `/tmp/gh-aw/repo-memory/default/`
- Artifact: `repo-memory-default`

#### Example 2: Custom Configuration (Object)

```yaml
tools:
  repo-memory:
    target-repo: myorg/memory-repo
    branch-name: memory/agent-state
    max-file-size: 524288  # 512 KB
    file-glob:
      - "*.md"
      - "*.json"
    description: Agent state storage
```

**Result**:
- Memory ID: `default` (implicit)
- Branch: `memory/agent-state`
- Path: `/tmp/gh-aw/repo-memory/default/`
- Artifact: `repo-memory-default`
- Only `.md` and `.json` files allowed

#### Example 3: Multiple Memories (Array)

```yaml
tools:
  repo-memory:
    - id: session
      branch-name: memory/session
      max-file-size: 10240
      description: Session state and context
    - id: logs
      branch-name: memory/logs
      max-file-size: 2097152  # 2 MB
      description: Execution logs and diagnostics
```

**Result**:
- Memory 1: `/tmp/gh-aw/repo-memory/session/`, artifact `repo-memory-session`
- Memory 2: `/tmp/gh-aw/repo-memory/logs/`, artifact `repo-memory-logs`

#### Example 4: Campaign Memory

```yaml
tools:
  repo-memory:
    - id: campaigns
      branch-name: memory/campaigns
      file-glob:
        - "go-file-size-reduction/**"
      campaign-id: go-file-size-reduction
```

**Result**:
- Memory ID: `campaigns`
- Branch: `memory/campaigns`
- Path: `/tmp/gh-aw/repo-memory/campaigns/`
- Artifact: `repo-memory-campaigns`
- Campaign mode validation enabled

## Validation Rules

### 1. Memory ID Uniqueness

```go
func validateNoDuplicateMemoryIDs(memories []RepoMemoryEntry) error
```

- Each memory must have a unique ID
- Error if duplicate IDs found
- Location: `pkg/workflow/repo_memory.go` lines 326-336

### 2. File Size Limits

- **Minimum**: 1 byte
- **Maximum**: 104857600 bytes (100 MB)
- **Default**: 10240 bytes (10 KB)
- Validated during config parsing (Go layer)
- Enforced during push (JavaScript layer)

### 3. File Count Limits

- **Minimum**: 1 file
- **Maximum**: 1000 files
- **Default**: 100 files
- Validated during config parsing (Go layer)
- Enforced during push (JavaScript layer)

### 4. File Glob Pattern Validation

**Pattern Matching**:
- Patterns match against **relative file paths** from artifact directory
- Branch name is NOT included in pattern matching
- Supports `*` (single segment) and `**` (multi-segment) wildcards

**Example**:
```yaml
branch-name: memory/code-metrics
file-glob:
  - "*.jsonl"           # Correct: matches history.jsonl
  - "metrics/**"        # Correct: matches metrics/2024-12-31.json
```

**Anti-Pattern**:
```yaml
file-glob:
  - "memory/code-metrics/*.jsonl"  # WRONG: includes branch name
```

### 5. Campaign Mode Validation

When `campaign-id` is set with `memory-id: campaigns`:

**Required Files**:
- `{campaign-id}/cursor.json` - Campaign checkpoint
- `{campaign-id}/metrics/*.json` - At least one metrics snapshot

**Cursor Schema**:
```json
{
  "campaign_id": "string (optional, must match if present)",
  "date": "YYYY-MM-DD (optional)",
  // Additional opaque checkpoint data allowed
}
```

**Metrics Snapshot Schema**:
```json
{
  "campaign_id": "string (required, must match)",
  "date": "YYYY-MM-DD (required)",
  "tasks_total": "integer >= 0 (required)",
  "tasks_completed": "integer >= 0 (required)",
  "tasks_in_progress": "integer >= 0 (optional)",
  "tasks_blocked": "integer >= 0 (optional)",
  "velocity_per_day": "number >= 0 (optional)",
  "estimated_completion": "string (optional)"
}
```

**Pattern Enforcement**:
- All `file-glob` patterns must start with `{campaign-id}/`
- Validates that campaign state is being written
- Fails if required files missing

## Testing Strategy

### Test Coverage Requirements

Each layer must have tests validating path consistency:

#### 1. Go Layer Tests

**File**: `pkg/workflow/repo_memory_test.go`

**Required Tests**:
- Configuration parsing (boolean, object, array)
- Path generation consistency
- Duplicate ID detection
- File size/count validation boundaries
- Prompt generation with correct paths
- Artifact upload step generation
- Push job generation

**Example Test**:
```go
func TestRepoMemoryPathConsistency(t *testing.T) {
    config := &RepoMemoryConfig{
        Memories: []RepoMemoryEntry{
            {ID: "test", BranchName: "memory/test"},
        },
    }
    
    // Test prompt path (with trailing slash)
    var promptBuilder strings.Builder
    generateRepoMemoryPromptSection(&promptBuilder, config)
    if !strings.Contains(promptBuilder.String(), "/tmp/gh-aw/repo-memory/test/") {
        t.Error("Prompt should show path with trailing slash")
    }
    
    // Test artifact path (no trailing slash)
    var artifactBuilder strings.Builder
    generateRepoMemoryArtifactUpload(&artifactBuilder, &WorkflowData{RepoMemoryConfig: config})
    if !strings.Contains(artifactBuilder.String(), "path: /tmp/gh-aw/repo-memory/test\n") {
        t.Error("Artifact path should not have trailing slash")
    }
}
```

#### 2. JavaScript Layer Tests

**File**: `actions/setup/js/push_repo_memory.test.cjs`

**Required Tests**:
- Glob pattern matching (basic, wildcards, nested)
- Path relativization (no branch name in patterns)
- Campaign mode validation
- File size/count enforcement
- Artifact directory handling

**Example Test**:
```javascript
describe("path consistency", () => {
  it("should match patterns against relative paths without branch name", () => {
    const regex = globPatternToRegex("*.jsonl");
    
    // Correct: relative path from artifact root
    expect(regex.test("history.jsonl")).toBe(true);
    
    // Wrong: includes branch name
    expect(regex.test("memory/default/history.jsonl")).toBe(false);
  });
});
```

#### 3. Integration Tests

**File**: `pkg/workflow/repo_memory_integration_test.go`

**Required Tests**:
- End-to-end compilation with repo-memory
- Generated YAML contains correct paths
- Clone steps reference correct directories
- Push steps reference correct artifact names
- Prompt contains correct agent-facing paths

### Cross-Layer Validation Tests

**New Test File**: `pkg/workflow/repo_memory_path_consistency_test.go`

```go
// TestRepoMemoryPathConsistencyAcrossLayers validates that all layers use consistent path patterns
func TestRepoMemoryPathConsistencyAcrossLayers(t *testing.T) {
    memoryID := "test-memory"
    
    // Expected paths
    expectedMemoryDir := "/tmp/gh-aw/repo-memory/test-memory"
    expectedPromptPath := "/tmp/gh-aw/repo-memory/test-memory/"
    expectedArtifactName := "repo-memory-test-memory"
    expectedBranchName := "memory/test-memory"
    
    // Test Go layer generates correct paths
    config := &RepoMemoryConfig{
        Memories: []RepoMemoryEntry{
            {
                ID:         memoryID,
                BranchName: expectedBranchName,
            },
        },
    }
    
    // Validate prompt path
    var promptBuilder strings.Builder
    generateRepoMemoryPromptSection(&promptBuilder, config)
    assert.Contains(t, promptBuilder.String(), expectedPromptPath)
    
    // Validate artifact name
    var artifactBuilder strings.Builder
    generateRepoMemoryArtifactUpload(&artifactBuilder, &WorkflowData{RepoMemoryConfig: config})
    assert.Contains(t, artifactBuilder.String(), fmt.Sprintf("name: %s", expectedArtifactName))
    
    // Validate memory directory
    assert.Contains(t, artifactBuilder.String(), fmt.Sprintf("path: %s", expectedMemoryDir))
}
```

## Documented Behaviors

### Merge Strategy

When pushing changes, conflicts are resolved with "ours" strategy:
```bash
git pull --no-rebase -X ours
```

**Behavior**: Current workflow changes win over remote changes

### Orphan Branch Creation

When `create-orphan: true` (default):
- Branch is created if it doesn't exist
- Uses `git checkout --orphan` for clean history
- Empty initial state

When `create-orphan: false`:
- Branch must exist before workflow runs
- Workflow fails if branch is missing

### Artifact Retention

- **Retention**: 1 day (GitHub Actions default for temp artifacts)
- **Cleanup**: Automatic after push job completes
- **Purpose**: Transfer state between jobs, not long-term storage

### Always Condition

Both upload and push steps use `if: always()`:
- Upload happens even if agent job fails
- Push happens even if agent job fails
- Ensures memory state is preserved for debugging

## Common Issues and Solutions

### Issue 1: File Pattern Not Matching

**Problem**: Files not pushed despite being in artifact

**Cause**: Patterns include branch name
```yaml
file-glob:
  - "memory/default/*.json"  # WRONG
```

**Solution**: Patterns match relative paths from artifact root
```yaml
file-glob:
  - "*.json"  # CORRECT
```

### Issue 2: Trailing Slash Inconsistency

**Problem**: Agent creates files outside memory directory

**Cause**: Prompt path missing trailing slash confuses directory detection

**Solution**: Always use trailing slash in prompts:
```go
memoryDir := fmt.Sprintf("/tmp/gh-aw/repo-memory/%s/", memory.ID)
```

### Issue 3: Campaign Validation Failing

**Problem**: Campaign mode rejects valid files

**Cause**: `file-glob` patterns don't start with `{campaign-id}/`

**Solution**: All campaign patterns must be under campaign subdirectory:
```yaml
campaign-id: my-campaign
file-glob:
  - "my-campaign/**"  # CORRECT
  - "*.json"          # WRONG (not under campaign-id/)
```

## Future Enhancements

### Potential Improvements

1. **Compression Support**
   - Automatically compress large files before artifact upload
   - Decompress during push job
   - Benefit: Reduce artifact storage costs

2. **Incremental Sync**
   - Only upload changed files (rsync-style)
   - Track file hashes between runs
   - Benefit: Faster uploads for large memories

3. **Multi-Repository Memory**
   - Support memory shared across multiple repositories
   - Centralized memory repository
   - Benefit: Agent context across organization

4. **Memory Quotas**
   - Per-repository or per-branch quotas
   - Automatic cleanup of old files
   - Benefit: Prevent unbounded growth

5. **Memory Snapshots**
   - Tag-based snapshots for rollback
   - Time-based retention policies
   - Benefit: Recovery from bad states

## References

### Related Documentation

- [Campaign Workflows](../docs/src/content/docs/guides/campaigns/)
- [GitHub Actions Artifacts](https://docs.github.com/en/actions/using-workflows/storing-workflow-data-as-artifacts)
- [Git Orphan Branches](https://git-scm.com/docs/git-checkout#Documentation/git-checkout.txt---orphanltnew-branchgt)

### Related Specifications

- [Safe Output Messages](./safe-output-messages.md) - Output handling patterns
- [Testing Framework](./testing.md) - Test organization and conventions
- [Code Organization](./code-organization.md) - File structure patterns

### Source Files

**Go Implementation**:
- `pkg/workflow/repo_memory.go` - Core logic (614 lines)
- `pkg/workflow/repo_memory_prompt.go` - Prompt generation (120 lines)
- `pkg/workflow/tools_types.go` - Type definitions

**JavaScript Implementation**:
- `actions/setup/js/push_repo_memory.cjs` - Push logic (522 lines)
- `actions/setup/js/glob_pattern_helpers.cjs` - Pattern matching

**Shell Implementation**:
- `actions/setup/sh/clone_repo_memory_branch.sh` - Clone logic (72 lines)

**Test Files**:
- `pkg/workflow/repo_memory_test.go` - Unit tests (709 lines)
- `pkg/workflow/repo_memory_integration_test.go` - Integration tests
- `actions/setup/js/push_repo_memory.test.cjs` - JavaScript tests

---

**Version**: 1.0.0  
**Date**: 2025-01-02  
**Status**: Complete

This specification defines the canonical behavior and conventions for the repo-memory feature across all implementation layers.
