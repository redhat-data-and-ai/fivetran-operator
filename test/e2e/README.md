# E2E Tests

End-to-end tests for the fivetran-operator. Tests run in a Kind cluster with the operator deployed, creating real Fivetran connectors against the `epic_basis` test account.

## Prerequisites

- Kind
- Podman (or Docker with `CONTAINER_TOOL=docker`)
- kubectl
- psql (PostgreSQL client: `brew install libpq` on macOS)
- AWS CLI authenticated to the internal testing account (for schema policy tests)

## Environment Variables

| Variable | Required for | Source |
|---|---|---|
| `E2E_FIVETRAN_API_KEY` | All lifecycle tests | Vault / GitHub secrets |
| `E2E_FIVETRAN_API_SECRET` | All lifecycle tests | Vault / GitHub secrets |
| `E2E_FIVETRAN_GROUP_ID` | All lifecycle tests | `epic_basis` |
| `E2E_GOOGLE_SHEET_ID` | Google Sheets tests | Test sheet ID |
| `E2E_GOOGLE_NAMED_RANGE` | Google Sheets tests | Test named range |
| `E2E_POSTGRES_HOST` | Schema policy tests | Set by `rds-lifecycle.sh` |
| `E2E_POSTGRES_PASSWORD` | Schema policy tests | Set by `rds-lifecycle.sh` |
| AWS credentials | Schema policy tests | SAML or IAM user |

## Running Tests

### Full suite (all 18 tests including schema policy)

Requires AWS credentials and Fivetran env vars:

```bash
export E2E_FIVETRAN_API_KEY="<ask team>"
export E2E_FIVETRAN_API_SECRET="<ask team>"
export E2E_FIVETRAN_GROUP_ID="epic_basis"
export E2E_GOOGLE_SHEET_ID="<test sheet ID>"
export E2E_GOOGLE_NAMED_RANGE="<test named range>"

# Authenticate to AWS (SAML or static credentials)
aws-saml.py  # choose poweruser role
export AWS_PROFILE=saml

# Single command: creates RDS, seeds, tests, destroys RDS
./test/e2e/scripts/run-e2e-with-rds.sh
```

### Without AWS/Postgres (Google Sheets + orphan + metrics)

Schema policy tests skip automatically when Postgres vars are not set:

```bash
export E2E_FIVETRAN_API_KEY="<ask team>"
export E2E_FIVETRAN_API_SECRET="<ask team>"
export E2E_FIVETRAN_GROUP_ID="epic_basis"
export E2E_GOOGLE_SHEET_ID="<test sheet ID>"
export E2E_GOOGLE_NAMED_RANGE="<test named range>"

make test-e2e
```

### Metrics only (no credentials needed)

```bash
export E2E_SKIP_LIFECYCLE=true
make test-e2e
```

### Without credentials (fails fast)

If env vars are missing and `E2E_SKIP_LIFECYCLE` is not set, BeforeSuite fails immediately with a clear error message.

## Test Structure

| File | Tests |
|---|---|
| `e2e_test.go` | Metrics endpoint |
| `connector_lifecycle_test.go` | Google Sheets create/delete, orphan deletion |
| `schema_policy_test.go` | BLOCK_ALL, ALLOW_COLUMNS, ALLOW_ALL, column policies, locked columns, validation level, force reconcile, hash skip |

## RDS Lifecycle

The schema policy tests use an ephemeral RDS Postgres instance:

```bash
# Manual control (if not using run-e2e-with-rds.sh)
cd test/e2e/scripts
./rds-lifecycle.sh create    # Create RDS + security group (~5-8 min)
./rds-lifecycle.sh seed      # Populate test schema
./rds-lifecycle.sh endpoint  # Print RDS endpoint
./rds-lifecycle.sh destroy   # Delete RDS + security group
```

The RDS is tagged with `appcode: FVTR-001` and restricted to Fivetran's published IP addresses. Credentials are randomly generated per run and stored in `.rds-state.env` (gitignored).
