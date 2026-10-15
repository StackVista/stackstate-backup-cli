#!/usr/bin/env bash

set -euo pipefail

for attempt in 1 2 3; do
  if go install "$@"; then
    exit 0
  else
    status=$?
  fi
  if [[ "${attempt}" -eq 3 ]]; then
    exit "${status}"
  fi
  echo "Go tool installation failed (attempt ${attempt}/3); retrying in 2 seconds" >&2
  sleep 2
done
