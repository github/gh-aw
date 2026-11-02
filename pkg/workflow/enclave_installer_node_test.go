package workflow

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTrustedEnclaveInstallerNode(t *testing.T) {
	for _, download := range []bool{false, true} {
		lines := trustedEnclaveInstallerNodeLines("$staging_dir", download)
		text := strings.Join(lines, "\n")
		require.Equal(t, download, strings.Contains(text, "curl "))
		for _, required := range []string{
			"node-v24.22.1-linux-$node_arch",
			"6c172a9ab6718f7b801fb5a7bd486af2ee5e32758086a04b84fcce2aa77095e6",
			"dbabf7a1cbf947e6059ffad2c27b80041db97dea1e553b62afdee7f13fca336e",
			"e32e2d16081cf644f88392934288086e7662529d0d43277c34f7aad2207cfafb",
			"c8c5721679d8d1a09e9987e345d5f9a64899bb85f7a384783ba8a93e6e62348a",
			`/usr/bin/install -o root -g root -m 0500 -- "$node_source" "$installer_dir/node"`,
		} {
			require.Contains(t, text, required)
		}
		require.Less(t, strings.Index(text, `"${node_archive_digest%% *}"`), strings.Index(text, "/usr/bin/tar"))
		require.Less(t, strings.Index(text, "/usr/bin/install"), strings.Index(text, `/usr/bin/sha256sum "$installer_dir/node"`))
		require.Contains(t, lines[len(lines)-1], `"${node_binary_digest%% *}" == "$node_binary_sha256"`)
		cmd := exec.Command("bash", "-n")
		cmd.Stdin = strings.NewReader(text)
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, string(output))
	}
}
