#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
VERSION="${1:-$(sed -n 's:.*<version>\(.*\)</version>.*:\1:p' "${ROOT_DIR}/pom.xml" | head -n 1)}"
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

VERSION="\${1:-${VERSION}}"
PREFIX="\${2:-\${PREFIX:-\$HOME/.local}}"
BASE_URL="\${RPCCTL_RELEASE_BASE_URL:-https://github.com/hex1n/sofa-rpcctl/releases/download/v\${VERSION}}"
TMP_DIR="\$(mktemp -d)"
trap 'rm -rf "\${TMP_DIR}"' EXIT
ARCHIVE_PATH="\${TMP_DIR}/sofa-rpcctl-\${VERSION}.tar.gz"

curl -fsSL "\${BASE_URL}/sofa-rpcctl-\${VERSION}.tar.gz" -o "\${ARCHIVE_PATH}"
tar -xzf "\${ARCHIVE_PATH}" -C "\${TMP_DIR}"
"\${TMP_DIR}/sofa-rpcctl-\${VERSION}/install.sh" "\${PREFIX}"
EOF
chmod +x "${BOOTSTRAP_PATH}"

(
  cd "${RELEASE_DIR}"
  shasum -a 256 ./* > checksums.txt
)

printf '%s\n' "${RELEASE_DIR}"
