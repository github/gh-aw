//go:build !integration

package parser

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFrontmatterHashIncludesBuiltinConnectorSnapshot(t *testing.T) {
	connectorPath := BuiltinPathPrefix + "connectors/hash-test.md"
	RegisterBuiltinVirtualFile(connectorPath, []byte("---\ndescription: initial\n---\nInitial connector body.\n"))

	workflowPath := filepath.Join(t.TempDir(), "workflow.md")
	workflowContent := "---\nengine: copilot\nimports:\n  - connector:hash-test\n---\nReview.\n"
	require.NoError(t, os.WriteFile(workflowPath, []byte(workflowContent), 0600))

	connectorContent := "---\ndescription: initial\n---\nInitial connector body.\n"
	reader := func(filePath string) ([]byte, error) {
		if filePath == workflowPath {
			return []byte(workflowContent), nil
		}
		if filePath == connectorPath {
			return []byte(connectorContent), nil
		}
		return nil, os.ErrNotExist
	}

	frontmatterHash, err := ComputeFrontmatterHashFromFileWithReader(workflowPath, nil, reader)
	require.NoError(t, err)
	bodyHash, err := ComputeBodyHashFromParsedContent("Review.", "engine: copilot\nimports:\n  - connector:hash-test", filepath.Dir(workflowPath), reader)
	require.NoError(t, err)

	connectorContent = "---\ndescription: changed\n---\nInitial connector body.\n"
	changedFrontmatterHash, err := ComputeFrontmatterHashFromFileWithReader(workflowPath, nil, reader)
	require.NoError(t, err)
	require.NotEqual(t, frontmatterHash, changedFrontmatterHash)

	connectorContent = "---\ndescription: initial\n---\nChanged connector body.\n"
	changedBodyHash, err := ComputeBodyHashFromParsedContent("Review.", "engine: copilot\nimports:\n  - connector:hash-test", filepath.Dir(workflowPath), reader)
	require.NoError(t, err)
	require.NotEqual(t, bodyHash, changedBodyHash)
}
