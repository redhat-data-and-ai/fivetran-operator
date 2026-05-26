package fivetran

import (
	"fmt"
	"strings"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

const (
	policyAllowAll = "ALLOW_ALL"
	policyBlockAll = "BLOCK_ALL"
)

// MergeSchemaWithPolicy applies the schema_change_handling policy by merging upstream state
// with the CR config. Mirrors the Terraform provider's Override logic.
func MergeSchemaWithPolicy(
	upstream connections.ConnectionSchemaDetailsResponse,
	crSchema *operatorv1alpha1.ConnectorSchemaConfig,
) *SchemaBuilder {
	builder := NewSchemaBuilder()

	if crSchema == nil {
		return builder
	}

	policy := crSchema.SchemaChangeHandling
	if policy != "" {
		builder.WithSchemaChangeHandling(policy)
	}

	if policy == policyAllowAll || policy == "" {
		return buildFromCROnly(builder, crSchema)
	}

	return buildWithMerge(builder, upstream, crSchema, policy)
}

func buildFromCROnly(builder *SchemaBuilder, crSchema *operatorv1alpha1.ConnectorSchemaConfig) *SchemaBuilder {
	for schemaName, schemaObj := range crSchema.Schemas {
		if schemaObj == nil {
			continue
		}
		builder.AddSchema(schemaName, schemaObj.Enabled)
		if !schemaObj.Enabled {
			continue
		}
		for tableName, tableObj := range schemaObj.Tables {
			if tableObj == nil {
				continue
			}
			builder.AddTable(schemaName, tableName, tableObj.Enabled, tableObj.SyncMode)
			addColumnsFromCR(builder, schemaName, tableName, tableObj.Columns)
		}
	}
	return builder
}

func buildWithMerge(
	builder *SchemaBuilder,
	upstream connections.ConnectionSchemaDetailsResponse,
	crSchema *operatorv1alpha1.ConnectorSchemaConfig,
	policy string,
) *SchemaBuilder {
	upstreamSchemas := upstream.Data.Schemas

	for schemaName, upstreamSchema := range upstreamSchemas {
		if upstreamSchema == nil {
			continue
		}

		crSchemaObj, inCR := crSchema.Schemas[schemaName]

		if !inCR || crSchemaObj == nil {
			builder.AddSchema(schemaName, false)
			continue
		}

		builder.AddSchema(schemaName, crSchemaObj.Enabled)

		if !crSchemaObj.Enabled {
			continue
		}

		mergeTablesForSchema(builder, schemaName, upstreamSchema.Tables, crSchemaObj.Tables, policy)
	}

	for schemaName, crSchemaObj := range crSchema.Schemas {
		if crSchemaObj == nil {
			continue
		}
		if _, existsUpstream := upstreamSchemas[schemaName]; !existsUpstream {
			builder.AddSchema(schemaName, crSchemaObj.Enabled)
			if !crSchemaObj.Enabled {
				continue
			}
			for tableName, tableObj := range crSchemaObj.Tables {
				if tableObj == nil {
					continue
				}
				builder.AddTable(schemaName, tableName, tableObj.Enabled, tableObj.SyncMode)
				addColumnsFromCR(builder, schemaName, tableName, tableObj.Columns)
			}
		}
	}

	return builder
}

func mergeTablesForSchema(
	builder *SchemaBuilder,
	schemaName string,
	upstreamTables map[string]*connections.ConnectionSchemaConfigTableResponse,
	crTables map[string]*operatorv1alpha1.TableObject,
	policy string,
) {
	for tableName, upstreamTable := range upstreamTables {
		crTableObj, inCR := crTables[tableName]

		if !inCR || crTableObj == nil {
			if isTablePatchAllowed(upstreamTable) {
				builder.AddTable(schemaName, tableName, false, "")
			}
			continue
		}

		builder.AddTable(schemaName, tableName, crTableObj.Enabled, crTableObj.SyncMode)
		mergeColumnsForTable(builder, schemaName, tableName, upstreamTable, crTableObj.Columns, policy)
	}

	for tableName, crTableObj := range crTables {
		if crTableObj == nil {
			continue
		}
		if _, existsUpstream := upstreamTables[tableName]; !existsUpstream {
			builder.AddTable(schemaName, tableName, crTableObj.Enabled, crTableObj.SyncMode)
			addColumnsFromCR(builder, schemaName, tableName, crTableObj.Columns)
		}
	}
}

func isTablePatchAllowed(table *connections.ConnectionSchemaConfigTableResponse) bool {
	if table == nil {
		return true
	}
	return table.EnabledPatchSettings.Allowed == nil || *table.EnabledPatchSettings.Allowed
}

// mergeColumnsForTable reconciles upstream columns with CR columns based on policy.
//   - If the table has no columns in CR: don't touch columns (rely on schema_change_handling)
//   - If the table has columns in CR: apply CR state to listed columns, and set unlisted
//     upstream columns to the policy default (disabled for BLOCK_ALL, enabled otherwise)
func mergeColumnsForTable(
	builder *SchemaBuilder,
	schemaName, tableName string,
	upstreamTable *connections.ConnectionSchemaConfigTableResponse,
	crColumns map[string]*operatorv1alpha1.ColumnObject,
	policy string,
) {
	if len(crColumns) == 0 {
		return
	}

	// Apply CR-specified columns
	for columnName, column := range crColumns {
		if column == nil {
			continue
		}
		builder.AddColumn(schemaName, tableName, columnName, column.Enabled, column.Hashed, column.IsPrimaryKey)
	}

	// Merge upstream columns not in CR to policy default
	if upstreamTable == nil {
		return
	}
	for columnName, upstreamCol := range upstreamTable.Columns {
		if _, inCR := crColumns[columnName]; inCR {
			continue
		}
		policyDefault := policy != policyBlockAll
		if !isColumnPatchAllowed(upstreamCol) {
			continue
		}
		builder.AddColumn(schemaName, tableName, columnName, policyDefault, false, false)
	}
}

func addColumnsFromCR(builder *SchemaBuilder, schemaName, tableName string, columns map[string]*operatorv1alpha1.ColumnObject) {
	for columnName, column := range columns {
		if column == nil {
			continue
		}
		builder.AddColumn(schemaName, tableName, columnName, column.Enabled, column.Hashed, column.IsPrimaryKey)
	}
}

func isColumnPatchAllowed(col *connections.ConnectionSchemaConfigColumnResponse) bool {
	if col == nil {
		return true
	}
	return col.EnabledPatchSettings.Allowed == nil || *col.EnabledPatchSettings.Allowed
}

// NeedsColumnSecondPass returns true if the CR has tables with columns blocks
// and the policy requires column management (BLOCK_ALL or ALLOW_COLUMNS).
// In this case, a second pass is needed because the first GetSchemaDetails after
// reload may not include columns — they only appear after tables are enabled.
func NeedsColumnSecondPass(crSchema *operatorv1alpha1.ConnectorSchemaConfig) bool {
	if crSchema == nil {
		return false
	}
	policy := crSchema.SchemaChangeHandling
	if policy == policyAllowAll || policy == "" {
		return false
	}
	for _, schemaObj := range crSchema.Schemas {
		if schemaObj == nil || !schemaObj.Enabled {
			continue
		}
		for _, tableObj := range schemaObj.Tables {
			if tableObj == nil {
				continue
			}
			if len(tableObj.Columns) > 0 {
				return true
			}
		}
	}
	return false
}

// ValidateLockedColumns checks if the CR attempts to disable any locked columns
// (e.g., primary keys or system columns). Returns an error listing all violations.
func ValidateLockedColumns(
	upstream connections.ConnectionSchemaDetailsResponse,
	crSchema *operatorv1alpha1.ConnectorSchemaConfig,
) error {
	if crSchema == nil {
		return nil
	}

	var violations []string

	for schemaName, schemaObj := range crSchema.Schemas {
		if schemaObj == nil || !schemaObj.Enabled {
			continue
		}
		upstreamSchema := upstream.Data.Schemas[schemaName]
		if upstreamSchema == nil {
			continue
		}
		for tableName, tableObj := range schemaObj.Tables {
			if tableObj == nil {
				continue
			}
			upstreamTable := upstreamSchema.Tables[tableName]
			if upstreamTable == nil {
				continue
			}
			for colName, colObj := range tableObj.Columns {
				if colObj == nil || colObj.Enabled {
					continue
				}
				upstreamCol := upstreamTable.Columns[colName]
				if upstreamCol == nil {
					continue
				}
				if !isColumnPatchAllowed(upstreamCol) {
					reason := "locked"
					if upstreamCol.EnabledPatchSettings.ReasonCode != nil {
						reason = *upstreamCol.EnabledPatchSettings.ReasonCode
					}
					if upstreamCol.EnabledPatchSettings.Reason != nil {
						reason += ": " + *upstreamCol.EnabledPatchSettings.Reason
					}
					violations = append(violations, fmt.Sprintf("%s.%s.%s (%s)", schemaName, tableName, colName, reason))
				}
			}
		}
	}

	if len(violations) > 0 {
		return fmt.Errorf("%s", strings.Join(violations, ", "))
	}
	return nil
}
