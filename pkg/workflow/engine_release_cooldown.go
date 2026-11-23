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
		} else {
			resetDetectionEngineTaskSettings(detector.EngineConfig)
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
	if def != nil && def.RuntimeID != "" {
		runtimeID = def.RuntimeID
	}
	var engine CodingAgentEngine
	if resolved, err := c.engineRegistry.GetEngine(runtimeID); err == nil {
		engine = resolved
	} else if resolved, err := c.engineRegistry.GetEngineByPrefix(id); err == nil {
		engine = resolved
	}
	version := getVersionForSetup(data, c.engineRegistry)
	if engine != nil {
		runtimeID = engine.GetID()
		if def == nil {
			if behaviorEngine, ok := engine.(*BehaviorDefinedEngine); ok {
				def = behaviorEngine.definition
			}
			if def == nil && c.engineCatalog != nil {
				def = c.engineCatalog.Get(runtimeID)
			}
		}
		installationData := data
		if data.EngineConfig == nil {
			copy := *data
			copy.EngineConfig = &EngineConfig{ID: id}
			installationData = &copy
		}
		version = getInstallationVersion(installationData, engine, c.engineRegistry)
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
		packageName = builtinEngineNpmPackage(runtimeID)
		if packageName == "" {
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
		c.warnEngineReleaseCooldown(target, markdownPath)
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
	key := target.endpoint + "@" + target.version
	cache.Lock()
	metadata, found := cache.entries[key]
	if found && now.Sub(metadata.fetched) < engineReleaseCacheTTL && !now.Before(metadata.fetched) {
		cache.Unlock()
		return metadata
	}
	cache.Unlock()

	metadata = engineReleaseMetadata{fetched: now}
	metadata.published, metadata.err = engineReleaseTimeLookup(c, target.endpoint, target.packageName, target.version)
	if metadata.err == nil && (metadata.published.IsZero() || metadata.published.After(now)) {
		metadata.err = errors.New("missing or future release publication timestamp")
	}
	if c.ctx == nil || c.ctx.Err() == nil {
		cache.Lock()
		if cache.entries == nil {
			cache.entries = map[string]engineReleaseMetadata{}
		}
		cache.entries[key] = metadata
		cache.Unlock()
	}
	return metadata
}

func (c *Compiler) warnEngineReleaseCooldown(target engineReleaseTarget, markdownPath string) {
	key := target.endpoint + "@" + target.version
	if c.engineReleaseWarnings == nil {
		c.engineReleaseWarnings = map[string]bool{}
	}
	if !c.engineReleaseWarnings[key] {
		c.engineReleaseWarnings[key] = true
		message := fmt.Sprintf("Could not validate engine.cooldown for %s@%s from public release metadata; validate the release age manually against every applicable engine.cooldown setting before running this workflow.", target.id, target.version)
		c.diagnostics = append(c.diagnostics, CompilerDiagnostic{Path: markdownPath, Level: "warning", Message: message})
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
	if packageName != "" {
		request.Header.Set("Accept", "application/vnd.npm.install-v1+json")
	}
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
	if packageName != "" {
		return parseNPMReleaseTime(response.Body, packageName, version)
	}
	const maxMetadataBytes = 8 * 1024 * 1024
	body, err := io.ReadAll(io.LimitReader(response.Body, maxMetadataBytes+1))
	if err != nil {
		return time.Time{}, err
	}
	if len(body) > maxMetadataBytes {
		return time.Time{}, errors.New("release metadata exceeds the size limit")
	}
	return parseEngineReleaseTime(body, version)
}

func parseEngineReleaseTime(body []byte, version string) (time.Time, error) {
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
	return time.Parse(time.RFC3339Nano, release.PublishedAt)
}

func parseNPMReleaseTime(body io.Reader, packageName, version string) (time.Time, error) {
	decoder := json.NewDecoder(body)
	token, err := decoder.Token()
	if err != nil {
		return time.Time{}, err
	}
	if token != json.Delim('{') {
		return time.Time{}, errors.New("npm release metadata must be a JSON object")
	}

	var name, timestamp string
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return time.Time{}, err
		}
		key, ok := keyToken.(string)
		if !ok {
			return time.Time{}, errors.New("npm release metadata contains an invalid key")
		}
		switch key {
		case "name":
			if err := decoder.Decode(&name); err != nil {
				return time.Time{}, err
			}
		case "time":
			timeToken, err := decoder.Token()
			if err != nil {
				return time.Time{}, err
			}
			if timeToken != json.Delim('{') {
				return time.Time{}, errors.New("npm release timestamps must be a JSON object")
			}
			for decoder.More() {
				versionToken, err := decoder.Token()
				if err != nil {
					return time.Time{}, err
				}
				publishedVersion, ok := versionToken.(string)
				if !ok {
					return time.Time{}, errors.New("npm release metadata contains an invalid version key")
				}
				if publishedVersion == version {
					if err := decoder.Decode(&timestamp); err != nil {
						return time.Time{}, err
					}
				} else if err := skipJSONValue(decoder); err != nil {
					return time.Time{}, err
				}
			}
			closingToken, err := decoder.Token()
			if err != nil {
				return time.Time{}, err
			}
			if closingToken != json.Delim('}') {
				return time.Time{}, errors.New("npm release timestamps are not properly closed")
			}
		default:
			if err := skipJSONValue(decoder); err != nil {
				return time.Time{}, err
			}
		}
	}
	closingToken, err := decoder.Token()
	if err != nil {
		return time.Time{}, err
	}
	if closingToken != json.Delim('}') {
		return time.Time{}, errors.New("npm release metadata is not properly closed")
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		if err == nil {
			return time.Time{}, errors.New("npm release metadata contains trailing JSON")
		}
		return time.Time{}, err
	}
	if name != packageName {
		return time.Time{}, errors.New("release metadata does not match the requested npm package")
	}
	if timestamp == "" {
		return time.Time{}, errors.New("npm release metadata does not contain the requested version")
	}
	return time.Parse(time.RFC3339Nano, timestamp)
}

func skipJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delimiter {
	case '{':
		for decoder.More() {
			if _, err := decoder.Token(); err != nil {
				return err
			}
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
		closingToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closingToken != json.Delim('}') {
			return errors.New("JSON object is not properly closed")
		}
		return nil
	case '[':
		for decoder.More() {
			if err := skipJSONValue(decoder); err != nil {
				return err
			}
		}
		closingToken, err := decoder.Token()
		if err != nil {
			return err
		}
		if closingToken != json.Delim(']') {
			return errors.New("JSON array is not properly closed")
		}
		return nil
	default:
		return errors.New("unexpected JSON delimiter")
	}
}
