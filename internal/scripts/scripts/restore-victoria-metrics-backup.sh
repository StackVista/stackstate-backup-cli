#!/usr/bin/env sh
set -eo

if [ "$#" -lt 2 ]; then
  echo "Usage: $0 <s3location> <metrics_addr>"
  echo "Example: $0 sts-victoria-metrics-backup/victoria-metrics-1-20251030143500 127.0.0.1:8421"
  exit 1
fi

S3_LOCATION=$1
METRICS_ADDR=$2
export AWS_ACCESS_KEY_ID
AWS_ACCESS_KEY_ID="$(cat /aws-keys/accesskey)"
export AWS_SECRET_ACCESS_KEY
AWS_SECRET_ACCESS_KEY="$(cat /aws-keys/secretkey)"

/vmrestore-prod -storageDataPath=/storage -src="s3://$S3_LOCATION" -customS3Endpoint="http://$MINIO_ENDPOINT" -httpListenAddr "$METRICS_ADDR"
