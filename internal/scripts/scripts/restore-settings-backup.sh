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

if [ -n "${BACKUP_CONFIGURATION_LOCAL_BUCKET:-}" ]; then
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
echo "==="
