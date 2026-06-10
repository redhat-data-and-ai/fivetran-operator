#!/usr/bin/env bash
#
# Runs the full E2E test suite with an ephemeral RDS Postgres instance.
# Creates the RDS, seeds it, runs tests, and destroys the RDS (always).
#
# Usage: ./run-e2e-with-rds.sh
#
# Required env vars:
#   E2E_FIVETRAN_API_KEY, E2E_FIVETRAN_API_SECRET, E2E_FIVETRAN_GROUP_ID
#   E2E_GOOGLE_SHEET_ID, E2E_GOOGLE_NAMED_RANGE
#   AWS credentials (AWS_ACCESS_KEY_ID/AWS_SECRET_ACCESS_KEY or AWS_PROFILE)
#
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(cd "${SCRIPT_DIR}/../../.." && pwd)"

cleanup() {
    echo ""
    echo "=== Cleaning up RDS (always runs) ==="
    cd "${SCRIPT_DIR}"
    ./rds-lifecycle.sh destroy || true
}
trap cleanup EXIT

echo "=== Step 1: Create RDS ==="
cd "${SCRIPT_DIR}"
./rds-lifecycle.sh create

echo ""
echo "=== Step 2: Seed database ==="
./rds-lifecycle.sh seed

echo ""
echo "=== Step 3: Run E2E tests ==="
# shellcheck source=/dev/null
source "${SCRIPT_DIR}/.rds-state.env"
export E2E_POSTGRES_HOST="${RDS_ENDPOINT}"
export E2E_POSTGRES_PASSWORD="${FIVETRAN_USER_PASSWORD}"

cd "${PROJECT_DIR}"
make test-e2e

echo ""
echo "=== All tests passed ==="
