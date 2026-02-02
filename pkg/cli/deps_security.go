package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
)

var depsSecurityLog = logger.New("cli:deps_security")

// SecurityAdvisory represents a security vulnerability
type SecurityAdvisory struct {
	GHSAID      string
	CVE         string
	Summary     string
	Severity    string
	Package     string
	PatchedVers []string
	AffectedVer string
	URL         string
}

// GitHubAdvisoryResponse represents the GitHub Advisory API response
type GitHubAdvisoryResponse struct {
	GHSAID   string `json:"ghsa_id"`
	CVEID    string `json:"cve_id"`
	Summary  string `json:"summary"`
	Severity string `json:"severity"`
	HTMLURL  string `json:"html_url"`
	// Vulnerabilities contains affected versions and patches
	Vulnerabilities []struct {
		Package struct {
			Ecosystem string `json:"ecosystem"`
			Name      string `json:"name"`
		} `json:"package"`
		VulnerableVersionRange string `json:"vulnerable_version_range"`
		FirstPatchedVersion    string `json:"first_patched_version"`
	} `json:"vulnerabilities"`
}

// CheckSecurityAdvisories checks for security vulnerabilities in dependencies
func CheckSecurityAdvisories(verbose bool) ([]SecurityAdvisory, error) {
	depsSecurityLog.Print("Starting security advisory check")

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

	depsSecurityLog.Printf("Checking %d dependencies for security advisories", len(deps))

	// Create map of dependencies for quick lookup
	depVersions := make(map[string]string)
	for _, dep := range deps {
		depVersions[dep.Path] = dep.Version
	}

	// Query GitHub Security Advisory API
	advisories, err := querySecurityAdvisories(depVersions, verbose)
	if err != nil {
		return nil, fmt.Errorf("failed to query security advisories: %w", err)
	}

	depsSecurityLog.Printf("Found %d security advisories", len(advisories))
	return advisories, nil
}

// DisplaySecurityAdvisories shows security advisories in a formatted output
func DisplaySecurityAdvisories(advisories []SecurityAdvisory) {
	if len(advisories) == 0 {
		fmt.Fprintln(os.Stderr, console.FormatSuccessMessage("✅ No known security vulnerabilities"))
		return
	}

	// Display header with bordered box using console helper
	errorBox := console.RenderErrorBox("🔴 SECURITY ADVISORIES")
	for _, line := range errorBox {
		fmt.Fprintln(os.Stderr, line)
	}
	fmt.Fprintln(os.Stderr, "")

	// Sort by severity (critical first)
	sort.Slice(advisories, func(i, j int) bool {
		return severityWeight(advisories[i].Severity) > severityWeight(advisories[j].Severity)
	})

	// Display each advisory
	for _, adv := range advisories {
		icon := getSeverityIcon(adv.Severity)
		header := fmt.Sprintf("%s %s: %s", icon, strings.ToUpper(adv.Severity), adv.Package)

		if adv.Severity == "critical" || adv.Severity == "high" {
			fmt.Fprintln(os.Stderr, console.FormatErrorMessage(header))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(header))
		}

		fmt.Fprintf(os.Stderr, "    %s", adv.Summary)
		if adv.CVE != "" {
			fmt.Fprintf(os.Stderr, " (%s)", adv.CVE)
		}
		fmt.Fprintln(os.Stderr, "")

		if len(adv.PatchedVers) > 0 {
			fmt.Fprintf(os.Stderr, "    Fixed in: %s\n", strings.Join(adv.PatchedVers, ", "))
		}

		fmt.Fprintf(os.Stderr, "    %s\n", adv.URL)
		fmt.Fprintln(os.Stderr, "")
	}
}

// querySecurityAdvisories queries the GitHub Security Advisory API
func querySecurityAdvisories(depVersions map[string]string, verbose bool) ([]SecurityAdvisory, error) {
	// GitHub Security Advisory API endpoint
	url := "https://api.github.com/advisories?ecosystem=go&per_page=100"

	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, err
	}

	// Set headers
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var apiAdvisories []GitHubAdvisoryResponse
	if err := json.Unmarshal(body, &apiAdvisories); err != nil {
		return nil, err
	}

	depsSecurityLog.Printf("Received %d advisories from GitHub API", len(apiAdvisories))

	// Filter advisories that match our dependencies
	var matchingAdvisories []SecurityAdvisory
	for _, apiAdv := range apiAdvisories {
		for _, vuln := range apiAdv.Vulnerabilities {
			if vuln.Package.Ecosystem == "Go" {
				// Check if this package is in our dependencies
				if currentVersion, found := depVersions[vuln.Package.Name]; found {
					// Create advisory entry
					adv := SecurityAdvisory{
						GHSAID:      apiAdv.GHSAID,
						CVE:         apiAdv.CVEID,
						Summary:     apiAdv.Summary,
						Severity:    apiAdv.Severity,
						Package:     vuln.Package.Name,
						AffectedVer: currentVersion,
						URL:         apiAdv.HTMLURL,
					}

					// Add patched version if available
					if vuln.FirstPatchedVersion != "" {
						adv.PatchedVers = []string{vuln.FirstPatchedVersion}
					}

					matchingAdvisories = append(matchingAdvisories, adv)

					if verbose {
						fmt.Fprintln(os.Stderr, console.FormatVerboseMessage(
							fmt.Sprintf("Found advisory for %s: %s", vuln.Package.Name, apiAdv.GHSAID)))
					}
				}
			}
		}
	}

	return matchingAdvisories, nil
}

// getSeverityIcon returns an emoji icon for the severity level
func getSeverityIcon(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return "🔴"
	case "high":
		return "🟠"
	case "medium":
		return "🟡"
	case "low":
		return "🟢"
	default:
		return "⚠️"
	}
}

// severityWeight returns a numeric weight for sorting by severity
func severityWeight(severity string) int {
	switch strings.ToLower(severity) {
	case "critical":
		return 4
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}
