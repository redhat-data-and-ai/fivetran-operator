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

// --- SchemaBuilder unit tests ---

func TestAddColumn_OverwritesTableConfig(t *testing.T) {
	builder := NewSchemaBuilder()
	builder.AddSchema("myschema", true)
	builder.AddTable("myschema", "mytable", true, "SOFT_DELETE")
	builder.AddColumn("myschema", "mytable", "mycol", true, false, false)

	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() returned unexpected error: %v", err)
	}

	schema, ok := schemas["myschema"]
	if !ok {
		t.Fatal("schema 'myschema' not found in build output")
	}

	tableReq := schema.Request().Tables["mytable"]
	if tableReq == nil {
		t.Fatal("table 'mytable' not found in schema request")
	}

	if tableReq.Enabled == nil {
		t.Fatal("table Enabled is nil — AddColumn overwrote the table config created by AddTable")
	}
	if !*tableReq.Enabled {
		t.Errorf("table Enabled = false, want true")
	}

	if tableReq.SyncMode == nil {
		t.Fatal("table SyncMode is nil — AddColumn overwrote the table config created by AddTable")
	}
	if *tableReq.SyncMode != "SOFT_DELETE" {
		t.Errorf("table SyncMode = %q, want %q", *tableReq.SyncMode, "SOFT_DELETE")
	}

	if tableReq.Columns == nil || tableReq.Columns["mycol"] == nil {
		t.Fatal("column 'mycol' not found — column was not added")
	}
}

func TestAddMultipleColumns_OverwritesTableConfig(t *testing.T) {
	builder := NewSchemaBuilder()
	builder.AddSchema("myschema", true)
	builder.AddTable("myschema", "mytable", false, "HISTORY")
	builder.AddColumn("myschema", "mytable", "col_a", true, true, false)
	builder.AddColumn("myschema", "mytable", "col_b", false, false, true)

	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() returned unexpected error: %v", err)
	}

	tableReq := schemas["myschema"].Request().Tables["mytable"]
	if tableReq == nil {
		t.Fatal("table 'mytable' not found")
	}

	if tableReq.Enabled == nil {
		t.Fatal("table Enabled is nil after multiple AddColumn calls")
	}
	if *tableReq.Enabled != false {
		t.Errorf("table Enabled = %v, want false", *tableReq.Enabled)
	}
	if tableReq.SyncMode == nil {
		t.Fatal("table SyncMode is nil after multiple AddColumn calls")
	}
	if *tableReq.SyncMode != "HISTORY" {
		t.Errorf("table SyncMode = %q, want %q", *tableReq.SyncMode, "HISTORY")
	}

	if tableReq.Columns == nil {
		t.Fatal("columns map is nil")
	}
	if tableReq.Columns["col_a"] == nil {
		t.Error("column 'col_a' missing")
	}
	if tableReq.Columns["col_b"] == nil {
		t.Error("column 'col_b' missing")
	}
}

func TestAddTable_WithoutColumns_PreservesConfig(t *testing.T) {
	builder := NewSchemaBuilder()
	builder.AddSchema("myschema", true)
	builder.AddTable("myschema", "mytable", true, "LIVE")

	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("Build() returned unexpected error: %v", err)
	}

	tableReq := schemas["myschema"].Request().Tables["mytable"]
	if tableReq == nil {
		t.Fatal("table 'mytable' not found")
	}

	if tableReq.Enabled == nil || !*tableReq.Enabled {
		t.Errorf("table Enabled = %v, want true", tableReq.Enabled)
	}
	if tableReq.SyncMode == nil || *tableReq.SyncMode != "LIVE" {
		t.Errorf("table SyncMode = %v, want LIVE", tableReq.SyncMode)
	}
}

// --- BuildSchemaConfig tests ---

func TestBuildSchemaConfig_NilCR(t *testing.T) {
	upstream := createUpstreamResponse("ALLOW_ALL", nil)
	builder := BuildSchemaConfig(nil, &upstream)
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

func TestBuildSchemaConfig_NilUpstream_BuildsFromCR(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, nil)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "BLOCK_ALL" {
		t.Errorf("expected BLOCK_ALL, got %q", handling)
	}
	if len(schemas) != 1 {
		t.Fatalf("expected 1 schema, got %d", len(schemas))
	}
	publicReq := schemas["public"].Request()
	if publicReq.Enabled == nil || *publicReq.Enabled != true {
		t.Error("public should be enabled")
	}
	if publicReq.Tables["users"] == nil || *publicReq.Tables["users"].Enabled != true {
		t.Error("users should be enabled")
	}
}

func TestBuildSchemaConfig_AllowAll_OnlyCRItems(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "ALLOW_ALL" {
		t.Errorf("expected ALLOW_ALL, got %q", handling)
	}
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema in payload, got %d", len(schemas))
	}
	if _, ok := schemas["schema_b"]; ok {
		t.Error("schema_b should NOT be in payload for ALLOW_ALL")
	}
}

func TestBuildSchemaConfig_AllowColumns_DisablesUnspecified(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "ALLOW_COLUMNS" {
		t.Errorf("expected ALLOW_COLUMNS, got %q", handling)
	}

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
	if s2Req.Tables["products"] == nil || s2Req.Tables["products"].Enabled == nil || *s2Req.Tables["products"].Enabled != true {
		t.Error("products should be enabled")
	}
	if s2Req.Tables["products"].SyncMode == nil || *s2Req.Tables["products"].SyncMode != "HISTORY" {
		t.Error("products sync_mode should be HISTORY")
	}
	if s2Req.Tables["warehouses"] == nil || s2Req.Tables["warehouses"].Enabled == nil || *s2Req.Tables["warehouses"].Enabled != false {
		t.Error("warehouses in inventory should be disabled")
	}
	if s2Req.Tables["stock"] == nil || s2Req.Tables["stock"].Enabled == nil || *s2Req.Tables["stock"].Enabled != false {
		t.Error("stock in inventory should be disabled")
	}

	// analytics should be enabled
	s3Req := schemas["analytics"].Request()
	if s3Req.Enabled == nil || *s3Req.Enabled != true {
		t.Error("analytics should be enabled")
	}
	if s3Req.Tables["page_views"] == nil || *s3Req.Tables["page_views"].Enabled != true {
		t.Error("page_views should be enabled")
	}
	if s3Req.Tables["sessions"] == nil || *s3Req.Tables["sessions"].Enabled != false {
		t.Error("sessions in analytics should be disabled")
	}
}

func TestBuildSchemaConfig_BlockAll_DisablesUnspecified(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
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

	internalReq := schemas["internal"].Request()
	if internalReq.Enabled == nil || *internalReq.Enabled != false {
		t.Error("internal schema should be disabled")
	}

	publicReq := schemas["public"].Request()
	if publicReq.Enabled == nil || *publicReq.Enabled != true {
		t.Error("public schema should be enabled")
	}
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

func TestBuildSchemaConfig_LockedTable_SkipsDisable(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	mydbReq := schemas["mydb"].Request()

	if mydbReq.Tables["wanted_table"] == nil || *mydbReq.Tables["wanted_table"].Enabled != true {
		t.Error("wanted_table should be enabled")
	}

	// locked_table should NOT be in the payload (skipped because patch not allowed)
	if _, exists := mydbReq.Tables["locked_table"]; exists {
		t.Error("locked_table should NOT be in payload since patch is not allowed")
	}

	if mydbReq.Tables["normal_table"] == nil || *mydbReq.Tables["normal_table"].Enabled != false {
		t.Error("normal_table should be disabled")
	}
}

func TestBuildSchemaConfig_SchemaNoTablesInCR(t *testing.T) {
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

			builder := BuildSchemaConfig(crSchema, &upstream)
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

func TestBuildSchemaConfig_EmptyPolicy_BehavesLikeAllowAll(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, handling, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handling != "" {
		t.Errorf("expected empty handling, got %q", handling)
	}
	if len(schemas) != 1 {
		t.Errorf("expected 1 schema, got %d", len(schemas))
	}
}

func TestBuildSchemaConfig_CRSchemaNotInUpstream(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(schemas) != 2 {
		t.Fatalf("expected 2 schemas, got %d", len(schemas))
	}

	newReq := schemas["new_schema"].Request()
	if newReq.Enabled == nil || *newReq.Enabled != true {
		t.Error("new_schema should be enabled")
	}
	if newReq.Tables["new_table"] == nil || *newReq.Tables["new_table"].Enabled != true {
		t.Error("new_table should be enabled")
	}
}

// --- Column merge tests ---

func TestBuildSchemaConfig_Columns_BlockAll_DisablesUnlisted(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	if tableReq.Columns["id"] == nil || *tableReq.Columns["id"].Enabled != true {
		t.Error("id should be enabled")
	}
	if tableReq.Columns["name"] == nil || *tableReq.Columns["name"].Enabled != true {
		t.Error("name should be enabled")
	}
	if tableReq.Columns["email"] == nil || *tableReq.Columns["email"].Enabled != true {
		t.Error("email should be enabled")
	}

	if tableReq.Columns["password"] == nil || *tableReq.Columns["password"].Enabled != false {
		t.Error("password should be disabled (not in CR, BLOCK_ALL)")
	}
	if tableReq.Columns["created_at"] == nil || *tableReq.Columns["created_at"].Enabled != false {
		t.Error("created_at should be disabled (not in CR, BLOCK_ALL)")
	}
}

func TestBuildSchemaConfig_Columns_AllowColumns_KeepsUnlistedEnabled(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	if tableReq.Columns["password"] == nil || *tableReq.Columns["password"].Enabled != false {
		t.Error("password should be disabled (explicit in CR)")
	}
	if tableReq.Columns["ssn"] == nil || *tableReq.Columns["ssn"].Enabled != false {
		t.Error("ssn should be disabled (explicit in CR)")
	}

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

func TestBuildSchemaConfig_Columns_NoColumnsInCR_SkipsColumnMerge(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	if len(tableReq.Columns) != 0 {
		t.Errorf("expected 0 columns in payload (no columns in CR), got %d", len(tableReq.Columns))
	}
}

func TestBuildSchemaConfig_Columns_LockedColumn_SkipsDisable(t *testing.T) {
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

	builder := BuildSchemaConfig(crSchema, &upstream)
	schemas, _, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	tableReq := schemas["public"].Request().Tables["users"]

	if tableReq.Columns["name"] == nil || *tableReq.Columns["name"].Enabled != true {
		t.Error("name should be enabled (in CR)")
	}

	if tableReq.Columns["email"] == nil || *tableReq.Columns["email"].Enabled != false {
		t.Error("email should be disabled (not in CR, BLOCK_ALL)")
	}

	// id is locked — should NOT be in payload
	if _, exists := tableReq.Columns["id"]; exists {
		t.Error("id should NOT be in payload (locked column, patch not allowed)")
	}
}

// --- ValidateLockedColumns tests ---

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
							"id":   {Enabled: false},
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
