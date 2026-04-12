#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(sed -n 's:.*<version>\(.*\)</version>.*:\1:p' "${ROOT_DIR}/pom.xml" | head -n 1)}"
sha256_file() {
  local file="$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "${file}" | awk '{print $1}'
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "${file}" | awk '{print $NF}'
    return 0
  fi
  echo "Missing SHA-256 tool (need shasum, sha256sum, or openssl)." >&2
  exit 1
}
if [[ -x "${ROOT_DIR}/scripts/build-all-runtimes.sh" ]]; then
  "${ROOT_DIR}/scripts/build-all-runtimes.sh"
else
  bash "${ROOT_DIR}/scripts/build-all-runtimes.sh"
fi

DIST_ARCHIVE="$("${ROOT_DIR}/scripts/dist.sh" "${VERSION}")"
RELEASE_DIR="${ROOT_DIR}/dist/release-assets"
BOOTSTRAP_PATH="${RELEASE_DIR}/get-rpcctl.sh"

mkdir -p "${RELEASE_DIR}"
cp "${DIST_ARCHIVE}" "${RELEASE_DIR}/"

if [[ -d "${ROOT_DIR}/target/runtimes" ]]; then
  find "${ROOT_DIR}/target/runtimes" -type f -name 'rpcctl-runtime-sofa-*.jar' -exec cp {} "${RELEASE_DIR}/" \;
fi

cat > "${BOOTSTRAP_PATH}" <<EOF
#!/usr/bin/env bash
set -euo pipefail

sha256_file() {
  local file="\$1"
  if command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "\${file}" | awk '{print \$1}'
    return 0
  fi
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "\${file}" | awk '{print \$1}'
    return 0
  fi
  if command -v openssl >/dev/null 2>&1; then
    openssl dgst -sha256 "\${file}" | awk '{print \$NF}'
    return 0
  fi
  echo "Missing SHA-256 tool (need shasum, sha256sum, or openssl)." >&2
  exit 1
}

checksum_from_checksums() {
  local checksums_file="\$1"
  local target_name="\$2"
  awk -v target="\${target_name}" '
    NF >= 2 {
      file=\$2
      sub(/^\*/, "", file)
      sub(/^\.\//, "", file)
      if (file == target) {
        print \$1
        exit
      }
    }
  ' "\${checksums_file}"
}

VERSION="\${1:-${VERSION}}"
PREFIX="\${2:-\${PREFIX:-\$HOME/.local}}"
BASE_URL="\${RPCCTL_RELEASE_BASE_URL:-https://github.com/hex1n/sofa-rpcctl/releases/download/v\${VERSION}}"
TMP_DIR="\$(mktemp -d)"
trap 'rm -rf "\${TMP_DIR}"' EXIT
ARCHIVE_PATH="\${TMP_DIR}/sofa-rpcctl-\${VERSION}.tar.gz"
CHECKSUMS_PATH="\${TMP_DIR}/checksums.txt"

curl -fsSL "\${BASE_URL}/sofa-rpcctl-\${VERSION}.tar.gz" -o "\${ARCHIVE_PATH}"
if [[ "\${RPCCTL_SKIP_CHECKSUM:-0}" != "1" ]]; then
  curl -fsSL "\${BASE_URL}/checksums.txt" -o "\${CHECKSUMS_PATH}"
  EXPECTED_CHECKSUM="\$(checksum_from_checksums "\${CHECKSUMS_PATH}" "sofa-rpcctl-\${VERSION}.tar.gz")"
  if [[ -z "\${EXPECTED_CHECKSUM}" ]]; then
    echo "Missing checksum entry for sofa-rpcctl-\${VERSION}.tar.gz in \${BASE_URL}/checksums.txt" >&2
    exit 1
  fi
  ACTUAL_CHECKSUM="\$(sha256_file "\${ARCHIVE_PATH}")"
  if [[ "\${EXPECTED_CHECKSUM}" != "\${ACTUAL_CHECKSUM}" ]]; then
    echo "Checksum mismatch for sofa-rpcctl-\${VERSION}.tar.gz" >&2
    echo "Expected: \${EXPECTED_CHECKSUM}" >&2
    echo "Actual:   \${ACTUAL_CHECKSUM}" >&2
    exit 1
  fi
  echo "Verified SHA-256 for sofa-rpcctl-\${VERSION}.tar.gz" >&2
fi
tar -xzf "\${ARCHIVE_PATH}" -C "\${TMP_DIR}"
"\${TMP_DIR}/sofa-rpcctl-\${VERSION}/install.sh" "\${PREFIX}"
EOF
chmod +x "${BOOTSTRAP_PATH}"

(
  cd "${RELEASE_DIR}"
  : > checksums.txt
  for file in ./*; do
    base="$(basename "${file}")"
    if [[ "${base}" == "checksums.txt" || "${base}" == *.sha256 ]]; then
      continue
    fi
    checksum="$(sha256_file "${file}")"
    printf '%s  %s\n' "${checksum}" "${base}" >> checksums.txt
    printf '%s  %s\n' "${checksum}" "${base}" > "${base}.sha256"
  done
)

printf '%s\n' "${RELEASE_DIR}"
