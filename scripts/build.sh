#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOFA_RPC_VERSION="${1:-${SOFA_RPC_VERSION:-5.4.0}}"
MAVEN_REPO_LOCAL="${ROOT_DIR}/.m2"
RUNTIME_TARGET_DIR="${ROOT_DIR}/target/runtimes/sofa-rpc/${SOFA_RPC_VERSION}"
RUNTIME_MANIFEST="${ROOT_DIR}/runtime-manifests/sofa-rpc/${SOFA_RPC_VERSION}.env"
PRESERVED_RUNTIMES_DIR=""
cleanup() {
  if [[ -n "${PRESERVED_RUNTIMES_DIR}" && -d "${PRESERVED_RUNTIMES_DIR}" ]]; then
    rm -rf "${PRESERVED_RUNTIMES_DIR}"
  fi
}
trap cleanup EXIT

if [[ -f "${RUNTIME_MANIFEST}" ]]; then
  # shellcheck disable=SC1090
  source "${RUNTIME_MANIFEST}"
fi

CURATOR_VERSION="${CURATOR_VERSION:-}"
if [[ -z "${CURATOR_VERSION}" ]]; then
  echo "Missing runtime dependency manifest for SOFARPC ${SOFA_RPC_VERSION}." >&2
  echo "Provide CURATOR_VERSION or add ${RUNTIME_MANIFEST}." >&2
  exit 1
fi

cd "${ROOT_DIR}"
if [[ -d "${ROOT_DIR}/target/runtimes" ]]; then
  PRESERVED_RUNTIMES_DIR="$(mktemp -d)"
  cp -R "${ROOT_DIR}/target/runtimes" "${PRESERVED_RUNTIMES_DIR}/runtimes"
fi

mkdir -p "${MAVEN_REPO_LOCAL}"
mvn \
  -Dmaven.repo.local="${MAVEN_REPO_LOCAL}" \
  -Dsofa-rpc.version="${SOFA_RPC_VERSION}" \
  -Dcurator.version="${CURATOR_VERSION}" \
  clean package

mkdir -p "${ROOT_DIR}/target" "${RUNTIME_TARGET_DIR}"
if [[ -n "${PRESERVED_RUNTIMES_DIR}" && -d "${PRESERVED_RUNTIMES_DIR}/runtimes" ]]; then
  mkdir -p "${ROOT_DIR}/target/runtimes"
  cp -R "${PRESERVED_RUNTIMES_DIR}/runtimes/." "${ROOT_DIR}/target/runtimes/"
fi
cp "${ROOT_DIR}/rpcctl-launcher/target/rpcctl-launcher.jar" "${ROOT_DIR}/target/rpcctl-launcher.jar"
cp \
  "${ROOT_DIR}/rpcctl-runtime-sofa/target/rpcctl-runtime-sofa-${SOFA_RPC_VERSION}.jar" \
  "${RUNTIME_TARGET_DIR}/rpcctl-runtime-sofa-${SOFA_RPC_VERSION}.jar"
