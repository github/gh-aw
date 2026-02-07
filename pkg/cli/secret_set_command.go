package cli

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/spf13/cobra"
	"golang.org/x/crypto/nacl/box"
)

var secretSetLog = logger.New("cli:secret_set_command")

type repoPublicKey struct {
	ID  string `json:"key_id"`
	Key string `json:"key"`
}

type secretPayload struct {
	EncryptedValue string `json:"encrypted_value"`
	KeyID          string `json:"key_id"`
}

const publicKeySize = 32 // NaCl box public key size

func newSecretsSetSubcommand() *cobra.Command {
	var (
		flagOwner    string
		flagRepo     string
		flagValue    string
		flagValueEnv string
		flagAPIBase  string
	)

	cmd := &cobra.Command{
		Use:   "set <secret-name>",
		Short: "Create or update a repository secret",
		Long: `Create or update a GitHub Actions secret for a repository.

The secret value can be provided in three ways:
  1. Via the --value flag
  2. Via the --value-from-env flag (reads from environment variable)
  3. From stdin (if neither flag is provided)

Examples:
  # From stdin
  gh aw secrets set MY_SECRET --owner myorg --repo myrepo

  # From flag
  gh aw secrets set MY_SECRET --value "secret123" --owner myorg --repo myrepo

  # From environment variable
  export MY_TOKEN="secret123"
  gh aw secrets set MY_SECRET --value-from-env MY_TOKEN --owner myorg --repo myrepo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			secretName := args[0]
			secretSetLog.Printf("Setting repository secret: name=%s", secretName)

			// Determine target repository: explicit --owner/--repo or current repo by default
			var owner, repo string
			if flagOwner != "" || flagRepo != "" {
				// Both must be provided together when overriding the target repository
				if flagOwner == "" || flagRepo == "" {
					return fmt.Errorf("both --owner and --repo must be specified together when overriding the target repository")
				}
				owner, repo = flagOwner, flagRepo
				secretSetLog.Printf("Using explicit repository: %s/%s", owner, repo)
			} else {
				repoSlug, err := GetCurrentRepoSlug()
				if err != nil {
					secretSetLog.Printf("Failed to detect current repository: %v", err)
					return fmt.Errorf("failed to detect current repository: %w", err)
				}
				var splitErr error
				owner, repo, splitErr = SplitRepoSlug(repoSlug)
				if splitErr != nil {
					return fmt.Errorf("invalid current repository slug %q: %w", repoSlug, splitErr)
				}
				secretSetLog.Printf("Using current repository: %s/%s", owner, repo)
			}

			// Create GitHub REST client using go-gh
			opts := api.ClientOptions{}
			if flagAPIBase != "" {
				opts.Host = strings.TrimPrefix(strings.TrimPrefix(flagAPIBase, "https://"), "http://")
			}
			client, err := api.NewRESTClient(opts)
			if err != nil {
				return fmt.Errorf("cannot create GitHub client: %w", err)
			}

			secretValue, err := resolveSecretValueForSet(flagValueEnv, flagValue)
			if err != nil {
				secretSetLog.Printf("Failed to resolve secret value: %v", err)
				return fmt.Errorf("cannot resolve secret value: %w", err)
			}

			secretSetLog.Print("Encrypting and uploading secret to GitHub")
			if err := setRepoSecret(client, owner, repo, secretName, secretValue); err != nil {
				secretSetLog.Printf("Failed to set secret: %v", err)
				return fmt.Errorf("failed to set secret: %w", err)
			}

			secretSetLog.Printf("Successfully set secret %s for %s/%s", secretName, owner, repo)
			fmt.Fprintln(os.Stderr, console.FormatSuccessMessage(fmt.Sprintf("Secret %s updated for %s/%s", secretName, owner, repo)))
			return nil
		},
	}

	cmd.Flags().StringVar(&flagOwner, "owner", "", "GitHub repository owner or organization (defaults to current repository)")
	cmd.Flags().StringVar(&flagRepo, "repo", "", "GitHub repository name (defaults to current repository)")
	cmd.Flags().StringVar(&flagValue, "value", "", "Secret value (if empty, read from stdin)")
	cmd.Flags().StringVar(&flagValueEnv, "value-from-env", "", "Environment variable to read secret value from")
	cmd.Flags().StringVar(&flagAPIBase, "api-url", "", "GitHub API base URL (default: https://api.github.com or $GITHUB_API_URL)")

	return cmd
}

func resolveSecretValueForSet(fromEnv, fromFlag string) (string, error) {
	if fromEnv != "" {
		v := os.Getenv(fromEnv)
		if v == "" {
			return "", fmt.Errorf("environment variable %s is not set or empty", fromEnv)
		}
		return v, nil
	}

	if fromFlag != "" {
		return fromFlag, nil
	}

	// Check if stdin is connected to a terminal (interactive mode)
	info, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}

	isTerminal := (info.Mode() & os.ModeCharDevice) != 0

	// If we're in an interactive terminal, use Huh for a better UX with password masking
	if isTerminal && tty.IsStderrTerminal() {
		secretSetLog.Print("Using interactive password prompt with Huh")
		value, err := console.PromptSecretInput(
			"Enter secret value",
			"The value will be encrypted and stored in the repository",
		)
		if err != nil {
			secretSetLog.Printf("Interactive prompt failed: %v", err)
			return "", fmt.Errorf("failed to read secret value: %w", err)
		}
		return value, nil
	}

	// Fallback to non-interactive stdin reading (piped input or non-TTY)
	secretSetLog.Print("Using non-interactive stdin reading")
	if isTerminal {
		fmt.Fprintln(os.Stderr, "Enter secret value, then press Ctrl+D:")
	}

	reader := io.Reader(os.Stdin)
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", err
	}

	value := strings.TrimRight(string(data), "\r\n")
	if value == "" {
		return "", errors.New("secret value is empty")
	}
	return value, nil
}

func setRepoSecret(client *api.RESTClient, owner, repo, name, value string) error {
	pubKey, err := getRepoPublicKey(client, owner, repo)
	if err != nil {
		return fmt.Errorf("get repo public key: %w", err)
	}

	encrypted, err := encryptWithPublicKey(pubKey.Key, value)
	if err != nil {
		return fmt.Errorf("encrypt secret: %w", err)
	}

	return putRepoSecret(client, owner, repo, name, pubKey.ID, encrypted)
}

func getRepoPublicKey(client *api.RESTClient, owner, repo string) (*repoPublicKey, error) {
	var key repoPublicKey
	path := fmt.Sprintf("repos/%s/%s/actions/secrets/public-key", owner, repo)
	if err := client.Get(path, &key); err != nil {
		return nil, fmt.Errorf("get public key: %w", err)
	}
	if key.ID == "" || key.Key == "" {
		return nil, errors.New("public key response missing key_id or key")
	}
	return &key, nil
}

// encryptWithPublicKey encrypts plaintext using NaCl's sealed box construction
// (Curve25519 + XSalsa20 + Poly1305) as required by GitHub's Actions Secrets API.
// The encrypted output can only be decrypted by the holder of the private key
// corresponding to the provided public key.
//
// Parameters:
//   - publicKeyB64: Base64-encoded 32-byte NaCl public key
//   - plaintext: Secret value to encrypt
//
// Returns base64-encoded ciphertext or error.
func encryptWithPublicKey(publicKeyB64, plaintext string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil {
		return "", fmt.Errorf("decode public key: %w", err)
	}
	if len(raw) != publicKeySize {
		return "", fmt.Errorf("unexpected public key length: %d, expected %d", len(raw), publicKeySize)
	}

	var pk [publicKeySize]byte
	copy(pk[:], raw)

	ciphertext, err := box.SealAnonymous(nil, []byte(plaintext), &pk, rand.Reader)
	if err != nil {
		return "", fmt.Errorf("nacl encryption failed: %w", err)
	}

	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func putRepoSecret(client *api.RESTClient, owner, repo, name, keyID, encryptedValue string) error {
	path := fmt.Sprintf("repos/%s/%s/actions/secrets/%s", owner, repo, name)
	payload := secretPayload{
		EncryptedValue: encryptedValue,
		KeyID:          keyID,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return client.Put(path, strings.NewReader(string(body)), nil)
}
