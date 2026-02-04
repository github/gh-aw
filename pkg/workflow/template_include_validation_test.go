//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestValidateNoIncludesInTemplateRegions(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid - include outside template region",
			input: `# Test Workflow

@include shared/tools.md

{{#if github.event.issue.number}}
This is inside a template.
{{/if}}`,
			wantErr: false,
		},
		{
			name: "invalid - include inside template region",
			input: `# Test Workflow

{{#if github.event.issue.number}}
@include shared/tools.md
Some content here.
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "invalid - import inside template region",
			input: `# Test Workflow

{{#if github.actor}}
@import shared/config.md
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "invalid - optional include inside template region",
			input: `# Test Workflow

{{#if github.repository}}
@include? shared/optional.md
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - multiple includes outside templates",
			input: `# Test Workflow

@include shared/tools.md
@import shared/config.md

{{#if github.event.issue.number}}
Content here.
{{/if}}

@include shared/footer.md`,
			wantErr: false,
		},
		{
			name: "valid - no templates, only includes",
			input: `# Test Workflow

@include shared/tools.md
@import shared/config.md

Regular content without templates.`,
			wantErr: false,
		},
		{
			name: "valid - no includes, only templates",
			input: `# Test Workflow

{{#if github.event.issue.number}}
Content inside template.
{{/if}}

Regular content outside template.`,
			wantErr: false,
		},
		{
			name: "invalid - multiple templates with include in one",
			input: `# Test Workflow

{{#if github.event.issue.number}}
First template - no include.
{{/if}}

{{#if github.actor}}
@include shared/tools.md
Second template - has include.
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - nested content but include outside",
			input: `# Test Workflow

@include shared/header.md

{{#if github.event.issue.number}}
Some content.
{{/if}}

@include shared/footer.md`,
			wantErr: false,
		},
		{
			name: "invalid - include with section reference inside template",
			input: `# Test Workflow

{{#if github.event.pull_request.number}}
@include shared/tools.md#Security
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - include with section reference outside template",
			input: `# Test Workflow

@include shared/tools.md#Security

{{#if github.event.pull_request.number}}
Content here.
{{/if}}`,
			wantErr: false,
		},
		{
			name: "invalid - include in multiline template content",
			input: `# Test Workflow

{{#if github.event.issue.number}}
This is a longer template block
with multiple lines of content.

@include shared/tools.md

More content after the include.
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - template inside template outside (complex nesting)",
			input: `# Test Workflow

@include shared/header.md

{{#if github.event.issue.number}}
Content 1
{{/if}}

Some text between templates.

{{#if github.actor}}
Content 2
{{/if}}

@include shared/footer.md`,
			wantErr: false,
		},
		{
			name: "valid - empty template",
			input: `# Test Workflow

{{#if github.event.issue.number}}{{/if}}

@include shared/tools.md`,
			wantErr: false,
		},
		{
			name: "invalid - include in template with wrapped expression",
			input: `# Test Workflow

{{#if ${{ github.event.issue.number }} }}
@include shared/tools.md
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - no templates or includes",
			input: `# Test Workflow

Just regular markdown content.
No templates or includes here.`,
			wantErr: false,
		},
		{
			name: "invalid - indented include inside template",
			input: `# Test Workflow

{{#if github.event.issue.number}}
  @include shared/tools.md
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "invalid - nested template with include in inner block",
			input: `# Test Workflow

{{#if github.event.issue.number}}
First level template.

  {{#if github.actor}}
  @include shared/nested-tools.md
  {{/if}}

End of first level.
{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - two leading spaces before opening tag, include outside",
			input: `# Test Workflow

@include shared/tools.md

  {{#if github.event.issue.number}}
  Content with leading spaces
  {{/if}}`,
			wantErr: false,
		},
		{
			name: "invalid - two leading spaces before opening tag, include inside",
			input: `# Test Workflow

  {{#if github.event.issue.number}}
  @include shared/tools.md
  Content with leading spaces
  {{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - four leading spaces before opening tag, include outside",
			input: `# Test Workflow

    {{#if github.actor}}
    Content with four leading spaces
    {{/if}}

@include shared/footer.md`,
			wantErr: false,
		},
		{
			name: "invalid - four leading spaces before opening tag, include inside",
			input: `# Test Workflow

    {{#if github.actor}}
    @include shared/tools.md
    Content with four leading spaces
    {{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - tab before opening tag, include outside",
			input: `# Test Workflow

	{{#if github.repository}}
	Content with tab indentation
	{{/if}}

@include shared/tools.md`,
			wantErr: false,
		},
		{
			name: "invalid - tab before opening tag, include inside",
			input: `# Test Workflow

	{{#if github.repository}}
	@include shared/tools.md
	Content with tab indentation
	{{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - mixed indentation levels, includes outside all blocks",
			input: `# Test Workflow

@include shared/header.md

{{#if github.actor}}
No indent
{{/if}}

  {{#if github.repository}}
  Two space indent
  {{/if}}

    {{#if github.event.issue.number}}
    Four space indent
    {{/if}}

@include shared/footer.md`,
			wantErr: false,
		},
		{
			name: "invalid - mixed indentation with include in middle block",
			input: `# Test Workflow

{{#if github.actor}}
No indent
{{/if}}

  {{#if github.repository}}
  @include shared/tools.md
  Two space indent
  {{/if}}

    {{#if github.event.issue.number}}
    Four space indent
    {{/if}}`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
		{
			name: "valid - realistic linter-formatted markdown with leading spaces",
			input: `# Analysis Workflow

@include shared/setup.md

## Conditional Analysis

  {{#if github.event.issue.number}}
  ### Issue Analysis
  
  This section analyzes issues.
  {{/if}}

  {{#if github.event.pull_request.number}}
  ### PR Analysis
  
  This section analyzes pull requests.
  {{/if}}

@include shared/conclusion.md`,
			wantErr: false,
		},
		{
			name: "invalid - realistic linter-formatted markdown with include inside",
			input: `# Analysis Workflow

@include shared/setup.md

## Conditional Analysis

  {{#if github.event.issue.number}}
  ### Issue Analysis
  
  @include shared/issue-helpers.md
  
  This section analyzes issues.
  {{/if}}

@include shared/conclusion.md`,
			wantErr: true,
			errMsg:  "import directives cannot be used inside template regions",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateNoIncludesInTemplateRegions(tt.input)

			if tt.wantErr {
				if err == nil {
					t.Errorf("validateNoIncludesInTemplateRegions() expected error, got nil")
					return
				}
				if tt.errMsg != "" && !strings.Contains(err.Error(), tt.errMsg) {
					t.Errorf("validateNoIncludesInTemplateRegions() error = %q, want to contain %q", err.Error(), tt.errMsg)
				}
			} else {
				if err != nil {
					t.Errorf("validateNoIncludesInTemplateRegions() unexpected error = %v", err)
				}
			}
		})
	}
}

// TestValidateNoIncludesInTemplateRegions_MultipleErrors tests that multiple errors are aggregated
func TestValidateNoIncludesInTemplateRegions_MultipleErrors(t *testing.T) {
	// Input with multiple includes inside different template regions
	input := `# Test Workflow

{{#if github.event.issue.number}}
@include shared/issue-tools.md
Some content here.
{{/if}}

{{#if github.actor}}
@import shared/actor-config.md
More content here.
{{/if}}

{{#if github.repository}}
@include? shared/optional.md
Even more content.
{{/if}}`

	err := validateNoIncludesInTemplateRegions(input)

	// Should return aggregated error with all three violations
	if err == nil {
		t.Fatal("validateNoIncludesInTemplateRegions() expected error, got nil")
	}

	errStr := err.Error()

	// Check that all three errors are present
	if !strings.Contains(errStr, "issue-tools.md") {
		t.Errorf("Error should contain first violation: issue-tools.md")
	}
	if !strings.Contains(errStr, "actor-config.md") {
		t.Errorf("Error should contain second violation: actor-config.md")
	}
	if !strings.Contains(errStr, "optional.md") {
		t.Errorf("Error should contain third violation: optional.md")
	}

	// Check for newline-separated errors (errors.Join behavior)
	errorLines := strings.Split(errStr, "\n")
	if len(errorLines) < 3 {
		t.Errorf("Expected at least 3 error lines, got %d", len(errorLines))
	}
}

// TestValidateNoIncludesInTemplateRegions_SingleError tests single error behavior
func TestValidateNoIncludesInTemplateRegions_SingleError(t *testing.T) {
	// Input with single include inside template region
	input := `# Test Workflow

{{#if github.event.issue.number}}
@include shared/tools.md
{{/if}}`

	err := validateNoIncludesInTemplateRegions(input)

	// Should return single error (not aggregated)
	if err == nil {
		t.Fatal("validateNoIncludesInTemplateRegions() expected error, got nil")
	}

	errStr := err.Error()
	if !strings.Contains(errStr, "tools.md") {
		t.Errorf("Error should contain violation: tools.md")
	}
}
