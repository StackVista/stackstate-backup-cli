#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

govulncheck() {
  printf '%s\n' "$@"
  return "${GOVULNCHECK_TEST_STATUS}"
}
export -f govulncheck

for status in 0 1 2 3 127; do
  export GOVULNCHECK_TEST_STATUS="${status}"
  actual=0
  output="$("${script_dir}/govulncheck-inform.sh" -mode=binary "fixture binary")" || actual=$?
  expected="${status}"
  [[ "${status}" -eq 3 ]] && expected=0

  [[ "${actual}" -eq "${expected}" ]]
  [[ "${output}" == $'-mode=binary\nfixture binary'* ]]
  if [[ "${status}" -eq 3 ]]; then
    [[ "${output}" == *"::warning::"* ]]
  else
    [[ "${output}" != *"::warning::"* ]]
  fi
done

echo "PASS: vulnerability findings warn, tool errors fail, and arguments are preserved"
