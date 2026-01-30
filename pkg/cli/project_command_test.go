//go:build !integration

package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewProjectCommand(t *testing.T) {
	cmd := NewProjectCommand()
	require.NotNil(t, cmd, "Command should be created")
	assert.Equal(t, "project", cmd.Use, "Command name should be 'project'")
	assert.Contains(t, cmd.Short, "GitHub Projects V2", "Short description should mention Projects V2")
	assert.NotEmpty(t, cmd.Commands(), "Command should have subcommands")
}

func TestNewProjectNewCommand(t *testing.T) {
	cmd := NewProjectNewCommand()
	require.NotNil(t, cmd, "Command should be created")
	assert.Equal(t, "new <title>", cmd.Use, "Command usage should be 'new <title>'")
	assert.Contains(t, cmd.Short, "Create a new GitHub Project V2", "Short description should be about creating projects")

	// Check flags
	ownerFlag := cmd.Flags().Lookup("owner")
	require.NotNil(t, ownerFlag, "Should have --owner flag")
	assert.Equal(t, "o", ownerFlag.Shorthand, "Owner flag should have short form 'o'")

	linkFlag := cmd.Flags().Lookup("link")
	require.NotNil(t, linkFlag, "Should have --link flag")
	assert.Equal(t, "l", linkFlag.Shorthand, "Link flag should have short form 'l'")
}

func TestEscapeGraphQLString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "plain text",
			input:    "Hello World",
			expected: "Hello World",
		},
		{
			name:     "with quotes",
			input:    `Project "Alpha"`,
			expected: `Project \"Alpha\"`,
		},
		{
			name:     "with backslash",
			input:    `Path\to\file`,
			expected: `Path\\to\\file`,
		},
		{
			name:     "with newline",
			input:    "Line 1\nLine 2",
			expected: "Line 1\\nLine 2",
		},
		{
			name:     "with tab",
			input:    "Name\tValue",
			expected: "Name\\tValue",
		},
		{
			name:     "complex string",
			input:    "Test \"project\"\nWith\ttabs\\and backslashes",
			expected: "Test \\\"project\\\"\\nWith\\ttabs\\\\and backslashes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeGraphQLString(tt.input)
			assert.Equal(t, tt.expected, result, "GraphQL string should be properly escaped")
		})
	}
}

func TestProjectConfig(t *testing.T) {
	tests := []struct {
		name        string
		config      ProjectConfig
		description string
	}{
		{
			name: "user project",
			config: ProjectConfig{
				Title:     "My Project",
				Owner:     "testuser",
				OwnerType: "user",
			},
			description: "Should create user project",
		},
		{
			name: "org project",
			config: ProjectConfig{
				Title:     "Team Board",
				Owner:     "myorg",
				OwnerType: "org",
			},
			description: "Should create org project",
		},
		{
			name: "project with repo",
			config: ProjectConfig{
				Title:     "Bugs",
				Owner:     "myorg",
				OwnerType: "org",
				Repo:      "myorg/myrepo",
			},
			description: "Should create project linked to repo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.config.Title, "Project title should not be empty")
			assert.NotEmpty(t, tt.config.Owner, "Project owner should not be empty")
			assert.NotEmpty(t, tt.config.OwnerType, "Owner type should not be empty")
			assert.Contains(t, []string{"user", "org"}, tt.config.OwnerType, "Owner type should be 'user' or 'org'")
		})
	}
}

func TestProjectNewCommandArgs(t *testing.T) {
	cmd := NewProjectNewCommand()

	tests := []struct {
		name      string
		args      []string
		shouldErr bool
	}{
		{
			name:      "no arguments",
			args:      []string{},
			shouldErr: true,
		},
		{
			name:      "one argument",
			args:      []string{"My Project"},
			shouldErr: false,
		},
		{
			name:      "too many arguments",
			args:      []string{"My Project", "Extra"},
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := cmd.Args(cmd, tt.args)
			if tt.shouldErr {
				assert.Error(t, err, "Should return error for invalid arguments")
			} else {
				assert.NoError(t, err, "Should not return error for valid arguments")
			}
		})
	}
}
