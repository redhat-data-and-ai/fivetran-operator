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
```

And this upstream state (after schema discovery):

```
inventory (enabled)
  - products (enabled)
  - warehouses (enabled)
  - stock (enabled)
sales (enabled)
  - orders (enabled)
  - invoices (enabled)
```

**Before (broken):** The operator only sent `inventory.products = enabled`. Everything else remained enabled. Result: 4 unintended tables syncing.

**After (fixed):** The merge engine sends:

```
inventory = enabled
  products = enabled, sync_mode=HISTORY
  warehouses = disabled
  stock = disabled
sales = disabled
```

Only `inventory.products` syncs. Everything else is explicitly disabled.

## How It Works

1. Operator fetches current schema state from Fivetran (`GetSchemaDetails`)
2. Merge engine (`MergeSchemaWithPolicy`) compares upstream state with the CR
3. For `BLOCK_ALL`/`ALLOW_COLUMNS`: any upstream schema/table not in the CR is set to `enabled=false`
4. For `ALLOW_ALL`: only CR-listed items are sent (unspecified items keep their upstream state)
5. The merged payload is sent via `UpdateSchema`

## Key Design Decisions

- **`exclude_mode=PRESERVE` on reload**: Schema discovery doesn't change enabled states. The merge engine handles all enable/disable logic.
- **No inline retry**: If post-apply verification fails, it's a non-retriable config error (`ErrSchemaMismatch`). The operator stops and surfaces the condition.
- **Locked tables respected**: Tables with `EnabledPatchSettings.Allowed=false` are skipped (not included in the disable payload).
