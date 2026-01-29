//go:build !integration

package workflow

import (
	"testing"
)

func TestUploadAssetsConfigDefaults(t *testing.T) {
	compiler := NewCompiler()

	// Test default configuration
	outputMap := map[string]any{
		"upload-asset": nil,
	}

	config := compiler.parseUploadAssetConfig(outputMap)
	if config == nil {
		t.Fatal("Expected config to be created with defaults")
	}

	// Check default extensions match problem statement requirement
	expectedExts := []string{".png", ".jpg", ".jpeg"}
	if len(config.AllowedExts) != len(expectedExts) {
		t.Errorf("Expected %d default extensions, got %d", len(expectedExts), len(config.AllowedExts))
	}

	for i, ext := range expectedExts {
		if i >= len(config.AllowedExts) || config.AllowedExts[i] != ext {
			t.Errorf("Expected extension %s at position %d, got %v", ext, i, config.AllowedExts)
		}
	}

	// Check default max size
	if config.MaxSizeKB != 10240 {
		t.Errorf("Expected default max size 10240, got %d", config.MaxSizeKB)
	}
}

func TestUploadAssetsConfigCustomExtensions(t *testing.T) {
	compiler := NewCompiler()

	// Test custom configuration like dev.md
	outputMap := map[string]any{
		"upload-asset": map[string]any{
			"allowed-exts": []any{".txt"},
			"max-size":     1024,
		},
	}

	config := compiler.parseUploadAssetConfig(outputMap)
	if config == nil {
		t.Fatal("Expected config to be created")
	}

	// Check custom extensions
	expectedExts := []string{".txt"}
	if len(config.AllowedExts) != len(expectedExts) {
		t.Errorf("Expected %d custom extensions, got %d", len(expectedExts), len(config.AllowedExts))
	}

	if config.AllowedExts[0] != ".txt" {
		t.Errorf("Expected custom extension .txt, got %s", config.AllowedExts[0])
	}

	// Check custom max size
	if config.MaxSizeKB != 1024 {
		t.Errorf("Expected custom max size 1024, got %d", config.MaxSizeKB)
	}
}
