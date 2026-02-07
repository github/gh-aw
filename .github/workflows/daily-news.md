---
description: Generates a daily news digest of repository activity including issues, PRs, discussions, and workflow runs
on:
  schedule:
    # Every day at 9am UTC, all days except Saturday and Sunday
    - cron: "0 9 * * 1-5"
  workflow_dispatch:

permissions:
  contents: read
  issues: read
  pull-requests: read
  discussions: read
  actions: read

tracker-id: daily-news-weekday
engine: copilot

timeout-minutes: 30  # Reduced from 45 since pre-fetching data is faster

network:
  allowed:
    - defaults
    - python
    - node

sandbox:
  agent: awf  # Firewall enabled (migrated from network.firewall)
safe-outputs:
  upload-asset:
  create-discussion:
    expires: 3d
    category: "daily-news"
    max: 1
    close-older-discussions: true

tools:
  repo-memory:
    branch-name: memory/daily-news
    description: "Historical news digest data"
    file-glob: ["memory/daily-news/*.json", "memory/daily-news/*.jsonl", "memory/daily-news/*.csv", "memory/daily-news/*.md"]
    max-file-size: 102400  # 100KB
  edit:
  bash:
    - "*"
  web-fetch:

# Pre-download GitHub data in steps to avoid excessive MCP calls
# Uses repo-memory to persist data across runs and avoid re-fetching
steps:
  - name: Setup directories and check cache
    id: check-cache
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      
      # Create directories
      mkdir -p /tmp/gh-aw/daily-news-data
      mkdir -p /tmp/gh-aw/repo-memory/default/daily-news-data
      
      # Check if cached data exists and is recent (< 24 hours old)
      CACHE_VALID=false
      CACHE_TIMESTAMP_FILE="/tmp/gh-aw/repo-memory/default/daily-news-data/.timestamp"
      
      if [ -f "$CACHE_TIMESTAMP_FILE" ]; then
        CACHE_AGE=$(($(date +%s) - $(cat "$CACHE_TIMESTAMP_FILE")))
        # 24 hours = 86400 seconds
        if [ $CACHE_AGE -lt 86400 ]; then
          echo "✅ Found valid cached data (age: ${CACHE_AGE}s, less than 24h)"
          CACHE_VALID=true
        else
          echo "⚠ Cached data is stale (age: ${CACHE_AGE}s, more than 24h)"
        fi
      else
        echo "ℹ No cached data found, will fetch fresh data"
      fi
      
      # Use cached data if valid
      if [ "$CACHE_VALID" = true ]; then
        echo "📦 Using cached data from previous run"
        cp -r /tmp/gh-aw/repo-memory/default/daily-news-data/* /tmp/gh-aw/daily-news-data/
        echo "✅ Cached data restored to working directory"
        echo "cache_valid=true" >> "$GITHUB_OUTPUT"
      else
        echo "🔄 Will fetch fresh data from GitHub API..."
        echo "cache_valid=false" >> "$GITHUB_OUTPUT"
        
        # Calculate date range (last 30 days)
        END_DATE=$(date -u +%Y-%m-%d)
        START_DATE=$(date -u -d '30 days ago' +%Y-%m-%d 2>/dev/null || date -u -v-30d +%Y-%m-%d)
        echo "Fetching data from $START_DATE to $END_DATE"
      fi

  - name: Fetch issues data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Fetching issues..."
      gh api graphql -f query="
        query(\$owner: String!, \$repo: String!) {
          repository(owner: \$owner, name: \$repo) {
            openIssues: issues(first: 100, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                state
                createdAt
                updatedAt
                author { login }
                labels(first: 10) { nodes { name } }
                comments { totalCount }
              }
            }
            closedIssues: issues(first: 100, states: CLOSED, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                state
                createdAt
                updatedAt
                closedAt
                author { login }
                labels(first: 10) { nodes { name } }
              }
            }
          }
        }
      " -f owner="${GITHUB_REPOSITORY_OWNER}" -f repo="${GITHUB_REPOSITORY#*/}" > /tmp/gh-aw/daily-news-data/issues.json
      echo "✅ Issues data fetched"

  - name: Fetch pull requests data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Fetching pull requests..."
      gh api graphql -f query="
        query(\$owner: String!, \$repo: String!) {
          repository(owner: \$owner, name: \$repo) {
            openPRs: pullRequests(first: 50, states: OPEN, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                state
                createdAt
                updatedAt
                author { login }
                additions
                deletions
                changedFiles
                reviews(first: 10) { totalCount }
              }
            }
            mergedPRs: pullRequests(first: 50, states: MERGED, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                state
                createdAt
                updatedAt
                mergedAt
                author { login }
                additions
                deletions
              }
            }
            closedPRs: pullRequests(first: 30, states: CLOSED, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                state
                createdAt
                closedAt
                author { login }
              }
            }
          }
        }
      " -f owner="${GITHUB_REPOSITORY_OWNER}" -f repo="${GITHUB_REPOSITORY#*/}" > /tmp/gh-aw/daily-news-data/pull_requests.json
      echo "✅ Pull requests data fetched"

  - name: Fetch commits data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Fetching commits..."
      gh api "repos/${GITHUB_REPOSITORY}/commits" \
        --paginate \
        --jq '[.[] | {sha, author: .commit.author, message: .commit.message, date: .commit.author.date, html_url}]' \
        > /tmp/gh-aw/daily-news-data/commits.json
      echo "✅ Commits data fetched"

  - name: Fetch releases data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Fetching releases..."
      gh api "repos/${GITHUB_REPOSITORY}/releases" \
        --jq '[.[] | {tag_name, name, created_at, published_at, html_url, body}]' \
        > /tmp/gh-aw/daily-news-data/releases.json
      echo "✅ Releases data fetched"

  - name: Fetch discussions data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Fetching discussions..."
      gh api graphql -f query="
        query(\$owner: String!, \$repo: String!) {
          repository(owner: \$owner, name: \$repo) {
            discussions(first: 50, orderBy: {field: UPDATED_AT, direction: DESC}) {
              nodes {
                number
                title
                createdAt
                updatedAt
                author { login }
                category { name }
                comments { totalCount }
                url
              }
            }
          }
        }
      " -f owner="${GITHUB_REPOSITORY_OWNER}" -f repo="${GITHUB_REPOSITORY#*/}" > /tmp/gh-aw/daily-news-data/discussions.json
      echo "✅ Discussions data fetched"

  - name: Check for changesets
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "Checking for changesets..."
      if [ -d ".changeset" ]; then
        find .changeset -name "*.md" -type f ! -name "README.md" > /tmp/gh-aw/daily-news-data/changesets.txt
      else
        echo "No changeset directory" > /tmp/gh-aw/daily-news-data/changesets.txt
      fi
      echo "✅ Changeset check complete"

  - name: Cache downloaded data
    if: steps.check-cache.outputs.cache_valid != 'true'
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      set -e
      echo "💾 Caching data for future runs..."
      cp -r /tmp/gh-aw/daily-news-data/* /tmp/gh-aw/repo-memory/default/daily-news-data/
      date +%s > "/tmp/gh-aw/repo-memory/default/daily-news-data/.timestamp"
      echo "✅ Data caching complete"

  - name: List downloaded data
    env:
      GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: |
      find /tmp/gh-aw/daily-news-data/ -maxdepth 1 -ls

imports:
  - shared/mood.md
  - shared/mcp/tavily.md
  - shared/jqschema.md
  - shared/reporting.md
  - shared/trends.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily News

Write an upbeat, friendly, motivating summary of recent activity in the repo.

## 📁 Pre-Downloaded Data Available

**IMPORTANT**: All GitHub data has been pre-downloaded to `/tmp/gh-aw/daily-news-data/` to avoid excessive MCP calls. Use these files instead of making GitHub API calls:

- **`issues.json`** - Open and recently closed issues (last 100 of each)
- **`pull_requests.json`** - Open, merged, and closed pull requests
- **`commits.json`** - Recent commits (up to last 100)
- **`releases.json`** - All releases
- **`discussions.json`** - Recent discussions (last 50)
- **`changesets.txt`** - List of changeset files (if directory exists)

**Load and analyze these files** instead of making repeated GitHub MCP calls. All data is in JSON format (except changesets.txt which lists file paths).

## 💾 Repo Memory Available

**Repo-memory is enabled** - You have access to persistent storage at `/tmp/gh-aw/repo-memory/default/` that persists across workflow runs:

- Use it to **store intermediate analysis results** that might be useful for future runs
- Store **processed data, statistics, or insights** that take time to compute
- Cache **expensive computations** like trend analysis or aggregated metrics
- Files stored here will be available in the next workflow run via Git branches

**Example use cases**:
- Save aggregated statistics (e.g., `/tmp/gh-aw/repo-memory/default/monthly-stats.json`)
- Cache processed trend data for faster chart generation
- Store analysis results that can inform future reports

## 📊 Trend Charts Requirement

**IMPORTANT**: Generate exactly 2 trend charts that showcase key metrics of the project. These charts should visualize trends over time to give the team insights into project health and activity patterns.

Use the pre-downloaded data from `/tmp/gh-aw/daily-news-data/` to generate all statistics and charts.

### Chart Generation Process

**Phase 1: Data Collection**

**Use the pre-downloaded data files** from `/tmp/gh-aw/daily-news-data/`:

1. **Issues Activity Data**: Load from `issues.json`
   - Parse `openIssues.nodes` and `closedIssues.nodes`
   - Extract `createdAt`, `updatedAt`, `closedAt` timestamps
   - Aggregate by day to count opens/closes
   - Calculate running count of open issues

2. **Pull Requests Activity Data**: Load from `pull_requests.json`
   - Parse `openPRs.nodes`, `mergedPRs.nodes`, `closedPRs.nodes`
   - Extract `createdAt`, `updatedAt`, `mergedAt`, `closedAt` timestamps
   - Aggregate by day to count opens/merges/closes

3. **Commit Activity Data**: Load from `commits.json`
   - Parse commit array
   - Extract `date` (commit.author.date) timestamps
   - Aggregate by day to count commits
   - Count unique authors per day

4. **Additional Context** (optional):
   - Load discussions from `discussions.json`
   - Load releases from `releases.json`
   - Read changeset files listed in `changesets.txt`

**Phase 2: Data Preparation**

1. Create a Python script at `/tmp/gh-aw/python/process_data.py` that:
   - Reads the JSON files from `/tmp/gh-aw/daily-news-data/`
   - Processes timestamps and aggregates by date
   - Generates CSV files in `/tmp/gh-aw/python/data/`:
     - `issues_prs_activity.csv` - Daily counts of issues and PRs
     - `commit_activity.csv` - Daily commit counts and contributors

2. Execute the Python script to generate the CSVs

**Guardrails**:
- **Maximum issues to process**: 200 (100 open + 100 closed from pre-downloaded data)
- **Maximum PRs to process**: 130 (50 open + 50 merged + 30 closed from pre-downloaded data)
- **Maximum commits to process**: 100 (from pre-downloaded data)
- **Date range**: Last 30 days from the data available
- If data is sparse, use what's available and note it in the analysis

**Phase 3: Chart Generation**

Generate exactly **2 high-quality trend charts**:

**Chart 1: Issues & Pull Requests Activity**
- Multi-line chart showing:
  - Issues opened (line)
  - Issues closed (line)
  - PRs opened (line)
  - PRs merged (line)
- X-axis: Date (last 30 days)
- Y-axis: Count
- Include a 7-day moving average overlay if data is noisy
- Save as: `/tmp/gh-aw/python/charts/issues_prs_trends.png`

**Chart 2: Commit Activity & Contributors**
- Dual-axis chart or stacked visualization showing:
  - Daily commit count (bar chart or line)
  - Number of unique contributors (line with markers)
- X-axis: Date (last 30 days)
- Y-axis: Count
- Save as: `/tmp/gh-aw/python/charts/commit_trends.png`

**Chart Quality Requirements**:
- DPI: 300 minimum
- Figure size: 12x7 inches for better readability
- Use seaborn styling with a professional color palette
- Include grid lines for easier reading
- Clear, large labels and legend
- Title with context (e.g., "Issues & PR Activity - Last 30 Days")
- Annotations for significant peaks or patterns

**Phase 4: Upload Charts**

1. Upload both charts using the `upload asset` tool
2. Collect the returned URLs for embedding in the discussion

**Phase 5: Embed Charts in Discussion**

Include the charts in your daily news discussion report with this structure:

```markdown
## 📈 Trend Analysis

### Issues & Pull Requests Activity
![Issues and PR Trends](URL_FROM_UPLOAD_ASSET_CHART_1)

[Brief 2-3 sentence analysis of the trends shown in this chart, highlighting notable patterns, increases, decreases, or insights]

### Commit Activity & Contributors
![Commit Activity Trends](URL_FROM_UPLOAD_ASSET_CHART_2)

[Brief 2-3 sentence analysis of the trends shown in this chart, noting developer engagement, busy periods, or collaboration patterns]
```

### Python Implementation Notes

- Use pandas for data manipulation and date handling
- Use matplotlib.pyplot and seaborn for visualization
- Set appropriate date formatters for x-axis labels
- Use `plt.xticks(rotation=45)` for readable date labels
- Apply `plt.tight_layout()` before saving
- Handle cases where data might be sparse or missing

### Error Handling

If insufficient data is available (less than 7 days):
- Generate the charts with available data
- Add a note in the analysis mentioning the limited data range
- Consider using a bar chart instead of line chart for very sparse data

---

**Data Sources** - Use the pre-downloaded files in `/tmp/gh-aw/daily-news-data/`:
- Include some or all of the following from the JSON files:
  * Recent issues activity (from `issues.json`)
  * Recent pull requests (from `pull_requests.json`)
  * Recent discussions (from `discussions.json`)
  * Recent releases (from `releases.json`)
  * Recent code changes (from `commits.json`)
  * Changesets (from `changesets.txt` file list)

- If little has happened, don't write too much.

- Give some deep thought to ways the team can improve their productivity, and suggest some ways to do that.

- Include a description of open source community engagement, if any.

- Highlight suggestions for possible investment, ideas for features and project plan, ways to improve community engagement, and so on.

- Be helpful, thoughtful, respectful, positive, kind, and encouraging.

- Use emojis to make the report more engaging and fun, but don't overdo it.

- Include a short haiku at the end of the report to help orient the team to the season of their work.

## 📝 Report Formatting Guidelines

Follow these formatting guidelines to create well-structured, readable news reports:

### 1. Header Levels
**Use h3 (###) or lower for all headers in your news report to maintain proper document hierarchy.**

When creating your news report:
- Use `###` (h3) for main sections (e.g., "### Top News", "### Trend Analysis")
- Use `####` (h4) for subsections (e.g., "#### Recent Releases", "#### Community Engagement")
- Never use `##` (h2) or `#` (h1) in the report body - these are reserved for titles

### 2. Progressive Disclosure
**Wrap detailed news analysis and long article sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability.**

Use collapsible sections for:
- Detailed article analysis
- Verbose commit logs or detailed change descriptions
- Additional news items that provide extra context
- Extended lists of issues or pull requests

Always keep critical information visible:
- Brief summary of top news items
- Key headlines with links
- High-level trend insights
- Important recommendations or takeaways

Example structure:
```markdown
<details>
<summary><b>Full News Analysis</b></summary>

[Long detailed content here...]

</details>
```

### 3. Suggested Report Structure

Structure your news report with these sections:

1. **Brief Summary** (always visible): 1-2 paragraphs highlighting the most important news
2. **Key Headlines** (always visible): Top 3-5 headlines with links to issues/PRs/releases
3. **📈 Trend Analysis** (always visible): Include the 2 required charts with brief analysis
4. **Detailed Article Analysis** (in `<details>` tags): Deep dive into specific items
5. **Additional News Items** (in `<details>` tags): Secondary stories and updates
6. **Recommendations & Takeaways** (always visible): Actionable insights for the team

### Design Principles

Your reports should:
- **Build trust through clarity**: Most important info immediately visible
- **Exceed expectations**: Add helpful context, summaries, and insights
- **Create delight**: Use progressive disclosure to reduce overwhelm
- **Maintain consistency**: Follow the same patterns as other reporting workflows

- In a note at the end of the report, include a log of:
  * All web search queries you used (if any)
  * All files you read from `/tmp/gh-aw/daily-news-data/`
  * Summary statistics: number of issues/PRs/commits/discussions analyzed
  * Date range of data analyzed
  * Any data limitations encountered

Create a new GitHub discussion with a title containing today's date (e.g., "Daily Status - 2024-10-10") containing a markdown report with your findings. Use links where appropriate.

Only a new discussion should be created, do not close or update any existing discussions.