---
bots: ["agentic-workflows-dev[bot]"]
timeout-minutes: 5
on:
  issues:
    types: [opened]
    lock-for-agent: true
  issue_comment:
    types: [created]
    lock-for-agent: true
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL to moderate (e.g., https://github.com/owner/repo/issues/123)'
        required: true
        type: string
engine:
  id: copilot
  model: gpt-5.1-codex-mini
tools:
  github:
    mode: local
    read-only: true
    toolsets: [default]
if: (github.event_name == 'workflow_dispatch') || (needs.check_external_user.outputs.should_skip != 'true')
permissions:
  contents: read
  issues: read
  pull-requests: read
safe-outputs:
  add-labels:
    allowed: [spam, ai-generated, link-spam, ai-inspected]
    target: "*"
  hide-comment:
    max: 5
    allowed-reasons: [spam]
  threat-detection: false
jobs:
  check_external_user:
    needs: [pre_activation]
    runs-on: ubuntu-slim
    outputs:
      should_skip: ${{ steps.check_actor.outputs.should_skip || github.event_name == 'workflow_dispatch' }}
    steps:
      - name: Check if actor is external user or GitHub Action bot
        id: check_actor
        uses: actions/github-script@v8
        if: ${{ github.event_name != 'workflow_dispatch' }}
        with:
          script: |
            const actor = context.actor;
            const { owner, repo } = context.repo;
            
            // Skip if the issue was opened by GitHub Action bot or Copilot bot
            const excludedBots = ['github-actions[bot]', 'github-actions', 'copilot[bot]'];
            if (actor.endsWith('[bot]') && excludedBots.includes(actor)) {
              core.info(`⏭️  Skipping workflow - issue opened by bot: ${actor}`);
              core.setOutput('should_skip', 'true');
              return;
            }
            
            try {
              core.info(`Checking permissions for user: ${actor}`);
              
              // Get the user's permission level
              const { data: permission } = await github.rest.repos.getCollaboratorPermissionLevel({
                owner,
                repo,
                username: actor
              });
              
              const userPermission = permission.permission;
              core.info(`User ${actor} has permission: ${userPermission}`);
              
              // Skip workflow for team members (admin, maintain, write)
              const teamPermissions = ['admin', 'maintain', 'write'];
              if (teamPermissions.includes(userPermission)) {
                core.info(`⏭️  Skipping workflow - ${actor} is a team member with ${userPermission} access`);
                core.setOutput('should_skip', 'true');
              } else {
                core.info(`✅ Running workflow - ${actor} is external user with ${userPermission} access`);
                core.setOutput('should_skip', 'false');
              }
            } catch (error) {
              // If we can't determine permission (e.g., user not a collaborator), assume external and run
              core.info(`⚠️  Could not determine permissions for ${actor}: ${error.message}`);
              core.info(`✅ Running workflow - assuming external user`);
              core.setOutput('should_skip', 'false');
            }
imports:
  - shared/mood.md
---

{{#runtime-import? .github/shared-instructions.md}}

# AI Moderator

You are an AI-powered moderation system that automatically detects spam, link spam, and AI-generated content in GitHub issues and comments.

## Context

Analyze the following content in repository ${{ github.repository }}:

**Issue Number**: #${{ github.event.issue.number }}
**Comment ID** (if applicable): ${{ github.event.comment.id }}
**Author**: ${{ github.actor }}
**Manual URL** (if provided via workflow_dispatch): ${{ github.event.inputs.issue_url }}

**Content to analyze**:

When running via `workflow_dispatch` with an `issue_url` input:
1. Parse the issue URL to extract the owner, repo, and issue number
2. Validate that the URL is an issue URL (not a pull request URL)
3. Use the GitHub MCP server tools (available via `github` toolset) to fetch the full issue content
4. Specifically, use the appropriate GitHub API tool to get the issue details including title and body

For other trigger types (issues, issue_comment):
1. Extract the relevant identifiers from the context:
   - For issues: Use issue number from ${{ github.event.issue.number }}
   - For comments: Use issue number and comment ID from the event payload
2. Use the GitHub MCP server tools to fetch the original, unsanitized content directly from GitHub API
3. Do NOT use the pre-sanitized text from the activation job - fetch fresh content to analyze the original user input

## Custom Moderation Rules (Optional)

If custom moderation instructions exist at `.github/prompts/custom-moderation.md` in the repository, read that file as additional system prompt instructions. The custom prompt should be in markdown format and contain repository-specific spam detection criteria.

Example custom moderation file (`.github/prompts/custom-moderation.md`):
```markdown
# Custom Moderation Rules

Additional spam indicators for this repository:
- Posts mentioning competitor products (CompetitorX, CompetitorY)
- Off-topic gaming discussions (this is a development tools project)
- Cryptocurrency or blockchain mentions (not relevant to this project)
- Generic "me too" comments without substance
```

## Detection Tasks

Perform the following detection analyses on the content:

### 1. Generic Spam Detection

Analyze for spam indicators:
- Promotional content or advertisements
- Irrelevant links or URLs
- Repetitive text patterns
- Low-quality or nonsensical content
- Requests for personal information
- Cryptocurrency or financial scams
- Content that doesn't relate to the repository's purpose

### 2. Link Spam Detection

Analyze for link spam indicators:
- Multiple unrelated links
- Links to promotional websites
- Short URL services used to hide destinations (bit.ly, tinyurl, etc.)
- Links to cryptocurrency, gambling, or adult content
- Links that don't relate to the repository or issue topic
- Suspicious domains or newly registered domains
- Links to download executables or suspicious files

### 3. AI-Generated Content Detection

Analyze for AI-generated content indicators:
- Use of em-dashes (—) in casual contexts
- Excessive use of emoji, especially in technical discussions
- Perfect grammar and punctuation in informal settings
- Constructions like "it's not X - it's Y" or "X isn't just Y - it's Z"
- Overly formal paragraph responses to casual questions
- Enthusiastic but content-free responses ("That's incredible!", "Amazing!")
- "Snappy" quips that sound clever but add little substance
- Generic excitement without specific technical engagement
- Perfectly structured responses that lack natural conversational flow
- Responses that sound like they're trying too hard to be engaging

Human-written content typically has:
- Natural imperfections in grammar and spelling
- Casual internet language and slang
- Specific technical details and personal experiences
- Natural conversational flow with genuine questions or frustrations
- Authentic emotional reactions to technical problems

## Actions

Based on your analysis:

1. **For Issues** (when issue number is present):
   - If generic spam is detected, use the `add-labels` safe output to add the `spam` label to the issue
   - If link spam is detected, use the `add-labels` safe output to add the `link-spam` label to the issue
   - If AI-generated content is detected, use the `add-labels` safe output to add the `ai-generated` label to the issue
   - Multiple labels can be added if multiple types are detected
   - **If no warnings or issues are found** and the content appears legitimate and on-topic, use the `add-labels` safe output to add the `ai-inspected` label to indicate the issue has been reviewed and no threats were found
   - **If workflow_dispatch** was used, ensure the labels are applied to the correct issue/PR as specified in the input URL when calling `add-labels`

2. **For Comments** (when comment ID is present):
   - If any type of spam, link spam, or AI-generated spam is detected:
     - Use the `hide-comment` safe output to hide the comment with reason 'spam'
     - Also add appropriate labels to the parent issue as described above
   - If the comment appears legitimate and on-topic, add the `ai-inspected` label to the parent issue

## Important Guidelines

- Be conservative with detections to avoid false positives
- Consider the repository context when evaluating relevance
- Technical discussions may naturally contain links to resources, documentation, or related issues
- New contributors may have less polished writing - this doesn't necessarily indicate AI generation
- Provide clear reasoning for each detection in your analysis
- Only take action if you have high confidence in the detection

<!--
# AI Moderator Workflow

An AI-powered GitHub Agentic Workflow that automatically detects spam in issues and comments, inspired by [github/ai-moderator](https://github.com/github/ai-moderator).

## Features

- **Automatic Spam Detection**: Detects promotional content, scams, and irrelevant posts
- **Link Spam Detection**: Identifies suspicious URLs and shortened links
- **AI-Generated Content Detection**: Recognizes artificially generated content patterns
- **Automated Actions**: 
  - Adds labels (`spam`, `link-spam`, `ai-generated`) to flagged issues
  - Minimizes detected spam comments
- **Multiple Trigger Support**: Works on new issues, issue comments, and PR review comments

## How It Works

This workflow uses GitHub Copilot's AI capabilities to analyze content posted to your repository. When triggered by:
- A new issue being opened
- A new comment on an issue
- Manual workflow dispatch with an issue URL

The AI agent:
1. Fetches the content to analyze
2. Runs three detection analyses (general spam, link spam, AI-generated)
3. Takes appropriate action based on findings:
   - For issues: Adds relevant labels
   - For comments: Hides the comment and adds labels to the parent issue
4. Skips processing for:
   - Issues opened by GitHub Action bots
   - Team members with write access or higher

## Detection Criteria

### Generic Spam
- Promotional content or advertisements
- Cryptocurrency or financial scams
- Repetitive text patterns
- Low-quality or nonsensical content
- Requests for personal information

### Link Spam
- Multiple unrelated links
- Short URL services (bit.ly, tinyurl, etc.)
- Links to promotional, gambling, or adult content
- Suspicious or newly registered domains
- Links to executables or suspicious downloads

### AI-Generated Content
- Overly formal responses in casual contexts
- Perfect grammar in informal settings
- Excessive emoji usage in technical discussions
- Generic enthusiasm without substance
- Perfectly structured responses lacking natural flow

## Configuration

The workflow is configured in `.github/workflows/ai-moderator.md` with the following settings:

```yaml
timeout-minutes: 5
on:
  issues:
    types: [opened]
  issue_comment:
    types: [created]
  workflow_dispatch:
    inputs:
      issue_url:
        description: 'Issue URL to moderate (e.g., https://github.com/owner/repo/issues/123)'
        required: true
        type: string
engine:
  id: copilot
  model: gpt-5.1-codex-mini
tools:
  github:
    mode: local
    read-only: true
    toolsets: [default]
safe-outputs:
  add-labels:
    allowed: [spam, ai-generated, link-spam, ai-inspected]
  hide-comment:
    max: 5
    allowed-reasons: [spam]
  threat-detection: false
permissions:
  models: read
  contents: read
  issues: read
```

### Labels

The workflow uses these labels:
- `spam` - Generic spam content
- `link-spam` - Suspicious or promotional links
- `ai-generated` - AI-generated content
- `ai-inspected` - Content reviewed and no threats found by AI moderator

### Safe Outputs

The workflow uses two built-in safe outputs:
- **add-labels**: Adds labels to issues and PRs (spam, link-spam, ai-generated, ai-inspected)
- **hide-comment**: Hides spam comments using GitHub's built-in functionality

The hide-comment safe output requires the GraphQL node ID of the comment, which the AI agent fetches from the GitHub API.

**Threat Detection**: Threat detection is disabled for this workflow to streamline the moderation process.

## Security

The workflow follows security best practices:
- **Read-only main job**: The AI analysis job only has read permissions
- **Safe outputs**: Write operations (labeling, minimizing) are handled by separate jobs with explicit permissions
- **Built-in safe outputs**: Uses gh-aw's built-in hide-comment instead of custom GraphQL code
- **Strict mode**: Enforces security constraints to prevent unauthorized actions

## Comparison with github/ai-moderator

| Feature | github/ai-moderator | gh-aw AI Moderator |
|---------|---------------------|-------------------|
| Spam Detection | ✅ | ✅ |
| Link Spam Detection | ✅ | ✅ |
| AI Content Detection | ✅ | ✅ |
| Label Application | ✅ | ✅ |
| Comment Minimization | ✅ | ✅ |
| Custom Prompts | ✅ File-based | ✅ Inline in workflow |
| Dry-run Mode | ✅ | 🔜 (future enhancement) |
| Configuration | Action inputs | Workflow frontmatter |
| AI Engine | GitHub Models | GitHub Copilot |

## Customization

To customize the detection behavior:

1. **Custom Prompt File (Recommended)**: Create a markdown file at `.github/prompts/custom-moderation.md` in your repository with repository-specific spam detection rules. The AI agent will read this file as additional system prompt instructions.
   
   Example custom moderation file:
   ```markdown
   # Custom Moderation Rules

   Additional spam indicators for this repository:
   - Posts mentioning competitor products (CompetitorX, CompetitorY)
   - Off-topic gaming discussions (this is a development tools project)
   - Cryptocurrency or blockchain mentions (not relevant to this project)
   - Generic "me too" comments without substance
   - Links to specific domains we don't allow: example-spam-site.com
   ```

2. **Edit the workflow**: Modify `.github/workflows/ai-moderator.md`
3. **Adjust detection criteria**: Update the prompt sections for each detection type
4. **Change labels**: Modify the `allowed` list in `safe-outputs.add-labels`
5. **Add/remove triggers**: Update the `on:` section
6. **Recompile**: Run `gh aw compile ai-moderator` to generate the updated `.lock.yml` file

## Example Workflow Run

1. A user opens a new issue with spam content
2. The workflow is triggered automatically
3. The AI agent fetches the issue content
4. Analyzes the content for spam, link spam, and AI-generated patterns
5. Detects spam indicators with high confidence
6. Adds the `spam` label to the issue
7. Logs the detection reasoning for transparency

## Limitations

- Requires GitHub Copilot access (via `engine: copilot` and appropriate permissions)
- Labels must be pre-created in the repository
- Conservative detection to minimize false positives
- May not catch sophisticated or evolving spam patterns

## Contributing

To improve the detection accuracy:
1. Review false positives/negatives in your workflow runs
2. Adjust the detection criteria in the workflow prompt
3. Test with `gh aw trial` before deploying
4. Submit feedback or improvements via pull request

## License

This workflow is part of the gh-aw project and follows the same license terms.
-->
