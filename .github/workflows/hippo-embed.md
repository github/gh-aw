---
private: true
emoji: "🦛"
name: Hippo Embed
description: Maintenance workflow to audit low-quality entries and embed all Hippo memories to restore semantic recall quality
on:
  workflow_dispatch:

permissions:
  contents: read

  copilot-requests: write
tracker-id: hippo-embed
model: openai/gpt-5.4
engine:
  id: pi
  model-provider: openai

timeout-minutes: 60

runs-on: aw-gpu-runner-T4

runtimes:
  node:
    version: "22"

network:
  allowed:
    - defaults
    - node

sandbox:
  agent:
    id: awf

tools:
  cli-proxy: true
  github:
    mode: gh-proxy
  bash:
    - "*"

steps:
  - name: Install @xenova/transformers
    run: |
      npm install -g @xenova/transformers

imports:
  - shared/pmg.md
  - shared/hippo-memory.md

  - shared/otlp.md
---

{{#runtime-import? .github/shared-instructions.md}}

# Hippo Memory — Audit and Embed

You are an AI agent running a maintenance pass to restore semantic recall quality in
the Hippo memory store. The store has grown to ~490 memories but fewer than 1% have
been embedded, severely degrading semantic search. Complete the steps below in order.

## Context

- **Repository**: ${{ github.repository }}
- **Memory store**: `.hippo/` (persisted in cache-memory across runs)

## Step 1 — Audit and interactively prune low-quality entries

Run the audit command and review each flagged low-quality memory in the guided
interface. Approve pruning for stale or low-signal entries (too-short fragments,
commit noise, vague notes) before embedding so they do not pollute the vector index:

```
mcpscripts-hippo args: "audit"
```

This maintenance pass is expected to include 7 low-quality memories flagged for
review. Note how many entries are pruned for your summary.

## Step 2 — Embed all memories

Generate vector embeddings for every memory in the store. This enables hybrid
BM25 + cosine similarity search and significantly improves semantic recall quality:

```
mcpscripts-hippo args: "embed"
```

This may take several minutes for a store of ~490 memories. Wait for completion.

## Step 3 — Verify and report

Check the store status to confirm embeddings were generated:

```
mcpscripts-hippo args: "status"
```

Then print a short summary to stdout (using the bash echo tool) covering:
- Memories pruned by audit
- Memories embedded (before vs. after)
- Whether semantic recall is now operational