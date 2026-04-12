#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME_VERSION="${1:-5.4.0}"
FIXTURE_SRC_DIR="$ROOT_DIR/e2e/fixtures/src"
FIXTURE_TARGET_DIR="$ROOT_DIR/e2e/fixtures/target/classes"
LOG_DIR="$ROOT_DIR/e2e/fixtures/target/orbstack"
ROOT_TARGET_DIR="$ROOT_DIR/target"
ROOT_LAUNCHER_JAR="$ROOT_TARGET_DIR/rpcctl-launcher.jar"
ROOT_RUNTIME_DIR="$ROOT_TARGET_DIR/runtimes/sofa-rpc/${RUNTIME_VERSION}"
ROOT_RUNTIME_JAR="$ROOT_RUNTIME_DIR/rpcctl-runtime-sofa-${RUNTIME_VERSION}.jar"
MODULE_LAUNCHER_JAR="$ROOT_DIR/rpcctl-launcher/target/rpcctl-launcher.jar"
RUNTIME_JAR="$ROOT_DIR/rpcctl-runtime-sofa/target/rpcctl-runtime-sofa-${RUNTIME_VERSION}.jar"
RPCCTL_BIN="$ROOT_DIR/bin/rpcctl"

DIRECT_PORT="${RPCCTL_ORB_DIRECT_PORT:-12240}"
ORDER_PORT="${RPCCTL_ORB_ORDER_PORT:-12241}"
PAYLOAD_PORT="${RPCCTL_ORB_PAYLOAD_PORT:-12243}"
ZK_PORT="${RPCCTL_ORB_ZK_PORT:-32181}"
DIRECT_INTERNAL_PORT=12240
ORDER_INTERNAL_PORT=12241
PAYLOAD_INTERNAL_PORT=12243
NETWORK_NAME="${RPCCTL_ORB_NETWORK:-rpcctl-e2e-orbstack-net}"
DIRECT_CONTAINER="${RPCCTL_ORB_DIRECT_CONTAINER:-rpcctl-e2e-user}"
ORDER_CONTAINER="${RPCCTL_ORB_ORDER_CONTAINER:-rpcctl-e2e-order}"
PAYLOAD_CONTAINER="${RPCCTL_ORB_PAYLOAD_CONTAINER:-rpcctl-e2e-payload}"
ZK_CONTAINER="${RPCCTL_ORB_ZK_CONTAINER:-rpcctl-e2e-zk}"

DIRECT_OUTPUT_FILE="$LOG_DIR/direct-output.json"
ORDER_OUTPUT_FILE="$LOG_DIR/order-output.json"
REGISTRY_OUTPUT_FILE="$LOG_DIR/registry-output.json"
ORDER_MANIFEST_FILE="$LOG_DIR/order-manifest.yaml"
ORDER_MANIFEST_OUTPUT_FILE="$LOG_DIR/order-manifest-output.json"

source "$ROOT_DIR/scripts/e2e-common.sh"

capture_container_log() {
  local container="$1"
  local output="$LOG_DIR/${container}.log"
  docker logs "$container" >"$output" 2>&1 || true
}

wait_for_container_log() {
  local container="$1"
  local marker="$2"
  local label="$3"
  local attempts=50

  while (( attempts > 0 )); do
    if docker logs "$container" 2>&1 | grep -q "$marker"; then
      return 0
    fi
    sleep 1
    attempts=$((attempts - 1))
  done

  capture_container_log "$container"
  fail "timeout waiting for ${label}"
}

cleanup() {
  capture_container_log "$DIRECT_CONTAINER"
  capture_container_log "$ORDER_CONTAINER"
  capture_container_log "$PAYLOAD_CONTAINER"
  capture_container_log "$ZK_CONTAINER"
  docker rm -f "$DIRECT_CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$ORDER_CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$PAYLOAD_CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$ZK_CONTAINER" >/dev/null 2>&1 || true
  docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
}

run_java_container() {
  local name="$1"
  local published_port="$2"
  local internal_port="$3"
  shift 3

  docker rm -f "$name" >/dev/null 2>&1 || true
  docker run --rm -d \
    --name "$name" \
    --network "$NETWORK_NAME" \
    -p "${published_port}:${internal_port}" \
    -v "$FIXTURE_TARGET_DIR:/app/classes:ro" \
    -v "$RUNTIME_JAR:/app/rpcctl-runtime-sofa.jar:ro" \
    eclipse-temurin:17-jre \
    java -cp /app/classes:/app/rpcctl-runtime-sofa.jar "$@" >/dev/null
}

run_direct_smoke() {
  run_java_container \
    "$DIRECT_CONTAINER" \
    "$DIRECT_PORT" \
    "$DIRECT_INTERNAL_PORT" \
    -Drpcctl.e2e.port="$DIRECT_INTERNAL_PORT" \
    -Drpcctl.e2e.host=0.0.0.0 \
    -Drpcctl.e2e.virtualHost=127.0.0.1 \
    -Drpcctl.e2e.virtualPort="$DIRECT_PORT" \
    com.example.ProviderMain

  wait_for_tcp "127.0.0.1" "$DIRECT_PORT" "orbstack direct provider"
  wait_for_container_log "$DIRECT_CONTAINER" "provider-ready" "orbstack direct provider readiness"

  "$RPCCTL_BIN" call \
    com.example.UserService/getUser \
    '[123]' \
    --direct-url "bolt://127.0.0.1:${DIRECT_PORT}" \
    --types java.lang.Long \
    --unique-id user-service \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$DIRECT_OUTPUT_FILE"

  assert_contains "$DIRECT_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "orbstack direct call should succeed"
  assert_contains "$DIRECT_OUTPUT_FILE" '"result"[[:space:]]*:[[:space:]]*"user-123"' "orbstack direct result should match"
  assert_contains "$DIRECT_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"schema"' "orbstack direct payload mode should be schema"
}

run_order_smoke() {
  "$RPCCTL_BIN" manifest generate \
    --output "$ORDER_MANIFEST_FILE" \
    --force \
    --stub-path "$FIXTURE_TARGET_DIR" \
    --service-class com.example.OrderService \
    --service-unique-id com.example.OrderService=order-service >"$ORDER_MANIFEST_OUTPUT_FILE"

  assert_contains "$ORDER_MANIFEST_OUTPUT_FILE" '"importedServiceCount"[[:space:]]*:[[:space:]]*1' "orbstack order manifest import should detect one service"

  run_java_container \
    "$ORDER_CONTAINER" \
    "$ORDER_PORT" \
    "$ORDER_INTERNAL_PORT" \
    -Drpcctl.e2e.port="$ORDER_INTERNAL_PORT" \
    -Drpcctl.e2e.host=0.0.0.0 \
    -Drpcctl.e2e.virtualHost=127.0.0.1 \
    -Drpcctl.e2e.virtualPort="$ORDER_PORT" \
    com.example.OrderProviderMain

  wait_for_tcp "127.0.0.1" "$ORDER_PORT" "orbstack order provider"
  wait_for_container_log "$ORDER_CONTAINER" "order-provider-ready" "orbstack order provider readiness"

  local payload
  payload="$(cat <<'JSON'
[{"requestId":"order-orbstack-01","customer":{"name":"alice","address":{"city":"Shanghai","street":"Century Ave","zipCode":200120}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3},{"sku":"sku-banana","quantity":3,"price":21.33}],"attributes":{"channel":"orbstack-direct","priority":"high"}}]
JSON
)"

  "$RPCCTL_BIN" call \
    com.example.OrderService/submit \
    "$payload" \
    --direct-url "bolt://127.0.0.1:${ORDER_PORT}" \
    --manifest "$ORDER_MANIFEST_FILE" \
    --stub-path "$FIXTURE_TARGET_DIR" \
    --confirm \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$ORDER_OUTPUT_FILE"

  assert_contains "$ORDER_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "orbstack order call should succeed"
  assert_contains "$ORDER_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"schema"' "orbstack order payload mode should be schema"
  assert_contains "$ORDER_OUTPUT_FILE" '"customerName"[[:space:]]*:[[:space:]]*"alice"' "orbstack order response should contain customer name"
  assert_contains "$ORDER_OUTPUT_FILE" '"channel"[[:space:]]*:[[:space:]]*"orbstack-direct"' "orbstack order response should contain channel"
}

run_registry_smoke() {
  docker rm -f "$ZK_CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$PAYLOAD_CONTAINER" >/dev/null 2>&1 || true
  docker run --rm -d \
    --name "$ZK_CONTAINER" \
    --network "$NETWORK_NAME" \
    -p "${ZK_PORT}:2181" \
    zookeeper:3.8 >/dev/null

  wait_for_tcp "127.0.0.1" "$ZK_PORT" "orbstack zookeeper"

  run_java_container \
    "$PAYLOAD_CONTAINER" \
    "$PAYLOAD_PORT" \
    "$PAYLOAD_INTERNAL_PORT" \
    -Drpcctl.e2e.port="$PAYLOAD_INTERNAL_PORT" \
    -Drpcctl.e2e.host=0.0.0.0 \
    -Drpcctl.e2e.virtualHost=127.0.0.1 \
    -Drpcctl.e2e.virtualPort="$PAYLOAD_PORT" \
    -Drpcctl.e2e.zkAddress="${ZK_CONTAINER}:2181" \
    com.example.PayloadProviderRegistryMain

  wait_for_tcp "127.0.0.1" "$PAYLOAD_PORT" "orbstack payload provider"
  wait_for_container_log "$PAYLOAD_CONTAINER" "payload-provider-registry-ready" "orbstack payload provider readiness"

  local payload
  payload="$(cat <<'JSON'
[{"requestId":"payload-orbstack-01","customer":{"name":"alice","address":{"city":"Shanghai","street":"Century Ave","zipCode":200120}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3},{"sku":"sku-banana","quantity":3,"price":21.33}],"meta":{"channel":"orbstack-registry","gift":true,"tags":["orbstack","registry","raw"]}}]
JSON
)"

  "$RPCCTL_BIN" call \
    com.example.PayloadService/submit \
    "$payload" \
    --registry "zookeeper://127.0.0.1:${ZK_PORT}" \
    --types java.util.Map \
    --unique-id payload-service \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$REGISTRY_OUTPUT_FILE"

  assert_contains "$REGISTRY_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "orbstack registry call should succeed"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"raw"' "orbstack registry payload mode should be raw"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"requestId"[[:space:]]*:[[:space:]]*"payload-orbstack-01"' "orbstack registry response should contain request id"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"channel"[[:space:]]*:[[:space:]]*"orbstack-registry"' "orbstack registry response should contain channel"
}

main() {
  trap cleanup EXIT
  require_command java
  require_command javac
  require_command docker

  ensure_build_artifacts
  compile_fixtures

  docker network rm "$NETWORK_NAME" >/dev/null 2>&1 || true
  docker network create "$NETWORK_NAME" >/dev/null

  run_direct_smoke
  run_order_smoke
  run_registry_smoke

  echo "orbstack e2e passed"
}

main "$@"
