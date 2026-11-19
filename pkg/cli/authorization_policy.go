package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path"
	"regexp"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
)

const maxAuthorizationPolicySize = 1 << 20

// AuthorizationPolicy has fixed v1 semantics: every eligible output must use
// this literal authority. There are no repository-local exemption switches.
type AuthorizationPolicy struct {
	Version           int                              `json:"version"`
	PolicyRevision    string                           `json:"policy_revision"`
	RepositoryID      string                           `json:"repository_id"`
	Authority         workflow.SafeOutputAuthorization `json:"authority"`
	RequiredWorkflows []string                         `json:"required_workflows"`
}

type AuthorizationFinding struct {
	Code                string `json:"code"`
	Workflow            string `json:"workflow,omitempty"`
	Route               string `json:"route,omitempty"`
	Message             string `json:"message"`
	ObservedBinding     string `json:"observed_binding,omitempty"`
	ExpectedFingerprint string `json:"expected_policy_fingerprint"`
}

type AuthorizationWorkflowResult struct {
	Source          string                     `json:"source"`
	SourceDigest    string                     `json:"source_digest"`
	Lock            string                     `json:"lock"`
	LockDigest      string                     `json:"lock_digest,omitempty"`
	GeneratedDigest string                     `json:"generated_digest,omitempty"`
	Imports         map[string]string          `json:"imports"`
	Routes          []AuthorizationRouteResult `json:"routes"`
}

type AuthorizationRouteResult struct {
	workflow.AuthorizationRoute
	Binding string `json:"binding"`
}

// AuthorizationPolicyResult is a versioned, fail-closed snapshot report.
type AuthorizationPolicyResult struct {
	Version             int                           `json:"version"`
	Status              string                        `json:"status"`
	CandidateCommit     string                        `json:"candidate_commit"`
	SnapshotDigest      string                        `json:"snapshot_digest,omitempty"`
	RepositoryID        string                        `json:"repository_id"`
	Repository          string                        `json:"repository,omitempty"`
	PolicyRevision      string                        `json:"policy_revision"`
	PolicyDigest        string                        `json:"policy_digest"`
	PolicyFingerprint   string                        `json:"policy_fingerprint"`
	CompilerVersion     string                        `json:"compiler_version"`
	CompilerDigest      string                        `json:"compiler_digest"`
	Eligible            int                           `json:"eligible"`
	Exempt              int                           `json:"exempt"`
	Staged              int                           `json:"staged"`
	Workflows           []AuthorizationWorkflowResult `json:"workflows"`
	Findings            []AuthorizationFinding        `json:"findings"`
	UngovernedWorkflows []string                      `json:"ungoverned_workflows"`
}

func authorizationDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func authorizationBinding(authority *workflow.SafeOutputAuthorization) string {
	if authority == nil {
		return "unconfigured"
	}
	if authority.FromEnvironment {
		return "environment"
	}
	return authorizationDigest([]byte(authority.Endpoint + "\x00" + authority.Audience))
}

var policyRepositoryIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,19}$`)
var policyRepositorySlugPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)
var policyRevisionPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,127}$`)

type authorizationPolicyJSON struct {
	Version        int    `json:"version"`
	PolicyRevision string `json:"policy_revision"`
	RepositoryID   string `json:"repository_id"`
	Authority      struct {
		Endpoint string `json:"endpoint"`
		Audience string `json:"audience"`
	} `json:"authority"`
	RequiredWorkflows []string `json:"required_workflows"`
}

func parseAuthorizationPolicy(data []byte) (*AuthorizationPolicy, error) {
	invalid := errors.New("invalid authorization policy: require closed v1 schema, a literal HTTPS authority, repository ID, revision, and unique canonical .github/workflows/*.md paths")
	if len(data) > maxAuthorizationPolicySize || rejectDuplicateJSONKeys(data) != nil {
		return nil, invalid
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil || len(fields) != 5 {
		return nil, invalid
	}
	for _, key := range []string{"version", "policy_revision", "repository_id", "authority", "required_workflows"} {
		if _, ok := fields[key]; !ok {
			return nil, invalid
		}
	}
	var authorityFields map[string]json.RawMessage
	if json.Unmarshal(fields["authority"], &authorityFields) != nil || len(authorityFields) != 2 ||
		authorityFields["endpoint"] == nil || authorityFields["audience"] == nil {
		return nil, invalid
	}
	// Decode the authority separately so compiler-owned JSON metadata cannot
	// expand the policy's closed schema.
	var raw authorizationPolicyJSON
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&raw); err != nil {
		return nil, invalid
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("invalid authorization policy: trailing JSON data")
	}
	if raw.Version != 1 ||
		!policyRevisionPattern.MatchString(raw.PolicyRevision) || !policyRepositoryIDPattern.MatchString(raw.RepositoryID) ||
		len(raw.RequiredWorkflows) == 0 || len(raw.RequiredWorkflows) > 4096 ||
		workflow.ValidateSafeOutputAuthorization(raw.Authority.Endpoint, raw.Authority.Audience) != nil {
		return nil, invalid
	}
	seen := map[string]struct{}{}
	for _, file := range raw.RequiredWorkflows {
		if len(file) > 256 || path.Dir(file) != constants.WorkflowsDir || path.Clean(file) != file ||
			!strings.HasSuffix(file, ".md") || strings.ContainsAny(file, "\\\x00\r\n") || setutil.Contains(seen, file) {
			return nil, invalid
		}
		seen[file] = struct{}{}
	}
	return &AuthorizationPolicy{
		Version: raw.Version, PolicyRevision: raw.PolicyRevision, RepositoryID: raw.RepositoryID,
		Authority:         workflow.SafeOutputAuthorization{Endpoint: raw.Authority.Endpoint, Audience: raw.Authority.Audience},
		RequiredWorkflows: raw.RequiredWorkflows,
	}, nil
}

func rejectDuplicateJSONKeys(data []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var value func(int) error
	value = func(depth int) error {
		if depth > 8 {
			return errors.New("authorization policy nesting limit exceeded")
		}
		token, err := decoder.Token()
		if err != nil {
			return err
		}
		delim, ok := token.(json.Delim)
		if !ok {
			return nil
		}
		seen := map[string]struct{}{}
		for decoder.More() {
			if delim == '{' {
				key, err := decoder.Token()
				if err != nil {
					return err
				}
				name, ok := key.(string)
				if !ok || setutil.Contains(seen, name) {
					return errors.New("duplicate JSON key")
				}
				seen[name] = struct{}{}
			}
			if err := value(depth + 1); err != nil {
				return err
			}
		}
		_, err = decoder.Token()
		return err
	}
	if err := value(0); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing JSON")
	}
	return nil
}

func (r *AuthorizationPolicyResult) addFinding(code, file, route, message, observed string) {
	r.Findings = append(r.Findings, AuthorizationFinding{
		Code: code, Workflow: file, Route: route, Message: message,
		ObservedBinding: observed, ExpectedFingerprint: r.PolicyFingerprint,
	})
	switch code {
	case "verification_incomplete", "inventory_unresolved", "policy_invalid":
		r.Status = "incomplete"
	default:
		if r.Status != "incomplete" {
			r.Status = "violations"
		}
	}
}
