//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/constants"
)

func TestSelectSerenaContainer(t *testing.T) {
	tests := []struct {
		name              string
		serenaTool        any
		expectedContainer string
	}{
		{
			name: "no languages specified - uses default",
			serenaTool: map[string]any{
				"mode": "docker",
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
		{
			name: "supported languages - uses default",
			serenaTool: map[string]any{
				"langs": []any{"go", "typescript"},
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
		{
			name: "all supported languages - uses default",
			serenaTool: map[string]any{
				"languages": map[string]any{
					"go":         map[string]any{},
					"typescript": map[string]any{},
					"python":     map[string]any{},
				},
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
		{
			name: "unsupported language - still uses default",
			serenaTool: map[string]any{
				"langs": []any{"unsupported-lang"},
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
		{
			name: "SerenaToolConfig with short syntax",
			serenaTool: &SerenaToolConfig{
				ShortSyntax: []string{"go", "rust"},
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
		{
			name: "SerenaToolConfig with detailed languages",
			serenaTool: &SerenaToolConfig{
				Languages: map[string]*SerenaLangConfig{
					"python": {},
					"java":   {},
				},
			},
			expectedContainer: constants.DefaultSerenaMCPServerContainer,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := selectSerenaContainer(tt.serenaTool)
			if result != tt.expectedContainer {
				t.Errorf("selectSerenaContainer() = %v, want %v", result, tt.expectedContainer)
			}
		})
	}
}

func TestSerenaLanguageSupport(t *testing.T) {
	// Test that the language support map is properly defined
	if len(constants.SerenaLanguageSupport) == 0 {
		t.Error("SerenaLanguageSupport map is empty")
	}

	// Test that default container has languages defined
	defaultLangs := constants.SerenaLanguageSupport[constants.DefaultSerenaMCPServerContainer]
	if len(defaultLangs) == 0 {
		t.Error("Default Serena container has no supported languages defined")
	}

	// Test that Oraios container has languages defined
	oraiosLangs := constants.SerenaLanguageSupport[constants.OraiosSerenaContainer]
	if len(oraiosLangs) == 0 {
		t.Error("Oraios Serena container has no supported languages defined")
	}

	// Verify some expected languages are present in default container
	expectedLangs := []string{"go", "typescript", "python", "java", "rust"}
	for _, lang := range expectedLangs {
		found := false
		for _, supportedLang := range defaultLangs {
			if supportedLang == lang {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected language '%s' not found in default container support list", lang)
		}
	}
}
