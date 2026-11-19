package cli

import (
	"context"
	"errors"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
)

func verifyAuthorizationSnapshot(ctx context.Context, root string, policy *AuthorizationPolicy, result *AuthorizationPolicyResult, approvedActions []string) {
	required := map[string]struct{}{}
	for _, file := range policy.RequiredWorkflows {
		required[file] = struct{}{}
	}
	directory := filepath.Join(root, constants.WorkflowsDir)
	if _, err := os.Stat(directory); errors.Is(err, os.ErrNotExist) {
		for _, file := range policy.RequiredWorkflows {
			result.addFinding("required_workflow_missing", file, "", "Baseline-required source is missing.", "")
		}
		return
	}
	files, err := getMarkdownWorkflowFiles(directory)
	if err != nil {
		result.addFinding("inventory_unresolved", "", "", "Cannot discover the governed workflow directory.", "")
		return
	}
	files, err = filterMarkdownFilesWithFrontmatter(files)
	if err != nil {
		result.addFinding("inventory_unresolved", "", "", "Cannot inspect workflow frontmatter.", "")
		return
	}
	locks := map[string]struct{}{}
	for _, file := range files {
		relative, err := filepath.Rel(root, file)
		if err != nil {
			result.addFinding("inventory_unresolved", "", "", "Cannot resolve workflow path.", "")
			continue
		}
		relative = filepath.ToSlash(relative)
		compiler := workflow.NewAuthorizationVerificationCompiler(root)
		compiler.SetContext(ctx)
		compiler.SetRepositorySlug(result.Repository)
		data, generated, err := compiler.CompileAuthorizationSnapshot(file)
		if err != nil {
			var shared *workflow.SharedWorkflowError
			if errors.As(err, &shared) && !setutil.Contains(required, relative) {
				continue
			}
			if shared == nil {
				locks[stringutil.MarkdownToLockFile(relative)] = struct{}{}
				delete(required, relative)
			}
			result.addFinding("verification_incomplete", relative, "", "Trusted compilation could not resolve and validate this entrypoint; check source, authorization conflicts and local dependencies.", "")
			continue
		}
		delete(required, relative)
		lock := stringutil.MarkdownToLockFile(relative)
		locks[lock] = struct{}{}
		verifyAuthorizationWorkflow(root, relative, data, generated, policy, result)
	}
	for _, file := range policy.RequiredWorkflows {
		if setutil.Contains(required, file) {
			result.addFinding("required_workflow_missing", file, "", "Baseline-required source is missing or is no longer a valid entrypoint.", "")
		}
	}
	verifyAuthorizationYAMLInventory(root, locks, approvedActions, result)
}

func verifyAuthorizationWorkflow(root, relative string, data *workflow.WorkflowData, generated string, policy *AuthorizationPolicy, result *AuthorizationPolicyResult) {
	entry := AuthorizationWorkflowResult{
		Source: relative, Lock: stringutil.MarkdownToLockFile(relative),
		Imports: map[string]string{}, Routes: []AuthorizationRouteResult{},
		GeneratedDigest: authorizationDigest([]byte(generated)),
	}
	source, err := os.ReadFile(filepath.Join(root, relative))
	if err != nil {
		result.addFinding("verification_incomplete", relative, "", "Cannot read workflow source.", "")
		return
	}
	entry.SourceDigest = authorizationDigest(source)
	// Global configuration must also match when the only outputs are diagnostics.
	if data.SafeOutputs != nil && data.SafeOutputs.Authorization != nil {
		verifyAuthorizationBinding(result, relative, "safe-outputs.authorization", data.SafeOutputs.Authorization, policy)
	}
	for _, route := range workflow.InspectEffectiveAuthorization(data) {
		entry.Routes = append(entry.Routes, AuthorizationRouteResult{AuthorizationRoute: route, Binding: authorizationBinding(route.Authorization)})
		if route.Exempt {
			result.Exempt++
		} else {
			result.Eligible++
			verifyAuthorizationBinding(result, relative, route.Name, route.Authorization, policy)
		}
		if route.Staged {
			result.Staged++
		}
		switch route.Coverage {
		case "manager", "standalone", "workflow", "script", "action", "job", "staged", "builtin-exempt", "unconfigured":
		default:
			result.addFinding("executor_uncovered", relative, route.Name, "No compiler-classified authorization executor covers this route.", "")
		}
	}
	fingerprintAuthorizationImports(root, data, &entry, result)
	actual, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(entry.Lock)))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			result.addFinding("generated_workflow_drift", relative, "", "Generated lock is missing.", "")
		} else {
			result.addFinding("verification_incomplete", relative, "", "Cannot read generated lock.", "")
		}
	} else {
		entry.LockDigest = authorizationDigest(actual)
		if string(actual) != generated {
			result.addFinding("generated_workflow_drift", relative, "", "Lock differs from trusted compiler output; recompile with the pinned compiler and approved inputs.", "")
		}
	}
	result.Workflows = append(result.Workflows, entry)
}

func fingerprintAuthorizationImports(root string, data *workflow.WorkflowData, entry *AuthorizationWorkflowResult, result *AuthorizationPolicyResult) {
	for _, imported := range append(slices.Clone(data.ImportedFiles), data.IncludedFiles...) {
		if strings.HasPrefix(imported, parser.BuiltinPathPrefix) {
			continue
		}
		importPath := imported
		if !filepath.IsAbs(importPath) {
			importPath = filepath.Join(root, filepath.Dir(entry.Source), imported)
		}
		relative, err := filepath.Rel(root, importPath)
		if err != nil || !filepath.IsLocal(relative) {
			result.addFinding("inventory_unresolved", entry.Source, "", "An imported dependency is outside the candidate snapshot.", "")
			continue
		}
		content, err := os.ReadFile(importPath)
		if err != nil {
			result.addFinding("inventory_unresolved", entry.Source, "", "Cannot fingerprint an imported dependency.", "")
			continue
		}
		entry.Imports[filepath.ToSlash(relative)] = authorizationDigest(content)
	}
}

func verifyAuthorizationBinding(result *AuthorizationPolicyResult, file, route string, observed *workflow.SafeOutputAuthorization, policy *AuthorizationPolicy) {
	code, message := "", ""
	switch {
	case observed == nil:
		code, message = "authorization_required", "Eligible output has no effective authorization requirement."
	case observed.FromEnvironment:
		code, message = "mutable_authority_binding", "Protected verification requires literal authority bindings, not runtime variables."
	case observed.Endpoint != policy.Authority.Endpoint || observed.Audience != policy.Authority.Audience:
		code, message = "authority_mismatch", "Effective authority does not match the protected baseline."
	}
	if code != "" {
		result.addFinding(code, file, route, message, authorizationBinding(observed))
	}
}

func verifyAuthorizationYAMLInventory(root string, locks map[string]struct{}, approved []string, result *AuthorizationPolicyResult) {
	allowed := map[string]struct{}{}
	for _, file := range approved {
		if path.Dir(file) != constants.WorkflowsDir || path.Clean(file) != file ||
			(!strings.HasSuffix(file, ".yml") && !strings.HasSuffix(file, ".yaml")) ||
			strings.HasSuffix(file, ".lock.yml") || strings.ContainsAny(file, "\\\x00\r\n") || setutil.Contains(allowed, file) {
			result.addFinding("verification_incomplete", "", "", "Invalid externally approved handwritten Actions inventory.", "")
			return
		}
		allowed[file] = struct{}{}
	}
	entries, err := os.ReadDir(filepath.Join(root, constants.WorkflowsDir))
	if err != nil {
		result.addFinding("inventory_unresolved", "", "", "Cannot enumerate executable workflows.", "")
		return
	}
	for _, entry := range entries {
		extension := path.Ext(entry.Name())
		if entry.IsDir() || (!strings.EqualFold(extension, ".yml") && !strings.EqualFold(extension, ".yaml")) {
			continue
		}
		file := path.Join(constants.WorkflowsDir, entry.Name())
		if setutil.Contains(locks, file) {
			continue
		}
		if strings.HasSuffix(file, ".lock.yml") {
			result.addFinding("generated_workflow_drift", file, "", "Orphaned generated workflow has no verified source entrypoint.", "")
		} else if !setutil.Contains(allowed, file) {
			result.addFinding("inventory_unresolved", file, "", "Unexpected executable Actions workflow requires separate protected inventory review; it is not an authorized gh-aw output.", "")
		} else {
			result.UngovernedWorkflows = append(result.UngovernedWorkflows, file)
		}
	}
}
