package fivetran

import (
	"context"
	"fmt"

	fivetran "github.com/fivetran/go-fivetran"
	"github.com/fivetran/go-fivetran/connections"
)

type schemaServiceImpl struct {
	client *fivetran.Client
}

func newSchemaService(client *fivetran.Client) SchemaService {
	return &schemaServiceImpl{client: client}
}

// CreateSchema configures the schema for a Connection
func (s *schemaServiceImpl) CreateSchema(ctx context.Context, ConnectionID string, builder *SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error) {
	schemas, schemaChangeHandling, err := builder.Build()
	if err != nil {
		return connections.ConnectionSchemaDetailsResponse{}, fmt.Errorf("failed to build schema config: %w", err)
	}

	schemaService := s.client.NewConnectionSchemaCreateService()
	service := schemaService.ConnectionID(ConnectionID)

	if schemaChangeHandling != "" {
		service = service.SchemaChangeHandling(schemaChangeHandling)
	}

	for schemaName, schema := range schemas {
		service = service.Schema(schemaName, schema)
	}

	resp, err := service.Do(ctx)

	return resp, WrapFivetranError(resp, err)
}

// UpdateSchema updates the schema configuration for a Connection
func (s *schemaServiceImpl) UpdateSchema(ctx context.Context, ConnectionID string, builder *SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error) {
	schemas, schemaChangeHandling, err := builder.Build()
	if err != nil {
		return connections.ConnectionSchemaDetailsResponse{}, fmt.Errorf("failed to build schema config: %w", err)
	}

	schemaService := s.client.NewConnectionSchemaUpdateService()
	service := schemaService.ConnectionID(ConnectionID)

	if schemaChangeHandling != "" {
		service = service.SchemaChangeHandling(schemaChangeHandling)
	}

	for schemaName, schema := range schemas {
		service = service.Schema(schemaName, schema)
	}

	resp, err := service.Do(ctx)
	return resp, WrapFivetranError(resp, err)
}

// GetSchemaDetails retrieves schema configuration details for a Connection
func (s *schemaServiceImpl) GetSchemaDetails(ctx context.Context, ConnectionID string) (connections.ConnectionSchemaDetailsResponse, error) {
	schemaService := s.client.NewConnectionSchemaDetails()
	resp, err := schemaService.ConnectionID(ConnectionID).Do(ctx)
	return resp, WrapFivetranError(resp, err)
}

// GetColumnConfig retrieves column configuration for a specific table via the per-table endpoint.
// Returns accurate enabled_patch_settings that the schema-level endpoint may not include.
func (s *schemaServiceImpl) GetColumnConfig(ctx context.Context, connectionID, schema, table string) (map[string]*connections.ConnectionSchemaConfigColumnResponse, error) {
	resp, err := s.client.NewConnectionColumnConfigListService().
		ConnectionId(connectionID).
		Schema(schema).
		Table(table).
		Do(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to get column config for %s.%s: %w", schema, table, err)
	}
	return resp.Data.Columns, nil
}

// ReloadSchema reloads the schema configuration for a Connection
func (s *schemaServiceImpl) ReloadSchema(ctx context.Context, ConnectionID string, excludeMode string) (connections.ConnectionSchemaDetailsResponse, error) {
	reloadService := s.client.NewConnectionSchemaReload()
	resp, err := reloadService.
		ConnectionID(ConnectionID).
		ExcludeMode(excludeMode).
		Do(ctx)
	return resp, WrapFivetranError(resp, err)
}
