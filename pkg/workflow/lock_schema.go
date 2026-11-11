package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var lockSchemaLog = logger.New("workflow:lock_schema")

var (
	lockMetadataPattern = regexp.MustCompile(`#\s*gh-aw-metadata:\s*(\{.+\})`)
	lockHashPattern     = regexp.MustCompile(`#\s*frontmatter-hash:\s*([0-9a-f]{64})`)
)

// LockSchemaVersion represents a lock file schema version
type LockSchemaVersion string

const (
	// LockSchemaV1 is the legacy lock file schema version (no strict field)
	LockSchemaV1 LockSchemaVersion = "v1"
	// LockSchemaV2 is the lock file schema version that adds the strict field
	LockSchemaV2 LockSchemaVersion = "v2"
	// LockSchemaV3 is the lock file schema version that adds agent id/model and detection agent id/model fields
	LockSchemaV3 LockSchemaVersion = "v3"
	// LockSchemaV4 is the current lock file schema version (adds body_hash for full stale-check coverage)
	LockSchemaV4 LockSchemaVersion = "v4"

	// CurrentSetupRuntimeContractVersion identifies the compiler/setup runtime
	// contract required to execute the generated activation metadata.
	CurrentSetupRuntimeContractVersion = 1
)

// LockMetadata represents the structured metadata embedded in lock files
type LockMetadata struct {
	SchemaVersion               LockSchemaVersion `json:"schema_version"`
	FrontmatterHash             string            `json:"frontmatter_hash,omitempty"`
	BodyHash                    string            `json:"body_hash,omitempty"`
	StopTime                    string            `json:"stop_time,omitempty"`
	CompilerVersion             string            `json:"compiler_version,omitempty"`
	SetupRuntimeContractVersion int               `json:"setup_runtime_contract_version,omitempty"`
	Docs                        string            `json:"docs,omitempty"`
	Strict                      bool              `json:"strict,omitempty"`
	DryRun                      bool              `json:"dry_run,omitempty"`
	// AgentMetadataInfo is embedded so agent fields are declared once and
	// serialized inline in the lock metadata JSON.
	AgentMetadataInfo
}

// AgentMetadataInfo holds agent and detection agent information for embedding in lock file metadata
type AgentMetadataInfo struct {
	EngineBaseURLCustomized bool              `json:"engine_base_url_customized,omitempty"`
	AgentID                 string            `json:"agent_id,omitempty"`
	AgentModel              string            `json:"agent_model,omitempty"`
	DetectionAgentID        string            `json:"detection_agent_id,omitempty"`
	DetectionAgentModel     string            `json:"detection_agent_model,omitempty"`
	EngineVersions          map[string]string `json:"engine_versions,omitempty"`
	AgentImageRunner        string            `json:"agent_image_runner,omitempty"`
}

// SupportedSchemaVersions lists all schema versions this build can consume
var SupportedSchemaVersions = []LockSchemaVersion{
	LockSchemaV1,
	LockSchemaV2,
	LockSchemaV3,
	LockSchemaV4,
}

// IsSchemaVersionSupported checks if a schema version is supported
func IsSchemaVersionSupported(version LockSchemaVersion) bool {
	return slices.Contains(SupportedSchemaVersions, version)
}

// ExtractMetadataFromLockFile extracts structured metadata from a lock file's comment header
// Returns metadata and whether legacy format (no metadata) was detected
func ExtractMetadataFromLockFile(content string) (*LockMetadata, bool, error) {
	// Look for JSON metadata in comments (format: # gh-aw-metadata: {...})
	// Use .+ to capture to end of line since metadata is single-line JSON
	matches := lockMetadataPattern.FindStringSubmatch(content)

	if len(matches) >= 2 {
		jsonStr := matches[1] //nolint:uncheckedsliceindex // The length check above guarantees this capture exists.
		var metadata LockMetadata
		decoder := json.NewDecoder(strings.NewReader(jsonStr))
		if err := decoder.Decode(&metadata); err != nil {
			return nil, false, fmt.Errorf("lock metadata JSON should be a single valid JSON object; recompile the workflow with gh aw compile to regenerate the lock file: %w", err)
		}
		if err := decoder.Decode(new(any)); err != io.EOF {
			if err == nil {
				err = errors.New("unexpected trailing JSON value")
			}
			return nil, false, fmt.Errorf("lock metadata JSON should be a single valid JSON object; recompile the workflow with gh aw compile to regenerate the lock file: %w", err)
		}
		lockSchemaLog.Printf("Extracted metadata from lock file: schema=%s", metadata.SchemaVersion)
		return &metadata, false, nil
	}

	// Legacy format: look for frontmatter-hash without JSON metadata
	if matches := lockHashPattern.FindStringSubmatch(content); len(matches) >= 2 {
		lockSchemaLog.Print("Legacy lock file detected (no schema version)")
		// Return a minimal metadata struct with just the hash for legacy files
		return &LockMetadata{FrontmatterHash: matches[1]}, true, nil //nolint:uncheckedsliceindex // The length check guarantees this capture exists.
	}

	// No metadata found at all
	return nil, false, nil
}

// formatSupportedVersions formats the list of supported versions for error messages
func formatSupportedVersions() string {
	versions := make([]string, len(SupportedSchemaVersions))
	for i, v := range SupportedSchemaVersions {
		versions[i] = string(v) //nolint:uncheckedsliceindex // versions has the same length as SupportedSchemaVersions.
	}
	return strings.Join(versions, ", ")
}

// LockHashInfo groups the hash fields written into lock metadata.
// Passing a struct rather than individual string args makes the signature
// stable against future hash additions.
type LockHashInfo struct {
	FrontmatterHash string
	BodyHash        string
}

// GenerateLockMetadata creates a LockMetadata struct for embedding in lock files
// For release builds, the compiler version is included in the metadata
func GenerateLockMetadata(hashInfo LockHashInfo, stopTime string, strict bool, agentInfo AgentMetadataInfo) *LockMetadata {
	lockSchemaLog.Printf("Generating lock metadata: schema=%s, strict=%t, hasStopTime=%t, hasBodyHash=%t", LockSchemaV4, strict, stopTime != "", hashInfo.BodyHash != "")

	metadata := &LockMetadata{
		SchemaVersion:               LockSchemaV4,
		FrontmatterHash:             hashInfo.FrontmatterHash,
		BodyHash:                    hashInfo.BodyHash,
		StopTime:                    stopTime,
		SetupRuntimeContractVersion: CurrentSetupRuntimeContractVersion,
		Strict:                      strict,
		AgentMetadataInfo:           agentInfo,
	}

	// Include compiler version only for release builds
	if IsRelease() {
		metadata.CompilerVersion = GetVersion()
		lockSchemaLog.Printf("Including compiler version in lock metadata: %s", metadata.CompilerVersion)
	}

	return metadata
}

// ToJSON converts LockMetadata to a compact JSON string for embedding in comments
func (m *LockMetadata) ToJSON() (string, error) {
	bytes, err := json.Marshal(m)
	if err != nil {
		return "", fmt.Errorf("lock metadata should contain JSON-serializable values; check metadata field values before writing the lock file: %w", err)
	}
	return string(bytes), nil
}
