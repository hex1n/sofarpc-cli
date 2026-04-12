#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
RUNTIME_VERSION="${1:-5.4.0}"
FIXTURE_SRC_DIR="$ROOT_DIR/e2e/fixtures/src"
FIXTURE_TARGET_DIR="$ROOT_DIR/e2e/fixtures/target/classes"
LOG_DIR="$ROOT_DIR/e2e/fixtures/target/logs"
ROOT_TARGET_DIR="$ROOT_DIR/target"
ROOT_LAUNCHER_JAR="$ROOT_TARGET_DIR/rpcctl-launcher.jar"
ROOT_RUNTIME_DIR="$ROOT_TARGET_DIR/runtimes/sofa-rpc/${RUNTIME_VERSION}"
ROOT_RUNTIME_JAR="$ROOT_RUNTIME_DIR/rpcctl-runtime-sofa-${RUNTIME_VERSION}.jar"
MODULE_LAUNCHER_JAR="$ROOT_DIR/rpcctl-launcher/target/rpcctl-launcher.jar"
RUNTIME_JAR="$ROOT_DIR/rpcctl-runtime-sofa/target/rpcctl-runtime-sofa-${RUNTIME_VERSION}.jar"
RPCCTL_BIN="$ROOT_DIR/bin/rpcctl"

DIRECT_PORT="${RPCCTL_E2E_DIRECT_PORT:-12240}"
ORDER_PORT="${RPCCTL_E2E_ORDER_PORT:-12241}"
OVERLOAD_PORT="${RPCCTL_E2E_OVERLOAD_PORT:-12242}"
REGISTRY_PORT="${RPCCTL_E2E_REGISTRY_PORT:-12243}"
ZK_PORT="${RPCCTL_E2E_ZK_PORT:-22181}"
ZK_CONTAINER="${RPCCTL_E2E_ZK_CONTAINER:-rpcctl-e2e-zk}"
UNREACHABLE_PORT="${RPCCTL_E2E_UNREACHABLE_PORT:-13240}"

DIRECT_LOG="$LOG_DIR/direct-provider.log"
ORDER_LOG="$LOG_DIR/order-provider.log"
OVERLOAD_LOG="$LOG_DIR/overloaded-provider.log"
REGISTRY_LOG="$LOG_DIR/payload-provider-registry.log"
DIRECT_OUTPUT_FILE="$LOG_DIR/direct-output.json"
DOCTOR_OUTPUT_FILE="$LOG_DIR/doctor-output.json"
DIRECT_METHOD_ERROR_OUTPUT_FILE="$LOG_DIR/direct-method-error-output.json"
DIRECT_UNREACHABLE_OUTPUT_FILE="$LOG_DIR/direct-unreachable-output.json"
ORDER_OUTPUT_FILE="$LOG_DIR/order-output.json"
OVERLOAD_OUTPUT_FILE="$LOG_DIR/overload-output.json"
OVERLOAD_AMBIGUOUS_OUTPUT_FILE="$LOG_DIR/overload-ambiguous-output.json"
REGISTRY_OUTPUT_FILE="$LOG_DIR/registry-output.json"
ORDER_MANIFEST_FILE="$LOG_DIR/order-manifest.yaml"
ORDER_MANIFEST_OUTPUT_FILE="$LOG_DIR/order-manifest-output.json"
OVERLOAD_MANIFEST_FILE="$LOG_DIR/overload-manifest.yaml"
OVERLOAD_MANIFEST_OUTPUT_FILE="$LOG_DIR/overload-manifest-output.json"

DIRECT_PID=""
ORDER_PID=""
OVERLOAD_PID=""
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
  if [[ -n "$OVERLOAD_PID" ]] && kill -0 "$OVERLOAD_PID" >/dev/null 2>&1; then
    kill "$OVERLOAD_PID" >/dev/null 2>&1 || true
    wait "$OVERLOAD_PID" >/dev/null 2>&1 || true
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

stage_existing_build_artifacts() {
  mkdir -p "$ROOT_TARGET_DIR" "$ROOT_RUNTIME_DIR"

  if [[ ! -f "$ROOT_LAUNCHER_JAR" && -f "$MODULE_LAUNCHER_JAR" ]]; then
    cp "$MODULE_LAUNCHER_JAR" "$ROOT_LAUNCHER_JAR"
  fi

  if [[ ! -f "$ROOT_RUNTIME_JAR" && -f "$RUNTIME_JAR" ]]; then
    cp "$RUNTIME_JAR" "$ROOT_RUNTIME_JAR"
  fi
}

ensure_build_artifacts() {
  stage_existing_build_artifacts

  if [[ -f "$ROOT_LAUNCHER_JAR" && -f "$RUNTIME_JAR" && -f "$ROOT_RUNTIME_JAR" ]]; then
    return 0
  fi

  "$ROOT_DIR/scripts/build.sh" "$RUNTIME_VERSION" >/dev/null
  stage_existing_build_artifacts

  if [[ ! -f "$ROOT_LAUNCHER_JAR" ]]; then
    fail "launcher jar not found: $ROOT_LAUNCHER_JAR"
  fi
  if [[ ! -f "$RUNTIME_JAR" ]]; then
    fail "runtime jar not found: $RUNTIME_JAR"
  fi
  if [[ ! -f "$ROOT_RUNTIME_JAR" ]]; then
    fail "staged runtime jar not found: $ROOT_RUNTIME_JAR"
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
  assert_contains "$DIRECT_OUTPUT_FILE" '"resolvedSofaRpcVersion"[[:space:]]*:[[:space:]]*"'"${RUNTIME_VERSION}"'"' "direct call should expose resolved runtime version"
  assert_contains "$DIRECT_OUTPUT_FILE" '"sofaRpcVersionSource"[[:space:]]*:[[:space:]]*"cli"' "direct call should expose version source"

  "$RPCCTL_BIN" doctor \
    --direct-url "bolt://127.0.0.1:${DIRECT_PORT}" \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$DOCTOR_OUTPUT_FILE"

  assert_contains "$DOCTOR_OUTPUT_FILE" '"ok"[[:space:]]*:[[:space:]]*true' "doctor should succeed against a reachable direct target"
  assert_contains "$DOCTOR_OUTPUT_FILE" '"resolvedVersion"[[:space:]]*:[[:space:]]*"'"${RUNTIME_VERSION}"'"' "doctor should expose resolved runtime version"
  assert_contains "$DOCTOR_OUTPUT_FILE" '"resolvedPath"[[:space:]]*:[[:space:]]*"[^"]+rpcctl-runtime-sofa-'"${RUNTIME_VERSION}"'\.jar"' "doctor should resolve a runtime jar"
  assert_contains "$DOCTOR_OUTPUT_FILE" '"reachable"[[:space:]]*:[[:space:]]*true' "doctor should report reachable target"

  if "$RPCCTL_BIN" call \
    com.example.UserService/missingUser \
    '[123]' \
    --direct-url "bolt://127.0.0.1:${DIRECT_PORT}" \
    --types java.lang.Long \
    --unique-id user-service \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$DIRECT_METHOD_ERROR_OUTPUT_FILE"; then
    cat "$DIRECT_METHOD_ERROR_OUTPUT_FILE" >&2
    fail "missing method call should fail"
  fi

  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*false' "missing method call should fail"
  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"errorCode"[[:space:]]*:[[:space:]]*"RPC_METHOD_NOT_FOUND"' "missing method call should report structured error code"
  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"errorPhase"[[:space:]]*:[[:space:]]*"invoke"' "missing method call should report invoke phase"
  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"retriable"[[:space:]]*:[[:space:]]*false' "missing method call should not be retriable"
  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"invokeStyle"[[:space:]]*:[[:space:]]*"\$invoke"' "missing method diagnostics should include invoke style"
  assert_contains "$DIRECT_METHOD_ERROR_OUTPUT_FILE" '"sofaRpcVersionDeclaredSupported"[[:space:]]*:[[:space:]]*"true"' "missing method call should expose declared version support"

  if "$RPCCTL_BIN" call \
    com.example.UserService/getUser \
    '[123]' \
    --direct-url "bolt://127.0.0.1:${UNREACHABLE_PORT}" \
    --types java.lang.Long \
    --unique-id user-service \
    --timeout-ms 800 \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$DIRECT_UNREACHABLE_OUTPUT_FILE"; then
    cat "$DIRECT_UNREACHABLE_OUTPUT_FILE" >&2
    fail "unreachable target call should fail"
  fi

  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*false' "unreachable target call should fail"
  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"errorCode"[[:space:]]*:[[:space:]]*"RPC_PROVIDER_UNREACHABLE"' "unreachable target should report provider unreachable"
  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"errorPhase"[[:space:]]*:[[:space:]]*"connect"' "unreachable target should report connect phase"
  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"retriable"[[:space:]]*:[[:space:]]*true' "unreachable target should be retriable"
  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"resolvedTarget"[[:space:]]*:[[:space:]]*"bolt://127\.0\.0\.1:'"${UNREACHABLE_PORT}"'"' "unreachable target should preserve resolved direct target"
  assert_contains "$DIRECT_UNREACHABLE_OUTPUT_FILE" '"targetMode"[[:space:]]*:[[:space:]]*"direct"' "unreachable target diagnostics should include target mode"
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

run_overload_smoke() {
  "$RPCCTL_BIN" manifest generate \
    --output "$OVERLOAD_MANIFEST_FILE" \
    --force \
    --stub-path "$FIXTURE_TARGET_DIR" \
    --service-class com.example.OverloadedService \
    --service-unique-id com.example.OverloadedService=overloaded-service >"$OVERLOAD_MANIFEST_OUTPUT_FILE"

  assert_contains "$OVERLOAD_MANIFEST_OUTPUT_FILE" '"importedServiceCount"[[:space:]]*:[[:space:]]*1' "overload manifest import should detect one service"
  assert_contains "$OVERLOAD_MANIFEST_OUTPUT_FILE" '"importedOverloadCount"[[:space:]]*:[[:space:]]*4' "overload manifest import should detect four overloads"
  assert_contains "$OVERLOAD_MANIFEST_FILE" 'overloads:' "overload manifest should preserve overload metadata"

  java -cp "$FIXTURE_TARGET_DIR:$RUNTIME_JAR" \
    -Drpcctl.e2e.port="$OVERLOAD_PORT" \
    com.example.OverloadedProviderMain >"$OVERLOAD_LOG" 2>&1 &
  OVERLOAD_PID=$!

  wait_for_log "$OVERLOAD_LOG" "overloaded-provider-ready" "overloaded provider"
  wait_for_tcp "127.0.0.1" "$OVERLOAD_PORT" "overloaded provider"

  "$RPCCTL_BIN" call \
    com.example.OverloadedService/ping \
    '["alpha",2]' \
    --direct-url "bolt://127.0.0.1:${OVERLOAD_PORT}" \
    --manifest "$OVERLOAD_MANIFEST_FILE" \
    --confirm \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$OVERLOAD_OUTPUT_FILE"

  assert_contains "$OVERLOAD_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "overload call should succeed"
  assert_contains "$OVERLOAD_OUTPUT_FILE" '"result"[[:space:]]*:[[:space:]]*"ping-2:alpha:2"' "overload arity selection should pick the two-arg variant"
  assert_contains "$OVERLOAD_OUTPUT_FILE" '"paramTypeSource"[[:space:]]*:[[:space:]]*"metadata"' "overload arity selection should come from metadata"

  if "$RPCCTL_BIN" call \
    com.example.OverloadedService/lookup \
    '["alice"]' \
    --direct-url "bolt://127.0.0.1:${OVERLOAD_PORT}" \
    --manifest "$OVERLOAD_MANIFEST_FILE" \
    --confirm \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$OVERLOAD_AMBIGUOUS_OUTPUT_FILE" 2>&1; then
    cat "$OVERLOAD_AMBIGUOUS_OUTPUT_FILE" >&2
    fail "overload ambiguity should require --types"
  fi

  assert_contains "$OVERLOAD_AMBIGUOUS_OUTPUT_FILE" 'Pass --types to disambiguate' "overload ambiguity should report a clear error"

  "$RPCCTL_BIN" call \
    com.example.OverloadedService/lookup \
    '[123]' \
    --direct-url "bolt://127.0.0.1:${OVERLOAD_PORT}" \
    --manifest "$OVERLOAD_MANIFEST_FILE" \
    --types java.lang.Long \
    --confirm \
    --sofa-rpc-version "$RUNTIME_VERSION" >"$OVERLOAD_OUTPUT_FILE"

  assert_contains "$OVERLOAD_OUTPUT_FILE" '"success"[[:space:]]*:[[:space:]]*true' "typed overload call should succeed"
  assert_contains "$OVERLOAD_OUTPUT_FILE" '"result"[[:space:]]*:[[:space:]]*"id:123"' "typed overload call should pick the long variant"
}

main() {
  trap cleanup EXIT
  require_command java
  require_command javac

  ensure_build_artifacts

  compile_fixtures
  run_direct_smoke
  run_stub_smoke
  run_overload_smoke
  run_registry_smoke

  echo "e2e smoke passed"
}

main "$@"
