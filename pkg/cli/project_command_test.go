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

func TestProjectNewCommandFlags(t *testing.T) {
	cmd := NewProjectNewCommand()

	// Check standard flags
	ownerFlag := cmd.Flags().Lookup("owner")
	require.NotNil(t, ownerFlag, "Should have --owner flag")

	linkFlag := cmd.Flags().Lookup("link")
	require.NotNil(t, linkFlag, "Should have --link flag")

	// Check project setup flag
	projectSetupFlag := cmd.Flags().Lookup("with-project-setup")
	require.NotNil(t, projectSetupFlag, "Should have --with-project-setup flag")
	assert.Equal(t, "bool", projectSetupFlag.Value.Type(), "Project setup flag should be boolean")

	// Verify removed flags don't exist
	viewsFlag := cmd.Flags().Lookup("views")
	assert.Nil(t, viewsFlag, "Should not have --views flag")

	fieldsFlag := cmd.Flags().Lookup("fields")
	assert.Nil(t, fieldsFlag, "Should not have --fields flag")
}

func TestParseProjectURL(t *testing.T) {
	tests := []struct {
		name           string
		url            string
		expectedScope  string
		expectedOwner  string
		expectedNumber int
		shouldErr      bool
	}{
		{
			name:           "org project",
			url:            "https://github.com/orgs/myorg/projects/123",
			expectedScope:  "orgs",
			expectedOwner:  "myorg",
			expectedNumber: 123,
			shouldErr:      false,
		},
		{
			name:           "user project",
			url:            "https://github.com/users/myuser/projects/456",
			expectedScope:  "users",
			expectedOwner:  "myuser",
			expectedNumber: 456,
			shouldErr:      false,
		},
		{
			name:      "invalid URL",
			url:       "https://github.com/myorg/myrepo",
			shouldErr: true,
		},
		{
			name:      "empty URL",
			url:       "",
			shouldErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := parseProjectURL(tt.url)
			if tt.shouldErr {
				assert.Error(t, err, "Should return error for invalid URL")
			} else {
				require.NoError(t, err, "Should not return error for valid URL")
				assert.Equal(t, tt.expectedScope, result.scope, "Scope should match")
				assert.Equal(t, tt.expectedOwner, result.ownerLogin, "Owner should match")
				assert.Equal(t, tt.expectedNumber, result.projectNumber, "Project number should match")
			}
		})
	}
}

func TestEnsureSingleSelectOptionBefore(t *testing.T) {
	tests := []struct {
		name           string
		options        []singleSelectOption
		desired        singleSelectOption
		beforeName     string
		expectChanged  bool
		expectedLength int
	}{
		{
			name: "add new option before Done",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY"},
				{Name: "In Progress", Color: "YELLOW"},
				{Name: "Done", Color: "GREEN"},
			},
			desired:        singleSelectOption{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
			beforeName:     "Done",
			expectChanged:  true,
			expectedLength: 4,
		},
		{
			name: "option already exists in correct position",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY"},
				{Name: "In Progress", Color: "YELLOW"},
				{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
				{Name: "Done", Color: "GREEN"},
			},
			desired:        singleSelectOption{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
			beforeName:     "Done",
			expectChanged:  false,
			expectedLength: 4,
		},
		{
			name: "option exists but in wrong position",
			options: []singleSelectOption{
				{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
				{Name: "Todo", Color: "GRAY"},
				{Name: "In Progress", Color: "YELLOW"},
				{Name: "Done", Color: "GREEN"},
			},
			desired:        singleSelectOption{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
			beforeName:     "Done",
			expectChanged:  true,
			expectedLength: 4,
		},
		{
			name: "beforeName option does not exist - appends to end",
			options: []singleSelectOption{
				{Name: "Todo", Color: "GRAY"},
				{Name: "In Progress", Color: "YELLOW"},
			},
			desired:        singleSelectOption{Name: "Review Required", Color: "BLUE", Description: "Needs review"},
			beforeName:     "NonExistent",
			expectChanged:  true,
			expectedLength: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, changed := ensureSingleSelectOptionBefore(tt.options, tt.desired, tt.beforeName)
			assert.Equal(t, tt.expectChanged, changed, "Changed status should match expectation")
			assert.Len(t, result, tt.expectedLength, "Result length should match")

			if !tt.expectChanged {
				// If nothing changed, result should be equal to input
				assert.Equal(t, tt.options, result, "Options should be unchanged")
			} else {
				// Find the desired option and Done option
				desiredIdx, doneIdx := -1, -1
				for i, opt := range result {
					if opt.Name == tt.desired.Name {
						desiredIdx = i
					}
					if opt.Name == tt.beforeName {
						doneIdx = i
					}
				}

				if desiredIdx >= 0 && doneIdx >= 0 {
					assert.Less(t, desiredIdx, doneIdx, "Desired option should be before Done")
				}
			}
		})
	}
}

func TestSingleSelectOptionsEqual(t *testing.T) {
	tests := []struct {
		name     string
		a        []singleSelectOption
		b        []singleSelectOption
		expected bool
	}{
		{
			name: "equal options",
			a: []singleSelectOption{
				{Name: "Option 1", Color: "RED"},
				{Name: "Option 2", Color: "BLUE"},
			},
			b: []singleSelectOption{
				{Name: "Option 1", Color: "RED"},
				{Name: "Option 2", Color: "BLUE"},
			},
			expected: true,
		},
		{
			name: "different lengths",
			a: []singleSelectOption{
				{Name: "Option 1", Color: "RED"},
			},
			b: []singleSelectOption{
				{Name: "Option 1", Color: "RED"},
				{Name: "Option 2", Color: "BLUE"},
			},
			expected: false,
		},
		{
			name: "different order",
			a: []singleSelectOption{
				{Name: "Option 1", Color: "RED"},
				{Name: "Option 2", Color: "BLUE"},
			},
			b: []singleSelectOption{
				{Name: "Option 2", Color: "BLUE"},
				{Name: "Option 1", Color: "RED"},
			},
			expected: false,
		},
		{
			name:     "both empty",
			a:        []singleSelectOption{},
			b:        []singleSelectOption{},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := singleSelectOptionsEqual(tt.a, tt.b)
			assert.Equal(t, tt.expected, result, "Equality check should match expectation")
		})
	}
}

func TestProjectConfigWithProjectSetup(t *testing.T) {
	tests := []struct {
		name        string
		config      ProjectConfig
		description string
	}{
		{
			name: "with project setup",
			config: ProjectConfig{
				Title:            "Project With Setup",
				Owner:            "myorg",
				OwnerType:        "org",
				WithProjectSetup: true,
			},
			description: "Should have project setup enabled",
		},
		{
			name: "without project setup",
			config: ProjectConfig{
				Title:            "Basic Project",
				Owner:            "myorg",
				OwnerType:        "org",
				WithProjectSetup: false,
			},
			description: "Should have project setup disabled",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.NotEmpty(t, tt.config.Title, "Project title should not be empty")
			assert.NotEmpty(t, tt.config.Owner, "Project owner should not be empty")

			// Verify flag settings
			if tt.config.WithProjectSetup {
				assert.True(t, tt.config.WithProjectSetup, "Project setup should be enabled")
			} else {
				assert.False(t, tt.config.WithProjectSetup, "Project setup should be disabled")
			}
		})
	}
}
