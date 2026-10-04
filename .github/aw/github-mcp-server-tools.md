---
description: Toolset-by-toolset reference for GitHub MCP server tools, including purposes and key parameters.
---

# GitHub MCP Server — Tools by Toolset

Full tool reference for each toolset. See [github-mcp-server.md](github-mcp-server.md) for overview, configuration, and recommended defaults.

### context
**Description**: Team-awareness helpers for GitHub org membership. Workflow metadata is injected separately as `<github-context>` whenever the GitHub tool is configured.

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_me` | Get details of the authenticated user | ⚠️ Do not use for workflow identity; read `<github-context>` instead |
| `get_team_members` | List members of a GitHub team | `org`, `team_slug` |
| `get_teams` | List teams the authenticated user belongs to | `org` |

---

### code_quality
**Description**: Code quality findings

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_code_quality_finding` | Get details of a specific code quality finding | `owner`, `repo`, `alert_number` |

---

### copilot
**Description**: GitHub Copilot assignment, review, and coding agent tools

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `assign_copilot_to_issue` | Assign GitHub Copilot to an issue | `owner`, `repo`, `issue_number` |
| `create_pull_request_with_copilot` | Ask Copilot to create a pull request | `owner`, `repo`, `issue_number` |
| `request_copilot_review` | Request a Copilot review on a pull request | `owner`, `repo`, `pullNumber` |

---

### copilot_issue_intents
**Description**: Opt-in Copilot issue assignment tools with intent metadata

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `assign_copilot_to_issue_with_intent` | Assign Copilot to an issue with intent metadata | `owner`, `repo`, `issue_number`, `rationale`, `confidence`, `is_suggestion` |

---

### copilot_spaces
**Description**: GitHub Copilot Spaces (remote-only)

> **Note**: Remote-only toolset — only available when using the GitHub MCP server in remote mode (`https://api.githubcopilot.com/mcp/`). Not available with the local `gh mcp` mode.

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_copilot_space` | Get details of a specific Copilot Space | `owner`, `name` |
| `list_copilot_spaces` | List Copilot Spaces for a user or organization | `owner` |

---

### repos
**Description**: Repository operations

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `create_branch` | Create a new branch | `owner`, `repo`, `branch`, `from_branch` |
| `create_or_update_file` | Create or update a file in a repository | `owner`, `repo`, `path`, `content`, `message`, `branch` |
| `create_repository` | Create a new GitHub repository | `name`, `description`, `private`, `auto_init` |
| `delete_file` | Delete a file from a repository | `owner`, `repo`, `path`, `message`, `sha`, `branch` |
| `fork_repository` | Fork a repository | `owner`, `repo`, `organization` |
| `get_commit` | Get details of a specific commit | `owner`, `repo`, `sha` |
| `get_file_blame` | Get line-by-line blame information for a file | `owner`, `repo`, `path`, `ref` |
| `get_file_contents` | Read file or directory contents | `owner`, `repo`, `path`, `ref` |
| `get_latest_release` | Get the latest release for a repository | `owner`, `repo` |
| `get_release_by_tag` | Get a release by its tag name | `owner`, `repo`, `tag` |
| `get_tag` | Get details of a specific tag | `owner`, `repo`, `tag` |
| `list_branches` | List branches in a repository | `owner`, `repo`, `page`, `per_page` |
| `list_commits` | List commits in a repository | `owner`, `repo`, `sha`, `path`, `page` |
| `list_releases` | List all releases for a repository | `owner`, `repo`, `page`, `per_page` |
| `list_repository_collaborators` | List collaborators of a repository | `owner`, `repo`, `affiliation`, `page`, `per_page` |
| `list_tags` | List tags in a repository | `owner`, `repo`, `page`, `per_page` |
| `search_code` | Search code across repositories | `query`, `page`, `per_page` |
| `search_commits` | Search commits across GitHub | `query`, `page`, `per_page` |
| `search_repositories` | Search for repositories | `query`, `page`, `per_page` |
| `push_files` | Push multiple files in a single commit | `owner`, `repo`, `branch`, `files`, `message` |

> **`search_repositories` known limitation — `repo:` qualifier is ignored**: The `repo:owner/name` qualifier has no effect in `search_repositories` queries. Instead of scoping results to the named repository, the API ranks by star count and may return a completely unrelated high-star repository as the top hit (e.g. querying `repo:github/gh-aw` may return `github/gitignore`). **Do not use `repo:` with `search_repositories`.**
>
> - To check whether a specific repository exists or to fetch its metadata, use `get_file_contents` (with explicit `owner` and `repo`) or any other `repos`-toolset call that takes `owner`/`repo` directly.
> - To discover repositories in a scope, use supported qualifiers such as `org:`, `user:`, `topic:`, `language:`, or `stars:`.

---

### git
**Description**: Git API operations (tree, refs)

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_repository_tree` | Get the file tree of a repository | `owner`, `repo`, `sha`, `recursive` |

---

### github_support_docs_search
**Description**: GitHub support documentation search (remote-only)

> **Note**: Remote-only toolset — only available when using the GitHub MCP server in remote mode (`https://api.githubcopilot.com/mcp/`). Not available with the local `gh mcp` mode.

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `github_support_docs_search` | Search GitHub support documentation | `query` |

---

### issues
**Description**: Issue management

> **Note**: `find_duplicate`, `issue_dependency_read`, and `issue_dependency_write` require their upstream feature flags to be enabled, independently of toolset selection.

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `add_issue_comment` | Add a comment to an issue | `owner`, `repo`, `issue_number`, `body` |
| `find_duplicate` | Find likely duplicate issues for an existing issue | `owner`, `repo`, `issue_number`, `confidence_threshold` |
| `get_label` | Get details of a specific label | `owner`, `repo`, `name` |
| `issue_dependency_read` | Read an issue's dependency relationships | `owner`, `repo`, `issue_number` |
| `issue_dependency_write` | Add or remove issue dependencies | `owner`, `repo`, `issue_number` |
| `issue_read` | Read issue details and comments | `owner`, `repo`, `issue_number` |
| `issue_write` | Create or update an issue | `owner`, `repo`, `title`, `body`, `labels`, `assignees` |
| `list_issue_fields` | List available issue fields for a repository | `owner`, `repo` |
| `list_issue_types` | List available issue types for a repository | `owner`, `repo` |
| `list_issues` | List issues in a repository | `owner`, `repo`, `state`, `labels`, `page` |
| `search_issues` | Search issues across GitHub | `query`, `page`, `per_page` |
| `semantic_issue_similarity_search` | Find GitHub issues semantically similar to a given issue | `owner`, `repo`, `issue_number` |
| `semantic_issues_search` | Search issues using natural language queries | `query`, `owner`, `repo` |
| `sub_issue_write` | Create or manage sub-issues | `owner`, `repo`, `issue_number` |

> **Tool availability note**: `semantic_issues_search` is listed in the mapping but was not observed during remote MCP self-inspection. Its natural-language search description overlaps with `search_issues`; whether it has been consolidated upstream is unconfirmed.

---

### pull_requests
**Description**: Pull request operations

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `add_comment_to_pending_review` | Add a comment to a pending PR review | `owner`, `repo`, `pull_number`, `review_id` |
| `add_reply_to_pull_request_comment` | Reply to a PR review comment | `owner`, `repo`, `pull_number`, `comment_id`, `body` |
| `create_pull_request` | Create a new pull request | `owner`, `repo`, `title`, `body`, `head`, `base` |
| `list_pull_requests` | List pull requests in a repository | `owner`, `repo`, `state`, `head`, `base` |
| `merge_pull_request` | Merge a pull request | `owner`, `repo`, `pull_number`, `merge_method` |
| `pull_request_read` | Read PR details, reviews, and comments | `owner`, `repo`, `pull_number` |
| `pull_request_review_write` | Create or submit a PR review | `owner`, `repo`, `pull_number`, `event`, `body` |
| `search_pull_requests` | Search pull requests across GitHub | `query`, `page`, `per_page` |
| `update_pull_request` | Update PR title, body, or state | `owner`, `repo`, `pull_number`, `title`, `body` |
| `update_pull_request_branch` | Update PR branch with latest base | `owner`, `repo`, `pull_number` |

---

### actions
**Description**: GitHub Actions workflows

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `actions_get` | Get details of a specific workflow run | `owner`, `repo`, `run_id` |
| `actions_list` | List GitHub Actions workflows and runs | `owner`, `repo`, `method`, `resource_id`, `per_page`, `page` |
| `actions_run_trigger` | Trigger a workflow run | `owner`, `repo`, `workflow_id`, `ref`, `inputs` |
| `get_job_logs` | Download logs for a specific workflow job | `owner`, `repo`, `job_id` |

---

### automations
**Description**: Copilot automation tools (provisional)

> **Note**: These tools were observed through remote MCP self-inspection but are not present in the upstream `github/github-mcp-server` main-branch tool registry. The toolset name and empty permission requirements are provisional pending confirmation.

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_automation` | Get a Copilot automation | `owner`, `repo`, `automation_id` |
| `get_automation_revision` | Get a revision of a Copilot automation | `owner`, `repo`, `automation_id`, `revision_id` |
| `list_automation_revisions` | List Copilot automation revisions | `owner`, `repo`, `automation_id` |
| `list_automations` | List Copilot automations | `owner`, `repo` |
| `list_available_automation_tools` | List tools available to Copilot automations | — |
| `list_available_automation_triggers` | List triggers available to Copilot automations | — |

---

### code_security
**Description**: Code scanning alerts

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_code_scanning_alert` | Get details of a specific code scanning alert | `owner`, `repo`, `alert_number` |
| `list_code_scanning_alerts` | List code scanning alerts for a repository | `owner`, `repo`, `state`, `severity` |

When calling `list_code_scanning_alerts` in workflow prompts/templates, always bound requests with `state: open` and `severity: critical,high`.

---

### dependabot
**Description**: Dependabot alerts

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_dependabot_alert` | Get details of a specific Dependabot alert | `owner`, `repo`, `alert_number` |
| `list_dependabot_alerts` | List Dependabot alerts for a repository | `owner`, `repo`, `state`, `severity` |

---

### discussions
**Description**: GitHub Discussions

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_discussion` | Get details of a specific discussion | `owner`, `repo`, `discussion_number` |
| `get_discussion_comments` | Get comments for a specific discussion | `owner`, `repo`, `discussion_number` |
| `list_discussion_categories` | List discussion categories for a repository | `owner`, `repo` |
| `list_discussions` | List discussions in a repository | `owner`, `repo`, `category_id` |

---

### gists
**Description**: Gist operations

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `create_gist` | Create a new gist | `description`, `files`, `public` |
| `get_gist` | Get a specific gist by ID | `gist_id` |
| `list_gists` | List gists for a user | `username`, `page`, `per_page` |
| `update_gist` | Update an existing gist | `gist_id`, `description`, `files` |

---

### governance
**Description**: Rulesets and custom properties at repository, organization, and enterprise levels

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `create_repository_ruleset` | Create a ruleset | `level`, `owner`, `repo`, `name`, `enforcement`, ruleset definition |
| `custom_properties_read` | Read custom properties | `level`, `owner`, `repo`, `org`, `enterprise` |
| `custom_properties_write` | Set custom properties | `level`, `owner`, `repo`, `org`, `enterprise`, `properties` |
| `repository_ruleset_read` | Read rulesets and rule suites | `level`, `method`, `owner`, `repo`, `ruleset_id` |

Repository-level ruleset operations require the `administration` permission; write operations require `administration: write`. Organization- and enterprise-level operations require the corresponding elevated access.

---

### labels
**Description**: Label management

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_label` | Get details of a specific label | `owner`, `repo`, `name` |
| `label_write` | Create or update a label | `owner`, `repo`, `name`, `color`, `description` |
| `list_label` | List labels in a repository | `owner`, `repo`, `page`, `per_page` |

---

### notifications
**Description**: Notification management

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `dismiss_notification` | Dismiss a specific notification | `notification_id` |
| `get_notification_details` | Get details of a specific notification | `notification_id` |
| `list_notifications` | List user notifications | `all`, `participating`, `page` |
| `manage_notification_subscription` | Manage notification subscription for a thread | `thread_id`, `subscribed` |
| `manage_repository_notification_subscription` | Manage notifications for a repository | `owner`, `repo`, `subscribed` |
| `mark_all_notifications_read` | Mark all notifications as read | `last_read_at` |

---

### orgs
**Description**: Organization operations

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `search_orgs` | Search GitHub organizations | `query`, `page`, `per_page` |

---

### projects
**Description**: GitHub Projects (requires PAT — not supported by GITHUB_TOKEN)

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `projects_get` | Get details of a specific project | `owner`, `project_number` |
| `projects_list` | List GitHub Projects for a user or organization | `owner`, `per_page` |
| `projects_write` | Create or update project items/fields | `owner`, `project_number` |

---

### secret_protection
**Description**: Secret scanning

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `get_secret_scanning_alert` | Get details of a specific secret scanning alert | `owner`, `repo`, `alert_number` |
| `list_secret_scanning_alerts` | List secret scanning alerts for a repository | `owner`, `repo`, `state` |
| `run_secret_scanning` | Scan file contents or diffs for exposed secrets | `content` |

---

### security_advisories
**Description**: Security advisories

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `check_dependency_vulnerabilities` | Check dependencies against known vulnerabilities in the GitHub Advisory Database | `owner`, `repo`, `dependencies` |
| `get_global_security_advisory` | Get a specific global security advisory | `ghsa_id` |
| `list_global_security_advisories` | List advisories from the GitHub Advisory Database | `type`, `severity`, `ecosystem` |
| `list_org_repository_security_advisories` | List security advisories for all repos in an org | `org`, `state` |
| `list_repository_security_advisories` | List security advisories for a specific repository | `owner`, `repo`, `state` |

---

### stargazers
**Description**: Repository stars

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `list_starred_repositories` | List repositories starred by a user | `username`, `page`, `per_page` |
| `star_repository` | Star a repository | `owner`, `repo` |
| `unstar_repository` | Unstar a repository | `owner`, `repo` |

---

### users
**Description**: User information

| Tool | Purpose | Key Parameters |
|------|---------|----------------|
| `search_users` | Search GitHub users | `query`, `page`, `per_page` |
