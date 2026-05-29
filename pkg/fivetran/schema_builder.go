package fivetran

import (
	"errors"
	"fmt"
	"strings"

	"github.com/fivetran/go-fivetran/connections"
	operatorv1alpha1 "github.com/redhat-data-and-ai/fivetran-operator/api/v1alpha1"
)

const (
	policyAllowAll = "ALLOW_ALL"
	policyBlockAll = "BLOCK_ALL"
)

// SchemaBuilder provides a fluent interface for building schema configurations
type SchemaBuilder struct {
	schemas              map[string]*connections.ConnectionSchemaConfigSchema
	tables               map[string]map[string]*connections.ConnectionSchemaConfigTable
	schemaChangeHandling string
	err                  error
}

// NewSchemaBuilder creates a new SchemaBuilder instance
func NewSchemaBuilder() *SchemaBuilder {
	return &SchemaBuilder{
		schemas: make(map[string]*connections.ConnectionSchemaConfigSchema),
		tables:  make(map[string]map[string]*connections.ConnectionSchemaConfigTable),
	}
}

// BuildSchemaConfig creates a SchemaBuilder payload from the CR spec.
// If upstream is nil, builds from CR only (greenfield / ALLOW_ALL).
// If upstream is provided, merges according to schema_change_handling policy.
func BuildSchemaConfig(
	cr *operatorv1alpha1.ConnectorSchemaConfig,
	upstream *connections.ConnectionSchemaDetailsResponse,
) *SchemaBuilder {
	builder := NewSchemaBuilder()

	if cr == nil {
		return builder
	}

	if cr.SchemaChangeHandling != "" {
		builder.WithSchemaChangeHandling(cr.SchemaChangeHandling)
	}

	policy := cr.SchemaChangeHandling
	if upstream == nil || policy == policyAllowAll || policy == "" {
		builder.fromCR(cr)
	} else {
		builder.mergeWithUpstream(cr, *upstream)
	}

	return builder
}

// WithSchemaChangeHandling sets the schema change handling policy
func (b *SchemaBuilder) WithSchemaChangeHandling(handling string) *SchemaBuilder {
	if b.err != nil {
		return b
	}
	b.schemaChangeHandling = handling
	return b
}

// AddSchema adds a schema configuration
func (b *SchemaBuilder) AddSchema(name string, enabled bool) *SchemaBuilder {
	if b.err != nil {
		return b
	}
	if name == "" {
		b.err = errors.New("schema name cannot be empty")
		return b
	}
	schema := &connections.ConnectionSchemaConfigSchema{}
	schema.Enabled(enabled)
	b.schemas[name] = schema
	return b
}

// AddTable adds a table configuration to a schema
func (b *SchemaBuilder) AddTable(schema, table string, enabled bool, syncMode string) *SchemaBuilder {
	if b.err != nil {
		return b
	}
	if schema == "" || table == "" {
		b.err = errors.New("schema and table names cannot be empty")
		return b
	}
	s, ok := b.schemas[schema]
	if !ok {
		b.err = fmt.Errorf("schema %q not found", schema)
		return b
	}

	tableConfig := &connections.ConnectionSchemaConfigTable{}
	tableConfig.Enabled(enabled)
	if syncMode != "" {
		tableConfig.SyncMode(syncMode)
	}
	s.Table(table, tableConfig)

	if b.tables[schema] == nil {
		b.tables[schema] = make(map[string]*connections.ConnectionSchemaConfigTable)
	}
	b.tables[schema][table] = tableConfig
	return b
}

// AddColumn adds a column configuration to a table
func (b *SchemaBuilder) AddColumn(schema, table, column string, enabled, hashed, isPrimaryKey bool) *SchemaBuilder {
	if b.err != nil {
		return b
	}
	if schema == "" || table == "" || column == "" {
		b.err = errors.New("schema, table, and column names cannot be empty")
		return b
	}

	tableConfig, ok := b.tables[schema][table]
	if !ok {
		b.err = fmt.Errorf("table %q not found in schema %q", table, schema)
		return b
	}

	columnConfig := &connections.ConnectionSchemaConfigColumn{}
	columnConfig.Enabled(enabled)
	if hashed {
		columnConfig.Hashed(hashed)
	}
	if isPrimaryKey {
		columnConfig.IsPrimaryKey(isPrimaryKey)
	}

	tableConfig.Column(column, columnConfig)

	return b
}

// Build returns the final schema configuration
func (b *SchemaBuilder) Build() (map[string]*connections.ConnectionSchemaConfigSchema, string, error) {
	if b.err != nil {
		return nil, "", b.err
	}
	return b.schemas, b.schemaChangeHandling, nil
}

// fromCR builds the payload from CR only (ALLOW_ALL / greenfield path)
func (b *SchemaBuilder) fromCR(cr *operatorv1alpha1.ConnectorSchemaConfig) {
	for schemaName, schemaObj := range cr.Schemas {
		if schemaObj == nil {
			continue
		}
		b.AddSchema(schemaName, schemaObj.Enabled)
		if !schemaObj.Enabled {
			continue
		}
		for tableName, tableObj := range schemaObj.Tables {
			if tableObj == nil {
				continue
			}
			b.AddTable(schemaName, tableName, tableObj.Enabled, tableObj.SyncMode)
			b.addColumnsFromCR(schemaName, tableName, tableObj.Columns)
		}
	}
}

// mergeWithUpstream merges upstream Fivetran state with CR configuration,
// enforcing schema_change_handling policy. Mirrors the Terraform provider's Override logic.
func (b *SchemaBuilder) mergeWithUpstream(cr *operatorv1alpha1.ConnectorSchemaConfig, upstream connections.ConnectionSchemaDetailsResponse) {
	policy := cr.SchemaChangeHandling
	upstreamSchemas := upstream.Data.Schemas

	// Process upstream schemas: disable those not in CR
	for schemaName, upstreamSchema := range upstreamSchemas {
		if upstreamSchema == nil {
			continue
		}

		crSchemaObj, inCR := cr.Schemas[schemaName]
		if !inCR || crSchemaObj == nil {
			b.AddSchema(schemaName, false)
			continue
		}

		b.AddSchema(schemaName, crSchemaObj.Enabled)
		if !crSchemaObj.Enabled {
			continue
		}

		b.mergeTables(schemaName, upstreamSchema.Tables, crSchemaObj.Tables, policy)
	}

	// Add CR schemas not yet in upstream
	for schemaName, crSchemaObj := range cr.Schemas {
		if crSchemaObj == nil {
			continue
		}
		if _, existsUpstream := upstreamSchemas[schemaName]; !existsUpstream {
			b.AddSchema(schemaName, crSchemaObj.Enabled)
			if !crSchemaObj.Enabled {
				continue
			}
			for tableName, tableObj := range crSchemaObj.Tables {
				if tableObj == nil {
					continue
				}
				b.AddTable(schemaName, tableName, tableObj.Enabled, tableObj.SyncMode)
				b.addColumnsFromCR(schemaName, tableName, tableObj.Columns)
			}
		}
	}
}

// mergeTables reconciles upstream tables with CR tables for a single schema
func (b *SchemaBuilder) mergeTables(
	schemaName string,
	upstreamTables map[string]*connections.ConnectionSchemaConfigTableResponse,
	crTables map[string]*operatorv1alpha1.TableObject,
	policy string,
) {
	// Disable upstream tables not listed in CR (respecting patch settings)
	for tableName, upstreamTable := range upstreamTables {
		crTableObj, inCR := crTables[tableName]

		if !inCR || crTableObj == nil {
			if isTablePatchAllowed(upstreamTable) {
				b.AddTable(schemaName, tableName, false, "")
			}
			continue
		}

		b.AddTable(schemaName, tableName, crTableObj.Enabled, crTableObj.SyncMode)
		b.mergeColumns(schemaName, tableName, upstreamTable, crTableObj.Columns, policy)
	}

	// Add CR tables not yet in upstream
	for tableName, crTableObj := range crTables {
		if crTableObj == nil {
			continue
		}
		if _, existsUpstream := upstreamTables[tableName]; !existsUpstream {
			b.AddTable(schemaName, tableName, crTableObj.Enabled, crTableObj.SyncMode)
			b.addColumnsFromCR(schemaName, tableName, crTableObj.Columns)
		}
	}
}

// mergeColumns reconciles upstream columns with CR columns based on policy.
//   - If the table has no columns in CR: don't touch columns (rely on schema_change_handling)
//   - If the table has columns in CR: apply CR state to listed columns, and set unlisted
//     upstream columns to the policy default (disabled for BLOCK_ALL, enabled otherwise)
func (b *SchemaBuilder) mergeColumns(
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
		b.AddColumn(schemaName, tableName, columnName, column.Enabled, column.Hashed, column.IsPrimaryKey)
	}

	// Merge upstream columns not in CR to policy default
	if upstreamTable == nil {
		return
	}
	for columnName, upstreamCol := range upstreamTable.Columns {
		if _, inCR := crColumns[columnName]; inCR {
			continue
		}
		if !isColumnPatchAllowed(upstreamCol) {
			continue
		}
		policyDefault := policy != policyBlockAll
		b.AddColumn(schemaName, tableName, columnName, policyDefault, false, false)
	}
}

// addColumnsFromCR adds all columns from the CR table to the builder
func (b *SchemaBuilder) addColumnsFromCR(schemaName, tableName string, columns map[string]*operatorv1alpha1.ColumnObject) {
	for columnName, column := range columns {
		if column == nil {
			continue
		}
		b.AddColumn(schemaName, tableName, columnName, column.Enabled, column.Hashed, column.IsPrimaryKey)
	}
}

func isTablePatchAllowed(table *connections.ConnectionSchemaConfigTableResponse) bool {
	if table == nil {
		return true
	}
	return table.EnabledPatchSettings.Allowed == nil || *table.EnabledPatchSettings.Allowed
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
