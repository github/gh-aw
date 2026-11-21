package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

// A nonempty, invalid URL suppresses the variable fallback without contacting a collector.
const envOTELDisabledEndpoint = "://gh-aw-otel-disabled"

type envOTELSecretMetadata struct {
	Name       string `json:"name"`
	Visibility string `json:"visibility"`
}

func envOTELSecret(ctx context.Context, p envOTELPlan, name, value string) error {
	encrypted, err := encryptWithPublicKey(p.key.Key, value)
	if err != nil {
		return errors.New("failed to encrypt OTLP secret")
	}
	payload := map[string]any{"key_id": p.key.ID, "encrypted_value": encrypted}
	envOTELAccess(payload, p.target, p.ids)
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("failed to encode OTLP secret")
	}
	path := strings.TrimSuffix(p.target.variablesEndpoint(), "variables") + "secrets/" + name
	_, err = envOTELRunGH(ctx, body, "api", "-X", "PUT", path, "--input", "-")
	return err
}

func envOTELSecrets(ctx context.Context, target defaultsTarget) ([]envOTELSecretMetadata, error) {
	path := strings.TrimSuffix(target.variablesEndpoint(), "variables") + "secrets?per_page=100"
	data, err := envOTELRunGH(ctx, nil, "api", path, "--paginate", "--jq",
		`.secrets[] | select(.name == "GH_AW_DEFAULT_OTLP_ENDPOINT" or .name == "GH_AW_DEFAULT_OTLP_HEADERS")`)
	if err != nil {
		return nil, fmt.Errorf("%s secret access: %w", target.displayName(), err)
	}
	var result []envOTELSecretMetadata
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var secret envOTELSecretMetadata
		if err := decoder.Decode(&secret); err == io.EOF {
			return result, nil
		} else if err != nil || secret.Name != compilerenv.DefaultOTLPEndpoint && secret.Name != compilerenv.DefaultOTLPHeaders {
			return nil, errors.New("invalid GitHub OTLP secret metadata")
		}
		result = append(result, secret)
	}
}

func envOTELCheckOverrides(ctx context.Context, p envOTELPlan, selected []string) error {
	if p.target.scope != defaultsScopeOrg {
		return nil
	}
	secrets, err := envOTELSecrets(ctx, p.target)
	if err != nil {
		return err
	}
	for _, secret := range secrets {
		if err := envOTELCheckSecretAccess(ctx, p, secret); err != nil {
			return err
		}
	}
	repositories, err := envOTELRepositories(ctx, p.target, selected)
	if err != nil {
		return err
	}
	for _, name := range repositories {
		owner, repo, found := strings.Cut(name, "/")
		if !found || !strings.EqualFold(owner, p.target.org) || !envOTELRepositoryName.MatchString(repo) || repo == "." || repo == ".." {
			return errors.New("invalid organization repository response")
		}
		secrets, err := envOTELSecrets(ctx, defaultsTarget{scope: defaultsScopeRepo, repoOwner: owner, repoName: repo})
		if err != nil {
			return err
		}
		endpoint, headers := false, false
		for _, secret := range secrets {
			endpoint = endpoint || secret.Name == compilerenv.DefaultOTLPEndpoint
			headers = headers || secret.Name == compilerenv.DefaultOTLPHeaders
		}
		if endpoint != headers {
			return fmt.Errorf("%s has a partial OTLP secret override; configure both endpoint and headers at repository scope before organization setup", name)
		}
	}
	return nil
}

func envOTELCheckSecretAccess(ctx context.Context, p envOTELPlan, secret envOTELSecretMetadata) error {
	mismatch := fmt.Errorf("%s OTLP secret visibility differs from the requested access; migrate both secrets together before changing visibility", p.target.displayName())
	if secret.Visibility != p.target.visibility {
		return mismatch
	}
	if secret.Visibility != defaultsVisibilitySel {
		return nil
	}
	endpoint := path.Join(strings.TrimSuffix(p.target.variablesEndpoint(), "variables"), "secrets", secret.Name, "repositories") + "?per_page=100"
	data, err := envOTELRunGH(ctx, nil, "api", endpoint, "--paginate", "--jq", ".repositories[].id")
	if err != nil {
		return fmt.Errorf("selected OTLP secret repository access: %w", err)
	}
	var ids []int64
	for value := range strings.FieldsSeq(string(data)) {
		id, err := strconv.ParseInt(value, 10, 64)
		if err != nil || id <= 0 {
			return errors.New("invalid selected OTLP secret repository response")
		}
		ids = append(ids, id)
	}
	want := slices.Clone(p.ids)
	slices.Sort(ids)
	slices.Sort(want)
	if !slices.Equal(ids, want) {
		return mismatch
	}
	return nil
}

func envOTELRepositories(ctx context.Context, target defaultsTarget, selected []string) ([]string, error) {
	if target.visibility == defaultsVisibilitySel {
		return selected, nil
	}
	data, err := envOTELRunGH(ctx, nil, "api", "orgs/"+target.org)
	if err != nil {
		return nil, fmt.Errorf("organization repository inventory: %w", err)
	}
	var org struct {
		PublicRepos  int  `json:"public_repos"`
		PrivateRepos *int `json:"total_private_repos"`
	}
	if json.Unmarshal(data, &org) != nil || org.PrivateRepos == nil {
		return nil, errors.New("cannot verify the organization's complete repository inventory; use credentials with organization owner access or --visibility selected")
	}
	data, err = envOTELRunGH(ctx, nil, "api", path.Join("orgs", target.org, "repos")+"?type=all&per_page=100", "--paginate", "--jq", ".[] | {full_name, private}")
	if err != nil {
		return nil, fmt.Errorf("organization repository enumeration: %w", err)
	}
	return envOTELRepositoryNames(data, target.visibility, org.PublicRepos, *org.PrivateRepos)
}

func envOTELRepositoryNames(data []byte, visibility string, publicTotal, privateTotal int) ([]string, error) {
	var names []string
	publicCount, privateCount := 0, 0
	seen := map[string]struct{}{}
	decoder := json.NewDecoder(bytes.NewReader(data))
	for {
		var repo struct {
			Name    string `json:"full_name"`
			Private *bool  `json:"private"`
		}
		if err := decoder.Decode(&repo); err == io.EOF {
			break
		} else if err != nil {
			return nil, errors.New("invalid organization repository inventory")
		}
		_, duplicate := seen[strings.ToLower(repo.Name)]
		if repo.Name == "" || repo.Private == nil || duplicate {
			return nil, errors.New("invalid organization repository inventory")
		}
		seen[strings.ToLower(repo.Name)] = struct{}{}
		if *repo.Private {
			privateCount++
		} else {
			publicCount++
		}
		if *repo.Private || visibility == defaultsVisibilityAll {
			names = append(names, repo.Name)
		}
	}
	if privateCount != privateTotal || publicCount != publicTotal {
		return nil, errors.New("organization repository inventory is incomplete or changed; no settings saved")
	}
	return names, nil
}
