#!/usr/bin/env bash
#
# Manages an ephemeral RDS Postgres instance for E2E schema policy tests.
# Usage: rds-lifecycle.sh {create|seed|destroy|endpoint}
#
set -euo pipefail

: "${AWS_REGION:=us-west-2}"
RDS_SUFFIX="${RDS_SUFFIX:=$(openssl rand -hex 4)}"
: "${RDS_INSTANCE_ID:=fivetran-e2e-${RDS_SUFFIX}}"
: "${RDS_DB_NAME:=fivetran_e2e}"
: "${RDS_MASTER_USER:=e2e_admin}"
: "${RDS_MASTER_PASSWORD:=$(openssl rand -hex 16)}"
: "${RDS_INSTANCE_CLASS:=db.t4g.micro}"
: "${RDS_SG_NAME:=fivetran-e2e-sg-${RDS_SUFFIX}}"
: "${FIVETRAN_CIDR:=3.239.194.48/29}"
: "${FIVETRAN_USER_PASSWORD:=$(openssl rand -hex 16)}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
STATE_FILE="${SCRIPT_DIR}/.rds-state.env"

save_state() {
    cat > "${STATE_FILE}" <<EOF
RDS_INSTANCE_ID=${RDS_INSTANCE_ID}
RDS_SG_NAME=${RDS_SG_NAME}
RDS_DB_NAME=${RDS_DB_NAME}
RDS_MASTER_USER=${RDS_MASTER_USER}
RDS_MASTER_PASSWORD=${RDS_MASTER_PASSWORD}
RDS_SG_ID=${RDS_SG_ID:-}
RDS_ENDPOINT=${RDS_ENDPOINT:-}
FIVETRAN_USER_PASSWORD=${FIVETRAN_USER_PASSWORD}
EOF
    chmod 600 "${STATE_FILE}"
}

load_state() {
    if [[ -f "${STATE_FILE}" ]]; then
        # shellcheck source=/dev/null
        source "${STATE_FILE}"
    else
        echo "ERROR: No state file found. Run 'create' first." >&2
        exit 1
    fi
}

cmd_create() {
    echo "=== Creating E2E RDS instance ==="

    local vpc_id
    vpc_id=$(aws ec2 describe-vpcs \
        --filters "Name=isDefault,Values=true" \
        --query "Vpcs[0].VpcId" --output text \
        --region "${AWS_REGION}")

    if [[ "${vpc_id}" == "None" || -z "${vpc_id}" ]]; then
        echo "ERROR: No default VPC found in ${AWS_REGION}" >&2
        exit 1
    fi
    echo "Using VPC: ${vpc_id}"

    RDS_SG_ID=$(aws ec2 describe-security-groups \
        --filters "Name=group-name,Values=${RDS_SG_NAME}" "Name=vpc-id,Values=${vpc_id}" \
        --query "SecurityGroups[0].GroupId" --output text \
        --region "${AWS_REGION}" 2>/dev/null || echo "")
    if [[ -z "${RDS_SG_ID}" || "${RDS_SG_ID}" == "None" ]]; then
        echo "Creating security group..."
        RDS_SG_ID=$(aws ec2 create-security-group \
            --group-name "${RDS_SG_NAME}" \
            --description "Fivetran E2E test - ephemeral" \
            --vpc-id "${vpc_id}" \
            --query "GroupId" --output text \
            --region "${AWS_REGION}")
    else
        echo "Reusing existing security group: ${RDS_SG_ID}"
    fi
    echo "Security group: ${RDS_SG_ID}"

    echo "Adding Fivetran IP ingress rule (${FIVETRAN_CIDR})..."
    aws ec2 authorize-security-group-ingress \
        --group-id "${RDS_SG_ID}" \
        --protocol tcp --port 5432 \
        --cidr "${FIVETRAN_CIDR}" \
        --region "${AWS_REGION}" > /dev/null 2>&1 || echo "  (rule already exists, skipping)"

    echo "Adding local IP ingress rule for seeding..."
    local my_ip
    my_ip=$(curl -s --retry 3 --retry-connrefused https://checkip.amazonaws.com || true)
    if [[ -z "${my_ip}" ]]; then
        echo "WARNING: Failed to detect local IP, skipping local ingress rule"
    elif [[ ! "${my_ip}" =~ ^[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
        echo "WARNING: Detected non-IPv4 address (${my_ip}), skipping local ingress rule"
    else
        aws ec2 authorize-security-group-ingress \
            --group-id "${RDS_SG_ID}" \
            --protocol tcp --port 5432 \
            --cidr "${my_ip}/32" \
            --region "${AWS_REGION}" > /dev/null 2>&1 || echo "  (rule already exists, skipping)"
        echo "Added ingress for ${my_ip}/32"
    fi

    echo "Creating RDS instance ${RDS_INSTANCE_ID}..."
    aws rds create-db-instance \
        --db-instance-identifier "${RDS_INSTANCE_ID}" \
        --db-instance-class "${RDS_INSTANCE_CLASS}" \
        --engine postgres \
        --engine-version "16" \
        --master-username "${RDS_MASTER_USER}" \
        --master-user-password "${RDS_MASTER_PASSWORD}" \
        --db-name "${RDS_DB_NAME}" \
        --allocated-storage 20 \
        --storage-type gp3 \
        --storage-encrypted \
        --vpc-security-group-ids "${RDS_SG_ID}" \
        --publicly-accessible \
        --backup-retention-period 0 \
        --no-multi-az \
        --tags "Key=appcode,Value=FVTR-001" "Key=purpose,Value=e2e-test" \
        --region "${AWS_REGION}" > /dev/null

    echo "Waiting for RDS to become available (this takes 5-8 minutes)..."
    aws rds wait db-instance-available \
        --db-instance-identifier "${RDS_INSTANCE_ID}" \
        --region "${AWS_REGION}"

    RDS_ENDPOINT=$(aws rds describe-db-instances \
        --db-instance-identifier "${RDS_INSTANCE_ID}" \
        --query "DBInstances[0].Endpoint.Address" --output text \
        --region "${AWS_REGION}")

    echo "RDS available at: ${RDS_ENDPOINT}"

    save_state

    echo ""
    echo "=== RDS created successfully ==="
    echo "Endpoint: ${RDS_ENDPOINT}"
    echo "Database: ${RDS_DB_NAME}"
    echo "Master user: ${RDS_MASTER_USER}"
    echo "State saved to: ${STATE_FILE}"
    echo ""
    echo "Next: run './rds-lifecycle.sh seed' to populate the test schema"
}

cmd_seed() {
    load_state
    echo "=== Seeding E2E database ==="

    local seed_sql="${SCRIPT_DIR}/seed.sql"
    if [[ ! -f "${seed_sql}" ]]; then
        echo "ERROR: seed.sql not found at ${seed_sql}" >&2
        exit 1
    fi

    echo "Connecting to ${RDS_ENDPOINT}..."
    PGPASSWORD="${RDS_MASTER_PASSWORD}" psql \
        -h "${RDS_ENDPOINT}" \
        -U "${RDS_MASTER_USER}" \
        -d "${RDS_DB_NAME}" \
        -v fivetran_password="${FIVETRAN_USER_PASSWORD}" \
        -f "${seed_sql}" \
        -q

    echo ""
    echo "=== Database seeded ==="
    echo "Fivetran user: fivetran_e2e"
    echo "Credentials stored in ${STATE_FILE} (not logged)"
    echo ""
    echo "Set env vars for E2E tests:"
    echo "  source ${STATE_FILE}"
    echo "  export E2E_POSTGRES_HOST=\${RDS_ENDPOINT}"
    echo "  export E2E_POSTGRES_PASSWORD=\${FIVETRAN_USER_PASSWORD}"
}

cmd_endpoint() {
    load_state
    echo "${RDS_ENDPOINT}"
}

cmd_destroy() {
    echo "=== Destroying E2E RDS instance ==="

    if [[ -f "${STATE_FILE}" ]]; then
        load_state
    fi

    if aws rds describe-db-instances \
        --db-instance-identifier "${RDS_INSTANCE_ID}" \
        --region "${AWS_REGION}" > /dev/null 2>&1; then
        echo "Deleting RDS instance ${RDS_INSTANCE_ID}..."
        aws rds delete-db-instance \
            --db-instance-identifier "${RDS_INSTANCE_ID}" \
            --skip-final-snapshot \
            --delete-automated-backups \
            --region "${AWS_REGION}" > /dev/null

        echo "Waiting for RDS deletion to complete..."
        aws rds wait db-instance-deleted \
            --db-instance-identifier "${RDS_INSTANCE_ID}" \
            --region "${AWS_REGION}" 2>/dev/null || true
        echo "RDS instance deleted."
    else
        echo "RDS instance ${RDS_INSTANCE_ID} not found, skipping."
    fi

    local sg_id="${RDS_SG_ID:-}"
    if [[ -z "${sg_id}" ]]; then
        sg_id=$(aws ec2 describe-security-groups \
            --filters "Name=group-name,Values=${RDS_SG_NAME}" \
            --query "SecurityGroups[0].GroupId" --output text \
            --region "${AWS_REGION}" 2>/dev/null || echo "")
    fi

    if [[ -n "${sg_id}" && "${sg_id}" != "None" ]]; then
        echo "Deleting security group ${sg_id}..."
        local deleted=false
        for i in {1..6}; do
            if aws ec2 delete-security-group --group-id "${sg_id}" --region "${AWS_REGION}" 2>/dev/null; then
                echo "Security group deleted."
                deleted=true
                break
            fi
            echo "Security group still in use, retrying in 10 seconds... ($i/6)"
            sleep 10
        done
        if [[ "${deleted}" = "false" ]]; then
            echo "WARNING: Failed to delete security group ${sg_id} after multiple retries."
        fi
    fi

    rm -f "${STATE_FILE}"
    echo "=== Cleanup complete ==="
}

check_psql() {
    if ! command -v psql &> /dev/null; then
        echo "ERROR: psql not installed." >&2
        echo "  macOS:  brew install libpq && brew link --force libpq" >&2
        echo "  Ubuntu: sudo apt-get install -y postgresql-client" >&2
        exit 1
    fi
}

check_aws_auth() {
    if ! aws sts get-caller-identity --region "${AWS_REGION}" > /dev/null 2>&1; then
        echo "ERROR: Not authenticated to AWS. Run your AWS login (e.g., aws-saml.py) first." >&2
        exit 1
    fi
}

case "${1:-}" in
    create)  check_psql; check_aws_auth; cmd_create ;;
    seed)    check_psql; cmd_seed ;;
    destroy) check_aws_auth; cmd_destroy ;;
    endpoint) cmd_endpoint ;;
    *)
        echo "Usage: $0 {create|seed|destroy|endpoint}" >&2
        echo ""
        echo "  create   - Create RDS instance and security group"
        echo "  seed     - Populate the database with test schema"
        echo "  destroy  - Delete RDS instance and security group"
        echo "  endpoint - Print the RDS endpoint"
        exit 1
        ;;
esac
