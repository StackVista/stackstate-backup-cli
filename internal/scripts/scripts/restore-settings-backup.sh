#!/usr/bin/env bash
set -Eeuo pipefail

export BACKUP_DIR=/settings-backup-data
export TMP_DIR=/tmp-data

setup_aws_credentials() {
  export AWS_ACCESS_KEY_ID
  AWS_ACCESS_KEY_ID="$(cat /aws-keys/accesskey)"
  export AWS_SECRET_ACCESS_KEY
  AWS_SECRET_ACCESS_KEY="$(cat /aws-keys/secretkey)"
}

download_from_s3() {
  local bucket="$1"
  local prefix="$2"
  local dest="$3"
  echo "=== Downloading Settings backup \"${BACKUP_FILE}\" from bucket \"${bucket}\"..."
  sts-toolbox aws s3 --endpoint "http://${MINIO_ENDPOINT}" --region minio cp "s3://${bucket}/${prefix}${BACKUP_FILE}" "${dest}/${BACKUP_FILE}"
}

RESTORE_FILE=""

if [ "${BACKUP_RESTORE_FROM_PVC:-}" == "true" ]; then
  # --from-pvc mode: use legacy PVC directly, no S3 fallback
  RESTORE_FILE="${BACKUP_DIR}/${BACKUP_FILE}"
elif [ -n "${BACKUP_CONFIGURATION_LOCAL_BUCKET:-}" ]; then
  # New mode: no PVC, download from local bucket first, fall back to remote bucket
  setup_aws_credentials

  if download_from_s3 "${BACKUP_CONFIGURATION_LOCAL_BUCKET}" "" "${TMP_DIR}"; then
    RESTORE_FILE="${TMP_DIR}/${BACKUP_FILE}"
  elif [ "${BACKUP_CONFIGURATION_UPLOAD_REMOTE}" == "true" ]; then
    echo "=== Backup not found in local bucket, trying remote bucket..."
    if download_from_s3 "${BACKUP_CONFIGURATION_BUCKET_NAME}" "${BACKUP_CONFIGURATION_S3_PREFIX}" "${TMP_DIR}"; then
      RESTORE_FILE="${TMP_DIR}/${BACKUP_FILE}"
    fi
  fi
else
  # Legacy mode: check PVC first, fall back to remote bucket
  RESTORE_FILE="${BACKUP_DIR}/${BACKUP_FILE}"

  if [ "$BACKUP_CONFIGURATION_UPLOAD_REMOTE" == "true" ] && [ ! -f "${RESTORE_FILE}" ]; then
    setup_aws_credentials

    download_from_s3 "${BACKUP_CONFIGURATION_BUCKET_NAME}" "${BACKUP_CONFIGURATION_S3_PREFIX}" "${TMP_DIR}"
    RESTORE_FILE="${TMP_DIR}/${BACKUP_FILE}"
  fi
fi

if [ -z "${RESTORE_FILE}" ] || [ ! -f "${RESTORE_FILE}" ]; then
  echo "=== Backup file \"${BACKUP_FILE}\" not found, exiting..."
  exit 1
fi

echo "=== Restoring settings backup from \"${BACKUP_FILE}\"..."
/opt/docker/bin/settings-backup -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -restore "${RESTORE_FILE}"
echo "=== Settings restore complete"

# === StackPacks Restore ===
if [ "${SKIP_STACKPACKS:-false}" == "true" ]; then
    echo "=== Skipping StackPacks restore (--skip-stackpacks flag set)"
else
    export STACKPACKS_BACKUP_DIR="${BACKUP_DIR}/stackpacks"

    # Construct stackpacks backup filename from the original backup file
    STACKPACKS_FILE="${BACKUP_FILE}.stackpacks.zip"
    STACKPACKS_RESTORE_FILE="${STACKPACKS_BACKUP_DIR}/${STACKPACKS_FILE}"

    echo "=== Checking for StackPacks backup \"${STACKPACKS_FILE}\"..."

    # Check local PVC first, then try S3 if not found and remote is enabled
    if [ ! -f "${STACKPACKS_RESTORE_FILE}" ] && [ "$BACKUP_CONFIGURATION_UPLOAD_REMOTE" == "true" ]; then
        # Ensure AWS credentials are set for S3 access
        export AWS_ACCESS_KEY_ID
        AWS_ACCESS_KEY_ID="$(cat /aws-keys/accesskey)"
        export AWS_SECRET_ACCESS_KEY
        AWS_SECRET_ACCESS_KEY="$(cat /aws-keys/secretkey)"

        # Check if file exists in S3
        if sts-toolbox aws s3 ls --endpoint "http://${MINIO_ENDPOINT}" --region minio --bucket "${BACKUP_CONFIGURATION_BUCKET_NAME}" --prefix "${BACKUP_CONFIGURATION_STACKPACKS_S3_PREFIX}${STACKPACKS_FILE}" 2>/dev/null | grep -q "${STACKPACKS_FILE}"; then
            echo "=== Downloading StackPacks backup from S3..."
            sts-toolbox aws s3 cp --endpoint "http://${MINIO_ENDPOINT}" --region minio "s3://${BACKUP_CONFIGURATION_BUCKET_NAME}/${BACKUP_CONFIGURATION_STACKPACKS_S3_PREFIX}${STACKPACKS_FILE}" "${TMP_DIR}/${STACKPACKS_FILE}"
            STACKPACKS_RESTORE_FILE="${TMP_DIR}/${STACKPACKS_FILE}"
        fi
    fi

    if [ -f "${STACKPACKS_RESTORE_FILE}" ]; then
        echo "=== Restoring StackPacks from \"${STACKPACKS_FILE}\"..."
        /opt/docker/bin/stack-packs-backup -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -restore "${STACKPACKS_RESTORE_FILE}"
        echo "=== StackPacks restore complete"
    else
        echo "=== WARNING: StackPacks backup \"${STACKPACKS_FILE}\" not found, skipping StackPacks restore"
    fi
fi
echo "==="
