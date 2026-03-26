#!/usr/bin/env bash

# Start one symmetric tunnel node from a deployed JSON config file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

binary_path="${TCP_OVER_KAFKA_INSTALLED_BINARY:-/usr/local/bin/tcp-over-kafka}"
config_path="${TCP_OVER_KAFKA_NODE_CONFIG:-/etc/tcp-over-kafka/node.json}"

exec "${binary_path}" node --config "${config_path}"
