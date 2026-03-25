#!/usr/bin/env bash

# Start the sample HTTPS target used by the tunnel validation flow.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

helper_path="${HELLO_HTTPS_SCRIPT_PATH:-/usr/local/bin/hello_https_server.py}"

exec "${helper_path}" \
	--bind "${HELLO_HTTPS_BIND:-0.0.0.0}" \
	--port "${HELLO_HTTPS_PORT:-443}" \
	--cert "${HELLO_HTTPS_CERT}" \
	--key "${HELLO_HTTPS_KEY}"
