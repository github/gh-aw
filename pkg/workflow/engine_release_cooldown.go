package workflow

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var engineReleaseVersionPattern = regexp.MustCompile(`^v?(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$`)
var engineReleasePackagePattern = regexp.MustCompile(`^(@[a-z0-9][a-z0-9._-]*/)?[a-z0-9][a-z0-9._-]*$`)
var engineReleaseCooldownLog = logger.New("workflow:engine_release_cooldown")
var engineReleaseNPMPackages = map[string]string{
	"claude": "@anthropic-ai/claude-code",
	"codex":  "@openai/codex",
	"pi":     "@earendil-works/pi-coding-agent",
	"gemini": "@google/gemini-cli",
}

const engineReleaseCacheTTL = 5 * time.Minute

type engineReleaseMetadata struct {
	published time.Time
	fetched   time.Time
	err       error
}

// Cache publication times, not policy decisions: another workflow can require a
// longer cooldown, and a young release can mature while the compiler is running.
// Both successes and failures expire after five minutes; nothing is persisted to disk.
type engineReleaseCache struct {
	sync.Mutex
	entries map[string]engineReleaseMetadata
}

var sharedEngineReleaseCache = &engineReleaseCache{entries: map[string]engineReleaseMetadata{}}
var engineReleaseTimeLookup = (*Compiler).fetchEngineReleaseTime

func literalEngineReleaseVersion(version string) bool {
	return engineReleaseVersionPattern.MatchString(version)
}

func (c *Compiler) validateEngineReleaseCooldowns(data *WorkflowData, markdownPath string) error {
	if data == nil {
		return nil
	}
	if err := c.validateEngineReleaseCooldown(data, markdownPath); err != nil {
		return err
	}
	if IsDetectionJobEnabled(data.SafeOutputs) && !data.SafeOutputs.ThreatDetection.EngineDisabled {
		id := c.getThreatDetectionEngineID(data)
		detector := &WorkflowData{AI: id, EngineConfig: resolveExternalDetectorEngineConfig(data, id)}
		if isFeatureEnabled(constants.GHAWDetectionFeatureFlag, data) {
			detector = buildExternalDetectorWorkflowData(data, id)
		}
		if ResolveEngineID(data) == id {
			detector.engineDefinition = data.engineDefinition
		}
		if err := c.validateEngineReleaseCooldown(detector, markdownPath); err != nil {
			return fmt.Errorf("safe-outputs.threat-detection: %w", err)
		}
	}
	return nil
}

type engineReleaseTarget struct {
	id          string
	version     string
	packageName string
	endpoint    string
}

func (c *Compiler) engineReleaseTarget(data *WorkflowData) (engineReleaseTarget, bool) {
	id := ResolveEngineID(data)
	def := data.engineDefinition
	if def == nil && c.engineCatalog != nil {
		def = c.engineCatalog.Get(id)
	}
	runtimeID := id
	version := getVersionForSetup(data, c.engineRegistry)
	if engine, err := c.engineRegistry.GetEngineByPrefix(id); err == nil {
		runtimeID = engine.GetID()
		version = getInstallationVersion(data, engine, c.engineRegistry)
	}
	if def != nil && def.RuntimeID != "" {
		runtimeID = def.RuntimeID
	}
	packageName := ""
	if def != nil && def.Behaviors != nil {
		install := def.Behaviors.Installation
		if install == nil || install.PackageManager != "npm" {
			return engineReleaseTarget{}, false
		}
		packageName = install.PackageName
		if version == "" {
			version = install.Version
		}
		if data.EngineConfig != nil && data.EngineConfig.Version != "" {
			version = data.EngineConfig.Version
		}
	} else if runtimeID != "copilot" {
		var supported bool
		if packageName, supported = engineReleaseNPMPackages[runtimeID]; !supported {
			return engineReleaseTarget{}, false
		}
	}
	if !literalEngineReleaseVersion(version) {
		return engineReleaseTarget{}, false
	}
	version = strings.TrimPrefix(version, "v")
	endpoint := "https://api.github.com/repos/github/copilot-cli/releases/tags/v" + url.PathEscape(version)
	if packageName != "" {
		if !engineReleasePackagePattern.MatchString(packageName) {
			return engineReleaseTarget{}, false
		}
		endpoint = "https://registry.npmjs.org/" + url.PathEscape(packageName)
	}
	return engineReleaseTarget{id: id, version: version, packageName: packageName, endpoint: endpoint}, true
}

func (c *Compiler) validateEngineReleaseCooldown(data *WorkflowData, markdownPath string) error {
	if data == nil {
		return nil
	}
	days := resolveEngineCooldownDays(data)
	if days == 0 || (data.EngineConfig != nil && data.EngineConfig.Command != "") {
		return nil
	}
	target, supported := c.engineReleaseTarget(data)
	if !supported {
		return nil
	}
	now := time.Now()
	if c.engineReleaseNow != nil {
		now = c.engineReleaseNow()
	}
	metadata := c.engineReleaseMetadata(target, now)
	if c.ctx != nil && c.ctx.Err() != nil {
		return c.ctx.Err()
	}
	if metadata.err != nil {
		c.warnEngineReleaseCooldown(target, days, markdownPath)
		return nil
	}
	if now.Sub(metadata.published).Hours()/24 < float64(days) {
		return NewValidationError("engine.cooldown", fmt.Sprintf("%s@%s", target.id, target.version),
			fmt.Sprintf("release published at %s is younger than the required %d-day cooldown", metadata.published.Format(time.RFC3339), days),
			"Choose an older engine.version or wait until the release satisfies the cooldown.")
	}
	return nil
}

func (c *Compiler) engineReleaseMetadata(target engineReleaseTarget, now time.Time) engineReleaseMetadata {
	if c.verifyAuthorization || c.contentOverride != "" {
		return engineReleaseMetadata{err: errors.New("metadata lookup is unavailable during offline or editor compilation")}
	}
	cache := c.engineReleaseCache
	if cache == nil {
		cache = sharedEngineReleaseCache
		if c.engineReleaseClient != nil {
			cache = &engineReleaseCache{entries: map[string]engineReleaseMetadata{}}
		}
		c.engineReleaseCache = cache
	}
	cache.Lock()
	defer cache.Unlock()
	key := target.endpoint + "@" + target.version
	metadata, found := cache.entries[key]
	if !found || now.Sub(metadata.fetched) >= engineReleaseCacheTTL || now.Before(metadata.fetched) {
		metadata = engineReleaseMetadata{fetched: now}
		metadata.published, metadata.err = engineReleaseTimeLookup(c, target.endpoint, target.packageName, target.version)
		if metadata.err == nil && (metadata.published.IsZero() || metadata.published.After(now)) {
			metadata.err = errors.New("missing or future release publication timestamp")
		}
		if cache.entries == nil {
			cache.entries = map[string]engineReleaseMetadata{}
		}
		if c.ctx == nil || c.ctx.Err() == nil {
			cache.entries[key] = metadata
		}
	}
	return metadata
}

func (c *Compiler) warnEngineReleaseCooldown(target engineReleaseTarget, days int, markdownPath string) {
	key := target.endpoint + "@" + target.version
	if c.engineReleaseWarnings == nil {
		c.engineReleaseWarnings = map[string]bool{}
	}
	if !c.engineReleaseWarnings[key] {
		c.engineReleaseWarnings[key] = true
		message := fmt.Sprintf("Could not validate engine.cooldown for %s@%s from public release metadata; validate the release age manually (minimum %d days) before running this workflow.", target.id, target.version, days)
		fmt.Fprintln(os.Stderr, formatCompilerMessage(markdownPath, "warning", message))
		c.IncrementWarningCount()
	}
}

func (c *Compiler) fetchEngineReleaseTime(endpoint, packageName, version string) (time.Time, error) {
	ctx := c.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return time.Time{}, err
	}
	request.Header.Set("Accept", "application/json")
	// Never inherit GH_HOST, registry configuration, or caller/runner credentials.
	client := &http.Client{Timeout: 5 * time.Second}
	if c.engineReleaseClient != nil {
		*client = *c.engineReleaseClient
	}
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error {
		return errors.New("release metadata redirects are not allowed")
	}
	response, err := client.Do(request)
	if err != nil {
		return time.Time{}, err
	}
	defer func() {
		if err := response.Body.Close(); err != nil {
			engineReleaseCooldownLog.Printf("Closing release metadata response: %v", err)
		}
	}()
	if response.StatusCode != http.StatusOK {
		return time.Time{}, fmt.Errorf("release metadata returned HTTP %d", response.StatusCode)
	}
	const maxMetadataBytes = 8 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return time.Time{}, err
	}
	if len(body) > maxMetadataBytes {
		return time.Time{}, errors.New("release metadata exceeds the size limit")
	}
	return parseEngineReleaseTime(body, packageName, version)
}

func parseEngineReleaseTime(body []byte, packageName, version string) (time.Time, error) {
	var timestamp string
	if packageName == "" {
		var release struct {
			TagName     string `json:"tag_name"`
			PublishedAt string `json:"published_at"`
			Draft       bool   `json:"draft"`
		}
		if err := json.Unmarshal(body, &release); err != nil {
			return time.Time{}, err
		}
		if release.TagName != "v"+version || release.Draft {
			return time.Time{}, errors.New("release metadata does not match the requested published tag")
		}
		timestamp = release.PublishedAt
	} else {
		var packument struct {
			Name string            `json:"name"`
			Time map[string]string `json:"time"`
		}
		if err := json.Unmarshal(body, &packument); err != nil {
			return time.Time{}, err
		}
		if packument.Name != packageName {
			return time.Time{}, errors.New("release metadata does not match the requested npm package")
		}
		timestamp = packument.Time[version]
	}
	return time.Parse(time.RFC3339Nano, timestamp)
}
