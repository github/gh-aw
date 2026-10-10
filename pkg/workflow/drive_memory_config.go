package workflow

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

const (
	defaultDriveMemoryDir       = "/tmp/gh-aw/drive-memory"
	driveMemoryDirPrefix        = "/tmp/gh-aw/drive-memory-"
	defaultDriveMemoryMountPath = ".gh-aw-drive-memory"
	driveMemoryMountPathPrefix  = ".gh-aw-drive-memory-"
	// defaultDriveMemoryDiskSize is the suggested drive size when creating a new drive.
	defaultDriveMemoryDiskSize = "100M"
)

// driveDiskSizePattern matches the disk sizes accepted by the GitHub Drives checkout
// action: a number with an optional K, M, G, or T suffix (e.g. "100M").
var driveDiskSizePattern = regexp.MustCompile(`^[0-9]+[KMGT]?$`)

// normalizeDriveDiskSize trims surrounding whitespace and uppercases the size
// suffix so common variants (e.g. "100m", " 100M ") match the upper-case-only
// format required by the GitHub Drives checkout action.
func normalizeDriveDiskSize(diskSize string) string {
	return strings.ToUpper(strings.TrimSpace(diskSize))
}

// DriveMemoryConfig holds configuration for drive-memory functionality.
type DriveMemoryConfig struct {
	Drives []DriveMemoryEntry `yaml:"drives,omitempty"`
}

// DriveMemoryEntry represents one persistent GitHub Drive.
type DriveMemoryEntry struct {
	ID                string                  `yaml:"id"`
	DriveName         string                  `yaml:"drive-name,omitempty"`
	Description       string                  `yaml:"description,omitempty"`
	DiskSize          string                  `yaml:"disk-size,omitempty"`
	Prefetch          bool                    `yaml:"prefetch,omitempty"`
	RestoreOnly       bool                    `yaml:"restore-only,omitempty"`
	AllowedExtensions []string                `yaml:"allowed-extensions,omitempty"`
	Validation        *MemoryValidationConfig `yaml:"validation,omitempty"`
}

func driveMemoryDirFor(id string) string {
	if id == "" || id == "default" {
		return defaultDriveMemoryDir
	}
	if !isValidCacheID(id) {
		return driveMemoryDirPrefix + memoryValidationStepID("invalid", id)
	}
	return driveMemoryDirPrefix + id
}

func driveMemoryMountPathFor(id string) string {
	if id == "" || id == "default" {
		return defaultDriveMemoryMountPath
	}
	if !isValidCacheID(id) {
		return driveMemoryMountPathPrefix + memoryValidationStepID("invalid", id)
	}
	return driveMemoryMountPathPrefix + id
}

func driveMemoryValidationStepID(id string) string {
	return memoryValidationStepID("validate_drive_memory", id)
}

func driveMemoryBaselineFilenameFor(id string) string {
	safeID := id
	if !isValidCacheID(safeID) {
		safeID = memoryValidationStepID("invalid", id)
	}
	return fmt.Sprintf("drive-memory-baseline-%s.sha256", safeID)
}

func driveMemoryBaselinePathFor(id string) string {
	return tmpSafePrefix + driveMemoryBaselineFilenameFor(id)
}

func driveHasValidationStep(drive DriveMemoryEntry) bool {
	return len(drive.AllowedExtensions) > 0 || drive.Validation != nil
}

func defaultDriveMemoryEntries() []DriveMemoryEntry {
	return []DriveMemoryEntry{{
		ID:                "default",
		DriveName:         "default",
		AllowedExtensions: constants.DefaultAllowedMemoryExtensions,
	}}
}

func parseDriveMemoryEntry(raw map[string]any, defaultID string) (DriveMemoryEntry, error) {
	entry := DriveMemoryEntry{
		ID:        defaultID,
		DriveName: defaultID,
	}
	if id, ok := raw["id"].(string); ok {
		if !isValidCacheID(id) {
			return entry, fmt.Errorf("invalid drive-memory id %q: must contain only letters, digits, underscores, or hyphens (1-64 characters)", id)
		}
		entry.ID = id
		entry.DriveName = id
	}
	if driveName, ok := raw["drive-name"].(string); ok && driveName != "" {
		if !isValidCacheID(driveName) {
			return entry, fmt.Errorf("invalid drive-memory drive-name %q: must contain only letters, digits, underscores, or hyphens (1-64 characters)", driveName)
		}
		entry.DriveName = driveName
	}
	if description, ok := raw["description"].(string); ok {
		entry.Description = description
	}
	if diskSize, ok := raw["disk-size"].(string); ok {
		diskSize = normalizeDriveDiskSize(diskSize)
		if diskSize != "" && !driveDiskSizePattern.MatchString(diskSize) {
			return entry, fmt.Errorf("invalid drive-memory disk-size %q: must be a number with an optional K, M, G, or T suffix (e.g. %q)", diskSize, defaultDriveMemoryDiskSize)
		}
		entry.DiskSize = diskSize
	}
	if prefetch, ok := raw["prefetch"].(bool); ok {
		entry.Prefetch = prefetch
	}
	if restoreOnly, ok := raw["restore-only"].(bool); ok {
		entry.RestoreOnly = restoreOnly
	}
	if err := parseDriveMemoryAllowedExtensions(raw, &entry); err != nil {
		return entry, err
	}
	validation, err := parseMemoryValidationConfig(raw, "tools.drive-memory.validation")
	if err != nil {
		return entry, err
	}
	entry.Validation = validation
	if len(entry.AllowedExtensions) == 0 {
		entry.AllowedExtensions = constants.DefaultAllowedMemoryExtensions
	}
	if err := validateMemorySchemaTargets(entry.Validation, entry.AllowedExtensions, nil, "tools.drive-memory.validation"); err != nil {
		return entry, err
	}
	return entry, nil
}

func parseDriveMemoryAllowedExtensions(raw map[string]any, entry *DriveMemoryEntry) error {
	value, exists := raw["allowed-extensions"]
	if !exists {
		return nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil
	}
	entry.AllowedExtensions = make([]string, 0, len(values))
	for _, value := range values {
		extension, ok := value.(string)
		if !ok {
			continue
		}
		if !isValidFileExtension(extension) {
			return fmt.Errorf("invalid allowed-extension %q: must start with '.' followed by alphanumeric characters only (e.g. .json)", extension)
		}
		entry.AllowedExtensions = append(entry.AllowedExtensions, extension)
	}
	return nil
}

func parseDriveMemoryEntries(values []any) ([]DriveMemoryEntry, error) {
	entries := make([]DriveMemoryEntry, 0, len(values))
	ids := make(map[string]struct{}, len(values))
	for _, value := range values {
		raw, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("tools.drive-memory array entries must be objects")
		}
		entry, err := parseDriveMemoryEntry(raw, "default")
		if err != nil {
			return nil, err
		}
		if _, exists := ids[entry.ID]; exists {
			return nil, fmt.Errorf("duplicate drive-memory id %q: each drive must have a unique id", entry.ID)
		}
		ids[entry.ID] = struct{}{}
		entries = append(entries, entry)
	}
	return entries, nil
}

func (c *Compiler) extractDriveMemoryConfig(toolsConfig *ToolsConfig) (*DriveMemoryConfig, error) {
	if toolsConfig == nil || toolsConfig.DriveMemory == nil {
		return nil, nil
	}

	config := &DriveMemoryConfig{}
	value := toolsConfig.DriveMemory.Raw
	if value == nil {
		config.Drives = defaultDriveMemoryEntries()
		return config, nil
	}
	if enabled, ok := value.(bool); ok {
		if enabled {
			config.Drives = defaultDriveMemoryEntries()
		}
		return config, nil
	}
	if values, ok := value.([]any); ok {
		entries, err := parseDriveMemoryEntries(values)
		if err != nil {
			return nil, err
		}
		config.Drives = entries
		return config, nil
	}
	if raw, ok := value.(map[string]any); ok {
		entry, err := parseDriveMemoryEntry(raw, "default")
		if err != nil {
			return nil, err
		}
		config.Drives = []DriveMemoryEntry{entry}
		return config, nil
	}
	return nil, nil
}

func validateDriveMemoryRuntime(data *WorkflowData) error {
	if data == nil || data.DriveMemoryConfig == nil || len(data.DriveMemoryConfig.Drives) == 0 {
		return nil
	}
	if data.Container != "" {
		return errors.New("tools.drive-memory requires the ubuntu-latest host runner and cannot be used with a job container")
	}
	if runsOn := strings.TrimSpace(data.RunsOn); runsOn != "" && runsOn != "runs-on: ubuntu-latest" {
		return errors.New("tools.drive-memory requires runs-on: ubuntu-latest during the GitHub Drives private preview")
	}
	for jobName, rawJob := range data.Jobs {
		job, ok := rawJob.(map[string]any)
		if !ok {
			continue
		}
		restoreMemory, _ := job["restore-memory"].(bool)
		if !restoreMemory {
			continue
		}
		if container, exists := job["container"]; exists && container != nil {
			return fmt.Errorf("jobs.%s.restore-memory cannot use drive-memory with a job container", jobName)
		}
		if runsOn, exists := job["runs-on"]; exists && runsOn != "ubuntu-latest" {
			return fmt.Errorf("jobs.%s.restore-memory requires runs-on: ubuntu-latest when drive-memory is configured", jobName)
		}
	}
	return nil
}
