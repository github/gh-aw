---
description: Reviews and improves technical documentation based on provided topics
on:
  workflow_dispatch:
    inputs:
      topic:
        description: 'Documentation topic to review'
        required: true
        type: string

permissions:
  contents: read
  pull-requests: read
  issues: read
  actions: read

engine:
  id: copilot
  agent: technical-doc-writer

network:
  allowed:
    - defaults
    - github

imports:
  - shared/mood.md
  - ../skills/documentation/SKILL.md
  - ../agents/technical-doc-writer.agent.md

safe-outputs:
  add-comment:
    max: 1
  create-pull-request:
    expires: 2d
    title-prefix: "[docs] "
    labels: [documentation]
    reviewers: copilot
    draft: false
  upload-asset:
  messages:
    footer: "> 📝 *Documentation by [{workflow_name}]({run_url})*"
    run-started: "✍️ The Technical Writer begins! [{workflow_name}]({run_url}) is documenting this {event_type}..."
    run-success: "📝 Documentation complete! [{workflow_name}]({run_url}) has written the docs. Clear as crystal! ✨"
    run-failure: "✍️ Writer's block! [{workflow_name}]({run_url}) {status}. The page remains blank..."

steps:
  - name: Setup Node.js
    uses: actions/setup-node@v6
    with:
      node-version: '24'
      cache: 'npm'
      cache-dependency-path: 'docs/package-lock.json'

  - name: Install dependencies
    working-directory: ./docs
    run: npm ci

  - name: Build documentation
    working-directory: ./docs
    env:
      GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}
    run: npm run build

tools:
  cache-memory: true
  github:
    toolsets: [default]
  edit:
  bash: true

timeout-minutes: 10

---

## Your Task

This workflow is triggered manually via workflow_dispatch with a documentation topic.

**Topic to review:** "${{ github.event.inputs.topic }}"

The documentation has been built successfully in the `docs/dist` folder. You can review both the source files in `docs/` and the built output in `docs/dist`.

### Available Commands

Use these commands from the repository root:

```bash
# Rebuild the documentation after making changes
make build-docs

# Start development server for live preview
make dev-docs

# Preview built documentation
make preview-docs

# Clean documentation artifacts
make clean-docs
```

### Documentation Review Process

When reviewing documentation for the specified topic in the **docs/** folder:

1. **Analyze the topic** provided in the workflow input: "${{ github.event.inputs.topic }}"

2. **Review relevant documentation files** in the docs/ folder related to the topic

3. **Make improvements** to the documentation as needed:
   - Fix clarity and conciseness issues
   - Improve tone and voice consistency with GitHub Docs
   - Enhance code block formatting and examples
   - Improve structure and organization
   - Add missing prerequisites or setup steps
   - Fix inappropriate use of GitHub alerts
   - Improve link quality and accessibility

4. **Rebuild and verify** after making changes:
   ```bash
   make build-docs
   ```
   - Fix any build errors that occur
   - Verify all links validate correctly
   - Ensure proper rendering in `docs/dist`

5. **Only after successful build**, create a pull request with improvements:
   - Use the safe-outputs create-pull-request functionality
   - Include a clear description of the improvements made
   - Document any build issues that were fixed
   - Only create a pull request if you have made actual changes

### Build Verification Requirements

**Before returning to the user or creating a pull request:**

- ✅ Run `make build-docs` to verify documentation builds successfully
- ✅ Fix any build errors, warnings, or link validation issues
- ✅ Verify the built output in `docs/dist` is properly generated
- ✅ Confirm all changes render correctly

**If build errors occur:**
- Read error messages carefully to understand the issue
- Fix broken links, invalid frontmatter, or markdown syntax errors
- Rebuild with `make build-docs` to verify fixes
- Do not proceed until the build succeeds without errors

Keep your feedback specific, actionable, and empathetic. Focus on the most impactful improvements for the topic: "${{ github.event.inputs.topic }}"

You have access to cache-memory for persistent storage across runs, which you can use to track documentation patterns and improvement suggestions.