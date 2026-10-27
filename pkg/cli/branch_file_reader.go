package cli

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/workflow"
)

var branchFileReaderLog = logger.New("cli:branch_file_reader")

func repositoryAPIArgs(repoOverride, hostname, endpoint string, flags ...string) ([]string, error) {
	if err := validateAPIEndpoint(endpoint); err != nil {
		return nil, fmt.Errorf("invalid endpoint %q: %w", endpoint, err)
	}
	ownerRepo, repoHost := repoutil.NormalizeRepoForAPI(repoOverride)
	if hostname == "" {
		hostname = repoHost
	}
	args := append([]string{"api", fmt.Sprintf("repos/%s/%s", escapeOwnerRepo(ownerRepo), endpoint)}, flags...)
	if hostname != "" {
		args = append(args, "--hostname", hostname)
	}
	return args, nil
}

func readRemoteRepoBranchFile(repoOverride, branchName, filePath, hostname string) ([]byte, error) {
	return readRemoteRepoBranchFileContext(context.Background(), repoOverride, branchName, filePath, hostname)
}

func readRemoteRepoBranchFileContext(ctx context.Context, repoOverride, branchName, filePath, hostname string) ([]byte, error) {
	if err := validateAPIEndpoint(filePath); err != nil {
		branchFileReaderLog.Printf("Rejecting unsafe remote file path %q: %v", filePath, err)
		return nil, fmt.Errorf("invalid file path %q: %w", filePath, err)
	}
	branchFileReaderLog.Printf("Fetching remote file: repo=%s, branch=%s, path=%s, hostname=%s", repoOverride, branchName, filePath, hostname)
	args, err := repositoryAPIArgs(repoOverride, hostname, "contents/"+escapeEndpoint(filePath),
		"--method", "GET",
		"--raw-field", "ref="+branchName,
		"--jq", ".content",
	)
	if err != nil {
		return nil, err
	}
	cmd := workflow.ExecGHContext(ctx, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errorutil.IsNotFoundOutput(string(out)) {
			branchFileReaderLog.Printf("Remote file not found: path=%s, branch=%s", filePath, branchName)
			return nil, os.ErrNotExist
		}
		branchFileReaderLog.Printf("Failed to fetch %s from %s: %v", filePath, branchName, err)
		return nil, fmt.Errorf("failed to fetch %s from %s: %w", filePath, branchName, err)
	}

	decoded, decodeErr := decodeBase64FileContent(string(out))
	if decodeErr != nil {
		branchFileReaderLog.Printf("Failed to decode %s from %s: %v", filePath, branchName, decodeErr)
		return nil, fmt.Errorf("failed to decode %s from %s: %w", filePath, branchName, decodeErr)
	}
	branchFileReaderLog.Printf("Fetched remote file %s from %s: %d bytes", filePath, branchName, len(decoded))
	return decoded, nil
}

func isRemoteFileNotFound(err error) bool {
	return errors.Is(err, os.ErrNotExist)
}
