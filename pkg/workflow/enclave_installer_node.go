package workflow

import "fmt"

func trustedEnclaveInstallerNodeLines(stagingDir string, download bool) []string {
	lines := []string{
		`          case "$(/usr/bin/uname -m)" in`,
		`            x86_64) node_arch=x64; node_archive_sha256=6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6; node_binary_sha256=e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb ;;`,
		`            aarch64) node_arch=arm64; node_archive_sha256=dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e; node_binary_sha256=c8c5721679d8d1a09e9987e345d5f9a64899bb85f7a384783ba8a93e6e62348a ;;`,
		`            *) echo "Unsupported trusted installer Node architecture" >&2; exit 1 ;;`,
		`          esac`,
		fmt.Sprintf(`          node_archive="%s/node.tar.xz"`, stagingDir),
		fmt.Sprintf(`          node_source="%s/node"`, stagingDir),
	}
	if download {
		lines = append(lines,
			`          curl --proto '=https' --proto-redir '=https' -fsSL --retry 8 --retry-max-time 180 --connect-timeout 15 --max-time 120 --max-filesize 134217728 \`,
			`            "https://nodejs.org/dist/v24.22.1/node-v24.22.1-linux-$node_arch.tar.xz" -o "$node_archive"`,
		)
	}
	return append(lines,
		`          node_archive_digest=$(/usr/bin/sha256sum "$node_archive")`,
		`          [[ "${node_archive_digest%% *}" == "$node_archive_sha256" ]]`,
		`          /usr/bin/tar -xJOf "$node_archive" "node-v24.22.1-linux-$node_arch/bin/node" > "$node_source"`,
		`          /usr/bin/sudo -n /usr/bin/install -o root -g root -m 0500 -- "$node_source" "$installer_dir/node"`,
		`          node_binary_digest=$(/usr/bin/sudo -n /usr/bin/sha256sum "$installer_dir/node")`,
		`          [[ "${node_binary_digest%% *}" == "$node_binary_sha256" ]]`,
	)
}
