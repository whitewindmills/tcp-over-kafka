#!/usr/bin/env bash

# Start the SOCKS5 client proxy from a deployed environment file.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

# Expand the JSON route map into repeatable CLI flags.
route_flags=()
append_json_flags route_flags --route "${CLIENT_ROUTES_JSON}"

binary_path="${TCP_OVER_KAFKA_INSTALLED_BINARY:-/usr/local/bin/tcp-over-kafka}"

exec "${binary_path}" client \
	--topic "${KAFKA_TOPIC}" \
	--broker "${BROKER_ADDR}" \
	--group "${CLIENT_GROUP}" \
	--listen "${CLIENT_LISTEN_ADDR}" \
	--platform-id "${CLIENT_PLATFORM_ID}" \
	--device-id "${CLIENT_DEVICE_ID}" \
	--max-frame "${CLIENT_MAX_FRAME_SIZE:-32768}" \
	"${route_flags[@]}"
