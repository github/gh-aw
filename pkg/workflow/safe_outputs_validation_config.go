package workflow

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
)

var safeOutputValidationLog = logger.New("workflow:safe_outputs_validation_config")

// FieldValidation defines validation rules for a single field
type FieldValidation struct {
	Required                 bool     `json:"required,omitempty"`
	Type                     string   `json:"type,omitempty"`
	TypeHint                 string   `json:"typeHint,omitempty"` // Overrides the type description in error messages (e.g. "GraphQL node ID string")
	Sanitize                 bool     `json:"sanitize,omitempty"`
	MaxLength                int      `json:"maxLength,omitempty"`
	MinLength                int      `json:"minLength,omitempty"`
	PositiveInteger          bool     `json:"positiveInteger,omitempty"`
	OptionalPositiveInteger  bool     `json:"optionalPositiveInteger,omitempty"`
	AllowAuto                bool     `json:"allowAuto,omitempty"`
	IssueOrPRNumber          bool     `json:"issueOrPRNumber,omitempty"`
	IssueNumberOrTemporaryID bool     `json:"issueNumberOrTemporaryId,omitempty"`
	Enum                     []string `json:"enum,omitempty"`
	ItemType                 string   `json:"itemType,omitempty"`
	ItemSanitize             bool     `json:"itemSanitize,omitempty"`
	ItemMaxLength            int      `json:"itemMaxLength,omitempty"`
	Pattern                  string   `json:"pattern,omitempty"`
	PatternError             string   `json:"patternError,omitempty"`
	TemporaryID              bool     `json:"temporaryId,omitempty"`
	// RejectIfOversized rejects the field outright when the raw value exceeds MaxLength,
	// instead of silently truncating it via sanitization. Used for external-system fields
	// (e.g. Linear) where truncation could turn an oversized/placeholder value into a
	// deceptively short but "valid" operation.
	RejectIfOversized bool `json:"rejectIfOversized,omitempty"`
	// StripOnError marks optional enrichment fields (e.g. confidence, rationale) that should be
	// silently dropped when they fail validation instead of rejecting the entire item.
	// Serialised as "x-strip-on-error" to follow the x- extension convention used in JSON Schema.
	StripOnError bool `json:"x-strip-on-error,omitempty"`
}

// TypeValidationConfig defines validation configuration for a safe output type
type TypeValidationConfig struct {
	DefaultMax       int                        `json:"defaultMax"`
	Fields           map[string]FieldValidation `json:"fields"`
	CustomValidation string                     `json:"customValidation,omitempty"`
	DataEnabled      bool                       `json:"dataEnabled,omitempty"`
	DataSchema       map[string]any             `json:"dataSchema,omitempty"`
}

// Constants for validation
const (
	MaxBodyLength           = 65000
	MaxGitHubUsernameLength = 39
	MaxGitHubTeamSlugLength = 100
	MinIssueBodyLength      = 20 // Minimum body length for create_issue to prevent placeholder-only submissions
	MinDiscussionBodyLength = 64 // Minimum body length for create_discussion to prevent placeholder-only submissions
	MinReleaseBodyLength    = 20 // Minimum body length for update_release to prevent placeholder-only submissions
)

// ValidationConfig contains all safe output type validation rules
// This is the single source of truth for validation rules
var ValidationConfig = map[string]TypeValidationConfig{
	"ado_create_work_item": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":        {Required: true, Type: "string", Sanitize: true, MinLength: 6, MaxLength: 255},
			"description":  {Required: true, Type: "string", Sanitize: true, MinLength: 31, MaxLength: MaxBodyLength},
			"tags":         {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 256},
			"temporary_id": {Required: true, Type: "string", Pattern: "^#aw_[A-Za-z0-9_]{3,12}$", TemporaryID: true},
		},
	},
	"ado_update_work_item": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:title,body,state,area_path,iteration_path,assignee,tags",
		Fields: map[string]FieldValidation{
			"id":             {Required: true, IssueNumberOrTemporaryID: true},
			"title":          {Type: "string", Sanitize: true, MinLength: 1, MaxLength: 255},
			"body":           {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"state":          {Type: "string", Sanitize: true, MaxLength: 128},
			"area_path":      {Type: "string", Sanitize: true, MaxLength: 512},
			"iteration_path": {Type: "string", Sanitize: true, MaxLength: 512},
			"assignee":       {Type: "string", Sanitize: true, MaxLength: 256},
			"tags":           {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 256},
		},
	},
	"ado_comment_on_work_item": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"work_item_id": {Required: true, IssueNumberOrTemporaryID: true},
			"body":         {Required: true, Type: "string", Sanitize: true, MinLength: 10, MaxLength: MaxBodyLength},
		},
	},
	"ado_assign_work_item": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"work_item_id": {Required: true, IssueNumberOrTemporaryID: true},
			"assignee":     {Required: true, Type: "string", Sanitize: true, MinLength: 1, MaxLength: 256},
		},
	},
	"ado_link_work_items": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"source_id": {Required: true, IssueNumberOrTemporaryID: true},
			"target_id": {Required: true, IssueNumberOrTemporaryID: true},
			"link_type": {Required: true, Type: "string", Enum: []string{"parent", "child", "related", "predecessor", "successor", "duplicate", "duplicate-of"}},
			"comment":   {Type: "string", Sanitize: true, MinLength: 5, MaxLength: 1024},
		},
	},
	"ado_upload_workitem_attachment": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"work_item_id": {Required: true, IssueNumberOrTemporaryID: true},
			"file_path":    {Required: true, Type: "string", MaxLength: 1024},
			"staged_file":  {Required: true, Type: "string", Pattern: "^[A-Za-z0-9._/-]+$"},
			"comment":      {Type: "string", Sanitize: true, MinLength: 3, MaxLength: 1024},
		},
	},
	"linear_create_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title": {Required: true, Type: "string", Sanitize: true, MaxLength: 128, RejectIfOversized: true},
			"body":  {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength, MinLength: MinIssueBodyLength, RejectIfOversized: true},
		},
	},
	"linear_add_comment": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body": {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength, RejectIfOversized: true},
		},
	},
	"linear_update_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title": {Type: "string", Sanitize: true, MaxLength: 128, RejectIfOversized: true},
			"body":  {Type: "string", Sanitize: true, MaxLength: MaxBodyLength, RejectIfOversized: true},
		},
	},
	"create_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":  {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":   {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength, MinLength: MinIssueBodyLength},
			"labels": {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
			"fields": {Type: "array"},
			"parent": {IssueOrPRNumber: true},
			// blocked_by accepts an issue number, temporary ID, owner/repo#number, issue URL,
			// or an array of these; reference parsing is handled by the create_issue handler.
			"blocked_by":   {},
			"temporary_id": {Type: "string"},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"create_agent_session": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body": {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"repo": {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"approve_workflow_run": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"run_id": {Required: true, PositiveInteger: true},
		},
	},
	"add_comment": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":         {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"item_number":  {IssueOrPRNumber: true},
			"pr_number":    {IssueOrPRNumber: true},
			"pr":           {IssueOrPRNumber: true},
			"temporary_id": {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
			"reply_to_id":  {Type: "string", MaxLength: 256}, // Optional: node ID of discussion comment to reply to (threading)
			"target":       {Type: "string", Enum: []string{"status"}},
			"comment_id":   {OptionalPositiveInteger: true},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"jira_create_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"project_key": {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"issue_type":  {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"summary":     {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"description": {Type: "string", MinLength: 1, MaxLength: 32767, Pattern: ".*\\S.*", PatternError: "must not be empty"},
		},
	},
	"jira_update_issue": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:summary,description",
		Fields: map[string]FieldValidation{
			"issue_key":   {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"summary":     {Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"description": {Type: "string", MinLength: 1, MaxLength: 32767, Pattern: ".*\\S.*", PatternError: "must not be empty"},
		},
	},
	"jira_add_comment": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_key": {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"body":      {Required: true, Type: "string", MinLength: 1, MaxLength: 32767, Pattern: ".*\\S.*", PatternError: "must not be empty"},
		},
	},
	"jira_add_label": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_key": {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"label":     {Required: true, Type: "string", MinLength: 1, MaxLength: 255, Pattern: "^[A-Za-z0-9_.-]+$", PatternError: "must contain only letters, numbers, periods, hyphens, and underscores"},
		},
	},
	"comment_memory": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":        {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"memory_id":   {Type: "string", Sanitize: true, MaxLength: 128, Pattern: "^[a-zA-Z0-9_-]+$", PatternError: "must contain only alphanumeric characters, hyphens, and underscores"},
			"item_number": {IssueOrPRNumber: true},
			"repo":        {Type: "string", MaxLength: 256},
		},
	},
	"create_pull_request": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":          {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":           {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"branch":         {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"base":           {Type: "string", Sanitize: true, MaxLength: 128},
			"stack_position": {OptionalPositiveInteger: true},
			"stack_root":     {Type: "string", Sanitize: true, MaxLength: 256},
			"dependencies":   {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 256},
			"labels":         {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
			"draft":          {Type: "boolean"},
			"repo":           {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
			"temporary_id":   {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
		},
	},
	"add_labels": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"labels":      {Required: true, Type: "array"}, // Item-level validation/sanitization handled by JS issue-intent label normalization.
			"item_number": {IssueNumberOrTemporaryID: true},
			"repo":        {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"add_reviewer": {
		DefaultMax:       3,
		CustomValidation: "requiresOneOf:reviewers,team_reviewers",
		Fields: map[string]FieldValidation{
			"reviewers":           {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: MaxGitHubUsernameLength},
			"team_reviewers":      {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: MaxGitHubTeamSlugLength},
			"pull_request_number": {IssueOrPRNumber: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"assign_milestone": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:milestone_number,milestone_title",
		Fields: map[string]FieldValidation{
			"issue_number":     {IssueNumberOrTemporaryID: true},
			"milestone_number": {OptionalPositiveInteger: true},
			"milestone_title":  {Type: "string", Sanitize: true, MaxLength: 128},
			"repo":             {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"set_issue_type": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"issue_number": {IssueOrPRNumber: true},
			"issue_type":   {Required: true, Type: "string", Sanitize: true, MaxLength: 128}, // Empty string clears the type
			"rationale":    {Type: "string", Sanitize: true, MaxLength: 280, StripOnError: true},
			"confidence":   {Type: "string", Enum: []string{"LOW", "MEDIUM", "HIGH"}, StripOnError: true},
			"suggest":      {Type: "boolean"},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"set_issue_field": {
		DefaultMax:       5,
		CustomValidation: "requiresOneOf:field_name,field_node_id",
		Fields: map[string]FieldValidation{
			"issue_number":  {IssueOrPRNumber: true},
			"field_name":    {Type: "string", Sanitize: true, MaxLength: 128},
			"field_node_id": {Type: "string", MaxLength: 256},
			"value":         {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"rationale":     {Type: "string", Sanitize: true, MaxLength: 280, StripOnError: true},
			"confidence":    {Type: "string", Enum: []string{"LOW", "MEDIUM", "HIGH"}, StripOnError: true},
			"suggest":       {Type: "boolean"},
			"repo":          {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"assign_to_agent": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:issue_number,pull_number",
		Fields: map[string]FieldValidation{
			"issue_number":      {IssueNumberOrTemporaryID: true},
			"pull_number":       {OptionalPositiveInteger: true},
			"agent":             {Type: "string", Sanitize: true, MaxLength: 128},
			"rationale":         {Type: "string", Sanitize: true, MaxLength: 280, StripOnError: true},
			"confidence":        {Type: "string", Enum: []string{"LOW", "MEDIUM", "HIGH"}, StripOnError: true},
			"suggest":           {Type: "boolean"},
			"pull_request_repo": {Type: "string", MaxLength: 256}, // Optional: repository where the PR should be created
			"repo":              {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"assign_to_user": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_number": {IssueOrPRNumber: true},
			"assignees":    {Type: "[]string", Sanitize: true, MaxLength: 39}, // GitHub username max length is 39
			"assignee":     {Type: "string", Sanitize: true, MaxLength: 39},   // Single assignee alternative
			"rationale":    {Type: "string", Sanitize: true, MaxLength: 280, StripOnError: true},
			"confidence":   {Type: "string", Enum: []string{"LOW", "MEDIUM", "HIGH"}, StripOnError: true},
			"suggest":      {Type: "boolean"},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"update_issue": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:status,title,body,labels,assignees,milestone",
		Fields: map[string]FieldValidation{
			"status":       {Type: "string", Enum: []string{"open", "closed"}},
			"title":        {Type: "string", Sanitize: true, MaxLength: 128},
			"body":         {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"operation":    {Type: "string", Enum: []string{"replace", "append", "prepend", "replace-island"}},
			"labels":       {Type: "array"},
			"assignees":    {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: MaxGitHubUsernameLength},
			"milestone":    {OptionalPositiveInteger: true},
			"issue_number": {IssueOrPRNumber: true},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"update_pull_request": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:title,body,update_branch",
		Fields: map[string]FieldValidation{
			"title":               {Type: "string", Sanitize: true, MaxLength: 256},
			"body":                {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"operation":           {Type: "string", Enum: []string{"replace", "append", "prepend", "replace-island"}},
			"update_branch":       {Type: "boolean"},
			"draft":               {Type: "boolean"},
			"pull_request_number": {IssueOrPRNumber: true},
			"pr_number":           {IssueOrPRNumber: true},
			"pr":                  {IssueOrPRNumber: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"merge_pull_request": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"pull_request_number": {IssueOrPRNumber: true},
			"merge_method":        {Type: "string", Enum: []string{"merge", "squash", "rebase"}},
			"commit_title":        {Type: "string", Sanitize: true, MaxLength: 256},
			"commit_message":      {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"push_to_pull_request_branch": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"message":             {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {IssueOrPRNumber: true},
			"branch":              {Type: "string", Sanitize: true, MaxLength: 256}, // Optional: stripped before MCP call; validated for type/length when present.
			"repo":                {Type: "string", MaxLength: 256},
		},
	},
	"create_pull_request_review_comment": {
		DefaultMax:       1,
		CustomValidation: "startLineLessOrEqualLine",
		Fields: map[string]FieldValidation{
			"path":                {Required: true, Type: "string"},
			"line":                {Required: true, PositiveInteger: true},
			"body":                {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {OptionalPositiveInteger: true},
			"start_line":          {OptionalPositiveInteger: true},
			"side":                {Type: "string", Enum: []string{"LEFT", "RIGHT"}},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"submit_pull_request_review": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":                {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"event":               {Type: "string", Enum: []string{"APPROVE", "REQUEST_CHANGES", "COMMENT"}},
			"pull_request_number": {IssueOrPRNumber: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"reply_to_pull_request_review_comment": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"comment_id":          {Required: true, PositiveInteger: true},
			"body":                {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {OptionalPositiveInteger: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"resolve_pull_request_review_thread": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"thread_id": {Required: true, Type: "string"},
		},
	},
	"create_discussion": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":    {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"body":     {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength, MinLength: MinDiscussionBodyLength},
			"category": {Type: "string", Sanitize: true, MaxLength: 128},
			"repo":     {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"close_discussion": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":              {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"reason":            {Type: "string", Enum: []string{"RESOLVED", "DUPLICATE", "OUTDATED", "ANSWERED"}},
			"discussion_number": {OptionalPositiveInteger: true},
			"repo":              {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"close_issue": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":         {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"issue_number": {OptionalPositiveInteger: true},
			"duplicate_of": {IssueOrPRNumber: true},
			"rationale":    {Type: "string", Sanitize: true, MaxLength: 280, StripOnError: true},
			"confidence":   {Type: "string", Enum: []string{"LOW", "MEDIUM", "HIGH"}, StripOnError: true},
			"suggest":      {Type: "boolean"},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"close_pull_request": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"body":                {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {OptionalPositiveInteger: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"dispatch_workflow": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"workflow_name": {Required: true, Type: "string", Sanitize: true, MinLength: 1, MaxLength: 256, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"inputs":        {Type: "object"},
			"ref":           {Type: "string", MinLength: 1, MaxLength: 256, Pattern: "^[^\\x00-\\x20\\x7f~^:?*\\[\\\\]+$", PatternError: "must be a valid git ref"},
		},
	},
	"call_workflow": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"workflow_name": {Required: true, Type: "string", Sanitize: true, MinLength: 1, MaxLength: 256, Pattern: ".*\\S.*", PatternError: "must not be empty"},
			"inputs":        {Type: "object"},
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
			"body":      {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength, MinLength: MinReleaseBodyLength},
		},
	},
	"upload_asset": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"path": {Required: true, Type: "string"},
		},
	},
	"upload_artifact": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"path":         {Type: "string"},
			"filters":      {Type: "object"},
			"temporary_id": {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
		},
	},
	"upload_code_coverage": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"file": {
				Required:     true,
				Type:         "string",
				MaxLength:    4096,
				Pattern:      `^(?!/)(?!.*\.\.).+$`,
				PatternError: `must be a relative path within the upload-code-coverage staging directory (no leading "/" or ".." segments)`,
			},
			"language": {Required: true, Type: "string", Sanitize: true, MaxLength: 64},
			"label":    {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
		},
	},
	"push_repo_memory": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"memory_id": {Type: "string", Sanitize: true, MaxLength: 128},
		},
	},
	"create_check_run": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"conclusion":          {Required: true, Type: "string", Enum: []string{"success", "failure", "neutral", "cancelled", "skipped", "timed_out", "action_required"}},
			"title":               {Required: true, Type: "string", Sanitize: true, MaxLength: 256},
			"summary":             {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"text":                {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"pull_request_number": {IssueOrPRNumber: true},
			"pr_number":           {IssueOrPRNumber: true},
			"pr":                  {IssueOrPRNumber: true},
			"pull_number":         {IssueOrPRNumber: true},
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
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"update_project": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"project":           {Required: true, Type: "string", Sanitize: true, MaxLength: 512, Pattern: "^(https://[^/]+/(orgs|users)/[^/]+/projects/\\d+|#?aw_[A-Za-z0-9_]{3,12})$", PatternError: "must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/42) or temporary project ID (e.g., #aw_project1)"},
			"operation":         {Type: "string", Enum: []string{"create_fields", "create_view"}},
			"content_type":      {Type: "string", Enum: []string{"issue", "pull_request", "draft_issue"}},
			"content_number":    {IssueNumberOrTemporaryID: true},
			"target_repo":       {Type: "string", Pattern: "^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$"},
			"issue":             {OptionalPositiveInteger: true}, // Legacy
			"pull_request":      {OptionalPositiveInteger: true}, // Legacy
			"draft_title":       {Type: "string", Sanitize: true, MaxLength: 256},
			"draft_body":        {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"draft_issue_id":    {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
			"temporary_id":      {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
			"fields":            {Type: "object"},
			"field_definitions": {Type: "array"},
			"view":              {Type: "object"},
			"create_if_missing": {Type: "boolean"},
		},
	},
	"create_project": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"title":        {Type: "string", Sanitize: true, MaxLength: 256},
			"owner":        {Type: "string", Sanitize: true, MaxLength: 128},
			"owner_type":   {Type: "string", Enum: []string{"org", "user"}},
			"item_url":     {Type: "string", Sanitize: true, MaxLength: 512},
			"temporary_id": {Type: "string", Pattern: "^#?aw_[A-Za-z0-9_]{3,12}$"},
		},
	},
	"create_project_status_update": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"project":     {Required: true, Type: "string", Sanitize: true, MaxLength: 512, Pattern: "^https://[^/]+/(orgs|users)/[^/]+/projects/\\d+", PatternError: "must be a full GitHub project URL (e.g., https://github.com/orgs/myorg/projects/42)"},
			"body":        {Required: true, Type: "string", Sanitize: true, MaxLength: 65536},
			"status":      {Type: "string", Enum: []string{"INACTIVE", "ON_TRACK", "AT_RISK", "OFF_TRACK", "COMPLETE"}},
			"start_date":  {Type: "string", Pattern: "^\\d{4}-\\d{2}-\\d{2}$", PatternError: "must be in YYYY-MM-DD format"},
			"target_date": {Type: "string", Pattern: "^\\d{4}-\\d{2}-\\d{2}$", PatternError: "must be in YYYY-MM-DD format"},
		},
	},
	"update_discussion": {
		DefaultMax:       1,
		CustomValidation: "requiresOneOf:title,body,labels",
		Fields: map[string]FieldValidation{
			"title":             {Type: "string", Sanitize: true, MaxLength: 128},
			"body":              {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"labels":            {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: 128},
			"discussion_number": {IssueOrPRNumber: true},
			"repo":              {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"remove_labels": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"labels":      {Required: true, Type: "array"}, // Item-level validation/sanitization handled by JS issue-intent label normalization.
			"item_number": {IssueNumberOrTemporaryID: true},
			"repo":        {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"replace_label": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"label_to_remove": {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"label_to_add":    {Required: true, Type: "string", Sanitize: true, MaxLength: 128},
			"item_number":     {IssueNumberOrTemporaryID: true},
			"repo":            {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"unassign_from_user": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"issue_number": {IssueOrPRNumber: true},
			"assignees":    {Type: "array", ItemType: "string", ItemSanitize: true, ItemMaxLength: MaxGitHubUsernameLength},
			"assignee":     {Type: "string", Sanitize: true, MaxLength: MaxGitHubUsernameLength},
			"repo":         {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"hide_comment": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"comment_id": {Required: true, Type: "string", MaxLength: 256, TypeHint: "GraphQL node ID string (e.g. 'IC_kwDOABCD123456'); numeric REST comment IDs are accepted but may not resolve for all comment types (e.g. PR review comments)"},
			"reason":     {Type: "string", Enum: []string{"SPAM", "ABUSE", "OFF_TOPIC", "OUTDATED", "RESOLVED", "LOW_QUALITY"}},
			"repo":       {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"missing_data": {
		DefaultMax: 20,
		Fields: map[string]FieldValidation{
			"data_type":    {Type: "string", Sanitize: true, MaxLength: 128},
			"reason":       {Type: "string", Sanitize: true, MaxLength: 256},
			"context":      {Type: "string", Sanitize: true, MaxLength: 256},
			"alternatives": {Type: "string", Sanitize: true, MaxLength: 256},
		},
	},
	"report_incomplete": {
		DefaultMax: 5,
		Fields: map[string]FieldValidation{
			"reason":  {Required: true, Type: "string", Sanitize: true, MaxLength: 1024},
			"details": {Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
		},
	},
	"autofix_code_scanning_alert": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"alert_number":    {PositiveInteger: true},
			"fix_description": {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"fix_code":        {Required: true, Type: "string", MaxLength: MaxBodyLength},
		},
	},
	"mark_pull_request_as_ready_for_review": {
		DefaultMax: 1,
		Fields: map[string]FieldValidation{
			"pull_request_number": {IssueOrPRNumber: true},
			"reason":              {Required: true, Type: "string", Sanitize: true, MaxLength: MaxBodyLength},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
	"dismiss_pull_request_review": {
		DefaultMax: 10,
		Fields: map[string]FieldValidation{
			"review_id":           {OptionalPositiveInteger: true, AllowAuto: true},
			"justification":       {Required: true, Type: "string", Sanitize: true, MinLength: 20, MaxLength: MaxBodyLength},
			"author":              {Type: "string", Sanitize: true, MaxLength: 128},
			"pull_request_number": {IssueOrPRNumber: true},
			"repo":                {Type: "string", MaxLength: 256}, // Optional: target repository in format "owner/repo"
		},
	},
}

// validationConfigJSONCache caches GetValidationConfigJSONWithDataSchema results keyed by the
// sorted, comma-joined enabledTypes string. ValidationConfig is a package-level constant so
// the output is deterministic for a given set of types; caching avoids repeated
// json.MarshalIndent calls on every workflow compilation.
var validationConfigJSONCache sync.Map // key: string → value: string

// GetValidationConfigJSONWithDataSchema behaves like GetValidationConfigJSONWithDataSchema and additionally
// injects a normalized data schema into body-bearing safe-output types.
//
//nolint:largefunc // Existing validation serialization flow remains linear and explicit.
func GetValidationConfigJSONWithDataSchema(enabledTypes []string, mentions map[string]any, dataEnabled bool, dataSchema map[string]any) (string, error) {
	safeOutputValidationLog.Printf("Getting validation config JSON for %d types (mentions=%t)", len(enabledTypes), len(mentions) > 0)

	// Cache only the schema-only path; mentions are workflow-specific and cheap to remarshal.
	if len(mentions) == 0 && !dataEnabled && dataSchema == nil {
		cacheKey := buildValidationConfigCacheKey(enabledTypes)
		if cached, ok := validationConfigJSONCache.Load(cacheKey); ok {
			safeOutputValidationLog.Print("Returning cached validation config JSON")
			result, ok := cached.(string)
			if !ok {
				// The cache exclusively stores string values; a non-string indicates a programmer error.
				return "", fmt.Errorf("validationConfigJSONCache: unexpected type %T for key %s", cached, cacheKey)
			}
			return result, nil
		}
	}

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
	if dataEnabled || dataSchema != nil {
		withDataSchema := make(map[string]TypeValidationConfig, len(configToMarshal))
		for typeName, typeConfig := range configToMarshal {
			copied := typeConfig
			if isDataSchemaEnabledType(typeName) {
				copied.DataEnabled = dataEnabled
				copied.DataSchema = dataSchema
			}
			withDataSchema[typeName] = copied
		}
		configToMarshal = withDataSchema
	}

	var data []byte
	var err error
	if len(mentions) > 0 {
		composite := make(map[string]any, len(configToMarshal)+1)
		for k, v := range configToMarshal {
			composite[k] = v
		}
		composite["mentions"] = mentions
		data, err = json.MarshalIndent(composite, "", "  ")
	} else {
		data, err = json.MarshalIndent(configToMarshal, "", "  ")
	}
	if err != nil {
		safeOutputValidationLog.Printf("Failed to marshal validation config: %v", err)
		return "", err
	}
	result := string(data)
	safeOutputValidationLog.Printf("Generated validation config JSON with %d bytes", len(result))
	if len(mentions) == 0 && !dataEnabled && dataSchema == nil {
		validationConfigJSONCache.Store(buildValidationConfigCacheKey(enabledTypes), result)
	}
	return result, nil
}

// buildValidationConfigCacheKey returns a stable cache key for GetValidationConfigJSON.
// For nil/empty enabledTypes the key is "" (full config). Otherwise the sorted type
// names are joined with commas so the order the caller provides does not affect caching.
func buildValidationConfigCacheKey(enabledTypes []string) string {
	if len(enabledTypes) == 0 {
		return ""
	}
	sorted := slices.Sorted(slices.Values(enabledTypes))
	return strings.Join(sorted, ",")
}
