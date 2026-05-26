package fivetran

import (
	"strings"
	"testing"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

func strPtr(v string) *string {
	return &v
}

func createUpstreamResponse(schemaChangeHandling string, schemas map[string]*connections.ConnectionSchemaConfigSchemaResponse) connections.ConnectionSchemaDetailsResponse {
	return connections.ConnectionSchemaDetailsResponse{
		Data: struct {
			SchemaChangeHandling string                                                       `json:"schema_change_handling"`
			Schemas              map[string]*connections.ConnectionSchemaConfigSchemaResponse `json:"schemas"`
		}{
			SchemaChangeHandling: schemaChangeHandling,
			Schemas:              schemas,
		},
	}
}

func TestMergeSchemaWithPolicy_NilCR(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_ALL", nil)
	builder := MergeSchemaWithPolicy(upstream, nil)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "" {
		t.Errorf("expected empty handling, got %q", handling)
	}
	if len(schemas) != 0 {
		t.Errorf("expected 0 schemas, got %d", len(schemas))
	}
}

func TestMergeSchemaWithPolicy_AllowAll_OnlyCRItems(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"schema_a": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"table_1": {Enabled: boolPtr(true)},
				"table_2": {Enabled: boolPtr(true)},
				"table_3": {Enabled: boolPtr(true)},
			},
		},
		"schema_b": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"table_x": {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "ALLOW_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"schema_a": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"table_1": {Enabled: true, SyncMode: "HISTORY"},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "ALLOW_ALL" {
		t.Errorf("expected ALLOW_ALL, got %q", handling)
	}
	// ALLOW_ALL should only include CR items, not disable others
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema in payload, got %d", len(schemas))
	}
	if _, ok := schemas["schema_b"]; ok {
		t.Error("schema_b should NOT be in payload for ALLOW_ALL")
	}
}

func TestMergeSchemaWithPolicy_AllowColumns_DisablesUnspecified(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_COLUMNS", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"sales": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"invoices":   {Enabled: boolPtr(true)},
				"line_items": {Enabled: boolPtr(true)},
			},
		},
		"inventory": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"products":   {Enabled: boolPtr(true)},
				"warehouses": {Enabled: boolPtr(true)},
				"stock":      {Enabled: boolPtr(true)},
			},
		},
		"analytics": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"page_views": {Enabled: boolPtr(true)},
				"sessions":   {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "ALLOW_COLUMNS",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"inventory": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"products": {Enabled: true, SyncMode: "HISTORY"},
				},
			},
			"analytics": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"page_views": {Enabled: true, SyncMode: "HISTORY"},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "ALLOW_COLUMNS" {
		t.Errorf("expected ALLOW_COLUMNS, got %q", handling)
	}

	// All 3 upstream schemas should be in payload
	if len(schemas) != 3 {
		t.Fatalf("expected 3 schemas in payload, got %d", len(schemas))
	}

	// sales should be disabled (not in CR)
	s1Req := schemas["sales"].Request()
	if s1Req.Enabled == nil || *s1Req.Enabled != false {
		t.Error("sales should be disabled")
	}

	// inventory should be enabled with tables
	s2Req := schemas["inventory"].Request()
	if s2Req.Enabled == nil || *s2Req.Enabled != true {
		t.Error("inventory should be enabled")
	}
	if len(s2Req.Tables) != 3 {
		t.Fatalf("expected 3 tables in inventory, got %d", len(s2Req.Tables))
	}
	// products should be enabled
	if s2Req.Tables["products"] == nil || s2Req.Tables["products"].Enabled == nil || *s2Req.Tables["products"].Enabled != true {
		t.Error("products should be enabled")
	}
	if s2Req.Tables["products"].SyncMode == nil || *s2Req.Tables["products"].SyncMode != "HISTORY" {
		t.Error("products sync_mode should be HISTORY")
	}
	// warehouses should be disabled
	if s2Req.Tables["warehouses"] == nil || s2Req.Tables["warehouses"].Enabled == nil || *s2Req.Tables["warehouses"].Enabled != false {
		t.Error("warehouses in inventory should be disabled")
	}
	// stock should be disabled
	if s2Req.Tables["stock"] == nil || s2Req.Tables["stock"].Enabled == nil || *s2Req.Tables["stock"].Enabled != false {
		t.Error("stock in inventory should be disabled")
	}

	// analytics should be enabled
	s3Req := schemas["analytics"].Request()
	if s3Req.Enabled == nil || *s3Req.Enabled != true {
		t.Error("analytics should be enabled")
	}
	// page_views enabled, sessions disabled
	if s3Req.Tables["page_views"] == nil || *s3Req.Tables["page_views"].Enabled != true {
		t.Error("page_views should be enabled")
	}
	if s3Req.Tables["sessions"] == nil || *s3Req.Tables["sessions"].Enabled != false {
		t.Error("sessions in analytics should be disabled")
	}
}

func TestMergeSchemaWithPolicy_BlockAll_DisablesUnspecified(t *testing.T) {
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users":  {Enabled: boolPtr(true)},
				"orders": {Enabled: boolPtr(true)},
				"logs":   {Enabled: boolPtr(true)},
			},
		},
		"internal": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"metrics": {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {Enabled: true, SyncMode: "SOFT_DELETE"},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "BLOCK_ALL" {
		t.Errorf("expected BLOCK_ALL, got %q", handling)
	}
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	// internal should be disabled
	internalReq := schemas["internal"].Request()
	if internalReq.Enabled == nil || *internalReq.Enabled != false {
		t.Error("internal schema should be disabled")
	}

	// public should be enabled
	publicReq := schemas["public"].Request()
	if publicReq.Enabled == nil || *publicReq.Enabled != true {
		t.Error("public schema should be enabled")
	}
	// users enabled, orders and logs disabled
	if publicReq.Tables["users"] == nil || *publicReq.Tables["users"].Enabled != true {
		t.Error("users should be enabled")
	}
	if publicReq.Tables["users"].SyncMode == nil || *publicReq.Tables["users"].SyncMode != "SOFT_DELETE" {
		t.Error("users sync_mode should be SOFT_DELETE")
	}
	if publicReq.Tables["orders"] == nil || *publicReq.Tables["orders"].Enabled != false {
		t.Error("orders should be disabled")
	}
	if publicReq.Tables["logs"] == nil || *publicReq.Tables["logs"].Enabled != false {
		t.Error("logs should be disabled")
	}
}

func TestMergeSchemaWithPolicy_LockedTable_SkipsDisable(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_COLUMNS", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"mydb": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"wanted_table": {Enabled: boolPtr(true)},
				"locked_table": {
					Enabled: boolPtr(true),
					EnabledPatchSettings: struct {
						Allowed    *bool   `json:"allowed"`
						ReasonCode *string `json:"reason_code"`
						Reason     *string `json:"reason"`
					}{
						Allowed:    boolPtr(false),
						ReasonCode: strPtr("SYSTEM_TABLE"),
						Reason:     strPtr("System table cannot be disabled"),
					},
				},
				"normal_table": {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "ALLOW_COLUMNS",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"mydb": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"wanted_table": {Enabled: true},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mydbReq := schemas["mydb"].Request()

	// wanted_table should be enabled
	if mydbReq.Tables["wanted_table"] == nil || *mydbReq.Tables["wanted_table"].Enabled != true {
		t.Error("wanted_table should be enabled")
	}

	// locked_table should NOT be in the payload (skipped because patch not allowed)
	if _, exists := mydbReq.Tables["locked_table"]; exists {
		t.Error("locked_table should NOT be in payload since patch is not allowed")
	}

	// normal_table should be disabled
	if mydbReq.Tables["normal_table"] == nil || *mydbReq.Tables["normal_table"].Enabled != false {
		t.Error("normal_table should be disabled")
	}
}

func TestMergeSchemaWithPolicy_SchemaNoTablesInCR(t *testing.T) {
	tests := []struct {
		name          string
		schemaEnabled bool
		expectEnabled bool
	}{
		{"enabled schema with no tables block", true, true},
		{"disabled schema in CR", false, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			upstream := createUpstreamResponse("ALLOW_COLUMNS", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
				"schema_a": {
					Enabled: boolPtr(true),
					Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
						"table_1": {Enabled: boolPtr(true)},
						"table_2": {Enabled: boolPtr(true)},
					},
				},
			})

			crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
				SchemaChangeHandling: "ALLOW_COLUMNS",
				Schemas: map[string]*operatorv1alpha1.SchemaObject{
					"schema_a": {
						Enabled: tt.schemaEnabled,
					},
				},
			}

			builder := MergeSchemaWithPolicy(upstream, crSchema)
			schemas, _, err := builder.Build()
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			req := schemas["schema_a"].Request()
			if req.Enabled == nil || *req.Enabled != tt.expectEnabled {
				t.Errorf("schema_a enabled = %v, want %v", req.Enabled, tt.expectEnabled)
			}
			if tt.schemaEnabled {
				if len(req.Tables) != 2 {
					t.Errorf("expected 2 tables in payload (explicitly disabled), got %d", len(req.Tables))
				}
				for _, tbl := range req.Tables {
					if tbl.Enabled == nil || *tbl.Enabled != false {
						t.Errorf("expected all tables disabled, got enabled")
					}
				}
			} else {
				if len(req.Tables) != 0 {
					t.Errorf("expected 0 tables for disabled schema, got %d", len(req.Tables))
				}
			}
		})
	}
}

func TestMergeSchemaWithPolicy_EmptyPolicy_BehavesLikeAllowAll(t *testing.T) {
	upstream := createUpstreamResponse("", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"schema_a": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"table_1": {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"schema_a": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"table_1": {Enabled: true},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "" {
		t.Errorf("expected empty handling, got %q", handling)
	}
	// Should only have CR items (ALLOW_ALL-like behavior)
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(schemas))
	}
}

func TestMergeSchemaWithPolicy_CRSchemaNotInUpstream(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_COLUMNS", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"existing_schema": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"table_1": {Enabled: boolPtr(true)},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "ALLOW_COLUMNS",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"existing_schema": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"table_1": {Enabled: true},
				},
			},
			"new_schema": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"new_table": {Enabled: true, SyncMode: "HISTORY"},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Both schemas should be in the payload
	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	// new_schema should be included from CR even though not in upstream
	newReq := schemas["new_schema"].Request()
	if newReq.Enabled == nil || *newReq.Enabled != true {
		t.Error("new_schema should be enabled")
	}
	if newReq.Tables["new_table"] == nil || *newReq.Tables["new_table"].Enabled != true {
		t.Error("new_table should be enabled")
	}
}

func TestMergeColumns_BlockAll_DisablesUnlisted(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id":         {Enabled: boolPtr(true)},
						"name":       {Enabled: boolPtr(true)},
						"email":      {Enabled: boolPtr(true)},
						"password":   {Enabled: boolPtr(true)},
						"created_at": {Enabled: boolPtr(true)},
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
							"id":    {Enabled: true},
							"name":  {Enabled: true},
							"email": {Enabled: true},
						},
					},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	// CR-specified columns should have their state applied
	if tableReq.Columns["id"] == nil || *tableReq.Columns["id"].Enabled != true {
		t.Error("id should be enabled")
	}
	if tableReq.Columns["name"] == nil || *tableReq.Columns["name"].Enabled != true {
		t.Error("name should be enabled")
	}
	if tableReq.Columns["email"] == nil || *tableReq.Columns["email"].Enabled != true {
		t.Error("email should be enabled")
	}

	// Unlisted upstream columns should be disabled under BLOCK_ALL
	if tableReq.Columns["password"] == nil || *tableReq.Columns["password"].Enabled != false {
		t.Error("password should be disabled (not in CR, BLOCK_ALL)")
	}
	if tableReq.Columns["created_at"] == nil || *tableReq.Columns["created_at"].Enabled != false {
		t.Error("created_at should be disabled (not in CR, BLOCK_ALL)")
	}
}

func TestMergeColumns_AllowColumns_KeepsUnlistedEnabled(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("ALLOW_COLUMNS", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id":       {Enabled: boolPtr(true)},
						"name":     {Enabled: boolPtr(true)},
						"email":    {Enabled: boolPtr(true)},
						"password": {Enabled: boolPtr(true)},
						"ssn":      {Enabled: boolPtr(true)},
					},
				},
			},
		},
	})

	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "ALLOW_COLUMNS",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {
						Enabled: true,
						Columns: map[string]*operatorv1alpha1.ColumnObject{
							"password": {Enabled: false},
							"ssn":      {Enabled: false},
						},
					},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	// Explicitly disabled columns in CR
	if tableReq.Columns["password"] == nil || *tableReq.Columns["password"].Enabled != false {
		t.Error("password should be disabled (explicit in CR)")
	}
	if tableReq.Columns["ssn"] == nil || *tableReq.Columns["ssn"].Enabled != false {
		t.Error("ssn should be disabled (explicit in CR)")
	}

	// Unlisted columns should be enabled under ALLOW_COLUMNS
	if tableReq.Columns["id"] == nil || *tableReq.Columns["id"].Enabled != true {
		t.Error("id should be enabled (not in CR, ALLOW_COLUMNS)")
	}
	if tableReq.Columns["name"] == nil || *tableReq.Columns["name"].Enabled != true {
		t.Error("name should be enabled (not in CR, ALLOW_COLUMNS)")
	}
	if tableReq.Columns["email"] == nil || *tableReq.Columns["email"].Enabled != true {
		t.Error("email should be enabled (not in CR, ALLOW_COLUMNS)")
	}
}

func TestMergeColumns_NoColumnsInCR_SkipsColumnMerge(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id":    {Enabled: boolPtr(true)},
						"name":  {Enabled: boolPtr(true)},
						"email": {Enabled: boolPtr(true)},
					},
				},
			},
		},
	})

	// Table enabled but no columns block — should not touch columns
	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {Enabled: true},
				},
			},
		},
	}

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	// No columns should be in the payload when CR has no columns block
	if len(tableReq.Columns) != 0 {
		t.Errorf("expected 0 columns in payload (no columns in CR), got %d", len(tableReq.Columns))
	}
}

func TestMergeColumns_LockedColumn_SkipsDisable(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id": {
							Enabled: boolPtr(true),
							EnabledPatchSettings: struct {
								Allowed    *bool   `json:"allowed"`
								ReasonCode *string `json:"reason_code"`
								Reason     *string `json:"reason"`
							}{
								Allowed:    boolPtr(false),
								ReasonCode: strPtr("PRIMARY_KEY"),
								Reason:     strPtr("Primary key cannot be disabled"),
							},
						},
						"name":  {Enabled: boolPtr(true)},
						"email": {Enabled: boolPtr(true)},
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

	builder := MergeSchemaWithPolicy(upstream, crSchema)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	// name is in CR — enabled
	if tableReq.Columns["name"] == nil || *tableReq.Columns["name"].Enabled != true {
		t.Error("name should be enabled (in CR)")
	}

	// email is not in CR — disabled under BLOCK_ALL
	if tableReq.Columns["email"] == nil || *tableReq.Columns["email"].Enabled != false {
		t.Error("email should be disabled (not in CR, BLOCK_ALL)")
	}

	// id is locked — should NOT be in payload
	if _, exists := tableReq.Columns["id"]; exists {
		t.Error("id should NOT be in payload (locked column, patch not allowed)")
	}
}

func TestValidateLockedColumns_ReturnsError(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id": {
							Enabled: boolPtr(true),
							EnabledPatchSettings: struct {
								Allowed    *bool   `json:"allowed"`
								ReasonCode *string `json:"reason_code"`
								Reason     *string `json:"reason"`
							}{
								Allowed:    boolPtr(false),
								ReasonCode: strPtr("SYSTEM_COLUMN"),
								Reason:     strPtr("Primary key column"),
							},
						},
						"name":  {Enabled: boolPtr(true)},
						"email": {Enabled: boolPtr(true)},
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
							"id":   {Enabled: false}, // locked — should fail
							"name": {Enabled: true},
						},
					},
				},
			},
		},
	}

	err := ValidateLockedColumns(upstream, crSchema)
	if err == nil {
		t.Fatal("expected error for locked column, got nil")
	}
	if !strings.Contains(err.Error(), "public.users.id") {
		t.Errorf("expected error to mention public.users.id, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "SYSTEM_COLUMN") {
		t.Errorf("expected error to mention SYSTEM_COLUMN, got: %s", err.Error())
	}
}

func TestValidateLockedColumns_NoError_WhenColumnsEnabled(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", map[string]*connections.ConnectionSchemaConfigSchemaResponse{
		"public": {
			Enabled: boolPtr(true),
			Tables: map[string]*connections.ConnectionSchemaConfigTableResponse{
				"users": {
					Enabled: boolPtr(true),
					Columns: map[string]*connections.ConnectionSchemaConfigColumnResponse{
						"id": {
							Enabled: boolPtr(true),
							EnabledPatchSettings: struct {
								Allowed    *bool   `json:"allowed"`
								ReasonCode *string `json:"reason_code"`
								Reason     *string `json:"reason"`
							}{
								Allowed:    boolPtr(false),
								ReasonCode: strPtr("SYSTEM_COLUMN"),
								Reason:     strPtr("Primary key column"),
							},
						},
						"name": {Enabled: boolPtr(true)},
					},
				},
			},
		},
	})

	// CR enables the locked column (not trying to disable it) — should pass
	crSchema := &operatorv1alpha1.ConnectorSchemaConfig{
		SchemaChangeHandling: "BLOCK_ALL",
		Schemas: map[string]*operatorv1alpha1.SchemaObject{
			"public": {
				Enabled: true,
				Tables: map[string]*operatorv1alpha1.TableObject{
					"users": {
						Enabled: true,
						Columns: map[string]*operatorv1alpha1.ColumnObject{
							"id":   {Enabled: true},
							"name": {Enabled: true},
						},
					},
				},
			},
		},
	}

	err := ValidateLockedColumns(upstream, crSchema)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestValidateLockedColumns_NilCR(t *testing.T) {
	t.Parallel()
	upstream := createUpstreamResponse("BLOCK_ALL", nil)
	err := ValidateLockedColumns(upstream, nil)
	if err != nil {
		t.Fatalf("expected no error for nil CR, got: %v", err)
	}
}
