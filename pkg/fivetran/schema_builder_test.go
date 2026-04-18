package fivetran

import (
	"testing"
)

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

	// Verify table enabled state survived AddColumn
	if tableReq.Enabled == nil {
		t.Fatal("table Enabled is nil — AddColumn overwrote the table config created by AddTable")
	}
	if !*tableReq.Enabled {
		t.Errorf("table Enabled = false, want true")
	}

	// Verify table sync mode survived AddColumn
	if tableReq.SyncMode == nil {
		t.Fatal("table SyncMode is nil — AddColumn overwrote the table config created by AddTable")
	}
	if *tableReq.SyncMode != "SOFT_DELETE" {
		t.Errorf("table SyncMode = %q, want %q", *tableReq.SyncMode, "SOFT_DELETE")
	}

	// Verify the column was still added
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

	// Table settings should be preserved from AddTable
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

	// Both columns should be present
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
