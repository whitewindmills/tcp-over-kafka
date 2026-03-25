#!/usr/bin/env bash

# Start the server relay from a deployed environment file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

# Expand the JSON service map into repeatable CLI flags.
service_flags=()
append_json_flags service_flags --service "${SERVER_SERVICES_JSON}"

binary_path="${TCP_OVER_KAFKA_INSTALLED_BINARY:-/usr/local/bin/tcp-over-kafka}"

exec "${binary_path}" server \
	--topic "${KAFKA_TOPIC}" \
	--broker "${BROKER_ADDR}" \
	--group "${SERVER_GROUP}" \
	--platform-id "${SERVER_PLATFORM_ID}" \
	--max-frame "${SERVER_MAX_FRAME_SIZE:-32768}" \
	"${service_flags[@]}"
