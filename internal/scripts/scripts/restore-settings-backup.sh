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
  local backup_file="$4"
  echo "=== Downloading Settings backup \"${backup_file}\" from bucket \"${bucket}\"..."
  sts-toolbox aws s3 --endpoint "http://${MINIO_ENDPOINT}" --region minio cp "s3://${bucket}/${prefix}${backup_file}" "${dest}/${backup_file}"
}

RESTORE_FILE=""

if [ "${BACKUP_RESTORE_FROM_PVC:-}" == "true" ]; then
  # --from-pvc mode: use legacy PVC directly, no S3 fallback
  RESTORE_FILE="${BACKUP_DIR}/${BACKUP_FILE}"
elif [ -n "${BACKUP_CONFIGURATION_LOCAL_BUCKET:-}" ]; then
  # New mode: no PVC, download from local bucket first, fall back to remote bucket
  setup_aws_credentials

  if download_from_s3 "${BACKUP_CONFIGURATION_LOCAL_BUCKET}" "" "${TMP_DIR}" "${BACKUP_FILE}"; then
    RESTORE_FILE="${TMP_DIR}/${BACKUP_FILE}"
  elif [ "${BACKUP_CONFIGURATION_UPLOAD_REMOTE}" == "true" ]; then
    echo "=== Backup not found in local bucket, trying remote bucket..."
    if download_from_s3 "${BACKUP_CONFIGURATION_BUCKET_NAME}" "${BACKUP_CONFIGURATION_S3_PREFIX}" "${TMP_DIR}" "${BACKUP_FILE}"; then
      RESTORE_FILE="${TMP_DIR}/${BACKUP_FILE}"
    fi
  fi
else
  # Legacy mode: check PVC first, fall back to remote bucket
  RESTORE_FILE="${BACKUP_DIR}/${BACKUP_FILE}"

  if [ "$BACKUP_CONFIGURATION_UPLOAD_REMOTE" == "true" ] && [ ! -f "${RESTORE_FILE}" ]; then
    setup_aws_credentials

    download_from_s3 "${BACKUP_CONFIGURATION_BUCKET_NAME}" "${BACKUP_CONFIGURATION_S3_PREFIX}" "${TMP_DIR}" "${BACKUP_FILE}"
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
# StackPacks backups are always stored, next to the settings backup.
if [ "${SKIP_STACKPACKS:-false}" == "true" ]; then
    echo "=== Skipping StackPacks restore (--skip-stackpacks flag set)"
else
    # Construct stackpacks backup filename from the original backup file
    STACKPACKS_FILE="${BACKUP_FILE}.stackpacks.zip"
    STACKPACKS_RESTORE_FILE=""
    
    echo "=== Checking for StackPacks backup \"${STACKPACKS_FILE}\" in bucket \"${BACKUP_CONFIGURATION_LOCAL_BUCKET}\"..."
    setup_aws_credentials

    if download_from_s3 "${BACKUP_CONFIGURATION_LOCAL_BUCKET}" "${BACKUP_CONFIGURATION_STACKPACKS_S3_PREFIX}" "${TMP_DIR}" "${STACKPACKS_FILE}"; then
      STACKPACKS_RESTORE_FILE="${TMP_DIR}/${STACKPACKS_FILE}"
    elif [ "${BACKUP_CONFIGURATION_UPLOAD_REMOTE}" == "true" ]; then
      echo "=== StackPacks backup not found in kubernetes settings storage, trying main backups storage..."
      if download_from_s3 "${BACKUP_CONFIGURATION_BUCKET_NAME}" "${BACKUP_CONFIGURATION_STACKPACKS_S3_PREFIX}" "${TMP_DIR}" "${STACKPACKS_FILE}"; then
        STACKPACKS_RESTORE_FILE="${TMP_DIR}/${STACKPACKS_FILE}"
      fi
    fi

    if [ -z "${STACKPACKS_RESTORE_FILE}" ] || [ ! -f "${STACKPACKS_RESTORE_FILE}" ]; then
      echo "=== WARNING: StackPacks backup \"${STACKPACKS_FILE}\" not found, skipping StackPacks restore"
      exit 0
    fi

    echo "=== Restoring StackPacks from \"${STACKPACKS_FILE}\"..."
    /opt/docker/bin/stack-packs-backup -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -restore "${STACKPACKS_RESTORE_FILE}"
    echo "=== StackPacks restore complete"
fi
echo "==="
