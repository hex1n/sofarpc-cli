#!/usr/bin/env bash

fail() {
  echo "e2e failed: $*" >&2
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
