#!/usr/bin/env bash

set -euo pipefail

status=0
govulncheck "$@" || status=$?

# Exit code 3 means vulnerability findings.
if [[ "${status}" -eq 3 ]]; then
  echo "::warning::govulncheck found Go vulnerabilities; see the scan output"
  exit 0
fi

exit "${status}"
