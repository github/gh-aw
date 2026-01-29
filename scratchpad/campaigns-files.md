# Campaign Files Architecture

This document describes how campaigns are discovered, compiled, and executed in GitHub Agentic Workflows. It covers the complete lifecycle from campaign spec files to running workflows.

## Overview

Campaigns are a first-class feature in gh-aw that enable coordinated, multi-repository initiatives. The campaign system consists of:

1. **Campaign Spec Files** (`.campaign.md`) - Declarative YAML frontmatter defining campaign configuration
2. **Discovery Script** (`campaign_discovery.cjs`) - JavaScript that searches GitHub for campaign items
3. **Orchestrator Generator** - Go code that builds agentic workflows from campaign specs
4. **Compiled Workflows** (`.campaign.lock.yml`) - GitHub Actions workflows that run the campaigns

## File Locations

```
.github/workflows/
├── <campaign-id>.campaign.md          # Campaign spec (source of truth)
├── <campaign-id>.campaign.g.md        # Generated orchestrator (debug artifact, not committed)
└── <campaign-id>.campaign.lock.yml    # Compiled workflow (committed)

actions/setup/js/
└── campaign_discovery.cjs             # Discovery precomputation script

pkg/campaign/
├── spec.go                            # Campaign spec data structures
├── loader.go                          # Campaign discovery and loading
├── orchestrator.go                    # Orchestrator generation
└── validation.go                      # Campaign spec validation
```

## Campaign Discovery Process

### 1. Local Repository Discovery

**Implementation**: `pkg/campaign/loader.go:LoadSpecs()`

The campaign system discovers campaign specs by scanning the local repository:

```go
// Scan .github/workflows/ for *.campaign.md files
workflowsDir := filepath.Join(rootDir, ".github", "workflows")
entries, err := os.ReadDir(workflowsDir)

// For each .campaign.md file:
//   1. Read file contents
//   2. Parse YAML frontmatter using parser.ExtractFrontmatterFromContent()
//   3. Unmarshal to CampaignSpec struct
//   4. Set default ID and Name if not provided
//   5. Store relative path in ConfigPath field
```

**Key features**:
- Only scans `.campaign.md` files (not `.md` or `.g.md`)
- Returns empty slice if `.github/workflows/` doesn't exist (no error)
- Populates `ConfigPath` with repository-relative path
- Auto-generates ID from filename if not specified in frontmatter

### 2. Campaign Spec Structure

**Implementation**: `pkg/campaign/spec.go:CampaignSpec`

Campaign specs use YAML frontmatter with these key fields:

```yaml
---
id: security-q1-2025
name: Security Q1 2025
version: v1
state: active

# Project integration
project-url: https://github.com/orgs/ORG/projects/1
tracker-label: z_campaign_security-q1-2025

# Associated workflows
workflows:
  - vulnerability-scanner
  - dependency-updater

# Repo-memory configuration
memory-paths:
  - memory/campaigns/security-q1-2025/**
metrics-glob: memory/campaigns/security-q1-2025/metrics/*.json
cursor-glob: memory/campaigns/security-q1-2025/cursor.json

# Governance
governance:
  max-new-items-per-run: 25
  max-discovery-items-per-run: 200
  max-discovery-pages-per-run: 10
  opt-out-labels: [no-campaign, no-bot]
  max-project-updates-per-run: 10
  max-comments-per-run: 10
---
```

## Campaign Compilation Process

### 1. Detection During Compile

**Implementation**: `pkg/cli/compile_workflow_processor.go:processCampaignSpec()`

During `gh aw compile`, the system:

1. Scans `.github/workflows/` for both `.md` and `.campaign.md` files
2. Detects `.campaign.md` suffix to trigger campaign processing
3. Loads and validates the campaign spec
4. Generates an orchestrator workflow if the spec has meaningful details

**Meaningful details check** (`pkg/campaign/orchestrator.go:BuildOrchestrator()`):
- Must have at least one of: workflows, memory paths, metrics glob, cursor glob, project URL, governance, or KPIs
- Returns `nil` if campaign has no actionable configuration
- This prevents empty orchestrators from being generated

### 2. Orchestrator Generation

**Implementation**: `pkg/campaign/orchestrator.go:BuildOrchestrator()`

The orchestrator generator creates a `workflow.WorkflowData` struct containing:

#### A. Discovery Precomputation Steps

**Function**: `buildDiscoverySteps()`

When a campaign has workflows or a tracker label, the generator adds discovery steps:

```yaml
steps:
  - name: Create workspace directory
    run: mkdir -p ./.gh-aw

  - name: Run campaign discovery precomputation
    id: discovery
    uses: actions/github-script@v8.0.0
    env:
      GH_AW_CAMPAIGN_ID: security-q1-2025
      GH_AW_WORKFLOWS: "vulnerability-scanner,dependency-updater"
      GH_AW_TRACKER_LABEL: z_campaign_security-q1-2025
      GH_AW_PROJECT_URL: https://github.com/orgs/ORG/projects/1
      GH_AW_MAX_DISCOVERY_ITEMS: 200
      GH_AW_MAX_DISCOVERY_PAGES: 10
      GH_AW_CURSOR_PATH: /tmp/gh-aw/repo-memory/campaigns/security-q1-2025/cursor.json
    with:
      github-token: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN || secrets.GH_AW_GITHUB_MCP_SERVER_TOKEN }}
      script: |
        const { setupGlobals } = require('/opt/gh-aw/actions/setup_globals.cjs');
        setupGlobals(core, github, context, exec, io);
        const { main } = require('/opt/gh-aw/actions/campaign_discovery.cjs');
        await main();
```

**Discovery script location**: The script is loaded from `/opt/gh-aw/actions/campaign_discovery.cjs`, which is copied during the `actions/setup` step.

#### B. Workflow Metadata

```go
data := &workflow.WorkflowData{
    Name:        spec.Name,
    Description: spec.Description,
    On:          "on:\n  schedule:\n    - cron: \"0 18 * * *\"\n  workflow_dispatch:\n",
    Concurrency: fmt.Sprintf("concurrency:\n  group: \"campaign-%s-orchestrator-${{ github.ref }}\"\n  cancel-in-progress: false", spec.ID),
    RunsOn:      "runs-on: ubuntu-latest",
    Roles:       []string{"admin", "maintainer", "write"},
}
```

#### C. Tools Configuration

```go
Tools: map[string]any{
    "repo-memory": []any{
        map[string]any{
            "id":          "campaigns",
            "branch-name": "memory/campaigns",
            "file-glob":   extractFileGlobPatterns(spec),
            "campaign-id": spec.ID,
        },
    },
    "bash": []any{"*"},
    "edit": nil,
}
```

Note: orchestrators deliberately omit GitHub tool access. All writes and GitHub API operations should be performed by dispatched worker workflows.

#### D. Safe Outputs Configuration

```go
safeOutputs := &workflow.SafeOutputsConfig{}

// Campaign orchestrators are dispatch-only: they may only dispatch allowlisted
// workflows via the dispatch-workflow safe output.
if len(spec.Workflows) > 0 {
  safeOutputs.DispatchWorkflow = &workflow.DispatchWorkflowConfig{
    BaseSafeOutputConfig: workflow.BaseSafeOutputConfig{Max: 3},
    Workflows:            spec.Workflows,
  }
}
```

Workers are responsible for side effects (Projects, issues/PRs, comments) using their own tool configuration and safe-outputs.

#### E. Prompt Section

The orchestrator includes detailed instructions for the AI agent:

```go
markdownBuilder.WriteString("# Campaign Orchestrator\n\n")
// Campaign details: objective, KPIs, workflows, memory paths, etc.

orchestratorInstructions := RenderOrchestratorInstructions(promptData)
projectInstructions := RenderProjectUpdateInstructions(promptData)
closingInstructions := RenderClosingInstructions()
```

### 3. Markdown Generation

**Implementation**: `pkg/cli/compile_orchestrator.go:renderGeneratedCampaignOrchestratorMarkdown()`

The orchestrator is rendered as a markdown file:

```
<campaign-id>.campaign.g.md
```

**Important**: This `.campaign.g.md` file is a **debug artifact**:
- Generated locally during compilation
- Helps users understand the orchestrator structure
- **NOT committed to git** (excluded via `.gitignore`)
- Can be reviewed locally to see generated workflow structure

**Compiled output**: Only the `.campaign.lock.yml` file is committed to version control.

### 4. Lock File Naming

**Implementation**: `pkg/stringutil/identifiers.go:CampaignOrchestratorToLockFile()`

Campaign orchestrators follow a special naming convention:

```
example.campaign.g.md   →   example.campaign.lock.yml
```

**Not**: `example.campaign.g.lock.yml` (the `.g` suffix is removed)

This ensures the lock file name matches the campaign spec name pattern.

## Discovery Script Architecture

### Script Location

**Source**: `actions/setup/js/campaign_discovery.cjs`

**Runtime location**: `/opt/gh-aw/actions/campaign_discovery.cjs`

The discovery script is copied to `/opt/gh-aw/actions/` during the `actions/setup` action, which runs before the agent job.

### Discovery Flow

**Implementation**: `actions/setup/js/campaign_discovery.cjs:main()`

1. **Read configuration from environment variables**:
   - `GH_AW_CAMPAIGN_ID` - Campaign identifier
   - `GH_AW_WORKFLOWS` - Comma-separated list of workflow IDs (tracker-ids)
   - `GH_AW_TRACKER_LABEL` - Optional label for discovery
   - `GH_AW_MAX_DISCOVERY_ITEMS` - Budget for items to discover (default: 100)
   - `GH_AW_MAX_DISCOVERY_PAGES` - Budget for API pages to fetch (default: 10)
   - `GH_AW_CURSOR_PATH` - Path to cursor file for pagination
   - `GH_AW_PROJECT_URL` - Project URL for reference

2. **Load cursor from repo-memory** (if configured):
   ```javascript
   function loadCursor(cursorPath) {
     if (fs.existsSync(cursorPath)) {
       const content = fs.readFileSync(cursorPath, "utf8");
       return JSON.parse(content);
     }
     return null;
   }
   ```

3. **Primary discovery: search by campaign-specific label**:
  - Derived label: `z_campaign_<campaign-id>`
  - Query: `label:"z_campaign_<campaign-id>"` scoped to repos/orgs

4. **Secondary discovery: search by generic `agentic-campaign` label**:
  - Query: `label:"agentic-campaign"` scoped to repos/orgs

5. **Fallback discovery: search by tracker-id markers**:
  ```javascript
  // For each workflow in spec.workflows:
  const searchQuery = `"gh-aw-tracker-id: ${trackerId}" type:issue`;
  // (scoped with repo:<owner/repo> and/or org:<org> terms)
  ```

6. **Legacy discovery: search by configured tracker label** (if provided):
  - Query: `label:"${label}"`

5. **Normalize discovered items**:
   ```javascript
   function normalizeItem(item, contentType) {
     return {
       url: item.html_url || item.url,
       content_type: contentType,  // "issue" or "pull_request"
       number: item.number,
       repo: item.repository?.full_name || "",
       created_at: item.created_at,
       updated_at: item.updated_at,
       state: item.state,
       title: item.title,
       closed_at: item.closed_at,
       merged_at: item.merged_at,
     };
   }
   ```

6. **Deduplicate items** (when using both tracker-id and tracker-label):
   ```javascript
   const existingUrls = new Set(allItems.map(i => i.url));
   for (const item of result.items) {
     if (!existingUrls.has(item.url)) {
       allItems.push(item);
     }
   }
   ```

7. **Sort for stable ordering**:
   ```javascript
   allItems.sort((a, b) => {
     if (a.updated_at !== b.updated_at) {
       return a.updated_at.localeCompare(b.updated_at);
     }
     return a.number - b.number;
   });
   ```

8. **Calculate summary counts**:
   ```javascript
   const needsAddCount = allItems.filter(i => i.state === "open").length;
   const needsUpdateCount = allItems.filter(i => i.state === "closed" || i.merged_at).length;
   ```

9. **Write manifest to `./.gh-aw/campaign.discovery.json`**:
   ```json
   {
     "schema_version": "v1",
     "campaign_id": "security-q1-2025",
     "generated_at": "2025-01-08T12:00:00.000Z",
     "project_url": "https://github.com/orgs/ORG/projects/1",
     "discovery": {
       "total_items": 42,
       "items_scanned": 100,
       "pages_scanned": 2,
       "max_items_budget": 200,
       "max_pages_budget": 10,
       "cursor": { "page": 3, "trackerId": "vulnerability-scanner" }
     },
     "summary": {
       "needs_add_count": 25,
       "needs_update_count": 17,
       "open_count": 25,
       "closed_count": 10,
       "merged_count": 7
     },
     "items": [
       {
         "url": "https://github.com/org/repo/issues/123",
         "content_type": "issue",
         "number": 123,
         "repo": "org/repo",
         "created_at": "2025-01-01T00:00:00Z",
         "updated_at": "2025-01-07T12:00:00Z",
         "state": "open",
         "title": "Upgrade dependency X"
       }
     ]
   }
   ```

10. **Save cursor to repo-memory** (for next run):
    ```javascript
    function saveCursor(cursorPath, cursor) {
      const dir = path.dirname(cursorPath);
      if (!fs.existsSync(dir)) {
        fs.mkdirSync(dir, { recursive: true });
      }
      fs.writeFileSync(cursorPath, JSON.stringify(cursor, null, 2));
    }
    ```

### Pagination Budgets

The discovery system enforces strict pagination budgets to prevent unbounded API usage:

- **Max items per run** (`governance.max-discovery-items-per-run`): Default 100, configurable
- **Max pages per run** (`governance.max-discovery-pages-per-run`): Default 10, configurable

When budgets are reached:
```javascript
if (itemsScanned >= maxItems || pagesScanned >= maxPages) {
  core.warning(`Reached discovery budget limits. Stopping discovery.`);
  break;
}
```

### Cursor Persistence

The cursor enables incremental discovery across runs:

**Cursor format**:
```json
{
  "page": 3,
  "trackerId": "vulnerability-scanner"
}
```

**Storage location**: Configured via `spec.CursorGlob`, typically:
```
memory/campaigns/<campaign-id>/cursor.json
```

**How it works**:
1. Discovery loads cursor from repo-memory
2. Continues from saved page number
3. Updates cursor after each workflow/label search
4. Saves updated cursor back to repo-memory
5. Next run picks up where previous run left off

### Campaign Item Protection

The current campaign system’s primary tracking label format is:

```
z_campaign_<campaign-id>
```

Workers should apply this label to all created issues/PRs so discovery can find them reliably.

Some workflows in this repo also treat `campaign:*` labels as a “do not touch” signal (legacy convention). If you need that compatibility, have workers apply both labels:

```
z_campaign_<campaign-id>
campaign:<campaign-id>
```

Campaign specs can also define `governance.opt-out-labels` (for example: `no-bot`, `no-campaign`) to let humans opt items out of automated handling.

## Campaign Workers

Campaign workers are specialized workflows designed to be orchestrated by campaign orchestrators. They follow a first-class worker pattern with explicit contracts and idempotency.

### Worker Design Principles

1. **Dispatch-only triggers**: Workers use `workflow_dispatch` as the primary/only trigger
   - No schedule, push, or pull_request triggers
   - Clear ownership: workers are orchestrated, not autonomous
   - Prevents duplicate execution from multiple trigger sources

2. **Standardized input contract**: All workers accept:
   - `campaign_id` (string): The campaign identifier orchestrating this worker
   - `payload` (string): JSON-encoded data specific to the work item
   
3. **Idempotency**: Workers implement deterministic behavior:
   - Compute deterministic work item keys (e.g., `campaign-{id}-{repo}-{alert-id}`)
   - Use keys in branch names, PR titles, issue titles
   - Check for existing PR/issue with key + tracker label before creating
   - Skip or update existing items rather than creating duplicates

4. **Orchestration agnostic**: Workers don't know about orchestration policy
   - Sequential vs parallel execution is orchestrator's concern
   - Workers are simple, focused, deterministic units

### Worker Workflow Template

```yaml
---
name: Campaign Worker Example
description: Example worker workflow for campaign orchestration

on:
  workflow_dispatch:
    inputs:
      campaign_id:
        description: 'Campaign identifier'
        required: true
        type: string
      payload:
        description: 'JSON payload with work item details'
        required: true
        type: string

tracker-id: campaign-worker-example

tools:
  github:
    toolsets: [default]

safe-outputs:
  create-pull-request:
    max: 1
  add-comment:
    max: 2
---

# Campaign Worker Example

You are a campaign worker that processes work items from a campaign orchestrator.

## Input Contract

The `payload` input contains JSON with the following structure:
```json
{
  "repository": "owner/repo",
  "work_item_id": "unique-identifier",
  "target_ref": "main",
  "additional_context": {}
}
```

Parse the payload and extract the work item details.

## Idempotency Requirements

Before creating any GitHub resources:

1. **Generate deterministic key**: 
   - Format: `campaign-${campaign_id}-${repository}-${work_item_id}`
   - Use this key in branch names, PR titles, issue titles

2. **Check for existing work**:
   - Search for PRs/issues with the deterministic key in the title
   - Filter by tracker label: `campaign:${campaign_id}`
   - If found: Skip creation or update existing item
   - If not found: Proceed with creation

3. **Label all created items**:
   - Apply tracker label: `campaign:${campaign_id}`
   - This enables discovery by the orchestrator
   - Prevents interference from other workflows

## Work to Perform

[Specific task description for this worker]

## Expected Output

Report completion status including:
- Whether work was skipped (already exists) or completed
- Links to created/updated PRs or issues
- Any errors or blockers encountered
```

### Idempotency Implementation Patterns

#### Pattern 1: Deterministic Branch Names

```yaml
# In worker prompt
Generate a deterministic branch name:
- Format: `campaign-${campaign_id}-${repository.replace('/', '-')}-${work_item_id}`
- Example: `campaign-security-q1-2025-myorg-myrepo-alert-123`

Before creating a new branch:
1. Check if the branch already exists
2. If exists: checkout and update
3. If not: create new branch
```

#### Pattern 2: PR Title Prefixing

```yaml
# In worker prompt
Use a deterministic PR title prefix:
- Format: `[campaign-${campaign_id}] ${work_item_description}`
- Example: `[campaign-security-q1-2025] Fix SQL injection in user.go`

Before creating a PR:
1. Search for open PRs with this title prefix in the target repo
2. If found: Add a comment with updates or close as duplicate
3. If not: Create new PR with title
```

#### Pattern 3: Issue Title Keying

```yaml
# In worker prompt
Use a deterministic issue title with key:
- Format: `[${work_item_id}] ${description}`
- Example: `[alert-123] High severity: Path traversal vulnerability`

Before creating an issue:
1. Search for issues with `[${work_item_id}]` in title
2. Filter by label: `z_campaign_${campaign_id}`
3. If found: Update existing issue with new information
4. If not: Create new issue
```

#### Pattern 4: Cursor-based Work Tracking

```yaml
# In worker prompt
Track processed work items in repo-memory:
- File: `memory/campaigns/${campaign_id}/processed-items.json`
- Structure: `{"processed": ["item-1", "item-2", ...]}`

Before processing a work item:
1. Load the processed items list from repo-memory
2. Check if current work_item_id is in the list
3. If found: Skip processing
4. If not: Process and add to list
5. Save updated list back to repo-memory
```

### Worker Discovery

Campaign orchestrators discover worker-created items via:

1. **Tracker Label**: Items labeled with `campaign:${campaign_id}`
2. **Tracker ID**: Items with `tracker-id: worker-name` in their description
3. **Discovery Script**: `campaign_discovery.cjs` searches for both

Workers should:
- Apply the campaign tracker label to all created items
- Include the worker's tracker-id in issue/PR descriptions (optional)
- This enables orchestrators to find and track worker output

### Example: Security Fix Worker

```yaml
---
name: Security Fix Worker
description: Creates PRs with security fixes for code scanning alerts

on:
  workflow_dispatch:
    inputs:
      campaign_id:
        description: 'Campaign identifier'
        required: true
        type: string
      payload:
        description: 'JSON with alert details'
        required: true
        type: string

tracker-id: security-fix-worker

tools:
  github:
    toolsets: [default, code_security]
  bash: ["*"]
  edit: true

safe-outputs:
  create-pull-request:
    max: 1
---

# Security Fix Worker

Process a code scanning alert and create a fix PR.

## Idempotency Implementation

```javascript
const payload = JSON.parse(process.env.PAYLOAD);
const campaignId = process.env.CAMPAIGN_ID;
const alertId = payload.alert_id;
const repository = payload.repository;

// Deterministic key
const workKey = `campaign-${campaignId}-alert-${alertId}`;
const branchName = `fix/${workKey}`;
const prTitle = `[${workKey}] Fix: ${payload.alert_title}`;

// Check for existing PR
const existingPRs = await searchPullRequests({
  query: `repo:${repository} is:pr is:open "${workKey}" in:title`
});

if (existingPRs.length > 0) {
  console.log(`PR already exists: ${existingPRs[0].url}`);
  // Optionally update with new information
  return;
}

// Proceed with fix and PR creation...
```

## Expected Behavior

1. Parse payload to get alert details
2. Check for existing PR with deterministic key
3. If exists: Skip or update
4. If not: Generate fix and create PR
5. Apply labels: `campaign:${campaign_id}`, `security`, `automated`
6. Report completion status
```

## For Third-Party Users

### Using gh-aw Compiler Outside This Repository

**Yes, it works!** The campaign system is designed to work in any repository with gh-aw installed.

#### Prerequisites

```bash
# Install gh-aw CLI
gh extension install githubnext/gh-aw

# Or use local binary
./gh-aw --help
```

#### Creating a Campaign

1. **Create campaign spec** in your repository:
   ```bash
   mkdir -p .github/workflows
   gh aw campaign new my-campaign
   ```

2. **Edit the spec** (`.github/workflows/my-campaign.campaign.md`):
   ```yaml
   ---
   id: my-campaign
   name: My Campaign
   version: v1
   project-url: https://github.com/orgs/ORG/projects/1
  tracker-label: z_campaign_my-campaign
   workflows:
     - my-worker-workflow
   memory-paths:
     - memory/campaigns/my-campaign/**
   ---
   
   # Campaign description goes here
   ```

3. **Compile the campaign**:
   ```bash
   gh aw compile
   ```

   This generates:
   - `.github/workflows/my-campaign.campaign.g.md` (local debug artifact)
   - `.github/workflows/my-campaign.campaign.lock.yml` (committed)

4. **Commit and push**:
   ```bash
   git add .github/workflows/my-campaign.campaign.md
   git add .github/workflows/my-campaign.campaign.lock.yml
   git commit -m "Add my-campaign"
   git push
   ```

5. **Run the orchestrator** from GitHub Actions tab

#### What Gets Executed

When the orchestrator runs:

1. **Setup Actions** - Copies JavaScript files to `/opt/gh-aw/actions/`:
   - Source: `actions/setup/js/campaign_discovery.cjs` (from gh-aw repository)
   - Runtime: `/opt/gh-aw/actions/campaign_discovery.cjs`

2. **Discovery Step** - Executes discovery precomputation:
   - Uses `actions/github-script@v8.0.0`
   - Calls `require('/opt/gh-aw/actions/campaign_discovery.cjs')`
   - Generates `./.gh-aw/campaign.discovery.json`

3. **Agent Job** - AI agent processes the manifest:
   - Reads `./.gh-aw/campaign.discovery.json`
   - Updates GitHub Project board via safe-outputs
   - Uses repo-memory for state persistence

#### Required Files

**In the gh-aw repository** (automatically included):
- `actions/setup/` - Setup action that copies JavaScript files
- `actions/setup/js/campaign_discovery.cjs` - Discovery script
- `actions/setup/js/setup_globals.cjs` - Global utilities

**In your repository** (you create):
- `.github/workflows/<id>.campaign.md` - Campaign spec
- `.github/workflows/<id>.campaign.lock.yml` - Compiled workflow (generated)

#### How the Compiler Finds Scripts

The discovery script is **not** included in the compiled `.lock.yml` file. Instead:

1. The compiled workflow includes an `actions/setup` step
2. `actions/setup` copies files from its repository to `/opt/gh-aw/actions/`
3. The discovery step uses `require('/opt/gh-aw/actions/campaign_discovery.cjs')`
4. This works because the path is available at runtime via the setup action

**Key insight**: The setup action is a composite action that copies JavaScript files to a runtime location. This allows campaigns in any repository to use the discovery script without duplicating it.

## Cross-References

### Code References

**Campaign package** (`pkg/campaign/`):
- `spec.go` - Data structures (CampaignSpec, CampaignKPI, CampaignGovernancePolicy)
- `loader.go` - Discovery and loading (LoadSpecs, FilterSpecs, CreateSpecSkeleton)
- `orchestrator.go` - Orchestrator generation (BuildOrchestrator, buildDiscoverySteps)
- `validation.go` - Spec validation (ValidateSpec)
- `command.go` - CLI commands (campaign, campaign status, campaign new, campaign validate)

**CLI package** (`pkg/cli/`):
- `compile_workflow_processor.go` - Workflow processing (processCampaignSpec)
- `compile_orchestrator.go` - Orchestrator rendering (renderGeneratedCampaignOrchestratorMarkdown)
- `compile_helpers.go` - Utility functions

**Actions** (`actions/setup/js/`):
- `campaign_discovery.cjs` - Discovery precomputation script
- `setup_globals.cjs` - Global utilities for GitHub Actions scripts

### Key Workflows

**Example campaigns** (in `.github/workflows/`):
- Look for `*.campaign.md` files in the repository root
- Compiled to `*.campaign.lock.yml` files

## Design Decisions

### Why Separate Discovery Step?

**Problem**: AI agents performing GitHub-wide discovery during Phase 1 is:
- Non-deterministic (different results on each run)
- Expensive (many API calls)
- Slow (sequential search)

**Solution**: Precomputation step that runs before the agent:
- Deterministic output (stable manifest)
- Enforced budgets (max items, max pages)
- Fast (parallel search possible)
- Cacheable (manifest can be reused)

### Why `.campaign.g.md` is Not Committed

**Rationale**:
- It's a generated artifact, not source code
- Users edit `.campaign.md`, not `.campaign.g.md`
- The `.lock.yml` file is the authoritative compiled output
- Keeping `.g.md` local aids debugging without cluttering git history

**Benefits**:
- Cleaner git history
- No merge conflicts on generated files
- Users can regenerate anytime with `gh aw compile`
- `.lock.yml` provides reproducible execution

### Why Cursor is in Repo-Memory

**Rationale**:
- Campaigns need durable state across runs
- Git branches provide versioned, auditable history
- Repo-memory integrates with existing GitHub workflows

**Alternatives considered**:
- Environment variables (lost between runs)
- Workflow artifacts (expire after 90 days)
- External database (requires additional infrastructure)

### Why Campaign-Specific Lock File Naming

**Problem**: Standard naming would produce:
```
example.campaign.g.md  →  example.campaign.g.lock.yml
```

This is verbose and inconsistent with the spec file name.

**Solution**: Special handling in `stringutil.CampaignOrchestratorToLockFile()`:
```
example.campaign.g.md  →  example.campaign.lock.yml
```

This keeps lock files aligned with spec files:
```
example.campaign.md        (spec)
example.campaign.lock.yml  (compiled)
```

## Debugging

### Enable Debug Logging

```bash
DEBUG=campaign:*,cli:* gh aw compile
```

### Check Generated Orchestrator

```bash
# Review local debug artifact
cat .github/workflows/<campaign-id>.campaign.g.md

# Review compiled workflow
cat .github/workflows/<campaign-id>.campaign.lock.yml
```

### Inspect Discovery Manifest

After running the orchestrator:

```bash
# Download workflow artifacts
gh run download <run-id>

# Check discovery manifest
cat .gh-aw/campaign.discovery.json
```

### Validate Campaign Spec

```bash
gh aw campaign validate
gh aw campaign validate my-campaign
gh aw campaign validate --json
```

## Future Enhancements

### Planned Improvements

1. **Multi-repository discovery**: Search across organization repositories
2. **Advanced filtering**: Filter items by milestone, assignee, or custom fields
3. **Discovery caching**: Cache discovery results to reduce API calls
4. **Incremental updates**: Only update changed items in project board
5. **Workflow templates**: Pre-built campaign templates for common scenarios

### Extension Points

1. **Custom discovery scripts**: Allow campaigns to provide custom discovery logic
2. **Discovery plugins**: Plugin system for discovery sources (Jira, Linear, etc.)
3. **Campaign hierarchies**: Parent/child campaigns with rollup metrics
4. **Cross-campaign dependencies**: Express dependencies between campaigns

---

**Last Updated**: 2025-01-08

**Related Issues**: #1234 (Campaign Architecture), #5678 (Discovery Optimization)
