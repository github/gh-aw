package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
)

var depsOutdatedLog = logger.New("cli:deps_outdated")

// OutdatedDependency represents a dependency that has a newer version available
type OutdatedDependency struct {
	Module  string
	Current string
	Latest  string
	Age     time.Duration
	IsV0    bool
}

// DependencyInfo represents parsed information about a Go module
type DependencyInfo struct {
	Path    string
	Version string
}

// ProxyInfo represents the latest version info from Go proxy
type ProxyInfo struct {
	Version string
	Time    time.Time
}

// CheckOutdatedDependencies analyzes go.mod for outdated dependencies
func CheckOutdatedDependencies(ctx context.Context, verbose bool) ([]OutdatedDependency, error) {
	depsOutdatedLog.Print("Starting outdated dependency check")

	// Find go.mod file
	goModPath, err := findGoMod()
	if err != nil {
		return nil, fmt.Errorf("failed to find go.mod: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessageStderr("Reading go.mod from: "+goModPath))
	}

	// Parse go.mod to get dependencies
	deps, err := parseGoMod(goModPath)
	if err != nil {
		return nil, fmt.Errorf("failed to parse go.mod: %w", err)
	}

	depsOutdatedLog.Printf("Found %d dependencies in go.mod", len(deps))

	// Check each dependency for updates
	var outdated []OutdatedDependency
	for _, dep := range deps {
		latest, age, err := getLatestVersion(ctx, dep.Path, dep.Version, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessageStderr(fmt.Sprintf("could not check %s: %v", dep.Path, err)))
			}
			continue
		}

		// Compare versions
		if latest != "" && latest != dep.Version {
			isV0 := strings.HasPrefix(dep.Version, "v0.")
			outdated = append(outdated, OutdatedDependency{
				Module:  dep.Path,
				Current: dep.Version,
				Latest:  latest,
				Age:     age,
				IsV0:    isV0,
			})
		}
	}

	depsOutdatedLog.Printf("Found %d outdated dependencies", len(outdated))
	return outdated, nil
}

// DisplayOutdatedDependencies shows outdated dependencies in a formatted table
func DisplayOutdatedDependencies(outdated []OutdatedDependency, totalDeps int) {
	if len(outdated) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessageStderr("All dependencies are up to date"))
		return
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("Outdated Dependencies"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr("====================="))
	fmt.Fprintln(os.Stderr, "")

	// Sort by module name
	slices.SortFunc(outdated, func(a, b OutdatedDependency) int {
		switch {
		case a.Module < b.Module:
			return -1
		case a.Module > b.Module:
			return 1
		default:
			return 0
		}
	})

	// Display table
	headers := []string{"Module", "Current", "Latest", "Age", "Status"}
	rows := make([][]string, 0, len(outdated))

	for _, dep := range outdated {
		age := formatAge(dep.Age)
		status := getUpdateStatus(dep)
		rows = append(rows, []string{
			stringutil.Truncate(dep.Module, 50),
			dep.Current,
			dep.Latest,
			age,
			status,
		})
	}

	tableConfig := console.TableConfig{
		Headers: headers,
		Rows:    rows,
	}
	fmt.Fprint(os.Stderr, console.RenderTableStderr(tableConfig))

	fmt.Fprintln(os.Stderr, "")

	// Summary
	percentage := float64(len(outdated)) / float64(totalDeps) * 100
	summary := fmt.Sprintf("Summary: %d of %d dependencies outdated (%.0f%%)", len(outdated), totalDeps, percentage)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessageStderr(summary))
}

// parseGoMod extracts direct dependency information from go.mod.
// Indirect dependencies are filtered out; use parseGoModFile for all dependencies.
func parseGoMod(path string) ([]DependencyInfo, error) {
	all, err := parseGoModFile(path)
	if err != nil {
		return nil, err
	}

	var deps []DependencyInfo
	for _, d := range all {
		if !d.Indirect {
			deps = append(deps, d.DependencyInfo)
		}
	}
	return deps, nil
}

// getLatestVersion queries the Go proxy for the latest version
func getLatestVersion(ctx context.Context, modulePath, currentVersion string, verbose bool) (string, time.Duration, error) {
	depsOutdatedLog.Printf("Checking latest version for %s (current: %s)", modulePath, currentVersion)

	// Query Go proxy API
	url := fmt.Sprintf("https://proxy.golang.org/%s/@latest", modulePath)

	client := &http.Client{Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", 0, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", 0, fmt.Errorf("proxy returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", 0, err
	}

	var info ProxyInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return "", 0, err
	}

	// Calculate age
	age := time.Since(info.Time)

	depsOutdatedLog.Printf("Latest version for %s: %s (age: %v)", modulePath, info.Version, age)
	return info.Version, age, nil
}

// formatAge formats a duration into a human-readable string
func formatAge(d time.Duration) string {
	days := int(d.Hours() / 24)
	if days == 0 {
		return "today"
	} else if days == 1 {
		return "1 day"
	} else if days < 30 {
		return fmt.Sprintf("%d days", days)
	} else if days < 365 {
		months := days / 30
		return fmt.Sprintf("%d months", months)
	} else {
		years := days / 365
		return fmt.Sprintf("%d years", years)
	}
}

// getUpdateStatus returns a status message for the dependency
func getUpdateStatus(dep OutdatedDependency) string {
	status := "Update available"
	if dep.IsV0 {
		status += " ⚠️ v0.x"
	}
	return status
}
