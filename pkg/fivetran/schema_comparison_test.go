package fivetran

import (
	"testing"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

// Helper function to create test schema response
func createSchemaResponse(schemas map[string]*connections.ConnectionSchemaConfigSchemaResponse) connections.ConnectionSchemaDetailsResponse {
	return connections.ConnectionSchemaDetailsResponse{
		Data: struct {
			SchemaChangeHandling string                                                       `json:"schema_change_handling"`
			Schemas              map[string]*connections.ConnectionSchemaConfigSchemaResponse `json:"schemas"`
		}{
			SchemaChangeHandling: "ALLOW_ALL",
			Schemas:              schemas,
		},
	}
}

func TestCompareSchemaWithCR(t *testing.T) {
	tests := []struct {
		name           string
		fivetranSchema connections.ConnectionSchemaDetailsResponse
		crSchema       *operatorv1alpha1.ConnectorSchemaConfig
		expectMatch    bool
		expectError    string
	}{
		{
			name:           "nil CR schema should match",
			fivetranSchema: connections.ConnectionSchemaDetailsResponse{},
			crSchema:       nil,
			expectMatch:    true,
		},
		{
			name:           "empty schemas should match",
			fivetranSchema: createSchemaResponse(make(map[string]*connections.ConnectionSchemaConfigSchemaResponse)),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas:              make(map[string]*operatorv1alpha1.SchemaObject),
			},
			expectMatch: true,
		},
		{
			name:           "schema change handling mismatch",
			fivetranSchema: createSchemaResponse(make(map[string]*connections.ConnectionSchemaConfigSchemaResponse)),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "BLOCK_ALL",
				Schemas:              make(map[string]*operatorv1alpha1.SchemaObject),
			},
			expectMatch: false,
			expectError: "expected BLOCK_ALL, got ALLOW_ALL",
		},
		{
			name:           "missing schema in Fivetran",
			fivetranSchema: createSchemaResponse(make(map[string]*connections.ConnectionSchemaConfigSchemaResponse)),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
					},
				},
			},
			expectMatch: false,
			expectError: "test_schema",
		},
		{
			name: "schema enabled state mismatch",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(false),
					Tables:  make(map[string]*connections.ConnectionSchemaConfigTableResponse),
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
					},
				},
			},
			expectMatch: false,
			expectError: "expected true, got false",
		},
		{
			name: "missing table in Fivetran",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(true),
					Tables:  make(map[string]*connections.ConnectionSchemaConfigTableResponse),
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"test_table": {
								Enabled: true,
							},
						},
					},
				},
			},
			expectMatch: false,
			expectError: "not found in source",
		},
		{
			name: "table enabled state mismatch",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"test_table": {
							Enabled: boolPtr(false),
						},
					},
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"test_table": {
								Enabled: true,
							},
						},
					},
				},
			},
			expectMatch: false,
			expectError: "enabled state mismatch: expected true, got false",
		},
		{
			name: "table sync mode mismatch - nil in Fivetran",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"test_table": {
							Enabled:  boolPtr(true),
							SyncMode: nil,
						},
					},
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"test_table": {
								Enabled:  true,
								SyncMode: "SOFT_DELETE",
							},
						},
					},
				},
			},
			expectMatch: false,
			expectError: "expected SOFT_DELETE, got nil",
		},
		{
			name: "table sync mode mismatch - different values",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"test_table": {
							Enabled:  boolPtr(true),
							SyncMode: stringPtr("HISTORY"),
						},
					},
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"test_table": {
								Enabled:  true,
								SyncMode: "SOFT_DELETE",
							},
						},
					},
				},
			},
			expectMatch: false,
			expectError: "expected SOFT_DELETE, got HISTORY",
		},
		{
			name: "perfect match should pass",
			fivetranSchema: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"test_schema": {
					Enabled: boolPtr(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"test_table": {
							Enabled:  boolPtr(true),
							SyncMode: stringPtr("SOFT_DELETE"),
						},
					},
				},
			}),
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"test_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"test_table": {
								Enabled:  true,
								SyncMode: "SOFT_DELETE",
							},
						},
					},
				},
			},
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches, mismatch := CompareSchemaWithCR(tt.fivetranSchema, tt.crSchema)

			if matches != tt.expectMatch {
				t.Errorf("CompareSchemaWithCR() matches = %v, want %v", matches, tt.expectMatch)
			}

			if tt.expectMatch {
				if mismatch.HasMismatch {
					t.Errorf("Expected no mismatch, but got: %s", mismatch.String())
				}
			} else {
				if !mismatch.HasMismatch {
					t.Errorf("Expected mismatch, but got none")
				}
				if tt.expectError != "" {
					mismatchStr := mismatch.String()
					if !contains(mismatchStr, tt.expectError) {
						t.Errorf("Expected error to contain '%s', got: %s", tt.expectError, mismatchStr)
					}
				}
			}
		})
	}
}

func TestCompareSchemaWithCR_DisabledNotFoundIsNotMismatch(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		crSchema    *operatorv1alpha1.ConnectorSchemaConfig
		upstream    connections.ConnectionSchemaDetailsResponse
		expectMatch bool
	}{
		{
			name: "disabled schema not found upstream is not a mismatch",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"missing_schema": {Enabled: false},
				},
			},
			upstream:    createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{}),
			expectMatch: true,
		},
		{
			name: "enabled schema not found upstream is a mismatch",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"missing_schema": {Enabled: true},
				},
			},
			upstream:    createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{}),
			expectMatch: false,
		},
		{
			name: "disabled table not found upstream is not a mismatch",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"missing_table": {Enabled: false},
						},
					},
				},
			},
			upstream: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": {
					Enabled: boolPtr(true),
					Tables:  map[string]*connections.ConnectionSchemaConfigTableResponse{},
				},
			}),
			expectMatch: true,
		},
		{
			name: "enabled table not found upstream is a mismatch",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"missing_table": {Enabled: true},
						},
					},
				},
			},
			upstream: createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": {
					Enabled: boolPtr(true),
					Tables:  map[string]*connections.ConnectionSchemaConfigTableResponse{},
				},
			}),
			expectMatch: false,
		},
		{
			name: "nil CR schema object is skipped",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_ALL",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"nil_schema": nil,
				},
			},
			upstream:    createSchemaResponse(map[string]*connections.ConnectionSchemaConfigSchemaResponse{}),
			expectMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			matches, mismatch := CompareSchemaWithCR(tt.upstream, tt.crSchema)
			if matches != tt.expectMatch {
				t.Errorf("CompareSchemaWithCR() = %v, want %v; details: %s", matches, tt.expectMatch, mismatch.String())
			}
		})
	}
}

// Helper functions
func boolPtr(b bool) *bool {
	return &b
}

func stringPtr(s string) *string {
	return &s
}

func contains(str, substr string) bool {
	return len(str) >= len(substr) &&
		(len(substr) == 0 ||
			str[:len(substr)] == substr ||
			str[len(str)-len(substr):] == substr ||
			containsSubstring(str, substr))
}

func containsSubstring(str, substr string) bool {
	for i := 0; i <= len(str)-len(substr); i++ {
		if str[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
