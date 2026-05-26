package fivetran

import (
	"context"

	"github.com/fivetran/go-fivetran/common"
	"github.com/fivetran/go-fivetran/connections"
)

// ConnectionService defines the interface for Connection operations
type ConnectorService interface {
	CreateConnection(ctx context.Context, Connection *Connector) (connections.DetailsWithCustomConfigResponse, error)
	GetConnection(ctx context.Context, ConnectionID string) (connections.DetailsWithCustomConfigNoTestsResponse, error)
	UpdateConnection(ctx context.Context, ConnectionID string, Connection *Connector) (connections.DetailsWithCustomConfigResponse, error)
	DeleteConnection(ctx context.Context, ConnectionID string) (common.CommonResponse, error)
	RunSetupTests(ctx context.Context, ConnectionID string, trustCertificates, trustFingerprints *bool) (connections.DetailsWithConfigResponse, error)
}

// SchemaService defines the interface for schema operations
type SchemaService interface {
	CreateSchema(ctx context.Context, connectorID string, builder *SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error)
	UpdateSchema(ctx context.Context, ConnectionID string, builder *SchemaBuilder) (connections.ConnectionSchemaDetailsResponse, error)
	GetSchemaDetails(ctx context.Context, ConnectionID string) (connections.ConnectionSchemaDetailsResponse, error)
	GetColumnConfig(ctx context.Context, connectionID, schema, table string) (map[string]*connections.ConnectionSchemaConfigColumnResponse, error)
	ReloadSchema(ctx context.Context, ConnectionID string, excludeMode string) (connections.ConnectionSchemaDetailsResponse, error)
}
