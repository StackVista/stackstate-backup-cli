#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

TMP_DIR=/tmp-data

source $SCRIPT_DIR/restore-stackgraph-backup-v2-env.sh

echo "=== Importing StackGraph data (v2) from \"${BACKUP_FILE}\"..."

/opt/docker/bin/stackstate-server -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -import-v2-live "s3a://${BACKUP_V2_LOCATION}" -backup-name "${BACKUP_FILE}" "${FORCE_DELETE}"

echo "=== StackGraph live data loaded, please continue with backfill"

# === StackPacks Restore ===
if [ "${SKIP_STACKPACKS:-false}" == "true" ]; then
    echo "=== Skipping StackPacks restore (--skip-stackpacks flag set)"
else
    # Construct stackpacks backup filename from the original backup file
    STACKPACKS_FILE="${BACKUP_FILE}.stackpacks.zip"

    echo "=== Checking for StackPacks backup (v2) \"${STACKPACKS_FILE}\" in bucket \"${BACKUP_STACKGRAPH_BUCKET_NAME}\"..."

    # Check if stackpacks backup exists in S3
    if ! sts-toolbox aws s3 ls --endpoint "http://${S3_ENDPOINT}" --region us-east-1 --bucket "${BACKUP_STACKGRAPH_BUCKET_NAME}" --prefix "${BACKUP_STACKGRAPH_S3_PREFIX}v2/${BACKUP_STACKGRAPH_STACKPACKS_DIR}${STACKPACKS_FILE}" 2>/dev/null | grep -q "${STACKPACKS_FILE}"; then
        echo "=== ERROR: StackPacks backup \"${STACKPACKS_FILE}\" not found in S3"
        exit 1
    fi

    echo "=== Downloading StackPacks backup..."
    sts-toolbox aws s3 cp --endpoint "http://${S3_ENDPOINT}" --region us-east-1 "s3://${BACKUP_STACKGRAPH_BUCKET_NAME}/${BACKUP_STACKGRAPH_S3_PREFIX}v2/${BACKUP_STACKGRAPH_STACKPACKS_DIR}${STACKPACKS_FILE}" "${TMP_DIR}/${STACKPACKS_FILE}"

    echo "=== Restoring StackPacks from \"${STACKPACKS_FILE}\"..."
    /opt/docker/bin/stack-packs-backup -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -restore "${TMP_DIR}/${STACKPACKS_FILE}"
    echo "=== StackPacks restore complete"
fi
echo "==="
