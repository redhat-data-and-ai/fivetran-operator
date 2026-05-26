# DATA-6769: Schema Policy Enforcement Fix

## The Bug

When `schema_change_handling` is set to `ALLOW_COLUMNS` or `BLOCK_ALL`, the operator was only sending CR-listed schemas/tables to the Fivetran API. Any upstream schema or table **not** mentioned in the CR was left untouched — meaning it stayed enabled and synced data.

## Example

Given this CR:

```yaml
connectorSchemas:
  schema_change_handling: BLOCK_ALL
  schemas:
    inventory:
      enabled: true
      tables:
        products:
          enabled: true
          sync_mode: HISTORY
          columns:
            sku:
              enabled: true
            name:
              enabled: true
```

And this upstream state (after schema discovery):

```
inventory (enabled)
  - products (enabled)
    columns: sku, name, weight, discontinued, internal_notes
  - warehouses (enabled)
  - stock (enabled)
sales (enabled)
  - orders (enabled)
  - invoices (enabled)
```

**Before (broken):** The operator only sent `inventory.products = enabled`. Everything else remained enabled. Columns were not managed at all. Result: 4 unintended tables syncing, all columns exposed.

**After (fixed):** The merge engine sends:

```
inventory = enabled
  products = enabled, sync_mode=HISTORY
    sku = enabled
    name = enabled
    weight = disabled
    discontinued = disabled
    internal_notes = disabled
  warehouses = disabled
  stock = disabled
sales = disabled
```

Only `inventory.products` syncs with only `sku` and `name` columns. Everything else is explicitly disabled.

## How It Works

1. Operator fetches current schema state from Fivetran (`GetSchemaDetails`)
2. For tables with columns in the CR, the operator fetches per-table column data (`GetColumnConfig`) to get accurate `enabled_patch_settings`
3. Locked columns are validated — if the CR attempts to disable a locked column (primary key, system column), reconciliation fails immediately with a clear error
4. Merge engine (`MergeSchemaWithPolicy`) compares upstream state with the CR
5. For `BLOCK_ALL`/`ALLOW_COLUMNS`: any upstream schema/table/column not in the CR is set to `enabled=false` (BLOCK_ALL) or left enabled (ALLOW_COLUMNS)
6. For `ALLOW_ALL`: only CR-listed items are sent (unspecified items keep their upstream state)
7. The merged payload is sent via `UpdateSchema`
8. A second pass runs as safety net for brand-new tables where columns weren't available on first fetch

## Column Management

The operator mirrors the Terraform provider's column handling:

| Policy | Columns in CR | Unlisted columns |
|--------|--------------|-----------------|
| `BLOCK_ALL` | Set to CR state | Disabled |
| `ALLOW_COLUMNS` | Set to CR state | Left enabled |
| `ALLOW_ALL` | Set to CR state | Left as-is |
| Any | No columns block | Not managed |

**Locked columns** (primary keys, system columns) cannot be disabled by Fivetran. The operator detects these via `enabled_patch_settings.allowed=false` and:
- Skips them in the merge payload (doesn't attempt to disable)
- Returns a non-retriable error if the CR explicitly sets `enabled: false` on a locked column

## Key Design Decisions

- **`exclude_mode=PRESERVE` on reload**: Schema discovery doesn't change enabled states. The merge engine handles all enable/disable logic.
- **Per-table column fetch**: The schema-level GET doesn't reliably return column data. The operator calls the per-table columns endpoint (like Terraform's `validateColumns`) to get accurate data before merging.
- **No inline retry**: If post-apply verification fails, it's a non-retriable config error (`ErrSchemaMismatch`). The operator stops and surfaces the condition.
- **Locked columns/tables respected**: Elements with `EnabledPatchSettings.Allowed=false` are skipped in the disable payload. Explicit attempts to disable them fail with `ErrLockedColumns` (non-retriable).
- **Two-pass safety net**: After the first PATCH enables tables, a second pass re-reads and applies column state for cases where the per-table endpoint had no data (brand-new tables not yet synced).
