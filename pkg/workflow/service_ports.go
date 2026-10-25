// This file provides helper functions for extracting service port mappings from
// GitHub Actions services: configuration and generating ${{ job.services['<id>'].ports['<port>'] }}
// expressions for AWF's --allow-host-service-ports flag.
//
// When a workflow uses GitHub Actions services: with port mappings (e.g., PostgreSQL, Redis),
// the compiled workflow runs the agent inside AWF's isolated network. The agent cannot reach
// service containers without explicit --allow-host-service-ports configuration. This file
// automatically detects service ports and generates the necessary expressions.

package workflow

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/sliceutil"
	"github.com/goccy/go-yaml"
)

var servicePortsLog = logger.New("workflow:service_ports")

// maxPortRangeExpansion is the maximum number of ports to expand from a range specification.
// This prevents accidentally generating thousands of expressions from a large port range.
const maxPortRangeExpansion = 32

// minPort and maxPort define the valid TCP/UDP port range.
const (
	minPort = 1
	maxPort = 65535
)

// awfDangerousHostPorts mirrors AWF's DANGEROUS_PORTS list (gh-aw-firewall
// src/squid/policy-manifest.ts) as of the pinned AWF release
// constants.DefaultFirewallVersion (v0.27.44). These ports are never allowed
// via --allow-host-ports, even with --enable-host-access: AWF blocks them at
// both the iptables and Squid policy layers to prevent the agent sandbox
// from reaching sensitive services directly. --allow-host-service-ports
// intentionally bypasses this list because it restricts traffic to the host
// gateway only (for GitHub Actions services:), but that flag requires
// sandbox.agent.runtime: docker-sudo-iptables.
//
// If the pinned AWF version is bumped and its DANGEROUS_PORTS list changes,
// update this map to match; there is no automated sync with upstream.
var awfDangerousHostPorts = map[int]string{
	22:    "SSH",
	23:    "Telnet",
	25:    "SMTP",
	110:   "POP3",
	143:   "IMAP",
	445:   "SMB",
	1433:  "MS SQL Server",
	1521:  "Oracle DB",
	3306:  "MySQL",
	3389:  "RDP",
	5432:  "PostgreSQL",
	5984:  "CouchDB",
	6379:  "Redis",
	6984:  "CouchDB (SSL)",
	8086:  "InfluxDB HTTP API",
	8088:  "InfluxDB RPC",
	9200:  "Elasticsearch HTTP API",
	9300:  "Elasticsearch transport",
	27017: "MongoDB",
	27018: "MongoDB sharding",
	28017: "MongoDB web interface",
}

// servicesYAMLWrapper is the top-level YAML wrapper for a services: block.
// It provides typed access to the service container map while the YAML is parsed
// via goccy/go-yaml with field-level annotations.
type servicesYAMLWrapper struct {
	Services map[string]*serviceContainerConfig `yaml:"services"`
}

// serviceContainerConfig represents a single GitHub Actions service container.
// Only the Ports field is consumed for port-expression generation; all other
// container fields (image, env, options, volumes, …) are intentionally omitted.
//
// Ports is declared as any because GitHub Actions allows the ports list to contain
// both string values ("5432:5432") and bare integers (5432), and the YAML may also
// omit the field entirely (nil) or supply a non-list scalar, which triggers a
// compile-time warning.
type serviceContainerConfig struct {
	Ports any `yaml:"ports"`
}

// ExtractServicePortExpressions parses the services: YAML string from WorkflowData.Services
// and returns a comma-separated string of ${{ job.services['<id>'].ports['<port>'] }} expressions
// for all TCP port mappings found.
//
// The returned string is suitable for passing as --allow-host-service-ports to AWF.
// Returns empty string if no services or no port mappings are found.
//
// Bracket notation (job.services['id']) is used for all service IDs to correctly handle
// IDs containing hyphens, digits-first names, or other characters invalid in dot-notation.
//
// Parameters:
//   - servicesYAML: Raw YAML string from WorkflowData.Services (includes "services:" wrapper)
//
// Returns:
//   - expressions: Comma-separated ${{ }} expressions for all service ports
//   - warnings: Any warnings generated during parsing (e.g., UDP ports, services without ports)
func ExtractServicePortExpressions(servicesYAML string) (string, []string) {
	if servicesYAML == "" {
		return "", nil
	}

	servicePortsLog.Print("Extracting service port expressions from services YAML")

	// Parse the services YAML into typed structs so field access is explicit
	// and does not rely on map[string]any type assertions.
	var wrapper servicesYAMLWrapper
	if err := yaml.Unmarshal([]byte(servicesYAML), &wrapper); err != nil {
		servicePortsLog.Printf("Failed to parse services YAML: %v", err)
		return "", nil
	}

	if wrapper.Services == nil {
		servicePortsLog.Print("No services map found in YAML")
		return "", nil
	}

	var expressions []string
	var warnings []string

	// Sort service IDs for deterministic output
	serviceIDs := sliceutil.SortedKeys(wrapper.Services)

	for _, serviceID := range serviceIDs {
		serviceExpressions, serviceWarnings := extractServiceContainerPorts(serviceID, wrapper.Services[serviceID])
		expressions = append(expressions, serviceExpressions...)
		warnings = append(warnings, serviceWarnings...)
	}

	if len(expressions) == 0 {
		servicePortsLog.Print("No service port expressions generated")
		return "", warnings
	}

	result := strings.Join(expressions, ",")
	servicePortsLog.Printf("Generated %d service port expressions", len(expressions))
	return result, warnings
}

func extractServiceContainerPorts(serviceID string, svc *serviceContainerConfig) ([]string, []string) {
	var expressions, warnings []string
	if svc == nil {
		servicePortsLog.Printf("Service %s is nil, skipping", serviceID)
		return nil, nil
	}

	if svc.Ports == nil {
		warnings = append(warnings, fmt.Sprintf("service %q has no ports mapping; it will not be reachable from the AWF sandbox", serviceID))
		servicePortsLog.Printf("Service %s has no ports, skipping", serviceID)
		return nil, warnings
	}

	portsList, ok := svc.Ports.([]any)
	if !ok {
		servicePortsLog.Printf("Service %s ports is not a list, skipping", serviceID)
		warnings = append(warnings, fmt.Sprintf("service %q has an invalid ports mapping (expected a list); it will not be reachable from the AWF sandbox", serviceID))
		return nil, warnings
	}

	for _, portSpec := range portsList {
		containerPorts, portWarnings := parsePortSpec(portSpec)
		for _, w := range portWarnings {
			warnings = append(warnings, fmt.Sprintf("service %q: %s", serviceID, w))
		}
		for _, cp := range containerPorts {
			escapedServiceID := strings.ReplaceAll(serviceID, "'", "''")
			expr := fmt.Sprintf("${{ job.services['%s'].ports['%d'] }}", escapedServiceID, cp)
			expressions = append(expressions, expr)
		}
	}
	return expressions, warnings
}

// parsePortSpec parses a single port specification and returns the container port(s).
// Supports formats:
//   - "5432:5432" (host:container)
//   - "5432" (container only, dynamic host port)
//   - "49152:5432" (remapped host port)
//   - "5432/tcp" (explicit TCP)
//   - "5432/udp" (skipped with warning)
//   - "6000-6010:6000-6010" (range)
//   - 5432 (integer)
//
// Returns container port numbers and any warnings.
func parsePortSpec(spec any) ([]int, []string) {
	servicePortsLog.Printf("Parsing port spec: %v (type %T)", spec, spec)
	var portStr string
	switch v := spec.(type) {
	case int:
		if v < minPort || v > maxPort {
			return nil, []string{fmt.Sprintf("port %d is outside valid range %d-%d", v, minPort, maxPort)}
		}
		servicePortsLog.Printf("Parsed integer port spec: %d", v)
		return []int{v}, nil
	case uint64:
		// goccy/go-yaml decodes unquoted integers as uint64
		p := int(v)
		if p < minPort || p > maxPort {
			return nil, []string{fmt.Sprintf("port %d is outside valid range %d-%d", p, minPort, maxPort)}
		}
		return []int{p}, nil
	case int64:
		p := int(v)
		if p < minPort || p > maxPort {
			return nil, []string{fmt.Sprintf("port %d is outside valid range %d-%d", p, minPort, maxPort)}
		}
		return []int{p}, nil
	case float64:
		// Some YAML libraries parse unquoted numbers as float64
		p := int(v)
		if float64(p) != v {
			return nil, []string{fmt.Sprintf("port %v is not an integer", v)}
		}
		if p < minPort || p > maxPort {
			return nil, []string{fmt.Sprintf("port %d is outside valid range %d-%d", p, minPort, maxPort)}
		}
		return []int{p}, nil
	case string:
		portStr = v
	default:
		return nil, []string{fmt.Sprintf("unsupported port spec type %T: %v", spec, spec)}
	}
	return parseStringPortSpec(portStr)
}

func parseStringPortSpec(portStr string) ([]int, []string) {
	portStr = strings.TrimSpace(portStr)
	if portStr == "" {
		return nil, nil
	}

	// Check for protocol suffix
	protocol := "tcp"
	if idx := strings.LastIndex(portStr, "/"); idx != -1 {
		protocol = strings.ToLower(portStr[idx+1:])
		portStr = portStr[:idx]
	}

	if protocol == "udp" {
		servicePortsLog.Printf("Skipping UDP port %q: AWF only supports TCP", portStr)
		return nil, []string{fmt.Sprintf("UDP port %q skipped; AWF only supports TCP", portStr)}
	}
	if protocol != "tcp" {
		servicePortsLog.Printf("Skipping unsupported protocol %q for port %q", protocol, portStr)
		return nil, []string{fmt.Sprintf("unsupported protocol %q for port %q; AWF only supports TCP", protocol, portStr)}
	}
	return parseContainerPortSpec(portStr)
}

func parseContainerPortSpec(portStr string) ([]int, []string) {
	// Split host:container
	var containerPart string
	if _, after, found := strings.Cut(portStr, ":"); found {
		containerPart = after
	} else {
		containerPart = portStr
	}

	// Check for port range (e.g., "6000-6010")
	if startStr, endStr, found := strings.Cut(containerPart, "-"); found {
		start, err1 := strconv.Atoi(startStr)
		end, err2 := strconv.Atoi(endStr)
		if err1 != nil || err2 != nil {
			return nil, []string{fmt.Sprintf("invalid port range %q", containerPart)}
		}

		if end < start {
			return nil, []string{fmt.Sprintf("invalid port range %q: end < start", containerPart)}
		}

		count := end - start + 1
		if count > maxPortRangeExpansion {
			return nil, []string{fmt.Sprintf("port range %q expands to %d ports, exceeding cap of %d", containerPart, count, maxPortRangeExpansion)}
		}

		if start < minPort || end > maxPort {
			return nil, []string{fmt.Sprintf("port range %q contains ports outside valid range %d-%d", containerPart, minPort, maxPort)}
		}

		ports := make([]int, 0, count)
		for p := start; p <= end; p++ {
			ports = append(ports, p)
		}
		servicePortsLog.Printf("Parsed port range %q: %d ports (%d-%d)", containerPart, count, start, end)
		return ports, nil
	}

	// Single port
	port, err := strconv.Atoi(containerPart)
	if err != nil {
		return nil, []string{fmt.Sprintf("invalid port number %q", containerPart)}
	}

	if port < minPort || port > maxPort {
		return nil, []string{fmt.Sprintf("port %d is outside valid range %d-%d", port, minPort, maxPort)}
	}

	return []int{port}, nil
}
