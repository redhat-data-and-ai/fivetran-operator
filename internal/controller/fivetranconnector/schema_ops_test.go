package fivetranconnector

import (
	"context"
	"fmt"
	"testing"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
	"github.com/redhat-data-and-ai/fivetran-operator/pkg/fivetran"
)

type mockSchemaService struct {
	columnConfigs map[string]map[string]*connections.ConnectionSchemaConfigColumnResponse
	callCount     int
}

func (m *mockSchemaService) CreateSchema(_ context.Context, _ string, _ *fivetran.SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error) {
	return connections.ConnectionSchemaDetailsResponse{}, nil
}
func (m *mockSchemaService) UpdateSchema(_ context.Context, _ string, _ *fivetran.SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error) {
	return connections.ConnectionSchemaDetailsResponse{}, nil
}
func (m *mockSchemaService) GetSchemaDetails(_ context.Context, _ string) (connections.ConnectionSchemaDetailsResponse, error) {
	return connections.ConnectionSchemaDetailsResponse{}, nil
}
func (m *mockSchemaService) GetColumnConfig(_ context.Context, _ string, schema, table string) (map[string]*connections.ConnectionSchemaConfigColumnResponse, error) {
	m.callCount++
	key := schema + "." + table
	if cols, ok := m.columnConfigs[key]; ok {
		return cols, nil
	}
	return nil, fmt.Errorf("table not found: %s", key)
}
func (m *mockSchemaService) ReloadSchema(_ context.Context, _ string, _ string) (connections.ConnectionSchemaDetailsResponse, error) {
	return connections.ConnectionSchemaDetailsResponse{}, nil
}

func boolP(v bool) *bool { return &v }

func TestValidateCRAgainstUpstream(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		crSchema   *operatorv1alpha1.ConnectorSchemaConfig
		upstream   connections.ConnectionSchemaDetailsResponse
		wantReload bool
	}{
		{
			name:       "nil CR returns false",
			crSchema:   nil,
			upstream:   connections.ConnectionSchemaDetailsResponse{},
			wantReload: false,
		},
		{
			name: "nil upstream schemas map triggers reload for enabled CR schema",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {Enabled: true},
				},
			},
			upstream:   connections.ConnectionSchemaDetailsResponse{},
			wantReload: true,
		},
		{
			name: "disabled CR schema not found upstream does not trigger reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {Enabled: false},
				},
			},
			upstream:   connections.ConnectionSchemaDetailsResponse{},
			wantReload: false,
		},
		{
			name: "nil schema object in CR is skipped",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": nil,
				},
			},
			upstream:   connections.ConnectionSchemaDetailsResponse{},
			wantReload: false,
		},
		{
			name: "enabled CR schema found upstream does not trigger reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {Enabled: true},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": {
					Enabled: boolP(true),
					Tables:  map[string]*connections.ConnectionSchemaConfigTableResponse{},
				},
			}),
			wantReload: false,
		},
		{
			name: "enabled CR schema not found upstream triggers reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"new_schema": {Enabled: true},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"existing_schema": {Enabled: boolP(true)},
			}),
			wantReload: true,
		},
		{
			name: "enabled CR table not found upstream triggers reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"new_table": {Enabled: true},
						},
					},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": {
					Enabled: boolP(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"existing_table": {Enabled: boolP(true)},
					},
				},
			}),
			wantReload: true,
		},
		{
			name: "disabled CR table not found upstream does not trigger reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"disabled_table": {Enabled: false},
						},
					},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": {
					Enabled: boolP(true),
					Tables:  map[string]*connections.ConnectionSchemaConfigTableResponse{},
				},
			}),
			wantReload: false,
		},
		{
			name: "nil upstream schema pointer triggers reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"my_schema": {Enabled: true},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"my_schema": nil,
			}),
			wantReload: true,
		},
		{
			name: "all CR items found upstream — no reload",
			crSchema: &operatorv1alpha1.ConnectorSchemaConfig{
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"schema_a": {
						Enabled: true,
						Tables: map[string]*operatorv1alpha1.TableObject{
							"table_1": {Enabled: true},
							"table_2": {Enabled: true},
						},
					},
					"schema_b": {Enabled: false},
				},
			},
			upstream: makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"schema_a": {
					Enabled: boolP(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"table_1": {Enabled: boolP(true)},
						"table_2": {Enabled: boolP(true)},
						"table_3": {Enabled: boolP(true)},
					},
				},
			}),
			wantReload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := validateCRAgainstUpstream(tt.crSchema, tt.upstream)
			if got != tt.wantReload {
				t.Errorf("validateCRAgainstUpstream() = %v, want %v", got, tt.wantReload)
			}
		})
	}
}

func makeUpstream(schemas map[string]*connections.ConnectionSchemaConfigSchemaResponse) connections.ConnectionSchemaDetailsResponse {
	return connections.ConnectionSchemaDetailsResponse{
		Data: struct {
			SchemaChangeHandling string                                                       `json:"schema_change_handling"`
			Schemas              map[string]*connections.ConnectionSchemaConfigSchemaResponse `json:"schemas"`
		}{
			Schemas: schemas,
		},
	}
}

func strP(v string) *string { return &v }

func TestPopulateColumnsForCR_FetchesWhenEmpty(t *testing.T) {
	t.Parallel()

	mock := &mockSchemaService{
		columnConfigs: map[string]map[string]*connections.ConnectionSchemaConfigColumnResponse{
			"public.users": {
				"id": {
					Enabled: boolP(true),
					EnabledPatchSettings: struct {
						Allowed    *bool   `json:"allowed"`
						ReasonCode *string `json:"reason_code"`
						Reason     *string `json:"reason"`
					}{
						Allowed:    boolP(false),
						ReasonCode: strP("SYSTEM_COLUMN"),
						Reason:     strP("Primary key"),
					},
				},
				"name":  {Enabled: boolP(true)},
				"email": {Enabled: boolP(true)},
			},
		},
	}

	r := &FivetranConnectorReconciler{
		FivetranClient: &fivetran.Client{Schemas: mock},
	}

	upstream := makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolP(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolP(true),
					Columns: nil, // empty — triggers fetch
				},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {
						Enabled: true,
						Columns: map[string]*operatorv1alpha1.ColumnObject{
							"name": {Enabled: true},
						},
					},
				},
			},
		},
	}

	fetched, err := r.populateColumnsForCR(context.Background(), "conn-123", crSchema, &upstream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fetched {
		t.Error("expected fetched=true when columns were missing")
	}

	if mock.callCount != 1 {
		t.Errorf("expected 1 API call, got %d", mock.callCount)
	}

	cols := upstream.Data.Schemas["public"].Tables["users"].Columns
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns populated, got %d", len(cols))
	}
	if cols["id"].EnabledPatchSettings.Allowed == nil || *cols["id"].EnabledPatchSettings.Allowed != false {
		t.Error("expected id column to have enabled_patch_settings.allowed=false")
	}
}

func TestPopulateColumnsForCR_SkipsWhenColumnsExist(t *testing.T) {
	t.Parallel()

	mock := &mockSchemaService{
		columnConfigs: map[string]map[string]*connections.ConnectionSchemaConfigColumnResponse{},
	}

	r := &FivetranConnectorReconciler{
		FivetranClient: &fivetran.Client{Schemas: mock},
	}

	upstream := makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolP(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolP(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"name": {Enabled: boolP(true)},
					},
				},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {
						Enabled: true,
						Columns: map[string]*operatorv1alpha1.ColumnObject{
							"name": {Enabled: true},
						},
					},
				},
			},
		},
	}

	fetched, err := r.populateColumnsForCR(context.Background(), "conn-123", crSchema, &upstream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetched {
		t.Error("expected fetched=false when columns already present")
	}

	// Should not call API since columns already exist and CR column is found
	if mock.callCount != 0 {
		t.Errorf("expected 0 API calls (columns already present), got %d", mock.callCount)
	}
}

func TestPopulateColumnsForCR_FetchesWhenCRColumnMissing(t *testing.T) {
	t.Parallel()

	mock := &mockSchemaService{
		columnConfigs: map[string]map[string]*connections.ConnectionSchemaConfigColumnResponse{
			"public.users": {
				"id":    {Enabled: boolP(true)},
				"name":  {Enabled: boolP(true)},
				"email": {Enabled: boolP(true)},
			},
		},
	}

	r := &FivetranConnectorReconciler{
		FivetranClient: &fivetran.Client{Schemas: mock},
	}

	upstream := makeUpstream(map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolP(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolP(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id":   {Enabled: boolP(true)},
						"name": {Enabled: boolP(true)},
						// "email" missing from schema-level response
					},
				},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {
						Enabled: true,
						Columns: map[string]*operatorv1alpha1.ColumnObject{
							"email": {Enabled: true}, // in CR but not in upstream
						},
					},
				},
			},
		},
	}

	fetched, err := r.populateColumnsForCR(context.Background(), "conn-123", crSchema, &upstream)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !fetched {
		t.Error("expected fetched=true when CR column missing from upstream")
	}

	if mock.callCount != 1 {
		t.Errorf("expected 1 API call (CR column missing from upstream), got %d", mock.callCount)
	}

	cols := upstream.Data.Schemas["public"].Tables["users"].Columns
	if len(cols) != 3 {
		t.Fatalf("expected 3 columns after populate, got %d", len(cols))
	}
}

func TestPopulateColumnsForCR_NilInputs(t *testing.T) {
	t.Parallel()

	r := &FivetranConnectorReconciler{}

	// nil CR
	_, err := r.populateColumnsForCR(context.Background(), "conn-123", nil, &connections.ConnectionSchemaDetailsResponse{})
	if err != nil {
		t.Fatalf("expected no error for nil CR, got: %v", err)
	}

	// nil upstream
	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {Enabled: true},
		},
	}
	_, err = r.populateColumnsForCR(context.Background(), "conn-123", crSchema, nil)
	if err != nil {
		t.Fatalf("expected no error for nil upstream, got: %v", err)
	}
}
