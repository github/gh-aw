---
name: Dictation Instructions
description: Fix speech-to-text errors and improve text clarity in dictated content related to GitHub Agentic Workflows
applyTo: "**/*"
---

# Dictation Instructions

## Technical Context

GitHub Agentic Workflows (gh-aw) is a CLI tool for writing agentic workflows in natural language using markdown files and running them as GitHub Actions. When fixing dictated text, use these project-specific terms and conventions, and improve text clarity by removing filler words and making it more professional.

## Project Glossary

@copilot
actions
activation
add-comment
add-labels
add-reviewer
admin
agent-task
agentic-workflow
agentic-workflows
allowed-domains
allowed-exts
allowed-labels
allowed-repos
api-key
api-url
app-id
args
array
assign-milestone
assign-to-agent
assign-to-user
assignees
attestations
audit
auto-close
auto-merge
auto-triage
automation
base-branch
bash
body
boolean
branch
branch-name
branch-protection-rule
branches
bug
cache
cache-memory
campaign-id
campaigns
cancel-in-progress
chatops
check-run
check-suite
checkout
checks
choice-param
claude
claude-sonnet
close-discussion
close-issue
close-pull-request
code-review
code-scanning-alert
codex
command-line
command-triggered
command-triggers
comment-triggered
commit
commit-sha
compile
concurrency
config
config-stdin
container
containers
contents
context-aware
copilot
copilot-cli
create-agent-task
create-code-scanning-alert
create-discussion
create-issue
create-pull-request
create-pull-request-review-comment
credentials
cross-repo
cross-repository
custom
custom-agent
custom-agents
custom-memory
custom-safe-outputs
custom-tool
dailyops
debug-mode
default-deny
dependencies
deployment
deployment-status
description
disable
discussion-comment
discussions
dispatchops
docker-container
downstream-fork
dry-run
edit
enable
endpoint
engine
engine-id
environment-variables
error-patterns
event
event-triggered
event-type
expression
fail-fast
fail-on-cache-miss
false-positive
feature-flag
fetch-depth
filter
fork
format
frontmatter
gh-aw
github-script
github-token
glob
grep
head-ref
http-mcp
id-token
import
inline
input
issue-comment
issue-number
issue-tracker
issueops
issues
job
job-name
json
key
label
labelops
latest
limit
lint
local
lock-yml
lockfile
log
logs
main-branch
markdown
matrix
max-parallel
mcp-gateway
mcp-server
mcp-servers
merge-commit
metadata
metrics
milestone
milestone-number
missing-tool
multi-repo
multirepoops
network
network-access
network-permissions
no-cache
npm-install
npx
on-demand
org-admin
org-level
output
output-dir
packages
permissions
pip-install
playwright
pr-number
pre-commit
private-key
private-repo
projectops
pull-request
pull-request-review-comment
pull-requests
push
read
recompile
remote
remote-repo
repo-name
repo-owner
repository-dispatch
taskops
reviewer
run-id
run-name
runs-on
safe-input
safe-inputs
safe-output
safe-outputs
sandbox
scheduled
secret-name
secrets
security-events
server-url
setup-node
setup-python
sha
siderepoops
sse-server
status-check
step-id
strict-mode
sub-task
sync-repo
tag
team-members
template
timeout-minutes
token-permissions
tool-name
toolset
toolsets
tools
triage-analysis
trialops
trigger-workflow
ubuntu-latest
unix-timestamp
update-discussion
update-issue
update-pull-request
upstream-repo
use-cache
user-agent
view
web-fetch
web-search
webhook
webhook-url
windows-latest
workflow-dispatch
workflow-file
workflow-id
workflow-name
workflow-run
workflow-run-id
write
yaml

## Fix Speech-to-Text Errors

Common speech-to-text misrecognitions and their corrections:

### Safe Outputs/Inputs
- "safe output" → safe-output
- "safe outputs" → safe-outputs
- "safe input" → safe-input
- "safe inputs" → safe-inputs
- "save outputs" → safe-outputs
- "save output" → safe-output

### Workflow Terms
- "agent ic workflows" → agentic workflows
- "agent tick workflows" → agentic workflows
- "work flow" → workflow
- "work flows" → workflows
- "G H A W" → gh-aw
- "G age A W" → gh-aw

### Configuration
- "front matter" → frontmatter
- "tool set" → toolset
- "tool sets" → toolsets
- "M C P servers" → MCP servers
- "M C P server" → MCP server
- "lock file" → lockfile

### Commands & Operations
- "re compile" → recompile
- "runs on" → runs-on
- "time out minutes" → timeout-minutes
- "work flow dispatch" → workflow-dispatch
- "pull request" → pull-request (in YAML contexts)

### GitHub Actions
- "add comment" → add-comment
- "add labels" → add-labels
- "close issue" → close-issue
- "create issue" → create-issue
- "pull request review" → pull-request-review

### AI Engines & Bots
- "co-pilot" → copilot (when referring to the engine)
- "Co-Pilot" → Copilot
- "at copilot" → @copilot (when assigning/mentioning the bot)
- "@ copilot" → @copilot
- "copilot" → @copilot (when context indicates assignment or mention)
- "code X" → codex
- "Code X" → Codex

### Spacing/Hyphenation Ambiguity
When context suggests a GitHub Actions key or CLI flag:
- Use hyphens: `timeout-minutes`, `runs-on`, `cache-memory`
- In YAML: prefer hyphenated form
- In prose: either form acceptable, prefer hyphenated for consistency

## Clean Up and Improve Text

Make dictated text clearer and more professional by:

### Remove Filler Words
Common filler words and verbal tics to remove:
- "humm", "hmm", "hm"
- "um", "uh", "uhh", "er", "err"
- "you know"
- "like" (when used as filler, not for comparisons)
- "basically", "actually", "essentially" (when redundant)
- "sort of", "kind of" (when used to hedge unnecessarily)
- "I mean", "I think", "I guess"
- "right?", "yeah", "okay" (at start/end of sentences)
- Repeated words: "the the", "and and", etc.

### Improve Clarity
- Make sentences more direct and concise
- Use active voice instead of passive voice where appropriate
- Remove redundant phrases
- Fix run-on sentences by splitting them appropriately
- Ensure proper sentence structure and punctuation
- Replace vague terms with specific technical terms from the glossary

### Maintain Professional Tone
- Keep technical accuracy
- Preserve the user's intended meaning
- Use neutral, technical language
- Avoid overly casual or conversational tone in technical contexts
- Maintain appropriate formality for documentation and technical discussions

### Examples
- "Um, so like, you need to basically compile the workflow, you know?" → "Compile the workflow."
- "I think we should, hmm, use safe-outputs for this" → "Use safe-outputs for this."
- "The workflow is kind of slow, actually" → "The workflow is slow."
- "You know, the MCP server needs to be configured" → "The MCP server needs to be configured."

## Guidelines

You do not have enough background information to plan or provide code examples.
- Do NOT generate code examples
- Do NOT plan steps or provide implementation guidance
- Focus on fixing speech-to-text errors (misrecognized words, spacing, hyphenation)
- Remove filler words and verbal tics (humm, you know, um, uh, like, etc.)
- Improve clarity and professionalism of the text
- Make text more direct and concise
- When unsure, prefer the hyphenated form for technical terms
- Preserve the user's intended meaning while correcting transcription errors and improving clarity
