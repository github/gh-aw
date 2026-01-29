//go:build !integration

package workflow

import (
	"reflect"
	"testing"
)

func TestExtractPipFromCommands(t *testing.T) {
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
			name:     "pip install multiple packages on one line",
			commands: "pip install requests numpy pandas",
			want:     []string{"requests"}, // Only first package is extracted
		},
		{
			name: "multiple pip install commands",
			commands: `pip install requests
pip install numpy
pip3 install pandas`,
			want: []string{"requests", "numpy", "pandas"},
		},
		{
			name:     "pip command with semicolon",
			commands: "pip install requests;",
			want:     []string{"requests"},
		},
		{
			name:     "pip command with ampersand",
			commands: "pip install requests&",
			want:     []string{"requests"},
		},
		{
			name:     "pip command with pipe",
			commands: "pip install requests|",
			want:     []string{"requests"},
		},
		{
			name:     "no pip commands",
			commands: "echo 'no packages here'",
			want:     nil,
		},
		{
			name:     "pip without install",
			commands: "pip list",
			want:     nil,
		},
		{
			name:     "empty command string",
			commands: "",
			want:     nil,
		},
		{
			name:     "pip install with complex flags and options",
			commands: "pip install -r requirements.txt",
			want:     []string{"requirements.txt"},
		},
		{
			name:     "pip install with package containing special chars",
			commands: "pip install Flask-CORS",
			want:     []string{"Flask-CORS"},
		},
		{
			name:     "pip install with package in quotes",
			commands: `pip install "requests[security]"`,
			want:     []string{`"requests[security]"`},
		},
		{
			name: "mixed commands",
			commands: `apt-get update
pip install requests
apt-get install python3-dev
pip3 install numpy`,
			want: []string{"requests", "numpy"},
		},
		{
			name: "pip command in script block",
			commands: `#!/bin/bash
set -e
pip install --upgrade pip
pip install requests==2.28.0`,
			want: []string{"pip", "requests==2.28.0"},
		},
		{
			name:     "pip install with environment variable",
			commands: "pip install $PACKAGE_NAME",
			want:     []string{"$PACKAGE_NAME"},
		},
		{
			name:     "pip install only flags",
			commands: "pip install --upgrade --no-deps",
			want:     nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPipFromCommands(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractPipFromCommands() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractUvFromCommands(t *testing.T) {
	tests := []struct {
		name     string
		commands string
		want     []string
	}{
		{
			name:     "simple uvx command",
			commands: "uvx ruff",
			want:     []string{"ruff"},
		},
		{
			name:     "uvx with version",
			commands: "uvx ruff@0.1.0",
			want:     []string{"ruff@0.1.0"},
		},
		{
			name:     "uvx with semicolon",
			commands: "uvx black;",
			want:     []string{"black"},
		},
		{
			name:     "uvx with ampersand",
			commands: "uvx mypy&",
			want:     []string{"mypy"},
		},
		{
			name:     "uvx with pipe",
			commands: "uvx pytest|",
			want:     []string{"pytest"},
		},
		{
			name:     "uv pip install",
			commands: "uv pip install requests",
			want:     []string{"requests"},
		},
		{
			name:     "uv pip install with version",
			commands: "uv pip install numpy==1.24.0",
			want:     []string{"numpy==1.24.0"},
		},
		{
			name:     "uv pip install with flags",
			commands: "uv pip install --upgrade requests",
			want:     []string{"requests"},
		},
		{
			name:     "uv pip install with multiple flags",
			commands: "uv pip install --no-cache --system requests",
			want:     []string{"requests"},
		},
		{
			name: "multiple uv commands",
			commands: `uvx ruff
uv pip install numpy
uvx black`,
			want: []string{"ruff", "numpy", "black"},
		},
		{
			name:     "no uv commands",
			commands: "pip install requests",
			want:     nil,
		},
		{
			name:     "uv without pip or uvx",
			commands: "uv --version",
			want:     nil,
		},
		{
			name:     "empty command string",
			commands: "",
			want:     nil,
		},
		{
			name:     "uv pip without install",
			commands: "uv pip list",
			want:     nil,
		},
		{
			name:     "uv pip install only flags",
			commands: "uv pip install --upgrade --no-deps",
			want:     nil,
		},
		{
			name:     "uv pip install with -r flag",
			commands: "uv pip install -r requirements.txt",
			want:     []string{"requirements.txt"},
		},
		{
			name: "mixed uv and pip commands",
			commands: `pip install old-package
uvx new-tool
uv pip install modern-package`,
			want: []string{"new-tool", "modern-package"},
		},
		{
			name:     "uv pip install with package containing special chars",
			commands: "uv pip install Flask-CORS",
			want:     []string{"Flask-CORS"},
		},
		{
			name:     "uvx with package in quotes",
			commands: `uvx "package-name"`,
			want:     []string{`"package-name"`},
		},
		{
			name: "uv command in script block",
			commands: `#!/bin/bash
set -e
uv pip install --upgrade pip
uvx ruff check .`,
			want: []string{"pip", "ruff"},
		},
		{
			name:     "uvx with environment variable",
			commands: "uvx $TOOL_NAME",
			want:     []string{"$TOOL_NAME"},
		},
		{
			name:     "multiple uvx on same line (edge case)",
			commands: "uvx black && uvx ruff",
			want:     []string{"black", "ruff"}, // Both are extracted since they're on separate tokens
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUvFromCommands(tt.commands)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractUvFromCommands() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractPipPackages(t *testing.T) {
	// This function extracts packages from custom steps and engine steps
	// We test it with CustomSteps field which is easier to test
	tests := []struct {
		name string
		data *WorkflowData
		want []string
	}{
		{
			name: "no custom steps",
			data: &WorkflowData{
				CustomSteps: "",
			},
			want: nil,
		},
		{
			name: "custom step with pip install",
			data: &WorkflowData{
				CustomSteps: "pip install requests",
			},
			want: []string{"requests"},
		},
		{
			name: "custom step with multiple pip installs",
			data: &WorkflowData{
				CustomSteps: `pip install requests
pip3 install numpy`,
			},
			want: []string{"requests", "numpy"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractPipPackages(tt.data)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractPipPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestExtractUvPackages(t *testing.T) {
	// This function extracts packages from custom steps and engine steps
	// We test it with CustomSteps field which is easier to test
	tests := []struct {
		name string
		data *WorkflowData
		want []string
	}{
		{
			name: "no custom steps",
			data: &WorkflowData{
				CustomSteps: "",
			},
			want: nil,
		},
		{
			name: "custom step with uvx",
			data: &WorkflowData{
				CustomSteps: "uvx ruff",
			},
			want: []string{"ruff"},
		},
		{
			name: "custom step with uv pip install",
			data: &WorkflowData{
				CustomSteps: "uv pip install black",
			},
			want: []string{"black"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractUvPackages(tt.data)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("extractUvPackages() = %v, want %v", got, tt.want)
			}
		})
	}
}
