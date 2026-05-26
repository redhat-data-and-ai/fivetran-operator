package fivetranconnector

import (
	"testing"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

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
