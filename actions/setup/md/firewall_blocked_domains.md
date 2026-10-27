> [!WARNING]
> <details>
> <summary>Firewall blocked {domain_count} {domain_word}</summary>
>
> The following {domain_word} {verb} blocked by the firewall during workflow execution:
>
> {domain_list}
>
> To allow these domains, add them to the `network.allowed` list in your workflow frontmatter:
>
> ```yaml
> network:
>   allowed:
>     - defaults
> {yaml_network_list}
> ```
>
> See [Network Configuration](https://github.github.com/gh-aw/reference/network/) for more information.
>
> </details>

{{#if {has_github_api_blocked}}}
> [!TIP]
> `api.github.com` is blocked because GitHub API access uses the built-in GitHub tools by default. Instead of adding `api.github.com` to `network.allowed`, use `tools.github.mode: gh-proxy` for direct pre-authenticated GitHub CLI access without requiring network access to `api.github.com`:
>
> ```yaml
> tools:
>   github:
>     mode: gh-proxy
> ```
>
> See [GitHub Tools](https://github.github.com/gh-aw/reference/github-tools/) for more information on `gh-proxy` mode.
{{/if}}
