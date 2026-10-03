---
name: npm
description: Troubleshoot npm registry, proxy, and certificate failures on the Microsoft corporate network or VPN using the 1ES public npm feed.
---

# npm on the Microsoft network or VPN

Use this skill when npm downloads are blocked by the Microsoft corporate network or VPN. Run commands in the affected package directory. Prefer `npm ci` with the existing lockfile and default registry when access works; do not change dependency versions to solve a network failure.

## Use the 1ES public npm feed

When access to npmjs is blocked, retry through the Microsoft 1ES public feed with command-scoped settings:

```sh
registry=https://ms-feed-25.pkgs.visualstudio.com/1es-public/_packaging/npm-public/npm/registry/
npm ci --registry="$registry" --replace-registry-host=npmjs
```

`--replace-registry-host=npmjs` routes npmjs lockfile downloads through the selected registry without rewriting the lockfile. Private registries and lifecycle scripts that download from other hosts still need their own approved network access. Do not edit manifests, lockfile URLs, or committed npm configuration just to work around the local VPN.

To diagnose feed access, query an actual package and exact version from the lockfile. For example, substitute the project's pinned package and version in:

```sh
npm view yaml@2.9.0 version --registry="$registry" \
  --fetch-retries=0 --fetch-timeout=15000
```

Use `npm view`, not `npm ping`: the feed may not implement the ping endpoint. A successful metadata query checks that package's availability, not tarball or postinstall access. Retry the original install to verify recovery.

If the feed returns an authentication error or lacks the pinned package, report the blocker rather than guessing credentials or trying arbitrary mirrors. [Caching an upstream package can require feed permissions](https://learn.microsoft.com/en-us/azure/devops/artifacts/concepts/upstream-sources?view=azure-devops).

## Proxy and certificate failures

The feed is an npm registry, **not an HTTP proxy**. Never set `HTTPS_PROXY` or `HTTP_PROXY` to the feed URL. If a forward proxy is required, use only an IT-approved proxy URL in the current shell and check `NO_PROXY` for unintended bypasses. Do not expose proxy credentials or copy machine-specific configuration into the repository or logs.

For certificate-chain errors, use the approved corporate CA rather than disabling TLS verification:

- On Node versions that support it, retry with `NODE_USE_SYSTEM_CA=1` so Node uses the system trust store.
- Otherwise, set `NODE_EXTRA_CA_CERTS` to the path of an IT-provided PEM certificate bundle **before starting npm**.
- Check whether an npm `cafile` override is selecting a different CA bundle.

Keep HTTPS and TLS verification enabled. Never use `strict-ssl=false`, `NODE_TLS_REJECT_UNAUTHORIZED=0`, `curl -k`, or an HTTP registry, and do not disconnect the VPN or bypass corporate network controls.

## Stop on unresolved network failures

For `ENOTCONN`, connection resets, timeouts, or firewall denials, report the failing hostname and error with secrets redacted. Distinguish registry metadata, package tarball, and lifecycle-script failures. Stop repeated installs once the network blocker is confirmed; do not delete the lockfile or clear caches as a network fix. Escalate to IT for approved HTTPS access to the failing host, including `ms-feed-25.pkgs.visualstudio.com` when the feed itself is blocked.

Based on the [githubnext/gh-aw-cao npm skill](https://github.com/githubnext/gh-aw-cao/blob/286380d992b94eec2081af88d08a06488be3f635/.github/skills/npm/SKILL.md), under the repository's MIT license.
