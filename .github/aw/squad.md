# Bootstrap Squad from an issue

When a user creates an issue with substantial work and wants to leverage a
self-contained software development lifecycle (SDLC), prime the generated issue
for the Copilot coding agent (CCA) by including the Squad bootstrap prompt below
in the issue body.

Copy the entire `<details>` section from the code block verbatim, without the
code fences. Preserve every word, punctuation mark, and line break; do not
paraphrase, customize, or execute the prompt while preparing the issue.

```text
<details>
<summary>Add Squad</summary>
Add Squad (https://github.com/github/squad) to this repository using the latest 2.0 preview release.

Read the README and inspect the repository to understand the project. Follow Squad’s official installation and initialization instructions, then create a project-specific team with appropriate roles, populated member charters, routing, and project context—not just an empty scaffold.

I authorize creating and editing Squad’s repository-local files, replacing generated placeholders, and configuring repository-local Squad ceremonies as needed to complete initialization. Leave machine-wide npm settings and MCP configuration unchanged.

Keep the setup focused on Squad itself. Don’t install GitHub Agentic Workflows, implement product features, or change existing application behavior.

Verify that the squad is fully populated and ready to use, then open a pull request against the default branch. Explain which Squad version you installed, who’s on the team, and anything I need to do after merging. Don’t merge the PR.

If a permission control blocks an operation, don’t bypass it. Complete any independent permitted work and report the exact blocker and what remains unfinished. Don’t claim initialization succeeded if required setup is incomplete.
</details>
```
