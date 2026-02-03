package workflow

import (
	"encoding/json"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputValidationLog = logger.New("workflow:safe_output_validation_config")

// FieldValidation defines validation rules for a single field
type FieldValidation struct {
	Required                 bool     `json:"required,omitempty"`
	Type                     string   `json:"type,omitempty"`
	Sanitize                 bool     `json:"sanitize,omitempty"`
	MaxLength                int      `json:"maxLength,omitempty"`
	PositiveInteger          bool     `json:"positiveInteger,omitempty"`
	OptionalPositiveInteger  bool     `json:"optionalPositiveInteger,omitempty"`
	IssueOrPRNumber          bool     `json:"issueOrPRNumber,omitempty"`
	IssueNumberOrTemporaryID bool     `json:"issueNumberOrTemporaryId,omitempty"`
	Enum                     []string `json:"enum,omitempty"`
	ItemType                 string   `json:"itemType,omitempty"`
	ItemSanitize             bool     `json:"itemSanitize,omitempty"`
	ItemMaxLength            int      `json:"itemMaxLength,omitempty"`
	Pattern                  string   `json:"pattern,omitempty"`
	PatternError             string   `json:"patternError,omitempty"`
	TemporaryID              bool     `json:"temporaryId,omitempty"`
}

// TypeValidationConfig defines validation configuration for a safe output type
type TypeValidationConfig struct {
	DefaultMax       int                        `json:"defaultMax"`
	Fields           map[string]FieldValidation `json:"fields"`
	CustomValidation string                     `json:"customValidation,omitempty"`
}

// Constants for validation
const (
	MaxBodyLength           = 65000
	MaxGitHubUsernameLength = 39
)

// ValidationConfig contains all safe output type validation rules
// This is the single source of truth for validation rules
var ValidationConfig = map[string]TypeValidationConfig{
	"create_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":        {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":         {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"labels":       {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
			"parent":       {IssueOrPRNumber: true},
			"temporary_id": {Type: "string"},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"create_agent_session": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body": {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
		},
	},
	"add_comment": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":        {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"item_number": {IssueOrPRNumber: true},
		},
	},
	"create_pull_request": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":  {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":   {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"branch": {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"labels": {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
		},
	},
	"add_labels": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"labels":      {Required: true, Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
			"item_number": {IssueOrPRNumber: true},
		},
	},
	"add_reviewer": {
		DefaultMax: 3,
		Fields: map[string]FieldValidation{
			"reviewers":           {Required: true, Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: MaxGitHubUsernameLength},
			"pull_request_number": {IssueOrPRNumber: true},
		},
	},
	"assign_milestone": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_number":     {IssueOrPRNumber: true},
			"milestone_number": {Required: true, PositiveInteger: true},
		},
	},
	"assign_to_agent": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:issue_number,pull_number",
		Fields: map[string]FieldValidation{
			"issue_number": {IssueNumberOrTemporaryID: true},
			"pull_number":  {OptionalPositiveInteger: true},
			"agent":        {Type: "string", Sanitize: true, MaxLength: 128},
		},
	},
	"assign_to_user": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_number": {IssueOrPRNumber: true},
			"assignees":    {Type: "[]string", Sanitize: true, MaxLength: 39}, // GitHub username max length is 39
			"assignee":     {Type: "string", Sanitize: true, MaxLength: 39},   // Single assignee alternative
		},
	},
	"update_issue": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:status,title,body",
		Fields: map[string]FieldValidation{
			"status":       {Type: "string", Enum: []string{"open", "closed"}},
			"title":        {Type: "string", Sanitize: true, MaxLength: 128},
			"body":         {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"issue_number": {IssueOrPRNumber: true},
		},
	},
	"update_pull_request": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:title,body",
		Fields: map[string]FieldValidation{
			"title":               {Type: "string", Sanitize: true, MaxLength: 256},
			"body":                {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"operation":           {Type: "string", Enum: []string{"replace", "append", "prepend"}},
			"pull_request_number": {IssueOrPRNumber: true},
		},
	},
	"push_to_pull_request_branch": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"branch":              {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"message":             {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {IssueOrPRNumber: true},
		},
	},
	"create_pull_request_review_comment": {
		DefaultMax:       1,
		CustomValidation: "startLineLessOrEqualLine",
		Fields: map[string]FieldValidation{
			"path":       {Required: true, Type: "string"},
			"line":       {Required: true, PositiveInteger: true},
			"body":       {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"start_line": {OptionalPositiveInteger: true},
			"side":       {Type: "string", Enum: []string{"LEFT", "RIGHT"}},
		},
	},
	"create_discussion": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":    {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":     {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"category": {Type: "string", Sanitize: true, MaxLength: 128},
			"repo":     {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"close_discussion": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":              {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"reason":            {Type: "string", Enum: []string{"RESOLVED", "DUPLICATE", "OUTDATED", "ANSWERED"}},
			"discussion_number": {OptionalPositiveInteger: true},
		},
	},
	"close_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":         {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"issue_number": {OptionalPositiveInteger: true},
		},
	},
	"close_pull_request": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":                {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {OptionalPositiveInteger: true},
		},
	},
	"missing_tool": {
		DefaultMax: 20,
		Fields: map[string]FieldValidation{
			"tool":         {Required: false, Type: "string", Sanitize: true, MaxLength: 128},
			"reason":       {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"alternatives": {Type: "string", Sanitize: true, MaxLength: 512},
		},
	},
	"update_release": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"tag":       {Type: "string", Sanitize: true, MaxLength: 256},
			"operation": {Required: true, Type: "string", Enum: []string{"replace", "append", "prepend"}},
			"body":      {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
		},
	},
	"upload_asset": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"path": {Required: true, Type: "string"},
		},
	},
	"noop": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"message": {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
		},
	},
	"create_code_scanning_alert": {
		DefaultMax: 40,
		Fields: map[string]FieldValidation{
			"file":         {Required: true, Type: "string", Sanitize: true, MaxLength: 512},
			"line":         {Required: true, PositiveInteger: true},
			"severity":     {Required: true, Type: "string", Enum: []string{"error", "warning", "info", "note"}},
			"message":      {Required: true, Type: "string", Sanitize: true, MaxLength: 2048},
			"column":       {OptionalPositiveInteger: true},
			"ruleIdSuffix": {Type: "string", Pattern: "^[a-zA-Z0-9_-]+$", PatternError: "must contain only alphanumeric characters, hyphens, and underscores", Sanitize: true, MaxLength: 128},
		},
	},
	"link_sub_issue": {
		DefaultMax:       5,
		CustomValidation: "parentAndSubDifferent",
		Fields: map[string]FieldValidation{
			"parent_issue_number": {Required: true, IssueNumberOrTemporaryID: true},
			"sub_issue_number":    {Required: true, IssueNumberOrTemporaryID: true},
		},
	},
	"update_project": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"project":        {Required: true, Type: "string", Sanitize: true, MaxLength: 512, Pattern: "^https://github\\.com/(orgs|users)/[^/]+/projects/\\d+", PatternError: "must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/42)"},
			"content_type":   {Type: "string", Enum: []string{"issue", "pull_request", "draft_issue"}},
			"content_number": {OptionalPositiveInteger: true},
			"issue":          {OptionalPositiveInteger: true}, // Legacy
			"pull_request":   {OptionalPositiveInteger: true}, // Legacy
			"draft_title":    {Type: "string", Sanitize: true, MaxLength: 256},
			"draft_body":     {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"fields":         {Type: "object"},
		},
	},
	"create_project": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":      {Type: "string", Sanitize: true, MaxLength: 256},
			"owner":      {Type: "string", Sanitize: true, MaxLength: 128},
			"owner_type": {Type: "string", Enum: []string{"org", "user"}},
			"item_url":   {Type: "string", Sanitize: true, MaxLength: 512, Pattern: "^https://github\\.com/[^/]+/[^/]+/issues/\\d+", PatternError: "must be a full GitHub issue URL (e.g., https://github.com/owner/repo/issues/123)"},
		},
	},
	"create_project_status_update": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"project":     {Required: true, Type: "string", Sanitize: true, MaxLength: 512, Pattern: "^https://github\\.com/(orgs|users)/[^/]+/projects/\\d+", PatternError: "must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/42)"},
			"body":        {Required: true, Type: "string", Sanitize: true, MaxLength: 65536},
			"status":      {Type: "string", Enum: []string{"INACTIVE", "ON_TRACK", "AT_RISK", "OFF_TRACK", "COMPLETE"}},
			"start_date":  {Type: "string", Pattern: "^\\d{4}-\\d{2}-\\d{2}$", PatternError: "must be in YYYY-MM-DD format"},
			"target_date": {Type: "string", Pattern: "^\\d{4}-\\d{2}-\\d{2}$", PatternError: "must be in YYYY-MM-DD format"},
		},
	},
}

// GetValidationConfigJSON returns the validation configuration as indented JSON
// If enabledTypes is empty or nil, returns all validation configs
// If enabledTypes is provided, returns only configs for the specified types
func GetValidationConfigJSON(enabledTypes []string) (string, error) {
	safeOutputValidationLog.Printf("Getting validation config JSON for %d types", len(enabledTypes))

	configToMarshal := ValidationConfig
	if len(enabledTypes) > 0 {
		safeOutputValidationLog.Printf("Filtering validation configs to enabled types: %v", enabledTypes)
		configToMarshal = make(map[string]TypeValidationConfig)
		for _, typeName := range enabledTypes {
			if config, ok := ValidationConfig[typeName]; ok {
				configToMarshal[typeName] = config
			}
		}
	} else {
		safeOutputValidationLog.Print("Returning all validation configs")
	}

	data, err := json.MarshalIndent(configToMarshal, "", "  ")
	if err != nil {
		safeOutputValidationLog.Printf("Failed to marshal validation config: %v", err)
		return "", err
	}
	safeOutputValidationLog.Printf("Generated validation config JSON with %d bytes", len(data))
	return string(data), nil
}

// GetValidationConfigForType returns the validation config for a specific type
func GetValidationConfigForType(typeName string) (TypeValidationConfig, bool) {
	config, ok := ValidationConfig[typeName]
	return config, ok
}

// GetDefaultMaxForType returns the default max for a type
func GetDefaultMaxForType(typeName string) int {
	if config, ok := ValidationConfig[typeName]; ok {
		return config.DefaultMax
	}
	return 1
}
