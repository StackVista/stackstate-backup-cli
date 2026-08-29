#!/usr/bin/env bash

# Negative control for assert-go-symbols.sh: proves the guard rejects a stripped
# binary for the absence of symbols alone, on a fixture no advisory matches, so
# the result cannot come from the vulnerability database.
#
# Every target in .goreleaser.yml is covered because the two platform families
# fail differently: stripping leaves an ELF or PE file with no symbol section at
# all, while a stripped Mach-O still yields a readable table that `runtime.main`
# is missing from. A guard that only checked whether `go tool nm` succeeded would
# pass both darwin artifacts.

set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
guard="${script_dir}/assert-go-symbols.sh"
fixture_dir="${script_dir}/testdata/assessable-fixture"
targets=(darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64)

work="$(mktemp -d)"
trap 'rm -rf "${work}"' EXIT

export CGO_ENABLED=0
failures=0

for target in "${targets[@]}"; do
  goos="${target%/*}"
  goarch="${target#*/}"
  suffix=""
  [[ "${goos}" == "windows" ]] && suffix=".exe"

  kept="${work}/${goos}-${goarch}-kept${suffix}"
  stripped="${work}/${goos}-${goarch}-stripped${suffix}"
  (cd "${fixture_dir}" && GOOS="${goos}" GOARCH="${goarch}" go build -ldflags '-w' -o "${kept}" main.go)
  (cd "${fixture_dir}" && GOOS="${goos}" GOARCH="${goarch}" go build -ldflags '-s -w' -o "${stripped}" main.go)

  if "${guard}" "${kept}" >/dev/null; then
    echo "PASS ${target}: built with -w only, accepted"
  else
    echo "FAIL ${target}: built with -w only, should have been accepted" >&2
    failures=1
  fi

  if "${guard}" "${stripped}" >/dev/null 2>&1; then
    echo "FAIL ${target}: built with -s -w, should have been rejected" >&2
    failures=1
  else
    echo "PASS ${target}: built with -s -w, rejected"
  fi
done

exit "${failures}"
