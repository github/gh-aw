---
emoji: "✅"
description: Detects inconsistencies between JSON schema, implementation code, and documentation
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  discussions: read
  issues: read
  pull-requests: read
model: openai/gpt-5.3-codex
engine:
  id: codex
  model-provider: openai
max-ai-credits: 1500
tools:
  cli-proxy: true
  edit:
  bash: ["*"]
  github:
    mode: local
    toolsets: [default, discussions]
  cache-memory:
    key: schema-consistency-cache-${{ github.workflow }}
timeout-minutes: 30
checkout:
  - fetch-depth: 1
    current: true
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[Schema Consistency] "
      expires: 1d
  - shared/otlp.md
pre-agent-steps:
  - name: Precompute schema analysis data
    run: |
      set -e
      mkdir -p /tmp/gh-aw/agent

      echo "=== Extracting schema fields ==="

      # 1. All top-level fields in the main JSON schema
      SCHEMA_FIELDS=$(jq -r '.properties | keys[]' pkg/parser/schemas/main_workflow_schema.json 2>/dev/null | sort -u || echo "")

      # 2. JSON/YAML-tagged fields in the top-level frontmatter type.
      # pkg/parser/frontmatter.go is only a logger declaration; frontmatter fields
      # are extracted and represented in pkg/workflow/frontmatter_types.go.
      FRONTMATTER_FIELDS=$(sed -n '/^type FrontmatterConfig struct {$/,/^}$/p' pkg/workflow/frontmatter_types.go 2>/dev/null \
        | grep -Eo '(json|yaml):"[^"]*"' \
        | sed -E 's/^(json|yaml):"//;s/"$//' \
        | sed 's/,omitempty//' \
        | sed 's/,.*$//' \
        | grep -v '^-$' \
        | grep -v '^$' \
        | sort -u || echo "")

      # 3. yaml-tagged struct fields in pkg/workflow/*.go
      WORKFLOW_YAML_FIELDS=$(grep -rh 'yaml:"' pkg/workflow/*.go 2>/dev/null \
        | grep -o 'yaml:"[^"]*"' \
        | sed 's/yaml:"//;s/"//' \
        | sed 's/,omitempty//' \
        | sed 's/,.*$//' \
        | grep -v '^-$' \
        | grep -v '^$' \
        | sort -u || echo "")

      # 4. Top-level frontmatter keys actually used in workflow .md files
      USED_FIELDS=$(bash scripts/extract-workflow-frontmatter-keys.sh .github/workflows/*.md 2>/dev/null || echo "")

      # 5. Schema field types for all top-level fields
      FIELD_TYPES=$(jq -r '.properties | to_entries[] |
        "\(.key): \(.value.type // (.value.anyOf // .value.oneOf // [] | map(.type // "complex") | unique | join("|")) // "complex")"' \
        pkg/parser/schemas/main_workflow_schema.json 2>/dev/null | sort || echo "")

      # 6. Fields in schema but absent from the frontmatter type definitions
      IN_SCHEMA_NOT_FRONTMATTER=$(comm -23 \
        <(echo "$SCHEMA_FIELDS") \
        <(echo "$FRONTMATTER_FIELDS" | sort -u) 2>/dev/null || echo "")

      # 7. Fields in frontmatter type definitions absent from schema
      IN_FRONTMATTER_NOT_SCHEMA=$(comm -23 \
        <(echo "$FRONTMATTER_FIELDS" | sort -u) \
        <(echo "$SCHEMA_FIELDS") 2>/dev/null || echo "")

      # 8. Fields in schema but absent from workflow compiler structs
      IN_SCHEMA_NOT_WORKFLOW=$(comm -23 \
        <(echo "$SCHEMA_FIELDS") \
        <(echo "$WORKFLOW_YAML_FIELDS" | sort -u) 2>/dev/null || echo "")

      # 9. Fields used in actual workflow .md files but not in schema
      IN_USED_NOT_SCHEMA=$(comm -23 \
        <(echo "$USED_FIELDS" | sort -u) \
        <(echo "$SCHEMA_FIELDS") 2>/dev/null || echo "")

      # Write JSON output
      jq -n \
        --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
        --arg schema_fields "$SCHEMA_FIELDS" \
        --arg frontmatter_fields "$FRONTMATTER_FIELDS" \
        --arg workflow_yaml_fields "$WORKFLOW_YAML_FIELDS" \
        --arg used_in_workflows "$USED_FIELDS" \
        --arg field_types "$FIELD_TYPES" \
        --arg in_schema_not_frontmatter "$IN_SCHEMA_NOT_FRONTMATTER" \
        --arg in_frontmatter_not_schema "$IN_FRONTMATTER_NOT_SCHEMA" \
        --arg in_schema_not_workflow "$IN_SCHEMA_NOT_WORKFLOW" \
        --arg in_used_not_schema "$IN_USED_NOT_SCHEMA" \
        '{
          generated_at: $generated_at,
          schema_fields: ($schema_fields | split("\n") | map(select(. != ""))),
          frontmatter_fields: ($frontmatter_fields | split("\n") | map(select(. != ""))),
          workflow_yaml_fields: ($workflow_yaml_fields | split("\n") | map(select(. != ""))),
          used_in_workflows: ($used_in_workflows | split("\n") | map(select(. != ""))),
          field_types: ($field_types | split("\n") | map(select(. != ""))),
          field_gaps: {
            in_schema_not_frontmatter: ($in_schema_not_frontmatter | split("\n") | map(select(. != ""))),
            in_frontmatter_not_schema: ($in_frontmatter_not_schema | split("\n") | map(select(. != ""))),
            in_schema_not_workflow: ($in_schema_not_workflow | split("\n") | map(select(. != ""))),
            in_used_not_schema: ($in_used_not_schema | split("\n") | map(select(. != "")))
          }
        }' > /tmp/gh-aw/agent/schema-diff.json

      echo "✓ Schema diff written to /tmp/gh-aw/agent/schema-diff.json"
      echo "Summary:"
      jq '{
        schema_field_count: (.schema_fields | length),
        frontmatter_field_count: (.frontmatter_fields | length),
        workflow_yaml_field_count: (.workflow_yaml_fields | length),
        gaps: {
          in_schema_not_frontmatter: (.field_gaps.in_schema_not_frontmatter | length),
          in_frontmatter_not_schema: (.field_gaps.in_frontmatter_not_schema | length),
          in_schema_not_workflow: (.field_gaps.in_schema_not_workflow | length),
          in_used_not_schema: (.field_gaps.in_used_not_schema | length)
        }
      }' /tmp/gh-aw/agent/schema-diff.json

      echo "=== AWF config source drift pre-check (gh-aw-firewall) ==="
      AWF_SNAPSHOT_CACHE_DIR=/tmp/gh-aw/cache-memory/awf-config-sources
      if [ "${RUNNER_ENVIRONMENT:-github-hosted}" = "self-hosted" ]; then
        AWF_SNAPSHOT_DIR="${HOME}/.cache/gh-aw/schema-consistency/last-known-snapshot"
      else
        AWF_SNAPSHOT_DIR=/tmp/gh-aw/agent/schema-consistency/last-known-snapshot
      fi
      mkdir -p "$AWF_SNAPSHOT_DIR"
      if [ -d "$AWF_SNAPSHOT_CACHE_DIR" ]; then
        cp -a "$AWF_SNAPSHOT_CACHE_DIR/." "$AWF_SNAPSHOT_DIR/"
      fi
      AWF_CANONICAL_FETCH_DEGRADED=false
      AWF_USING_SNAPSHOT=false
      AWF_SNAPSHOT_EXPIRED=false
      AWF_FETCH_FAILED_SOURCES=""
      AWF_SNAPSHOT_MAX_AGE_SECONDS=604800
      AWF_SNAPSHOT_DELETE_AGE_SECONDS=1209600

      snapshot_age_seconds() {
        [ -s "$AWF_SNAPSHOT_DIR/detected_at" ] || return 1
        snapshot_epoch=$(date -u -d "$(cat "$AWF_SNAPSHOT_DIR/detected_at")" +%s 2>/dev/null) || return 1
        now_epoch=$(date -u +%s)
        [ "$snapshot_epoch" -le "$now_epoch" ] || return 1
        printf '%s\n' "$((now_epoch - snapshot_epoch))"
      }

      AWF_SNAPSHOT_AGE_SECONDS=$(snapshot_age_seconds || true)
      if [ -e "$AWF_SNAPSHOT_DIR/awf-config.schema.json" ] && [ -z "$AWF_SNAPSHOT_AGE_SECONDS" ]; then
        AWF_SNAPSHOT_EXPIRED=true
        echo "AWF last-known snapshot has no valid refresh timestamp; stale data will not suppress drift warnings"
      elif [ -n "$AWF_SNAPSHOT_AGE_SECONDS" ] && [ "$AWF_SNAPSHOT_AGE_SECONDS" -gt "$AWF_SNAPSHOT_MAX_AGE_SECONDS" ]; then
        AWF_SNAPSHOT_EXPIRED=true
        echo "AWF last-known snapshot is older than 7 days; stale data will not suppress drift warnings"
        if [ "$AWF_SNAPSHOT_AGE_SECONDS" -gt "$AWF_SNAPSHOT_DELETE_AGE_SECONDS" ]; then
          rm -rf "$AWF_SNAPSHOT_DIR" "$AWF_SNAPSHOT_CACHE_DIR"
          mkdir -p "$AWF_SNAPSHOT_DIR"
        fi
      fi

      fetch_awf_source() {
        local source_path="$1"
        local target_path="$2"
        if ! gh api -H "Accept: application/vnd.github.raw" "/repos/github/gh-aw-firewall/contents/${source_path}" > "$target_path"; then
          AWF_CANONICAL_FETCH_DEGRADED=true
          AWF_FETCH_FAILED_SOURCES="${AWF_FETCH_FAILED_SOURCES}${source_path}\n"
          rm -f "$target_path"
          return 1
        fi
      }

      if [ -n "${GH_TOKEN:-${GITHUB_TOKEN:-}}" ]; then
        fetch_awf_source docs/awf-config.schema.json /tmp/gh-aw/agent/awf-config.schema.json || true
        fetch_awf_source src/awf-config-schema.json /tmp/gh-aw/agent/awf-config-runtime.schema.json || true
        fetch_awf_source docs/awf-config-spec.md /tmp/gh-aw/agent/awf-config-spec.md || true
      else
        AWF_CANONICAL_FETCH_DEGRADED=true
        AWF_FETCH_FAILED_SOURCES="docs/awf-config.schema.json\nsrc/awf-config-schema.json\ndocs/awf-config-spec.md\n"
        echo "⚠️ AWF canonical source fetch degraded: GH_TOKEN/GITHUB_TOKEN is not set"
      fi

      for source_path in docs/awf-config.schema.json src/awf-config-schema.json docs/awf-config-spec.md; do
        source_file=$(basename "$source_path")
        if [ "$source_path" = "src/awf-config-schema.json" ]; then
          target_path=/tmp/gh-aw/agent/awf-config-runtime.schema.json
          source_file=awf-config-runtime.schema.json
        else
          target_path=/tmp/gh-aw/agent/"$source_file"
        fi

        if [ ! -s "$target_path" ] && [ "$AWF_SNAPSHOT_EXPIRED" = false ] && [ -s "$AWF_SNAPSHOT_DIR/$source_file" ]; then
          cp "$AWF_SNAPSHOT_DIR/$source_file" "$target_path"
          AWF_USING_SNAPSHOT=true
          echo "Using last-known AWF snapshot for $source_path"
        fi
      done

      if [ -s /tmp/gh-aw/agent/awf-config.schema.json ] && [ -s /tmp/gh-aw/agent/awf-config-runtime.schema.json ] && [ -s /tmp/gh-aw/agent/awf-config-spec.md ]; then
        if [ "$AWF_CANONICAL_FETCH_DEGRADED" = false ]; then
          cp /tmp/gh-aw/agent/awf-config.schema.json "$AWF_SNAPSHOT_DIR/awf-config.schema.json"
          cp /tmp/gh-aw/agent/awf-config-runtime.schema.json "$AWF_SNAPSHOT_DIR/awf-config-runtime.schema.json"
          cp /tmp/gh-aw/agent/awf-config-spec.md "$AWF_SNAPSHOT_DIR/awf-config-spec.md"
          date -u +%Y-%m-%dT%H:%M:%SZ > "$AWF_SNAPSHOT_DIR/detected_at"
          rm -rf "$AWF_SNAPSHOT_CACHE_DIR"
          mkdir -p "$AWF_SNAPSHOT_CACHE_DIR"
          cp -a "$AWF_SNAPSHOT_DIR/." "$AWF_SNAPSHOT_CACHE_DIR/"
        fi

        jq -r '.properties | keys[]' /tmp/gh-aw/agent/awf-config.schema.json | sort -u \
          > /tmp/gh-aw/agent/awf-config-top-level.txt
        jq -r '.properties | keys[]' /tmp/gh-aw/agent/awf-config-runtime.schema.json | sort -u \
          > /tmp/gh-aw/agent/awf-config-runtime-top-level.txt
        rg --no-heading --no-filename 'apiProxy|container|sandbox|auth|network' pkg/workflow actions/setup \
          | head -200 > /tmp/gh-aw/agent/awf-config-ghaw-refs.txt || true

        FAILED_SOURCES_JSON=$(printf "%b" "$AWF_FETCH_FAILED_SOURCES" | sed '/^$/d' | sort -u | jq -Rsc 'split("\n") | map(select(length > 0))')
        jq -n \
          --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
          --arg spec_path "docs/awf-config-spec.md" \
          --arg schema_path "docs/awf-config.schema.json" \
          --arg runtime_schema_path "src/awf-config-schema.json" \
          --arg top_level_count "$(wc -l < /tmp/gh-aw/agent/awf-config-top-level.txt | tr -d ' ')" \
          --arg runtime_top_level_count "$(wc -l < /tmp/gh-aw/agent/awf-config-runtime-top-level.txt | tr -d ' ')" \
          --arg refs_sample_count "$(wc -l < /tmp/gh-aw/agent/awf-config-ghaw-refs.txt | tr -d ' ')" \
          --arg degraded "$AWF_CANONICAL_FETCH_DEGRADED" \
          --arg using_snapshot "$AWF_USING_SNAPSHOT" \
          --arg snapshot_expired "$AWF_SNAPSHOT_EXPIRED" \
          --arg snapshot_path "$AWF_SNAPSHOT_DIR" \
          --argjson failed_sources "$FAILED_SOURCES_JSON" \
          '{
            generated_at: $generated_at,
            source_repo: "github/gh-aw-firewall",
            canonical_spec: $spec_path,
            canonical_schema: $schema_path,
            canonical_runtime_schema: $runtime_schema_path,
            top_level_property_count: ($top_level_count | tonumber),
            runtime_top_level_property_count: ($runtime_top_level_count | tonumber),
            ghaw_reference_sample_count: ($refs_sample_count | tonumber),
            degraded: ($degraded == "true"),
            using_snapshot: ($using_snapshot == "true"),
            snapshot_expired: ($snapshot_expired == "true"),
            snapshot_path: $snapshot_path,
            failed_sources: $failed_sources
          }' > /tmp/gh-aw/agent/awf-config-drift.json
        if [ "$AWF_CANONICAL_FETCH_DEGRADED" = true ]; then
          printf 'AWF canonical source retrieval failed at %s for:\n%b' \
            "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$AWF_FETCH_FAILED_SOURCES"
          echo "AWF canonical source fetch degraded; continuing in non-fatal mode"
        else
          echo "✓ AWF config source pre-check artifacts written under /tmp/gh-aw/agent/"
        fi
      else
        AWF_CANONICAL_FETCH_DEGRADED=true
        FAILED_SOURCES_JSON=$(printf "%b" "$AWF_FETCH_FAILED_SOURCES" | sed '/^$/d' | sort -u | jq -Rsc 'split("\n") | map(select(length > 0))')
        jq -n \
          --arg generated_at "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
          --argjson failed_sources "$FAILED_SOURCES_JSON" \
          '{
            generated_at: $generated_at,
            source_repo: "github/gh-aw-firewall",
            degraded: true,
            warning: "canonical source retrieval failed; skipping destructive AWF drift actions",
            failed_sources: $failed_sources
          }' > /tmp/gh-aw/agent/awf-config-drift.json
        printf 'AWF canonical source retrieval failed at %s for:\n%b' \
          "$(date -u +%Y-%m-%dT%H:%M:%SZ)" "$AWF_FETCH_FAILED_SOURCES"
        echo "AWF canonical source fetch failed; run marked degraded (non-fatal)"
      fi
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Schema Consistency Checker

You are an expert system that detects inconsistencies between:
- The main JSON schema of the frontmatter (`pkg/parser/schemas/main_workflow_schema.json`)
- The parser and compiler implementation (`pkg/workflow/frontmatter_types.go` and `pkg/workflow/*.go`)
- The documentation (`docs/src/content/docs/**/*.md`)
- The workflows in the project (`.github/workflows/*.md`)

## Mission

Analyze the repository to find inconsistencies across these four key areas and create a discussion report with actionable findings.

Before reporting AWF config-source drift, read `/tmp/gh-aw/agent/awf-config-drift.json`. When `degraded` is true, report canonical-source unavailability as a non-authoritative warning only. Do not fail a required check, create a corrective pull request, or create a drift issue from the incomplete or stale AWF comparison.

## Cache Memory Strategy Storage

Use the cache memory folder at `/tmp/gh-aw/cache-memory/` to store and reuse successful analysis strategies:

1. **Read Previous Strategies**: Check `/tmp/gh-aw/cache-memory/strategies.json` for previously successful detection methods
2. **Strategy Selection**: 
   - 70% of the time: Use a proven strategy from the cache
   - 30% of the time: Try a radically different approach to discover new inconsistencies
   - Implementation: Use the day of year (e.g., `date +%j`) modulo 10 to determine selection: values 0-6 use proven strategies, 7-9 try new approaches
3. **Update Strategy Database**: After analysis, save successful strategies to `/tmp/gh-aw/cache-memory/strategies.json`

Strategy database structure:
```json
{
  "strategies": [
    {
      "id": "strategy-1",
      "name": "Schema field enumeration check",
      "description": "Compare schema enum values with parser constants",
      "success_count": 5,
      "last_used": "2024-01-15",
      "findings": 3
    }
  ],
  "last_updated": "2024-01-15"
}
```

## Analysis Areas

### 1. Schema vs Parser Implementation

**Check for:**
- Fields defined in schema but not handled in parser/compiler
- Fields handled in parser/compiler but missing from schema
- Type mismatches (schema says `string`, parser expects `object`)
- Enum values in schema not validated in parser/compiler
- Required fields not enforced
- Default values inconsistent between schema and parser/compiler

**Key files to analyze:**
- `pkg/parser/schemas/main_workflow_schema.json`
- `pkg/parser/schemas/mcp_config_schema.json`
- `pkg/workflow/frontmatter_types.go` and `pkg/workflow/frontmatter_extraction_yaml.go`
- `pkg/workflow/compiler.go` - main workflow compiler
- `pkg/workflow/tools.go` - tools configuration processing
- `pkg/workflow/safe_outputs.go` - safe-outputs configuration
- `pkg/workflow/cache.go` - cache and cache-memory configuration
- `pkg/workflow/permissions.go` - permissions processing
- `pkg/workflow/engine.go` - engine config and network permissions types
- `pkg/workflow/domains.go` - network domain allowlist functions
- `pkg/workflow/engine_network_hooks.go` - network hook generation
- `pkg/workflow/engine_firewall_support.go` - firewall support checking
- `pkg/workflow/strict_mode.go` - strict mode validation
- `pkg/workflow/stop_after.go` - stop-after processing
- `pkg/workflow/safe_jobs.go` - safe-jobs configuration (internal - accessed via safe-outputs.jobs)
- `pkg/workflow/runtime_setup.go` - runtime overrides
- `pkg/workflow/github_token.go` - github-token configuration
- `pkg/workflow/*.go` (all workflow processing files that use frontmatter)

### 2. Schema vs Documentation

**Check for:**
- Schema fields not documented
- Documented fields not in schema
- Type descriptions mismatch
- Example values that violate schema
- Missing or outdated examples
- Enum values documented but not in schema

**Key files to analyze:**
- `docs/src/content/docs/reference/frontmatter.md`
- `docs/src/content/docs/reference/frontmatter-full.md`
- `docs/src/content/docs/reference/*.md` (all reference docs)

### 3. Schema vs Actual Workflows

**Check for:**
- Workflows using fields not in schema
- Workflows using deprecated fields
- Invalid field values according to schema
- Missing required fields
- Type violations in actual usage
- Undocumented field combinations

**Key files to analyze:**
- `.github/workflows/*.md` (all workflow files)
- `.github/workflows/shared/**/*.md` (shared components)

### 4. Parser vs Documentation

**Check for:**
- Parser/compiler features not documented
- Documented features not implemented in parser/compiler
- Error messages that don't match docs
- Validation rules not documented

**Focus on:**
- `pkg/workflow/frontmatter_types.go` - frontmatter field definitions
- `pkg/workflow/*.go` - workflow compilation and feature processing

## Detection Strategies

Here are proven strategies you can use or build upon:

### Strategy 1: Field Enumeration Diff
1. Extract all field names from schema
2. Extract all field names from parser code (look for YAML tags, map keys)
3. Extract all field names from documentation
4. Compare and find missing/extra fields

### Strategy 2: Type Analysis
1. For each field in schema, note its type
2. Search parser for how that field is processed
3. Check if types match
4. Report type mismatches

### Strategy 3: Enum Validation
1. Extract enum values from schema
2. Search for those enums in parser validation
3. Check if all enum values are handled
4. Find undocumented enum values

### Strategy 4: Example Validation
1. Extract code examples from documentation
2. Validate each example against the schema
3. Report examples that don't validate
4. Suggest corrections

### Strategy 5: Real-World Usage Analysis
1. Parse all workflow files in the repo
2. Extract frontmatter configurations
3. Check each against schema
4. Find patterns that work but aren't in schema (potential missing features)

### Strategy 6: Grep-Based Pattern Detection
1. Use bash/grep to find specific patterns
2. Example: `grep -r "type.*string" pkg/parser/schemas/ | grep engine`
3. Cross-reference with parser implementation

## Turn Budget

You have a maximum turn budget. **Spend turns wisely**:

- **Batch multiple bash checks into a single command** using `&&`, `||`, or heredocs — one tool call is always better than two.
- **Stop investigating once you have enough findings** for the report. Depth on 3–5 real issues is better than breadth over 20 superficial checks.
- **Prioritize field_gaps** from the pre-computed data — they are high-signal and require no additional discovery turns.
- **Do NOT iterate through every schema field one-by-one** — use bulk grep/jq queries that scan all fields in a single pass.
- **Skip a category** if you have already found 3+ findings from other categories — you do not need to cover every area every run.

## Implementation Steps

### Step 0: Read Pre-Computed Data + Strategies in One Pass (Start Here)

Before doing anything else, read both the schema diff and the strategy cache in a single command:

```bash
echo "=== SCHEMA DIFF ===" && cat /tmp/gh-aw/agent/schema-diff.json && \
echo "=== STRATEGIES ===" && \
([ -f /tmp/gh-aw/cache-memory/strategies.json ] && cat /tmp/gh-aw/cache-memory/strategies.json || echo "No strategies cached yet")
```

The schema diff contains:
- `schema_fields`: All top-level field names in the main JSON schema
- `frontmatter_fields`: All JSON/YAML-tagged fields in `pkg/workflow/frontmatter_types.go`
- `workflow_yaml_fields`: All yaml-tagged struct fields in `pkg/workflow/*.go`
- `used_in_workflows`: All top-level frontmatter keys used in `.github/workflows/*.md`
- `field_types`: Schema field types for all top-level fields
- `field_gaps.in_schema_not_frontmatter`: Fields in schema absent from frontmatter type definitions
- `field_gaps.in_frontmatter_not_schema`: Fields in frontmatter type definitions absent from schema
- `field_gaps.in_schema_not_workflow`: Fields in schema absent from workflow compiler yaml tags
- `field_gaps.in_used_not_schema`: Fields used in workflow files but not in schema

**Use this pre-computed data as your primary starting point.** Do NOT re-run the field enumeration commands from scratch — instead, refine and supplement the pre-computed data with targeted follow-up queries (e.g., checking a specific file for a specific field).

### Step 1: Choose Analysis Focus

Using the pre-computed `field_gaps` plus the strategy cache:
- If `field_gaps` show promising leads, start there (they are likely high-signal)
- If cache has strategies, use a proven strategy 70% of the time; try a new approach 30% of the time

```bash
# Determine selection mode (0-6 = proven strategy, 7-9 = new approach)
day_mod=$(( $(date +%j) % 10 ))
if [ "$day_mod" -le 6 ]; then
  echo "Use proven strategy from cache"
else
  echo "Try new approach"
fi
```

### Step 2: Execute Targeted Analysis (Batch Operations)

Use the pre-computed data as context and run **targeted, batched** follow-up commands only when
deeper inspection is needed.

**Batch multiple verifications together** (one call, multiple results):
```bash
# Replace fieldA/fieldB/fieldC with actual field names from field_gaps in the pre-computed data
for field in fieldA fieldB fieldC; do
  echo "=== $field ===" && \
  grep -r "$field" pkg/parser/ pkg/workflow/ 2>/dev/null | grep -v "_test.go" | head -5 || echo "(not found)"
done
```

**Bulk type checking — all fields in one jq pass**:
```bash
jq -r '
  (.properties // {}) | to_entries[] | 
  "\(.key): \(.value.type // .value.oneOf // .value.anyOf // .value.allOf // "complex")"
' pkg/parser/schemas/main_workflow_schema.json 2>/dev/null || echo "Failed to parse schema"
```

### Step 3: Record Findings
Create a structured list of inconsistencies found:

```markdown
## Inconsistencies Found

### Schema ↔ Parser Mismatches
1. **Field `engine.version`**: 
   - Schema: defines as string
   - Parser: not validated in frontmatter.go
   - Impact: Invalid values could pass through

### Schema ↔ Documentation Mismatches  
1. **Field `cache-memory`**:
   - Schema: defines array of objects with `id` and `key`
   - Docs: only shows simple boolean example
   - Impact: Advanced usage not documented

### Parser ↔ Documentation Mismatches
1. **Error message for invalid `on` field**:
   - Parser: "trigger configuration is required"
   - Docs: doesn't mention this error
   - Impact: Users may not understand error
```

### Step 4: Update Cache
Save successful strategy and findings to cache:
```bash
# Update strategies.json with results
cat > /tmp/gh-aw/cache-memory/strategies.json << 'EOF'
{
  "strategies": [...],
  "last_updated": "2024-XX-XX"
}
EOF
```

### Step 5: Create Discussion

**⚠️ MANDATORY STEP**: After completing your analysis, you **MUST** call the `create_discussion` safe-output tool with your findings report. **DO NOT just write the report in your output text** — you MUST actually invoke the tool. The workflow will fail if you skip this step.

Use this discussion format for the content you pass to `create_discussion`:

## Discussion Report Format

Create a well-structured discussion report:

```markdown
# 🔍 Schema Consistency Check - [DATE]

## Summary

- **Inconsistencies Found**: [NUMBER]
- **Categories Analyzed**: Schema, Parser, Documentation, Workflows
- **Strategy Used**: [STRATEGY NAME]
- **New Strategy**: [YES/NO]

## Critical Issues

[List high-priority inconsistencies that could cause bugs]

## Documentation Gaps

[List areas where docs don't match reality]

## Schema Improvements Needed

[List schema enhancements needed]

## Parser Updates Required

[List parser code that needs updates]

## Workflow Violations

[List workflows using invalid/undocumented features]

## Recommendations

1. [Specific actionable recommendation]
2. [Specific actionable recommendation]
3. [...]

## Strategy Performance

- **Strategy Used**: [NAME]
- **Findings**: [COUNT]
- **Effectiveness**: [HIGH/MEDIUM/LOW]
- **Should Reuse**: [YES/NO]

## Next Steps

- [ ] Fix schema definitions
- [ ] Update parser validation
- [ ] Update documentation
- [ ] Fix workflow files
```

## Important Guidelines

### Security
- Never execute untrusted code from workflows
- Validate all file paths before reading
- Sanitize all grep/bash commands
- Read-only access to schema, parser, and documentation files for analysis
- Only modify files in `/tmp/gh-aw/cache-memory/` (never modify source files)

### Quality
- Be thorough but focused on actionable findings
- Prioritize issues by severity (critical bugs vs documentation gaps)
- Provide specific file:line references when possible
- Include code snippets to illustrate issues
- Suggest concrete fixes

### Efficiency  
- **Always start from `/tmp/gh-aw/agent/schema-diff.json`** — this pre-computed diff eliminates the need to re-read all source files
- Use targeted bash commands to verify specific leads from the pre-computed data
- Cache results when re-analyzing same data
- Don't re-check things found in previous runs (check cache first)
- Focus on high-impact areas (field gaps with parser mismatches are usually most critical)

### Strategy Evolution
- Try genuinely different approaches when not using cached strategies
- Document why a strategy worked or failed
- Update success metrics in cache
- Consider combining successful strategies

## Tools Available

You have access to:
- **bash**: Any command (use grep, jq, find, cat, etc.)
- **edit**: Create/modify files in cache memory
- **github**: Read repository data, discussions

## Success Criteria

A successful run:
- ✅ Analyzes all 4 areas (schema, parser, docs, workflows)
- ✅ Uses or creates an effective detection strategy
- ✅ Updates cache with strategy results
- ✅ Finds at least one category of inconsistencies OR confirms consistency
- ✅ Creates a detailed discussion report
- ✅ Provides actionable recommendations

Begin your analysis now. Check the cache, choose a strategy, execute it, and **call `create_discussion` with your findings** to complete the workflow.