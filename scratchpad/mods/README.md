# Go Module Usage Summaries

This directory contains AI-generated summaries of Go module usage patterns in the gh-aw repository, created by the [Go Fan workflow](/.github/workflows/go-fan.md).

## File Naming Convention

Each module summary file is named after the Go module path with slashes replaced by dashes:
- `github.com/goccy/go-yaml` → `goccy-go-yaml.md`
- `github.com/spf13/cobra` → `spf13-cobra.md`

## Update Frequency

The Go Fan workflow runs daily on weekdays (Monday-Friday) at 7 AM UTC. Each run analyzes one module in round-robin order using cache-memory to track progress.

## Summary Contents

Each summary includes:
- Module overview and version used
- Files and APIs using the module
- Research findings from the module's GitHub repository
- Improvement opportunities (quick wins, feature opportunities, best practices)
- References to documentation and changelog
