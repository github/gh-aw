package connectors

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

var stagedFileName = regexp.MustCompile(`^[0-9a-f]{64}(?:\.[A-Za-z0-9]{1,16})?$`)
var stagedFileExtension = regexp.MustCompile(`^\.[a-z0-9]{1,16}$`)

func validateBinaryRequest(request Request, inputs map[string]Input, phase string) error {
	policy := request.Binary
	if policy == nil {
		return nil
	}
	input, exists := inputs[policy.Input]
	if phase != "safe-output" || request.Method != http.MethodPost || request.GraphQL != "" || len(request.Body)+len(request.Patch)+len(request.AdditionalPatch) != 0 ||
		!exists || !input.Required || input.Type != "string" || input.Sanitize != "" || input.MaxLength > 1024 || policy.MaxBytes < 1 || policy.MaxBytes > 10*1024*1024 {
		return errors.New("binary requires a safe-output POST with one required string file input, no other body, and maxBytes from 1 to 10485760")
	}
	if len(policy.Extensions) > 16 || len(policy.RejectContains) > 16 {
		return errors.New("binary policy supports at most 16 extensions and rejected byte sequences")
	}
	for _, extension := range policy.Extensions {
		if !stagedFileExtension.MatchString(extension) {
			return errors.New("binary extensions require literal lowercase suffixes such as .png")
		}
	}
	for _, literal := range policy.RejectContains {
		if literal == "" || len(literal) > 128 {
			return errors.New("binary rejected byte sequences require 1 to 128 literal bytes")
		}
	}
	return nil
}

func validateBinaryOperation(op Operation, phase string) error {
	if err := validateBinaryRequest(op.Request, op.Inputs, phase); err != nil {
		return err
	}
	if op.Request.Binary != nil && (len(op.TrustedInputs)+len(op.Lookups) != 0 || op.Preflight != nil) {
		return errors.New("binary operations require canonical/receipt-bound inputs without secret preflights or alias lookups")
	}
	return nil
}

func readConnectorFile(root string, policy *BinaryRequest, args map[string]any) ([]byte, error) {
	name, ok := args[policy.Input].(string)
	if root == "" || !ok || !stagedFileName.MatchString(name) {
		return nil, errors.New("binary input requires a content-addressed file in the trusted host staging root")
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		return nil, errors.New("trusted connector file staging root is unavailable")
	}
	defer func() {
		if err := dir.Close(); err != nil {
			log.Printf("close connector staging root: %v", err)
		}
	}()
	info, err := dir.Lstat(name)
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("connector staged file must be a regular file, not a symlink")
	}
	file, err := openConnectorStagedFile(dir, name)
	if err != nil {
		return nil, errors.New("connector staged file could not be opened")
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("close connector staged file: %v", err)
		}
	}()
	opened, err := file.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return nil, errors.New("connector staged file changed or is not a regular file")
	}
	content, err := io.ReadAll(io.LimitReader(file, int64(policy.MaxBytes)+1))
	if err != nil {
		return nil, errors.New("connector staged file could not be read")
	}
	if err := validateConnectorFile(name, content, policy); err != nil {
		return nil, err
	}
	return content, nil
}

func validateConnectorFile(name string, content []byte, policy *BinaryRequest) error {
	sum := sha256.Sum256(content)
	if !stagedFileName.MatchString(name) || !strings.HasPrefix(name, hex.EncodeToString(sum[:])) || len(content) > policy.MaxBytes {
		return errors.New("connector staged file digest or byte bound does not match")
	}
	extension := strings.ToLower(filepath.Ext(name))
	if len(policy.Extensions) != 0 && !slices.Contains(policy.Extensions, extension) {
		return errors.New("connector staged file extension is not approved")
	}
	for _, literal := range policy.RejectContains {
		if bytes.Contains(content, []byte(literal)) {
			return errors.New("connector staged file contains a rejected byte sequence")
		}
	}
	return nil
}
