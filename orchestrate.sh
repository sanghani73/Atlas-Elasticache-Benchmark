#!/usr/bin/env bash
set -euo pipefail

PROJECT_DIR="$(cd "$(dirname "$0")" && pwd)"
TF_DIR="${PROJECT_DIR}/terraform"
SSH_OPTS="-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o LogLevel=ERROR"
REMOTE_DIR="~/bench"
PIPELINE_START=$(date +%s)

# ─────────────────────────────────────────────
# Helpers
# ─────────────────────────────────────────────

elapsed() {
    local now=$(date +%s)
    local elapsed=$(( now - PIPELINE_START ))
    printf "%dm %ds" $(( elapsed / 60 )) $(( elapsed % 60 ))
}

log_phase() {
    echo ""
    echo "================================================================"
    echo "  Phase: $1"
    echo "  Time:  $(date)  |  Elapsed: $(elapsed)"
    echo "================================================================"
}

cleanup() {
    local exit_code=$?
    if (( exit_code != 0 )); then
        echo ""
        echo "ERROR: orchestrate.sh failed (exit code ${exit_code})."
        echo "Infrastructure may still be running."
        echo ""
        echo "To clean up manually:"
        echo "  terraform -chdir=terraform destroy -auto-approve \\"
        echo "    -var=\"aws_profile=\${AWS_PROFILE}\" -var=\"my_ip=\$(curl -s -4 https://checkip.amazonaws.com)\" \\"
        echo "    -var=\"tag_owner=\${TAG_OWNER}\" -var=\"atlas_public_key=\${ATLAS_PUBLIC_KEY}\" \\"
        echo "    -var=\"atlas_private_key=\${ATLAS_PRIVATE_KEY}\" -var=\"atlas_project_id=\${ATLAS_PROJECT_ID}\" \\"
        echo "    -var=\"db_password=\${DB_PASSWORD}\""
    fi
}
trap cleanup EXIT

# ─────────────────────────────────────────────
# Phase 1: Preflight Checks
# ─────────────────────────────────────────────

log_phase "1/12 - Preflight Checks"

for cmd in terraform aws ssh scp; do
    if ! command -v "$cmd" &>/dev/null; then
        echo "ERROR: '${cmd}' is required but not found in PATH."
        exit 1
    fi
    echo "  ✓ ${cmd} found"
done

if [[ ! -f "${PROJECT_DIR}/config.env" ]]; then
    echo "ERROR: config.env not found."
    echo "  cp config.env.template config.env   # then fill in your values"
    exit 1
fi
echo "  ✓ config.env found"

source "${PROJECT_DIR}/config.env"

if [[ "${AWS_PROFILE:-}" == "your-aws-sso-profile" || -z "${AWS_PROFILE:-}" ]]; then
    echo "ERROR: AWS_PROFILE is not set in config.env."
    exit 1
fi

if [[ "${ATLAS_PUBLIC_KEY:-}" == "your-public-key" || -z "${ATLAS_PUBLIC_KEY:-}" ]]; then
    echo "ERROR: Atlas API credentials not configured in config.env."
    exit 1
fi

if [[ "${TAG_OWNER:-}" == "your-name" || -z "${TAG_OWNER:-}" ]]; then
    echo "ERROR: TAG_OWNER is not set in config.env."
    exit 1
fi

echo "  Verifying AWS SSO session for profile '${AWS_PROFILE}'..."
if ! aws sts get-caller-identity --profile "${AWS_PROFILE}" &>/dev/null; then
    echo "ERROR: AWS SSO session expired or invalid."
    echo "  Run: aws sso login --profile ${AWS_PROFILE}"
    exit 1
fi
echo "  ✓ AWS SSO session active"

echo "  Detecting public IP for SSH security group..."
MY_IP=$(curl -s -4 https://checkip.amazonaws.com | tr -d '[:space:]')
echo "  ✓ Public IP: ${MY_IP}"

# Build Terraform variable flags
TF_VARS=(
    -var="aws_profile=${AWS_PROFILE}"
    -var="aws_region=${AWS_REGION:-eu-west-2}"
    -var="my_ip=${MY_IP}"
    -var="tag_owner=${TAG_OWNER}"
    -var="tag_purpose=${TAG_PURPOSE:-benchmark}"
    -var="atlas_public_key=${ATLAS_PUBLIC_KEY}"
    -var="atlas_private_key=${ATLAS_PRIVATE_KEY}"
    -var="atlas_project_id=${ATLAS_PROJECT_ID}"
    -var="atlas_cluster_name=${ATLAS_CLUSTER_NAME:-bench-atlas-m30}"
    -var="atlas_instance_size=${ATLAS_INSTANCE_SIZE:-M30}"
    -var="db_username=${DB_USERNAME:-benchuser}"
    -var="db_password=${DB_PASSWORD}"
    -var="instance_type=${EC2_INSTANCE_TYPE:-c5.2xlarge}"
    -var="atlas_region=${ATLAS_REGION:-EU_WEST_2}"
)
if [[ -n "${TAG_EXPIRE_ON:-}" ]]; then
    TF_VARS+=(-var="tag_expire_on=${TAG_EXPIRE_ON}")
fi

# ─────────────────────────────────────────────
# Phase 2: Terraform Apply
# ─────────────────────────────────────────────

log_phase "2/12 - Provision Infrastructure (Terraform)"

echo "  Running terraform init..."
terraform -chdir="${TF_DIR}" init -input=false

echo "  Running terraform apply (this may take 10-15 minutes for Atlas + ElastiCache)..."
terraform -chdir="${TF_DIR}" apply -auto-approve -input=false \
    "${TF_VARS[@]}"

EC2_IP=$(terraform -chdir="${TF_DIR}" output -raw ec2_public_ip)
SSH_KEY=$(terraform -chdir="${TF_DIR}" output -raw ssh_private_key_path)
EC2_ID=$(terraform -chdir="${TF_DIR}" output -raw ec2_instance_id)
REDIS_ENDPOINT=$(terraform -chdir="${TF_DIR}" output -raw redis_endpoint)
ATLAS_CONN_PRIVATE=$(terraform -chdir="${TF_DIR}" output -raw atlas_connection_string_private 2>/dev/null || echo "")
ATLAS_CONN_PUBLIC=$(terraform -chdir="${TF_DIR}" output -raw atlas_connection_string_public)
ATLAS_VERSION=$(terraform -chdir="${TF_DIR}" output -raw atlas_mongodb_version 2>/dev/null || echo "unknown")

# Use private connection string (VPC peering) if available, otherwise public
if [[ -n "${ATLAS_CONN_PRIVATE}" ]]; then
    ATLAS_SRV="${ATLAS_CONN_PRIVATE}"
    echo "  ✓ Using Atlas private connection string (VPC peering)"
else
    ATLAS_SRV="${ATLAS_CONN_PUBLIC}"
    echo "  ⚠ VPC peering not yet active, using public connection string"
fi

# Build the full MongoDB URI
MONGODB_URI="mongodb+srv://${DB_USERNAME}:${DB_PASSWORD}@${ATLAS_SRV#mongodb+srv://}/benchmark?retryWrites=true&w=1"

echo "  EC2 instance:       ${EC2_ID} (${EC2_IP})"
echo "  Redis:              ${REDIS_ENDPOINT}:6379"
echo "  Atlas cluster:      ${ATLAS_SRV}"

# Save connection info for reference
mkdir -p "${PROJECT_DIR}/results"
cat > "${PROJECT_DIR}/results/connections.env" <<EOF
EC2_IP="${EC2_IP}"
REDIS_ENDPOINT="${REDIS_ENDPOINT}"
ATLAS_CONNECTION_STRING="${ATLAS_SRV}"
ATLAS_MONGODB_VERSION="${ATLAS_VERSION}"
MONGODB_URI="${MONGODB_URI}"
EOF

# ─────────────────────────────────────────────
# Phase 3: Wait for EC2 Readiness
# ─────────────────────────────────────────────

log_phase "3/12 - Wait for EC2 & Install Prerequisites"

echo "  Waiting for SSH to become available..."
MAX_SSH_WAIT=300
WAITED=0
while ! ssh ${SSH_OPTS} -i "${SSH_KEY}" -o ConnectTimeout=5 ec2-user@"${EC2_IP}" "true" 2>/dev/null; do
    if (( WAITED >= MAX_SSH_WAIT )); then
        echo "ERROR: Timed out waiting for SSH after ${MAX_SSH_WAIT}s."
        exit 1
    fi
    sleep 15
    WAITED=$((WAITED + 15))
    echo "    ... waiting (${WAITED}s)"
done
echo "  ✓ SSH accessible"

echo "  Checking if prerequisites already installed..."
if ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" "test -f /tmp/userdata-complete" 2>/dev/null; then
    echo "  ✓ Prerequisites already installed (previous run)"
else
    echo "  Installing prerequisites via SSH..."
    ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" bash -s <<'SETUPEOF'
set -euxo pipefail
export HOME="${HOME:-/home/ec2-user}"

echo "--- Updating packages ---"
sudo dnf update -y --skip-broken

echo "--- Installing base tools ---"
sudo dnf install -y python3 git tar gzip bind-utils

echo "--- Installing Go 1.22.6 ---"
GO_VERSION="1.22.6"
curl -fsSL "https://go.dev/dl/go${GO_VERSION}.linux-amd64.tar.gz" -o /tmp/go.tar.gz
sudo tar -C /usr/local -xzf /tmp/go.tar.gz
rm -f /tmp/go.tar.gz

sudo tee /etc/profile.d/golang.sh > /dev/null << 'GOEOF'
export PATH=$PATH:/usr/local/go/bin
export GOPATH=${HOME}/go
export PATH=$PATH:$GOPATH/bin
GOEOF

export PATH=$PATH:/usr/local/go/bin
export GOPATH=${HOME}/go
export PATH=$PATH:$GOPATH/bin
go version

echo "--- Installing redis-cli ---"
sudo dnf install -y redis6 2>/dev/null || {
    sudo dnf install -y gcc make openssl-devel
    curl -fsSL https://download.redis.io/redis-stable.tar.gz -o /tmp/redis.tar.gz
    tar -C /tmp -xzf /tmp/redis.tar.gz
    cd /tmp/redis-stable
    make redis-cli BUILD_TLS=yes
    sudo cp src/redis-cli /usr/local/bin/
    cd ~
    rm -rf /tmp/redis-stable /tmp/redis.tar.gz
}

# redis6 package may install as redis6-cli
if ! command -v redis-cli &>/dev/null; then
    if command -v redis6-cli &>/dev/null; then
        sudo ln -sf "$(which redis6-cli)" /usr/local/bin/redis-cli
    fi
fi

redis-cli --version
python3 --version

touch /tmp/userdata-complete
echo "--- Setup complete ---"
SETUPEOF
    echo "  ✓ Prerequisites installed"
fi

echo "  Verifying prerequisites on EC2..."
ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" "source /etc/profile.d/golang.sh && go version"
ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" "python3 --version"
echo "  ✓ Go and Python available"

# ─────────────────────────────────────────────
# Phase 4: Upload & Compile Benchmark Tool
# ─────────────────────────────────────────────

log_phase "4/12 - Upload & Compile Benchmark Tool"

ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" "mkdir -p ${REMOTE_DIR}/{results,analysis}"

scp ${SSH_OPTS} -i "${SSH_KEY}" -r \
    "${PROJECT_DIR}/benchmark" \
    "${PROJECT_DIR}/analysis" \
    "${PROJECT_DIR}/config.env" \
    ec2-user@"${EC2_IP}":${REMOTE_DIR}/

echo "  ✓ Files uploaded"

echo "  Compiling Go benchmark tool..."
ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" \
    "source /etc/profile.d/golang.sh && cd ${REMOTE_DIR}/benchmark && go mod tidy && go build -o ../benchmark-tool ."
echo "  ✓ Benchmark tool compiled"

# ─────────────────────────────────────────────
# Phase 5: Connectivity Checks
# ─────────────────────────────────────────────

log_phase "5/12 - Connectivity Checks"

echo "  Pinging Redis..."
ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" \
    "redis-cli -h ${REDIS_ENDPOINT} -p 6379 ping"
echo "  ✓ Redis reachable"

echo "  Measuring network RTT to each target..."
RTT_FILE="${PROJECT_DIR}/results/network_rtt.txt"
ssh ${SSH_OPTS} -i "${SSH_KEY}" ec2-user@"${EC2_IP}" bash -s <<RTTEOF > "${RTT_FILE}"
echo "=== Network RTT Measurements ==="
echo ""
echo "--- Redis (${REDIS_ENDPOINT}) ---"
redis-cli -h ${REDIS_ENDPOINT} -p 6379 --latency-history -i 1 2>/dev/null | head -5 || echo "  (latency measurement not available)"
echo ""
echo "--- Atlas MongoDB ---"
echo "  Testing with benchmark tool ping (will be measured during seeding)"
RTTEOF
echo "  ✓ Network RTT captured"

# ─────────────────────────────────────────────
# Phase 6: Seed Small Dataset
# ─────────────────────────────────────────────

log_phase "6/12 - Seed Small Dataset (${SMALL_RECORD_COUNT} records)"

echo "  Seeding MongoDB + Redis..."
ssh ${SSH_OPTS} -i "${SSH_KEY}" -o ServerAliveInterval=60 -o ServerAliveCountMax=60 \
    ec2-user@"${EC2_IP}" bash -s <<SEEDEOF
cd ${REMOTE_DIR}
source /etc/profile.d/golang.sh 2>/dev/null || true

./benchmark-tool seed \
    --mongodb-uri '${MONGODB_URI}' \
    --redis-addr '${REDIS_ENDPOINT}:6379' \
    --records ${SMALL_RECORD_COUNT} \
    --output-dir ./results 2>&1 | tee results/seed_small_main.log
SEEDEOF

echo "  ✓ Small dataset seeded"

# ─────────────────────────────────────────────
# Phase 7: Run Benchmarks (Small Dataset)
# ─────────────────────────────────────────────

log_phase "7/12 - Run Benchmarks (Small Dataset, In-Memory)"

WORKLOADS="point_lookup secondary_lookup filtered_query covered_query range_scan mixed_readwrite"

total_runs=0
for workload in $WORKLOADS; do
    for threads in $THREAD_COUNTS; do
        total_runs=$((total_runs + 2))  # 2 targets
    done
done
current_run=0

for workload in $WORKLOADS; do
    for threads in $THREAD_COUNTS; do
        for target_info in "mongodb:${MONGODB_URI}" "redis:${REDIS_ENDPOINT}:6379"; do
            target="${target_info%%:*}"
            addr="${target_info#*:}"
            current_run=$((current_run + 1))

            echo ""
            echo "--- Run ${current_run}/${total_runs}: ${target} | ${workload} | ${threads} threads ---"
            echo "  Started at: $(date)"

            if [[ "$target" == "mongodb" ]]; then
                URI_FLAG="--mongodb-uri '${addr}'"
            else
                URI_FLAG="--redis-addr '${addr}'"
            fi

            ssh ${SSH_OPTS} -i "${SSH_KEY}" -o ServerAliveInterval=60 -o ServerAliveCountMax=60 \
                ec2-user@"${EC2_IP}" \
                "cd ${REMOTE_DIR} && source /etc/profile.d/golang.sh 2>/dev/null; ./benchmark-tool run \
                    --target ${target} \
                    ${URI_FLAG} \
                    --workload ${workload} \
                    --threads ${threads} \
                    --duration ${RUN_DURATION_SECONDS} \
                    --warmup ${WARMUP_SECONDS} \
                    --records ${SMALL_RECORD_COUNT} \
                    --output-dir ./results/small" || echo "  WARNING: Run failed, continuing..."

            echo "  Finished at: $(date)"
            sleep "${COOLDOWN_SECONDS}"
        done
    done
done

echo ""
echo "  ✓ Small dataset benchmarks complete"

# ─────────────────────────────────────────────
# Phase 8: Seed Over-Memory Dataset
# ─────────────────────────────────────────────

log_phase "8/12 - Seed Over-Memory Dataset (${LARGE_RECORD_COUNT} total records)"

echo "  Adding records beyond ${SMALL_RECORD_COUNT} up to ${LARGE_RECORD_COUNT}..."
echo "  Redis LRU eviction will kick in — this is by design."

ssh ${SSH_OPTS} -i "${SSH_KEY}" -o ServerAliveInterval=60 -o ServerAliveCountMax=60 \
    ec2-user@"${EC2_IP}" bash -s <<SEEDLGEOF
cd ${REMOTE_DIR}
source /etc/profile.d/golang.sh 2>/dev/null || true

./benchmark-tool seed \
    --mongodb-uri '${MONGODB_URI}' \
    --redis-addr '${REDIS_ENDPOINT}:6379' \
    --records ${LARGE_RECORD_COUNT} \
    --start-from ${SMALL_RECORD_COUNT} \
    --output-dir ./results 2>&1 | tee results/seed_large_main.log
SEEDLGEOF

echo "  ✓ Over-memory dataset seeded"

# ─────────────────────────────────────────────
# Phase 9: Run Benchmarks (Over-Memory Dataset)
# ─────────────────────────────────────────────

log_phase "9/12 - Run Benchmarks (Over-Memory Dataset)"

# Run all workloads with the larger dataset
OVERMEM_WORKLOADS="point_lookup secondary_lookup filtered_query covered_query range_scan mixed_readwrite"

total_runs=0
for workload in $OVERMEM_WORKLOADS; do
    for threads in $THREAD_COUNTS; do
        total_runs=$((total_runs + 2))
    done
done
current_run=0

for workload in $OVERMEM_WORKLOADS; do
    for threads in $THREAD_COUNTS; do
        for target_info in "mongodb:${MONGODB_URI}" "redis:${REDIS_ENDPOINT}:6379"; do
            target="${target_info%%:*}"
            addr="${target_info#*:}"
            current_run=$((current_run + 1))

            echo ""
            echo "--- Over-Memory Run ${current_run}/${total_runs}: ${target} | ${workload} | ${threads} threads ---"

            if [[ "$target" == "mongodb" ]]; then
                URI_FLAG="--mongodb-uri '${addr}'"
            else
                URI_FLAG="--redis-addr '${addr}'"
            fi

            ssh ${SSH_OPTS} -i "${SSH_KEY}" -o ServerAliveInterval=60 -o ServerAliveCountMax=60 \
                ec2-user@"${EC2_IP}" \
                "cd ${REMOTE_DIR} && source /etc/profile.d/golang.sh 2>/dev/null; ./benchmark-tool run \
                    --target ${target} \
                    ${URI_FLAG} \
                    --workload ${workload} \
                    --threads ${threads} \
                    --duration ${RUN_DURATION_SECONDS} \
                    --warmup ${WARMUP_SECONDS} \
                    --records ${LARGE_RECORD_COUNT} \
                    --output-dir ./results/overmemory" || echo "  WARNING: Run failed, continuing..."

            sleep "${COOLDOWN_SECONDS}"
        done
    done
done

echo ""
echo "  ✓ Over-memory benchmarks complete"

# ─────────────────────────────────────────────
# Phase 10: Download Results
# ─────────────────────────────────────────────

log_phase "10/12 - Download Results"

scp ${SSH_OPTS} -i "${SSH_KEY}" -r \
    ec2-user@"${EC2_IP}":${REMOTE_DIR}/results/* \
    "${PROJECT_DIR}/results/"

echo "  ✓ Results downloaded to ${PROJECT_DIR}/results/"

# ─────────────────────────────────────────────
# Phase 11: Generate Report
# ─────────────────────────────────────────────

log_phase "11/12 - Generate Report"

python3 "${PROJECT_DIR}/analysis/generate_report.py"

echo "  ✓ Report generated"

# ─────────────────────────────────────────────
# Phase 12: Tear Down Infrastructure
# ─────────────────────────────────────────────

log_phase "12/12 - Tear Down Infrastructure"

echo "  Running terraform destroy..."
terraform -chdir="${TF_DIR}" destroy -auto-approve -input=false \
    "${TF_VARS[@]}"

echo "  ✓ All infrastructure terminated"

# ─────────────────────────────────────────────
# Done
# ─────────────────────────────────────────────

echo ""
echo "  ╔══════════════════════════════════════════════════════════════╗"
echo "  ║  ALL DONE                                                    ║"
echo "  ║                                                              ║"
echo "  ║  Report:  results/report.md                                  ║"
echo "  ║  CSV:     results/results.csv                                ║"
echo "  ║  Raw:     results/small/  and  results/overmemory/           ║"
echo "  ║                                                              ║"
printf "  ║  Total elapsed: %-41s║\n" "$(elapsed)"
echo "  ║                                                              ║"
echo "  ║  Atlas cluster:    DELETED                                   ║"
echo "  ║  ElastiCache:      DELETED                                   ║"
echo "  ║  EC2 instance:     TERMINATED                                ║"
echo "  ╚══════════════════════════════════════════════════════════════╝"
echo ""
