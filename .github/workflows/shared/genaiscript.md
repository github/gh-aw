---
engine:
  id: custom
  env:
    GH_AW_AGENT_VERSION: "2.5.1"
    GH_AW_AGENT_MODEL_VERSION: "openai:gpt-4o"
  steps:
    - name: Validate OPENAI_API_KEY secret
      run: |
        if [ -z "$OPENAI_API_KEY" ]; then
          echo "Error: OPENAI_API_KEY secret is not set"
          echo "The GenAIScript engine with openai:gpt-4o model requires OPENAI_API_KEY secret to be configured."
          echo "Please configure this secret in your repository settings."
          echo "Documentation: https://github.github.com/gh-aw/reference/engines/"
          exit 1
        fi
        echo "OPENAI_API_KEY secret is configured"
      env:
        OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
    
    - name: Install GenAIScript
      run: npm install -g genaiscript@${GH_AW_AGENT_VERSION} && genaiscript --version
      env:
        GH_AW_AGENT_VERSION: ${{ env.GH_AW_AGENT_VERSION }}
    
    - name: Convert prompt to GenAI format
      run: |
        mkdir -p /tmp/gh-aw/aw-prompts
        echo "---" > /tmp/gh-aw/aw-prompts/prompt.genai.md
        echo "model: ${GH_AW_AGENT_MODEL_VERSION}" >> /tmp/gh-aw/aw-prompts/prompt.genai.md
        echo "system: []" >> /tmp/gh-aw/aw-prompts/prompt.genai.md
        echo "system-safety: false" >> /tmp/gh-aw/aw-prompts/prompt.genai.md
        echo "---" >> /tmp/gh-aw/aw-prompts/prompt.genai.md
        cat "$GH_AW_PROMPT" >> /tmp/gh-aw/aw-prompts/prompt.genai.md
        echo "Generated GenAI prompt file:"
        cat /tmp/gh-aw/aw-prompts/prompt.genai.md
      env:
        GH_AW_PROMPT: ${{ env.GH_AW_PROMPT }}
        GH_AW_AGENT_MODEL_VERSION: ${{ env.GH_AW_AGENT_MODEL_VERSION }}
    
    - name: Run GenAIScript
      id: genaiscript
      run: genaiscript run /tmp/gh-aw/aw-prompts/prompt.genai.md --mcp-config "$GH_AW_MCP_CONFIG" --out /tmp/gh-aw/genaiscript-output.md
      env:
        DEBUG: genaiscript:*
        GH_AW_PROMPT: ${{ env.GH_AW_PROMPT }}
        GH_AW_MCP_CONFIG: ${{ env.GH_AW_MCP_CONFIG }}
        GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        OPENAI_API_KEY: ${{ secrets.OPENAI_API_KEY }}
---

<!--
This shared configuration sets up a custom agentic engine using microsoft/genaiscript.

**Usage:**
Include this file in your workflow using frontmatter imports:

```yaml
---
imports:
  - shared/genaiscript.md
---
```

**Requirements:**
- The workflow will install genaiscript npm package using version from `GH_AW_AGENT_VERSION` env var
- The original prompt file is converted to GenAI markdown format (prompt.genai.md)
- GenAIScript is executed with MCP server configuration if available
- Output is captured in the agent log file
- **OPENAI_API_KEY secret must be configured** in repository settings when using OpenAI models

**Note**: 
- This workflow requires internet access to install npm packages
- The genaiscript version can be customized by setting the `GH_AW_AGENT_VERSION` environment variable (default: `2.5.1`)
- The AI model can be customized by setting the `GH_AW_AGENT_MODEL_VERSION` environment variable (default: `openai:gpt-4o`)
- MCP server configuration is automatically passed if configured in the workflow
- When using `openai:` models, ensure the `OPENAI_API_KEY` secret is configured in your repository settings
-->
