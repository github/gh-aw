//go:build !integration

package workflow

import (
	"reflect"
	"testing"
)

func TestPackageExtractor_ExtractPackages_NpxPattern(t *testing.T) {
	// Test npx pattern (no required subcommand)
	extractor := PackageExtractor{
		CommandNames:       []string{"npx"},
		RequiredSubcommand: "",
		TrimSuffixes:       "&|;",
	}

	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "simple npx command",
			commands: "npx playwright",
			want:     []string{"playwright"},
		},
		{
			name:     "npx with version",
			commands: "npx @playwright/mcp@latest",
			want:     []string{"@playwright/mcp@latest"},
		},
		{
			name:     "npx with flags",
			commands: "npx --yes playwright",
			want:     []string{"playwright"},
		},
		{
			name:     "npx with semicolon",
			commands: "npx playwright;",
			want:     []string{"playwright"},
		},
		{
			name:     "npx with ampersand",
			commands: "npx playwright&",
			want:     []string{"playwright"},
		},
		{
			name:     "npx with pipe",
			commands: "npx playwright|",
			want:     []string{"playwright"},
		},
		{
			name: "multiple npx commands",
			commands: `npx playwright
npx typescript`,
			want: []string{"playwright", "typescript"},
		},
		{
			name:     "no npx command",
			commands: "echo 'hello'",
			want:     nil,
		},
		{
			name:     "empty command",
			commands: "",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.ExtractPackages(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_ExtractPackages_PipPattern(t *testing.T) {
	// Test pip pattern (required "install" subcommand)
	extractor := PackageExtractor{
		CommandNames:       []string{"pip", "pip3"},
		RequiredSubcommand: "install",
		TrimSuffixes:       "&|;",
	}

	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "simple pip install",
			commands: "pip install requests",
			want:     []string{"requests"},
		},
		{
			name:     "pip3 install",
			commands: "pip3 install numpy",
			want:     []string{"numpy"},
		},
		{
			name:     "pip install with version",
			commands: "pip install requests==2.28.0",
			want:     []string{"requests==2.28.0"},
		},
		{
			name:     "pip install with flags",
			commands: "pip install --upgrade pip",
			want:     []string{"pip"},
		},
		{
			name:     "pip install with multiple flags",
			commands: "pip install --no-cache-dir --upgrade requests",
			want:     []string{"requests"},
		},
		{
			name: "multiple pip commands",
			commands: `pip install requests
pip3 install numpy`,
			want: []string{"requests", "numpy"},
		},
		{
			name:     "pip without install",
			commands: "pip list",
			want:     nil,
		},
		{
			name:     "pip install only flags",
			commands: "pip install --upgrade --no-deps",
			want:     nil,
		},
		{
			name:     "pip command with semicolon",
			commands: "pip install requests;",
			want:     []string{"requests"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.ExtractPackages(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_ExtractPackages_GoPattern(t *testing.T) {
	// Test go pattern (required "install" or "get" subcommand)
	// Note: This requires handling multiple possible subcommands,
	// which we'll test separately for "install" and "get"
	extractorInstall := PackageExtractor{
		CommandNames:       []string{"go"},
		RequiredSubcommand: "install",
		TrimSuffixes:       "&|;",
	}

	extractorGet := PackageExtractor{
		CommandNames:       []string{"go"},
		RequiredSubcommand: "get",
		TrimSuffixes:       "&|;",
	}

	tests := []struct {
		name      string
		extractor PackageExtractor
		commands  string
		want      []string
	}{
		{
			name:      "go install",
			extractor: extractorInstall,
			commands:  "go install github.com/user/tool@v1.0.0",
			want:      []string{"github.com/user/tool@v1.0.0"},
		},
		{
			name:      "go get",
			extractor: extractorGet,
			commands:  "go get golang.org/x/tools@latest",
			want:      []string{"golang.org/x/tools@latest"},
		},
		{
			name:      "go install with flags",
			extractor: extractorInstall,
			commands:  "go install -v github.com/user/tool",
			want:      []string{"github.com/user/tool"},
		},
		{
			name:      "go without install",
			extractor: extractorInstall,
			commands:  "go build main.go",
			want:      nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.extractor.ExtractPackages(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_ExtractPackages_MultipleSubcommands(t *testing.T) {
	// Test extraction with multiple required subcommands (e.g., "go install" and "go get")
	extractor := PackageExtractor{
		CommandNames:        []string{"go"},
		RequiredSubcommands: []string{"install", "get"},
		TrimSuffixes:        "&|;",
	}

	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "go install with multiple subcommands",
			commands: "go install github.com/user/tool@v1.0.0",
			want:     []string{"github.com/user/tool@v1.0.0"},
		},
		{
			name:     "go get with multiple subcommands",
			commands: "go get golang.org/x/tools@latest",
			want:     []string{"golang.org/x/tools@latest"},
		},
		{
			name: "mixed go install and go get",
			commands: `go install github.com/user/tool@v1.0.0
go get golang.org/x/lint@latest`,
			want: []string{"github.com/user/tool@v1.0.0", "golang.org/x/lint@latest"},
		},
		{
			name:     "go install with flags",
			commands: "go install -v github.com/user/tool",
			want:     []string{"github.com/user/tool"},
		},
		{
			name:     "go without install or get",
			commands: "go build main.go",
			want:     nil,
		},
		{
			name:     "go mod command (not extracted)",
			commands: "go mod tidy",
			want:     nil,
		},
		{
			name:     "empty command",
			commands: "",
			want:     nil,
		},
		{
			name:     "go get with flags",
			commands: "go get -u github.com/user/tool@latest",
			want:     []string{"github.com/user/tool@latest"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.ExtractPackages(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_getRequiredSubcommands(t *testing.T) {
	tests := []struct {
		name      string
		extractor PackageExtractor
		want      []string
	}{
		{
			name: "only RequiredSubcommand set",
			extractor: PackageExtractor{
				RequiredSubcommand: "install",
			},
			want: []string{"install"},
		},
		{
			name: "only RequiredSubcommands set",
			extractor: PackageExtractor{
				RequiredSubcommands: []string{"install", "get"},
			},
			want: []string{"install", "get"},
		},
		{
			name: "both fields set - RequiredSubcommands takes precedence",
			extractor: PackageExtractor{
				RequiredSubcommand:  "deprecated",
				RequiredSubcommands: []string{"install", "get"},
			},
			want: []string{"install", "get"},
		},
		{
			name:      "neither field set",
			extractor: PackageExtractor{},
			want:      nil,
		},
		{
			name: "empty RequiredSubcommand",
			extractor: PackageExtractor{
				RequiredSubcommand: "",
			},
			want: nil,
		},
		{
			name: "empty RequiredSubcommands slice",
			extractor: PackageExtractor{
				RequiredSubcommands: []string{},
			},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.extractor.getRequiredSubcommands()
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getRequiredSubcommands() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_isCommandName(t *testing.T) {
	extractor := PackageExtractor{
		CommandNames: []string{"pip", "pip3"},
	}

	tests := []struct {
		name string
		word string
		want bool
	}{
		{
			name: "matches pip",
			word: "pip",
			want: true,
		},
		{
			name: "matches pip3",
			word: "pip3",
			want: true,
		},
		{
			name: "does not match npm",
			word: "npm",
			want: false,
		},
		{
			name: "does not match empty",
			word: "",
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.isCommandName(tt.word)
			if got != tt.want {
				t.Errorf("isCommandName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_findPackageName(t *testing.T) {
	extractor := PackageExtractor{
		TrimSuffixes: "&|;",
	}

	tests := []struct {
		name       string
		words      []string
		startIndex int
		want       string
	}{
		{
			name:       "finds package name",
			words:      []string{"install", "requests"},
			startIndex: 1,
			want:       "requests",
		},
		{
			name:       "skips flags",
			words:      []string{"install", "--upgrade", "requests"},
			startIndex: 1,
			want:       "requests",
		},
		{
			name:       "trims semicolon",
			words:      []string{"install", "requests;"},
			startIndex: 1,
			want:       "requests",
		},
		{
			name:       "trims ampersand",
			words:      []string{"install", "requests&"},
			startIndex: 1,
			want:       "requests",
		},
		{
			name:       "trims pipe",
			words:      []string{"install", "requests|"},
			startIndex: 1,
			want:       "requests",
		},
		{
			name:       "no package found",
			words:      []string{"install", "--upgrade"},
			startIndex: 1,
			want:       "",
		},
		{
			name:       "start index out of bounds",
			words:      []string{"install"},
			startIndex: 5,
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractor.findPackageName(tt.words, tt.startIndex)
			if got != tt.want {
				t.Errorf("findPackageName() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestPackageExtractor_ComplexScenarios(t *testing.T) {
	tests := []struct {
		name      string
		extractor PackageExtractor
		commands  string
		want      []string
	}{
		{
			name: "mixed commands with pip",
			extractor: PackageExtractor{
				CommandNames:       []string{"pip", "pip3"},
				RequiredSubcommand: "install",
				TrimSuffixes:       "&|;",
			},
			commands: `apt-get update
pip install requests
apt-get install python3-dev
pip3 install numpy`,
			want: []string{"requests", "numpy"},
		},
		{
			name: "script block with pip",
			extractor: PackageExtractor{
				CommandNames:       []string{"pip"},
				RequiredSubcommand: "install",
				TrimSuffixes:       "&|;",
			},
			commands: `#!/bin/bash
set -e
pip install --upgrade pip
pip install requests==2.28.0`,
			want: []string{"pip", "requests==2.28.0"},
		},
		{
			name: "multiple npx on same line",
			extractor: PackageExtractor{
				CommandNames:       []string{"npx"},
				RequiredSubcommand: "",
				TrimSuffixes:       "&|;",
			},
			commands: "npx black && npx ruff",
			want:     []string{"black", "ruff"},
		},
		{
			name: "package with special characters",
			extractor: PackageExtractor{
				CommandNames:       []string{"pip"},
				RequiredSubcommand: "install",
				TrimSuffixes:       "&|;",
			},
			commands: "pip install Flask-CORS",
			want:     []string{"Flask-CORS"},
		},
		{
			name: "package in quotes",
			extractor: PackageExtractor{
				CommandNames:       []string{"pip"},
				RequiredSubcommand: "install",
				TrimSuffixes:       "&|;",
			},
			commands: `pip install "requests[security]"`,
			want:     []string{`"requests[security]"`},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.extractor.ExtractPackages(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}
