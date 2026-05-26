package fivetran

import (
	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

const policyAllowAll = "ALLOW_ALL"

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

	return buildWithMerge(builder, upstream, crSchema)
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

		mergeTablesForSchema(builder, schemaName, upstreamSchema.Tables, crSchemaObj.Tables)
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
		addColumnsFromCR(builder, schemaName, tableName, crTableObj.Columns)
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

func addColumnsFromCR(builder *SchemaBuilder, schemaName, tableName string, columns map[string]*operatorv1alpha1.ColumnObject) {
	for columnName, column := range columns {
		if column == nil {
			continue
		}
		builder.AddColumn(schemaName, tableName, columnName, column.Enabled, column.Hashed, column.IsPrimaryKey)
	}
}
