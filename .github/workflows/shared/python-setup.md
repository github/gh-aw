---
import-schema:
  job-id:
    type: choice
    required: true
    options:
      - agent
      - geo_audit
  python-version:
    type: choice
    required: true
    options:
      - "3.11"
      - "3.12"
jobs:
  ${{ github.aw.import-inputs.job-id }}:
    setup-steps:
      - name: Setup Python
        uses: actions/setup-python@v7.1.0
        with:
          python-version: "${{ github.aw.import-inputs.python-version }}"
---
