package vault

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	vaultapi "github.com/hashicorp/vault/api"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	vaultpkg "github.com/redhat-data-and-ai/fivetran-operator/pkg/vault"
)

var (
	ErrInvalidVaultReference = errors.New("invalid vault reference format (expected format: vault:path#key)")
	ErrSecretDataNil         = errors.New("secret data is nil")
	ErrSecretNotFound        = errors.New("secret not found at path")
	ErrKeyNotFound           = errors.New("key not found in vault secret")
)

// VaultError represents a vault resolution error with retryability information
type VaultError struct {
	Err       error
	Retryable bool
	KeyPath   string
	VaultRef  string
}

func (e *VaultError) Error() string {
	if e.KeyPath != "" && e.VaultRef != "" {
		return fmt.Sprintf("%s: vault reference '%s': %s", e.KeyPath, e.VaultRef, e.Err.Error())
	}
	return e.Err.Error()
}

func (e *VaultError) IsRetryable() bool {
	return e.Retryable
}

// IsRetryableError checks if an error is retryable
func IsRetryableError(err error) bool {
	var vErr *VaultError
	if errors.As(err, &vErr) {
		return vErr.IsRetryable()
	}
	return true // Unknown errors are retryable by default
}

// Helper functions for creating standard errors
func NewInvalidReferenceError(keyPath, vaultRef, details string) *VaultError {
	return &VaultError{
		Err:       fmt.Errorf("%w: %s", ErrInvalidVaultReference, details),
		Retryable: false,
		KeyPath:   keyPath,
		VaultRef:  vaultRef,
	}
}

func NewKeyNotFoundError(keyPath, key, path string, availableKeys []string) *VaultError {
	return &VaultError{
		Err:       fmt.Errorf("%w '%s' at path '%s' (available keys: %v)", ErrKeyNotFound, key, path, availableKeys),
		Retryable: false,
		KeyPath:   keyPath,
		VaultRef:  fmt.Sprintf("vault:%s#%s", path, key),
	}
}

func NewSecretNotFoundError(keyPath, vaultRef, path string) *VaultError {
	return &VaultError{
		Err:       fmt.Errorf("%w '%s'", ErrSecretNotFound, path),
		Retryable: false,
		KeyPath:   keyPath,
		VaultRef:  vaultRef,
	}
}

func NewSecretDataNilError(keyPath, vaultRef string) *VaultError {
	return &VaultError{
		Err:       ErrSecretDataNil,
		Retryable: false,
		KeyPath:   keyPath,
		VaultRef:  vaultRef,
	}
}

func NewVaultAPIError(keyPath, vaultRef string, err error) *VaultError {
	return &VaultError{
		Err:       fmt.Errorf("failed to read vault secret: %w", err),
		Retryable: true,
		KeyPath:   keyPath,
		VaultRef:  vaultRef,
	}
}

// ResolveSecrets resolves string values that start with "vault:" (vault:path#key)
// throughout the given RawExtension. It minimizes Vault API usage by caching
// path lookups and fails fast on any error.
func ResolveSecrets(ctx context.Context, vaultClient *vaultpkg.VaultClient, rawConfig *runtime.RawExtension) error {
	if rawConfig == nil || rawConfig.Raw == nil {
		return nil
	}

	var data any
	if err := json.Unmarshal(rawConfig.Raw, &data); err != nil {
		return fmt.Errorf("ResolveSecrets: failed to unmarshal config: %w", err)
	}

	// Use a simple cache map for this call
	cache := make(map[string]map[string]any)

	resolvedData, err := resolveValue(ctx, vaultClient, cache, data, "")
	if err != nil {
		return err
	}

	updatedConfig, err := json.Marshal(resolvedData)
	if err != nil {
		return fmt.Errorf("ResolveSecrets: failed to marshal resolved config: %w", err)
	}

	rawConfig.Raw = updatedConfig
	return nil
}

// resolveValue recursively processes data structures to resolve vault secrets
func resolveValue(ctx context.Context, vaultClient *vaultpkg.VaultClient, cache map[string]map[string]any, data any, keyPath string) (any, error) {
	switch v := data.(type) {
	case map[string]any:
		return resolveMap(ctx, vaultClient, cache, v, keyPath)
	case []any:
		return resolveSlice(ctx, vaultClient, cache, v, keyPath)
	case string:
		return resolveString(ctx, vaultClient, cache, v, keyPath)
	default:
		return data, nil
	}
}

func resolveMap(ctx context.Context, vaultClient *vaultpkg.VaultClient, cache map[string]map[string]any, data map[string]any, keyPath string) (map[string]any, error) {
	result := make(map[string]any)

	for key, value := range data {
		currentPath := buildKeyPath(keyPath, key)
		resolvedValue, err := resolveValue(ctx, vaultClient, cache, value, currentPath)
		if err != nil {
			return nil, err
		}
		result[key] = resolvedValue
	}

	return result, nil
}

func resolveSlice(ctx context.Context, vaultClient *vaultpkg.VaultClient, cache map[string]map[string]any, data []any, keyPath string) ([]any, error) {
	result := make([]any, len(data))

	for i, item := range data {
		currentPath := fmt.Sprintf("%s[%d]", keyPath, i)
		resolvedValue, err := resolveValue(ctx, vaultClient, cache, item, currentPath)
		if err != nil {
			return nil, err
		}
		result[i] = resolvedValue
	}

	return result, nil
}

func resolveString(ctx context.Context, vaultClient *vaultpkg.VaultClient, cache map[string]map[string]any, value string, keyPath string) (any, error) {
	if !strings.HasPrefix(value, "vault:") {
		return value, nil
	}

	logger := log.FromContext(ctx)
	logger.V(1).Info("Resolving vault reference", "value", value)

	path, key, err := parseVaultReference(value)
	if err != nil {
		logger.V(1).Info("Failed to parse vault reference", "value", value, "error", err)
		return "", NewInvalidReferenceError(keyPath, value, err.Error())
	}

	// Get secret data with caching
	secretData, err := getPathData(vaultClient, cache, path, keyPath, value)
	if err != nil {
		logger.V(1).Info("Failed to get vault secret", "value", value, "error", err)
		return "", err
	}

	secretValue, exists := secretData[key]
	if !exists {
		availableKeys := getKeys(secretData)
		return "", NewKeyNotFoundError(keyPath, key, path, availableKeys)
	}

	return secretValue, nil
}

// getPathData returns secret data for a Vault KV path, using cache when possible
func getPathData(vaultClient *vaultpkg.VaultClient, cache map[string]map[string]any, path, keyPath, vaultRef string) (map[string]any, error) {
	// Check cache first
	if data, ok := cache[path]; ok {
		return data, nil
	}

	secret, err := vaultClient.Client.KVv2(vaultClient.Config.MountPath).Get(context.Background(), path)
	if err != nil {
		return nil, NewVaultAPIError(keyPath, vaultRef, err)
	}

	data, err := extractSecretData(secret.Raw)
	if err != nil {
		if errors.Is(err, ErrSecretDataNil) {
			return nil, NewSecretDataNilError(keyPath, vaultRef)
		}
		return nil, NewSecretNotFoundError(keyPath, vaultRef, path)
	}

	// Cache the result
	cache[path] = data
	return data, nil
}

// parseVaultReference parses vault:path#key format
func parseVaultReference(value string) (path, key string, err error) {
	ref := strings.TrimPrefix(value, "vault:")
	parts := strings.SplitN(ref, "#", 2)
	if len(parts) != 2 || len(parts[0]) == 0 || len(parts[1]) == 0 {
		return "", "", fmt.Errorf("%w: '%s'", ErrInvalidVaultReference, value)
	}
	return parts[0], parts[1], nil
}

// extractSecretData extracts secret data from KV v2 format
func extractSecretData(secret *vaultapi.Secret) (map[string]any, error) {
	if secret == nil {
		return nil, ErrSecretDataNil
	}

	if secret.Data == nil {
		return nil, ErrSecretDataNil
	}

	// Extract data from KV v2 format (data is nested under "data" key)
	data, ok := secret.Data["data"].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("invalid KV v2 format: 'data' key not found or not a map")
	}

	return data, nil
}

// buildKeyPath builds nested key paths for error messages
func buildKeyPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}

// getKeys returns available keys from a map for error messages
func getKeys(data map[string]any) []string {
	keys := make([]string, 0, len(data))
	for k := range data {
		keys = append(keys, k)
	}
	return keys
}
