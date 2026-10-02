# Setup gh-aw CLI Action

This GitHub Action installs the `gh-aw` CLI extension using the latest stable release, an exact release tag, or a major-version channel.

## Features

- ✅ **Flexible versions**: Supports latest, exact release tags, and major-version channels
- ✅ **Checksum verification**: Validates SHA256 checksums for downloaded binaries
- ✅ **Automatic fallback**: Tries `gh extension install` first, falls back to direct download if needed
- ✅ **Cross-platform**: Works on Linux, macOS, Windows, and FreeBSD
- ✅ **Multi-architecture**: Supports amd64, arm64, 386, and arm architectures

## Usage

### Install Latest Stable Release

Omit `version` to install the latest stable release:

```yaml
- uses: github/gh-aw/actions/setup-cli@main
```

### Install a Major-Version Channel

Use a `vN` channel to install the highest stable SemVer release in that major:

```yaml
- uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v1
```

Major-version channels are preparatory until stable releases exist for those majors. There is no stable `v1` release yet, so `v1` currently fails with a clear error. The `v0` channel can resolve to the existing stable `v0.x` release line.

### Install an Exact Release Tag

```yaml
- name: Install gh-aw
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18
```

### Complete Workflow Example

```yaml
name: Test gh-aw

on: [push]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - name: Checkout repository
        uses: actions/checkout@v7
      
      - name: Install gh-aw
        uses: github/gh-aw/actions/setup-cli@main
        with:
          version: v0.37.18
      
      - name: Verify installation
        run: |
          gh aw version
          gh aw --help
```

## Inputs

### `version` (optional)

The version selector for gh-aw.

- **Default**: `latest`
- **Latest**: Selects the latest stable release overall
- **Major-version channel**: `vN`, e.g. `v0` or `v1`, selects the highest stable SemVer release in that major
- **Release tag**: e.g., `v0.37.18`, installs that exact release

Prereleases are not selected by `latest` or a major-version channel. A channel without a stable release fails; for example, `v1` has no matching release yet.

### `github-token` (optional)

GitHub token for authentication. Used for both `gh` CLI operations and GitHub API calls.

- **Default**: `${{ github.token }}` (automatically provided by GitHub Actions)
- **Required**: No (uses the default GITHUB_TOKEN automatically)
- **When to override**: Only needed in special cases like using a PAT with additional permissions

## Outputs

### `installed-version`

The version tag that was actually installed.

## How It Works

1. **Version resolution**: Resolves `latest` or a `vN` channel to a stable release tag; exact tags are kept unchanged
2. **Primary installation method**: Attempts to install using `gh extension install github/gh-aw`
3. **Fallback method**: If primary method fails, downloads the binary directly from GitHub releases
4. **Checksum verification**: Downloads and verifies SHA256 checksums for the binary
5. **Binary verification**: Ensures the installed binary works correctly

## Requirements

- GitHub CLI (`gh`) must be available (pre-installed on GitHub Actions runners)
- `curl` must be available (pre-installed on GitHub Actions runners)

## Error Handling

The action will fail if:

- The specified release tag doesn't exist
- A major-version channel has no stable release
- The binary download fails
- The downloaded binary is not executable or doesn't work

## Platform Support

| OS | Architectures |
|----|---------------|
| Linux | amd64, arm64, 386, arm |
| macOS | amd64, arm64 |
| FreeBSD | amd64, arm64, 386 |
| Windows | amd64, arm64, 386 |

## Examples

### Install Specific Version

```yaml
- uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18
```

### Install Latest Stable Release

```yaml
- uses: github/gh-aw/actions/setup-cli@main
```

### Install a Major-Version Channel

```yaml
- uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v1
```

`v1` is an example of a forward-looking channel and will work once a stable v1 release is published. Use `v0` for the existing stable v0 release line.

### Use Output

```yaml
- name: Install gh-aw
  id: install
  uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18

- name: Show installed version
  run: |
    echo "Installed version: ${{ steps.install.outputs.installed-version }}"
```

### Matrix Testing Across Versions

```yaml
jobs:
  test:
    strategy:
      matrix:
        version: [v0.37.18, v0.37.17, v0.37.16]
    runs-on: ubuntu-latest
    steps:
      - uses: github/gh-aw/actions/setup-cli@main
        with:
          version: ${{ matrix.version }}
      
      - name: Test workflow compilation
        run: gh aw compile workflow.md
```

### Using a Custom GitHub Token

```yaml
- uses: github/gh-aw/actions/setup-cli@main
  with:
    version: v0.37.18
    github-token: ${{ secrets.MY_CUSTOM_TOKEN }}
```

**Note**: In most cases, you don't need to specify the `github-token` input. The action automatically uses `${{ github.token }}` which is provided by GitHub Actions.

## Troubleshooting

### "Release X does not exist"

Verify the release exists at: https://github.com/github/gh-aw/releases

### "Release X does not exist"

Verify the release exists at: https://github.com/github/gh-aw/releases

### "gh extension install failed"

The action automatically falls back to direct download when `gh extension install` fails. Check the action logs for details.

## Development

This action is part of the gh-aw repository. The `install.sh` and `install.ps1` scripts are generated during the build process by copying from the root `install-gh-aw.sh` and `install-gh-aw.ps1` files.

### Building

The installation scripts are copied during the build process:

```bash
make build  # Copies install-gh-aw.sh to actions/setup-cli/install.sh
            # and install-gh-aw.ps1 to actions/setup-cli/install.ps1
```

The generated `install.sh` and `install.ps1` files are marked as `linguist-generated=true` in `.gitattributes`.

## License

This action is part of the gh-aw project and follows the same license terms.
