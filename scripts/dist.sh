#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(sed -n 's:.*<version>\(.*\)</version>.*:\1:p' "${ROOT_DIR}/pom.xml" | head -n 1)}"
PACKAGE_NAME="sofa-rpcctl-${VERSION}"
DIST_ROOT="${ROOT_DIR}/dist"
BUILD_ROOT="${ROOT_DIR}/target/dist-build"
PACKAGE_ROOT="${BUILD_ROOT}/${PACKAGE_NAME}"
ARCHIVE_PATH="${DIST_ROOT}/${PACKAGE_NAME}.tar.gz"
LAUNCHER_JAR="${ROOT_DIR}/target/rpcctl-launcher.jar"
RUNTIMES_DIR="${ROOT_DIR}/target/runtimes"

if [[ ! -f "${LAUNCHER_JAR}" ]]; then
  "${ROOT_DIR}/scripts/build.sh"
fi

if [[ ! -d "${RUNTIMES_DIR}" ]]; then
  echo "Missing ${RUNTIMES_DIR}. Run ./scripts/build.sh first." >&2
  exit 1
fi

rm -rf "${PACKAGE_ROOT}"
mkdir -p "${PACKAGE_ROOT}/bin" "${PACKAGE_ROOT}/lib" "${PACKAGE_ROOT}/share/sofa-rpcctl/examples" "${DIST_ROOT}"

cp "${LAUNCHER_JAR}" "${PACKAGE_ROOT}/lib/rpcctl-launcher.jar"
cp -R "${RUNTIMES_DIR}" "${PACKAGE_ROOT}/lib/runtimes"
cp "${ROOT_DIR}/config/rpcctl.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/rpcctl.yaml"
cp "${ROOT_DIR}/config/metadata.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/metadata.yaml"
cp "${ROOT_DIR}/config/contexts.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/contexts.yaml"
cp "${ROOT_DIR}/rpcctl-manifest.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/rpcctl-manifest.yaml"
cp "${ROOT_DIR}/README.md" "${PACKAGE_ROOT}/README.md"
cp "${ROOT_DIR}/README.zh-CN.md" "${PACKAGE_ROOT}/README.zh-CN.md"

cat > "${PACKAGE_ROOT}/bin/rpcctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
exec java -jar "${PACKAGE_ROOT}/lib/rpcctl-launcher.jar" "$@"
EOF
chmod +x "${PACKAGE_ROOT}/bin/rpcctl"

cat > "${PACKAGE_ROOT}/install.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

PACKAGE_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PREFIX="${1:-${PREFIX:-$HOME/.local}}"
BIN_DIR="${PREFIX}/bin"
LIB_DIR="${PREFIX}/lib/sofa-rpcctl"
SHARE_DIR="${PREFIX}/share/sofa-rpcctl"
INSTALLED_BIN="${BIN_DIR}/rpcctl"
INSTALLED_JAR="${LIB_DIR}/rpcctl-launcher.jar"

mkdir -p "${BIN_DIR}" "${LIB_DIR}" "${SHARE_DIR}/examples"
cp "${PACKAGE_ROOT}/lib/rpcctl-launcher.jar" "${INSTALLED_JAR}"
rm -rf "${LIB_DIR}/runtimes"
cp -R "${PACKAGE_ROOT}/lib/runtimes" "${LIB_DIR}/runtimes"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/rpcctl.yaml" "${SHARE_DIR}/examples/rpcctl.yaml"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/metadata.yaml" "${SHARE_DIR}/examples/metadata.yaml"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/contexts.yaml" "${SHARE_DIR}/examples/contexts.yaml"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/examples/rpcctl-manifest.yaml" "${SHARE_DIR}/examples/rpcctl-manifest.yaml"

cat > "${INSTALLED_BIN}" <<EOS
#!/usr/bin/env bash
set -euo pipefail
exec java -jar "${INSTALLED_JAR}" "\$@"
EOS
chmod +x "${INSTALLED_BIN}"

cat <<EOS
Installed:
  binary: ${INSTALLED_BIN}
  jar:    ${INSTALLED_JAR}
  share:  ${SHARE_DIR}
  examples:
    ${SHARE_DIR}/examples/rpcctl.yaml
    ${SHARE_DIR}/examples/metadata.yaml
    ${SHARE_DIR}/examples/contexts.yaml
    ${SHARE_DIR}/examples/rpcctl-manifest.yaml

If '${BIN_DIR}' is not already in PATH, add:
  export PATH="${BIN_DIR}:\$PATH"

Then run:
  rpcctl --help

Optional:
  copy the example YAML files into ~/.config/sofa-rpcctl/ if you want
  reusable --env shortcuts and metadata-backed list/describe commands.
  copy contexts.yaml into ~/.config/sofa-rpcctl/contexts.yaml if you want
  named profiles via 'rpcctl context use'.
  copy rpcctl-manifest.yaml into a project root if you want auto-discovery,
  defaultEnv, and automatic method/type/uniqueId completion.
EOS
EOF
chmod +x "${PACKAGE_ROOT}/install.sh"

cat > "${PACKAGE_ROOT}/install-from-archive.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail

if [[ $# -lt 1 ]]; then
  echo "Usage: $0 <archive-path-or-url> [prefix]" >&2
  exit 1
fi

SOURCE="$1"
PREFIX="${2:-${PREFIX:-$HOME/.local}}"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT
ARCHIVE_PATH="${TMP_DIR}/rpcctl.tar.gz"
ARCHIVE_BASENAME="$(basename "${SOURCE%%\?*}")"

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

checksum_from_file() {
  local checksum_file="$1"
  [[ -f "${checksum_file}" ]] || return 1
  awk '{print $1}' "${checksum_file}" | head -n 1
}

checksum_from_checksums() {
  local checksums_file="$1"
  local target_name="$2"
  [[ -f "${checksums_file}" ]] || return 1
  awk -v target="${target_name}" '
    NF >= 2 {
      file=$2
      sub(/^\*/, "", file)
      sub(/^\.\//, "", file)
      if (file == target) {
        print $1
        exit
      }
    }
  ' "${checksums_file}"
}

resolve_expected_checksum() {
  local source="$1"
  local target_name="$2"
  local checksum_file
  local checksums_file
  local checksum_value=""
  case "${source}" in
    http://*|https://*|file://*)
      checksum_file="${TMP_DIR}/${target_name}.sha256"
      checksums_file="${TMP_DIR}/checksums.txt"
      if curl -fsSL "${source}.sha256" -o "${checksum_file}" >/dev/null 2>&1; then
        checksum_value="$(checksum_from_file "${checksum_file}" || true)"
      fi
      if [[ -z "${checksum_value}" ]]; then
        local base_url="${source%/*}"
        if curl -fsSL "${base_url}/checksums.txt" -o "${checksums_file}" >/dev/null 2>&1; then
          checksum_value="$(checksum_from_checksums "${checksums_file}" "${target_name}" || true)"
        fi
      fi
      ;;
    *)
      checksum_file="${source}.sha256"
      checksums_file="$(dirname "${source}")/checksums.txt"
      if [[ -f "${checksum_file}" ]]; then
        checksum_value="$(checksum_from_file "${checksum_file}" || true)"
      fi
      if [[ -z "${checksum_value}" && -f "${checksums_file}" ]]; then
        checksum_value="$(checksum_from_checksums "${checksums_file}" "${target_name}" || true)"
      fi
      ;;
  esac
  printf '%s' "${checksum_value}"
}

case "${SOURCE}" in
  http://*|https://*|file://*)
    curl -fsSL "${SOURCE}" -o "${ARCHIVE_PATH}"
    ;;
  *)
    cp "${SOURCE}" "${ARCHIVE_PATH}"
    ;;
esac

if [[ "${RPCCTL_SKIP_CHECKSUM:-0}" != "1" ]]; then
  EXPECTED_CHECKSUM="$(resolve_expected_checksum "${SOURCE}" "${ARCHIVE_BASENAME}")"
  if [[ -n "${EXPECTED_CHECKSUM}" ]]; then
    ACTUAL_CHECKSUM="$(sha256_file "${ARCHIVE_PATH}")"
    if [[ "${EXPECTED_CHECKSUM}" != "${ACTUAL_CHECKSUM}" ]]; then
      echo "Checksum mismatch for ${ARCHIVE_BASENAME}." >&2
      echo "Expected: ${EXPECTED_CHECKSUM}" >&2
      echo "Actual:   ${ACTUAL_CHECKSUM}" >&2
      exit 1
    fi
    echo "Verified SHA-256 for ${ARCHIVE_BASENAME}." >&2
  else
    echo "Warning: no checksum metadata found for ${ARCHIVE_BASENAME}; continuing without archive verification." >&2
  fi
fi

tar -xzf "${ARCHIVE_PATH}" -C "${TMP_DIR}"
PACKAGE_DIR="$(find "${TMP_DIR}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"

if [[ -z "${PACKAGE_DIR}" ]]; then
  echo "Failed to unpack release archive: ${SOURCE}" >&2
  exit 1
fi

"${PACKAGE_DIR}/install.sh" "${PREFIX}"
EOF
chmod +x "${PACKAGE_ROOT}/install-from-archive.sh"

tar -czf "${ARCHIVE_PATH}" -C "${BUILD_ROOT}" "${PACKAGE_NAME}"
printf '%s\n' "${ARCHIVE_PATH}"
