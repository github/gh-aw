---
# Python Data Visualization Setup
# Shared configuration for Python-based data visualization workflows
#
# Usage:
#   imports:
#     - shared/python-dataviz.md
#
# This import provides:
# - Python environment setup with directory structure
# - Scientific library installation (NumPy, Pandas, Matplotlib, Seaborn, SciPy)
# - Automatic artifact upload for charts and source files
# - Upload asset capability via safe-outputs (upload-asset)
# - Instructions on data visualization best practices including artifact uploads
#
# Note: This configuration ensures data separation by enforcing external data storage.

tools:
  cache-memory: true
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
    run: |
      # Create working directory for Python scripts
      mkdir -p /tmp/gh-aw/python
      mkdir -p /tmp/gh-aw/python/data
      mkdir -p /tmp/gh-aw/python/charts
      mkdir -p /tmp/gh-aw/python/artifacts
      
      echo "Python environment setup complete"
      echo "Working directory: /tmp/gh-aw/python"
      echo "Data directory: /tmp/gh-aw/python/data"
      echo "Charts directory: /tmp/gh-aw/python/charts"
      echo "Artifacts directory: /tmp/gh-aw/python/artifacts"

  - name: Install Python scientific libraries
    env:
      UV_PYTHON_INSTALL_DIR: /tmp/gh-aw/python/uv-python
    run: |
      # Create a virtual environment for proper package isolation (avoids --break-system-packages)
      # Use /tmp/gh-aw/python/venv to avoid polluting the agent artifact upload path (/tmp/gh-aw/agent/)
      # Use uv-managed CPython so the interpreter works inside the agent sandbox (runner CPython needs a newer GLIBC).
      # This must match shared/python-nlp.md so either import can create the shared environment first.
      if [ ! -d /tmp/gh-aw/python/venv ]; then
        uv venv --python 3.12 --python-preference only-managed --seed /tmp/gh-aw/python/venv
      fi
      echo "/tmp/gh-aw/python/venv/bin" >> "$GITHUB_PATH"
      /tmp/gh-aw/python/venv/bin/pip install --quiet numpy pandas matplotlib seaborn scipy
      
      # Verify installations
      /tmp/gh-aw/python/venv/bin/python3 -c "import numpy; print(f'NumPy {numpy.__version__} installed')"
      /tmp/gh-aw/python/venv/bin/python3 -c "import pandas; print(f'Pandas {pandas.__version__} installed')"
      /tmp/gh-aw/python/venv/bin/python3 -c "import matplotlib; print(f'Matplotlib {matplotlib.__version__} installed')"
      /tmp/gh-aw/python/venv/bin/python3 -c "import seaborn; print(f'Seaborn {seaborn.__version__} installed')"
      /tmp/gh-aw/python/venv/bin/python3 -c "import scipy; print(f'SciPy {scipy.__version__} installed')"
      
      echo "All scientific libraries installed successfully"

  - name: Upload source files and data
    if: always()
    uses: actions/upload-artifact@v7.0.2
    with:
      name: python-source-and-data
      path: |
        /tmp/gh-aw/python/*.py
        /tmp/gh-aw/python/data/*
      if-no-files-found: warn
      retention-days: 30
---

# Python Data Visualization

Dirs: `/tmp/gh-aw/python/` (scripts), `/tmp/gh-aw/python/data/` (inputs), `/tmp/gh-aw/python/charts/` (outputs).

**Data**: Never inline data. Write to `/tmp/gh-aw/python/data/` and load with `pd.read_csv()`/`pd.read_json()`.

**Charts**: `fig, ax = plt.subplots(figsize=(10,6), dpi=300)` + `sns.set_style("whitegrid")`, save PNG to `/tmp/gh-aw/python/charts/`, DPI ≥ 300, `bbox_inches='tight'`.

**Upload**:
1. `plt.savefig('/tmp/gh-aw/python/charts/my_chart.png', dpi=300, bbox_inches='tight')`
2. `{ "type": "upload_asset", "path": "/tmp/gh-aw/python/charts/my_chart.png" }`
3. `![Description](ASSET_URL_FROM_UPLOAD)` in report

Up to 5 uploads per run; PNG, JPG, JPEG, SVG allowed.
