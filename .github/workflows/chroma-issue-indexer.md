---
on:
  schedule:
    - cron: '0 */4 * * *'  # Every 4 hours
  workflow_dispatch:
engine:
  id: copilot
  model: gpt-5.1-codex-mini
imports:
  - shared/mood.md
  - shared/mcp/chroma.md
permissions:
  contents: read
  issues: read
tools:
  github:
    mode: local
    read-only: true
    toolsets: [issues]
---

# Chroma Issue Indexer

This workflow indexes issues from the repository into a Chroma vector database for semantic search and duplicate detection.

## Task

Index the 100 most recent issues from the repository into the Chroma vector database:

1. **Create Chroma Collection First**:
   - IMPORTANT: Check if the "issues" collection exists using `chroma_list_collections`
   - If it doesn't exist, create it using `chroma_create_collection` with:
     - Collection name: "issues"
     - Use default embedding function (omit embedding_function_name parameter)

2. **Fetch Issues Using GitHub MCP Tools** (NOT Python scripts):
   - Use the `list_issues` tool from GitHub MCP server to fetch issues
   - Fetch issues in batches of 5 at a time using the `perPage: 5` parameter
   - Start with page 1, then page 2, page 3, etc. until you have 100 issues total
   - Include both open and closed issues (omit state parameter to get both)
   - Order by created date descending to get most recent first: `orderBy: "CREATED_AT"`, `direction: "DESC"`
   - For each issue, extract: number, title, body, state, createdAt, author.login, url

3. **Index Issues in Batches**:
   - Process each batch of 5 issues immediately after fetching
   - For each batch, use `chroma_add_documents` to add all 5 issues at once
   - Use ID format: `issue-{issue_number}` (e.g., "issue-123")
   - Document content: `{title}\n\n{body}` (combine title and body)
   - If body is empty/null, use just the title as content
   - Include metadata for each issue:
     - `number`: Issue number (as string)
     - `title`: Issue title
     - `state`: Issue state (OPEN or CLOSED)
     - `author`: Issue author username
     - `created_at`: Issue creation date (ISO 8601 format)
     - `url`: Issue URL

4. **Report Progress**:
   - After processing all batches, use `chroma_get_collection_count` to get total issue count
   - Report how many issues were successfully indexed
   - Note any issues that couldn't be indexed (e.g., API errors)

## Important Notes

- **MUST use GitHub MCP tools** (`list_issues` tool), NOT Python scripts or `gh` CLI
- **MUST create collection first** before attempting to add documents
- Process exactly 5 issues per batch using `perPage: 5` and incrementing page number
- Skip duplicate issues (Chroma will update if ID exists)
- The collection persists in `/tmp/gh-aw/cache-memory-chroma/` across runs
- This helps other workflows search for similar issues using semantic search

<!--
# Chroma Issue Indexer Workflow

An automated workflow that indexes repository issues into a Chroma vector database for semantic search capabilities.

## Features

- **Scheduled Execution**: Runs every 4 hours to keep the index up-to-date
- **Batch Indexing**: Indexes the 100 most recent issues per run
- **Persistent Storage**: Uses cache-memory with Chroma for persistent vector database
- **Semantic Search Ready**: Enables other workflows to search for similar issues
- **Duplicate Detection**: Helps identify duplicate or related issues

## How It Works

1. **Schedule**: Runs automatically every 4 hours via cron schedule
2. **Fetch Issues**: Uses GitHub MCP server to get the latest 100 issues
3. **Index**: Adds each issue to the Chroma "issues" collection with:
   - Vector embeddings for semantic search
   - Metadata (number, title, state, author, dates)
   - Combined title and body as searchable content
4. **Persist**: Stores the vector database in `/tmp/gh-aw/cache-memory-chroma/`
5. **Share**: Makes the indexed issues available for other workflows

## Configuration

```yaml
on:
  schedule:
    - cron: '0 */4 * * *'  # Every 4 hours
  workflow_dispatch:
engine:
  id: copilot
  model: gpt-5.1-codex-mini
imports:
  - shared/mcp/chroma.md
permissions:
  contents: read
  issues: read
```

## Usage

The indexed issues can be queried by other workflows that import `shared/mcp/chroma.md`:

```yaml
# Search for similar issues
chroma_query_documents(
  collection_name="issues",
  query="My issue description",
  limit=5
)
```

## Benefits

- **Duplicate Detection**: Find similar issues before creating new ones
- **Issue Triage**: Quickly find related issues for context
- **Search Enhancement**: Semantic search beyond keyword matching
- **Historical Context**: Maintain searchable issue history

## Maintenance

- Indexes automatically every 4 hours
- Can be manually triggered via workflow_dispatch
- Stores data persistently across runs
- No manual cleanup needed - cache managed by GitHub Actions
-->
