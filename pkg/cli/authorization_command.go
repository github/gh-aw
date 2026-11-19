package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/spf13/cobra"
)

func authorizationPolicyModeRequested(cmd *cobra.Command) bool {
	for _, name := range []string{"authorization-policy", "authorization-repository-id", "authorization-repository", "authorization-commit", "authorization-policy-sha256", "authorization-actions-workflow"} {
		if cmd.Flags().Changed(name) {
			return true
		}
	}
	return false
}

func digestAuthorizationExecutable(path string) (digest string, resultErr error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func runAuthorizationPolicyValidation(cmd *cobra.Command, args []string, policyPath string) error {
	result := AuthorizationPolicyResult{
		Version: 1, Status: "compliant", CompilerVersion: workflow.GetVersion(),
		Workflows: []AuthorizationWorkflowResult{}, Findings: []AuthorizationFinding{},
		UngovernedWorkflows: []string{},
	}
	verifyAuthorizationCommand(cmd, args, policyPath, &result)
	jsonOutput, _ := cmd.Flags().GetBool("json")
	if jsonOutput {
		if err := json.NewEncoder(cmd.OutOrStdout()).Encode(result); err != nil {
			return fmt.Errorf("writing authorization verification result: %w", err)
		}
	} else {
		if _, err := fmt.Fprintf(cmd.OutOrStdout(), "Authorization policy: %s (%d eligible, %d exempt, %d staged routes)\n", result.Status, result.Eligible, result.Exempt, result.Staged); err != nil {
			return err
		}
		for _, finding := range result.Findings {
			if _, err := fmt.Fprintf(cmd.OutOrStdout(), "%s %s %s: %s\n", finding.Code, finding.Workflow, finding.Route, finding.Message); err != nil {
				return err
			}
		}
	}
	if result.Status != "compliant" {
		return fmt.Errorf("authorization policy verification: %s", result.Status)
	}
	return nil
}

func verifyAuthorizationCommand(cmd *cobra.Command, args []string, policyPath string, result *AuthorizationPolicyResult) {
	if len(args) != 0 || constants.GetWorkflowDir() != constants.WorkflowsDir {
		result.addFinding("verification_incomplete", "", "", "Policy mode requires the complete .github/workflows inventory; subsets and directory overrides are not allowed.", "")
		return
	}
	for _, flag := range []string{"dir", "engine", "strict", "fail-fast", "stats", "allow-action-refs", "validate-images"} {
		if cmd.Flags().Changed(flag) {
			result.addFinding("verification_incomplete", "", "", "Policy mode does not accept compilation or scope overrides.", "")
			return
		}
	}
	data, err := readBoundedAuthorizationFile(policyPath, maxAuthorizationPolicySize)
	if err != nil {
		result.addFinding("policy_invalid", "", "", "Cannot read bounded protected policy input.", "")
		return
	}
	policyDigest := authorizationDigest(data)
	expectedDigest, _ := cmd.Flags().GetString("authorization-policy-sha256")
	if expectedDigest != "" && policyDigest != "sha256:"+strings.TrimPrefix(expectedDigest, "sha256:") {
		result.addFinding("policy_invalid", "", "", "Policy digest does not match the checker-pinned digest.", "")
		return
	}
	policy, err := parseAuthorizationPolicy(data)
	if err != nil {
		result.addFinding("policy_invalid", "", "", err.Error(), "")
		return
	}
	result.PolicyDigest = policyDigest
	result.PolicyRevision = policy.PolicyRevision
	result.PolicyFingerprint = authorizationBinding(&policy.Authority)
	identity, _ := cmd.Flags().GetString("authorization-repository-id")
	result.RepositoryID = identity
	if identity == "" || identity != policy.RepositoryID {
		result.addFinding("verification_incomplete", "", "", "Checker must supply a verified platform repository ID matching the protected policy.", "")
		return
	}
	repository, _ := cmd.Flags().GetString("authorization-repository")
	if repository != "" && !policyRepositorySlugPattern.MatchString(repository) {
		result.addFinding("verification_incomplete", "", "", "Checker repository context must be a verified owner/repository slug.", "")
		return
	}
	result.Repository = repository
	executable, err := os.Executable()
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot identify trusted compiler executable.", "")
		return
	}
	digest, err := digestAuthorizationExecutable(executable)
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot fingerprint trusted compiler executable.", "")
		return
	}
	result.CompilerDigest = digest
	verifyAuthorizationCandidate(cmd, policy, result)
}

func verifyAuthorizationCandidate(cmd *cobra.Command, policy *AuthorizationPolicy, result *AuthorizationPolicyResult) {
	rootBytes, err := authorizationGit(cmd.Context(), ".", "rev-parse", "--show-toplevel")
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Candidate must be an available Git repository.", "")
		return
	}
	root := strings.TrimSpace(string(rootBytes))
	commit, _ := cmd.Flags().GetString("authorization-commit")
	if commit == "" {
		head, err := authorizationGit(cmd.Context(), root, "rev-parse", "--verify", "HEAD^{commit}")
		if err != nil {
			result.addFinding("verification_incomplete", "", "", "Cannot resolve immutable candidate commit.", "")
			return
		}
		commit = strings.TrimSpace(string(head))
	}
	if !authorizationCommitPattern.MatchString(commit) {
		result.addFinding("verification_incomplete", "", "", "Candidate revision must be a full lowercase commit object ID.", "")
		return
	}
	resolved, err := authorizationGit(cmd.Context(), root, "rev-parse", "--verify", commit+"^{commit}")
	if err != nil || strings.TrimSpace(string(resolved)) != commit {
		result.addFinding("verification_incomplete", "", "", "Candidate revision is not an available immutable commit.", "")
		return
	}
	result.CandidateCommit = commit
	snapshot, err := os.MkdirTemp("", "gh-aw-authorization-")
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot create isolated candidate snapshot.", "")
		return
	}
	defer func() {
		if err := os.RemoveAll(snapshot); err != nil {
			result.addFinding("verification_incomplete", "", "", "Cannot clean isolated candidate snapshot.", "")
		}
	}()
	snapshotDigest, err := materializeAuthorizationSnapshot(cmd.Context(), root, commit, snapshot)
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot materialize immutable candidate snapshot: "+err.Error(), "")
		return
	}
	result.SnapshotDigest = snapshotDigest
	verifyAuthorizationCandidateDirectory(cmd, snapshot, policy, result)
}

func verifyAuthorizationCandidateDirectory(cmd *cobra.Command, snapshot string, policy *AuthorizationPolicy, result *AuthorizationPolicyResult) {
	repo, err := workflow.LoadRepoConfig(snapshot)
	if err != nil {
		result.addFinding("verification_incomplete", workflow.RepoConfigFileName, "", "Repository configuration is invalid.", "")
		return
	}
	if repo != nil && repo.SafeOutputAuthorization != nil {
		verifyAuthorizationBinding(result, workflow.RepoConfigFileName, "safe_output_authorization", repo.SafeOutputAuthorization, policy)
	}
	// Some compiler lookups are repository-relative. The only working directory
	// used for candidate parsing is the non-executable, private object snapshot.
	cwd, err := os.Getwd()
	if err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot establish compiler working directory.", "")
		return
	}
	if err := os.Chdir(snapshot); err != nil {
		result.addFinding("verification_incomplete", "", "", "Cannot enter isolated candidate snapshot.", "")
		return
	}
	defer func() {
		if err := os.Chdir(cwd); err != nil {
			result.addFinding("verification_incomplete", "", "", "Cannot restore compiler working directory.", "")
		}
	}()
	approved, _ := cmd.Flags().GetStringArray("authorization-actions-workflow")
	verifyAuthorizationSnapshot(cmd.Context(), snapshot, policy, result, approved)
}

func readBoundedAuthorizationFile(path string, limit int64) (data []byte, resultErr error) {
	file, err := os.Open(filepath.Clean(path))
	if err != nil {
		return nil, err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	data, err = io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("authorization input exceeds size limit")
	}
	return data, nil
}
