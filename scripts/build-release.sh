#!/usr/bin/env bash
# SPDX-License-Identifier: Apache-2.0

set -euo pipefail

script_directory="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
repository_root="$(cd -- "${script_directory}/.." && pwd)"
cd "${repository_root}"

version="${1:-}"
output_directory="${2:-dist}"
if [[ ! "${version}" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  echo "usage: scripts/build-release.sh <major.minor.patch> [new-output-directory]" >&2
  exit 2
fi
expected_version="$(tr -d '[:space:]' < VERSION)"
if [[ "${version}" != "${expected_version}" ]]; then
  echo "version ${version} does not match VERSION (${expected_version})" >&2
  exit 1
fi
if [[ -e "${output_directory}" ]]; then
  echo "output path already exists: ${output_directory}" >&2
  exit 1
fi

commit="${GITHUB_SHA:-$(git rev-parse HEAD)}"
if [[ ! "${commit}" =~ ^[0-9a-fA-F]{40}$ ]]; then
  echo "build commit must be a full hexadecimal Git commit" >&2
  exit 1
fi
source_epoch="${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD)}"
if [[ ! "${source_epoch}" =~ ^[0-9]+$ ]]; then
  echo "SOURCE_DATE_EPOCH must be a non-negative integer" >&2
  exit 1
fi
build_date="${BUILD_DATE:-$(date -u -d "@${source_epoch}" +%Y-%m-%dT%H:%M:%SZ)}"
if [[ ! "${build_date}" =~ ^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z$ ]]; then
  echo "BUILD_DATE must be an RFC 3339 UTC timestamp" >&2
  exit 1
fi

mkdir -p "${output_directory}"
targets=(
  "linux/amd64"
  "linux/arm64"
  "darwin/amd64"
  "darwin/arm64"
  "windows/amd64"
  "windows/arm64"
)
ldflags="-s -w"
ldflags+=" -X github.com/soulteary/Error-Tracer/internal/buildinfo.version=${version}"
ldflags+=" -X github.com/soulteary/Error-Tracer/internal/buildinfo.commit=${commit}"
ldflags+=" -X github.com/soulteary/Error-Tracer/internal/buildinfo.builtAt=${build_date}"

for target in "${targets[@]}"; do
  IFS=/ read -r operating_system architecture <<< "${target}"
  name="error-tracer_${version}_${operating_system}_${architecture}"
  staging_directory="${output_directory}/${name}"
  binary="error-tracer"
  if [[ "${operating_system}" == "windows" ]]; then
    binary+=".exe"
  fi
  mkdir -p "${staging_directory}"
  CGO_ENABLED=0 GOOS="${operating_system}" GOARCH="${architecture}" \
    go build -trimpath -buildvcs=false -ldflags "${ldflags}" \
    -o "${staging_directory}/${binary}" ./cmd/error-tracer
  cp LICENSE NOTICE README.md "${staging_directory}/"
  find "${staging_directory}" -exec touch -d "@${source_epoch}" {} +

  if [[ "${operating_system}" == "windows" ]]; then
    (
      cd "${output_directory}"
      TZ=UTC zip -X -9 -q -r "${name}.zip" "${name}"
    )
  else
    tar --sort=name --mtime="@${source_epoch}" --owner=0 --group=0 \
      --numeric-owner -C "${output_directory}" -cf - "${name}" | \
      gzip -n -9 > "${output_directory}/${name}.tar.gz"
  fi
done

linux_binary="${output_directory}/error-tracer_${version}_linux_amd64/error-tracer"
if [[ "$("${linux_binary}" version)" != "error-tracer ${version} (commit ${commit}, built ${build_date})" ]]; then
  echo "release binary metadata verification failed" >&2
  exit 1
fi

cat > "${output_directory}/release-manifest.txt" <<EOF
version=${version}
commit=${commit}
built_at=${build_date}
source_date_epoch=${source_epoch}
targets=${targets[*]}
EOF
touch -d "@${source_epoch}" "${output_directory}/release-manifest.txt"

printf 'Built Error-Tracer %s release archives in %s\n' "${version}" "${output_directory}"
