#!/bin/bash
set +o histexpand
set -e

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
INSTALLER_PATH="${1:-$PROJECT_ROOT/install-gh-aw.sh}"
REAL_CURL="$(command -v curl)"

fail() {
    echo "FAIL: $1" >&2
    exit 1
}

# Derive the installer's expected platform asset name the same way install-gh-aw.sh does,
# so the fixture curl works on every Linux architecture the installer supports.
case "$(uname -m)" in
    x86_64) ARCH_NAME="amd64" ;;
    aarch64|arm64) ARCH_NAME="arm64" ;;
    armv7l|armv6l) ARCH_NAME="arm" ;;
    i386|i686) ARCH_NAME="386" ;;
    *) fail "Unsupported test architecture: $(uname -m)" ;;
esac
PLATFORM="linux-${ARCH_NAME}"

make_binary() {
    local path=$1
    local version=$2
    local invalid=${3:-false}

    cat > "$path" <<EOF
#!/bin/sh
case "\$1" in
    --help)
        [ "$invalid" = true ] && exit 1
        echo "gh aw help"
        ;;
    version) echo "gh aw version $version" ;;
esac
EOF
    chmod +x "$path"
}

assert_no_staging_leftovers() {
    local install_dir=$1
    if find "$install_dir" -maxdepth 1 \( -name '.gh-aw-install.*' -o -name 'checksums.txt' \) -print | grep -q .; then
        fail "staging leftovers found in $install_dir"
    fi
}

test_manual_binary_replacement() (
    local case_name=$1
    local case_root home fixture_bin install_dir binary_path asset checksums old_binary target output
    case_root=$(mktemp -d)
    trap 'rm -rf -- "$case_root"' EXIT
    home="$case_root/home with space"
    fixture_bin="$case_root/fixture-bin"
    install_dir="$home/.local/share/gh/extensions/gh-aw"
    binary_path="$install_dir/gh-aw"
    asset="$case_root/candidate"
    checksums="$case_root/checksums.txt"
    old_binary="$case_root/old-gh-aw"
    target="$case_root/external/gh-aw"
    output="$case_root/installer-output"
    mkdir -p "$fixture_bin" "$install_dir"

    make_binary "$asset" "v1.2.3" "$([ "$case_name" = invalid-binary ] && echo true || echo false)"
    {
        for platform in linux-amd64 linux-arm64 linux-arm linux-386; do
            if [ "$case_name" = checksum-mismatch ] && [ "$platform" = "$PLATFORM" ]; then
                printf '%064d %s\n' 0 "$platform"
            else
                sha256sum "$asset" | awk -v platform="$platform" '{print $1 " " platform}'
            fi
        done
    } > "$checksums"
    printf '{"tag_name":"v1.2.3"}\n' > "$case_root/latest.json"

    case "$case_name" in
        dangling-symlink)
            ln -s "$case_root/missing-parent/gh-aw" "$binary_path"
            ;;
        live-symlink|checksum-mismatch)
            mkdir -p "$(dirname "$target")"
            make_binary "$target" "v0.0.1"
            ln -s "$target" "$binary_path"
            cp "$target" "$case_root/old-bytes"
            ;;
        partial-download|invalid-binary)
            make_binary "$binary_path" "v0.0.1"
            cp "$binary_path" "$case_root/old-bytes"
            ;;
        directory-conflict)
            mkdir "$binary_path"
            printf 'sentinel\n' > "$binary_path/sentinel"
            ;;
        *)
            fail "unknown test case: $case_name"
            ;;
    esac

    cat > "$fixture_bin/curl" <<'EOF'
#!/bin/bash
set -e
url="${!#}"
if [ "${FIXTURE_PARTIAL_DOWNLOAD:-}" = true ]; then
    for ((i = 1; i <= $#; i++)); do
        if [ "${!i}" = "-o" ]; then
            next=$((i + 1))
            printf 'partial\n' > "${!next}"
            break
        fi
    done
    exit 18
fi
case "$url" in
    https://api.github.com/repos/github/gh-aw/releases/latest) source_file="$FIXTURE_LATEST" ;;
    "https://github.com/github/gh-aw/releases/latest/download/$PLATFORM"|"https://github.com/github/gh-aw/releases/download/v1.2.3/$PLATFORM") source_file="$FIXTURE_BINARY" ;;
    https://github.com/github/gh-aw/releases/latest/download/checksums.txt|https://github.com/github/gh-aw/releases/download/v1.2.3/checksums.txt) source_file="$FIXTURE_CHECKSUMS" ;;
    *) echo "unexpected curl URL: $url" >&2; exit 1 ;;
esac
args=("$@")
args[$(($# - 1))]="file://$source_file"
exec "$REAL_CURL" "${args[@]}"
EOF
    cat > "$fixture_bin/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF
    chmod +x "$fixture_bin/curl" "$fixture_bin/sleep"

    echo "Test: $case_name"
    if env -u INPUT_VERSION -u GITHUB_OUTPUT \
        HOME="$home" \
        PATH="$fixture_bin:$PATH" \
        REAL_CURL="$REAL_CURL" \
        PLATFORM="$PLATFORM" \
        FIXTURE_LATEST="$case_root/latest.json" \
        FIXTURE_BINARY="$asset" \
        FIXTURE_CHECKSUMS="$checksums" \
        FIXTURE_PARTIAL_DOWNLOAD="$([ "$case_name" = partial-download ] && echo true || echo false)" \
        bash < "$INSTALLER_PATH" > "$output" 2>&1; then
        case "$case_name" in
            partial-download|checksum-mismatch|invalid-binary|directory-conflict)
                fail "$case_name unexpectedly succeeded"
                ;;
        esac
    else
        case "$case_name" in
            dangling-symlink|live-symlink)
                cat "$output" >&2
                fail "$case_name failed"
                ;;
        esac
    fi

    case "$case_name" in
        dangling-symlink)
            [ -f "$binary_path" ] && [ ! -L "$binary_path" ] && [ -x "$binary_path" ] ||
                fail "dangling symlink was not replaced with an executable"
            [ ! -e "$case_root/missing-parent" ] || fail "dangling link target parent was created"
            [ "$("$binary_path" version)" = "gh aw version v1.2.3" ] || fail "new binary does not run"
            ;;
        live-symlink)
            [ ! -L "$binary_path" ] || fail "live symlink was not replaced"
            [ "$("$binary_path" version)" = "gh aw version v1.2.3" ] || fail "new binary does not run"
            cmp "$target" "$case_root/old-bytes" || fail "external symlink target changed"
            [ "$("$target" version)" = "gh aw version v0.0.1" ] || fail "external binary behavior changed"
            ;;
        partial-download|invalid-binary)
            cmp "$binary_path" "$case_root/old-bytes" || fail "existing binary changed"
            [ "$("$binary_path" version)" = "gh aw version v0.0.1" ] || fail "existing binary no longer runs"
            ;;
        checksum-mismatch)
            [ -L "$binary_path" ] || fail "existing symlink changed"
            [ "$(readlink "$binary_path")" = "$target" ] || fail "existing symlink target changed"
            cmp "$target" "$case_root/old-bytes" || fail "external target changed"
            ;;
        directory-conflict)
            [ -d "$binary_path" ] && [ "$(cat "$binary_path/sentinel")" = sentinel ] ||
                fail "directory destination changed"
            [ ! -e "$binary_path/gh-aw" ] || fail "candidate was moved into destination directory"
            ;;
    esac
    assert_no_staging_leftovers "$install_dir"
    echo "  PASS"
)

if [ "$(uname -s)" != Linux ]; then
    echo "Skipping behavioral installer tests outside Linux."
    exit 0
fi

bash -n "$INSTALLER_PATH"
for case_name in dangling-symlink live-symlink partial-download checksum-mismatch invalid-binary directory-conflict; do
    test_manual_binary_replacement "$case_name"
done

echo "All installer behavior tests passed."
