package connectors

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorStagedFileBounds(t *testing.T) {
	policy := &BinaryRequest{Input: "file", MaxBytes: 8, Extensions: []string{".png"}, RejectContains: []string{"##vso["}}
	root := t.TempDir()
	put := func(content []byte, extension string) string {
		sum := sha256.Sum256(content)
		name := hex.EncodeToString(sum[:]) + extension
		require.NoError(t, os.WriteFile(filepath.Join(root, name), content, 0600))
		return name
	}
	name := put([]byte("12345678"), ".png")
	content, err := readConnectorFile(root, policy, map[string]any{"file": name})
	require.NoError(t, err)
	require.Equal(t, []byte("12345678"), content)
	for _, bad := range []string{
		put([]byte("123456789"), ".png"), put([]byte("##vso["), ".png"), put([]byte("safe"), ".txt"), "../escape.png",
	} {
		_, err := readConnectorFile(root, policy, map[string]any{"file": bad})
		require.Error(t, err)
	}
	hardlink := filepath.Join(root, name+".other")
	require.NoError(t, os.Link(filepath.Join(root, name), hardlink))
	_, err = readConnectorFile(root, policy, map[string]any{"file": name})
	require.Error(t, err, "hardlinks are not isolated staged files")
}
