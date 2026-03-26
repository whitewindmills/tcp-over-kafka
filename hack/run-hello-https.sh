#!/usr/bin/env bash

# Start the sample HTTPS target used by the tunnel validation flow.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/lib.sh
. "${SCRIPT_DIR}/lib.sh"

load_runtime_env

helper_path="${HELLO_HTTPS_SCRIPT_PATH:-/usr/local/bin/hello_https_server.py}"
cert_path="${HELLO_HTTPS_CERT}"
key_path="${HELLO_HTTPS_KEY}"

if [ ! -f "${cert_path}" ] || [ ! -f "${key_path}" ]; then
	require_command openssl
	install -d "$(dirname "${cert_path}")"
	openssl req \
		-x509 \
		-newkey rsa:2048 \
		-nodes \
		-keyout "${key_path}" \
		-out "${cert_path}" \
		-days 3650 \
		-subj "/CN=$(hostname -f 2>/dev/null || hostname)" >/dev/null 2>&1
fi

exec "${helper_path}" \
	--bind "${HELLO_HTTPS_BIND:-0.0.0.0}" \
	--port "${HELLO_HTTPS_PORT:-443}" \
	--cert "${cert_path}" \
	--key "${key_path}"
