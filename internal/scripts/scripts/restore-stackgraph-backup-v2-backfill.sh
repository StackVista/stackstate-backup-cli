#!/usr/bin/env bash
set -Eeuo pipefail

SCRIPT_DIR="$( cd "$( dirname "${BASH_SOURCE[0]}" )" &> /dev/null && pwd )"

source $SCRIPT_DIR/restore-stackgraph-backup-v2-env.sh

echo "=== Backfilling historic StackGraph backup data (v2)"

/opt/docker/bin/stackstate-server -Dlogback.configurationFile=/opt/docker/etc_log/logback.xml -import-v2-backfill "s3a://${BACKUP_V2_LOCATION}"
echo "=== StackGraph restore complete"


