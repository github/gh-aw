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
    https://api.github.com/repos/github/gh-aw/releases\?per_page=100\&page=*)
        page="${url##*page=}"
        source_file="$FIXTURE_RELEASES_DIR/page-$page.json"
        ;;
    https://github.com/github/gh-aw/releases/download/*/"$PLATFORM")
        tag="${url#https://github.com/github/gh-aw/releases/download/}"
        tag="${tag%/$PLATFORM}"
        [ "$tag" = "$FIXTURE_TAG" ] || { echo "unexpected release tag: $tag" >&2; exit 1; }
        source_file="$FIXTURE_BINARY"
        ;;
    https://github.com/github/gh-aw/releases/latest/download/"$PLATFORM") source_file="$FIXTURE_BINARY" ;;
    https://github.com/github/gh-aw/releases/download/*/checksums.txt)
        tag="${url#https://github.com/github/gh-aw/releases/download/}"
        tag="${tag%/checksums.txt}"
        [ "$tag" = "$FIXTURE_TAG" ] || { echo "unexpected checksum tag: $tag" >&2; exit 1; }
        source_file="$FIXTURE_CHECKSUMS"
        ;;
    https://github.com/github/gh-aw/releases/latest/download/checksums.txt) source_file="$FIXTURE_CHECKSUMS" ;;
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
        FIXTURE_TAG="v1.2.3" \
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
    grep -q "No version specified, using 'latest'" "$output" || fail "unset INPUT_VERSION did not default to latest"
    assert_no_staging_leftovers "$install_dir"
    echo "  PASS"
)

emit_release() {
    printf '{"tag_name":"%s","body":"release notes mention %s true, %s true","draft":%s,"prerelease":%s}' \
        "$1" '\"draft\":' '\"prerelease\":' "${3:-false}" "$2"
}

test_version_resolution() (
    local case_name=$1
    local case_root home fixture_bin install_dir binary_path asset checksums output requested_version expected_tag gh_log
    case_root=$(mktemp -d)
    trap 'rm -rf -- "$case_root"' EXIT
    home="$case_root/home"
    fixture_bin="$case_root/fixture-bin"
    install_dir="$home/.local/share/gh/extensions/gh-aw"
    binary_path="$install_dir/gh-aw"
    asset="$case_root/candidate"
    checksums="$case_root/checksums.txt"
    output="$case_root/installer-output"
    gh_log="$case_root/gh-arguments"
    mkdir -p "$fixture_bin" "$install_dir" "$case_root/releases"

    case "$case_name" in
        channel-page-and-semver)
            requested_version="v0"
            expected_tag="v0.10.0"
            {
                printf '['
                separator=""
                for i in $(seq 0 96); do
                    printf '%s' "$separator"
                    emit_release "v1.0.$i" false
                    separator=','
                done
                printf '%s' "$separator"
                emit_release "v0.9.0" false
                printf ','
                emit_release "v0.99.0-rc.1" true
                printf ','
                emit_release "v0.20.0" false true
                printf ']'
            } > "$case_root/releases/page-1.json"
            {
                printf '['
                emit_release "v0.10.0" false
                printf ']'
            } > "$case_root/releases/page-2.json"
            ;;
        channel-no-v1-release)
            requested_version="v1"
            expected_tag=""
            {
                printf '['
                emit_release "v0.37.18" false
                printf ','
                emit_release "v1.0.0-rc.1" true
                printf ','
                emit_release "v1.99.0" false true
                printf ','
                emit_release "v2.0.0" false
                printf ']'
            } > "$case_root/releases/page-1.json"
            ;;
        exact-tag)
            requested_version="v0.37.18"
            expected_tag="$requested_version"
            ;;
        gh-install-metadata-tag)
            requested_version="v0.37.18+build.1"
            expected_tag="$requested_version"
            ;;
        *)
            fail "unknown version-resolution test case: $case_name"
            ;;
    esac

    make_binary "$asset" "$([ -n "$expected_tag" ] && echo "$expected_tag" || echo v0.0.1)"
    {
        for platform in linux-amd64 linux-arm64 linux-arm linux-386; do
            sha256sum "$asset" | awk -v platform="$platform" '{print $1 " " platform}'
        done
    } > "$checksums"
    if [ "$case_name" = channel-no-v1-release ]; then
        make_binary "$binary_path" "v0.0.1"
        cp "$binary_path" "$case_root/old-bytes"
    fi

    cat > "$fixture_bin/curl" <<'EOF'
#!/bin/bash
set -e
url="${!#}"
if [[ "$url" == https://api.github.com/repos/github/gh-aw/releases\?* ]]; then
    has_header() {
        local expected_header=$1
        shift
        while [ "$#" -gt 0 ]; do
            if [ "$1" = "-H" ] || [ "$1" = "--header" ]; then
                shift
                [ "$1" = "$expected_header" ] && return 0
            fi
            shift
        done
        return 1
    }
    has_header "Accept: application/vnd.github+json" "$@" || { echo "missing GitHub API Accept header" >&2; exit 1; }
    has_header "User-Agent: gh-aw-install-gh-aw.sh" "$@" || { echo "missing GitHub API User-Agent header" >&2; exit 1; }
    has_header "X-GitHub-Api-Version: 2022-11-28" "$@" || { echo "missing GitHub API version header" >&2; exit 1; }
    if [ "$FIXTURE_EXPECT_GH_TOKEN" = true ]; then
        expected_authorization="Authorization: Bearer "
        expected_authorization+="$GH_TOKEN"
        has_header "$expected_authorization" "$@" || { echo "missing GH_TOKEN authorization header" >&2; exit 1; }
    fi
fi
case "$url" in
    https://api.github.com/repos/github/gh-aw/releases\?per_page=100\&page=*)
        page="${url##*page=}"
        source_file="$FIXTURE_RELEASES_DIR/page-$page.json"
        ;;
    https://github.com/github/gh-aw/releases/download/*/"$PLATFORM")
        tag="${url#https://github.com/github/gh-aw/releases/download/}"
        tag="${tag%/$PLATFORM}"
        [ "$tag" = "$FIXTURE_TAG" ] || { echo "unexpected release tag: $tag" >&2; exit 1; }
        source_file="$FIXTURE_BINARY"
        ;;
    https://github.com/github/gh-aw/releases/download/*/checksums.txt)
        tag="${url#https://github.com/github/gh-aw/releases/download/}"
        tag="${tag%/checksums.txt}"
        [ "$tag" = "$FIXTURE_TAG" ] || { echo "unexpected checksum tag: $tag" >&2; exit 1; }
        source_file="$FIXTURE_CHECKSUMS"
        ;;
    *) echo "unexpected curl URL: $url" >&2; exit 1 ;;
esac
args=("$@")
args[$(($# - 1))]="file://$source_file"
exec "$REAL_CURL" "${args[@]}"
EOF
    cat > "$fixture_bin/gh" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$GH_ARGUMENT_LOG"
if [ "$FIXTURE_GH_INSTALL_SUCCESS" = true ]; then
    case "$*" in
        "aw version")
            echo "gh aw version $FIXTURE_INSTALLED_VERSION"
            exit 0
            ;;
        "extension install "*)
            exit 0
            ;;
    esac
fi
exit 1
EOF
    cat > "$fixture_bin/sleep" <<'EOF'
#!/bin/sh
exit 0
EOF
    chmod +x "$fixture_bin/curl" "$fixture_bin/gh" "$fixture_bin/sleep"

    echo "Test: $case_name"
    if env -u GITHUB_OUTPUT \
        INPUT_VERSION="$requested_version" \
        HOME="$home" \
        PATH="$fixture_bin:$PATH" \
        REAL_CURL="$REAL_CURL" \
        PLATFORM="$PLATFORM" \
        FIXTURE_TAG="$expected_tag" \
        FIXTURE_BINARY="$asset" \
        FIXTURE_CHECKSUMS="$checksums" \
        FIXTURE_RELEASES_DIR="$case_root/releases" \
        FIXTURE_EXPECT_GH_TOKEN="$([ "$case_name" = channel-page-and-semver ] && echo true || echo false)" \
        GH_TOKEN="$([ "$case_name" = channel-page-and-semver ] && echo test-token || echo '')" \
        FIXTURE_GH_INSTALL_SUCCESS="$([ "$case_name" = gh-install-metadata-tag ] && echo true || echo false)" \
        FIXTURE_INSTALLED_VERSION="v0.37.18" \
        GH_ARGUMENT_LOG="$gh_log" \
        bash "$INSTALLER_PATH" > "$output" 2>&1; then
        [ "$case_name" != channel-no-v1-release ] || fail "v1 without a stable release unexpectedly succeeded"
    else
        [ "$case_name" = channel-no-v1-release ] || {
            cat "$output" >&2
            fail "$case_name failed"
        }
    fi

    if [ "$case_name" = channel-no-v1-release ]; then
        grep -q "No stable release found for version channel v1" "$output" ||
            fail "missing stable channel did not produce a clear error"
        cmp "$binary_path" "$case_root/old-bytes" || fail "missing channel changed the existing binary"
        [ ! -s "$gh_log" ] || fail "gh extension install ran before channel resolution"
    elif [ "$case_name" = gh-install-metadata-tag ]; then
        grep -q "Successfully installed gh-aw using gh extension install" "$output" ||
            fail "gh extension install rejected a matching version with build metadata"
        grep -q -- "--pin $expected_tag" "$gh_log" || fail "gh extension install did not use the metadata-bearing tag"
    else
        grep -q -- "--pin $expected_tag" "$gh_log" || fail "gh extension install did not use resolved/exact tag"
        [ "$("$binary_path" version)" = "gh aw version $expected_tag" ] ||
            fail "download did not install the resolved/exact tag"
    fi
    assert_no_staging_leftovers "$install_dir"
    echo "  PASS"
)

test_checksum_verification() (
    local case_name=$1
    local case_root fixture_bin home install_dir binary_path asset checksums output digest
    local expected_filename="linux-amd64" expected_error="" succeeds=false
    local tool command_path checksum_attempts binary_attempts requested_version="v1.2.3"
    local installer_args=()
    case_root=$(mktemp -d)
    trap 'rm -rf -- "$case_root"' EXIT
    fixture_bin="$case_root/bin"
    home="$case_root/home with space"
    install_dir="$home/.local/share/gh/extensions/gh-aw"
    binary_path="$install_dir/gh-aw"
    asset="$case_root/candidate"
    checksums="$case_root/checksums.txt"
    output="$case_root/output"
    mkdir -p "$fixture_bin" "$install_dir"

    # An isolated PATH prevents host tools or credentials from affecting these tests.
    for tool in awk chmod cp head ls mkdir mktemp mv rm sed seq; do
        command_path=$(command -v "$tool")
        ln -s "$command_path" "$fixture_bin/$tool"
    done
    if [ "$case_name" != no-checksum-tool ] && [ "$case_name" != skip-no-tool ]; then
        if [ "$case_name" = shasum ] || ! command -v sha256sum >/dev/null 2>&1; then
            ln -s "$(command -v shasum)" "$fixture_bin/shasum"
        else
            ln -s "$(command -v sha256sum)" "$fixture_bin/sha256sum"
        fi
    fi

    case "$case_name" in
        windows-valid|windows-missing)
            expected_filename="windows-amd64.exe"
            binary_path="$install_dir/gh-aw.exe"
            ;;
    esac
    cat > "$asset" <<'EOF'
#!/bin/sh
printf '%s\n' "$1" >> "$FIXTURE_EXECUTION_LOG"
case "$1" in
    --help) echo "gh aw help" ;;
    version) echo "gh aw version v1.2.3" ;;
esac
EOF
    if command -v sha256sum >/dev/null 2>&1; then
        digest=$(sha256sum "$asset" | awk '{print $1}')
    else
        digest=$(shasum -a 256 "$asset" | awk '{print $1}')
    fi
    printf '%s  %s\n' "$digest" "$expected_filename" > "$checksums"
    printf 'existing binary\n' > "$binary_path"
    cp "$binary_path" "$case_root/old-bytes"
    case "$case_name" in
        valid|shasum|windows-valid) succeeds=true ;;
        uppercase)
            printf '%s  %s\n' "$(printf '%s' "$digest" | tr 'a-f' 'A-F')" "$expected_filename" > "$checksums"
            succeeds=true
            ;;
        unavailable|partial-checksum)
            expected_error="Failed to download checksums file after 3 attempts"
            ;;
        empty)
            : > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        missing|windows-missing)
            printf '%s  not-%s\n' "$digest" "$expected_filename" > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        malformed)
            printf '%064d  %s\n' 0 "$expected_filename" | tr '0' 'z' > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        short-digest)
            printf 'abc123  %s\n' "$expected_filename" > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        extra-field)
            printf '%s  %s  unexpected\n' "$digest" "$expected_filename" > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        duplicate)
            printf '%s  %s\n%s  %s\n' "$digest" "$expected_filename" "$digest" "$expected_filename" > "$checksums"
            expected_error="Missing or malformed checksum"
            ;;
        mismatch)
            printf '%064d  %s\n' 0 "$expected_filename" > "$checksums"
            expected_error="Checksum verification failed"
            ;;
        no-checksum-tool)
            expected_error="Neither sha256sum nor shasum is available"
            ;;
        checksum-tool-failure)
            rm -f "$fixture_bin/sha256sum"
            printf '#!/bin/sh\nexit 1\n' > "$fixture_bin/sha256sum"
            chmod +x "$fixture_bin/sha256sum"
            expected_error="Failed to compute binary checksum"
            ;;
        checksum-tool-malformed)
            rm -f "$fixture_bin/sha256sum"
            printf '#!/bin/sh\necho invalid\n' > "$fixture_bin/sha256sum"
            chmod +x "$fixture_bin/sha256sum"
            expected_error="Checksum verification failed"
            ;;
        retry|latest-fallback) succeeds=true ;;
        skip|skip-no-tool)
            installer_args+=(--skip-checksum)
            succeeds=true
            ;;
        *) fail "unknown checksum case: $case_name" ;;
    esac
    [ "$case_name" != latest-fallback ] || requested_version="latest"

    cat > "$fixture_bin/uname" <<'EOF'
#!/bin/sh
case "$1" in
    -s)
        case "$FIXTURE_CASE" in
            windows-*) echo MINGW64_NT ;;
            *) echo Linux ;;
        esac
        ;;
    -m) echo x86_64 ;;
esac
EOF
    cat > "$fixture_bin/sleep" <<'EOF'
#!/bin/sh
printf '%s\n' "$1" >> "$FIXTURE_SLEEP_LOG"
EOF
    cat > "$fixture_bin/curl" <<'EOF'
#!/bin/bash
set -e
destination=""
while [ "$#" -gt 0 ]; do
    case "$1" in
        -o) destination="$2"; shift 2 ;;
        https://*) url="$1"; shift ;;
        *) shift ;;
    esac
done
printf '%s\n' "$url" >> "$FIXTURE_DOWNLOAD_LOG"
case "$url" in
    https://api.github.com/repos/github/gh-aw/releases/latest)
        printf '{"tag_name":"v1.2.3"}\n'
        exit 0
        ;;
    https://github.com/github/gh-aw/releases/*/checksums.txt)
        printf 'attempt\n' >> "$FIXTURE_CHECKSUM_LOG"
        case "$FIXTURE_CASE" in
            unavailable) exit 22 ;;
            partial-checksum)
                cp "$FIXTURE_CHECKSUMS" "$destination"
                exit 18
                ;;
            retry)
                if [ ! -f "$FIXTURE_RETRIED" ]; then
                    : > "$FIXTURE_RETRIED"
                    exit 22
                fi
                ;;
        esac
        cp "$FIXTURE_CHECKSUMS" "$destination"
        ;;
    https://github.com/github/gh-aw/releases/*/linux-amd64|https://github.com/github/gh-aw/releases/*/windows-amd64.exe)
        if [ "$FIXTURE_CASE" = latest-fallback ] && [[ "$url" == */latest/download/* ]]; then
            exit 22
        fi
        cp "$FIXTURE_BINARY" "$destination"
        ;;
    *) echo "unexpected URL: $url" >&2; exit 1 ;;
esac
EOF
    chmod +x "$fixture_bin/curl" "$fixture_bin/uname" "$fixture_bin/sleep"

    echo "Test: checksum-$case_name"
    if env -u INPUT_VERSION -u GITHUB_OUTPUT -u ANDROID_ROOT \
        HOME="$home" PATH="$fixture_bin" FIXTURE_CASE="$case_name" \
        FIXTURE_BINARY="$asset" FIXTURE_CHECKSUMS="$checksums" \
        FIXTURE_EXECUTION_LOG="$case_root/executions" \
        FIXTURE_DOWNLOAD_LOG="$case_root/downloads" \
        FIXTURE_CHECKSUM_LOG="$case_root/checksum-attempts" \
        FIXTURE_SLEEP_LOG="$case_root/sleeps" \
        FIXTURE_RETRIED="$case_root/retried" \
        /bin/bash "$INSTALLER_PATH" "$requested_version" "${installer_args[@]}" > "$output" 2>&1; then
        [ "$succeeds" = true ] || fail "$case_name unexpectedly succeeded"
    else
        [ "$succeeds" = false ] || { cat "$output" >&2; fail "$case_name failed"; }
    fi

    checksum_attempts=0
    [ ! -f "$case_root/checksum-attempts" ] || checksum_attempts=$(wc -l < "$case_root/checksum-attempts" | tr -d ' ')
    if [ "$succeeds" = true ]; then
        cmp "$binary_path" "$asset" || fail "$case_name did not install the candidate"
        [ -x "$binary_path" ] || fail "$case_name installed a non-executable candidate"
        printf '%s\n' --help version > "$case_root/expected-executions"
        cmp "$case_root/executions" "$case_root/expected-executions" || fail "$case_name did not verify/run the installed fixture"
        case "$case_name" in
            skip|skip-no-tool)
                [ "$checksum_attempts" -eq 0 ] || fail "explicit skip downloaded checksums"
                grep -q 'Checksum verification skipped (--skip-checksum flag used)' "$output" || fail "missing explicit skip warning"
                ;;
            *)
                grep -q 'Checksum verification passed' "$output" || fail "verification did not pass"
                [ "$checksum_attempts" -ge 1 ] || fail "checksums were not downloaded"
                ;;
        esac
    else
        cmp "$binary_path" "$case_root/old-bytes" || fail "$case_name changed existing binary"
        [ ! -e "$case_root/executions" ] || fail "$case_name executed an unverified candidate"
        grep -q "\\[ERROR\\].*$expected_error" "$output" || { cat "$output" >&2; fail "$case_name lacked an actionable error"; }
        ! grep -q 'Installation complete' "$output" || fail "$case_name reported success"
    fi
    case "$case_name" in
        unavailable|partial-checksum)
            [ "$checksum_attempts" -eq 3 ] || fail "checksum failures did not exhaust three retries"
            printf '2\n2\n' > "$case_root/expected-sleeps"
            cmp "$case_root/sleeps" "$case_root/expected-sleeps" || fail "checksum retry delays changed"
            ;;
        retry)
            [ "$checksum_attempts" -eq 2 ] || fail "transient checksum failure did not retry"
            [ "$(cat "$case_root/sleeps")" = 2 ] || fail "checksum retry delay changed"
            ;;
        latest-fallback)
            binary_attempts=$(grep -c '/latest/download/linux-amd64$' "$case_root/downloads")
            [ "$binary_attempts" -eq 3 ] || fail "latest redirect did not retry three times"
            grep -q '/download/v1.2.3/linux-amd64$' "$case_root/downloads" || fail "missing binary fallback"
            grep -q '/download/v1.2.3/checksums.txt$' "$case_root/downloads" || fail "checksums did not follow binary fallback"
            ! grep -q '/latest/download/checksums.txt$' "$case_root/downloads" || fail "checksums used the wrong release"
            printf '2\n4\n' > "$case_root/expected-sleeps"
            cmp "$case_root/sleeps" "$case_root/expected-sleeps" || fail "binary retry backoff changed"
            ;;
        no-checksum-tool)
            [ ! -e "$case_root/downloads" ] || fail "missing checksum tool did not fail before downloading"
            ;;
    esac
    assert_no_staging_leftovers "$install_dir"
    echo "  PASS"
)

bash -n "$INSTALLER_PATH"
for case_name in valid unavailable partial-checksum empty missing malformed short-digest extra-field duplicate mismatch no-checksum-tool checksum-tool-failure checksum-tool-malformed retry latest-fallback uppercase shasum skip skip-no-tool windows-valid windows-missing; do
    if [ "$case_name" = shasum ] && ! command -v shasum >/dev/null 2>&1; then
        echo "Skipping shasum fallback test: shasum is unavailable on this host."
        continue
    fi
    test_checksum_verification "$case_name"
done
if command -v pwsh >/dev/null 2>&1; then
    POWERSHELL_INSTALLER_PATH="$(cd "$(dirname "$INSTALLER_PATH")" && pwd)/$(basename "${INSTALLER_PATH%.sh}.ps1")"
    pwsh -NoProfile -File "$SCRIPT_DIR/test-install-script.ps1" -InstallerPath "$POWERSHELL_INSTALLER_PATH"
fi

if [ "$(uname -s)" != Linux ]; then
    echo "Skipping Bash behavioral installer tests outside Linux."
    exit 0
fi

for case_name in dangling-symlink live-symlink partial-download checksum-mismatch invalid-binary directory-conflict; do
    test_manual_binary_replacement "$case_name"
done
for case_name in channel-page-and-semver channel-no-v1-release exact-tag gh-install-metadata-tag; do
    test_version_resolution "$case_name"
done

echo "All installer behavior tests passed."
