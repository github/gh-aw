---
private: true
emoji: "📝"
name: Weekly Editors Health Check
description: Checks that the workflow editors listed in the documentation are still valid, takes Playwright screenshots, and opens a PR to update the docs with preview images
on:
  schedule: weekly
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
strict: true
tracker-id: weekly-editors-health-check
engine: copilot
timeout-minutes: 30

network:
  allowed:
    - defaults
    - playwright
    - github.github.com
    - ashleywolf.github.io
    - mossaka.github.io

imports:
  - shared/otlp.md
  - shared/noop-reminder.md
tools:
  cli-proxy: true
  playwright:
  web-fetch:
  bash:
    - "curl*"
    - "cat*"
  edit:

safe-outputs:
  upload-asset:
    max: 5
    allowed-exts: [.png, .jpg, .jpeg, .svg]
  steer: true
  create-pull-request:
    title-prefix: "[docs] "
    labels: [documentation, automation]
    reviewers: [copilot]
    expires: 7d
  noop:

sandbox:
  agent:
    runtime: cloud-hypervisor
    id: awf
---

# Weekly Editors Health Check

Monitor the health of all workflow editors listed in the documentation and keep their preview screenshots up to date.

**Repository**: ${{ github.repository }} | **Run**: ${{ github.run_id }}

## Editors to Check

The editors are documented in `docs/src/content/docs/reference/editors.mdx`.

## Process

### Step 0: Discover Editors

Read the file `docs/src/content/docs/reference/editors.mdx` using the `cat` command to extract the list of editors to inspect. Parse the file sequentially top to bottom:

1. Track the most recent `###` heading encountered — this is the editor name.
2. For each editor section (under a `###` heading), extract the editor URL from `<LinkButton href="...">`, markdown links (`[...](...)`), or plain URL bullet items (`- https://...` or `https://...`).
3. Derive the editor-id by converting the `###` heading to lowercase kebab-case (e.g., "Compiler Playground" → `compiler-playground`).
4. For any `href`/URL that is not an absolute `https://` URL (e.g., it uses template expressions like `${import.meta.env.BASE_URL}` or starts with `/`), resolve it relative to the documentation base URL `https://github.github.com/gh-aw/`. For example, `${import.meta.env.BASE_URL}editor/` resolves to `https://github.github.com/gh-aw/editor/`.
5. Build a working list of editors with: name, editor-id, and resolved URL.

Proceed with the full list of editors discovered in this step.

### Step 1: URL Availability Check

For each editor discovered in Step 0, verify that its URL is reachable:

1. Use `web_fetch` (or `curl -sS -o /dev/null -w "%{http_code}" <url>`) to perform an HTTP GET request.
2. Record the HTTP status code for each URL.
3. A status code of **200** means the editor is available.
4. Any other status code (or a connection error) means the editor is unavailable.

### Step 2: Take Playwright Screenshots

For each editor URL that responded with HTTP 200 in Step 1:

1. Run `playwright-cli browser_navigate --url "<URL>"` to navigate to the URL.
2. Wait for the page to fully load (wait for network idle).
3. Run `playwright-cli browser_take_screenshot --filename /tmp/gh-aw/agent/editors/<editor-id>-screenshot.png --full-page true` to take a full-page screenshot, where `<editor-id>` is the kebab-case id derived in Step 0.

### Step 3: Upload Screenshots as Assets

For each screenshot file saved in Step 2:

1. Use the `upload_asset` safe output to upload the PNG file.
2. Record the returned asset URL for each uploaded screenshot.

### Step 4: Update the Documentation

1. Read the current content of `docs/src/content/docs/reference/editors.mdx`.
2. For each editor section, add or replace an image tag that renders the screenshot as a preview. The image should be placed below the editor link or `<LinkButton>` component for that editor. Use this format:

   ```mdx
   ![Screenshot of <Editor Name>](<asset-url>)
   ```

   - Place the image after the editor link/button line for the corresponding editor section.
   - If an image tag already exists for a given editor, replace its URL with the newly uploaded asset URL so the preview stays fresh.
   - Only add an image for editors whose URL was reachable in Step 1.

3. Save the updated file with the `edit` tool.

### Step 5: Create a Pull Request

After updating the documentation file, use the `create-pull-request` safe output to open a pull request with:

- **Title**: `Update editor preview screenshots – <YYYY-MM-DD>`
- **Body**: Include a summary table with one row per editor showing:
  - Editor name
  - URL
  - HTTP status (✅ 200 / ❌ <status code>)
  - Embedded screenshot (if available)

Example body (rows reflect whatever editors were discovered in Step 0):

```markdown
### Editor Health Report – <date>

<details>
<summary>Editor Status & Screenshots</summary>

| Editor | URL | Status | Preview |
|--------|-----|--------|---------|
| Compiler Playground | https://github.github.com/gh-aw/editor/ | ✅ 200 | ![preview](<url>) |
| Agentic Prompt Generator | https://ashleywolf.github.io/agentic-prompt-generator/ | ✅ 200 | ![preview](<url>) |
| ... | ... | ... | ... |

</details>
```

## Error Handling

- If a URL is unreachable (non-200 status or connection error), skip the screenshot step for that editor but still include the editor in the PR body with its error status.
- If a screenshot cannot be taken (Playwright error), log the error and continue with the remaining editors.
- If no screenshots were successfully taken or no documentation changes are needed (e.g., all screenshots and links are unchanged and already up to date), do **not** open a pull request. Instead, call the `noop` safe output tool explaining what was checked and why no PR was needed.
- Always attempt all editors before deciding whether to create a PR or call `noop`.


### Output Format

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.