# FivetranConnector CRD Documentation

## Overview

The `FivetranConnector` is a Kubernetes Custom Resource Definition (CRD) that allows you to manage Fivetran connectors declaratively within your Kubernetes cluster.

**API Version:** `operator.dataverse.redhat.com/v1alpha1`  
**Kind:** `FivetranConnector`

## Specification

The `FivetranConnectorSpec` defines the desired state of a FivetranConnector and consists of two main components:

- `connector` (required): Core connector configuration
- `connectorSchemas` (optional): Schema-level configuration for data synchronization

---

## Fields Reference

### `spec.connector` (Object, Required)

Core connector configuration that defines how the connector behaves and connects to the data source.

#### Identity Fields (Immutable)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `group_id` | string | **Yes** | The unique identifier for the group within the Fivetran system. **This field is immutable after creation.** |
| `service` | string | **Yes** | The connector name/type within the Fivetran system (e.g., `postgres`, `mysql`, `s3`). **This field is immutable after creation.** |
| `config` | Object | **Yes** | The connector configuration parameters. This is a flexible object that varies by connector type. **Supports Vault secret references** using `vault:path#key` format for sensitive values. **Refer to the [Fivetran API documentation](https://fivetran.com/docs/rest-api/api-reference/connections/create-connection) for service-specific configuration options.** See [Configuration Examples](#configuration-examples) below. |
| `auth` | Object | No | The connector authorization parameters. Structure varies by connector type. **Supports Vault secret references** using `vault:path#key` format for sensitive values. **Refer to the [Fivetran API documentation](https://fivetran.com/docs/rest-api/api-reference/connections/create-connection) for service-specific authentication options.** |
| `schedule_type` | string | No | `auto`, `manual` | The connection schedule configuration type |
| `sync_frequency` | integer | No | `1`, `5`, `15`, `30`, `60`, `120`, `180`, `360`, `480`, `720`, `1440` | The connection sync frequency in minutes |
| `daily_sync_time` | string | No | Format: `HH:00` (00:00-23:00) | The sync start time in 24-hour format (e.g., "14:00", "21:00"). **Can only be specified when `sync_frequency` is `1440` (daily).** |
| `paused` | boolean | **Yes** | `false` | Specifies whether the connection is paused |
| `run_setup_tests` | boolean | No | `true` | Specifies whether the setup tests should be run automatically |
| `pause_after_trial` | boolean | No | `false` | Specifies whether the connection should be paused after the free trial period has ended |
| `trust_certificates` | boolean | No | `true` | Specifies whether to trust certificates automatically |
| `trust_fingerprints` | boolean | No | `true` | Specifies whether to trust SSH fingerprints automatically |
| `data_delay_sensitivity` | string | No | `LOW`, `NORMAL`, `HIGH`, `CUSTOM`, `SYNC_FREQUENCY` | The level of data delay notification threshold |
| `data_delay_threshold` | integer | No | Any positive integer | Custom sync delay notification threshold in minutes. Used when `data_delay_sensitivity` is `CUSTOM` |
| `networking_method` | string | No | `Directly`, `PrivateLink`, `SshTunnel`, `ProxyAgent` | How the connector connects to the data source |
| `proxy_agent_id` | string | No | - | The unique identifier for the proxy agent. Used when `networking_method` is `ProxyAgent` |
| `private_link_id` | string | No | - | The unique identifier for the self-served private link. Used when `networking_method` is `PrivateLink` |
| `hybrid_deployment_agent_id` | string | No | - | The unique identifier for the hybrid deployment agent |

---

### `spec.connectorSchemas` (Object, Optional)

Schema-level configuration that defines how data schemas, tables, and columns are synchronized.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `schema_change_handling` | string | No | Controls how new schemas, tables, and columns are handled |
| `schemas` | map[string]Object | No | Map of schema names to schema configuration objects |

#### `schema_change_handling` Valid Values

| Value | Description |
|-------|-------------|
| `ALLOW_ALL` | Includes all new schemas, tables, and columns automatically |
| `ALLOW_COLUMNS` | Excludes new schemas and tables but includes new columns |
| `BLOCK_ALL` | Excludes all new schemas, tables, and columns |

#### `schemas` Structure

Each key in the `schemas` map represents a schema name, with the value being a schema configuration object.

**Schema Object Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | boolean | **Yes** | Whether this schema should be synchronized |
| `tables` | map[string]Object | No | Map of table names to table configuration objects |

**Table Object Fields:**

| Field | Type | Required | Valid Values | Description |
|-------|------|----------|--------------|-------------|
| `enabled` | boolean | **Yes** | - | Whether this table should be synchronized |
| `sync_mode` | string | No | `SOFT_DELETE`, `HISTORY`, `LIVE` | The sync mode for the table |
| `columns` | map[string]Object | No | - | Map of column names to column configuration objects |

**Table `sync_mode` Values:**

| Value | Description |
|-------|-------------|
| `SOFT_DELETE` | Preserves deleted records with deletion markers |
| `HISTORY` | Maintains full change history for records |
| `LIVE` | Provides real-time data without historical tracking |

**Column Object Fields:**

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `enabled` | boolean | **Yes** | Whether this column should be synchronized |
| `hashed` | boolean | **Yes** | Whether the column data should be hashed |
| `is_primary_key` | boolean | **Yes** | Whether this column is part of the primary key |
| `masking_algorithm` | string | No | The masking algorithm to apply to column data |

**Column `masking_algorithm` Values:**

| Value | Description |
|-------|-------------|
| `PLAINTEXT` | Stores data as-is without any masking |
| `HASHED` | Applies hashing to the column data |
| `ENCRYPTED` | Applies encryption to the column data |

---

## Vault Secret References

For sensitive configuration data like passwords, API keys, and tokens, the FivetranConnector supports **Vault secret references** instead of storing secrets directly in the YAML configuration.

### Vault Reference Format

Use the following format for any string value in `config` or `auth` fields:

```
vault:path#key
```

Where:
- `path`: The Vault KV path to the secret
- `key`: The specific key within the secret

### How It Works

1. **Automatic Resolution**: The operator automatically detects string values starting with `vault:`
2. **Recursive Processing**: Vault references work anywhere within `config` and `auth` objects (nested objects, arrays, etc.)
3. **Caching**: Multiple references to the same Vault path are cached to minimize API calls
4. **Error Handling**: Clear error messages for invalid references, missing secrets, or missing keys

### Examples

```yaml
config:
  # Direct vault reference for database password
  password: "vault:somepath#key"
  
  # Vault reference for host information
  host: "vault:database/endpoints#primary_host"
  
  # Multiple vault references
  user: "vault:database/somepath#username"
  api_key: "vault:external-apis/tokens#fivetran_key"
  
  # Works in nested objects
  ssl_config:
    cert_path: "vault:certificates/ssl#cert_file"
    key_path: "vault:certificates/ssl#key_file"
    
  # Works in arrays
  allowed_ips: 
    - "vault:network/access#ip1"
    - "vault:network/access#ip2"
```

---

## Configuration Examples

> **⚠️ Important**: The `config` and `auth` fields vary significantly based on the connector service type. Each service (S3, MySQL, PostgreSQL, Google Sheets, etc.) has different required and optional parameters.
> 
> **📖 Always refer to the [Fivetran API documentation](https://fivetran.com/docs/rest-api/api-reference/connections/create-connection)** for the exact configuration parameters required for your specific service.
>
> The examples below show different connector types. Use them as templates but verify the exact field requirements in the official Fivetran API docs.

### S3 Connector Example

```yaml
apiVersion: operator.dataverse.redhat.com/v1alpha1
kind: FivetranConnector
metadata:
  name: s3-connector
spec:
  connector:
    group_id: "my_group_id"
    service: "s3"
    paused: false
    schedule_type: "auto"
    sync_frequency: 1440
    daily_sync_time: "14:00"
    trust_certificates: true
    trust_fingerprints: true
    run_setup_tests: true
    networking_method: "Directly"
    config:
      access_key_id: "vault:path#access_key_id_value"
      access_key_secret: "vault:path#access_key_secret_value"
      bucket: "bucket_name"
      prefix: "folder_path"
      auth_type: "ACCESS_KEY"
      file_type: "csv"
      schema: "schema_name"
      delimiter: ","
      quote_char: "\""
      compression: "gzip"
      on_error: "skip"
```

### Google Sheets Connector Example

```yaml
apiVersion: operator.dataverse.redhat.com/v1alpha1
kind: FivetranConnector
metadata:
  name: google-sheets-connector
spec:
  connector:
    group_id: "my_group_id"
    service: "google_sheets"
    paused: false
    schedule_type: "auto"
    sync_frequency: 60
    trust_certificates: true
    trust_fingerprints: true
    run_setup_tests: true
    config:
      auth_type: "OAuth"
      sheet_id: "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
      named_range: "range"
      schema: "schema_name"
      table: "table_name"
    auth:
      refresh_token: "vault:google/oauth#refresh_token"
      client_access:
        client_secret: "vault:google/oauth#client_secret"
        client_id: "vault:google/oauth#client_id"
```

### Fivetran Log Connector Example

This example shows the `fivetran_log` service which provides access to Fivetran's own operational logs:

```yaml
apiVersion: operator.dataverse.redhat.com/v1alpha1
kind: FivetranConnector
metadata:
  name: fivetran-log-connector
  labels:
    app.kubernetes.io/name: fivetran-operator
    app.kubernetes.io/managed-by: kustomize
spec:
  connector:
    group_id: "logging_group"
    service: "fivetran_log"
    paused: false
    daily_sync_time: "21:00"
    sync_frequency: 1440  # Daily sync
    schedule_type: "auto"
    pause_after_trial: false
    config:
      is_account_level_connector: false
      schema: "fivetran_log"
```

### Maria (MySQL) Connector Example

This example shows real-world usage with Vault secret references for sensitive data:

```yaml
apiVersion: operator.dataverse.redhat.com/v1alpha1
kind: FivetranConnector
metadata:
  name: maria-mysql-connector
spec:
  connector:
    group_id: "production_group"
    service: "maria"
    paused: true
    schedule_type: "manual"
    networking_method: "Directly"
    hybrid_deployment_agent_id: "agent_xyz123"
    trust_certificates: true
    trust_fingerprints: false
    config:
      always_encrypted: true
      auth_method: "PASSWORD"
      connection_type: "Directly"
      database: "application_database"
      # Vault references for sensitive database connection details
      host: "vault:somepath#host"
      password: "vault:somepath#passkey"
      user: "vault:maria/production/somepath#username"
      # Non-sensitive configuration
      port: "3306"
      update_method: "TELEPORT"
      schema_prefix: "myapp_prod"
  connectorSchemas:
    schema_change_handling: "ALLOW_COLUMNS"
    schemas:
      application_database:
        enabled: true
        tables:
          users:
            enabled: true
          orders:
            enabled: true
          products:
            enabled: true
          # ... (configure tables as needed for your database)
          audit_logs:
            enabled: true
          sessions:
            enabled: true
          permissions:
            enabled: true
```


---

## Status Fields

The FivetranConnector provides status information about the managed connector:

- `status.connectorUrl`: URL of the created Fivetran connector
- `status.connectorId`: ID of the created Fivetran connector  
- `status.conditions`: Array of conditions representing the resource state

Common condition types include:
- `ConnectorReady`: Indicates if the connector is successfully created and configured
- `SetupTestReady`: Indicates if setup tests have passed
- `SchemaReady`: Indicates if schema configuration is applied successfully
