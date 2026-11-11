//go:build !integration

package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func packageOwnershipRemovalFixture(t *testing.T) (string, *packageOwnershipRecord) {
	t.Helper()
	parent := testutil.TempDir(t, "package-removal-*")
	root := filepath.Join(parent, "repo")
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".git"), 0o755))
	t.Chdir(root)
	require.NoError(t, os.MkdirAll(filepath.Join(root, ".github", "aw"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(root, ".github", "aw", "owned.txt"), []byte("installed\n"), 0o644))
	return root, &packageOwnershipRecord{
		SchemaVersion: packageOwnershipSchemaVersion,
		Package:       "owner/repo",
		Source:        "owner/repo@v1",
		Files: []packageOwnershipFileEntry{{
			Source:      "owned.txt",
			Destination: ".github/aw/owned.txt",
			SHA256:      sha256Bytes([]byte("installed\n")),
		}},
	}
}

func writeRemovalOwnershipRecord(t *testing.T, root string, record *packageOwnershipRecord) (string, []byte) {
	t.Helper()
	recordPath := packageOwnershipRecordPath(root, "owner/repo")
	require.NoError(t, os.MkdirAll(filepath.Dir(recordPath), 0o755))
	data, err := json.Marshal(record)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(recordPath, data, 0o644))
	return recordPath, data
}

func TestRemovePackageOwnedFilesIfUnusedRejectsUnsafeDestinations(t *testing.T) {
	tests := []struct {
		name        string
		destination string
	}{
		{"parent traversal", "../victim"},
		{"nested traversal", ".github/../../victim"},
		{"symlinked ancestor", "escape/victim"},
		{"symlinked file", "victim-link"},
		{"empty destination", ""},
		{"repository root", "."},
		{"absolute path", "/victim"},
		{"windows absolute path", `C:\victim`},
		{"null character", "bad\x00path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, record := packageOwnershipRemovalFixture(t)
			victim := filepath.Join(filepath.Dir(root), "victim")
			const content = "external file\n"
			require.NoError(t, os.WriteFile(victim, []byte(content), 0o644))
			require.NoError(t, os.Symlink(filepath.Dir(root), filepath.Join(root, "escape")))
			require.NoError(t, os.Symlink(victim, filepath.Join(root, "victim-link")))
			record.Files = append(record.Files, packageOwnershipFileEntry{
				Source:      "victim",
				Destination: tt.destination,
				SHA256:      sha256Bytes([]byte(content)),
			})
			recordPath, original := writeRemovalOwnershipRecord(t, root, record)

			err := removePackageOwnedFilesIfUnused("owner/repo")

			require.ErrorContains(t, err, "invalid destination")
			got, err := os.ReadFile(victim)
			require.NoError(t, err)
			assert.Equal(t, content, string(got), "matching hashes must not authorize external deletion")
			assert.FileExists(t, filepath.Join(root, ".github", "aw", "owned.txt"), "validate every entry before deleting any file")
			got, err = os.ReadFile(recordPath)
			require.NoError(t, err)
			assert.Equal(t, original, got, "rejected records must not be rewritten")
		})
	}
}

func TestRemovePackageOwnedFilesIfUnusedRejectsMalformedRecords(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*packageOwnershipRecord)
		raw    string
		want   string
	}{
		{name: "invalid JSON", raw: "{", want: "failed to read package ownership record"},
		{name: "null record", raw: "null", want: "unsupported schema version"},
		{name: "missing fields", raw: "{}", want: "unsupported schema version"},
		{name: "unsupported schema", mutate: func(r *packageOwnershipRecord) { r.SchemaVersion++ }, want: "unsupported schema version"},
		{name: "wrong package", mutate: func(r *packageOwnershipRecord) { r.Package = "other/repo" }, want: "does not match"},
		{name: "missing files", mutate: func(r *packageOwnershipRecord) { r.Files = nil }, want: "files must be an array"},
		{name: "empty entry", mutate: func(r *packageOwnershipRecord) { r.Files = append(r.Files, packageOwnershipFileEntry{}) }, want: "invalid destination"},
		{name: "missing hash", mutate: func(r *packageOwnershipRecord) { r.Files[0].SHA256 = "" }, want: "invalid SHA256"},
		{name: "short hash", mutate: func(r *packageOwnershipRecord) { r.Files[0].SHA256 = "abcd" }, want: "invalid SHA256"},
		{name: "nonhex hash", mutate: func(r *packageOwnershipRecord) { r.Files[0].SHA256 = "not-a-hash" }, want: "invalid SHA256"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			root, record := packageOwnershipRemovalFixture(t)
			if tt.mutate != nil {
				tt.mutate(record)
			}
			recordPath, original := writeRemovalOwnershipRecord(t, root, record)
			if tt.raw != "" {
				original = []byte(tt.raw)
				require.NoError(t, os.WriteFile(recordPath, original, 0o644))
			}

			require.ErrorContains(t, removePackageOwnedFilesIfUnused("owner/repo"), tt.want)

			assert.FileExists(t, filepath.Join(root, ".github", "aw", "owned.txt"))
			got, err := os.ReadFile(recordPath)
			require.NoError(t, err)
			assert.Equal(t, original, got)
		})
	}
}

func TestRemovePackageOwnedFilesIfUnusedPreservesCleanupBehavior(t *testing.T) {
	for _, modified := range []bool{false, true} {
		name := "unchanged owned file"
		if modified {
			name = "locally modified owned file"
		}
		t.Run(name, func(t *testing.T) {
			root, record := packageOwnershipRemovalFixture(t)
			record.Files = append(record.Files, packageOwnershipFileEntry{
				Source:      "workflows/removed.md",
				Destination: ".github/workflows/removed.md",
				SHA256:      sha256Bytes([]byte("removed workflow\n")),
			})
			recordPath, _ := writeRemovalOwnershipRecord(t, root, record)
			ownedPath := filepath.Join(root, ".github", "aw", "owned.txt")
			if modified {
				require.NoError(t, os.WriteFile(ownedPath, []byte("local edits\n"), 0o644))
			}

			require.NoError(t, removePackageOwnedFilesIfUnused("owner/repo"))

			if !modified {
				assert.NoFileExists(t, ownedPath)
				assert.NoFileExists(t, recordPath)
				return
			}
			got, err := os.ReadFile(ownedPath)
			require.NoError(t, err)
			assert.Equal(t, "local edits\n", string(got))
			kept, err := readPackageOwnershipRecord(recordPath)
			require.NoError(t, err)
			assert.Equal(t, record.Files[:1], kept.Files)
		})
	}
}

func TestRemovePackageOwnedFilesIfUnusedRejectsExternalRecord(t *testing.T) {
	root, record := packageOwnershipRemovalFixture(t)
	recordPath, original := writeRemovalOwnershipRecord(t, root, record)
	externalRecord := filepath.Join(filepath.Dir(root), "ownership.json")
	require.NoError(t, os.Rename(recordPath, externalRecord))
	require.NoError(t, os.Symlink(externalRecord, recordPath))

	require.ErrorContains(t, removePackageOwnedFilesIfUnused("owner/repo"), "invalid package ownership record path")

	got, err := os.ReadFile(externalRecord)
	require.NoError(t, err)
	assert.Equal(t, original, got)
	assert.FileExists(t, filepath.Join(root, ".github", "aw", "owned.txt"))
}
