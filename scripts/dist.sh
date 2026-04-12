#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(sed -n 's:.*<version>\(.*\)</version>.*:\1:p' "${ROOT_DIR}/pom.xml" | head -n 1)}"
PACKAGE_NAME="sofa-rpcctl-${VERSION}"
DIST_ROOT="${ROOT_DIR}/dist"
BUILD_ROOT="${ROOT_DIR}/target/dist-build"
PACKAGE_ROOT="${BUILD_ROOT}/${PACKAGE_NAME}"
ARCHIVE_PATH="${DIST_ROOT}/${PACKAGE_NAME}.tar.gz"
TARGET_JAR="${ROOT_DIR}/target/sofa-rpcctl.jar"

if [[ ! -f "${TARGET_JAR}" ]]; then
  "${ROOT_DIR}/scripts/build.sh"
fi

rm -rf "${PACKAGE_ROOT}"
mkdir -p "${PACKAGE_ROOT}/bin" "${PACKAGE_ROOT}/lib" "${PACKAGE_ROOT}/share/sofa-rpcctl" "${DIST_ROOT}"

cp "${TARGET_JAR}" "${PACKAGE_ROOT}/lib/sofa-rpcctl.jar"
cp "${ROOT_DIR}/config/rpcctl.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/rpcctl.yaml"
cp "${ROOT_DIR}/config/metadata.yaml" "${PACKAGE_ROOT}/share/sofa-rpcctl/metadata.yaml"
cp "${ROOT_DIR}/README.md" "${PACKAGE_ROOT}/README.md"

cat > "${PACKAGE_ROOT}/bin/rpcctl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PACKAGE_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"
exec java -jar "${PACKAGE_ROOT}/lib/sofa-rpcctl.jar" "$@"
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
CONFIG_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/sofa-rpcctl"
INSTALLED_BIN="${BIN_DIR}/rpcctl"
INSTALLED_JAR="${LIB_DIR}/sofa-rpcctl.jar"

mkdir -p "${BIN_DIR}" "${LIB_DIR}" "${SHARE_DIR}" "${CONFIG_DIR}"
cp "${PACKAGE_ROOT}/lib/sofa-rpcctl.jar" "${INSTALLED_JAR}"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/rpcctl.yaml" "${SHARE_DIR}/rpcctl.yaml"
cp "${PACKAGE_ROOT}/share/sofa-rpcctl/metadata.yaml" "${SHARE_DIR}/metadata.yaml"

if [[ ! -f "${CONFIG_DIR}/rpcctl.yaml" ]]; then
  cp "${PACKAGE_ROOT}/share/sofa-rpcctl/rpcctl.yaml" "${CONFIG_DIR}/rpcctl.yaml"
fi

if [[ ! -f "${CONFIG_DIR}/metadata.yaml" ]]; then
  cp "${PACKAGE_ROOT}/share/sofa-rpcctl/metadata.yaml" "${CONFIG_DIR}/metadata.yaml"
fi

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
  config: ${CONFIG_DIR}/rpcctl.yaml
  meta:   ${CONFIG_DIR}/metadata.yaml

If '${BIN_DIR}' is not already in PATH, add:
  export PATH="${BIN_DIR}:\$PATH"

Then run:
  rpcctl --help
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

case "${SOURCE}" in
  http://*|https://*)
    curl -fsSL "${SOURCE}" -o "${ARCHIVE_PATH}"
    ;;
  *)
    cp "${SOURCE}" "${ARCHIVE_PATH}"
    ;;
esac

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
