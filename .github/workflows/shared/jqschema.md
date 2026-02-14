---
tools:
  bash:
    - "jq *"
    - "/tmp/gh-aw/jqschema.sh"
    - "git"
steps:
  - name: Setup jq utilities directory
    run: |
      mkdir -p /tmp/gh-aw
      cat > /tmp/gh-aw/jqschema.sh << 'EOF'
      #!/usr/bin/env bash
      # jqschema.sh
      jq -c '
      def walk(f):
        . as $in |
        if type == "object" then
          reduce keys[] as $k ({}; . + {($k): ($in[$k] | walk(f))})
        elif type == "array" then
          if length == 0 then [] else [.[0] | walk(f)] end
        else
          type
        end;
      walk(.)
      '
      EOF
      chmod +x /tmp/gh-aw/jqschema.sh
---

## jqschema - JSON Schema Discovery

A utility script is available at `/tmp/gh-aw/jqschema.sh` to help you discover the structure of complex JSON responses.

### Purpose

Generate a compact structural schema (keys + types) from JSON input. This is particularly useful when:
- Analyzing tool outputs from GitHub search (search_code, search_issues, search_repositories)
- Exploring API responses with large payloads
- Understanding the structure of unfamiliar data without verbose output
- Planning queries before fetching full data

### Usage

```bash
# Analyze a file
cat data.json | /tmp/gh-aw/jqschema.sh

# Analyze command output
echo '{"name": "test", "count": 42, "items": [{"id": 1}]}' | /tmp/gh-aw/jqschema.sh

# Analyze GitHub search results
gh api search/repositories?q=language:go | /tmp/gh-aw/jqschema.sh
```

### How It Works

The script transforms JSON data by:
1. Replacing object values with their type names ("string", "number", "boolean", "null")
2. Reducing arrays to their first element's structure (or empty array if empty)
3. Recursively processing nested structures
4. Outputting compact (minified) JSON

### Example

**Input:**
```json
{
  "total_count": 1000,
  "items": [
    {"login": "user1", "id": 123, "verified": true},
    {"login": "user2", "id": 456, "verified": false}
  ]
}
```

**Output:**
```json
{"total_count":"number","items":[{"login":"string","id":"number","verified":"boolean"}]}
```

### Best Practices

**Use this script when:**
- You need to understand the structure of tool outputs before requesting full data
- GitHub search tools return large datasets (use `perPage: 1` and pipe through schema minifier first)
- Exploring unfamiliar APIs or data structures
- Planning data extraction strategies

**Example workflow for GitHub search tools:**
```bash
# Step 1: Get schema with minimal data (fetch just 1 result)
# This helps understand the structure before requesting large datasets
echo '{}' | gh api search/repositories -f q="language:go" -f per_page=1 | /tmp/gh-aw/jqschema.sh

# Output shows the schema:
# {"incomplete_results":"boolean","items":[{...}],"total_count":"number"}

# Step 2: Review schema to understand available fields

# Step 3: Request full data with confidence about structure
# Now you know what fields are available and can query efficiently
```

**Using with GitHub MCP tools:**
When using tools like `search_code`, `search_issues`, or `search_repositories`, pipe the output through jqschema to discover available fields:
```bash
# Save a minimal search result to a file
gh api search/code -f q="jq in:file language:bash" -f per_page=1 > /tmp/sample.json

# Generate schema to understand structure
cat /tmp/sample.json | /tmp/gh-aw/jqschema.sh

# Now you know which fields exist and can use them in your analysis
```
