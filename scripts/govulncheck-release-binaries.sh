#!/usr/bin/env bash

# Binary-mode govulncheck can only judge which packages are linked while the
# release binaries keep their symbol table, so stripping it again (`-s` in the
# GoReleaser ldflags) makes this fail instead of publishing artifacts whose
# reachability nobody can assess.

set -euo pipefail

rootfs_dir="${1:-artifact-scan/rootfs}"
govulncheck_version="${GOVULNCHECK_VERSION:-v1.6.0}"
expected_binaries=5
binaries=()

while IFS= read -r binary; do
  binaries+=("${binary}")
done < <(find "${rootfs_dir}" -type f \( -name sts-backup -o -name sts-backup.exe \) -print | sort)

if [[ "${#binaries[@]}" -ne "${expected_binaries}" ]]; then
  echo "Expected ${expected_binaries} release binaries under '${rootfs_dir}', found ${#binaries[@]}" >&2
  exit 1
fi

status=0
for binary in "${binaries[@]}"; do
  echo "== ${binary}"
  if ! go run "golang.org/x/vuln/cmd/govulncheck@${govulncheck_version}" -mode=binary "${binary}"; then
    status=1
  fi
done

exit "${status}"
