#!/usr/bin/env bash

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
test_dir="$(mktemp -d)"
trap 'rm -rf "${test_dir}"' EXIT
export test_dir

go() {
  printf '%s\0' "$@" > "${test_dir}/args"
  cmp "${test_dir}/expected-args" "${test_dir}/args" || return 99
  local attempt
  attempt="$(cat "${test_dir}/attempts")"
  attempt=$((attempt + 1))
  echo "${attempt}" > "${test_dir}/attempts"
  if [[ "${attempt}" -le "${failures}" ]]; then
    return "$((40 + attempt))"
  fi
}

sleep() {
  [[ "$#" -eq 1 && "$1" == 2 ]] || return 99
  echo "$1" >> "${test_dir}/sleeps"
}
export -f go sleep

args=(-tags "tag with spaces" "" "example.com/tool*@\$version")
printf '%s\0' install "${args[@]}" > "${test_dir}/expected-args"

for failures in 0 1 2 3; do
  export failures
  echo 0 > "${test_dir}/attempts"
  : > "${test_dir}/sleeps"
  actual=0
  "${script_dir}/install-go-tool.sh" "${args[@]}" || actual=$?
  expected=0
  attempts=$((failures + 1))
  if [[ "${failures}" -eq 3 ]]; then
    expected=43
    attempts=3
  fi
  [[ "${actual}" -eq "${expected}" ]]
  [[ "$(cat "${test_dir}/attempts")" -eq "${attempts}" ]]
  [[ "$(wc -l < "${test_dir}/sleeps")" -eq $((attempts - 1)) ]]
done

echo "PASS: bounded retries, final failure status, delays, and arguments are preserved"
