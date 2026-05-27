#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BASELINE_GOLDEN_DIR="${ROOT}/internal/sofarpcwire/testdata/golden"

golden_dir=""
check_drift=1
versions=()
temp_dirs=""

usage() {
  cat <<'EOF'
Usage:
  bash scripts/verify-wire-fixtures.sh
  bash scripts/verify-wire-fixtures.sh --sofarpc-version 5.8.0
  bash scripts/verify-wire-fixtures.sh --matrix 5.4.0,5.7.6,5.8.0

Options:
  --golden-dir DIR          Write/read fixtures from DIR.
  --sofarpc-version VALUE   Run one compatibility check with this SOFARPC version.
  --matrix VALUES           Comma-separated SOFARPC versions to check in temp dirs.
  --no-drift-check          Skip git diff check for committed baseline fixtures.
  -h, --help                Show this help.
EOF
}

cleanup() {
  if [ -z "${temp_dirs}" ]; then
    return
  fi
  while IFS= read -r dir; do
    if [ -n "${dir}" ]; then
      rm -rf "${dir}"
    fi
  done <<<"${temp_dirs}"
}
trap cleanup EXIT

while [ "$#" -gt 0 ]; do
  case "$1" in
    --golden-dir)
      if [ "$#" -lt 2 ]; then
        echo "--golden-dir requires a value" >&2
        exit 2
      fi
      golden_dir="$2"
      shift 2
      ;;
    --sofarpc-version)
      if [ "$#" -lt 2 ]; then
        echo "--sofarpc-version requires a value" >&2
        exit 2
      fi
      versions+=("$2")
      shift 2
      ;;
    --matrix)
      if [ "$#" -lt 2 ]; then
        echo "--matrix requires a comma-separated value" >&2
        exit 2
      fi
      IFS=',' read -r -a matrix_versions <<<"$2"
      for version in "${matrix_versions[@]}"; do
        if [ -n "${version}" ]; then
          versions+=("${version}")
        fi
      done
      shift 2
      ;;
    --no-drift-check)
      check_drift=0
      shift
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "unknown option: $1" >&2
      usage >&2
      exit 2
      ;;
  esac
done

make_temp_dir() {
  local dir
  dir="$(mktemp -d "${TMPDIR:-/tmp}/sofarpcwire-fixtures.XXXXXX")"
  temp_dirs="${temp_dirs}${dir}"$'\n'
  echo "${dir}"
}

run_fixture_check() {
  local version="$1"
  local output_dir="$2"
  local classpath_file="target/classpath.txt"
  local maven_args=(-q)

  mkdir -p "${output_dir}"

  if [ -n "${version}" ]; then
    classpath_file="target/classpath-${version}.txt"
    maven_args+=("-Dsofarpc.version=${version}")
    echo "Verifying SOFARPC ${version} fixtures in ${output_dir}"
  else
    echo "Verifying baseline fixtures in ${output_dir}"
  fi

  (
    cd "${ROOT}"
    go run ./internal/sofarpcwire/testdata/go-fixtures "${output_dir}"
  )

  (
    cd "${ROOT}/internal/sofarpcwire/testdata/java-fixtures"
    mvn "${maven_args[@]}" package
    mvn "${maven_args[@]}" dependency:build-classpath "-Dmdep.outputFile=${classpath_file}"
    java -cp "target/classes:$(cat "${classpath_file}")" com.example.WireFixtureGenerator "${output_dir}"
    java -cp "target/classes:$(cat "${classpath_file}")" com.example.WireFixtureVerifier "${output_dir}"
  )

  (
    cd "${ROOT}"
    SOFARPCWIRE_GOLDEN_DIR="${output_dir}" go test ./internal/sofarpcwire
  )
}

if [ "${#versions[@]}" -eq 0 ]; then
  output_dir="${golden_dir:-${BASELINE_GOLDEN_DIR}}"
  run_fixture_check "" "${output_dir}"

  if [ "${check_drift}" -eq 1 ] && [ "$(cd "${output_dir}" && pwd)" = "$(cd "${BASELINE_GOLDEN_DIR}" && pwd)" ]; then
    (
      cd "${ROOT}"
      git diff --exit-code -- internal/sofarpcwire/testdata/golden/*.json
      if [ "${CI:-}" = "true" ]; then
        untracked="$(git ls-files --others --exclude-standard -- internal/sofarpcwire/testdata/golden/*.json)"
        if [ -n "${untracked}" ]; then
          echo "untracked generated fixture(s):" >&2
          echo "${untracked}" >&2
          exit 1
        fi
      fi
    )
  fi
else
  for version in "${versions[@]}"; do
    if [ -n "${golden_dir}" ] && [ "${#versions[@]}" -eq 1 ]; then
      output_dir="${golden_dir}"
    else
      output_dir="$(make_temp_dir)"
    fi
    run_fixture_check "${version}" "${output_dir}"
  done
fi
