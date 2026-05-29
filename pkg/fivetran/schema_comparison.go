package fivetran

import (
	"fmt"
	"strings"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

// NOTE: Schema validation scope
//
// This implementation validates SCHEMA and TABLE levels only. Column validation is intentionally
// not implemented to avoid performance issues with data sources that have thousands of tables.
//
// Fivetran's schema details API only returns schema and table configurations. Full column validation
// would require additional API calls per table, potentially causing thousands of requests during
// reconciliation loops.
//
// Current scope: schema change handling, schema/table enabled states, table sync modes
// Not validated: column existence, enabled state, hashed state, primary key state

// SchemaMismatch represents detailed information about schema configuration mismatches
type SchemaMismatch struct {
	HasMismatch          bool
	SchemaChangeHandling *string
	MissingSchemas       []string
	SchemaMismatches     map[string]*string  // schema name -> mismatch reason
	TableMismatches      map[string][]string // schema name -> list of table issues
}

// String returns a human-readable summary of the mismatches
func (sm *SchemaMismatch) String() string {
	if !sm.HasMismatch {
		return "No schema mismatches found"
	}

	var parts []string

	if sm.SchemaChangeHandling != nil {
		parts = append(parts, fmt.Sprintf("Schema Change Handling: %s", *sm.SchemaChangeHandling))
	}

	if len(sm.MissingSchemas) > 0 {
		parts = append(parts, fmt.Sprintf("Missing Schemas: %s", strings.Join(sm.MissingSchemas, ", ")))
	}

	if len(sm.SchemaMismatches) > 0 {
		for schema, reason := range sm.SchemaMismatches {
			parts = append(parts, fmt.Sprintf("Schema %s: %s", schema, *reason))
		}
	}

	if len(sm.TableMismatches) > 0 {
		for schema, issues := range sm.TableMismatches {
			parts = append(parts, fmt.Sprintf("Schema %s tables: %s", schema, strings.Join(issues, ", ")))
		}
	}

	return strings.Join(parts, "; ")
}

// CompareSchemaWithCR compares the Fivetran schema response with the CR schema configuration
// Returns true if the CR schema configuration is already applied in Fivetran, and detailed mismatch information
func CompareSchemaWithCR(fivetranSchema connections.ConnectionSchemaDetailsResponse, crSchema *operatorv1alpha1.ConnectorSchemaConfig) (bool, *SchemaMismatch) {
	mismatch := &SchemaMismatch{
		HasMismatch:      false,
		SchemaMismatches: make(map[string]*string),
		TableMismatches:  make(map[string][]string),
	}

	if crSchema == nil {
		return true, mismatch // No schema config in CR means nothing to compare
	}

	// Check schema change handling
	if crSchema.SchemaChangeHandling != "" &&
		fivetranSchema.Data.SchemaChangeHandling != crSchema.SchemaChangeHandling {
		mismatch.HasMismatch = true
		reason := fmt.Sprintf("expected %s, got %s", crSchema.SchemaChangeHandling, fivetranSchema.Data.SchemaChangeHandling)
		mismatch.SchemaChangeHandling = &reason
	}

	// Check each schema in CR
	for crSchemaName, crSchemaObj := range crSchema.Schemas {
		if crSchemaObj == nil {
			continue
		}
		fivetranSchemaObj, exists := fivetranSchema.Data.Schemas[crSchemaName]
		if !exists || fivetranSchemaObj == nil {
			if !crSchemaObj.Enabled {
				continue
			}
			mismatch.HasMismatch = true
			mismatch.MissingSchemas = append(mismatch.MissingSchemas, crSchemaName)
			continue
		}

		// Check schema enabled state
		if fivetranSchemaObj.Enabled != nil && *fivetranSchemaObj.Enabled != crSchemaObj.Enabled {
			mismatch.HasMismatch = true
			reason := fmt.Sprintf("enabled state mismatch: expected %v, got %v", crSchemaObj.Enabled, *fivetranSchemaObj.Enabled)
			mismatch.SchemaMismatches[crSchemaName] = &reason
		}

		// Check tables only if schema is enabled — disabled schemas don't sync, table state is irrelevant
		if crSchemaObj.Enabled && crSchemaObj.Tables != nil {
			tableMismatches := compareTablesWithFivetran(fivetranSchemaObj.Tables, crSchemaObj.Tables)
			if len(tableMismatches) > 0 {
				mismatch.HasMismatch = true
				mismatch.TableMismatches[crSchemaName] = tableMismatches
			}
		}
	}

	return !mismatch.HasMismatch, mismatch
}

// compareTablesWithFivetran compares CR table configuration with Fivetran table response
// Returns table mismatches
func compareTablesWithFivetran(fivetranTables map[string]*connections.ConnectionSchemaConfigTableResponse, crTables map[string]*operatorv1alpha1.TableObject) []string {
	var tableMismatches []string

	for crTableName, crTableObj := range crTables {
		if crTableObj == nil {
			continue
		}
		fivetranTableObj, exists := fivetranTables[crTableName]
		if !exists || fivetranTableObj == nil {
			if !crTableObj.Enabled {
				continue
			}
			tableMismatches = append(tableMismatches, fmt.Sprintf("table %s not found in source", crTableName))
			continue
		}

		var tableIssues []string

		// Check table enabled state
		if fivetranTableObj.Enabled != nil && *fivetranTableObj.Enabled != crTableObj.Enabled {
			tableIssues = append(tableIssues, fmt.Sprintf("enabled state mismatch: expected %v, got %v", crTableObj.Enabled, *fivetranTableObj.Enabled))
		}

		// Check sync mode if specified in CR
		if crTableObj.SyncMode != "" {
			if fivetranTableObj.SyncMode == nil {
				tableIssues = append(tableIssues, fmt.Sprintf("sync mode mismatch: expected %s, got nil", crTableObj.SyncMode))
			} else if *fivetranTableObj.SyncMode != crTableObj.SyncMode {
				tableIssues = append(tableIssues, fmt.Sprintf("sync mode mismatch: expected %s, got %s", crTableObj.SyncMode, *fivetranTableObj.SyncMode))
			}
		}

		// If there are table-level issues, add them
		if len(tableIssues) > 0 {
			tableMismatches = append(tableMismatches, fmt.Sprintf("table %s: %s", crTableName, strings.Join(tableIssues, ", ")))
		}
	}

	return tableMismatches
}
