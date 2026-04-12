#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
MANIFEST_DIR="${ROOT_DIR}/runtime-manifests/sofa-rpc"

if [[ ! -d "${MANIFEST_DIR}" ]]; then
  echo "Missing runtime manifest directory: ${MANIFEST_DIR}" >&2
  exit 1
fi

FOUND=0
for manifest in "${MANIFEST_DIR}"/*.env; do
  if [[ ! -f "${manifest}" ]]; then
    continue
  fi
  FOUND=1
  version="$(basename "${manifest}" .env)"
  echo "Building SOFARPC runtime ${version}" >&2
  "${ROOT_DIR}/scripts/build.sh" "${version}"
done

if [[ "${FOUND}" -eq 0 ]]; then
  echo "No runtime manifests found under ${MANIFEST_DIR}" >&2
  exit 1
fi
