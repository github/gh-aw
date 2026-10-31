package cli

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

func validateOfflineRunIdentity(expected, actual WorkflowRun) error {
	expectedHost, expectedRepo, err := offlineRunIdentityParts(expected)
	if err != nil {
		return err
	}
	actualHost, actualRepo, err := offlineRunIdentityParts(actual)
	if err != nil {
		return err
	}
	if expected.DatabaseID > 0 && actual.DatabaseID > 0 && expected.DatabaseID != actual.DatabaseID {
		return fmt.Errorf("offline run metadata has conflicting run IDs: %d versus %d", expected.DatabaseID, actual.DatabaseID)
	}
	if expected.Attempt > 0 && actual.Attempt > 0 && expected.Attempt != actual.Attempt {
		return fmt.Errorf("offline run %d has conflicting attempt metadata: %d versus %d", expected.DatabaseID, expected.Attempt, actual.Attempt)
	}
	if expectedHost != "" && actualHost != "" && !strings.EqualFold(expectedHost, actualHost) {
		return fmt.Errorf("offline run %d has conflicting host metadata: %q versus %q", expected.DatabaseID, expectedHost, actualHost)
	}
	if expectedRepo != "" && actualRepo != "" && !strings.EqualFold(expectedRepo, actualRepo) {
		return fmt.Errorf("offline run %d has conflicting repository metadata: %q versus %q", expected.DatabaseID, expectedRepo, actualRepo)
	}
	return nil
}

func offlineRunIdentityParts(run WorkflowRun) (string, string, error) {
	if run.URL == "" {
		return "", run.Repository, nil
	}
	parsed, err := url.Parse(run.URL)
	if err != nil || parsed.Host == "" {
		return "", "", fmt.Errorf("offline run %d has invalid recorded URL %q", run.DatabaseID, run.URL)
	}
	parts := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	if len(parts) < 5 {
		return "", "", fmt.Errorf("offline run %d has invalid Actions URL %q", run.DatabaseID, run.URL)
	}
	repository := strings.Join(parts[:2], "/")
	if run.Repository != "" && !strings.EqualFold(run.Repository, repository) {
		return "", "", fmt.Errorf("offline run %d URL conflicts with recorded repository %q", run.DatabaseID, run.Repository)
	}
	id, err := strconv.ParseInt(parts[4], 10, 64) //nolint:uncheckedsliceindex // len(parts) >= 5 above.
	if err != nil || id <= 0 || run.DatabaseID > 0 && run.DatabaseID != id {
		return "", "", fmt.Errorf("offline run %d URL conflicts with recorded run ID", run.DatabaseID)
	}
	return parsed.Host, repository, nil
}
