//go:build !integration

package cli

import (
	"crypto/rand"
	"encoding/base64"
	"strings"
	"testing"

	"golang.org/x/crypto/nacl/box"
)

func TestResolveSecretValueForSet(t *testing.T) {
	tests := []struct {
		name        string
		fromEnv     string
		fromFlag    string
		envValue    string
		wantErr     bool
		wantValue   string
		errContains string
	}{
		{
			name:      "from flag",
			fromFlag:  "secret123",
			wantValue: "secret123",
		},
		{
			name:      "from env var - set",
			fromEnv:   "TEST_SECRET",
			envValue:  "envvalue123",
			wantValue: "envvalue123",
		},
		{
			name:        "from env var - empty",
			fromEnv:     "TEST_SECRET_MISSING",
			wantErr:     true,
			errContains: "not set or empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Set up environment
			if tt.envValue != "" {
				t.Setenv(tt.fromEnv, tt.envValue)
			}

			got, err := resolveSecretValueForSet(tt.fromEnv, tt.fromFlag)
			if (err != nil) != tt.wantErr {
				t.Errorf("resolveSecretValueForSet() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.wantErr && tt.errContains != "" {
				if err == nil || !strings.Contains(err.Error(), tt.errContains) {
					t.Errorf("expected error containing %q, got %v", tt.errContains, err)
				}
			}
			if !tt.wantErr && got != tt.wantValue {
				t.Errorf("resolveSecretValueForSet() = %v, want %v", got, tt.wantValue)
			}
		})
	}
}

func TestEncryptWithPublicKey(t *testing.T) {
	// Valid 32-byte public key in base64
	validKey := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXpBQkNERUY="
	plaintext := "my-secret-value"

	encrypted, err := encryptWithPublicKey(validKey, plaintext)
	if err != nil {
		t.Fatalf("encryptWithPublicKey() error = %v", err)
	}

	if encrypted == "" {
		t.Error("encryptWithPublicKey() returned empty string")
	}

	// The encrypted value should be different from the plaintext
	if encrypted == plaintext {
		t.Error("encrypted value should differ from plaintext")
	}
}

func TestEncryptWithPublicKeyInvalidKey(t *testing.T) {
	tests := []struct {
		name        string
		key         string
		plaintext   string
		errContains string
	}{
		{
			name:        "invalid base64",
			key:         "not-valid-base64!@#$",
			plaintext:   "secret",
			errContains: "decode public key",
		},
		{
			name:        "wrong key length",
			key:         "YWJjZA==", // "abcd" in base64 = 4 bytes, not 32
			plaintext:   "secret",
			errContains: "unexpected public key length",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := encryptWithPublicKey(tt.key, tt.plaintext)
			if err == nil {
				t.Fatal("encryptWithPublicKey() expected error, got nil")
			}
			if !strings.Contains(err.Error(), tt.errContains) {
				t.Errorf("expected error containing %q, got %v", tt.errContains, err)
			}
		})
	}
}

func TestEncryptWithPublicKeyEmptyPlaintext(t *testing.T) {
	validKey := "YWJjZGVmZ2hpamtsbW5vcHFyc3R1dnd4eXpBQkNERUY="
	encrypted, err := encryptWithPublicKey(validKey, "")
	if err != nil {
		t.Fatalf("encryptWithPublicKey() error = %v, expected no error", err)
	}
	if encrypted == "" {
		t.Error("expected non-empty ciphertext even for empty plaintext")
	}
	// NaCl sealed box adds 48 bytes overhead (16 auth + 32 ephemeral pubkey)
	// So even empty plaintext should produce base64 of at least 64 characters
	if len(encrypted) < 64 {
		t.Errorf("encrypted length = %d, expected at least 64 (base64 of 48-byte overhead)", len(encrypted))
	}
}

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Generate a real key pair for testing
	pub, priv, err := box.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("failed to generate key pair: %v", err)
	}

	// Encode public key as base64
	pubB64 := base64.StdEncoding.EncodeToString(pub[:])
	plaintext := "test-secret-value"

	// Encrypt using our function
	encrypted, err := encryptWithPublicKey(pubB64, plaintext)
	if err != nil {
		t.Fatalf("encryptWithPublicKey() error = %v", err)
	}

	// Decrypt using NaCl box to verify correctness
	cipherBytes, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("failed to decode encrypted data: %v", err)
	}

	decrypted, ok := box.OpenAnonymous(nil, cipherBytes, pub, priv)
	if !ok {
		t.Fatal("decryption failed - ciphertext was not valid")
	}

	if string(decrypted) != plaintext {
		t.Errorf("decrypted = %q, want %q", string(decrypted), plaintext)
	}
}
