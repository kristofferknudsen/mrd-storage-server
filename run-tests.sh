#!/bin/bash

set -eu
set -o pipefail

usage() {
  cat <<EOF

Executes the tests defined in this repo under various configurations. Docker Compose is used to run servers and databases
and these services are left running at the end of the test run.

Usage: $0 [options]

Options:
  --in-proc   Executes all unit tests and end-to-end tests. End-to-end tests are run for different supported
              provider configurations with the SUT running in the test process.

  --remote    Runs end-to-end tests are run for different supported
              provider configurations against test servers that are running in
              containers launched with docker compose. This mode builds the container
              image first and is therefore slower.

  --down      Skips running tests and performs docker compose down

  -h, --help  Prints this menu
EOF
}

while [[ $# -gt 0 ]]; do
  key="$1"

  case $key in
  --remote)
    remote=1
    shift
    ;;
  --in-proc)
    inProc=1
    shift
    ;;
  --down)
    down=1
    shift
    ;;
  -h | --help)
    usage
    exit
    ;;
  *)
    echo "ERROR: unknown option \"$key\""
    usage
    exit 1
    ;;
  esac
done

if [ -n "${down:-}" ]; then
  MRD_STORAGE_SERVER_IMAGE="unused" docker compose down --remove-orphans
  exit
fi

if [ -z "${remote:-}" ]; then
  inProc=1
fi

if [ -n "${inProc:-}" ]; then
  echo "******* Running unit tests *******"
  # shellcheck disable=SC2046
  go test $(go list ./... | grep -v github.com/ismrmrd/mrd-storage-server$) | grep -v "\\[[no test files\\]"
fi

echo
echo "******* Setting up environment for E2E tests *******"
if [ -n "${remote:-}" ]; then
  image=$(ko publish --local -t latest --base-import-paths .)
  MRD_STORAGE_SERVER_IMAGE=${image} docker compose --profile remote up -d
else
  MRD_STORAGE_SERVER_IMAGE="unused" docker compose --profile inproc up -d
fi

additionalConfigurations=()

if [ -n "${inProc:-}" ]; then
  additionalConfigurations=("sqlite,filesystem" "postgresql,azureblob")
fi

if [ -n "${remote:-}" ]; then
  additionalConfigurations+=("http://localhost:3334,sqlite,azureblob" "http://localhost:3335,postgresql,filesystem")
fi

for configuration in "${additionalConfigurations[@]}"; do
  echo
  echo "******* Running E2E tests with configuration ${configuration} *******"

  IFS=',' read -r -a additionalConfigurations <<<"${configuration}"
  if [ ${#additionalConfigurations[@]} == 2 ]; then
    TEST_DB_PROVIDER=${additionalConfigurations[0]} TEST_STORAGE_PROVIDER=${additionalConfigurations[1]} go test
  else
    TEST_REMOTE_URL=${additionalConfigurations[0]} go test
  fi
done
