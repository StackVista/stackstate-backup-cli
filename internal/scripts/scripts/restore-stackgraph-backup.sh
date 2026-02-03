#!/usr/bin/env bash
set -Eeuo pipefail

TMP_DIR=/tmp-data

export AWS_ACCESS_KEY_ID
AWS_ACCESS_KEY_ID="$(cat /aws-keys/accesskey)"
export AWS_SECRET_ACCESS_KEY
AWS_SECRET_ACCESS_KEY="$(cat /aws-keys/secretkey)"

echo "=== Downloading StackGraph backup \"${BACKUP_FILE}\" from bucket \"${BACKUP_STACKGRAPH_BUCKET_NAME}\"..."

if [ "${BACKUP_STACKGRAPH_MULTIPART_ARCHIVE:-false}" == "false" ]; then
    sts-toolbox aws s3 cp --endpoint "http://${MINIO_ENDPOINT}" --region minio "s3://${BACKUP_STACKGRAPH_BUCKET_NAME}/${BACKUP_STACKGRAPH_S3_PREFIX}${BACKUP_FILE}" "${TMP_DIR}/${BACKUP_FILE}"
else
    # Check if the filename of the snapshot is one of the multiparts
    # sts-backup-20240222-0730.graph.00 -> sts-backup-20240222-0730.graph
    BACKUP_FILE="${BACKUP_FILE/%.[0-9]*/}"
    rm -f "${TMP_DIR}/${BACKUP_FILE}.*"
    sts-toolbox aws s3 ls --endpoint "http://${MINIO_ENDPOINT}" --region minio --bucket "${BACKUP_STACKGRAPH_BUCKET_NAME}" --prefix "${BACKUP_STACKGRAPH_S3_PREFIX}${BACKUP_FILE}" | while read -r backup_file
    do
      sts-toolbox aws s3 cp --endpoint "http://${MINIO_ENDPOINT}" --region minio "s3://${BACKUP_STACKGRAPH_BUCKET_NAME}/${BACKUP_STACKGRAPH_S3_PREFIX}${backup_file}" "${TMP_DIR}/${backup_file}"
    done
    # Concatenate a multipart arhive
    find ${TMP_DIR} -name "${BACKUP_FILE}.*" | sort | while read -r multipart
    do
      cat "${multipart}" >> "${TMP_DIR}/${BACKUP_FILE}"
    done
fi

echo "=== Importing StackGraph data from \"${BACKUP_FILE}\"..."
/opt/docker/bin/stackstate-server -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -import "${TMP_DIR}/${BACKUP_FILE}" "${FORCE_DELETE}"
echo "=== StackGraph restore complete"

# === StackPacks Restore ===
if [ "${SKIP_STACKPACKS:-false}" == "true" ]; then
    echo "=== Skipping StackPacks restore (--skip-stackpacks flag set)"
else
    # Construct stackpacks backup filename from the original backup file
    STACKPACKS_FILE="${BACKUP_FILE}.stackpacks.zip"

    echo "=== Checking for StackPacks backup \"${STACKPACKS_FILE}\" in bucket \"${BACKUP_STACKGRAPH_BUCKET_NAME}\"..."

    # Check if stackpacks backup exists in S3
    if sts-toolbox aws s3 ls --endpoint "http://${MINIO_ENDPOINT}" --region minio --bucket "${BACKUP_STACKGRAPH_BUCKET_NAME}" --prefix "${BACKUP_STACKGRAPH_STACKPACKS_S3_PREFIX}${STACKPACKS_FILE}" 2>/dev/null | grep -q "${STACKPACKS_FILE}"; then
        echo "=== Downloading StackPacks backup..."
        sts-toolbox aws s3 cp --endpoint "http://${MINIO_ENDPOINT}" --region minio "s3://${BACKUP_STACKGRAPH_BUCKET_NAME}/${BACKUP_STACKGRAPH_STACKPACKS_S3_PREFIX}${STACKPACKS_FILE}" "${TMP_DIR}/${STACKPACKS_FILE}"

        echo "=== Restoring StackPacks from \"${STACKPACKS_FILE}\"..."
        /opt/docker/bin/stack-packs-backup -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -restore "${TMP_DIR}/${STACKPACKS_FILE}"
        echo "=== StackPacks restore complete"
    else
        echo "=== WARNING: StackPacks backup \"${STACKPACKS_FILE}\" not found in S3, skipping StackPacks restore"
    fi
fi
echo "==="
