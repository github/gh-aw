---
mcp-servers:
  kreuzberg:
    container: "ghcr.io/xberg-io/xberg"
    version: "latest"
    entrypointArgs:
      - "mcp"
    mounts:
      - ${GITHUB_WORKSPACE}:${GITHUB_WORKSPACE}:ro
    allowed:
      # Document extraction tools (read-only)
      - "extract_file"
      - "extract_bytes"
      - "batch_extract_files"
      # Format discovery tools (read-only)
      - "detect_mime_type"
      - "list_formats"
      - "get_version"
      # Text processing tools (read-only)
      - "embed_text"
      - "chunk_text"
      # Cache inspection tools (read-only)
      - "cache_stats"
      - "cache_manifest"
      # Excluded write/mutating operations:
      # - "cache_clear"   # Evicts all cached results
      # - "cache_warm"    # Pre-downloads embedding models
      # Excluded feature-flag-gated operations:
      # - "extract_structured"  # Requires liter-llm feature flag at build time
# Kreuzberg MCP Server
#
# Kreuzberg is a polyglot document intelligence engine. It supports 97+ file
# formats including PDF, DOCX, PPTX, images (with Tesseract OCR), and legacy
# Office formats (with LibreOffice in the full image).
#
# Documentation: https://docs.kreuzberg.dev/guides/docker/
# MCP integration guide: https://docs.kreuzberg.dev/guides/mcp-integration/
# GitHub: https://github.com/kreuzberg-dev/kreuzberg
#
# Container images:
# - Core (~1.0–1.3 GB): Modern formats and Tesseract OCR (12 languages)
# - Full (~1.5–2.1 GB): Adds LibreOffice for legacy `.doc`/`.ppt` files.
#   Use tag `full` or `latest-full` to select the full image.
#
# No API token is required. The workspace is mounted read-only at the same
# host path, so extract_file and batch_extract_files can use absolute paths
# such as `${{ github.workspace }}/document.pdf`.
#
# Usage:
# imports:
#   - shared/mcp/kreuzberg.md
---
Use `extract_file` to extract text from workspace documents.
