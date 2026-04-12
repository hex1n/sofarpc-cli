#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SOFA_RPC_VERSION="${1:-${SOFA_RPC_VERSION:-5.4.0}}"

cd "${ROOT_DIR}"
mvn -Dsofa-rpc.version="${SOFA_RPC_VERSION}" clean package
