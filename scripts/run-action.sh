#!/usr/bin/env bash
set -euo pipefail

if [[ "${RUNNER_OS:-Linux}" != "Linux" ]]; then
  echo "lazycat-action supports Linux runners only" >&2
  exit 1
fi

runner_arch="${RUNNER_ARCH:-$(uname -m)}"
case "${runner_arch}" in
  X64|x86_64|amd64)
    arch="amd64"
    ;;
  ARM64|aarch64|arm64)
    arch="arm64"
    ;;
  *)
    echo "unsupported runner architecture ${runner_arch}; supported values are X64 and ARM64" >&2
    exit 1
    ;;
esac

echo "Action host: linux/${arch}; LazyCat target: loaded from Action configuration"

if [[ -n "${LAZYCAT_ACTION_BINARY:-}" ]]; then
  if [[ ! -f "${LAZYCAT_ACTION_BINARY}" || ! -x "${LAZYCAT_ACTION_BINARY}" || -L "${LAZYCAT_ACTION_BINARY}" ]]; then
    echo "LAZYCAT_ACTION_BINARY must name an executable regular file" >&2
    exit 1
  fi
  exec "${LAZYCAT_ACTION_BINARY}" "$@"
fi

version="${LAZYCAT_ACTION_VERSION:-}"
if [[ ! "${version}" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]]; then
  echo "LAZYCAT_ACTION_VERSION must be an exact v-prefixed SemVer" >&2
  exit 1
fi

tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

archive="lazycat-action_linux_${arch}.tar.gz"
release_repository="${GITHUB_ACTION_REPOSITORY:-wcaqrl/lazycat-action}"
base_url="${LAZYCAT_ACTION_RELEASE_BASE_URL:-https://github.com/${release_repository}/releases/download/${version}}"

curl_args=(
  --fail
  --silent
  --show-error
  --location
  --retry 4
  --retry-all-errors
  --retry-delay 0
  --retry-max-time 300
  --connect-timeout 15
  --max-time 60
)
curl "${curl_args[@]}" "${base_url}/${archive}" -o "${tmp}/${archive}"
curl "${curl_args[@]}" "${base_url}/checksums.txt" -o "${tmp}/checksums.txt"

if ! (cd "${tmp}" && grep -E "^[0-9a-fA-F]{64}  ${archive}$" checksums.txt | sha256sum --check --status); then
  echo "checksum verification failed for ${archive}" >&2
  exit 1
fi

entries="$(tar -tzf "${tmp}/${archive}")"
if [[ "${entries}" != "lazycat-action" ]]; then
  echo "release archive contains unexpected entries" >&2
  exit 1
fi
entry_type="$(tar -tvzf "${tmp}/${archive}" | cut -c1)"
if [[ "${entry_type}" != "-" ]]; then
  echo "release archive binary is not a regular file" >&2
  exit 1
fi

tar -xzf "${tmp}/${archive}" -C "${tmp}" lazycat-action
chmod 0755 "${tmp}/lazycat-action"
set +e
"${tmp}/lazycat-action" "$@"
status=$?
set -e
exit "${status}"
