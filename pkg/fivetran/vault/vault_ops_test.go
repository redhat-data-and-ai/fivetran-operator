package vault

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	vaultapi "github.com/hashicorp/vault/api"
	vaulthttp "github.com/hashicorp/vault/http"
	"github.com/hashicorp/vault/vault"
	vaultpkg "github.com/redhat-data-and-ai/fivetran-operator/pkg/vault"
	"k8s.io/apimachinery/pkg/runtime"
)

func setupTestVault(t *testing.T) (*vaultapi.Client, func()) {
	t.Helper()

	// Set VAULT_SKIP_VERIFY for testing
	if err := os.Setenv("VAULT_SKIP_VERIFY", "true"); err != nil {
		t.Fatalf("failed to set VAULT_SKIP_VERIFY: %v", err)
	}

	cluster := vault.NewTestCluster(t, &vault.CoreConfig{
		DevToken: "test-token",
		LogLevel: "error",
	}, &vault.TestClusterOptions{
		HandlerFunc: vaulthttp.Handler,
		NumCores:    1,
	})
	cluster.Start()

	core := cluster.Cores[0].Core
	vault.TestWaitActive(t, core)
	client := cluster.Cores[0].Client

	// Create "apps" KV v2 mount
	if err := client.Sys().Mount("apps", &vaultapi.MountInput{
		Type: "kv-v2",
	}); err != nil {
		t.Fatalf("failed to create apps mount: %v", err)
	}

	// Write test secrets
	if _, err := client.KVv2("apps").Put(context.Background(), "test-secret", map[string]any{
		"api_key":  "my-test-key",
		"username": "test-user",
		"password": "test-pass",
	}); err != nil {
		t.Fatalf("failed to write test secret: %v", err)
	}

	return client, func() {
		if err := os.Unsetenv("VAULT_SKIP_VERIFY"); err != nil {
			t.Logf("failed to unset VAULT_SKIP_VERIFY: %v", err)
		}
		cluster.Cleanup()
	}
}

func TestResolveSecrets(t *testing.T) {
	client, cleanup := setupTestVault(t)
	defer cleanup()

	tests := []struct {
		name        string
		input       map[string]any
		expected    map[string]any
		expectError bool
	}{
		{
			name:        "nil input",
			input:       nil,
			expected:    nil,
			expectError: false,
		},
		{
			name: "simple vault reference",
			input: map[string]any{
				"key": "vault:test-secret#api_key",
			},
			expected: map[string]any{
				"key": "my-test-key",
			},
			expectError: false,
		},
		{
			name: "array with vault references",
			input: map[string]any{
				"list": []any{
					"vault:test-secret#username",
					"vault:test-secret#password",
				},
			},
			expected: map[string]any{
				"list": []any{
					"test-user",
					"test-pass",
				},
			},
			expectError: false,
		},
		{
			name: "nested structure",
			input: map[string]any{
				"config": map[string]any{
					"auth": map[string]any{
						"user": "vault:test-secret#username",
						"pass": "vault:test-secret#password",
					},
				},
			},
			expected: map[string]any{
				"config": map[string]any{
					"auth": map[string]any{
						"user": "test-user",
						"pass": "test-pass",
					},
				},
			},
			expectError: false,
		},
		{
			name: "failure stops processing",
			input: map[string]any{
				"valid":   "vault:test-secret#api_key",
				"invalid": "vault:test-secret#nonexistent",
				"plain":   "no-vault-ref",
			},
			expected:    nil, // No result expected on error
			expectError: true,
		},
		{
			name: "invalid format fails fast",
			input: map[string]any{
				"bad": "vault:invalid-format",
			},
			expected:    nil, // No result expected on error
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create RawExtension
			var rawExt *runtime.RawExtension
			if tt.input != nil {
				inputJSON, err := json.Marshal(tt.input)
				if err != nil {
					t.Fatalf("failed to marshal test input: %v", err)
				}
				rawExt = &runtime.RawExtension{Raw: inputJSON}
			}

			// Call ResolveSecrets
			vaultClient := &vaultpkg.VaultClient{
				Client: client,
				Config: &vaultpkg.ClientConfig{
					MountPath: "apps", // Set the mount path for KV v2 operations
				},
			}
			err := ResolveSecrets(context.Background(), vaultClient, rawExt)

			// Check error expectations
			if tt.expectError && err == nil {
				t.Errorf("expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("expected no error but got: %v", err)
			}

			// Check result
			if tt.expected != nil && rawExt != nil {
				var result map[string]any
				if err := json.Unmarshal(rawExt.Raw, &result); err != nil {
					t.Fatalf("failed to unmarshal result: %v", err)
				}

				if !reflect.DeepEqual(result, tt.expected) {
					t.Errorf("result mismatch:\nexpected: %+v\ngot:      %+v", tt.expected, result)
				}
			}
		})
	}
}

func TestParseVaultReference(t *testing.T) {
	tests := []struct {
		input       string
		path, key   string
		expectError bool
	}{
		{"vault:apps/secret#mykey", "apps/secret", "mykey", false},
		{"vault:apps/secret", "", "", true}, // missing #
		{"vault:#key", "", "", true},        // empty path
		{"vault:path#", "", "", true},       // empty key
	}

	for _, tt := range tests {
		path, key, err := parseVaultReference(tt.input)
		if tt.expectError {
			if err == nil {
				t.Errorf("parseVaultReference(%q) expected error but got none", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("parseVaultReference(%q) unexpected error: %v", tt.input, err)
			}
			if path != tt.path || key != tt.key {
				t.Errorf("parseVaultReference(%q) = (%q, %q), expected (%q, %q)",
					tt.input, path, key, tt.path, tt.key)
			}
		}
	}
}

func TestVaultErrorRetryability(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		retryable bool
	}{
		{
			name:      "invalid reference error is not retryable",
			err:       NewInvalidReferenceError("config.password", "vault:invalid", "missing #"),
			retryable: false,
		},
		{
			name:      "key not found error is not retryable",
			err:       NewKeyNotFoundError("config.password", "missing_key", "apps/test", []string{"available_key"}),
			retryable: false,
		},
		{
			name:      "secret not found error is not retryable",
			err:       NewSecretNotFoundError("config.password", "vault:apps/test#key", "apps/test"),
			retryable: false,
		},
		{
			name:      "secret data nil error is not retryable",
			err:       NewSecretDataNilError("config.password", "vault:apps/test#key"),
			retryable: false,
		},
		{
			name:      "vault API error is retryable",
			err:       NewVaultAPIError("config.password", "vault:apps/test#key", errors.New("network timeout")),
			retryable: true,
		},
		{
			name:      "unknown error is retryable by default",
			err:       errors.New("some unknown error"),
			retryable: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if IsRetryableError(tt.err) != tt.retryable {
				t.Errorf("IsRetryableError() = %v, expected %v", IsRetryableError(tt.err), tt.retryable)
			}

			// Test VaultError interface if it's a VaultError
			var vErr *VaultError
			if errors.As(tt.err, &vErr) {
				if vErr.IsRetryable() != tt.retryable {
					t.Errorf("VaultError.IsRetryable() = %v, expected %v", vErr.IsRetryable(), tt.retryable)
				}
			}
		})
	}
}
