#!/usr/bin/env bash

# Stop the locally managed containerized broker if it is running.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

runtime="${BROKER_RUNTIME:-docker}"
if command -v "$runtime" >/dev/null 2>&1; then
	"$runtime" stop tcp-over-kafka-broker >/dev/null 2>&1 || true
fi
