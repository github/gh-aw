---
safe-outputs:
  jobs:
    post-to-slack-channel:
      description: "Post a message to a Slack channel. Message must be 200 characters or less. Supports basic Slack markdown: *bold*, _italic_, ~strike~, `code`, ```code block```, >quote, and links <url|text>. Requires GH_AW_SLACK_CHANNEL_ID environment variable to be set."
      runs-on: ubuntu-latest
      output: "Message posted to Slack successfully!"
      inputs:
        message:
          description: "The message to post (max 200 characters, supports Slack markdown)"
          required: true
          type: string
      permissions:
        contents: read
      steps:
        - name: Post message to Slack
          uses: actions/github-script@v9
          env:
            SLACK_BOT_TOKEN: "${{ secrets.SLACK_BOT_TOKEN }}"
            SLACK_CHANNEL_ID: "${{ env.GH_AW_SLACK_CHANNEL_ID }}"
          with:
            script: |
              const fs = require('fs');
              const slackBotToken = process.env.SLACK_BOT_TOKEN;
              const slackChannelId = process.env.SLACK_CHANNEL_ID;
              const isStaged = process.env.GH_AW_SAFE_OUTPUTS_STAGED === 'true';
              const outputContent = process.env.GH_AW_AGENT_OUTPUT;
              
              // Validate required environment variables
              if (!slackBotToken) {
                core.setFailed('SLACK_BOT_TOKEN secret is not configured. Please add it to your repository secrets.');
                return;
              }
              
              if (!slackChannelId) {
                core.setFailed('GH_AW_SLACK_CHANNEL_ID environment variable is required');
                return;
              }
              
              // Read and parse agent output
              if (!outputContent) {
                core.info('No GH_AW_AGENT_OUTPUT environment variable found');
                return;
              }
              
              let agentOutputData;
              try {
                const fileContent = fs.readFileSync(outputContent, 'utf8');
                agentOutputData = JSON.parse(fileContent);
              } catch (error) {
                core.setFailed(`Error reading or parsing agent output: ${error instanceof Error ? error.message : String(error)}`);
                return;
              }
              
              if (!agentOutputData.items || !Array.isArray(agentOutputData.items)) {
                core.info('No valid items found in agent output');
                return;
              }
              
              // Filter for post_to_slack_channel items
              const slackMessageItems = agentOutputData.items.filter(item => item.type === 'post_to_slack_channel');
              
              if (slackMessageItems.length === 0) {
                core.info('No post_to_slack_channel items found in agent output');
                return;
              }
              
              core.info(`Found ${slackMessageItems.length} post_to_slack_channel item(s)`);
              
              // Process each message item
              for (let i = 0; i < slackMessageItems.length; i++) {
                const item = slackMessageItems[i];
                const message = item.message;
                
                if (!message) {
                  core.warning(`Item ${i + 1}: Missing message field, skipping`);
                  continue;
                }
                
                // Validate message length (max 200 characters)
                const maxLength = 200;
                if (message.length > maxLength) {
                  core.warning(`Item ${i + 1}: Message length (${message.length} characters) exceeds maximum allowed length of ${maxLength} characters, skipping`);
                  continue;
                }
                
                if (isStaged) {
                  let summaryContent = "## 🎭 Staged Mode: Slack Message Preview\n\n";
                  summaryContent += "The following message would be posted to Slack if staged mode was disabled:\n\n";
                  summaryContent += `**Channel ID:** ${slackChannelId}\n\n`;
                  summaryContent += `**Message:** ${message}\n\n`;
                  summaryContent += `**Message Length:** ${message.length} characters\n\n`;
                  await core.summary.addRaw(summaryContent).write();
                  core.info("📝 Slack message preview written to step summary");
                  continue;
                }
                
                core.info(`Posting message ${i + 1}/${slackMessageItems.length} to Slack channel: ${slackChannelId}`);
                core.info(`Message length: ${message.length} characters`);
                
                try {
                  const response = await fetch('https://slack.com/api/chat.postMessage', {
                    method: 'POST',
                    headers: {
                      'Content-Type': 'application/json; charset=utf-8',
                      'Authorization': `Bearer ${slackBotToken}`
                    },
                    body: JSON.stringify({
                      channel: slackChannelId,
                      text: message
                    })
                  });
                  
                  const data = await response.json();
                  
                  if (!response.ok) {
                    core.setFailed(`Slack API HTTP error (${response.status}): ${response.statusText}`);
                    return;
                  }
                  
                  if (!data.ok) {
                    core.setFailed(`Slack API error: ${data.error || 'Unknown error'}`);
                    if (data.error === 'invalid_auth') {
                      core.error('Authentication failed. Please verify your SLACK_BOT_TOKEN is correct.');
                    } else if (data.error === 'channel_not_found') {
                      core.error('Channel not found. Please verify the GH_AW_SLACK_CHANNEL_ID environment variable is correct and the bot has access to it.');
                    }
                    return;
                  }
                  
                  core.info(`✅ Message ${i + 1} posted successfully to Slack`);
                  core.info(`Message timestamp: ${data.ts}`);
                  core.info(`Channel: ${data.channel}`);
                } catch (error) {
                  core.setFailed(`Failed to post message ${i + 1} to Slack: ${error instanceof Error ? error.message : String(error)}`);
                  return;
                }
              }
---

## Slack Integration

This shared configuration provides a custom safe-job for posting messages to Slack channels.

### Safe Job: post-to-slack-channel

The `post-to-slack-channel` safe-job allows agentic workflows to post messages to Slack channels through the Slack API.

**Agent Output Format:**

The agent should output JSON with items of type `post_to_slack_channel`:

```json
{
  "items": [
    {
      "type": "post_to_slack_channel",
      "message": "Your message here (max 200 characters)"
    }
  ]
}
```

**Required Environment Variable:**
- `GH_AW_SLACK_CHANNEL_ID`: The Slack channel ID (e.g., C1234567890) where messages will be posted

**Message Field:**
- `message`: The message text to post (maximum 200 characters)

**Message Length Limit:**
Messages are limited to 200 characters to ensure concise, focused updates. Items with messages exceeding this limit will be skipped with a warning.

**Supported Slack Markdown:**
The message supports basic Slack markdown syntax:
- `*bold*` - Bold text
- `_italic_` - Italic text
- `~strike~` - Strikethrough text
- `` `code` `` - Inline code
- ` ```code block``` ` - Code block
- `>quote` - Block quote
- `<url|text>` - Hyperlink with custom text

**Example Usage in Workflow:**

```
Please post a summary using the post_to_slack_channel output type.
Keep the message under 200 characters.
```

Note: The `GH_AW_SLACK_CHANNEL_ID` environment variable must be set in your workflow configuration or repository environment variables.

**Staged Mode Support:**

This safe-job fully supports staged mode. When `staged: true` is set in the workflow's safe-outputs configuration, messages will be previewed in the step summary instead of being posted to Slack.

### Setup

1. **Create a Slack App** with a Bot User OAuth Token:
   - Go to https://api.slack.com/apps
   - Create a new app or select an existing one
   - Navigate to "OAuth & Permissions"
   - Add the `chat:write` bot token scope
   - Install the app to your workspace
   - Copy the "Bot User OAuth Token" (starts with `xoxb-`)

2. **Add the bot to your channel**:
   - In Slack, go to the channel where you want to post messages
   - Type `/invite @YourBotName` to add the bot
   - Get the channel ID from the channel details

3. **Configure GitHub Secrets and Environment Variables**:
   - Add `SLACK_BOT_TOKEN` secret to your repository with the Bot User OAuth Token
   - Add `GH_AW_SLACK_CHANNEL_ID` as an environment variable or repository variable with the Slack channel ID

4. **Include this configuration in your workflow**:
   ```yaml
   imports:
     - shared/mcp/slack.md
   ```
