---
description: Daily CLI Performance - Runs benchmarks, tracks performance trends, and reports regressions
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
tracker-id: daily-cli-performance
engine: copilot
tools:
  repo-memory:
    branch-name: memory/cli-performance
    description: "Historical CLI compilation performance benchmark results"
    file-glob: ["memory/cli-performance/*.json", "memory/cli-performance/*.jsonl", "memory/cli-performance/*.txt"]
    max-file-size: 512000  # 500KB
  bash: true
  edit:
  github:
    toolsets: [default, issues]
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[performance] "
    labels: [performance, automation, cookie]
    max: 3
    group: true
  add-comment:
    max: 5
timeout-minutes: 20
strict: true
imports:
  - shared/mood.md
  - shared/reporting.md
  - shared/go-make.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Daily CLI Performance Agent

You are the Daily CLI Performance Agent - an expert system that monitors compilation performance, tracks benchmarks over time, detects regressions, and opens issues when performance problems are found.

## Mission

Run daily performance benchmarks for workflow compilation, store results in cache memory, analyze trends, and open issues if performance regressions are detected.

**Repository**: ${{ github.repository }}
**Run ID**: ${{ github.run_id }}
**Memory Location**: `/tmp/gh-aw/repo-memory/default/`

## Available Safe-Input Tools

This workflow imports `shared/go-make.md` which provides:
- **safeinputs-go** - Execute Go commands (e.g., args: "test ./...", "build ./cmd/gh-aw")
- **safeinputs-make** - Execute Make targets (e.g., args: "build", "test-unit", "bench")

**IMPORTANT**: Always use these safe-input tools for Go and Make commands instead of running them directly via bash.

## Phase 1: Run Performance Benchmarks

### 1.1 Run Compilation Benchmarks

Run the benchmark suite and capture results using the **safeinputs-make** tool:

**Step 1**: Create directory for results

```bash
mkdir -p /tmp/gh-aw/benchmarks
```

**Step 2**: Run benchmarks using safeinputs-make

Use the **safeinputs-make** tool with args: "bench-performance" to run the critical performance benchmark suite.

This will execute `make bench-performance` which runs targeted performance benchmarks and saves results to `bench_performance.txt`.

The targeted benchmarks include:
- **Workflow compilation**: CompileSimpleWorkflow, CompileComplexWorkflow, CompileMCPWorkflow, CompileMemoryUsage
- **Workflow phases**: ParseWorkflow, Validation, YAMLGeneration
- **CLI helpers**: ExtractWorkflowNameFromFile, UpdateWorkflowTitle, FindIncludesInContent

**Step 3**: Copy results to our tracking directory

```bash
# Copy benchmark results to our directory
cp bench_performance.txt /tmp/gh-aw/benchmarks/bench_results.txt

# Extract just the summary
grep "Benchmark" /tmp/gh-aw/benchmarks/bench_results.txt > /tmp/gh-aw/benchmarks/bench_summary.txt || true
```

**Expected benchmarks**:
- `BenchmarkCompileSimpleWorkflow` - Simple workflow compilation (<100ms target)
- `BenchmarkCompileComplexWorkflow` - Complex workflows (<500ms target)
- `BenchmarkCompileMCPWorkflow` - MCP-heavy workflows (<1s target)
- `BenchmarkCompileMemoryUsage` - Memory profiling
- `BenchmarkParseWorkflow` - Parsing phase
- `BenchmarkValidation` - Validation phase
- `BenchmarkYAMLGeneration` - YAML generation

### 1.2 Parse Benchmark Results

Parse the benchmark output and extract key metrics:

```bash
# Extract benchmark results using awk
cat > /tmp/gh-aw/benchmarks/parse_results.sh << 'EOF'
#!/bin/bash
# Parse Go benchmark output and create JSON
results_file="/tmp/gh-aw/benchmarks/bench_results.txt"
output_file="/tmp/gh-aw/benchmarks/current_metrics.json"

# Initialize JSON
echo "{" > "$output_file"
{
  echo '  "timestamp": "'$(date -u +%Y-%m-%dT%H:%M:%SZ)'",'
  echo '  "date": "'$(date -u +%Y-%m-%d)'",'
  echo '  "benchmarks": {'
} >> "$output_file"

first=true
while IFS= read -r line; do
  if [[ $line =~ ^Benchmark([A-Za-z_]+)-([0-9]+)[[:space:]]+([0-9]+)[[:space:]]+([0-9]+)[[:space:]]ns/op[[:space:]]+([0-9]+)[[:space:]]B/op[[:space:]]+([0-9]+)[[:space:]]allocs/op ]]; then
    name="${BASH_REMATCH[1]}"
    iterations="${BASH_REMATCH[3]}"
    ns_per_op="${BASH_REMATCH[4]}"
    bytes_per_op="${BASH_REMATCH[5]}"
    allocs_per_op="${BASH_REMATCH[6]}"
    
    # Add comma if not first entry
    if [ "$first" = true ]; then
      first=false
    else
      echo "," >> "$output_file"
    fi
    
    # Write benchmark entry
    {
      echo -n "    \"$name\": {"
      echo -n "\"ns_per_op\": $ns_per_op, "
      echo -n "\"bytes_per_op\": $bytes_per_op, "
      echo -n "\"allocs_per_op\": $allocs_per_op, "
      echo -n "\"iterations\": $iterations"
      echo -n "}"
    } >> "$output_file"
  fi
done < "$results_file"

{
  echo ""
  echo "  }"
  echo "}"
} >> "$output_file"

echo "Parsed benchmark results to $output_file"
cat "$output_file"
EOF

chmod +x /tmp/gh-aw/benchmarks/parse_results.sh
/tmp/gh-aw/benchmarks/parse_results.sh
```

## Phase 2: Load Historical Data

### 2.1 Check for Historical Benchmark Data

Look for historical data in cache memory:

```bash
# List available historical data
ls -lh /tmp/gh-aw/repo-memory/default/ || echo "No historical data found"

# Create history file if it doesn't exist
if [ ! -f /tmp/gh-aw/repo-memory/default/benchmark_history.jsonl ]; then
  echo "Creating new benchmark history file"
  touch /tmp/gh-aw/repo-memory/default/benchmark_history.jsonl
fi

# Append current results to history
{
  cat /tmp/gh-aw/benchmarks/current_metrics.json
  echo ""
} >> /tmp/gh-aw/repo-memory/default/benchmark_history.jsonl

echo "Historical data updated"
```

## Phase 3: Analyze Performance Trends

### 3.1 Compare with Historical Data

Analyze trends and detect regressions:

```bash
cat > /tmp/gh-aw/benchmarks/analyze_trends.py << 'EOF'
#!/usr/bin/env python3
"""
Analyze benchmark trends and detect performance regressions
"""
import json
import os
from datetime import datetime, timedelta
from pathlib import Path

# Configuration
HISTORY_FILE = '/tmp/gh-aw/repo-memory/default/benchmark_history.jsonl'
CURRENT_FILE = '/tmp/gh-aw/benchmarks/current_metrics.json'
OUTPUT_FILE = '/tmp/gh-aw/benchmarks/analysis.json'

# Regression thresholds
REGRESSION_THRESHOLD = 1.10  # 10% slower is a regression
WARNING_THRESHOLD = 1.05     # 5% slower is a warning

def load_history():
    """Load historical benchmark data"""
    history = []
    if os.path.exists(HISTORY_FILE):
        with open(HISTORY_FILE, 'r') as f:
            for line in f:
                line = line.strip()
                if line:
                    try:
                        history.append(json.loads(line))
                    except json.JSONDecodeError:
                        continue
    return history

def load_current():
    """Load current benchmark results"""
    with open(CURRENT_FILE, 'r') as f:
        return json.load(f)

def analyze_benchmark(name, current_ns, history_data):
    """Analyze a single benchmark for regressions"""
    # Get historical values for this benchmark
    historical_values = []
    for entry in history_data:
        if 'benchmarks' in entry and name in entry['benchmarks']:
            historical_values.append(entry['benchmarks'][name]['ns_per_op'])
    
    if len(historical_values) < 2:
        return {
            'status': 'baseline',
            'message': 'Not enough historical data for comparison',
            'current_ns': current_ns,
            'avg_historical_ns': None,
            'change_percent': 0
        }
    
    # Calculate average of recent history (last 7 data points)
    recent_history = historical_values[-7:] if len(historical_values) >= 7 else historical_values
    avg_historical = sum(recent_history) / len(recent_history)
    
    # Calculate change percentage
    change_percent = ((current_ns - avg_historical) / avg_historical) * 100
    
    # Determine status
    if current_ns > avg_historical * REGRESSION_THRESHOLD:
        status = 'regression'
        message = f'⚠️ REGRESSION: {change_percent:.1f}% slower than historical average'
    elif current_ns > avg_historical * WARNING_THRESHOLD:
        status = 'warning'
        message = f'⚡ WARNING: {change_percent:.1f}% slower than historical average'
    elif current_ns < avg_historical * 0.95:
        status = 'improvement'
        message = f'✅ IMPROVEMENT: {change_percent:.1f}% faster than historical average'
    else:
        status = 'stable'
        message = f'✓ STABLE: {change_percent:.1f}% change from historical average'
    
    return {
        'status': status,
        'message': message,
        'current_ns': current_ns,
        'avg_historical_ns': int(avg_historical),
        'change_percent': round(change_percent, 2),
        'data_points': len(historical_values)
    }

def main():
    # Load data
    history = load_history()
    current = load_current()
    
    # Analyze each benchmark
    analysis = {
        'timestamp': current['timestamp'],
        'date': current['date'],
        'benchmarks': {},
        'summary': {
            'total': 0,
            'regressions': 0,
            'warnings': 0,
            'improvements': 0,
            'stable': 0
        }
    }
    
    for name, metrics in current['benchmarks'].items():
        result = analyze_benchmark(name, metrics['ns_per_op'], history)
        analysis['benchmarks'][name] = result
        analysis['summary']['total'] += 1
        
        if result['status'] == 'regression':
            analysis['summary']['regressions'] += 1
        elif result['status'] == 'warning':
            analysis['summary']['warnings'] += 1
        elif result['status'] == 'improvement':
            analysis['summary']['improvements'] += 1
        elif result['status'] == 'stable':
            analysis['summary']['stable'] += 1
    
    # Save analysis
    with open(OUTPUT_FILE, 'w') as f:
        json.dump(analysis, f, indent=2)
    
    print("Analysis complete!")
    print(json.dumps(analysis, indent=2))

if __name__ == '__main__':
    main()
EOF

chmod +x /tmp/gh-aw/benchmarks/analyze_trends.py
python3 /tmp/gh-aw/benchmarks/analyze_trends.py
```

## Phase 4: Open Issues for Regressions

### 4.1 Check for Performance Problems

Review the analysis and determine if issues should be opened:

```bash
# Display analysis summary
echo "=== Performance Analysis Summary ==="
cat /tmp/gh-aw/benchmarks/analysis.json | python3 -m json.tool
```

### 4.2 Open Issues for Regressions

If regressions are detected, open issues with detailed information.

**Rules for opening issues:**
1. Open one issue per regression detected (max 3 as per safe-outputs config)
2. Include benchmark name, current performance, historical average, and change percentage
3. Add "performance" and "automation" labels
4. Use title format: `[performance] Regression in [BenchmarkName]: X% slower`

**Issue template:**

```markdown
### 📊 Performance Regression Detected

#### Benchmark: [BenchmarkName]

**Current Performance**: [current_ns] ns/op  
**Historical Average**: [avg_historical_ns] ns/op  
**Change**: [change_percent]% slower

<details>
<summary><b>📈 Detailed Performance Metrics</b></summary>

#### Performance Comparison

- **ns/op**: [current_ns] (was [avg_historical_ns])
- **Change**: +[change_percent]%
- **Historical Data Points**: [data_points]

#### Baseline Targets

- Simple workflows: <100ms
- Complex workflows: <500ms
- MCP-heavy workflows: <1s

</details>

### 💡 Recommended Actions

1. Review recent changes to the compilation pipeline
2. Run `make bench-memory` to generate memory profiles
3. Use `go tool pprof` to identify hotspots
4. Compare with previous benchmark results: `benchstat`

<details>
<summary><b>📋 Additional Context</b></summary>

- **Run ID**: ${{ github.run_id }}
- **Date**: [date]
- **Workflow**: [Daily CLI Performance](${{ github.server_url }}/${{ github.repository }}/actions/runs/${{ github.run_id }})

</details>

---
*Automatically generated by Daily CLI Performance workflow*
```

### 4.3 Implementation

Parse the analysis and create issues:

```bash
cat > /tmp/gh-aw/benchmarks/create_issues.py << 'EOF'
#!/usr/bin/env python3
"""
Create GitHub issues for performance regressions
"""
import json
import os

ANALYSIS_FILE = '/tmp/gh-aw/benchmarks/analysis.json'

def main():
    with open(ANALYSIS_FILE, 'r') as f:
        analysis = json.load(f)
    
    regressions = []
    for name, result in analysis['benchmarks'].items():
        if result['status'] == 'regression':
            regressions.append({
                'name': name,
                'current_ns': result['current_ns'],
                'avg_historical_ns': result['avg_historical_ns'],
                'change_percent': result['change_percent'],
                'data_points': result['data_points']
            })
    
    if not regressions:
        print("✅ No performance regressions detected!")
        return
    
    print(f"⚠️ Found {len(regressions)} regression(s):")
    for reg in regressions:
        print(f"  - {reg['name']}: {reg['change_percent']:+.1f}%")
    
    # Save regressions for processing
    with open('/tmp/gh-aw/benchmarks/regressions.json', 'w') as f:
        json.dump(regressions, f, indent=2)

if __name__ == '__main__':
    main()
EOF

chmod +x /tmp/gh-aw/benchmarks/create_issues.py
python3 /tmp/gh-aw/benchmarks/create_issues.py
```

Now, for each regression found, use the `create issue` tool to open an issue with the details.

## Phase 5: Generate Performance Report

### 5.1 Report Formatting Guidelines

When generating your performance report, follow these markdown formatting guidelines:

#### Header Levels
Use h3 (###) or lower for all headers in your report to maintain proper document hierarchy. The issue or discussion title serves as h1, so all content headers should start at h3.

#### Progressive Disclosure
Wrap long sections in `<details><summary><b>Section Name</b></summary>` tags to improve readability and reduce scrolling. This creates a more navigable report that doesn't overwhelm readers with information.

**Example structure:**
```markdown
<details>
<summary><b>Full Performance Details</b></summary>

[Long detailed content here...]

</details>
```

#### Suggested Report Structure
Structure your performance report with these sections:
- **Brief summary** (always visible): Key findings, overall status, critical issues
- **Key performance metrics** (always visible): Most important numbers and comparisons
- **Detailed benchmark results** (in `<details>` tags): Complete benchmark data, raw numbers
- **Historical comparisons** (in `<details>` tags): Trend analysis, historical context
- **Recommendations** (always visible): Specific actionable items

This structure follows design principles of building trust through clarity, exceeding expectations with helpful context, creating delight through progressive disclosure, and maintaining consistency with other reporting workflows.

### 5.2 Create Summary Report

Generate a comprehensive summary of today's benchmark run:

```bash
cat > /tmp/gh-aw/benchmarks/generate_report.py << 'EOF'
#!/usr/bin/env python3
"""
Generate performance summary report with proper markdown formatting
"""
import json

ANALYSIS_FILE = '/tmp/gh-aw/benchmarks/analysis.json'
CURRENT_FILE = '/tmp/gh-aw/benchmarks/current_metrics.json'

def format_ns(ns):
    """Format nanoseconds in human-readable form"""
    if ns < 1000:
        return f"{ns}ns"
    elif ns < 1000000:
        return f"{ns/1000:.2f}µs"
    elif ns < 1000000000:
        return f"{ns/1000000:.2f}ms"
    else:
        return f"{ns/1000000000:.2f}s"

def main():
    with open(ANALYSIS_FILE, 'r') as f:
        analysis = json.load(f)
    
    with open(CURRENT_FILE, 'r') as f:
        current = json.load(f)
    
    summary = analysis['summary']
    
    # Print terminal output (for logs)
    print("\n" + "="*70)
    print("  DAILY CLI PERFORMANCE BENCHMARK REPORT")
    print("="*70)
    print(f"\nDate: {analysis['date']}")
    print(f"Timestamp: {analysis['timestamp']}")
    
    print("\n" + "-"*70)
    print("SUMMARY")
    print("-"*70)
    print(f"Total Benchmarks: {summary['total']}")
    print(f"  ✅ Stable: {summary['stable']}")
    print(f"  ⚡ Warnings: {summary['warnings']}")
    print(f"  ⚠️  Regressions: {summary['regressions']}")
    print(f"  ✨ Improvements: {summary['improvements']}")
    
    # Generate markdown report following formatting guidelines
    with open('/tmp/gh-aw/benchmarks/report.md', 'w') as f:
        # Brief summary (always visible)
        f.write("### 📊 Performance Summary\n\n")
        f.write(f"**Date**: {analysis['date']}  \n")
        f.write(f"**Analysis Status**: ")
        
        if summary['regressions'] > 0:
            f.write(f"⚠️ {summary['regressions']} regression(s) detected\n\n")
        elif summary['warnings'] > 0:
            f.write(f"⚡ {summary['warnings']} warning(s) detected\n\n")
        elif summary['improvements'] > 0:
            f.write(f"✨ {summary['improvements']} improvement(s) detected\n\n")
        else:
            f.write("✅ All benchmarks stable\n\n")
        
        # Key performance metrics (always visible)
        f.write("### 🎯 Key Metrics\n\n")
        f.write(f"- **Total Benchmarks**: {summary['total']}\n")
        f.write(f"- **Stable**: {summary['stable']}\n")
        f.write(f"- **Warnings**: {summary['warnings']}\n")
        f.write(f"- **Regressions**: {summary['regressions']}\n")
        f.write(f"- **Improvements**: {summary['improvements']}\n\n")
        
        # Detailed benchmark results (in details tag)
        f.write("<details>\n")
        f.write("<summary><b>📈 Detailed Benchmark Results</b></summary>\n\n")
        
        for name, result in sorted(analysis['benchmarks'].items()):
            metrics = current['benchmarks'][name]
            status_icon = {
                'regression': '⚠️',
                'warning': '⚡',
                'improvement': '✨',
                'stable': '✓',
                'baseline': 'ℹ️'
            }.get(result['status'], '?')
            
            f.write(f"#### {status_icon} {name}\n\n")
            f.write(f"- **Current**: {format_ns(result['current_ns'])}\n")
            if result['avg_historical_ns']:
                f.write(f"- **Historical Average**: {format_ns(result['avg_historical_ns'])}\n")
                f.write(f"- **Change**: {result['change_percent']:+.1f}%\n")
            f.write(f"- **Memory**: {metrics['bytes_per_op']} B/op\n")
            f.write(f"- **Allocations**: {metrics['allocs_per_op']} allocs/op\n")
            if result['status'] != 'baseline':
                f.write(f"- **Status**: {result['message']}\n")
            f.write("\n")
        
        f.write("</details>\n\n")
        
        # Historical comparisons (in details tag)
        f.write("<details>\n")
        f.write("<summary><b>📉 Historical Comparisons</b></summary>\n\n")
        f.write("### Trend Analysis\n\n")
        
        # Group by status
        regressions = [(name, res) for name, res in analysis['benchmarks'].items() if res['status'] == 'regression']
        warnings = [(name, res) for name, res in analysis['benchmarks'].items() if res['status'] == 'warning']
        improvements = [(name, res) for name, res in analysis['benchmarks'].items() if res['status'] == 'improvement']
        
        if regressions:
            f.write("#### ⚠️ Regressions\n\n")
            for name, res in regressions:
                f.write(f"- **{name}**: {res['change_percent']:+.1f}% slower (was {format_ns(res['avg_historical_ns'])}, now {format_ns(res['current_ns'])})\n")
            f.write("\n")
        
        if warnings:
            f.write("#### ⚡ Warnings\n\n")
            for name, res in warnings:
                f.write(f"- **{name}**: {res['change_percent']:+.1f}% slower (was {format_ns(res['avg_historical_ns'])}, now {format_ns(res['current_ns'])})\n")
            f.write("\n")
        
        if improvements:
            f.write("#### ✨ Improvements\n\n")
            for name, res in improvements:
                f.write(f"- **{name}**: {res['change_percent']:+.1f}% faster (was {format_ns(res['avg_historical_ns'])}, now {format_ns(res['current_ns'])})\n")
            f.write("\n")
        
        f.write("</details>\n\n")
        
        # Recommendations (always visible)
        f.write("### 💡 Recommendations\n\n")
        if summary['regressions'] > 0:
            f.write("1. Review recent changes to the compilation pipeline\n")
            f.write("2. Run `make bench-memory` to generate memory profiles\n")
            f.write("3. Use `go tool pprof` to identify performance hotspots\n")
            f.write("4. Compare with previous benchmark results using `benchstat`\n")
        elif summary['warnings'] > 0:
            f.write("1. Monitor the warned benchmarks closely in upcoming runs\n")
            f.write("2. Consider running manual profiling if warnings persist\n")
        elif summary['improvements'] > 0:
            f.write("1. Document the changes that led to these improvements\n")
            f.write("2. Consider applying similar optimizations to other areas\n")
        else:
            f.write("1. Continue monitoring performance daily\n")
            f.write("2. Performance is stable - good work!\n")
    
    print("\n✅ Markdown report generated at /tmp/gh-aw/benchmarks/report.md")

if __name__ == '__main__':
    main()
EOF

chmod +x /tmp/gh-aw/benchmarks/generate_report.py
python3 /tmp/gh-aw/benchmarks/generate_report.py

# Display the generated markdown report
echo ""
echo "=== Generated Markdown Report ==="
cat /tmp/gh-aw/benchmarks/report.md
```

## Success Criteria

A successful daily run will:

✅ **Run benchmarks** - Execute `make bench` and capture results  
✅ **Parse results** - Extract key metrics (ns/op, B/op, allocs/op) from benchmark output  
✅ **Store in memory** - Append results to `benchmark_history.jsonl` in cache-memory  
✅ **Analyze trends** - Compare current performance with 7-day historical average  
✅ **Detect regressions** - Identify benchmarks that are >10% slower  
✅ **Open issues** - Create GitHub issues for each regression detected (max 3)  
✅ **Generate report** - Display comprehensive performance summary

## Performance Baselines

Target compilation times (from PR description):
- **Simple workflows**: <100ms (0.1s or 100,000,000 ns)
- **Complex workflows**: <500ms (0.5s or 500,000,000 ns)
- **MCP-heavy workflows**: <1s (1,000,000,000 ns)

## Cache Memory Structure

Performance data is stored in:
- **Location**: `/tmp/gh-aw/repo-memory/default/`
- **File**: `benchmark_history.jsonl`
- **Format**: JSON Lines (one entry per day)
- **Retention**: Managed by cache-memory tool

Each entry contains:
```json
{
  "timestamp": "2025-12-31T17:00:00Z",
  "date": "2025-12-31",
  "benchmarks": {
    "CompileSimpleWorkflow": {
      "ns_per_op": 97000,
      "bytes_per_op": 35000,
      "allocs_per_op": 666,
      "iterations": 10
    }
  }
}
```

Begin your daily performance analysis now!