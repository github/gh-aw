---
# Metrics Calculation Patterns
# Provides standardized Python and bash patterns for common repository metrics
#
# Usage:
#   imports:
#     - shared/metrics-patterns.md
#
# This import provides:
# - Standardized metric calculation code snippets
# - References to scratchpad/metrics-glossary.md for metric definitions
# - Python patterns for issue, PR, code, and workflow metrics
# - Bash/jq patterns for shell-based calculations
# - Trend calculation helper functions
# - Cross-platform compatibility utilities
#
# Benefits:
# - Single source of truth for metrics calculations
# - Consistent metric names across all workflows
# - Easier maintenance and bug fixes
# - Enables accurate cross-report comparisons

imports:
  - shared/python-dataviz.md
---

# Metrics Calculation Patterns

This shared component provides standardized code patterns for calculating common repository metrics, ensuring consistency across all reporting workflows.

**Reference**: All metric names and definitions follow `scratchpad/metrics-glossary.md` standards.

## Available Patterns

### 1. Issue Metrics (Python)

Use these patterns for analyzing GitHub issues data. All metric names follow `scratchpad/metrics-glossary.md` standards.

```python
import pandas as pd
import numpy as np
from datetime import datetime, timedelta
import json

# Load issues data
# See scratchpad/metrics-glossary.md for metric definitions
with open('/tmp/gh-aw/issues-data/issues.json', 'r') as f:
    issues = json.load(f)
df = pd.DataFrame(issues)

# Convert timestamps
df['createdAt'] = pd.to_datetime(df['createdAt'])
df['updatedAt'] = pd.to_datetime(df['updatedAt'])
df['closedAt'] = pd.to_datetime(df['closedAt'])

# Standardized metric calculations (see scratchpad/metrics-glossary.md)
now = datetime.now(df['createdAt'].iloc[0].tzinfo if len(df) > 0 else None)

# Volume metrics
# Scope: All issues in repository, no filters
total_issues = len(df)

# Scope: Issues where state = "open" at report time
open_issues = len(df[df['state'] == 'OPEN'])

# Scope: Issues where state = "closed" at report time
closed_issues = len(df[df['state'] == 'CLOSED'])

# Time-based metrics
# Scope: Issues created in last 7 days
issues_opened_7d = len(df[df['createdAt'] > now - timedelta(days=7)])

# Scope: Issues created in last 30 days
issues_opened_30d = len(df[df['createdAt'] > now - timedelta(days=30)])

# Scope: Issues closed in last 30 days
issues_closed_30d = len(df[df['closedAt'] > now - timedelta(days=30)])

# Quality metrics
# Scope: Issues with empty labels array
issues_without_labels = len(df[df['labels'].map(len) == 0])

# Scope: Issues with empty assignees array
issues_without_assignees = len(df[df['assignees'].map(len) == 0])

# Scope: Open issues with no activity in last 30 days
stale_issues = len(df[
    (df['state'] == 'OPEN') & 
    (df['updatedAt'] < now - timedelta(days=30))
])

# Average time to close
# Scope: Closed issues with valid timestamps
closed_df = df[df['closedAt'].notna()]
if len(closed_df) > 0:
    closed_df_copy = closed_df.copy()
    closed_df_copy['time_to_close'] = closed_df_copy['closedAt'] - closed_df_copy['createdAt']
    avg_close_time_days = closed_df_copy['time_to_close'].mean().days
else:
    avg_close_time_days = 0
```

### 2. Pull Request Metrics (jq + bash)

```bash
# Calculate PR metrics using jq
# Input: /tmp/gh-aw/pr-data/prs.json
# See scratchpad/metrics-glossary.md for metric definitions

# Volume metrics
# Scope: All PRs in repository
total_prs=$(jq 'length' /tmp/gh-aw/pr-data/prs.json)

# Scope: PRs where merged = true
merged_prs=$(jq '[.[] | select(.mergedAt != null)] | length' /tmp/gh-aw/pr-data/prs.json)

# Scope: PRs where state = "open"
open_prs=$(jq '[.[] | select(.state == "OPEN")] | length' /tmp/gh-aw/pr-data/prs.json)

# Scope: PRs where state = "closed" and merged = false
closed_prs=$(jq '[.[] | select(.state == "CLOSED" and .mergedAt == null)] | length' /tmp/gh-aw/pr-data/prs.json)

# Time-based metrics (last 7 days)
# Cross-platform date handling (works on GNU and BSD systems)
if date --version >/dev/null 2>&1; then
    # GNU date (Linux)
    DATE_7D=$(date -d '7 days ago' '+%Y-%m-%dT%H:%M:%SZ')
else
    # BSD date (macOS)
    DATE_7D=$(date -v-7d '+%Y-%m-%dT%H:%M:%SZ')
fi

# Scope: PRs created in last 7 days
prs_opened_7d=$(jq --arg date "$DATE_7D" '[.[] | select(.createdAt >= $date)] | length' /tmp/gh-aw/pr-data/prs.json)

# Calculate merge rate
# Scope: merged_prs / total_prs
if [ "$total_prs" -gt 0 ]; then
    merge_rate=$(echo "scale=3; $merged_prs / $total_prs" | bc)
else
    merge_rate="0"
fi

echo "Total PRs: $total_prs"
echo "Merged PRs: $merged_prs"
echo "Open PRs: $open_prs"
echo "Merge Rate: $merge_rate"
```

### 3. Code Metrics (bash + cloc)

```bash
# Lines of code metrics
# Uses cloc for accurate language detection
# See scratchpad/metrics-glossary.md for metric definitions

# Ensure output directory exists
mkdir -p /tmp/gh-aw/data

# Total LOC by language
# Scope: All source files in repository
cloc . --json --quiet > /tmp/gh-aw/data/cloc_output.json

# Check if cloc produced valid output
if [ -f /tmp/gh-aw/data/cloc_output.json ] && [ -s /tmp/gh-aw/data/cloc_output.json ]; then
    lines_of_code_total=$(jq '.SUM.code' /tmp/gh-aw/data/cloc_output.json)
    
    # Test LOC (find test files and measure)
    # Scope: Files matching test patterns (*_test.go, *.test.js, *.test.cjs, test_*.py, *_test.py)
    test_files=$(find . -name "*_test.go" -o -name "*.test.js" -o -name "*.test.cjs" -o -name "test_*.py" -o -name "*_test.py" 2>/dev/null)
    
    if [ -n "$test_files" ]; then
        echo "$test_files" | xargs cloc --json --quiet > /tmp/gh-aw/data/test_cloc.json
        test_lines_of_code=$(jq '.SUM.code' /tmp/gh-aw/data/test_cloc.json)
    else
        test_lines_of_code=0
    fi
    
    # Calculate source LOC (excluding tests)
    # Scope: All source files excluding test files
    # This is needed for the correct test-to-source ratio calculation
    source_lines_of_code=$((lines_of_code_total - test_lines_of_code))
    
    # Calculate test-to-source ratio
    # Scope: test_lines_of_code / source_lines_of_code (excludes test files from source LOC)
    # Per scratchpad/metrics-glossary.md: "Excludes test files from source LOC calculation"
    if [ "$source_lines_of_code" -gt 0 ]; then
        test_to_source_ratio=$(echo "scale=3; $test_lines_of_code / $source_lines_of_code" | bc)
    else
        test_to_source_ratio="0"
    fi
    
    echo "Total LOC: $lines_of_code_total"
    echo "Test LOC: $test_lines_of_code"
    echo "Source LOC (excluding tests): $source_lines_of_code"
    echo "Test-to-Source Ratio: $test_to_source_ratio"
else
    echo "Error: cloc did not produce valid output"
    exit 1
fi
```

### 4. Workflow Performance Metrics (Python)

```python
# Calculate workflow performance metrics from logs data
# Input: workflow_runs dict from agentic-workflows tool
# See scratchpad/metrics-glossary.md for metric definitions

import json

# Example structure:
# workflow_runs = {
#     'workflow-name': [
#         {'conclusion': 'success', 'duration': 120, ...},
#         {'conclusion': 'failure', 'duration': 45, ...},
#         ...
#     ]
# }

def calculate_workflow_metrics(workflow_runs):
    """
    Calculate standardized workflow metrics
    
    Args:
        workflow_runs: Dict mapping workflow name to list of run objects
    
    Returns:
        Dict with workflow metrics following scratchpad/metrics-glossary.md
    """
    workflow_metrics = {}
    
    for workflow_name, runs in workflow_runs.items():
        # Scope: All runs for this workflow
        total = len(runs)
        
        # Scope: Runs where conclusion = "success"
        successful = len([r for r in runs if r.get('conclusion') == 'success'])
        
        # Scope: Runs where conclusion != "success"
        failed = total - successful
        
        # Calculate success rate
        success_rate = round(successful / total, 3) if total > 0 else 0
        
        # Calculate average duration
        durations = [r.get('duration', 0) for r in runs if 'duration' in r]
        avg_duration = sum(durations) / len(durations) if durations else 0
        
        workflow_metrics[workflow_name] = {
            'total_runs': total,
            'successful_runs': successful,
            'failed_runs': failed,
            'success_rate': success_rate,
            'avg_duration_seconds': round(avg_duration, 2)
        }
    
    return workflow_metrics

# Example usage:
# metrics = calculate_workflow_metrics(workflow_runs)
# 
# Save to file for reporting:
# with open('/tmp/gh-aw/python/data/workflow_metrics.json', 'w') as f:
#     json.dump(metrics, f, indent=2)
```

### 5. Trend Calculation (Python)

```python
# Calculate trends from historical data
# See scratchpad/metrics-glossary.md for metric definitions

def calculate_trend(current_value, historical_values):
    """
    Calculate trend indicator (⬆️/➡️/⬇️) and percentage change
    
    This function compares the current value to the average of historical values
    and returns a trend indicator and percentage change.
    
    Args:
        current_value: Current metric value (numeric)
        historical_values: List of historical values (7 or 30 days)
    
    Returns:
        dict with:
        - 'indicator': Emoji arrow (⬆️/➡️/⬇️)
        - 'percentage_change': Percentage change from historical average
        - 'direction': Text direction ('up', 'stable', 'down')
    
    Example:
        >>> calculate_trend(100, [80, 85, 90])
        {'indicator': '⬆️', 'percentage_change': 17.6, 'direction': 'up'}
    """
    # Handle edge case: no historical data
    if not historical_values or len(historical_values) == 0:
        return {
            'indicator': '➡️',
            'percentage_change': 0,
            'direction': 'stable'
        }
    
    # Calculate average of historical values
    avg_historical = sum(historical_values) / len(historical_values)
    
    # Handle edge case: historical average is zero
    if avg_historical == 0:
        if current_value > 0:
            return {
                'indicator': '⬆️',
                'percentage_change': 100,
                'direction': 'up'
            }
        else:
            return {
                'indicator': '➡️',
                'percentage_change': 0,
                'direction': 'stable'
            }
    
    # Calculate percentage change
    pct_change = ((current_value - avg_historical) / avg_historical) * 100
    
    # Determine trend direction (threshold: ±5%)
    if pct_change > 5:
        return {
            'indicator': '⬆️',
            'percentage_change': round(pct_change, 1),
            'direction': 'up'
        }
    elif pct_change < -5:
        return {
            'indicator': '⬇️',
            'percentage_change': round(pct_change, 1),
            'direction': 'down'
        }
    else:
        return {
            'indicator': '➡️',
            'percentage_change': round(pct_change, 1),
            'direction': 'stable'
        }

# Example usage:
# trend = calculate_trend(current_value=100, historical_values=[80, 85, 90, 95])
# print(f"{trend['indicator']} {trend['percentage_change']}% ({trend['direction']})")
```

### 6. Cross-Platform Date Handling (bash)

```bash
# Cross-platform date calculations
# Works on both GNU (Linux) and BSD (macOS) systems

# Function to calculate date N days ago
get_date_n_days_ago() {
    local days=$1
    local format="${2:-%Y-%m-%dT%H:%M:%SZ}"
    
    if date --version >/dev/null 2>&1; then
        # GNU date (Linux)
        date -d "$days days ago" +"$format"
    else
        # BSD date (macOS)
        date -v-"${days}d" +"$format"
    fi
}

# Example usage:
# DATE_7D=$(get_date_n_days_ago 7)
# DATE_30D=$(get_date_n_days_ago 30)
# DATE_CUSTOM=$(get_date_n_days_ago 14 "%Y-%m-%d")

# Calculate dates for common time ranges
DATE_7D=$(get_date_n_days_ago 7)
DATE_14D=$(get_date_n_days_ago 14)
DATE_30D=$(get_date_n_days_ago 30)

echo "7 days ago: $DATE_7D"
echo "14 days ago: $DATE_14D"
echo "30 days ago: $DATE_30D"
```

## Best Practices

### 1. Always Reference Metrics Glossary

Include comment references to `scratchpad/metrics-glossary.md` above metric calculations:

```python
# See scratchpad/metrics-glossary.md for metric definitions

# Scope: All issues in repository, no filters
total_issues = len(df)
```

### 2. Document Scope Explicitly

Add scope comments for each metric calculation to clarify what data is included:

```python
# Scope: Open issues created in last 7 days
recent_open_issues = len(df[
    (df['state'] == 'OPEN') & 
    (df['createdAt'] > now - timedelta(days=7))
])
```

### 3. Use Standardized Names

Follow naming conventions from `scratchpad/metrics-glossary.md`:

✅ **GOOD**:
```python
issues_opened_7d = len(df[df['createdAt'] > now - timedelta(days=7)])
issues_without_labels = len(df[df['labels'].map(len) == 0])
```

❌ **BAD**:
```python
issues_7d = len(df[df['createdAt'] > now - timedelta(days=7)])  # Ambiguous
unlabeled = len(df[df['labels'].map(len) == 0])  # Non-standard
```

### 4. Handle Edge Cases

Always check for empty datasets, null values, and division by zero:

```python
# Safe division
if total_prs > 0:
    merge_rate = merged_prs / total_prs
else:
    merge_rate = 0

# Check for non-empty dataframe
if len(df) > 0:
    avg_value = df['column'].mean()
else:
    avg_value = 0

# Handle null values
closed_df = df[df['closedAt'].notna()]
```

### 5. Cross-Platform Compatibility

Use date commands that work on both GNU and BSD systems:

```bash
# Use the get_date_n_days_ago function from pattern #6
DATE_7D=$(get_date_n_days_ago 7)

# Or use inline conditional
if date --version >/dev/null 2>&1; then
    DATE_7D=$(date -d '7 days ago' '+%Y-%m-%dT%H:%M:%SZ')
else
    DATE_7D=$(date -v-7d '+%Y-%m-%dT%H:%M:%SZ')
fi
```

### 6. Validate Data Sources

Always verify that input files exist and contain valid data:

```python
import os

# Check file exists
if not os.path.exists('/tmp/gh-aw/issues-data/issues.json'):
    raise FileNotFoundError("Issues data file not found")

# Load and validate JSON
with open('/tmp/gh-aw/issues-data/issues.json', 'r') as f:
    issues = json.load(f)

if not issues or len(issues) == 0:
    print("Warning: No issues data available")
```

```bash
# Check file exists and is not empty
if [ ! -f /tmp/gh-aw/pr-data/prs.json ] || [ ! -s /tmp/gh-aw/pr-data/prs.json ]; then
    echo "Error: PR data file not found or empty"
    exit 1
fi
```

## Usage Examples

### Example 1: Issue Analysis Workflow

```yaml
imports:
  - shared/metrics-patterns.md
  - shared/issues-data-fetch.md
```

Then in your workflow, use the patterns:

```python
# Data is already loaded by issues-data-fetch
# Use the issue metrics patterns from this shared component

# Copy-paste pattern #1 (Issue Metrics) here
# All metrics will follow scratchpad/metrics-glossary.md standards
```

### Example 2: Workflow Performance Report

```yaml
imports:
  - shared/metrics-patterns.md
```

Then in your workflow:

```python
# Use pattern #4 (Workflow Performance Metrics)
# Calculate standardized workflow metrics
metrics = calculate_workflow_metrics(workflow_runs)

# Use pattern #5 (Trend Calculation) for historical comparison
trend = calculate_trend(
    current_value=metrics['daily-report']['success_rate'],
    historical_values=[0.85, 0.87, 0.90, 0.88]
)
```

## Impact

By using these standardized patterns, workflows benefit from:

- **Consistency**: All workflows calculate metrics the same way
- **Accuracy**: Reduced risk of calculation errors
- **Maintainability**: Fix bugs once, benefit everywhere
- **Comparability**: Metrics can be reliably compared across reports
- **Documentation**: Clear definitions and scope for all metrics

## Related Documentation

- **Metrics Definitions**: `scratchpad/metrics-glossary.md`
- **Python Visualization**: `shared/python-dataviz.md`
- **Trend Charts**: `shared/trends.md`
- **Issue Data Fetching**: `shared/issues-data-fetch.md`
