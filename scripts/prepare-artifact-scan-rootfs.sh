#!/usr/bin/env bash

set -euo pipefail

dist_dir="${1:-dist}"
output_dir="${2:-artifact-scan}"
rootfs_dir="${output_dir}/rootfs"
expected_toolchain="$(go env GOVERSION)"
checksum_files=()

while IFS= read -r checksum; do
  checksum_files+=("${checksum}")
done < <(find "${dist_dir}" -maxdepth 1 -type f -name '*_checksums.txt' -print)
if [[ "${#checksum_files[@]}" -ne 1 ]]; then
  echo "Expected exactly one GoReleaser checksum file, found ${#checksum_files[@]}" >&2
  exit 1
fi
checksum_file="${checksum_files[0]}"
checksum_name="$(basename "${checksum_file}")"

if command -v sha256sum >/dev/null 2>&1; then
  (cd "${dist_dir}" && sha256sum --check "${checksum_name}")
else
  (cd "${dist_dir}" && shasum -a 256 --check "${checksum_name}")
fi

rm -rf "${output_dir}"
mkdir -p "${rootfs_dir}/opt/standalone/stackstate-backup-cli" "${output_dir}/metadata"

archive_count=0

prepare_archive() {
  local target="$1"
  local pattern="$2"
  local matches=()
  local binaries=()
  local archive
  local target_dir
  local binary
  local metadata
  local embedded_toolchain
  local embedded_goos
  local embedded_goarch
  local expected_goos
  local expected_goarch

  while IFS= read -r match; do
    matches+=("${match}")
  done < <(find "${dist_dir}" -maxdepth 1 -type f -name "${pattern}" -print)
  if [[ "${#matches[@]}" -ne 1 ]]; then
    echo "Expected exactly one ${target} archive, found ${#matches[@]}" >&2
    exit 1
  fi

  archive="${matches[0]}"
  target_dir="${rootfs_dir}/opt/standalone/stackstate-backup-cli/${target}"
  mkdir -p "${target_dir}"
  case "${archive}" in
    *.zip) unzip -q "${archive}" -d "${target_dir}" ;;
    *.tar.gz) tar -xzf "${archive}" -C "${target_dir}" ;;
    *) echo "Unsupported archive '${archive}'" >&2; exit 1 ;;
  esac

  while IFS= read -r candidate; do
    binaries+=("${candidate}")
  done < <(find "${target_dir}" -type f \( -name sts-backup -o -name sts-backup.exe \) -print)
  if [[ "${#binaries[@]}" -ne 1 ]]; then
    echo "Expected exactly one sts-backup binary in '${archive}', found ${#binaries[@]}" >&2
    exit 1
  fi

  binary="${binaries[0]}"
  metadata="$(go version -m "${binary}")"
  printf '%s\n' "${metadata}" > "${output_dir}/metadata/${target}.txt"

  embedded_toolchain="$(go version "${binary}" | awk '{print $2}')"
  embedded_goos="$(awk -F= '$1 ~ /GOOS$/ {print $2}' <<< "${metadata}")"
  embedded_goarch="$(awk -F= '$1 ~ /GOARCH$/ {print $2}' <<< "${metadata}")"
  expected_goos="${target%-*}"
  expected_goarch="${target##*-}"

  if [[ "${embedded_toolchain}" != "${expected_toolchain}" ]]; then
    echo "${target} was built with ${embedded_toolchain}; expected ${expected_toolchain}" >&2
    exit 1
  fi
  if [[ "${embedded_goos}/${embedded_goarch}" != "${expected_goos}/${expected_goarch}" ]]; then
    echo "${target} contains ${embedded_goos}/${embedded_goarch}; expected ${expected_goos}/${expected_goarch}" >&2
    exit 1
  fi

  archive_count=$((archive_count + 1))
}

prepare_archive darwin-amd64 "*.darwin-x86_64.tar.gz"
prepare_archive darwin-arm64 "*.darwin-arm64.tar.gz"
prepare_archive linux-amd64 "*.linux-x86_64.tar.gz"
prepare_archive linux-arm64 "*.linux-arm64.tar.gz"
prepare_archive windows-amd64 "*.windows-x86_64.zip"

COPYFILE_DISABLE=1 tar --no-xattrs -C "${rootfs_dir}" -cf "${output_dir}/rootfs.tar" .
echo "Prepared and verified ${archive_count} release artifacts built with ${expected_toolchain}"
