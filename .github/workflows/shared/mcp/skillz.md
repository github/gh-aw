---
# Skillz MCP Server
# Docker-based MCP server that loads Claude-style skills from the repository
#
# Documentation: https://github.com/intellectronica/skillz
#
# Skills Directory:
#   The server looks for skills in `.github/skills/` directory of the repository.
#   Each skill is a folder or zip archive containing a SKILL.md file.
#
#   To set up skills, create the directory structure:
#     mkdir -p .github/skills/my-skill
#     touch .github/skills/my-skill/SKILL.md
#
# Example skills directory structure:
#   .github/skills/
#   ├── summarize-docs/
#   │   ├── SKILL.md
#   │   └── summarize.py
#   └── translate/
#       ├── SKILL.md
#       └── helpers/
#           └── translate.js
#
# SKILL.md format:
#   ---
#   name: my-skill
#   description: What this skill does
#   ---
#   
#   # My Skill
#   
#   Instructions for the agent on how to use this skill.
#
# Available tools:
#   Tools are dynamically exposed based on discovered skills.
#   Each skill defined in SKILL.md becomes a callable tool.
#
# Security:
#   Skills are mounted read-only from the repository.
#   All exposed tools are allowed since skills are committed code
#   (same trust level as any other repository code).
#
# Usage:
#   imports:
#     - shared/mcp/skillz.md

mcp-servers:
  skillz:
    container: "intellectronica/skillz"
    version: "latest@sha256:4b3156dd57057abdc68b05401fefe4373a1e3015b43e0db72ebe33273e7e4052"
    args:
      - "-v"
      - "${{ github.workspace }}/.github/skills:/skillz:ro"
      - "/skillz"
    env:
      GH_TOKEN: "${{ github.token }}"
      GITHUB_TOKEN: "${{ github.token }}"
    # Security decision (2026-05-19): keep wildcard because tools are dynamic per repository skill set.
    # Restricting to static names would break legitimate skill tool discovery.
    allowed: ["*"]
---
