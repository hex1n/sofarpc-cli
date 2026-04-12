#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME_VERSION="${1:-5.4.0}"
FIXTURE_SRC_DIR="$ROOT_DIR/e2e/fixtures/src"
FIXTURE_TARGET_DIR="$ROOT_DIR/e2e/fixtures/target/classes"
LOG_DIR="$ROOT_DIR/e2e/fixtures/target/logs"
RUNTIME_JAR="$ROOT_DIR/rpcctl-runtime-sofa/target/rpcctl-runtime-sofa-${RUNTIME_VERSION}.jar"
RPCCTL_BIN="$ROOT_DIR/bin/rpcctl"

DIRECT_PORT="${RPCCTL_E2E_DIRECT_PORT:-12240}"
ORDER_PORT="${RPCCTL_E2E_ORDER_PORT:-12241}"
REGISTRY_PORT="${RPCCTL_E2E_REGISTRY_PORT:-12243}"
ZK_PORT="${RPCCTL_E2E_ZK_PORT:-22181}"
ZK_CONTAINER="${RPCCTL_E2E_ZK_CONTAINER:-rpcctl-e2e-zk}"

DIRECT_LOG="$LOG_DIR/direct-provider.log"
ORDER_LOG="$LOG_DIR/order-provider.log"
REGISTRY_LOG="$LOG_DIR/payload-provider-registry.log"
DIRECT_OUTPUT_FILE="$LOG_DIR/direct-output.json"
ORDER_OUTPUT_FILE="$LOG_DIR/order-output.json"
REGISTRY_OUTPUT_FILE="$LOG_DIR/registry-output.json"
ORDER_MANIFEST_FILE="$LOG_DIR/order-manifest.yaml"
ORDER_MANIFEST_OUTPUT_FILE="$LOG_DIR/order-manifest-output.json"

DIRECT_PID=""
ORDER_PID=""
REGISTRY_PID=""

cleanup() {
  if [[ -n "$DIRECT_PID" ]] && kill -0 "$DIRECT_PID" >/dev/null 2>&1; then
    kill "$DIRECT_PID" >/dev/null 2>&1 || true
    wait "$DIRECT_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$REGISTRY_PID" ]] && kill -0 "$REGISTRY_PID" >/dev/null 2>&1; then
    kill "$REGISTRY_PID" >/dev/null 2>&1 || true
    wait "$REGISTRY_PID" >/dev/null 2>&1 || true
  fi
  if [[ -n "$ORDER_PID" ]] && kill -0 "$ORDER_PID" >/dev/null 2>&1; then
    kill "$ORDER_PID" >/dev/null 2>&1 || true
    wait "$ORDER_PID" >/dev/null 2>&1 || true
  fi
  docker rm -f "$ZK_CONTAINER" >/dev/null 2>&1 || true
}

fail() {
  echo "e2e smoke failed: $*" >&2
  exit 1
}

require_command() {
  if ! command -v "$1" >/dev/null 2>&1; then
    fail "missing required command: $1"
  fi
}

wait_for_log() {
  local file="$1"
  local marker="$2"
  local label="$3"
  local attempts=50

  while (( attempts > 0 )); do
    if [[ -f "$file" ]] && grep -q "$marker" "$file"; then
      return 0
    fi
    sleep 1
    attempts=$((attempts - 1))
  done

  if [[ -f "$file" ]]; then
    cat "$file" >&2
  fi
  fail "timeout waiting for ${label}"
}

wait_for_tcp() {
  local host="$1"
  local port="$2"
  local label="$3"
  local attempts=50

  while (( attempts > 0 )); do
    if (echo >/dev/tcp/"$host"/"$port") >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
    attempts=$((attempts - 1))
  done

  fail "timeout waiting for ${label} on ${host}:${port}"
}

assert_contains() {
  local file="$1"
  local pattern="$2"
  local label="$3"

  if ! grep -Eq "$pattern" "$file"; then
    cat "$file" >&2
    fail "assertion failed: ${label}"
  fi
}

compile_fixtures() {
  rm -rf "$FIXTURE_TARGET_DIR" "$LOG_DIR"
  mkdir -p "$FIXTURE_TARGET_DIR" "$LOG_DIR"
  find "$FIXTURE_SRC_DIR" -name '*.java' -print0 \
    | xargs -0 javac -cp "$RUNTIME_JAR" -d "$FIXTURE_TARGET_DIR"
}

run_direct_smoke() {
  java -cp "$FIXTURE_TARGET_DIR:$RUNTIME_JAR" \
    -Drpcctl.e2e.port="$DIRECT_PORT" \
    com.example.ProviderMain >"$DIRECT_LOG" 2>&1 &
  DIRECT_PID=$!

  wait_for_log "$DIRECT_LOG" "provider-ready" "direct provider"
  wait_for_tcp "127.0.0.1" "$DIRECT_PORT" "direct provider"

  "$RPCCTL_BIN" call \
    com.example.UserService/getUser \
    '[123]' \
    --direct-url "bolt://127.0.0.1:${DIRECT_PORT}" \
    --types java.lang.Long \
    --unique-id user-service \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$DIRECT_OUTPUT_FILE"

  assert_contains "$DIRECT_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "direct call should succeed"
  assert_contains "$DIRECT_OUTPUT_FILE" '"result"[[:space:]]*:[[:space:]]*"user-123"' "direct result should match"
  assert_contains "$DIRECT_OUTPUT_FILE" '"paramTypeSource"[[:space:]]*:[[:space:]]*"metadata"' "direct call should resolve types from metadata"
  assert_contains "$DIRECT_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"schema"' "direct payload mode should be schema"
}

run_registry_smoke() {
  require_command docker
  docker rm -f "$ZK_CONTAINER" >/dev/null 2>&1 || true
  docker run --rm -d --name "$ZK_CONTAINER" -p "${ZK_PORT}:2181" zookeeper:3.8 >/dev/null

  wait_for_tcp "127.0.0.1" "$ZK_PORT" "zookeeper"

  java -cp "$FIXTURE_TARGET_DIR:$RUNTIME_JAR" \
    -Drpcctl.e2e.port="$REGISTRY_PORT" \
    -Drpcctl.e2e.zkAddress="127.0.0.1:${ZK_PORT}" \
    -Drpcctl.e2e.host="127.0.0.1" \
    -Drpcctl.e2e.virtualHost="127.0.0.1" \
    com.example.PayloadProviderRegistryMain >"$REGISTRY_LOG" 2>&1 &
  REGISTRY_PID=$!

  wait_for_log "$REGISTRY_LOG" "payload-provider-registry-ready" "registry payload provider"
  wait_for_tcp "127.0.0.1" "$REGISTRY_PORT" "registry payload provider"

  local payload
  payload="$(cat <<'JSON'
[{"requestId":"payload-e2e-01","customer":{"name":"alice","address":{"city":"Shanghai","street":"Century Ave","zipCode":200120}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3},{"sku":"sku-banana","quantity":3,"price":21.33}],"meta":{"channel":"e2e-smoke","gift":true,"tags":["smoke","registry","raw"]}}]
JSON
)"

  "$RPCCTL_BIN" call \
    com.example.PayloadService/submit \
    "$payload" \
    --registry "zookeeper://127.0.0.1:${ZK_PORT}" \
    --types java.util.Map \
    --unique-id payload-service \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$REGISTRY_OUTPUT_FILE"

  assert_contains "$REGISTRY_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "registry call should succeed"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"requestId"[[:space:]]*:[[:space:]]*"payload-e2e-01"' "registry response should contain request id"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"customerName"[[:space:]]*:[[:space:]]*"alice"' "registry response should contain customer name"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"channel"[[:space:]]*:[[:space:]]*"e2e-smoke"' "registry response should contain channel"
  assert_contains "$REGISTRY_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"raw"' "registry payload mode should be raw"
}

run_stub_smoke() {
  "$RPCCTL_BIN" manifest generate \
    --output "$ORDER_MANIFEST_FILE" \
    --force \
    --stub-path "$FIXTURE_TARGET_DIR" \
    --service-class com.example.OrderService \
    --service-unique-id com.example.OrderService=order-service >"$ORDER_MANIFEST_OUTPUT_FILE"

  assert_contains "$ORDER_MANIFEST_OUTPUT_FILE" '"importedServiceCount"[[:space:]]*:[[:space:]]*1' "manifest import should detect one service"
  assert_contains "$ORDER_MANIFEST_FILE" 'com\.example\.OrderService' "manifest should contain imported order service"
  assert_contains "$ORDER_MANIFEST_FILE" 'submit:' "manifest should contain submit method"

  java -cp "$FIXTURE_TARGET_DIR:$RUNTIME_JAR" \
    -Drpcctl.e2e.port="$ORDER_PORT" \
    com.example.OrderProviderMain >"$ORDER_LOG" 2>&1 &
  ORDER_PID=$!

  wait_for_log "$ORDER_LOG" "order-provider-ready" "order provider"
  wait_for_tcp "127.0.0.1" "$ORDER_PORT" "order provider"

  local payload
  payload="$(cat <<'JSON'
[{"requestId":"order-e2e-01","customer":{"name":"alice","address":{"city":"Shanghai","street":"Century Ave","zipCode":200120}},"lines":[{"sku":"sku-apple","quantity":2,"price":12.3},{"sku":"sku-banana","quantity":3,"price":21.33}],"attributes":{"channel":"stub-smoke","priority":"high"}}]
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

  assert_contains "$ORDER_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "stub-aware DTO call should succeed"
  assert_contains "$ORDER_OUTPUT_FILE" '"payloadMode"[[:space:]]*:[[:space:]]*"schema"' "stub-aware call should report schema payload mode"
  assert_contains "$ORDER_OUTPUT_FILE" '"invokeStyle"[[:space:]]*:[[:space:]]*"\$invoke"' 'stub-aware call should use $invoke'
  assert_contains "$ORDER_OUTPUT_FILE" '"customerName"[[:space:]]*:[[:space:]]*"alice"' "stub-aware response should contain customer name"
  assert_contains "$ORDER_OUTPUT_FILE" '"channel"[[:space:]]*:[[:space:]]*"stub-smoke"' "stub-aware response should contain channel"
}

main() {
  trap cleanup EXIT
  require_command java
  require_command javac

  "$ROOT_DIR/scripts/build.sh" "$RUNTIME_VERSION" >/dev/null
  if [[ ! -f "$RUNTIME_JAR" ]]; then
    fail "runtime jar not found: $RUNTIME_JAR"
  fi

  compile_fixtures
  run_direct_smoke
  run_stub_smoke
  run_registry_smoke

  echo "e2e smoke passed"
}

main "$@"
