package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
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
func CheckOutdatedDependencies(verbose bool) ([]OutdatedDependency, error) {
	depsOutdatedLog.Print("Starting outdated dependency check")

	// Find go.mod file
	goModPath, err := findGoMod()
	if err != nil {
		return nil, fmt.Errorf("failed to find go.mod: %w", err)
	}

	if verbose {
		fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(fmt.Sprintf("Reading go.mod from: %s", goModPath)))
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
		latest, age, err := getLatestVersion(dep.Path, dep.Version, verbose)
		if err != nil {
			if verbose {
				fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("Warning: could not check %s: %v", dep.Path, err)))
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
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✅ All dependencies are up to date"))
		return
	}

	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Outdated Dependencies"))
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage("====================="))
	fmt.Fprintln(os.Stderr, "")

	// Sort by module name
	sort.Slice(outdated, func(i, j int) bool {
		return outdated[i].Module < outdated[j].Module
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
	fmt.Fprint(os.Stderr, console.RenderTable(tableConfig))

	fmt.Fprintln(os.Stderr, "")

	// Summary
	percentage := float64(len(outdated)) / float64(totalDeps) * 100
	summary := fmt.Sprintf("Summary: %d of %d dependencies outdated (%.0f%%)", len(outdated), totalDeps, percentage)
	fmt.Fprintln(os.Stderr, console.FormatInfoMessage(summary))
}

// findGoMod locates the go.mod file in the repository
func findGoMod() (string, error) {
	// Try current directory first
	if _, err := os.Stat("go.mod"); err == nil {
		return filepath.Abs("go.mod")
	}

	// Try git root
	root, err := findGitRoot()
	if err != nil {
		return "", fmt.Errorf("not in a Go module (no go.mod found)")
	}

	goModPath := filepath.Join(root, "go.mod")
	if _, err := os.Stat(goModPath); err != nil {
		return "", fmt.Errorf("not in a Go module (no go.mod found)")
	}

	return goModPath, nil
}

// parseGoMod extracts dependency information from go.mod
func parseGoMod(path string) ([]DependencyInfo, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var deps []DependencyInfo
	lines := strings.Split(string(content), "\n")
	inRequire := false

	for _, line := range lines {
		line = strings.TrimSpace(line)

		// Track require block
		if strings.HasPrefix(line, "require (") {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}

		// Parse dependency line
		if inRequire || strings.HasPrefix(line, "require ") {
			// Remove "require " prefix if present
			line = strings.TrimPrefix(line, "require ")

			// Skip lines with // indirect comment (indirect dependencies)
			if strings.Contains(line, "// indirect") {
				continue
			}

			parts := strings.Fields(line)
			if len(parts) >= 2 {
				deps = append(deps, DependencyInfo{
					Path:    parts[0],
					Version: parts[1],
				})
			}
		}
	}

	return deps, nil
}

// getLatestVersion queries the Go proxy for the latest version
func getLatestVersion(modulePath, currentVersion string, verbose bool) (string, time.Duration, error) {
	depsOutdatedLog.Printf("Checking latest version for %s (current: %s)", modulePath, currentVersion)

	// Query Go proxy API
	url := fmt.Sprintf("https://proxy.golang.org/%s/@latest", modulePath)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
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
