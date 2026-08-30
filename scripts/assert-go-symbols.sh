#!/usr/bin/env bash

# Binary-mode govulncheck can only tell which packages a binary links while its
# Go symbol table is present; without one it conservatively assumes every package
# named in an advisory is linked. A stripped binary therefore still exits
# non-zero today only because an advisory happens to match one of its modules,
# which is a property of the current vulnerability database rather than of the
# artifact — so assessability is asserted here instead of inferred from
# govulncheck's exit status.

set -euo pipefail

# Linked into every Go binary regardless of platform or build tags, so its
# absence means the symbol table was stripped rather than that the program
# differs.
required_symbol='runtime.main'

if [[ "$#" -eq 0 ]]; then
  echo "usage: $0 <binary>..." >&2
  exit 2
fi

status=0
for binary in "$@"; do
  if ! symbols="$(go tool nm "${binary}" 2>&1)"; then
    echo "FAIL ${binary}: no readable Go symbol table -- ${symbols%%$'\n'*}" >&2
    status=1
    continue
  fi
  if ! grep -q "[[:space:]]${required_symbol}\$" <<<"${symbols}"; then
    echo "FAIL ${binary}: symbol table has no ${required_symbol}" >&2
    status=1
    continue
  fi
  echo "OK ${binary}: Go symbol table is usable"
done

exit "${status}"
