#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PREFIX="${1:-${PREFIX:-$HOME/.local}}"
DIST_ARCHIVE="$("${ROOT_DIR}/scripts/dist.sh")"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TMP_DIR}"' EXIT

tar -xzf "${DIST_ARCHIVE}" -C "${TMP_DIR}"
PACKAGE_DIR="$(find "${TMP_DIR}" -mindepth 1 -maxdepth 1 -type d | head -n 1)"

if [[ -z "${PACKAGE_DIR}" ]]; then
  echo "Failed to unpack distribution archive: ${DIST_ARCHIVE}" >&2
  exit 1
fi

"${PACKAGE_DIR}/install.sh" "${PREFIX}"
