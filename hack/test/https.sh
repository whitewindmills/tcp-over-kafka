#!/usr/bin/env bash

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=hack/test/lib.sh
. "${SCRIPT_DIR}/lib.sh"

maybe_run_in_kubernetes_runner "$(basename "$0")"

run_https_check() {
	local source_node=$1
	local target_node=$2
	local body

	log "https check ${source_node} -> ${target_node}"
	body="$(https_probe "${source_node}" "${target_node}")"
	[ "${body}" = "hello world" ] || die "unexpected https body from ${target_node}: ${body}"
}

if directional_e2e_mode; then
	assert_node_ready "${E2E_LOCAL_NODE}"
	run_https_check "${E2E_LOCAL_NODE}" "${E2E_REMOTE_NODE}"
else
	assert_node_ready node-a
	assert_node_ready node-b
	run_https_check node-a node-b
	run_https_check node-b node-a
fi

log "https tests passed"
