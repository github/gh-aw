---
# Trending Charts (Simple) - Python environment with NumPy, Pandas, Matplotlib, Seaborn, SciPy
# Cache-memory integration for persistent trending data, automatic artifact upload

runtimes:
  uv: {}

tools:
  cache-memory:
    key: trending-data-${{ env.GH_AW_WORKFLOW_ID_SANITIZED }}
  bash:
    - "*"

network:
  allowed:
    - defaults
    - python

safe-outputs:
  upload-asset:
    max: 5
    allowed-exts: [.png, .jpg, .jpeg, .svg]

steps:
  - name: Setup Python environment
    env:
      UV_PYTHON_INSTALL_DIR: /tmp/gh-aw/python/uv-python
    run: |
      set -euo pipefail
      mkdir -p /tmp/gh-aw/python/{data,charts,artifacts}
      # The agent sandbox cannot run the runner's CPython (glibc mismatch) and only ships
      # PyPy, which has no wheels for the chart libraries — installing them from inside the
      # sandbox builds NumPy/SciPy from source and exhausts the runner disk. Install a
      # portable uv-managed CPython plus the chart libraries under /tmp/gh-aw, which is
      # mounted read-write into the sandbox, so the agent can import them directly.
      # This must match shared/python-dataviz.md and shared/python-nlp.md so either import can
      # create the shared environment first; never recreate it, or a sibling import's packages
      # (for example the NLP libraries) would be discarded.
      if [ ! -d /tmp/gh-aw/python/venv ]; then
        uv venv --python 3.12 --python-preference only-managed --seed /tmp/gh-aw/python/venv
      fi
      uv pip install --quiet --python /tmp/gh-aw/python/venv/bin/python numpy pandas matplotlib seaborn scipy
      echo "/tmp/gh-aw/python/venv/bin" >> "$GITHUB_PATH"
      /tmp/gh-aw/python/venv/bin/python -c "import numpy,pandas,matplotlib,seaborn,scipy;print('chart-libraries-ready')"

  - name: Upload source files and data
    if: always()
    uses: actions/upload-artifact@v7.0.2
    with:
      name: trending-source-and-data
      path: |
        /tmp/gh-aw/python/*.py
        /tmp/gh-aw/python/data/*
      if-no-files-found: warn
      retention-days: 30
---

# Python Environment Ready

Libraries: NumPy, Pandas, Matplotlib, Seaborn, SciPy
Directories: `/tmp/gh-aw/python/{data,charts,artifacts}`, `/tmp/gh-aw/cache-memory/`

**Always run chart scripts with `/tmp/gh-aw/python/venv/bin/python`** (for example
`/tmp/gh-aw/python/venv/bin/python script.py`). The sandbox `python3` on `PATH` is PyPy and
cannot import these libraries; do not try to `pip install` them from the agent shell.

## Store Historical Data (JSON Lines)

```python
import json
from datetime import datetime

# Append data point
with open('/tmp/gh-aw/cache-memory/trending/<metric>/history.jsonl', 'a') as f:
    f.write(json.dumps({"timestamp": datetime.now().isoformat(), "value": 42}) + '\n')
```

## Generate Charts

```python
import pandas as pd
import matplotlib.pyplot as plt
import seaborn as sns

df = pd.read_json('history.jsonl', lines=True)
df['date'] = pd.to_datetime(df['timestamp']).dt.date

sns.set_style("whitegrid")
fig, ax = plt.subplots(figsize=(12, 7), dpi=300)
df.groupby('date')['value'].mean().plot(ax=ax, marker='o')
ax.set_title('Trend', fontsize=16, fontweight='bold')
plt.xticks(rotation=45)
plt.tight_layout()
plt.savefig('/tmp/gh-aw/python/charts/trend.png', dpi=300, bbox_inches='tight')
```

## Upload Charts

Chart images are uploaded individually via the `upload_asset` safe-output tool. This returns a persistent asset URL for inline rendering in issues, discussions, and pull requests.

### Step 1: Generate Chart

```python
plt.savefig('/tmp/gh-aw/python/charts/trend.png', dpi=300, bbox_inches='tight')
```

### Step 2: Upload as Asset

Call the `upload_asset` tool for each chart image:

```json
{ "type": "upload_asset", "path": "/tmp/gh-aw/python/charts/trend.png" }
```

The tool returns a direct URL to the uploaded image.

### Step 3: Embed in Markdown

Use the returned asset URL to render the chart inline:

```markdown
![Trend Chart](ASSET_URL_FROM_UPLOAD)
```

> **Note**: Up to 5 chart images can be uploaded per run.

## Best Practices

- Use JSON Lines (`.jsonl`) for append-only storage
- Include ISO 8601 timestamps in all data points
- Implement 90-day retention: `df[df['timestamp'] >= cutoff_date]`
- Charts: 300 DPI, 12x7 inches, clear labels, seaborn style
