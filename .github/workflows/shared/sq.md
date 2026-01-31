---
steps:
  - name: Install sq from GitHub releases
    run: |
      # Install sq binary from official install script
      # This downloads the latest release from GitHub and installs it to /usr/local/bin
      /bin/sh -c "$(curl -fsSL https://sq.io/install.sh)"
      
  - name: Verify sq installation
    run: |
      sq version
      echo "sq is installed and ready to use"
---
<!--
## sq Data Wrangler

This shared configuration provides setup for `sq`, a command-line tool that offers jq-style access to structured data sources including SQL databases, CSV, Excel, and other document formats.

### About sq

`sq` is the lovechild of sql+jq. It executes jq-like queries or database-native SQL, can join across sources (e.g., join a CSV file to a Postgres table), and outputs to multiple formats including JSON, Excel, CSV, HTML, Markdown, and XML.

**Links:**
- Documentation: https://sq.io/
- GitHub Repository: https://github.com/neilotoole/sq
- Terminal Trove: https://terminaltrove.com/sq/
- Docker Image: https://github.com/neilotoole/sq/pkgs/container/sq

### Installation

The shared workflow installs the sq binary directly from GitHub releases using the official install script. This downloads the latest version and installs it to `/usr/local/bin`.

### Usage in Workflows

Import this shared configuration to make sq available in your workflow:

```yaml
imports:
  - shared/sq.md
```

Then use sq commands in your workflow steps:

```bash
# Inspect a data file
sq inspect database.db

# Query a CSV file with jq-like syntax
sq '.actor | .first_name, .last_name' actors.csv

# Join data from multiple sources
sq '@csv_data | join @postgres_db.users' data.csv
```

### Common Use Cases

1. **Query structured data files**: Use jq-like syntax to query CSV, Excel, JSON files
2. **Cross-source joins**: Combine data from different sources (databases, files)
3. **Data format conversion**: Convert between formats (CSV to JSON, Excel to Markdown, etc.)
4. **Database inspection**: View metadata about database structure
5. **Database operations**: Copy, truncate, or drop tables
6. **Data comparison**: Use `sq diff` to compare tables or databases

### Example Workflow

```yaml
---
on:
  workflow_dispatch:
imports:
  - shared/sq.md
permissions:
  contents: read
safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[data-analysis] "
---

# Data Analysis with sq

Analyze the CSV files in the repository using sq and create a summary report.

Use sq to:
1. Inspect the data structure
2. Query for interesting patterns
3. Generate summary statistics
4. Create a formatted report

Available sq commands:
- `sq inspect file.csv`
- `sq '.table | select(.column > 100)' file.csv`
- `sq --json '.table' file.csv`
```

### Tips

- sq is installed directly and available in PATH
- Use relative paths from the workspace root
- Specify output format with flags like `--json`, `--csv`, `--markdown`
- For databases, use connection strings or add sources with `sq add`
- All operations work directly on files in the workspace
-->

You have access to the `sq` data wrangling tool for working with structured data sources.

**sq capabilities:**
- Query CSV, Excel, JSON, and database files using jq-like syntax
- Join data across different source types
- Convert between data formats
- Inspect database structures and metadata
- Perform database operations (copy, truncate, drop tables)
- Compare data with `sq diff`

**Using sq in this workflow:**
The sq binary is installed and available in PATH. Use it directly:
```bash
sq [command] [arguments]
```

**Example commands:**
```bash
# Inspect a data file
sq inspect file.csv

# Query data with jq-like syntax
sq '.table | .column' file.csv

# Output as JSON
sq --json '.table' file.csv

# Filter and aggregate
sq '.table | where(.value > 100) | count' file.csv

# Convert to different format
sq --markdown '.table' file.csv
```

For more information, see: https://sq.io/docs/
