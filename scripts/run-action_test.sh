#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "${tmp}"' EXIT

fixtures="${tmp}/fixtures"
fake_bin="${tmp}/bin"
mkdir -p "${fixtures}" "${fake_bin}"

for arch in amd64 arm64; do
  package_dir="${tmp}/package-${arch}"
  mkdir -p "${package_dir}"
  printf '#!/usr/bin/env bash\necho "binary-%s target=linux/amd64"\n' "${arch}" >"${package_dir}/lazycat-action"
  chmod 0755 "${package_dir}/lazycat-action"
  tar -czf "${fixtures}/lazycat-action_linux_${arch}.tar.gz" -C "${package_dir}" lazycat-action
done
(cd "${fixtures}" && sha256sum lazycat-action_linux_amd64.tar.gz lazycat-action_linux_arm64.tar.gz >checksums.txt)

cat >"${fake_bin}/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url=""
output=""
retry=0
retry_all_errors=false
connect_timeout=""
max_time=""
retry_max_time=""
retry_delay=""
while (($#)); do
  case "$1" in
    -o)
      output="$2"
      shift 2
      ;;
    --retry)
      retry="$2"
      shift 2
      ;;
    --retry-all-errors)
      retry_all_errors=true
      shift
      ;;
    --connect-timeout)
      connect_timeout="$2"
      shift 2
      ;;
    --max-time)
      max_time="$2"
      shift 2
      ;;
    --retry-max-time)
      retry_max_time="$2"
      shift 2
      ;;
    --retry-delay)
      retry_delay="$2"
      shift 2
      ;;
    -*)
      shift
      ;;
    *)
      url="$1"
      shift
      ;;
  esac
done
printf '%s\n' "${url}" >>"${CURL_LOG}"
name="${url##*/}"
if [[ "${FLAKY_DOWNLOAD:-}" == "1" && "${name}" == lazycat-action_linux_*.tar.gz ]]; then
  if ((retry != 4 || connect_timeout != 15 || max_time != 60 || retry_max_time != 300 || retry_max_time < max_time)) || [[ "${retry_all_errors}" != "true" || "${retry_delay}" != "0" ]]; then
    echo "curl: (35) Recv failure: Connection reset by peer" >&2
    exit 35
  fi
  printf '%s\n' "${url}" >>"${CURL_LOG}"
fi
if [[ "${BAD_CHECKSUM:-}" == "1" && "${name}" == "checksums.txt" ]]; then
  printf '%064d  lazycat-action_linux_amd64.tar.gz\n' 0 >"${output}"
else
  cp "${FIXTURE_DIR}/${name}" "${output}"
fi
EOF
chmod 0755 "${fake_bin}/curl"

run_download_case() {
  local runner_arch="$1"
  local archive_arch="$2"
  local action_tmp="${tmp}/action-tmp-${archive_arch}"
  mkdir -p "${action_tmp}"
  : >"${tmp}/curl.log"
  output="$(TMPDIR="${action_tmp}" PATH="${fake_bin}:${PATH}" FIXTURE_DIR="${fixtures}" CURL_LOG="${tmp}/curl.log" GITHUB_ACTION_REPOSITORY=example/lazycat-action RUNNER_OS=Linux RUNNER_ARCH="${runner_arch}" LAZYCAT_ACTION_VERSION=v1.0.0 bash "${root}/scripts/run-action.sh")"
  grep -q "https://github.com/example/lazycat-action/releases/download/v1.0.0/lazycat-action_linux_${archive_arch}.tar.gz" "${tmp}/curl.log"
  grep -q "binary-${archive_arch} target=linux/amd64" <<<"${output}"
  grep -q "Action host: linux/${archive_arch}; LazyCat target: loaded from Action configuration" <<<"${output}"
  test -z "$(find "${action_tmp}" -mindepth 1 -print -quit)"
}

run_download_case X64 amd64
run_download_case ARM64 arm64

: >"${tmp}/curl.log"
output="$(PATH="${fake_bin}:${PATH}" FIXTURE_DIR="${fixtures}" CURL_LOG="${tmp}/curl.log" FLAKY_DOWNLOAD=1 RUNNER_OS=Linux RUNNER_ARCH=X64 LAZYCAT_ACTION_VERSION=v1.0.0 bash "${root}/scripts/run-action.sh")"
test "$(grep -c 'lazycat-action_linux_amd64.tar.gz' "${tmp}/curl.log")" -eq 2
grep -q "binary-amd64 target=linux/amd64" <<<"${output}"

if RUNNER_OS=Windows RUNNER_ARCH=X64 bash "${root}/scripts/run-action.sh" >"${tmp}/windows.out" 2>"${tmp}/windows.err"; then
  echo "Windows runner unexpectedly succeeded" >&2
  exit 1
fi
grep -q "Linux runners only" "${tmp}/windows.err"

if RUNNER_OS=Linux RUNNER_ARCH=ARM bash "${root}/scripts/run-action.sh" >"${tmp}/arm.out" 2>"${tmp}/arm.err"; then
  echo "unsupported ARM runner unexpectedly succeeded" >&2
  exit 1
fi
grep -q "supported values are X64 and ARM64" "${tmp}/arm.err"

: >"${tmp}/curl.log"
if PATH="${fake_bin}:${PATH}" FIXTURE_DIR="${fixtures}" CURL_LOG="${tmp}/curl.log" BAD_CHECKSUM=1 RUNNER_OS=Linux RUNNER_ARCH=X64 LAZYCAT_ACTION_VERSION=v1.0.0 bash "${root}/scripts/run-action.sh" >"${tmp}/checksum.out" 2>"${tmp}/checksum.err"; then
  echo "bad checksum unexpectedly succeeded" >&2
  exit 1
fi
grep -q "checksum verification failed" "${tmp}/checksum.err"
if grep -q "binary-amd64" "${tmp}/checksum.out"; then
  echo "binary executed before checksum verification" >&2
  exit 1
fi

cat >"${tmp}/local-action" <<'EOF'
#!/usr/bin/env bash
echo "local-action target=linux/amd64"
EOF
chmod 0755 "${tmp}/local-action"
output="$(RUNNER_OS=Linux RUNNER_ARCH=ARM64 LAZYCAT_ACTION_BINARY="${tmp}/local-action" bash "${root}/scripts/run-action.sh")"
grep -q "local-action target=linux/amd64" <<<"${output}"

echo "run-action bootstrap tests passed"
